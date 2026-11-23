// Package api is QuotaPulse over HTTP: the JSON API the dashboard and
// scripts use, the event stream that keeps an open dashboard current, the
// Prometheus endpoint, and the dashboard's own files.
//
// It decides who may do what and translates between HTTP and the engine.
// It holds no rules of its own: anything the API can do, an MCP tool or an
// imported bundle does through the same engine calls.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/engine"
	"github.com/itswl/quotapulse/internal/runtime"
	"github.com/itswl/quotapulse/internal/secret"
	"github.com/itswl/quotapulse/internal/store"
)

const (
	sessionCookie = "qp_session"
	sessionLife   = 30 * 24 * time.Hour
	// A session is renewed once it is this close to expiring, so someone who
	// opens the dashboard now and then never has to sign in again.
	sessionRenew = 15 * 24 * time.Hour

	maxBody       = 1 << 20
	maxImportBody = 64 << 20
)

// Server serves QuotaPulse.
type Server struct {
	Engine *engine.Engine
	Keys   *secret.Keyring
	Runner *runtime.Runner
	Log    *slog.Logger
	// Password is QUOTAPULSE_PASSWORD. Set, it is the dashboard's password,
	// and the dashboard cannot change it. Empty, the password is chosen in
	// the dashboard the first time it is opened and kept in the database;
	// until then nobody can sign in (access.go).
	Password string
	// Started is when the server started: a new install can be set up by
	// whoever opens it within setupWindow of that. Zero: when Handler ran.
	Started time.Time
	// Assets is the built dashboard; nil serves a note saying it is missing.
	Assets fs.FS
	// MCP handles /mcp once a token has been checked; nil leaves it off.
	MCP     http.Handler
	Version string
	// Demo marks an instance running on generated data.
	Demo bool

	logins limiter

	// envVerifier is Password's verifier, worked out once.
	envOnce     sync.Once
	envVerifier []byte
	// setupMu makes choosing the password one step: two people cannot both
	// claim an install.
	setupMu sync.Mutex
}

// principal is who a request acts as.
type principal struct {
	// Session is true for someone signed in with the password, who may do
	// everything.
	Session bool
	Token   core.Token
}

func (p principal) allows(scope string) bool {
	return p.Session || p.Token.Allows(scope)
}

// Name identifies the caller in logs.
func (p principal) Name() string {
	if p.Session {
		return "session"
	}
	return "token:" + p.Token.Name
}

type principalKey struct{}

// PrincipalScopes returns the scopes a request was authorized with; the MCP
// handler uses it to decide which tools to offer.
func PrincipalScopes(ctx context.Context) (name string, allows func(scope string) bool) {
	p, _ := ctx.Value(principalKey{}).(principal)
	return p.Name(), p.allows
}

// Handler builds the routes.
func (s *Server) Handler() http.Handler {
	if s.Started.IsZero() {
		s.Started = time.Now()
	}
	mux := http.NewServeMux()

	// Probes are open: an orchestrator has no password.
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)

	mux.HandleFunc("GET /api/v1/session", s.handleSessionInfo)
	mux.HandleFunc("POST /api/v1/session", s.handleSignIn)
	mux.HandleFunc("DELETE /api/v1/session", s.handleSignOut)
	mux.HandleFunc("POST /api/v1/setup", s.handleSetup)

	read, act, ingest, admin := s.scoped(core.ScopeRead), s.scoped(core.ScopeAct), s.scoped(core.ScopeIngest), s.scoped(core.ScopeAdmin)

	mux.Handle("GET /api/v1/overview", read(s.handleOverview))
	mux.Handle("GET /api/v1/stream", read(s.handleStream))
	mux.Handle("GET /metrics", read(s.handleMetrics))

	mux.Handle("GET /api/v1/accounts", read(s.handleAccounts))
	mux.Handle("POST /api/v1/accounts", admin(s.handleCreateAccount))
	mux.Handle("POST /api/v1/accounts/test", admin(s.handleTestAccount))
	mux.Handle("GET /api/v1/accounts/{id}", read(s.handleAccount))
	mux.Handle("PATCH /api/v1/accounts/{id}", admin(s.handleUpdateAccount))
	mux.Handle("DELETE /api/v1/accounts/{id}", admin(s.handleDeleteAccount))
	mux.Handle("POST /api/v1/accounts/{id}/sample", act(s.handleSample))
	mux.Handle("POST /api/v1/sample", act(s.handleSample))

	mux.Handle("GET /api/v1/meters", read(s.handleMeters))
	mux.Handle("GET /api/v1/meters/{id}", read(s.handleMeter))
	mux.Handle("PATCH /api/v1/meters/{id}", admin(s.handleUpdateMeter))
	mux.Handle("DELETE /api/v1/meters/{id}", admin(s.handleDeleteMeter))
	mux.Handle("GET /api/v1/meters/{id}/series", read(s.handleSeries))
	mux.Handle("PUT /api/v1/meters/{id}/mute", act(s.handleMute))
	mux.Handle("DELETE /api/v1/meters/{id}/mute", act(s.handleMute))
	mux.Handle("POST /api/v1/ingest", ingest(s.handleIngest))

	mux.Handle("GET /api/v1/reminders", read(s.handleReminders))
	mux.Handle("POST /api/v1/reminders", act(s.handleCreateReminder))
	mux.Handle("POST /api/v1/reminders/preview", read(s.handlePreviewReminder))
	mux.Handle("GET /api/v1/reminders/{id}", read(s.handleReminder))
	mux.Handle("PATCH /api/v1/reminders/{id}", act(s.handleUpdateReminder))
	mux.Handle("DELETE /api/v1/reminders/{id}", act(s.handleDeleteReminder))
	mux.Handle("POST /api/v1/reminders/{id}/done", act(s.handleReminderDone))
	mux.Handle("DELETE /api/v1/reminders/{id}/done", act(s.handleReminderDone))
	mux.Handle("PUT /api/v1/reminders/{id}/snooze", act(s.handleReminderSnooze))
	mux.Handle("DELETE /api/v1/reminders/{id}/snooze", act(s.handleReminderSnooze))

	mux.Handle("GET /api/v1/incidents", read(s.handleIncidents))
	mux.Handle("POST /api/v1/incidents/{id}/ack", act(s.handleAck))
	mux.Handle("DELETE /api/v1/incidents/{id}/ack", act(s.handleAck))
	mux.Handle("GET /api/v1/deliveries", read(s.handleDeliveries))
	mux.Handle("POST /api/v1/deliveries/{id}/retry", admin(s.handleRetryDelivery))
	mux.Handle("GET /api/v1/spend", read(s.handleSpend))

	mux.Handle("GET /api/v1/providers", read(s.handleProviders))
	mux.Handle("PUT /api/v1/providers/{id}", admin(s.handleSaveProvider))
	mux.Handle("DELETE /api/v1/providers/{id}", admin(s.handleDeleteProvider))

	mux.Handle("GET /api/v1/channel-types", read(s.handleChannelTypes))
	mux.Handle("GET /api/v1/channels", admin(s.handleChannels))
	mux.Handle("POST /api/v1/channels", admin(s.handleCreateChannel))
	mux.Handle("PATCH /api/v1/channels/{id}", admin(s.handleUpdateChannel))
	mux.Handle("DELETE /api/v1/channels/{id}", admin(s.handleDeleteChannel))
	mux.Handle("POST /api/v1/channels/{id}/test", admin(s.handleTestChannel))

	mux.Handle("GET /api/v1/push", admin(s.handlePushKey))
	mux.Handle("POST /api/v1/push/subscriptions", admin(s.handlePushSubscribe))

	mux.Handle("GET /api/v1/tokens", admin(s.handleTokens))
	mux.Handle("POST /api/v1/tokens", admin(s.handleCreateToken))
	mux.Handle("DELETE /api/v1/tokens/{id}", admin(s.handleDeleteToken))

	mux.Handle("PUT /api/v1/password", admin(s.handleChangePassword))

	mux.Handle("GET /api/v1/settings", read(s.handleSettings))
	mux.Handle("PATCH /api/v1/settings", admin(s.handleUpdateSettings))
	mux.Handle("POST /api/v1/digest", admin(s.handleDigest))

	mux.Handle("GET /api/v1/system", admin(s.handleSystem))
	mux.Handle("POST /api/v1/system/backup", admin(s.handleBackup))
	mux.Handle("GET /api/v1/export", admin(s.handleExport))
	mux.Handle("POST /api/v1/import", admin(s.handleImport))

	if s.MCP != nil {
		// Which tools a token gets is decided inside, from its scopes.
		mux.Handle("/mcp", s.authenticated(s.MCP, true))
	}

	// Anything else under /api/ is a mistake, not a page of the dashboard.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "not_found", "there is no such endpoint")
	})
	// Registered without a method: "GET /" would be ambiguous against the
	// method-less patterns above. The handler refuses what is not a read.
	mux.Handle("/", s.assets())

	return s.recovering(s.hardened(s.audited(mux)))
}

// statusWriter remembers the status a handler answered with.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Flush keeps the event stream working through the wrapper.
func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// audited logs every request that changes something: who asked, for what,
// and how it went. Never the body — that is where the credentials are.
func (s *Server) audited(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if safeMethod(r.Method) || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		// The handler fills this in once it knows who is calling.
		who := &caller{name: "anonymous"}
		recorder := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r.WithContext(context.WithValue(r.Context(), callerKey{}, who)))
		s.log().Info("change", "method", r.Method, "path", r.URL.Path, "by", who.name, "status", recorder.status)
	})
}

type callerKey struct{}

// caller is a slot the audit log reads after the request has been
// authenticated further down the chain.
type caller struct{ name string }

func noteCaller(r *http.Request, name string) {
	if who, ok := r.Context().Value(callerKey{}).(*caller); ok {
		who.name = name
	}
}

// scoped wraps a handler so that it runs only for a caller with the scope.
func (s *Server) scoped(scope string) func(http.HandlerFunc) http.Handler {
	return func(next http.HandlerFunc) http.Handler {
		return s.authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, _ := r.Context().Value(principalKey{}).(principal)
			if !p.allows(scope) {
				fail(w, http.StatusForbidden, "forbidden", "this token does not have the "+scope+" scope")
				return
			}
			next(w, r)
		}), false)
	}
}

// authenticated resolves who is calling: a signed-in browser or a token.
// tokenOnly refuses a session, for endpoints meant for machines.
func (s *Server) authenticated(next http.Handler, tokenOnly bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bearer := bearerToken(r); bearer != "" {
			token, ok := s.Engine.Authenticate(r.Context(), bearer)
			if !ok {
				fail(w, http.StatusUnauthorized, "unauthorized", "the token is not valid, or has expired")
				return
			}
			p := principal{Token: token}
			noteCaller(r, p.Name())
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
			return
		}
		if tokenOnly {
			fail(w, http.StatusUnauthorized, "unauthorized", "send an API token as Authorization: Bearer <token>")
			return
		}
		verifier, err := s.verifier(r.Context())
		if err != nil {
			s.failWith(w, r, err)
			return
		}
		if verifier == nil {
			fail(w, http.StatusUnauthorized, "no_password", "no password has been chosen yet: open the dashboard to set it up")
			return
		}
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			fail(w, http.StatusUnauthorized, "unauthorized", "sign in first")
			return
		}
		expires, ok := s.Keys.CheckSession(verifier, cookie.Value, time.Now())
		if !ok {
			fail(w, http.StatusUnauthorized, "unauthorized", "the session has expired; sign in again")
			return
		}
		// A cookie is sent by the browser whoever asked. Requests that
		// change something must come from the dashboard itself.
		if !safeMethod(r.Method) && !sameOrigin(r) {
			fail(w, http.StatusForbidden, "cross_site", "cross-site request refused")
			return
		}
		if time.Until(expires) < sessionRenew {
			s.setSession(w, r, verifier)
		}
		noteCaller(r, "session")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{Session: true})))
	})
}

func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) > 7 && strings.EqualFold(header[:7], "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return ""
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// sameOrigin reports whether a request was made by a page of this site.
// Browsers say so in Sec-Fetch-Site; older ones at least send Origin.
func sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
	default:
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // not a browser's cross-site request: those always carry one
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

func (s *Server) setSession(w http.ResponseWriter, r *http.Request, verifier []byte) {
	expires := time.Now().Add(sessionLife)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: s.Keys.Session(verifier, expires), Path: "/",
		Expires: expires, MaxAge: int(sessionLife.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: isHTTPS(r),
	})
}

// isHTTPS reports whether the browser reached us over TLS, directly or
// through a proxy that says so.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// ---------- session ----------

func (s *Server) handleSessionInfo(w http.ResponseWriter, r *http.Request) {
	verifier, err := s.verifier(r.Context())
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	signedIn := false
	if cookie, err := r.Cookie(sessionCookie); err == nil && verifier != nil {
		_, signedIn = s.Keys.CheckSession(verifier, cookie.Value, time.Now())
	}
	info := map[string]any{
		"signed_in":    signedIn,
		"password_set": verifier != nil,
		"version":      s.Version,
		"demo":         s.Demo,
		"muted":        s.Engine.Muted(),
	}
	if verifier == nil {
		// How the password can be chosen, and whether the install is new
		// enough for the setup to ask its time zone and language as well.
		mode, fresh, err := s.setup(r.Context())
		if err != nil {
			s.failWith(w, r, err)
			return
		}
		info["setup"], info["fresh"] = mode, fresh
	}
	if signedIn {
		info["password_from_env"] = s.Password != ""
		// Every time on screen is shown by the server's clock and in the
		// configured zone, so the dashboard needs both before anything else.
		info["now"] = s.Engine.Now().UTC().Truncate(time.Second)
		if settings, err := s.Engine.Store().Settings(r.Context()); err == nil {
			info["timezone"] = settings.Timezone
		}
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleSignIn(w http.ResponseWriter, r *http.Request) {
	verifier, err := s.verifier(r.Context())
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	if verifier == nil {
		fail(w, http.StatusServiceUnavailable, "no_password", "no password has been chosen yet: open the dashboard to set it up")
		return
	}
	if !sameOrigin(r) {
		fail(w, http.StatusForbidden, "cross_site", "cross-site request refused")
		return
	}
	client := clientKey(r)
	if wait := s.logins.blocked(client, time.Now()); wait > 0 {
		w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
		fail(w, http.StatusTooManyRequests, "too_many_attempts", "too many attempts; wait a minute and try again")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Password == "" || !s.Keys.CheckPassword(body.Password, verifier) {
		s.logins.failed(client, time.Now())
		s.log().Warn("failed sign-in", "client", client)
		fail(w, http.StatusUnauthorized, "wrong_password", "that is not the password")
		return
	}
	s.setSession(w, r, verifier)
	s.learnAddress(r)
	writeJSON(w, http.StatusOK, map[string]any{"signed_in": true})
}

// learnAddress remembers where the owner signed in from a browser, when no
// public URL has been set: the links in notifications then lead somewhere
// without anyone having to configure them. An address only this machine can
// reach teaches nothing, and neither does a demo.
func (s *Server) learnAddress(r *http.Request) {
	if s.Demo {
		return
	}
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || origin.Host == "" || origin.Host != r.Host || (origin.Scheme != "http" && origin.Scheme != "https") {
		return
	}
	if host := origin.Hostname(); host == "localhost" || net.ParseIP(host).IsLoopback() {
		return
	}
	address := origin.Scheme + "://" + origin.Host
	switch learned, err := s.Engine.LearnPublicURL(r.Context(), address); {
	case err != nil:
		s.log().Warn("could not remember the dashboard's address", "error", err)
	case learned:
		s.log().Info("notifications will link to the address just signed in at; change it under Settings", "public_url", address)
	}
}

func (s *Server) handleSignOut(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: isHTTPS(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"signed_in": false})
}

// clientKey identifies who is trying to sign in, for rate limiting. Behind a
// proxy every request shares one address, so the forwarded one is included;
// it can be forged, which is why there is a global limit as well.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		host += "|" + strings.TrimSpace(first)
	}
	return host
}

// limiter slows down password guessing: a few failures per client, and a
// ceiling on failures overall.
type limiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

const (
	loginWindow       = time.Minute
	loginPerClient    = 5
	loginGlobal       = 30
	limiterGlobalName = "*"
)

func (l *limiter) blocked(client string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	for key, limit := range map[string]int{client: loginPerClient, limiterGlobalName: loginGlobal} {
		if attempts := l.failures[key]; len(attempts) >= limit {
			return attempts[0].Add(loginWindow).Sub(now)
		}
	}
	return 0
}

func (l *limiter) failed(client string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failures == nil {
		l.failures = map[string][]time.Time{}
	}
	l.failures[client] = append(l.failures[client], now)
	l.failures[limiterGlobalName] = append(l.failures[limiterGlobalName], now)
}

func (l *limiter) prune(now time.Time) {
	for key, attempts := range l.failures {
		kept := attempts[:0]
		for _, at := range attempts {
			if now.Sub(at) < loginWindow {
				kept = append(kept, at)
			}
		}
		if len(kept) == 0 {
			delete(l.failures, key)
		} else {
			l.failures[key] = kept
		}
	}
}

// ---------- plumbing ----------

// contentSecurityPolicy lets the dashboard load its own files and nothing
// else. Styles allow inline attributes because charts size themselves that way.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; " +
	"base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

func (s *Server) hardened(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if isHTTPS(r) {
			// Reached over TLS once, a browser should never try plain HTTP
			// again — that is how a session cookie ends up on the wire.
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recovering(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				s.log().Error("request handler panicked", "path", r.URL.Path, "panic", recovered)
				fail(w, http.StatusInternalServerError, "internal", "something went wrong on the server")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fail writes an error the way every endpoint does: a stable code for
// programs, a sentence for people.
func fail(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// failWith maps an engine error onto a status.
func (s *Server) failWith(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case engine.IsInvalid(err):
		fail(w, http.StatusBadRequest, "invalid", err.Error())
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found", "there is no such thing")
	case errors.Is(err, store.ErrConflict):
		fail(w, http.StatusConflict, "conflict", "that already exists")
	case errors.Is(err, context.Canceled):
		// the client went away; nobody is listening
	default:
		s.log().Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		fail(w, http.StatusInternalServerError, "internal", "something went wrong on the server")
	}
}

// decode reads a JSON body into v, answering the request itself when it
// cannot. Unknown fields are refused: a typo in a field name should be an
// error, not a silently ignored setting.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeLimited(w, r, v, maxBody)
}

func decodeLimited(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "too_large", "the request body is too large")
		return false
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "invalid", "the request body is not valid: "+jsonProblem(err))
		return false
	}
	return true
}

func jsonProblem(err error) string {
	text := err.Error()
	text = strings.TrimPrefix(text, "json: ")
	return text
}

// language picks the language a response is written in: what the dashboard
// asks for, else what the browser prefers. Empty leaves it to the settings.
func language(r *http.Request) string {
	for _, candidate := range []string{r.URL.Query().Get("lang"), r.Header.Get("X-QuotaPulse-Lang")} {
		if candidate == "zh" || candidate == "en" {
			return candidate
		}
	}
	return ""
}

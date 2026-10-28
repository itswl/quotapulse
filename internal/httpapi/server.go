package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/mailscan"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/monitor"
	"github.com/itswl/quotapulse/internal/push"
	"github.com/itswl/quotapulse/internal/state"
	"github.com/itswl/quotapulse/internal/store"
	"github.com/itswl/quotapulse/internal/subscription"
)

// Server holds every HTTP-layer dependency.
type Server struct {
	Settings *config.Settings
	Resolver *config.Resolver
	Store    store.Store
	State    *state.Manager
	Monitor  *monitor.Monitor
	Subs     *subscription.Checker
	Scanner  *mailscan.Scanner
	Log      *slog.Logger

	// Assets is the bundled frontend, injected from the cmd layer via embed.FS.
	Assets fs.FS
	MCP    http.Handler

	// WrapHandler wraps the router just before it serves; the app installs the
	// metrics middleware here so httpapi stays free of metrics imports. Nil is
	// fine — the router then serves unwrapped.
	WrapHandler func(http.Handler) http.Handler

	// Callbacks like OnBalanceUpdated keep metrics in sync with page actions;
	// otherwise Grafana lags until the next scheduled job after a config edit.
	OnBalanceUpdated      func([]model.CheckResult)
	OnSubscriptionUpdated func([]model.SubscriptionResult)
	OnEmailScanned        func(model.ScanResult)

	refreshGuard cooldown
	scanGuard    cooldown
	Push         *push.Manager

	mcpLimitOnce sync.Once
	mcpLimit     *rateLimiter
}

// ValidateWiring asserts that every dependency the handlers rely on was wired by the
// app constructor. A missing field is a programming error (the field exists on the
// struct but the constructor forgot to set it), so startup fails loudly listing every
// gap instead of serving requests that answer "X is not available".
func (s *Server) ValidateWiring() error {
	// Check fields one by one: a typed nil pointer inside an any does not equal
	// nil, so a map-driven walk would under-report.
	var missing []string
	add := func(name string, wired bool) {
		if !wired {
			missing = append(missing, name)
		}
	}
	add("Log", s.Log != nil)
	add("Settings", s.Settings != nil)
	add("Resolver", s.Resolver != nil)
	add("Store", s.Store != nil)
	add("State", s.State != nil)
	add("Monitor", s.Monitor != nil)
	add("Subs", s.Subs != nil)
	add("Scanner", s.Scanner != nil)
	add("Push", s.Push != nil)
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("httpapi server is missing wired dependencies: %s", strings.Join(missing, ", "))
}

// Cooldown after heavy page-triggered operations.
const guardCooldownSeconds = 30

// Handler wires the routes.
func (s *Server) Handler() http.Handler {
	s.refreshGuard.seconds = guardCooldownSeconds
	s.scanGuard.seconds = guardCooldownSeconds

	mux := http.NewServeMux()

	// probes: /live only proves the process is up; /health checks data and jobs
	mux.HandleFunc("GET /live", s.handleLive)
	mux.HandleFunc("GET /health", s.handleHealth)

	// core read endpoints
	mux.HandleFunc("GET /api/features", s.handleFeatures)
	mux.HandleFunc("POST /api/session", s.handleSessionCreate)
	mux.HandleFunc("DELETE /api/session", s.handleSessionDelete)
	mux.HandleFunc("GET /api/costs/summary", s.handleCostsSummary)
	mux.HandleFunc("POST /api/usage/ingest", s.handleUsageIngest)
	mux.HandleFunc("GET /api/credits", s.handleCredits)
	mux.HandleFunc("GET /api/subscriptions", s.handleSubscriptions)
	mux.HandleFunc("GET /api/jobs", s.handleJobs)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/refresh", s.handleRefresh)
	mux.HandleFunc("POST /api/refresh", s.handleRefresh)
	mux.HandleFunc("POST /api/notify/test", s.handleNotifyTest)
	mux.HandleFunc("GET /api/push/config", s.handlePushConfig)
	mux.HandleFunc("POST /api/push/subscribe", s.handlePushSubscribe)
	mux.HandleFunc("POST /api/push/unsubscribe", s.handlePushUnsubscribe)
	mux.HandleFunc("POST /api/push/test", s.handlePushTest)
	mux.HandleFunc("POST /api/subscription/snooze", s.handleSubscriptionSnooze)
	mux.HandleFunc("POST /api/subscription/timezone", s.handleSubscriptionTimezone)
	mux.HandleFunc("POST /api/subscription/webhook", s.handleSubscriptionWebhook)

	// project config
	mux.HandleFunc("GET /api/providers", s.handleProviders)
	mux.HandleFunc("GET /api/config/providers", s.handleListCustomProviders)
	mux.HandleFunc("POST /api/config/provider", s.handleSaveCustomProvider)
	mux.HandleFunc("POST /api/config/provider/delete", s.handleDeleteCustomProvider)
	mux.HandleFunc("POST /api/config/provider/test", s.handleTestCustomProvider)
	mux.HandleFunc("GET /api/config/projects", s.handleListProjects)
	mux.HandleFunc("POST /api/config/project", s.handleSaveProject)
	mux.HandleFunc("POST /api/config/project/delete", s.handleDeleteProject)
	mux.HandleFunc("POST /api/config/threshold", s.handleUpdateThreshold)

	// subscriptions
	mux.HandleFunc("GET /api/config/subscriptions", s.handleListSubscriptions)
	mux.HandleFunc("POST /api/config/subscription", s.handleUpdateSubscription)
	mux.HandleFunc("POST /api/subscription/add", s.handleAddSubscription)
	mux.HandleFunc("POST /api/subscription/delete", s.handleDeleteSubscription)
	mux.HandleFunc("DELETE /api/subscription/delete", s.handleDeleteSubscription)
	mux.HandleFunc("POST /api/subscription/mark_renewed", s.handleMarkRenewed)
	mux.HandleFunc("POST /api/subscription/clear_renewed", s.handleClearRenewed)

	// mailboxes
	mux.HandleFunc("GET /api/config/emails", s.handleListMailboxes)
	mux.HandleFunc("POST /api/config/email", s.handleSaveMailbox)
	mux.HandleFunc("POST /api/config/email/delete", s.handleDeleteMailbox)
	mux.HandleFunc("GET /api/email/scan", s.handleEmailScanState)
	mux.HandleFunc("POST /api/email/scan", s.handleEmailScan)

	// history queries: the whole group is not registered without ENABLE_HISTORY_API, so it 404s, matching the legacy version
	if s.Settings.EnableHistoryAPI {
		mux.HandleFunc("GET /api/history/balance", s.handleBalanceHistory)
		mux.HandleFunc("GET /api/history/trend/{projectID}", s.handleBalanceTrend)
		mux.HandleFunc("GET /api/history/alerts", s.handleAlertHistory)
		mux.HandleFunc("GET /api/history/stats", s.handleAlertStats)
		mux.HandleFunc("GET /api/history/email-alerts", s.handleEmailAlertHistory)
		// Muting a sender needs somewhere to keep it, like the history it filters.
		mux.HandleFunc("GET /api/email/suppressions", s.handleListEmailSuppressions)
		mux.HandleFunc("POST /api/email/suppression", s.handleAddEmailSuppression)
		mux.HandleFunc("POST /api/email/suppression/delete", s.handleDeleteEmailSuppression)
	}
	if s.Settings.EnableMCP && s.MCP != nil {
		mux.Handle("POST /mcp", s.MCP)
		mux.Handle("GET /mcp", s.MCP)
		mux.Handle("DELETE /mcp", s.MCP)
	}

	// dashboard static assets, last as the fallback
	mux.Handle("GET /", s.assetHandler())

	return s.withCORS(s.withAPIKey(s.withRecovery(mux)))
}

// withAPIKey guards every /api/ path. Without WEB_API_KEY everything is 503:
// far safer than defaulting to open — the keys live behind this.
func (s *Server) withAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protected := strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/mcp"
		if !protected || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		token := extractAPIKey(r)
		if r.URL.Path == "/mcp" {
			s.serveMCP(w, r, token, next)
			return
		}
		keys := s.Settings.APIKeys()
		if len(keys) == 0 {
			fail(w, http.StatusServiceUnavailable, "API key is not configured; set WEB_API_KEY")
			return
		}
		matched := false
		for _, key := range keys {
			if subtle.ConstantTimeCompare([]byte(token), []byte(key)) == 1 {
				matched = true
				break
			}
		}
		viaCookie := false
		if !matched && token == "" {
			// A browser that typed the key once carries this instead. The
			// value is an HMAC of its expiry, not the key itself.
			if cookie, err := r.Cookie(sessionCookieName); err == nil {
				signer, expires, ok := sessionSigner(cookie.Value, keys, time.Now())
				if ok {
					matched, viaCookie = true, true
					if time.Until(expires) < sessionRenewWithin {
						s.setSessionCookie(w, r, signer)
					}
				}
			}
		}
		if !matched {
			fail(w, http.StatusUnauthorized, "API key is invalid or missing")
			return
		}
		if viaCookie && !safeMethod(r.Method) && !sameSiteRequest(r) {
			fail(w, http.StatusForbidden, "cross-site request refused")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// serveMCP authenticates an /mcp request. MCP keys are read-only and scoped, and are
// accepted here only, never on /api/*. WEB_API_KEY is accepted too, with every scope,
// unless MCP_REQUIRE_SCOPED_KEY is set. The caller rides along in the request context,
// so the MCP server lists and serves only what its scopes allow.
func (s *Server) serveMCP(w http.ResponseWriter, r *http.Request, token string, next http.Handler) {
	if len(s.Settings.MCPKeys) == 0 && len(s.Settings.APIKeys()) == 0 {
		fail(w, http.StatusServiceUnavailable, "API key is not configured; set WEB_API_KEY or MCP_API_KEYS")
		return
	}
	caller, ok, expired := s.mcpCaller(token, time.Now())
	if expired {
		fail(w, http.StatusUnauthorized, "This MCP key has expired")
		return
	}
	if !ok {
		fail(w, http.StatusUnauthorized, "API key is invalid or missing")
		return
	}
	s.mcpLimitOnce.Do(func() { s.mcpLimit = newRateLimiter(s.Settings.MCPRateLimitPerMinute) })
	if wait := s.mcpLimit.take(caller.Name, time.Now()); wait > 0 {
		seconds := int(math.Ceil(wait.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		fail(w, http.StatusTooManyRequests, fmt.Sprintf("MCP rate limit reached; retry in %d seconds", seconds))
		return
	}
	next.ServeHTTP(w, r.WithContext(config.WithMCPCaller(r.Context(), caller)))
}

// projectDisabled reports whether a run named a disabled project, which is never checked.
func projectDisabled(err error) bool { return errors.Is(err, monitor.ErrProjectDisabled) }

// refreshStatus is the HTTP status for a failed refresh: a named project that doesn't
// exist, or is disabled, is the caller's mistake, not ours.
func refreshStatus(err error) int {
	switch {
	case errors.Is(err, monitor.ErrProjectNotFound):
		return http.StatusNotFound
	case projectDisabled(err):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// mcpCaller matches token against the MCP keys, then WEB_API_KEY. Every comparison is
// constant-time, like the web key check.
func (s *Server) mcpCaller(token string, now time.Time) (caller config.MCPCaller, ok, expired bool) {
	if token == "" {
		return caller, false, false
	}
	for _, key := range s.Settings.MCPKeys {
		if subtle.ConstantTimeCompare([]byte(token), []byte(key.Key)) == 1 {
			if key.Expired(now) {
				return caller, false, true
			}
			return config.MCPCaller{Name: key.Name, Scopes: key.Scopes}, true, false
		}
	}
	if !s.Settings.MCPRequireScopedKey {
		for _, key := range s.Settings.APIKeys() {
			if subtle.ConstantTimeCompare([]byte(token), []byte(key)) == 1 {
				return config.FullMCPCaller("web"), true, false
			}
		}
	}
	return caller, false, false
}

func extractAPIKey(r *http.Request) string {
	if token := strings.TrimSpace(r.Header.Get("X-API-Key")); token != "" {
		return token
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	if !s.Settings.WebEnableCORS {
		return next
	}
	allowed := s.Settings.CORSOriginList()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		for _, candidate := range allowed {
			if candidate == origin || candidate == "*" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization, If-None-Match")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				break
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withRecovery catches handler panics: one bad request must not take down the process.
func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.log().Error("Request handler panicked", "path", r.URL.Path, "panic", recovered)
				fail(w, http.StatusInternalServerError, "Internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// assetHandler serves the bundled frontend; without embedded assets it prints a hint so dev issues are obvious.
func (s *Server) assetHandler() http.Handler {
	if s.Assets == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "The frontend is not embedded; run npm --prefix ui run build before building the binary", http.StatusNotFound)
		})
	}
	files := http.FileServer(http.FS(s.Assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SPA: any non-asset path falls back to index.html; the frontend switches views by hash
		fallback := false
		if path := strings.TrimPrefix(r.URL.Path, "/"); path != "" {
			if _, err := fs.Stat(s.Assets, path); err != nil {
				r = r.Clone(r.Context())
				r.URL.Path = "/"
				fallback = true
			}
		}
		// HTML (and the manifest) must revalidate every load: only index.html
		// points at the content-hashed asset URLs (?v=<sha256>), so a cached
		// page pins a stale bundle for as long as proxies honor its TTL.
		if fallback || r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") || strings.HasSuffix(r.URL.Path, ".webmanifest") {
			w.Header().Set("Cache-Control", "no-cache")
		} else if r.URL.Query().Has("v") {
			// ?v=<content hash> makes this exact URL immutable; deploys change the query
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Server) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// ListenAndServe runs the server and shuts down gracefully when ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	handler := s.Handler()
	if s.WrapHandler != nil {
		handler = s.WrapHandler(handler)
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		// keep serving briefly after the stop signal: K8s removes the Pod from endpoints
		// and sends SIGTERM in parallel; closing immediately drops the last requests.
		// No shell outside the container to sleep, so wait here.
		if delay := s.Settings.ShutdownDelaySeconds; delay > 0 {
			s.log().Info("Stop signal received; still serving for a moment before shutting down", "delay_seconds", delay)
			time.Sleep(time.Duration(delay) * time.Second)
		}
		s.log().Info("Shutting down the web server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	s.log().Info("Web server started", "addr", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

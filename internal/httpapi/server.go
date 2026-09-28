package httpapi

import (
	"context"
	"crypto/subtle"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/mailscan"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/monitor"
	"github.com/itswl/quotapulse/internal/state"
	"github.com/itswl/quotapulse/internal/store"
	"github.com/itswl/quotapulse/internal/subscription"
)

// Implementation note.
type Server struct {
	Settings *config.Settings
	Resolver *config.Resolver
	Store    store.Store
	State    *state.Manager
	Monitor  *monitor.Monitor
	Subs     *subscription.Checker
	Scanner  *mailscan.Scanner
	Log      *slog.Logger

	// Implementation note.
	Assets fs.FS
	MCP    http.Handler

	// Implementation note.
	// Implementation note.
	OnBalanceUpdated      func([]model.CheckResult)
	OnSubscriptionUpdated func([]model.SubscriptionResult)
	OnEmailScanned        func(model.ScanResult)

	refreshGuard cooldown
	scanGuard    cooldown
}

// Implementation note.
const guardCooldownSeconds = 30

// Implementation note.
func (s *Server) Handler() http.Handler {
	s.refreshGuard.seconds = guardCooldownSeconds
	s.scanGuard.seconds = guardCooldownSeconds

	mux := http.NewServeMux()

	// Implementation note.
	mux.HandleFunc("GET /live", s.handleLive)
	mux.HandleFunc("GET /health", s.handleHealth)

	// Implementation note.
	mux.HandleFunc("GET /api/features", s.handleFeatures)
	mux.HandleFunc("GET /api/credits", s.handleCredits)
	mux.HandleFunc("GET /api/subscriptions", s.handleSubscriptions)
	mux.HandleFunc("GET /api/jobs", s.handleJobs)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/refresh", s.handleRefresh)
	mux.HandleFunc("POST /api/refresh", s.handleRefresh)
	mux.HandleFunc("POST /api/notify/test", s.handleNotifyTest)

	// Implementation note.
	mux.HandleFunc("GET /api/providers", s.handleProviders)
	mux.HandleFunc("GET /api/config/projects", s.handleListProjects)
	mux.HandleFunc("POST /api/config/project", s.handleSaveProject)
	mux.HandleFunc("POST /api/config/project/delete", s.handleDeleteProject)
	mux.HandleFunc("POST /api/config/threshold", s.handleUpdateThreshold)

	// Implementation note.
	mux.HandleFunc("GET /api/config/subscriptions", s.handleListSubscriptions)
	mux.HandleFunc("POST /api/config/subscription", s.handleUpdateSubscription)
	mux.HandleFunc("POST /api/subscription/add", s.handleAddSubscription)
	mux.HandleFunc("POST /api/subscription/delete", s.handleDeleteSubscription)
	mux.HandleFunc("DELETE /api/subscription/delete", s.handleDeleteSubscription)
	mux.HandleFunc("POST /api/subscription/mark_renewed", s.handleMarkRenewed)
	mux.HandleFunc("POST /api/subscription/clear_renewed", s.handleClearRenewed)

	// Implementation note.
	mux.HandleFunc("GET /api/config/emails", s.handleListMailboxes)
	mux.HandleFunc("POST /api/config/email", s.handleSaveMailbox)
	mux.HandleFunc("POST /api/config/email/delete", s.handleDeleteMailbox)
	mux.HandleFunc("GET /api/email/scan", s.handleEmailScanState)
	mux.HandleFunc("POST /api/email/scan", s.handleEmailScan)

	// Implementation note.
	if s.Settings.EnableHistoryAPI {
		mux.HandleFunc("GET /api/history/balance", s.handleBalanceHistory)
		mux.HandleFunc("GET /api/history/trend/{projectID}", s.handleBalanceTrend)
		mux.HandleFunc("GET /api/history/alerts", s.handleAlertHistory)
		mux.HandleFunc("GET /api/history/stats", s.handleAlertStats)
		mux.HandleFunc("GET /api/history/email-alerts", s.handleEmailAlertHistory)
	}
	if s.Settings.EnableMCP && s.MCP != nil {
		mux.Handle("POST /mcp", s.MCP)
		mux.Handle("GET /mcp", s.MCP)
		mux.Handle("DELETE /mcp", s.MCP)
	}

	// Implementation note.
	mux.Handle("GET /", s.assetHandler())

	return s.withCORS(s.withAPIKey(s.withRecovery(mux)))
}

// Implementation note.
// Implementation note.
func (s *Server) withAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protected := strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/mcp"
		if !protected || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		keys := s.Settings.APIKeys()
		if len(keys) == 0 {
			fail(w, http.StatusServiceUnavailable, "API Key operation,operation WEB_API_KEY")
			return
		}
		token := extractAPIKey(r)
		matched := false
		for _, key := range keys {
			if subtle.ConstantTimeCompare([]byte(token), []byte(key)) == 1 {
				matched = true
				break
			}
		}
		if token == "" || !matched {
			fail(w, http.StatusUnauthorized, "API Key operation")
			return
		}
		next.ServeHTTP(w, r)
	})
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

// Implementation note.
func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.log().Error("operation panic", "path", r.URL.Path, "panic", recovered)
				fail(w, http.StatusInternalServerError, "operation")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Implementation note.
func (s *Server) assetHandler() http.Handler {
	if s.Assets == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "operation,operation npm --prefix ui run build operation", http.StatusNotFound)
		})
	}
	files := http.FileServer(http.FS(s.Assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Implementation note.
		if path := strings.TrimPrefix(r.URL.Path, "/"); path != "" {
			if _, err := fs.Stat(s.Assets, path); err != nil {
				r = r.Clone(r.Context())
				r.URL.Path = "/"
			}
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

// Implementation note.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		// Implementation note.
		// Implementation note.
		if delay := s.Settings.ShutdownDelaySeconds; delay > 0 {
			s.log().Info("operation,operation", "delay_seconds", delay)
			time.Sleep(time.Duration(delay) * time.Second)
		}
		s.log().Info("operation Web operation")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	s.log().Info("Web operation", "addr", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

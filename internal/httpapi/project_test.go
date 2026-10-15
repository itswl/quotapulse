package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/mailscan"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/monitor"
	"github.com/itswl/quotapulse/internal/state"
	"github.com/itswl/quotapulse/internal/store"
	"github.com/itswl/quotapulse/internal/subscription"
)

// newServerWithDB spins up a Server backed by a real SQLite database so the
// dynamic-config endpoints can be exercised end to end.
func newServerWithDB(t *testing.T, tweak func(*config.Settings)) (*Server, http.Handler, store.Store) {
	t.Helper()
	settings := &config.Settings{
		WebAPIKey: testAPIKey, AppVersion: "1.0.0",
		RequestTimeout: 5, ResponseCacheTTL: 0,
		EnableDynamicConfig: true,
	}
	if tweak != nil {
		tweak(settings)
	}
	st, err := store.Open(context.Background(), store.Options{
		DatabaseURL: "sqlite:///" + filepath.Join(t.TempDir(), "httpapi_test.db"),
	})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	resolver := config.NewResolver(settings, st, log)
	s := &Server{
		Settings: settings, Resolver: resolver, Store: st, State: state.New(),
		Monitor: &monitor.Monitor{Settings: settings, Resolver: resolver, Store: st, Log: log},
		Subs:    &subscription.Checker{Store: st, Log: log},
		Scanner: &mailscan.Scanner{Store: st, Log: log},
		Log:     log,
	}
	return s, s.Handler(), st
}

// The threshold-only shortcut must update just the threshold and leave the
// stored key untouched.
func TestUpdateThresholdEndpoint(t *testing.T) {
	_, handler, st := newServerWithDB(t, nil)
	ctx := context.Background()
	if err := st.UpsertProject(ctx, model.Project{
		Name: "alpha", Provider: "deepseek", APIKey: "sk-live-key", Threshold: 5, Enabled: true,
	}); err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}

	got := request(t, handler, "POST", "/api/config/threshold",
		`{"project_name":"alpha","new_threshold":42}`, true)
	if got.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", got.Code, got.Body.String())
	}
	saved := projectByName(t, st, "alpha")
	if saved.Threshold != 42 {
		t.Errorf("threshold = %g, want 42", saved.Threshold)
	}
	if saved.APIKey != "sk-live-key" {
		t.Errorf("the shortcut must not touch the key, got %q", saved.APIKey)
	}

	// unknown project: 404 with the name in the message
	got = request(t, handler, "POST", "/api/config/threshold",
		`{"project_name":"ghost","new_threshold":1}`, true)
	if got.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", got.Code)
	}

	// negative thresholds are rejected before anything is written
	got = request(t, handler, "POST", "/api/config/threshold",
		`{"project_name":"alpha","new_threshold":-1}`, true)
	if got.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", got.Code)
	}
}

// Resubmitting the masked key must not overwrite the stored secret; an empty
// key on update means unchanged.
func TestSaveProjectKeepsKeyWhenMasked(t *testing.T) {
	_, handler, st := newServerWithDB(t, nil)
	ctx := context.Background()
	if err := st.UpsertProject(ctx, model.Project{
		Name: "alpha", Provider: "deepseek", APIKey: "sk-live-key", Threshold: 5, Enabled: true,
	}); err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}

	// the page shows the masked value; submitting it back must keep the key
	got := request(t, handler, "POST", "/api/config/project",
		`{"name":"alpha","api_key":"sk-l***-key"}`, true)
	if got.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", got.Code, got.Body.String())
	}
	saved := projectByName(t, st, "alpha")
	if saved.APIKey != "sk-live-key" {
		t.Errorf("masked resubmit clobbered the key: %q", saved.APIKey)
	}

	// a genuinely new key still replaces the old one
	got = request(t, handler, "POST", "/api/config/project",
		`{"name":"alpha","api_key":"sk-rotated"}`, true)
	if got.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", got.Code, got.Body.String())
	}
	saved = projectByName(t, st, "alpha")
	if saved.APIKey != "sk-rotated" {
		t.Errorf("rotation did not apply: %q", saved.APIKey)
	}
}

// Deleting an env-discovered project is rejected: it is not in the database
// and would reappear on the next start.
func TestDeleteEnvDiscoveredProjectRejected(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-from-env-1234")
	_, handler, _ := newServerWithDB(t, nil)

	got := request(t, handler, "POST", "/api/config/project/delete",
		`{"name":"deepseek"}`, true)
	if got.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", got.Code, got.Body.String())
	}
	if !strings.Contains(got.Body.String(), "auto-discovered") {
		t.Errorf("the error should explain the env origin: %s", got.Body.String())
	}
}

// mark_renewed pins today as the renewal date and reports the next one.
func TestMarkRenewedReturnsNextDate(t *testing.T) {
	_, handler, st := newServerWithDB(t, func(s *config.Settings) { s.EnableSubscriptions = true })
	ctx := context.Background()
	if err := st.UpsertSubscription(ctx, model.Subscription{
		Name: "Claude Max", CycleType: model.CycleMonthly, RenewalDay: 15,
		AlertDaysBefore: 3, Enabled: true,
	}); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}

	got := request(t, handler, "POST", "/api/subscription/mark_renewed",
		`{"name":"Claude Max"}`, true)
	if got.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", got.Code, got.Body.String())
	}
	body := decode(t, got)
	if next, _ := body["next_renewal_date"].(string); next == "" {
		t.Errorf("expected next_renewal_date in response: %v", body)
	}
	var renewed *string
	list, err := st.ListSubscriptions(ctx)
	if err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}
	for _, sub := range list {
		if sub.Name == "Claude Max" {
			renewed = sub.LastRenewedDate
		}
	}
	if renewed == nil || *renewed == "" {
		t.Error("last_renewed_date must be recorded")
	}
}

// The event stream returns JSON lines with a status field per event.
func TestEventsEndpointStreams(t *testing.T) {
	s, handler := newServer(t, nil)
	s.State.SetBalance([]model.CheckResult{{
		Project: "alpha", Provider: "deepseek", Success: true, Type: "balance",
	}})

	got := request(t, handler, "GET", "/api/events", "", true)
	if got.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", got.Code)
	}
	if got.Header().Get("Content-Type") == "" {
		t.Error("content type must be set")
	}
}

func projectByName(t *testing.T, st store.Store, name string) model.Project {
	t.Helper()
	list, err := st.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	for _, p := range list {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("project %q not found", name)
	return model.Project{}
}

package selfcheck

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/app"
	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/store"
)

// newApp builds the minimal *app.App Run needs: settings from the (test)
// environment and a resolver over the Null store.
func newApp(t *testing.T) *app.App {
	t.Helper()
	t.Setenv("ENABLE_DATABASE", "false")
	settings, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	return &app.App{
		Settings: settings,
		Log:      log,
		Store:    store.Null(),
		Resolver: config.NewResolver(settings, store.Null(), log),
	}
}

// An empty deployment must report every blocking problem: no webhook, no API
// key and no projects.
func TestRunReportsEmptyDeployment(t *testing.T) {
	a := newApp(t)
	var out bytes.Buffer
	problems := Run(context.Background(), a, &out)
	if problems == 0 {
		t.Fatal("an empty deployment must report problems")
	}
	text := out.String()
	for _, want := range []string{
		"WEBHOOK_URL is not set",
		"WEB_API_KEY is not set",
		"No projects",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "Found 3 problem(s)") {
		t.Errorf("expected exactly 3 problems (webhook, api key, projects), got:\n%s", text)
	}
}

// A healthy env-discovered project with a threshold earns a check mark and no
// problem count.
func TestRunHealthyProject(t *testing.T) {
	a := newApp(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-test-1234567890")
	t.Setenv("DEEPSEEK_THRESHOLD", "10")
	settings, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	a.Settings = settings
	a.Resolver = config.NewResolver(settings, a.Store, a.Log)

	var out bytes.Buffer
	problems := Run(context.Background(), a, &out)
	text := out.String()
	if !strings.Contains(text, markOK+" deepseek [deepseek/balance] threshold 10") {
		t.Errorf("expected a healthy deepseek line, got:\n%s", text)
	}
	if !strings.Contains(text, "auto-discovered from environment variables") {
		t.Errorf("expected the env origin note, got:\n%s", text)
	}
	// webhook + api key still missing: exactly those two problems remain
	if !strings.Contains(text, "Found 2 problem(s)") {
		t.Errorf("expected 2 remaining problems, got:\n%s", text)
	}
	if problems != 2 {
		t.Errorf("Run returned %d problems, want 2", problems)
	}
}

// A zero threshold never alerts; the report must warn instead of passing
// silently.
func TestRunWarnsOnZeroThreshold(t *testing.T) {
	a := newApp(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-test-1234567890")
	settings, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	a.Settings = settings
	a.Resolver = config.NewResolver(settings, a.Store, a.Log)

	var out bytes.Buffer
	Run(context.Background(), a, &out)
	if !strings.Contains(out.String(), "threshold 0 never alerts; set DEEPSEEK_THRESHOLD") {
		t.Errorf("expected the zero-threshold warning, got:\n%s", out.String())
	}
}

// Expired MCP keys must be flagged with the bad marker.
func TestRunFlagsExpiredMCPKey(t *testing.T) {
	a := newApp(t)
	past := time.Now().Add(-time.Hour).UTC()
	a.Settings.EnableMCP = true
	a.Settings.MCPKeys = []config.MCPKey{{
		Name:    "agent",
		Key:     "mcp-key-0123456789abcdef",
		Scopes:  map[string]bool{config.ScopeBalance: true},
		Expires: &past,
	}}

	var out bytes.Buffer
	problems := Run(context.Background(), a, &out)
	if !strings.Contains(out.String(), markBad+" MCP key agent has expired") {
		t.Errorf("expected the expired-key line, got:\n%s", out.String())
	}
	if problems == 0 {
		t.Error("an expired MCP key must count as a problem")
	}
}

// Subscriptions without the database are a configuration mistake worth a
// problem, not just a warning-level note.
func TestRunFlagsSubscriptionsWithoutDatabase(t *testing.T) {
	a := newApp(t)
	a.Settings.EnableSubscriptions = true
	var out bytes.Buffer
	problems := Run(context.Background(), a, &out)
	if !strings.Contains(out.String(), "Subscription reminders are on") {
		t.Errorf("expected the subscription warning, got:\n%s", out.String())
	}
	if problems == 0 {
		t.Error("subscriptions without a database must count as a problem")
	}
}

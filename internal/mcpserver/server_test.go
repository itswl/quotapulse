package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/state"
)

func TestStateKind(t *testing.T) {
	for _, tc := range []struct {
		uri, want string
	}{
		{"quotapulse://state/balance", "balance"},
		{"quotapulse://state/subscriptions", "subscriptions"},
		{"quotapulse://state/email", "email"},
	} {
		got, err := stateKind(tc.uri)
		if err != nil || got != tc.want {
			t.Errorf("stateKind(%q) = %q, %v; want %q", tc.uri, got, err, tc.want)
		}
	}
	for _, uri := range []string{"https://example.test/state/balance", "quotapulse://other/balance", "quotapulse://state/"} {
		if _, err := stateKind(uri); err == nil {
			t.Errorf("stateKind(%q) accepted an invalid URI", uri)
		}
	}
}

func TestBounded(t *testing.T) {
	if got := bounded(0, 30, 1, 365); got != 30 {
		t.Errorf("zero should use fallback, got %d", got)
	}
	if got := bounded(-1, 30, 1, 365); got != 1 {
		t.Errorf("low value should clamp, got %d", got)
	}
	if got := bounded(500, 30, 1, 365); got != 365 {
		t.Errorf("high value should clamp, got %d", got)
	}
}

func TestHealthSnapshotReportsFreshnessAndVersion(t *testing.T) {
	runtime := state.New()
	runtime.SetBalance([]model.CheckResult{{Project: "demo"}})
	old := time.Now().Add(-4 * time.Minute).Format(time.RFC3339Nano)
	stateValue := runtime.Balance()
	stateValue.LastUpdate = &old
	settings := &config.Settings{
		AppVersion:                    "v-test",
		BalanceRefreshIntervalSeconds: intPtr(60),
	}

	got := healthSnapshot(settings, runtime, stateValue, runtime.Jobs())
	if got["version"] != "v-test" || got["is_stale"] != true || got["status"] != "degraded" {
		t.Fatalf("health snapshot missing freshness details: %#v", got)
	}
}

func TestCapabilitySnapshotDoesNotExposeSecrets(t *testing.T) {
	settings := &config.Settings{AppVersion: "v-test", EnableMCP: true}
	got := capabilitySnapshot(nil, settings, nil)
	if got["version"] != "v-test" {
		t.Fatalf("capability snapshot missing version: %#v", got)
	}
	features, ok := got["features"].(map[string]bool)
	if !ok || !features["mcp"] || features["email_scan"] {
		t.Fatalf("unexpected capability flags: %#v", got)
	}
	if _, ok := got["api_key"]; ok {
		t.Fatal("capability snapshot must not expose credentials")
	}
}

func intPtr(value int) *int { return &value }

func TestFilterBalanceByProjectAndProvider(t *testing.T) {
	runtime := state.New()
	runtime.SetBalance([]model.CheckResult{
		{Project: "deepseek-prod", Provider: "deepseek"},
		{Project: "volc-1", Provider: "volc"},
		{Project: "volc-2", Provider: "volc"},
	})
	balance := runtime.Balance()

	if got := len(filterBalance(balance, "", "").Projects); got != 3 {
		t.Fatalf("empty filters must keep every project, got %d", got)
	}
	if got := len(filterBalance(balance, "volc", "").Projects); got != 2 {
		t.Fatalf("project filter should keep both volc projects, got %d", got)
	}
	if got := len(filterBalance(balance, "DEEPSEEK-PROD", "").Projects); got != 1 {
		t.Fatalf("project filter must match case-insensitively, got %d", got)
	}
	if got := len(filterBalance(balance, "", "deepseek").Projects); got != 1 {
		t.Fatalf("provider filter should keep one project, got %d", got)
	}
	if got := len(filterBalance(balance, " ", "").Projects); got != 3 {
		t.Fatalf("whitespace-only filters must keep every project, got %d", got)
	}
	if got := len(filterBalance(balance, "nope", "").Projects); got != 0 {
		t.Fatalf("an unknown filter should return an empty list, got %d", got)
	}
}

func TestProvidersCatalogIsAvailable(t *testing.T) {
	list := provider.All()
	if len(list) == 0 {
		t.Fatal("provider registry should not be empty")
	}
	body, err := json.Marshal(list)
	if err != nil || !strings.Contains(string(body), "\"label\"") {
		t.Fatalf("provider catalog should serialize the UI contract (label/value/default_type): %s (%v)", body, err)
	}
}

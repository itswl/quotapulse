package mcpserver

import (
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/model"
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

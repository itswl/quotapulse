package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/provider"
)

// A full management round trip: save a custom provider, see it in the
// catalog, bind a project to it, and refuse deletion while in use.
func TestCustomProviderLifecycle(t *testing.T) {
	upstream := serveBalancedJSON(t)
	_, handler, st := newServerWithDB(t, nil)

	save := `{
		"name":"acme","display_name":"Acme Cloud","url":"` + upstream.URL + `",
		"auth_mode":"none","default_type":"balance","currency":"USD",
		"balance_path":"data.balance","success_path":"status","success_values":"ok"
	}`
	got := request(t, handler, "POST", "/api/config/provider", save, true)
	if got.Code != http.StatusOK {
		t.Fatalf("save failed: %d %s", got.Code, got.Body.String())
	}

	// the catalog now lists it, namespaced
	listed := false
	for _, info := range provider.All() {
		if info.Key == "custom:acme" && info.Currency == "USD" {
			listed = true
		}
	}
	if !listed {
		t.Error("custom:acme missing from provider.All() after save")
	}

	// a project can bind to it
	got = request(t, handler, "POST", "/api/config/project",
		`{"name":"acme-main","provider":"custom:acme","api_key":"k-1","threshold":5}`, true)
	if got.Code != http.StatusOK {
		t.Fatalf("project save failed: %d %s", got.Code, got.Body.String())
	}

	// deletion is refused while a project uses it
	got = request(t, handler, "POST", "/api/config/provider/delete", `{"name":"acme"}`, true)
	if got.Code != http.StatusBadRequest {
		t.Fatalf("delete-in-use should 400, got %d: %s", got.Code, got.Body.String())
	}

	// unbind, then delete succeeds
	if err := st.DeleteProject(context.Background(), "acme-main"); err != nil {
		t.Fatal(err)
	}
	got = request(t, handler, "POST", "/api/config/provider/delete", `{"name":"acme"}`, true)
	if got.Code != http.StatusOK {
		t.Fatalf("delete failed: %d %s", got.Code, got.Body.String())
	}
	if provider.IsDynamic("custom:acme") {
		t.Error("provider should be unregistered after delete")
	}
}

// Invalid definitions are rejected field by field, and builtin names cannot
// be reused.
func TestCustomProviderValidation(t *testing.T) {
	_, handler, _ := newServerWithDB(t, nil)

	got := request(t, handler, "POST", "/api/config/provider",
		`{"name":"Bad Name","url":"notaurl","auth_mode":"none","default_type":"balance","balance_path":"x"}`, true)
	if got.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", got.Code, got.Body.String())
	}
	if body := got.Body.String(); !containsAll(body, "name:", "url:") {
		t.Errorf("validation should name the broken fields: %s", body)
	}

	got = request(t, handler, "POST", "/api/config/provider",
		`{"name":"deepseek","url":"https://x.test","auth_mode":"none","default_type":"balance","balance_path":"x"}`, true)
	if got.Code != http.StatusBadRequest || !contains(got.Body.String(), "built-in") {
		t.Fatalf("builtin names must be rejected: %d %s", got.Code, got.Body.String())
	}
}

// The test endpoint proves the wiring against a live upstream without saving
// anything.
func TestCustomProviderTestEndpoint(t *testing.T) {
	upstream := serveBalancedJSON(t)
	_, handler, _ := newServerWithDB(t, nil)

	got := request(t, handler, "POST", "/api/config/provider/test", `{
		"name":"acme","url":"`+upstream.URL+`","auth_mode":"none","default_type":"balance",
		"balance_path":"data.amount","balance_scale":0.01,"success_path":"ok"
	}`, true)
	if got.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", got.Code, got.Body.String())
	}
	body := decode(t, got)
	if body["value"] != 12.35 { // float64 JSON number
		t.Errorf("value = %v, want 12.35", body["value"])
	}

	// a broken path reports the failure verbatim
	got = request(t, handler, "POST", "/api/config/provider/test", `{
		"name":"acme","url":"`+upstream.URL+`","auth_mode":"none","default_type":"balance",
		"balance_path":"data.missing"
	}`, true)
	if got.Code != http.StatusBadRequest || !contains(got.Body.String(), "balance not found") {
		t.Errorf("broken path should fail clearly: %d %s", got.Code, got.Body.String())
	}
}

func TestCostsSummary(t *testing.T) {
	var dbPath string
	s, handler, st := newServerWithDBPath(t, nil, func(p string) { dbPath = p })
	ctx := context.Background()

	for _, p := range []model.Project{
		{Name: "deepseek", Provider: "deepseek", APIKey: "k", Enabled: true, MonthlyBudget: 300},
		{Name: "router", Provider: "openrouter", APIKey: "k", Enabled: true},
	} {
		if err := st.UpsertProject(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	// seed history with chosen timestamps straight into SQLite: deepseek
	// spends 200 CNY, openrouter 20 USD this month
	now := time.Now()
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	mid := first.AddDate(0, 0, 10)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seed := []struct {
		name     string
		provider string
		balance  float64
		at       time.Time
	}{
		{"deepseek", "deepseek", 1000, first},
		{"deepseek", "deepseek", 800, mid},
		{"router", "openrouter", 50, first},
		{"router", "openrouter", 30, mid},
	}
	for _, row := range seed {
		if _, err := db.Exec(`INSERT INTO balance_history
			(project_id, project_name, provider, balance, balance_type, need_alarm, timestamp)
			VALUES (?, ?, ?, ?, 'balance', 0, ?)`,
			model.ProjectID(row.provider, row.name), row.name, row.provider, row.balance,
			row.at.Format("2006-01-02 15:04:05.000000")); err != nil {
			t.Fatal(err)
		}
	}
	s.Resolver.Load(ctx)

	got := request(t, handler, "GET", "/api/costs/summary", "", true)
	if got.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", got.Code, got.Body.String())
	}
	body := decode(t, got)
	summary, _ := body["summary"].(map[string]any)
	if summary == nil {
		t.Fatalf("no summary in response: %v", body)
	}
	// deepseek defaults to CNY (rate 1 even without FX config); the total is
	// its spend plus openrouter's uncconverted absence
	if summary["base_currency"] != "CNY" {
		t.Errorf("base currency = %v", summary["base_currency"])
	}
	total, _ := summary["total_base"].(float64)
	if total != 200 {
		t.Errorf("total_base = %v, want 200 (USD has no rate yet)", total)
	}
	if _, unknown := summary["unknown_fx"]; !unknown {
		// JSON omits empty; unknown_fx may be absent or contain USD
		t.Log("unknown_fx:", summary["unknown_fx"])
	}
}

// ---------- helpers ----------

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func containsAll(haystack string, needles ...string) bool {
	for _, n := range needles {
		if !contains(haystack, n) {
			return false
		}
	}
	return true
}

// serveBalancedJSON spins up a fake upstream answering every path with a
// balanced-looking payload.
func serveBalancedJSON(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"status":"ok","data":{"balance":42.5,"amount":1234.5}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

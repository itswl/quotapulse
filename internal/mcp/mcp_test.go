package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/itswl/quotapulse/internal/api"
	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/engine"
	"github.com/itswl/quotapulse/internal/mcp"
	"github.com/itswl/quotapulse/internal/recur"
	"github.com/itswl/quotapulse/internal/secret"
	"github.com/itswl/quotapulse/internal/store"
)

var ctx = context.Background()

// bearer adds a token to every request, as an agent's MCP client would.
type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}

type world struct {
	t      *testing.T
	engine *engine.Engine
	url    string
	now    time.Time
}

func newWorld(t *testing.T) *world {
	t.Helper()
	keys := secret.FromPassphrase("test")
	st, err := store.Open(filepath.Join(t.TempDir(), "quotapulse.db"), keys)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	w := &world{t: t, now: time.Date(2026, 11, 16, 2, 0, 0, 0, time.UTC)}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	w.engine = engine.New(engine.Options{Store: st, Log: quiet, Now: func() time.Time { return w.now }})
	settings, _ := w.engine.Settings(ctx)
	settings.Timezone, settings.BaseCurrency = "Asia/Shanghai", "CNY"
	settings.FX = map[string]float64{"USD": 7}
	if _, err := w.engine.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer((&api.Server{
		Engine: w.engine, Keys: keys, Password: "pw", Version: "test", Log: quiet, MCP: mcp.Handler(w.engine, quiet),
	}).Handler())
	t.Cleanup(server.Close)
	w.url = server.URL + "/mcp"
	return w
}

// connect opens an MCP session with a token carrying the given scopes.
func (w *world) connect(scopes ...string) *sdk.ClientSession {
	w.t.Helper()
	token, err := w.engine.CreateToken(ctx, engine.TokenInput{Name: "agent-" + strings.Join(scopes, "-"), Scopes: scopes})
	if err != nil {
		w.t.Fatal(err)
	}
	return w.connectWith(token.Secret)
}

func (w *world) connectWith(token string) *sdk.ClientSession {
	w.t.Helper()
	return w.connectTo(w.url, token)
}

// connectTo opens an MCP session at an endpoint, which may carry a query —
// as ?lang=zh does — with a token.
func (w *world) connectTo(endpoint, token string) *sdk.ClientSession {
	w.t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "test-agent", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint: endpoint, HTTPClient: &http.Client{Transport: bearer{token}}, DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { session.Close() })
	return session
}

func toolNames(t *testing.T, session *sdk.ClientSession) []string {
	t.Helper()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
		if tool.Description == "" || tool.InputSchema == nil {
			t.Errorf("%s has no description or schema", tool.Name)
		}
	}
	sort.Strings(names)
	return names
}

// call runs a tool and decodes its structured result.
func call[T any](t *testing.T, session *sdk.ClientSession, name string, args map[string]any) (T, *sdk.CallToolResult) {
	t.Helper()
	var out T
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if result.IsError {
		return out, result
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %v in %s", name, err, raw)
	}
	return out, result
}

func errorText(result *sdk.CallToolResult) string {
	var parts []string
	for _, content := range result.Content {
		if text, ok := content.(*sdk.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, " ")
}

// refused reports whether a call did not go through, whether the tool is
// not offered to this token or it answered with an error.
func refused(t *testing.T, session *sdk.ClientSession, name string, args map[string]any) bool {
	t.Helper()
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	return err != nil || result.IsError
}

func ptr[T any](v T) *T { return &v }

func TestToolsFollowTheTokensScopes(t *testing.T) {
	w := newWorld(t)
	acting := w.connect(core.ScopeAct)
	read := []string{"attention", "get_meter", "get_settings", "get_spend", "list_accounts", "list_activity", "list_channel_types",
		"list_dates", "list_deliveries", "list_meters", "list_providers", "preview_date"}
	act := []string{"acknowledge", "add_date", "complete_date", "delete_date", "mute_meter", "reopen_date", "sample_now",
		"snooze_date", "unacknowledge", "unmute_meter", "unsnooze_date", "update_date"}
	ingest := []string{"record_reading", "record_readings"}
	admin := []string{"add_account", "add_channel", "backup_now", "create_token", "delete_account", "delete_channel",
		"delete_meter", "delete_provider", "export_bundle", "get_system", "import_bundle", "list_channels", "list_tokens",
		"retry_delivery", "revoke_token", "save_provider", "send_digest", "test_account", "test_channel", "update_account",
		"update_channel", "update_meter", "update_settings"}

	if got := toolNames(t, w.connect(core.ScopeRead)); strings.Join(got, ",") != strings.Join(read, ",") {
		t.Errorf("read token sees %v", got)
	}
	if got := toolNames(t, acting); strings.Join(got, ",") != strings.Join(act, ",") {
		t.Errorf("act token sees %v", got)
	}
	if got := toolNames(t, w.connect(core.ScopeIngest)); strings.Join(got, ",") != strings.Join(ingest, ",") {
		t.Errorf("ingest token sees %v", got)
	}
	// An admin token is offered everything: what the dashboard can do.
	all := append(append(append(append([]string{}, read...), act...), ingest...), admin...)
	sort.Strings(all)
	if got := toolNames(t, w.connect(core.ScopeAdmin)); strings.Join(got, ",") != strings.Join(all, ",") {
		t.Errorf("admin token sees %v", got)
	}

	// A tool that is not offered cannot be called by guessing its name.
	if !refused(t, w.connect(core.ScopeRead, core.ScopeIngest), "add_date", map[string]any{"title": "x", "freq": "once", "date": "2030-01-01"}) {
		t.Fatal("a read token added a date")
	}
	if !refused(t, acting, "list_tokens", map[string]any{}) {
		t.Fatal("an act token listed the tokens")
	}
	if all, _ := w.engine.Reminders(ctx, "en"); len(all) != 0 {
		t.Fatal("a reminder was created without the scope")
	}
}

// TestToolsSayWhatTheyAre checks the annotations a client relies on to
// know what is safe to call on its own initiative.
func TestToolsSayWhatTheyAre(t *testing.T) {
	w := newWorld(t)
	listed, err := w.connect(core.ScopeAdmin).ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		a := tool.Annotations
		if a == nil || !a.ReadOnlyHint && a.DestructiveHint == nil {
			t.Errorf("%s says neither read-only nor whether it is destructive", tool.Name)
		}
		switch tool.Name {
		case "delete_date", "delete_meter", "delete_account", "delete_channel", "delete_provider", "revoke_token":
			if a.DestructiveHint == nil || !*a.DestructiveHint {
				t.Errorf("%s removes something for good and must say so", tool.Name)
			}
		default:
			if a.DestructiveHint != nil && *a.DestructiveHint {
				t.Errorf("%s is marked destructive", tool.Name)
			}
		}
	}
}

func TestNoTokenNoMCP(t *testing.T) {
	w := newWorld(t)
	for _, token := range []string{"", "qp_made-up"} {
		req, _ := http.NewRequest("POST", w.url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("token %q: status %d", token, resp.StatusCode)
		}
	}
}

type meterOut struct {
	ID, Name, Kind, Unit, Status, Headroom, Summary string
	Value                                           *float64
	RunwayDays                                      *float64 `json:"runway_days"`
	RemainingPct                                    *float64 `json:"remaining_pct"`
}

func TestAnAgentChecksHeadroomBeforeAJob(t *testing.T) {
	w := newWorld(t)
	reset := w.now.Add(90 * time.Minute)
	_, err := w.engine.Ingest(ctx, []engine.Push{
		{Meter: "deepseek", Name: "DeepSeek", Kind: core.Balance, Unit: "CNY", Value: ptr(547.67)},
		{Meter: "plan", Name: "GLM window", Kind: core.Quota, Unit: "%", Value: ptr(6.0), Limit: ptr(100.0), ResetsAt: &reset},
		{Meter: "gone", Name: "Empty wallet", Kind: core.Balance, Unit: "USD", Value: ptr(0.0)},
		{Meter: "claude", Name: "Claude Code", Kind: core.Counter, Unit: "USD", Value: ptr(12.5)},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := w.connect(core.ScopeRead)

	listed, _ := call[struct {
		Timezone string
		Meters   []meterOut
	}](t, session, "list_meters", nil)
	if listed.Timezone != "Asia/Shanghai" || len(listed.Meters) != 4 {
		t.Fatalf("listed = %+v", listed)
	}
	byName := map[string]meterOut{}
	for _, m := range listed.Meters {
		byName[m.Name] = m
	}
	if m := byName["DeepSeek"]; m.Headroom != "ok" || m.Summary != "547.67 CNY left." {
		t.Errorf("healthy balance = %+v", m)
	}
	if m := byName["GLM window"]; m.Headroom != "low" || *m.RemainingPct != 6 || m.Summary != "6% left, resets in 1 h 30 min." {
		t.Errorf("low quota = %+v", m)
	}
	if m := byName["Empty wallet"]; m.Headroom != "exhausted" || m.Status != "critical" {
		t.Errorf("empty balance = %+v", m)
	}
	if m := byName["Claude Code"]; m.Kind != "counter" || m.Summary != "12.50 USD so far." {
		t.Errorf("counter = %+v", m)
	}

	filtered, _ := call[struct{ Meters []meterOut }](t, session, "list_meters", map[string]any{"query": "glm", "kind": "quota"})
	if len(filtered.Meters) != 1 || filtered.Meters[0].Name != "GLM window" {
		t.Errorf("filtered = %+v", filtered)
	}
	if _, result := call[struct{}](t, session, "list_meters", map[string]any{"kind": "gauge"}); !result.IsError || !strings.Contains(errorText(result), "kind must be") {
		t.Errorf("a bad kind: %+v", result)
	}

	// One meter, by name or by id.
	detail, _ := call[struct {
		Meter     meterOut
		Incidents []struct{ Headline, Severity string }
		Daily     []struct{ Date string }
	}](t, session, "get_meter", map[string]any{"meter": "empty wallet"})
	if detail.Meter.Name != "Empty wallet" || len(detail.Incidents) != 1 || detail.Incidents[0].Headline != "Balance is empty · Empty wallet" {
		t.Fatalf("detail = %+v", detail)
	}
	if len(detail.Daily) == 0 {
		t.Error("no daily spend")
	}
	byID, _ := call[struct{ Meter meterOut }](t, session, "get_meter", map[string]any{"meter": detail.Meter.ID})
	if byID.Meter.Name != "Empty wallet" {
		t.Errorf("by id = %+v", byID)
	}
	_, result := call[struct{}](t, session, "get_meter", map[string]any{"meter": "nothing like this"})
	if !result.IsError || !strings.Contains(errorText(result), `no meter is called "nothing like this"`) {
		t.Errorf("unknown meter: %s", errorText(result))
	}
}

func TestAttention(t *testing.T) {
	w := newWorld(t)
	session := w.connect(core.ScopeRead)

	type attention struct {
		Status, Summary string
		Open            []struct{ ID, Kind, Severity, Headline string }
		Upcoming        []struct {
			Title, Next, Kind, Lunar, Rule string
			Days                           int
		}
	}
	calm, _ := call[attention](t, session, "attention", nil)
	if calm.Status != "ok" || calm.Summary != "Nothing needs attention." || calm.Open == nil || calm.Upcoming == nil {
		t.Fatalf("calm = %+v", calm)
	}

	_, err := w.engine.Ingest(ctx, []engine.Push{{Meter: "gone", Name: "Wallet", Kind: core.Balance, Unit: "USD", Value: ptr(0.0)}})
	if err != nil {
		t.Fatal(err)
	}
	lunar := recur.LunarOf(recur.NewDate(2026, 11, 17))
	_, err = w.engine.CreateReminder(ctx, engine.ReminderInput{
		Title: ptr("Mom"), Kind: ptr(core.ReminderBirthday), LeadDays: &[]int{1, 0},
		Rule: &recur.Rule{Freq: recur.Yearly, Calendar: recur.LunarCal, Month: lunar.Month, Day: lunar.Day},
	}, "en")
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.engine.CreateReminder(ctx, engine.ReminderInput{Title: ptr("Far"), Rule: &recur.Rule{Freq: recur.Yearly, Month: 6, Day: 1}}, "en")
	if err != nil {
		t.Fatal(err)
	}

	busy, _ := call[attention](t, session, "attention", nil)
	if busy.Status != "critical" || len(busy.Open) != 1 || busy.Open[0].Headline != "Balance is empty · Wallet" {
		t.Fatalf("busy = %+v", busy)
	}
	// A date that is near is not a problem: it is listed as upcoming, once.
	if len(busy.Upcoming) != 1 || busy.Upcoming[0].Title != "Mom" || busy.Upcoming[0].Days != 1 || busy.Upcoming[0].Next != "2026-11-17" {
		t.Fatalf("upcoming = %+v", busy.Upcoming)
	}
	if busy.Upcoming[0].Lunar != "十月初九" || !strings.Contains(busy.Upcoming[0].Rule, "lunar month 10 day 9") {
		t.Errorf("lunar = %+v", busy.Upcoming[0])
	}
	if busy.Summary != "1 open: Balance is empty · Wallet." {
		t.Errorf("summary = %q", busy.Summary)
	}
	wide, _ := call[attention](t, session, "attention", map[string]any{"days": 366})
	if len(wide.Upcoming) != 2 {
		t.Errorf("a year ahead = %+v", wide.Upcoming)
	}
}

func TestAnAgentActs(t *testing.T) {
	w := newWorld(t)
	session := w.connect(core.ScopeRead, core.ScopeAct, core.ScopeIngest)

	type dated struct {
		Date struct {
			ID, Title, Next, State, Rule, Currency string
			Days                                   int
			Amount                                 float64
		}
	}
	added, result := call[dated](t, session, "add_date", map[string]any{
		"title": "Domain renewal", "kind": "renewal", "freq": "yearly", "month": 12, "day": 1,
		"lead_days": []int{7, 0}, "amount": 68.5,
	})
	if result.IsError {
		t.Fatalf("add_date: %s", errorText(result))
	}
	if added.Date.Next != "2026-12-01" || added.Date.Days != 15 || added.Date.Currency != "CNY" || added.Date.Rule != "every year on month 12 day 1" {
		t.Fatalf("added = %+v", added)
	}
	if _, result := call[dated](t, session, "add_date", map[string]any{"title": "Broken", "freq": "monthly", "day": 40}); !result.IsError || !strings.Contains(errorText(result), "day must be between 1 and 31") {
		t.Errorf("an impossible rule: %s", errorText(result))
	}

	done, result := call[dated](t, session, "complete_date", map[string]any{"date": "domain renewal"})
	if result.IsError || done.Date.Next != "2027-12-01" {
		t.Fatalf("complete_date: %+v %s", done, errorText(result))
	}

	recorded, result := call[struct{ Meter meterOut }](t, session, "record_reading", map[string]any{"meter": "wallet", "kind": "balance", "unit": "USD", "value": 0, "name": "Wallet"})
	if result.IsError || recorded.Meter.Headroom != "exhausted" {
		t.Fatalf("record_reading: %+v %s", recorded, errorText(result))
	}
	if _, result := call[struct{}](t, session, "record_reading", map[string]any{"meter": "new", "value": 1}); !result.IsError || !strings.Contains(errorText(result), "kind is required") {
		t.Errorf("a new meter without a kind: %s", errorText(result))
	}

	activity, _ := call[struct {
		Incidents []struct {
			ID, State, Headline string
			Acknowledged        bool
		}
	}](t, session, "list_activity", map[string]any{"state": "open"})
	if len(activity.Incidents) != 1 || activity.Incidents[0].Acknowledged {
		t.Fatalf("activity = %+v", activity)
	}
	acked, result := call[struct{ Acknowledged bool }](t, session, "acknowledge", map[string]any{"incident_id": activity.Incidents[0].ID})
	if result.IsError || !acked.Acknowledged {
		t.Fatalf("acknowledge: %s", errorText(result))
	}
	activity, _ = call[struct {
		Incidents []struct {
			ID, State, Headline string
			Acknowledged        bool
		}
	}](t, session, "list_activity", nil)
	if !activity.Incidents[0].Acknowledged {
		t.Error("the acknowledgement does not show")
	}
	if _, result := call[struct{}](t, session, "acknowledge", map[string]any{"incident_id": "inc_missing"}); !result.IsError {
		t.Error("acknowledging nothing succeeded")
	}

	if out, result := call[struct{ Requested string }](t, session, "sample_now", nil); result.IsError || out.Requested != "every account" {
		t.Errorf("sample_now: %+v %s", out, errorText(result))
	}
	if _, result := call[struct{}](t, session, "sample_now", map[string]any{"account": "nope"}); !result.IsError {
		t.Error("sampling an unknown account succeeded")
	}
}

func TestSpendTool(t *testing.T) {
	w := newWorld(t)
	_, err := w.engine.CreateReminder(ctx, engine.ReminderInput{
		Title: ptr("VPS"), Kind: ptr(core.ReminderRenewal), Rule: &recur.Rule{Freq: recur.Monthly, Day: 20},
		Amount: ptr(10.0), Currency: ptr("USD"),
	}, "en")
	if err != nil {
		t.Fatal(err)
	}
	session := w.connect(core.ScopeRead)
	spend, result := call[struct {
		Month, BaseCurrency string  `json:"-"`
		M                   string  `json:"month"`
		Base                string  `json:"base_currency"`
		Renewals            float64 `json:"renewals_total"`
		Rows                []struct {
			Title, Date string
			Base        float64 `json:"amount_in_base"`
		} `json:"renewals"`
		Unconverted []string
	}](t, session, "get_spend", nil)
	if result.IsError {
		t.Fatalf("get_spend: %s", errorText(result))
	}
	if spend.M != "2026-11" || spend.Base != "CNY" || spend.Renewals != 70 || len(spend.Rows) != 1 || spend.Rows[0].Date != "2026-11-20" || spend.Rows[0].Base != 70 {
		t.Fatalf("spend = %+v", spend)
	}
	if _, result := call[struct{}](t, session, "get_spend", map[string]any{"month": "next"}); !result.IsError {
		t.Error("a bad month succeeded")
	}
}

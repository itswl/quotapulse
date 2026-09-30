package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/state"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
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

func intPtr(value int) *int { return &value }

// testDeps is a server's dependencies without a database or resolver, for a caller with
// the given scopes.
func testDeps(scopes ...string) *deps {
	granted := map[string]bool{}
	for _, scope := range scopes {
		granted[scope] = true
	}
	return &deps{
		settings: &config.Settings{AppVersion: "v-test", EnableMCP: true, BalanceRefreshIntervalSeconds: intPtr(60)},
		runtime:  state.New(),
		log:      slog.New(slog.DiscardHandler),
		caller:   config.MCPCaller{Name: "agent", Scopes: granted},
		now:      time.Now,
	}
}

// connect serves d over an in-memory transport and returns a client session on it.
func connect(t *testing.T, d *deps) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	if _, err := newServer(d).Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func call(t *testing.T, session *sdkmcp.ClientSession, tool string, args map[string]any) *sdkmcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return res
}

// structured decodes a successful result's structured content into out.
func structured(t *testing.T, res *sdkmcp.CallToolResult, out any) {
	t.Helper()
	if res.IsError || res.StructuredContent == nil {
		t.Fatalf("expected a structured result, got %+v", res.Content)
	}
	body, err := json.Marshal(res.StructuredContent)
	if err == nil {
		err = json.Unmarshal(body, out)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func text(res *sdkmcp.CallToolResult, i int) string {
	if i >= len(res.Content) {
		return ""
	}
	if content, ok := res.Content[i].(*sdkmcp.TextContent); ok {
		return content.Text
	}
	return ""
}

func toolNames(t *testing.T, session *sdkmcp.ClientSession) []string {
	t.Helper()
	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestToolsFollowTheKeysScopes(t *testing.T) {
	names := toolNames(t, connect(t, testDeps(config.ScopeBalance)))
	for _, want := range []string{"health", "capabilities", "providers", "job_status", "balance_status", "balance_history", "balance_trend"} {
		if !slices.Contains(names, want) {
			t.Errorf("a balance key should see %s; got %v", want, names)
		}
	}
	for _, hidden := range []string{"recent_alerts", "events", "subscription_status", "email_scan_status", "project_config", "mailbox_config", "subscription_config"} {
		if slices.Contains(names, hidden) {
			t.Errorf("a balance key must not see %s", hidden)
		}
	}

	names = toolNames(t, connect(t, testDeps(config.ScopeConfig, config.ScopeSubscriptions)))
	if !slices.Contains(names, "subscription_config") || slices.Contains(names, "project_config") || slices.Contains(names, "mailbox_config") {
		t.Errorf("config only opens the configuration views of the key's other scopes; got %v", names)
	}

	if got := len(toolNames(t, connect(t, testDeps(config.AllScopes...)))); got != 17 {
		t.Errorf("a key with every scope should see all 17 tools, got %d", got)
	}
}

func TestToolsAreAnnotatedAndTyped(t *testing.T) {
	session := connect(t, testDeps(config.AllScopes...))
	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if list.CacheScope != "private" {
		t.Errorf("tool lists depend on the key and must be private, got %q", list.CacheScope)
	}
	for _, tool := range list.Tools {
		a := tool.Annotations
		if a == nil || !a.ReadOnlyHint || !a.IdempotentHint || a.DestructiveHint == nil || *a.DestructiveHint ||
			a.OpenWorldHint == nil || *a.OpenWorldHint || a.Title == "" {
			t.Errorf("%s: annotations should mark it read-only, idempotent and closed-world: %+v", tool.Name, a)
		}
		if tool.OutputSchema == nil || tool.Title == "" {
			t.Errorf("%s: needs an output schema and a title", tool.Name)
		}
	}
	caps := session.InitializeResult().Capabilities
	if caps.Tools == nil || caps.Tools.ListChanged || caps.Resources == nil || caps.Resources.ListChanged {
		t.Errorf("a stateless server must not promise list_changed notifications: %+v %+v", caps.Tools, caps.Resources)
	}
}

func TestBalanceStatusIsStructured(t *testing.T) {
	d := testDeps(config.ScopeBalance)
	threshold := 10.0
	d.runtime.SetBalance([]model.CheckResult{
		{Project: "deepseek-prod", Provider: "deepseek", Type: "balance", Success: true, Credits: model.Ptr(42.0), Threshold: &threshold},
		{Project: "volc-1", Provider: "volc", Type: "balance", Success: true, Credits: model.Ptr(3.0), Threshold: &threshold, NeedAlarm: true},
		{Project: "volc-2", Provider: "volc", Type: "balance", Error: model.Ptr("HTTP 401: invalid api key")},
	})
	session := connect(t, d)

	res := call(t, session, "balance_status", nil)
	var out BalanceStatusOutput
	structured(t, res, &out)
	if out.Counts != (BalanceCounts{Total: 3, Healthy: 1, BelowThreshold: 1, CheckFailed: 1}) {
		t.Errorf("counts = %+v", out.Counts)
	}
	if !strings.HasPrefix(text(res, 0), "3 projects:") || !json.Valid([]byte(text(res, 1))) {
		t.Errorf("text content should be a summary and the JSON: %q / %q", text(res, 0), text(res, 1))
	}
	if out.Meta.Source != "live_state" || out.Meta.DataUpdatedAt == nil || out.Meta.AsOf == "" || out.Meta.Timezone == "" {
		t.Errorf("meta should say where the data comes from and how fresh it is: %+v", out.Meta)
	}
	failed := out.Projects[2]
	if failed.ProjectID != model.ProjectID("volc", "volc-2") || failed.Status != "check_failed" || failed.Check.Error == nil {
		t.Fatalf("failed project = %+v", failed)
	}
	if e := failed.Check.Error; e.Category != "auth" || e.Retryable || e.Provider != "volc" {
		t.Errorf("an invalid key is an auth error that retrying won't fix: %+v", e)
	}
	if out.Projects[0].Unit == "" || out.Projects[0].RunwayNote == "" {
		t.Errorf("a balance should carry its unit, and a missing runway its reason: %+v", out.Projects[0])
	}

	structured(t, call(t, session, "balance_status", map[string]any{"provider": "volc"}), &out)
	if out.Counts.Total != 2 {
		t.Errorf("the provider filter should keep both volc projects, got %d", out.Counts.Total)
	}
	if res := call(t, session, "balance_status", map[string]any{"provider": "not-a-provider"}); !res.IsError {
		t.Error("an unknown provider should be rejected by the input schema's enum")
	}
}

func TestHistoryToolsExplainAMissingDatabase(t *testing.T) {
	session := connect(t, testDeps(config.AllScopes...))
	for _, tool := range []string{"balance_history", "recent_alerts", "recent_email_alerts", "events", "alert_stats"} {
		res := call(t, session, tool, nil)
		if !res.IsError || res.StructuredContent != nil {
			t.Errorf("%s: expected an error result without structured content", tool)
			continue
		}
		var failure ErrorOutput
		if err := json.Unmarshal([]byte(text(res, 1)), &failure); err != nil || failure.Error.Category != "unavailable" ||
			!strings.Contains(failure.Error.Message, "ENABLE_DATABASE") {
			t.Errorf("%s: the error should say what to enable: %q (%v)", tool, text(res, 1), err)
		}
	}
	res := call(t, session, "balance_trend", map[string]any{"project_id": ""})
	if !res.IsError || !strings.Contains(text(res, 1), `"invalid_argument"`) {
		t.Errorf("an empty project_id is an invalid argument: %q", text(res, 1))
	}
}

func TestJobStatusExplainsInactiveJobs(t *testing.T) {
	d := testDeps()
	next := time.Now().Add(time.Hour)
	d.runtime.RegisterJob("email_scan", "Scan mailboxes", "10:00", true, next)
	d.runtime.RegisterJob("weekly_report", "Weekly summary", "Mon 09:00", true, next)
	d.runtime.RegisterJob("alert_check", "Alert check", "09:00", true, next)
	d.runtime.RegisterJob("database_backup", "Backup", "", false, time.Time{})
	d.runtime.RecordJobRun("alert_check", false, time.Now(), time.Second, errors.New("webhook down"), nil, next)

	var out JobStatusOutput
	structured(t, call(t, connect(t, d), "job_status", nil), &out)
	want := map[string]string{
		"email_scan": "configured_but_inactive", "weekly_report": "scheduled_not_yet_run",
		"alert_check": "failing", "database_backup": "disabled",
	}
	for _, job := range out.Jobs {
		if job.Status != want[job.Name] || job.Reason == "" {
			t.Errorf("%s: status %q (%q), want %q", job.Name, job.Status, job.Reason, want[job.Name])
		}
	}
}

func TestHealthReportsStaleData(t *testing.T) {
	d := testDeps()
	d.runtime.SetBalance([]model.CheckResult{{Project: "demo", Success: true}})
	d.now = func() time.Time { return time.Now().Add(10 * time.Minute) }

	var out HealthOutput
	structured(t, call(t, connect(t, d), "health", nil), &out)
	if out.Version != "v-test" || !out.IsStale || !out.Meta.IsStale || out.Status != "degraded" {
		t.Errorf("ten minutes without a refresh at a 60 s interval is stale: %+v", out)
	}
	if out.Meta.DataAgeSeconds == nil || *out.Meta.DataAgeSeconds < 590 {
		t.Errorf("data age should be about 600 s: %v", out.Meta.DataAgeSeconds)
	}
}

func TestCapabilitiesShowScopesButNoSecrets(t *testing.T) {
	d := testDeps(config.ScopeBalance, config.ScopeAlerts)
	d.settings.WebAPIKey = "web-key-that-must-stay-hidden"
	res := call(t, connect(t, d), "capabilities", nil)
	var out CapabilitiesOutput
	structured(t, res, &out)
	if out.Caller != "agent" || !slices.Equal(out.Scopes, []string{config.ScopeBalance, config.ScopeAlerts}) {
		t.Errorf("caller/scopes = %q %v", out.Caller, out.Scopes)
	}
	if !out.Features["mcp"] || out.Features["email_scan"] || out.Features["database"] {
		t.Errorf("features = %v", out.Features)
	}
	if out.Conventions.Units["quota"] == "" {
		t.Error("capabilities should explain the units")
	}
	if strings.Contains(text(res, 1), "web-key-that-must-stay-hidden") {
		t.Fatal("capabilities must not expose credentials")
	}
}

func TestEmailScanStatusMasksAddresses(t *testing.T) {
	d := testDeps(config.ScopeEmail)
	d.runtime.SetEmailScan(model.ScanResult{Days: 1, Mailboxes: []model.MailboxResult{
		{Name: "billing", Host: "imap.example.com", Port: 993, Username: "alice@example.com", Success: true},
	}})
	res := call(t, connect(t, d), "email_scan_status", nil)
	var out EmailScanOutput
	structured(t, res, &out)
	if len(out.Mailboxes) != 1 || out.Mailboxes[0].Username != "a***@example.com" || strings.Contains(text(res, 1), "alice@") {
		t.Errorf("mailbox addresses must be masked: %+v", out.Mailboxes)
	}
	if out.Status != "configured_but_inactive" || out.Reason == "" {
		t.Errorf("without an enabled mailbox the scan is inactive: %q %q", out.Status, out.Reason)
	}
}

func TestResourcesFollowTheKeysScopes(t *testing.T) {
	session := connect(t, testDeps(config.ScopeBalance))
	ctx := context.Background()
	res, err := session.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: "quotapulse://state/balance"})
	if err != nil || len(res.Contents) != 1 || !strings.Contains(res.Contents[0].Text, `"counts"`) {
		t.Fatalf("balance resource: %v %+v", err, res)
	}
	if res.CacheScope != "private" {
		t.Errorf("resource reads must be private, got %q", res.CacheScope)
	}
	if _, err := session.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: "quotapulse://state/email"}); err == nil {
		t.Error("a balance key must not read the email state")
	}
}

func TestSubscriptionConfigHidesTheWebhookURL(t *testing.T) {
	view := subscriptionConfigView(model.Subscription{Name: "Claude Max", CycleType: "monthly", RenewalDay: 12, WebhookURL: "https://hooks.example.test/secret-token"})
	body, _ := json.Marshal(view)
	if !view.WebhookOverride || strings.Contains(string(body), "secret-token") {
		t.Fatalf("the webhook URL must never be returned: %s", body)
	}
}

func TestMaskEmail(t *testing.T) {
	for in, want := range map[string]string{
		"alice@example.com": "a***@example.com", "@example.com": "***@example.com", "ab": "***", "billing": "b***",
	} {
		if got := maskEmail(in); got != want {
			t.Errorf("maskEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClassifyCheckError(t *testing.T) {
	for message, want := range map[string]string{
		"HTTP 401: Unauthorized":                "auth",
		"HTTP 429: Too Many Requests":           "rate_limited",
		"dial tcp: i/o timeout":                 "network",
		"HTTP 502: Bad Gateway":                 "provider",
		"response is not valid JSON":            "invalid_response",
		"unknown provider \"acme\"":             "config",
		"something else went wrong upstream ok": "provider",
	} {
		if got, _ := classifyCheckError(message); got != want {
			t.Errorf("classifyCheckError(%q) = %q, want %q", message, got, want)
		}
	}
}

func TestProvidersCatalogIsAvailable(t *testing.T) {
	list := provider.All()
	if len(list) == 0 {
		t.Fatal("provider registry should not be empty")
	}
	var out ProvidersOutput
	structured(t, call(t, connect(t, testDeps()), "providers", nil), &out)
	if out.Count != len(list) || out.Providers[0].Provider == "" || out.Providers[0].Unit == "" {
		t.Fatalf("providers = %+v", out)
	}
}

func TestDashboardSummaryPutsTheUrgentFirst(t *testing.T) {
	d := testDeps(config.AllScopes...)
	d.settings.RunwayAlertDays = 7
	threshold := 10.0
	d.runtime.SetBalance([]model.CheckResult{
		{Project: "healthy", Provider: "deepseek", Type: "balance", Success: true, Credits: model.Ptr(500.0), Threshold: &threshold,
			Runway: &model.Runway{RunwayDays: model.Ptr(40.0), Confidence: "high", BurnPerDay: model.Ptr(12.5)}},
		{Project: "short", Provider: "deepseek", Type: "balance", Success: true, Credits: model.Ptr(30.0), Threshold: &threshold,
			Runway: &model.Runway{RunwayDays: model.Ptr(2.5), DepletionDate: model.Ptr("2026-10-03"), Confidence: "medium", BurnPerDay: model.Ptr(12.0)}},
		{Project: "low", Provider: "volc", Type: "balance", Success: true, Credits: model.Ptr(3.0), Threshold: &threshold, NeedAlarm: true},
		{Project: "broken", Provider: "volc", Type: "balance", Error: model.Ptr("HTTP 401: invalid api key")},
	})
	d.runtime.SetSubscriptions([]model.SubscriptionResult{
		{Name: "Claude Max", CycleType: "monthly", NextRenewalDate: "2026-10-01", DaysUntilRenewal: 1, NeedAlert: true, Amount: 100},
		{Name: "Cursor", CycleType: "monthly", NextRenewalDate: "2026-10-20", DaysUntilRenewal: 20, Amount: 20},
	})
	next := time.Now().Add(time.Hour)
	d.runtime.RegisterJob("alert_check", "Alert check", "09:00", true, next)
	d.runtime.RecordJobRun("alert_check", false, time.Now(), time.Second, errors.New("webhook down"), nil, next)

	res := call(t, connect(t, d), "dashboard_summary", nil)
	var out DashboardSummaryOutput
	structured(t, res, &out)
	var got []string
	for _, item := range out.Attention {
		got = append(got, item.Severity+" "+item.Kind+" "+item.Subject)
	}
	want := []string{
		"critical below_threshold low", "critical check_failed broken", "critical renewal_due Claude Max",
		"warning runway_short short", "warning job_failing alert_check",
	}
	if !slices.Equal(got, want) {
		t.Errorf("attention =\n%v\nwant\n%v", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if out.Status != "degraded" || !strings.HasPrefix(text(res, 0), "degraded: low below threshold") {
		t.Errorf("a failing job degrades the service: %q / %q", out.Status, text(res, 0))
	}
	if out.Balances == nil || out.Balances.Counts.Total != 4 || out.Balances.ShortestRunway == nil || out.Balances.ShortestRunway.ProjectName != "short" {
		t.Errorf("balances overview = %+v", out.Balances)
	}
	if s := out.Subscriptions; s == nil || s.Due != 1 || s.Within7Days != 1 || s.Within30Days != 2 || s.Next == nil || s.Next.Name != "Claude Max" {
		t.Errorf("subscriptions overview = %+v", out.Subscriptions)
	}
	if out.Alerts != nil {
		t.Error("without a database there is no alert overview")
	}

	var scoped DashboardSummaryOutput
	structured(t, call(t, connect(t, &deps{
		settings: d.settings, runtime: d.runtime, log: d.log, now: d.now,
		caller: config.MCPCaller{Name: "subs", Scopes: map[string]bool{config.ScopeSubscriptions: true}},
	}), "dashboard_summary", nil), &scoped)
	if scoped.Balances != nil || scoped.Email != nil || scoped.Subscriptions == nil {
		t.Errorf("a subscriptions key sees only the subscriptions overview: %+v", scoped)
	}
	for _, item := range scoped.Attention {
		if item.Kind == "below_threshold" || item.Kind == "check_failed" || item.Kind == "runway_short" {
			t.Errorf("a subscriptions key must not learn about balances: %+v", item)
		}
	}
}

func TestTimeRangeParsing(t *testing.T) {
	shanghai := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, shanghai)
	w, err := timeRange{Since: "2026-09-28", Until: "2026-09-29"}.parse(now, shanghai)
	if err != nil {
		t.Fatal(err)
	}
	if !w.since.Equal(time.Date(2026, 9, 28, 0, 0, 0, 0, shanghai)) || !w.until.Equal(time.Date(2026, 9, 30, 0, 0, 0, 0, shanghai)) {
		t.Errorf("dates are local days, and until includes its day: %v – %v", w.since, w.until)
	}
	if w.days != 4 {
		t.Errorf("the read must reach back to since: %d days", w.days)
	}
	if w, err := (timeRange{Since: "2026-09-30T01:00:00Z"}).parse(now, shanghai); err != nil || !w.since.Equal(time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC)) {
		t.Errorf("RFC 3339 since: %v %v", w.since, err)
	}
	for _, bad := range []timeRange{{Since: "yesterday"}, {Until: "2026-13-01"}, {Since: "2026-09-29", Until: "2026-09-28"}, {Cursor: "not-a-cursor"}} {
		if _, err := bad.parse(now, shanghai); err == nil {
			t.Errorf("%+v should be rejected", bad)
		}
	}
	if w, _ := (timeRange{Since: "2020-01-01"}).parse(now, shanghai); w.days != 365 {
		t.Errorf("the read is capped at 365 days, got %d", w.days)
	}
}

func TestPaginateWalksNewestFirst(t *testing.T) {
	base := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	var entries []entry[int]
	for i := range 5 {
		// Two rows share each timestamp, to prove ties break on the ID.
		at := base.Add(time.Duration(i/2) * time.Hour).Format(time.RFC3339Nano)
		entries = append(entries, newEntry(at, rowID("alert", int64(i)), i))
	}
	w := window{}
	first := paginate(slices.Clone(entries), w, 2, false)
	if !slices.Equal(first.items, []int{4, 3}) || first.next == "" {
		t.Fatalf("first page = %v (next %q)", first.items, first.next)
	}
	after, err := decodeCursor(first.next)
	if err != nil {
		t.Fatal(err)
	}
	w.after = after
	second := paginate(slices.Clone(entries), w, 2, false)
	w.after, _ = decodeCursor(second.next)
	third := paginate(slices.Clone(entries), w, 2, true)
	if !slices.Equal(second.items, []int{2, 1}) || !slices.Equal(third.items, []int{0}) || third.next != "" || !third.truncated {
		t.Errorf("pages = %v, %v (next %q, truncated %v)", second.items, third.items, third.next, third.truncated)
	}
	w = window{since: base.Add(time.Hour), until: base.Add(2 * time.Hour)}
	if got := paginate(slices.Clone(entries), w, 10, false).items; !slices.Equal(got, []int{3, 2}) {
		t.Errorf("since/until should keep the second hour only, got %v", got)
	}
}

func TestTrendBucketsFollowTheServerDay(t *testing.T) {
	shanghai := time.FixedZone("CST", 8*3600)
	at := func(day, hour int) time.Time { return time.Date(2026, 9, day, hour, 0, 0, 0, shanghai) }
	samples := []sample{
		{at: at(27, 23), balance: 100}, // Sunday
		{at: at(28, 1), balance: 90},   // Monday, a new week
		{at: at(28, 12), balance: 80},
		{at: at(28, 13), balance: 150}, // top-up
		{at: at(29, 9), balance: 140, below: true},
	}
	days := bucket(samples, "day", shanghai)
	if len(days) != 3 || days[1].Timestamp != "2026-09-28T00:00:00+08:00" || days[1].Balance != 150 || days[1].Min != 80 || days[1].Max != 150 || days[1].Samples != 3 {
		t.Fatalf("days = %+v", days)
	}
	if !days[2].BelowThreshold {
		t.Error("a bucket is below the threshold when its last snapshot is")
	}
	if weeks := bucket(samples, "week", shanghai); len(weeks) != 2 || weeks[1].Timestamp != "2026-09-28T00:00:00+08:00" {
		t.Errorf("weeks start on Monday: %+v", weeks)
	}
	if hours := bucket(samples, "hour", shanghai); len(hours) != 5 {
		t.Errorf("hours = %d", len(hours))
	}
	if raw := bucket(samples, "raw", shanghai); len(raw) != 5 || raw[0].Timestamp != "2026-09-27T15:00:00Z" {
		t.Errorf("raw keeps every snapshot in UTC: %+v", raw[0])
	}

	s := summarize(samples)
	if s.First != 100 || s.Last != 140 || s.Min != 80 || s.Max != 150 || s.Change != 40 || s.Consumed != 30 || s.ToppedUp != 70 || s.TopUps != 1 || s.Average != 112 {
		t.Errorf("summary = %+v", s)
	}
}

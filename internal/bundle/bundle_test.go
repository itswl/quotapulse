package bundle

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quotapulse/internal/core"
	"quotapulse/internal/engine"
	"quotapulse/internal/provider"
	"quotapulse/internal/recur"
	"quotapulse/internal/secret"
	"quotapulse/internal/store"
)

var (
	ctx = context.Background()
	now = time.Date(2026, 11, 16, 2, 0, 0, 0, time.UTC)
)

func newEngine(t *testing.T) *engine.Engine {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "quotapulse.db"), secret.FromPassphrase("test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return engine.New(engine.Options{
		Store: st, Now: func() time.Time { return now },
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }

// populate fills an install with one of everything.
func populate(t *testing.T, e *engine.Engine) {
	t.Helper()
	settings, _ := e.Settings(ctx)
	settings.Timezone, settings.Language, settings.BaseCurrency = "Asia/Shanghai", "zh", "CNY"
	settings.FX = map[string]float64{"USD": 7.16}
	settings.Quiet.Enabled = false
	_, err := e.UpdateSettings(ctx, settings)
	must(t, err)

	_, err = e.SaveProvider(ctx, provider.Definition{
		ID: "relay", Name: "Relay",
		Fields:  []provider.Field{{Key: "api_key", Label: "API Key", Secret: true}},
		Request: provider.Request{URL: "https://relay.example/balance", Auth: provider.Auth{Type: provider.AuthBearer}},
		Extract: "{value: .balance}",
	})
	must(t, err)
	_, err = e.CreateChannel(ctx, engine.ChannelInput{
		Name: ptr("ops"), Type: ptr("webhook"), Config: map[string]string{"url": "https://hook.example/secret-0001"},
		Topics: &[]string{core.TopicMeters}, MinSeverity: ptr("warning"),
	}, "en")
	must(t, err)
	account, err := e.CreateAccount(ctx, engine.AccountInput{
		Name: ptr("Main"), Provider: ptr("relay"), Group: ptr("AI"), Interval: ptr(600),
		Credential: map[string]string{"api_key": "sk-secret-0001"},
	}, "en")
	must(t, err)

	// A sampled meter with a threshold and some history, straight into storage.
	st := e.Store()
	meter := core.Meter{AccountID: account.ID, Key: "balance", Name: "Balance", Kind: core.Balance, Unit: "CNY", Rules: core.Rules{Low: ptr(50.0)}}
	must(t, st.CreateMeter(ctx, &meter, now))
	var readings []core.Reading
	for h := 48; h >= 0; h-- {
		readings = append(readings, core.Reading{At: now.Add(-time.Duration(h) * time.Hour), Value: 500 - float64(48-h)})
	}
	must(t, st.ImportReadings(ctx, &meter, readings))

	_, err = e.Ingest(ctx, []engine.Push{{Meter: "claude-code", Name: "Claude Code", Kind: core.Counter, Unit: "USD", Value: ptr(12.5)}})
	must(t, err)

	_, err = e.CreateReminder(ctx, engine.ReminderInput{
		Title: ptr("Alex"), Kind: ptr(core.ReminderBirthday),
		Rule:     &recur.Rule{Freq: recur.Yearly, Calendar: recur.LunarCal, Month: 11, Day: 16, Since: 1990},
		LeadDays: &[]int{2, 0}, Notes: ptr("gift"),
	}, "en")
	must(t, err)
	_, err = e.CreateToken(ctx, engine.TokenInput{Name: "hermes", Scopes: []string{core.ScopeRead}})
	must(t, err)

	resolved := now.Add(-time.Hour)
	must(t, st.SaveIncident(ctx, core.Incident{
		ID: "inc_old", SubjectType: core.SubjectMeter, SubjectID: meter.ID, Kind: core.IncidentLevel,
		Severity: core.Warning, State: core.StateResolved, Reason: core.ReasonRunway, Title: "Main",
		Data: map[string]any{"value": 40.0}, OpenedAt: now.Add(-3 * time.Hour), UpdatedAt: resolved, ResolvedAt: &resolved,
	}))
}

func TestRoundTrip(t *testing.T) {
	source := newEngine(t)
	populate(t, source)
	hermes, _ := source.Tokens(ctx)

	exported, err := Export(ctx, source, ExportOptions{Secrets: true, History: true, Source: "test"})
	must(t, err)
	raw, err := json.Marshal(exported)
	must(t, err)

	// Through JSON, as a file would go, into an empty install.
	parsed, err := Parse(raw)
	must(t, err)
	target := newEngine(t)
	report, err := Import(ctx, target, parsed, ImportOptions{Settings: true})
	must(t, err)
	if got := report.Summary(); got != "1 providers, 1 channels, 1 accounts, 2 meters, 50 readings, 1 reminders, 1 tokens, 1 incidents" {
		t.Fatalf("summary = %q, notes = %v", got, report.Notes)
	}

	settings, _ := target.Settings(ctx)
	if settings.Timezone != "Asia/Shanghai" || settings.FX["USD"] != 7.16 {
		t.Errorf("settings = %+v", settings)
	}
	st := target.Store()
	account, err := st.AccountByName(ctx, "Main")
	must(t, err)
	if account.Credential["api_key"] != "sk-secret-0001" || account.Group != "AI" || account.Interval != 600 || !account.Enabled {
		t.Errorf("account = %+v", account)
	}
	meter, err := st.MeterByKey(ctx, account.ID, "balance")
	must(t, err)
	if *meter.Rules.Low != 50 || *meter.Value != 452 || meter.Unit != "CNY" {
		t.Errorf("meter = %+v", meter)
	}
	if n, _ := st.ReadingCount(ctx, meter.ID); n != 49 {
		t.Errorf("readings = %d", n)
	}
	pushed, err := st.PushedMeter(ctx, "claude-code")
	if err != nil || pushed.Name != "Claude Code" || *pushed.Value != 12.5 {
		t.Errorf("pushed = %+v, %v", pushed, err)
	}
	channels, _ := st.Channels(ctx)
	if len(channels) != 1 || channels[0].Config["url"] != "https://hook.example/secret-0001" || channels[0].MinSeverity != core.Warning || len(channels[0].Topics) != 1 {
		t.Errorf("channels = %+v", channels)
	}
	reminders, _ := st.Reminders(ctx)
	if len(reminders) != 1 || reminders[0].Rule.Since != 1990 || reminders[0].Notes != "gift" || len(reminders[0].LeadDays) != 2 {
		t.Errorf("reminders = %+v", reminders)
	}
	// A token issued before the move still works after it.
	hashes, _ := source.Store().TokenHashes(ctx)
	restored, _ := st.TokenHashes(ctx)
	found := false
	for _, hash := range restored {
		found = found || hash == hashes[hermes[0].ID]
	}
	if !found {
		t.Error("the token's hash did not survive")
	}
	// History comes along, attached to the right meter.
	closed, _ := st.Incidents(ctx, store.IncidentFilter{State: core.StateResolved})
	if len(closed) != 1 || closed[0].SubjectID != meter.ID || closed[0].Reason != core.ReasonRunway || closed[0].Data["value"] != 40.0 {
		t.Errorf("incidents = %+v", closed)
	}

	// Importing the same bundle again changes nothing.
	again, err := Import(ctx, target, parsed, ImportOptions{})
	must(t, err)
	if again.Summary() != "1 incidents" && again.Summary() != "nothing new" {
		t.Errorf("second import = %q", again.Summary())
	}
	if again.Skipped["accounts"] != 1 || again.Skipped["meters"] != 2 || again.Skipped["reminders"] != 1 || again.Skipped["tokens"] != 1 || again.Skipped["channels"] != 1 {
		t.Errorf("skipped = %v", again.Skipped)
	}
	if stats, _ := st.Stats(ctx); stats.Accounts != 1 || stats.Meters != 2 || stats.Reminders != 1 || stats.Readings != 50 {
		t.Errorf("stats after a second import = %+v", stats)
	}
}

func TestExportWithoutSecretsCarriesNone(t *testing.T) {
	source := newEngine(t)
	populate(t, source)
	exported, err := Export(ctx, source, ExportOptions{History: false})
	must(t, err)
	raw, _ := json.Marshal(exported)
	for _, secret := range []string{"sk-secret-0001", "secret-0001", "hash"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("a redacted export contains %q", secret)
		}
	}
	if !exported.Redacted() || len(exported.Tokens) != 0 || len(exported.Incidents) != 0 {
		t.Errorf("exported = %+v", exported)
	}
	for _, m := range exported.Meters {
		if len(m.Readings) != 0 {
			t.Error("history was exported without being asked for")
		}
	}

	// Imported, what lost its secret comes in switched off, with a note
	// saying why — not as a broken account that fails at the next sample.
	target := newEngine(t)
	report, err := Import(ctx, target, exported, ImportOptions{Settings: true})
	must(t, err)
	account, _ := target.Store().AccountByName(ctx, "Main")
	if account.Enabled {
		t.Error("an account without a credential was imported enabled")
	}
	channels, _ := target.Store().Channels(ctx)
	if channels[0].Enabled {
		t.Error("a channel without settings was imported enabled")
	}
	notes := strings.Join(report.Notes, "\n")
	if !strings.Contains(notes, `account "Main": imported disabled`) || !strings.Contains(notes, `channel "ops": imported disabled`) {
		t.Errorf("notes = %v", report.Notes)
	}

	full, _ := Export(ctx, source, ExportOptions{Secrets: true})
	if full.Redacted() {
		t.Error("an export with secrets says it has none")
	}
}

func TestImportIsForgivingAboutWhatItCannotPlace(t *testing.T) {
	target := newEngine(t)
	report, err := Import(ctx, target, Bundle{
		Format: Format, Version: Version,
		Notes: []string{"the old email scanner has no equivalent"},
		Accounts: []Account{
			{Name: "Ghost", Provider: "nowhere", Enabled: true},
			{Name: "DeepSeek", Provider: "deepseek", Credential: map[string]string{"api_key": "sk-test-0001"}, Enabled: true},
		},
		Meters: []Meter{
			{Account: "Ghost", Key: "balance", Kind: core.Balance},
			{Account: "DeepSeek", Key: "balance-cny", Kind: core.Balance, Unit: "CNY", Rules: core.Rules{Low: ptr(50.0)},
				Readings: [][]float64{{1794700800, 550}, {1794704400, 548, 0}, {7}}},
			{Key: "bad", Kind: "gauge"},
		},
		Reminders: []Reminder{
			{Title: "broken", Rule: recur.Rule{Freq: "daily"}, Enabled: true},
			{Title: "ok", Rule: recur.Rule{Freq: recur.Monthly, Day: 1}, Enabled: true},
		},
		Tokens: []Token{
			{Name: "agent", Scopes: []string{core.ScopeRead}, Token: "legacy-key-0001"},
			{Name: "nothing", Scopes: []string{core.ScopeRead}},
		},
		Channels:  []Channel{{Name: "pigeon", Type: "pigeon"}},
		Providers: []provider.Definition{{ID: "deepseek", Name: "Mine"}, {ID: "bad", Name: "Bad"}},
		Incidents: []Incident{{SubjectType: core.SubjectMeter, Subject: "Ghost/balance", Kind: core.IncidentLevel, Severity: core.Warning, Title: "x", OpenedAt: now, ResolvedAt: now}},
	}, ImportOptions{})
	must(t, err)
	if got := report.Summary(); got != "1 accounts, 1 meters, 2 readings, 1 reminders, 1 tokens" {
		t.Fatalf("summary = %q\nnotes = %v", got, report.Notes)
	}
	if report.Notes[0] != "the old email scanner has no equivalent" {
		t.Errorf("the bundle's own notes come first: %v", report.Notes)
	}
	notes := strings.Join(report.Notes, "\n")
	for _, want := range []string{
		`account "Ghost": there is no provider called "nowhere"`,
		`meter "Ghost/balance": its account is not in the database`,
		`meter "bad": kind must be`,
		`reminder "broken"`,
		`token "nothing"`,
		`channel "pigeon": unknown type`,
		`provider "deepseek": built in`,
		`provider "bad":`,
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes do not mention %q:\n%s", want, notes)
		}
	}
	// The key that was already in use authenticates.
	if _, ok := target.Authenticate(ctx, "legacy-key-0001"); !ok {
		t.Error("a migrated key does not authenticate")
	}
	// The threshold is in place before the account is ever sampled.
	account, _ := target.Store().AccountByName(ctx, "DeepSeek")
	meter, err := target.Store().MeterByKey(ctx, account.ID, "balance-cny")
	must(t, err)
	if *meter.Rules.Low != 50 || *meter.Value != 548 {
		t.Errorf("meter = %+v", meter)
	}
	// Settings were not asked for, so they were not touched.
	if s, _ := target.Settings(ctx); s.Timezone != "UTC" {
		t.Errorf("settings changed: %+v", s)
	}
}

func TestBrowserSubscriptionsTravelWithTheirKeys(t *testing.T) {
	source := newEngine(t)
	key, err := source.PushKey(ctx)
	must(t, err)
	sub := engine.PushSubscription{Endpoint: "https://push.example/send/device-0001"}
	sub.Keys.P256dh, sub.Keys.Auth = "BKey", "auth"
	_, err = source.SubscribePush(ctx, "Phone", sub, "en")
	must(t, err)

	// Without secrets a subscription has nothing left to export.
	redacted, err := Export(ctx, source, ExportOptions{})
	must(t, err)
	if redacted.Push != nil || len(redacted.Channels) != 0 || !redacted.Redacted() {
		t.Fatalf("redacted = %+v", redacted)
	}

	full, err := Export(ctx, source, ExportOptions{Secrets: true})
	must(t, err)
	if full.Push == nil || full.Push.Public != key || len(full.Channels) != 1 || full.Redacted() {
		t.Fatalf("full = %+v", full)
	}

	// Into an empty install: the keys and the device both arrive, and match.
	target := newEngine(t)
	report, err := Import(ctx, target, full, ImportOptions{})
	must(t, err)
	if got, _ := target.PushKey(ctx); got != key || report.Created["channels"] != 1 {
		t.Fatalf("key %q, created %v", got, report.Created)
	}

	// Into an install with keys of its own: its browsers are bound to those,
	// so the bundle's subscriptions are useless there — and say so.
	other := newEngine(t)
	own, err := other.PushKey(ctx)
	must(t, err)
	report, err = Import(ctx, other, full, ImportOptions{})
	must(t, err)
	if got, _ := other.PushKey(ctx); got != own {
		t.Fatal("existing push keys were replaced by an import")
	}
	if report.Created["channels"] != 0 || !strings.Contains(strings.Join(report.Notes, "\n"), "already has push keys of its own") {
		t.Fatalf("report = %+v", report)
	}

	// A subscription with no keys in the bundle at all cannot work anywhere.
	orphan := full
	orphan.Push = nil
	report, err = Import(ctx, newEngine(t), orphan, ImportOptions{})
	must(t, err)
	if report.Created["channels"] != 0 || !strings.Contains(strings.Join(report.Notes, "\n"), "without its push keys") {
		t.Fatalf("report = %+v", report)
	}
}

func TestSettingsCanBePartial(t *testing.T) {
	parsed, err := Parse([]byte(`{"format":"quotapulse-bundle","version":1,
		"settings":{"timezone":"Asia/Shanghai","language":"zh","quiet":{"enabled":false}}}`))
	must(t, err)
	target := newEngine(t)
	_, err = Import(ctx, target, parsed, ImportOptions{Settings: true})
	must(t, err)
	s, _ := target.Settings(ctx)
	// What was written is applied; everything else keeps its default.
	if s.Timezone != "Asia/Shanghai" || s.Language != "zh" || s.Quiet.Enabled {
		t.Errorf("settings = %+v", s)
	}
	if s.SampleSeconds != 1800 || s.Quiet.Start != "23:00" || s.BaseCurrency != "USD" || !s.Digest.Enabled {
		t.Errorf("defaults were lost: %+v", s)
	}

	bad, err := Parse([]byte(`{"format":"quotapulse-bundle","version":1,"settings":{"timezone":"Mars/Olympus"}}`))
	must(t, err)
	if _, err := Import(ctx, newEngine(t), bad, ImportOptions{Settings: true}); err == nil || !strings.Contains(err.Error(), "timezone") {
		t.Errorf("an invalid setting: %v", err)
	}
	typo, err := Parse([]byte(`{"format":"quotapulse-bundle","version":1,"settings":{"timezon":"UTC"}}`))
	must(t, err)
	if _, err := Import(ctx, newEngine(t), typo, ImportOptions{Settings: true}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("a misspelt setting: %v", err)
	}
}

func TestParse(t *testing.T) {
	cases := map[string]string{
		`{}`: "not a QuotaPulse bundle",
		`{"format":"quotapulse-bundle","version":99}`:             "version 99 is not supported",
		`{"format":"quotapulse-bundle","version":1,"acounts":[]}`: "unknown field",
		`not json`: "not a valid bundle",
	}
	for raw, want := range cases {
		if _, err := Parse([]byte(raw)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%s) = %v, want %q", raw, err, want)
		}
	}
	if _, err := Parse([]byte(`{"format":"quotapulse-bundle","version":1}`)); err != nil {
		t.Errorf("an empty bundle is valid: %v", err)
	}
}

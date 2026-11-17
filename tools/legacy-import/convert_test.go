package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fernet/fernet-go"

	"github.com/itswl/quotapulse/internal/bundle"
	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/engine"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/recur"
	"github.com/itswl/quotapulse/internal/secret"
	"github.com/itswl/quotapulse/internal/store"
)

var (
	ctx = context.Background()
	now = time.Date(2026, 11, 16, 2, 0, 0, 0, time.UTC) // 10:00 in Shanghai
)

func openOld(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, _, err := openDatabase("sqlite:///" + path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// importInto runs a bundle through the real import, as a file would go.
func importInto(t *testing.T, b bundle.Bundle) (*engine.Engine, bundle.Report) {
	t.Helper()
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := bundle.Parse(raw)
	if err != nil {
		t.Fatalf("the converter wrote a bundle the importer rejects: %v", err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "quotapulse.db"), secret.FromPassphrase("test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := engine.New(engine.Options{
		Store: st, Now: func() time.Time { return now }, Mute: true,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	report, err := bundle.Import(ctx, e, parsed, bundle.ImportOptions{Settings: true})
	if err != nil {
		t.Fatal(err)
	}
	return e, report
}

func notesOf(b bundle.Bundle) string { return strings.Join(b.Notes, "\n") }

func settingsOf(t *testing.T, b bundle.Bundle) core.Settings {
	t.Helper()
	s, err := b.ReadSettings()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// testdata/legacy.db is a database written by the very first version: six
// tables, none of the columns added later. If this reads, every install reads.
func TestTheOldestDatabase(t *testing.T) {
	out, err := convert(source{db: openOld(t, "testdata/legacy.db"), env: map[string]string{"TZ": "Asia/Shanghai"}, now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Accounts) != 2 || len(out.Meters) != 2 || len(out.Reminders) != 1 || len(out.Incidents) != 1 {
		t.Fatalf("converted %d accounts, %d meters, %d reminders, %d incidents", len(out.Accounts), len(out.Meters), len(out.Reminders), len(out.Incidents))
	}

	volc := out.Accounts[0]
	if volc.Name != "火山-主账号" || volc.Provider != "volcengine" || volc.Group != "云服务" || !volc.Enabled {
		t.Errorf("account = %+v", volc)
	}
	// The old single "ID:Secret" string becomes the two fields it always was.
	if volc.Credential["access_key_id"] != "AK" || volc.Credential["secret_access_key"] != "SK" {
		t.Errorf("credential = %v", volc.Credential)
	}
	if deepseek := out.Accounts[1]; deepseek.Provider != "deepseek" || deepseek.Enabled || deepseek.Credential["api_key"] != "sk-old" {
		t.Errorf("a disabled account stays disabled, with its key: %+v", deepseek)
	}

	meter := out.Meters[0]
	if meter.Account != "火山-主账号" || meter.Key != "balance" || meter.Unit != "CNY" || *meter.Rules.Low != 7000 || meter.Name != "余额" {
		t.Errorf("meter = %+v", meter)
	}
	if len(meter.Readings) != 5 || meter.Readings[0][1] != 9000 || meter.Readings[4][1] != 6900 {
		t.Errorf("readings = %v", meter.Readings)
	}
	// Stored as naive UTC "2026-09-16 16:01:04.553371".
	if at := time.Unix(int64(meter.Readings[0][0]), 0).UTC(); at.Format(time.RFC3339) != "2026-09-16T16:01:04Z" {
		t.Errorf("first reading at %s", at)
	}
	if out.Meters[1].Key != "balance-cny" || *out.Meters[1].Rules.Low != 50 {
		t.Errorf("deepseek meter = %+v", out.Meters[1])
	}

	renewal := out.Reminders[0]
	if renewal.Title != "域名续费" || renewal.Kind != core.ReminderRenewal || renewal.Rule.Freq != recur.Yearly || renewal.Rule.Month != 3 || renewal.Rule.Day != 15 {
		t.Errorf("reminder = %+v", renewal)
	}
	if *renewal.Amount != 88 || renewal.Currency != "CNY" || renewal.Group != "基础设施" || len(renewal.LeadDays) != 4 || renewal.LeadDays[0] != 3 {
		t.Errorf("reminder = %+v", renewal)
	}

	incident := out.Incidents[0]
	if incident.Subject != "火山-主账号/balance" || incident.Kind != core.IncidentLevel || incident.Reason != core.ReasonLow || incident.Data["imported"] != "余额不足: 6900.0 < 7000.0" {
		t.Errorf("incident = %+v", incident)
	}
	if !strings.Contains(notesOf(out), "1 mailbox(es)") {
		t.Errorf("the dropped mailbox is not mentioned:\n%s", notesOf(out))
	}
	if s := settingsOf(t, out); s.Language != "zh" || s.BaseCurrency != "CNY" {
		t.Errorf("settings = %+v", s)
	}

	// And the whole thing goes into a 1.0 database.
	e, report := importInto(t, out)
	if got := report.Summary(); got != "2 accounts, 2 meters, 5 readings, 1 reminders, 1 incidents" {
		t.Fatalf("import = %q\n%s", got, strings.Join(report.Notes, "\n"))
	}
	meters, err := e.Meters(ctx, "zh")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range meters {
		if m.Display == "火山-主账号" {
			found = true
			// The last reading, 6900, is under the 7000 threshold that came
			// with it: the incident opens from history alone.
			if *m.Value != 6900 || m.Status != "critical" || len(m.Incidents) != 1 {
				t.Errorf("migrated meter = %+v", m)
			}
		}
	}
	if !found {
		t.Fatalf("meters = %+v", meters)
	}
	history, _ := e.Incidents(ctx, store.IncidentFilter{State: core.StateResolved}, "zh")
	if len(history) != 1 || history[0].Detail != "余额不足: 6900.0 < 7000.0" || history[0].Headline != "余额不足 · 火山-主账号" {
		t.Errorf("history = %+v", history)
	}
}

// currentSchema is the last schema the old version wrote.
const currentSchema = `
CREATE TABLE project_config (id INTEGER PRIMARY KEY, name VARCHAR(200) NOT NULL UNIQUE, owner_project VARCHAR(200),
	provider VARCHAR(50) NOT NULL, api_key TEXT NOT NULL, threshold FLOAT, type VARCHAR(20), currency VARCHAR(10),
	monthly_budget FLOAT, enabled BOOLEAN, created_at DATETIME, updated_at DATETIME);
CREATE TABLE balance_history (id INTEGER PRIMARY KEY, project_id VARCHAR(200) NOT NULL, project_name VARCHAR(200) NOT NULL,
	provider VARCHAR(50) NOT NULL, balance FLOAT NOT NULL, threshold FLOAT, balance_type VARCHAR(20), need_alarm BOOLEAN, timestamp DATETIME);
CREATE TABLE alert_history (id INTEGER PRIMARY KEY, project_id VARCHAR(200) NOT NULL, project_name VARCHAR(200) NOT NULL,
	alert_type VARCHAR(50) NOT NULL, status VARCHAR(20), message TEXT, balance_value FLOAT, threshold_value FLOAT, timestamp DATETIME);
CREATE TABLE subscription_config (id INTEGER PRIMARY KEY, name VARCHAR(200) NOT NULL UNIQUE, owner_project VARCHAR(200),
	cycle_type VARCHAR(20), renewal_day INTEGER, alert_days_before INTEGER, amount FLOAT, enabled BOOLEAN,
	last_renewed_date VARCHAR(20), snoozed_until VARCHAR(20), timezone VARCHAR(50), webhook_url TEXT, created_at DATETIME, updated_at DATETIME);
CREATE TABLE email_config (id INTEGER PRIMARY KEY, name VARCHAR(200), host VARCHAR(200), port INTEGER, username VARCHAR(200), password TEXT);
CREATE TABLE push_subscriptions (id INTEGER PRIMARY KEY, endpoint VARCHAR(500), p256dh TEXT, auth TEXT);
CREATE TABLE app_settings (setting_key VARCHAR(200) PRIMARY KEY, setting_value TEXT NOT NULL);
CREATE TABLE custom_provider_config (id INTEGER PRIMARY KEY, name VARCHAR(50) NOT NULL UNIQUE, display_name VARCHAR(100), url TEXT NOT NULL,
	auth_mode VARCHAR(10) NOT NULL, auth_param VARCHAR(50), default_type VARCHAR(20) NOT NULL, currency VARCHAR(10),
	balance_path VARCHAR(200) NOT NULL, balance_scale FLOAT, success_path VARCHAR(200), success_values VARCHAR(200),
	error_path VARCHAR(200), subtract_path VARCHAR(200), extract_mode VARCHAR(20), window_types VARCHAR(100),
	window_reset_hours FLOAT, extra_params VARCHAR(400), sign_region VARCHAR(50), sign_service VARCHAR(50));
CREATE TABLE usage_events (id INTEGER PRIMARY KEY, source VARCHAR(50), project_name VARCHAR(200), entry_date VARCHAR(10),
	model VARCHAR(100), cost DOUBLE, currency VARCHAR(10), input_tokens INTEGER, output_tokens INTEGER, requests INTEGER);
`

func oldSeal(t *testing.T, passphrase, plaintext string) string {
	t.Helper()
	var key fernet.Key
	sum := sha256.Sum256([]byte(passphrase))
	copy(key[:], sum[:])
	token, err := fernet.EncryptAndSign([]byte(plaintext), &key)
	if err != nil {
		t.Fatal(err)
	}
	return encryptedPrefix + string(token)
}

func newOld(t *testing.T, statements ...string) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "old.db")
	writer, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range append([]string{currentSchema}, statements...) {
		if _, err := writer.Exec(statement); err != nil {
			t.Fatalf("%v in %s", err, statement)
		}
	}
	writer.Close()
	return openOld(t, path)
}

func TestAFullInstall(t *testing.T) {
	relayID := oldProjectID("custom:relay", "Relay main")
	deepseekID := oldProjectID("deepseek", "deepseek")
	db := newOld(t,
		`INSERT INTO custom_provider_config (name, display_name, url, auth_mode, auth_param, default_type, currency, balance_path, balance_scale, success_path, success_values, error_path, subtract_path, extra_params) VALUES
			('deepseek', 'DeepSeek', 'https://api.deepseek.com/user/balance', 'bearer', '', 'balance', 'CNY', 'balance_infos.{currency:CNY}.total_balance', NULL, '', '', '', '', ''),
			('relay', 'My Relay', 'https://relay.example/v1/usage', 'header', 'X-API-Key', 'credits', 'USD', 'data.accounts.{currency:USD}.granted | data.total', 0.01, 'code', '0,200', 'msg', 'data.used', 'unit=cents,v=2'),
			('glm-custom', 'GLM again', 'https://glm.example/quota', 'bearer', '', 'quota', '', 'data.limits', NULL, '', '', '', '', '')`,
		`INSERT INTO custom_provider_config (name, url, auth_mode, default_type, balance_path, extract_mode) VALUES ('window', 'https://w.example/q', 'bearer', 'quota', 'data.limits', 'min_window')`,
		`INSERT INTO project_config (name, owner_project, provider, api_key, threshold, type, currency, monthly_budget, enabled) VALUES
			('Relay main', 'AI', 'custom:relay', '`+oldSeal(t, "old passphrase", "relay-key-0001")+`', 20, 'credits', 'USD', 300, 1),
			('Sealed elsewhere', '', 'openrouter', '`+oldSeal(t, "another passphrase", "or-key")+`', 5, 'credits', '', 0, 1),
			('From env', '', 'tikhub', '', 100, 'balance', '', 0, 1),
			('Unknown', '', 'acme', 'k', 1, 'balance', '', 0, 1)`,
		`INSERT INTO balance_history (project_id, project_name, provider, balance, timestamp) VALUES
			('`+relayID+`', 'Relay main', 'custom:relay', 80, '2026-11-14 01:00:00'),
			('`+relayID+`', 'Relay main', 'custom:relay', 75.5, '2026-11-15 01:00:00'),
			('`+deepseekID+`', 'deepseek', 'deepseek', 547.67, '2026-11-16 01:41:47.915878')`,
		`INSERT INTO alert_history (project_id, project_name, alert_type, status, message, balance_value, timestamp) VALUES
			('`+deepseekID+`', 'deepseek', 'spend_spike', 'sent', 'Spending spike: today 2.53 is 25.3x the baseline', 2.53, '2026-11-13 07:00:02'),
			('`+deepseekID+`', 'deepseek', 'check_failed', 'sent', 'Balance check failed: HTTP 503', NULL, '2026-11-10 07:00:00'),
			('`+oldSubscriptionID("Parent")+`', 'Parent', 'subscription_renewal', 'sent', 'Subscription reminder: Parent due in 2 days', 0, '2026-10-17 01:00:00'),
			('nobody', 'gone', 'low_balance', 'sent', 'x', 1, '2026-10-01 00:00:00')`,
		`INSERT INTO subscription_config (name, owner_project, cycle_type, renewal_day, alert_days_before, amount, enabled, last_renewed_date, snoozed_until, webhook_url) VALUES
			('Parent', NULL, 'lunar_yearly', 920, 2, 0, 1, NULL, NULL, NULL),
			('春节', NULL, 'lunar_yearly', 101, 2, 0, 1, NULL, NULL, NULL),
			('元旦', NULL, 'yearly', 101, 2, 0, 1, NULL, NULL, NULL),
			('VPS', 'infra', 'monthly', 20, 3, 52, 1, '2026-11-18', '2026-11-30', 'https://hook.example/other'),
			('Weekly sync', NULL, 'weekly', 5, 0, 0, 0, NULL, '2020-01-01', NULL),
			('Long lead', NULL, 'yearly', 615, 30, 1200, 1, NULL, NULL, NULL),
			('Broken', NULL, 'yearly', 9999, 1, 0, 1, NULL, NULL, NULL)`,
		`INSERT INTO usage_events (source, project_name, entry_date, model, cost, currency) VALUES
			('claude-code', 'claude-code', '2026-10-30', 'opus', 4, 'USD'),
			('claude-code', 'claude-code', '2026-10-31', 'opus', 6, 'USD'),
			('claude-code', 'claude-code', '2026-11-01', 'opus', 1.5, 'USD'),
			('claude-code', 'claude-code', '2026-11-01', 'sonnet', 0.5, 'USD'),
			('claude-code', 'claude-code', '2026-11-02', 'opus', 3, 'USD')`,
		`INSERT INTO email_config (name, host, port, username, password) VALUES ('a', 'imap.example.com', 993, 'me@example.com', 'pw'), ('b', 'imap.example.com', 993, 'x@example.com', 'pw')`,
		`INSERT INTO push_subscriptions (endpoint, p256dh, auth) VALUES ('https://push.example/send/device-0001', 'p256dh-0001', 'auth-0001'), ('https://push.example/send/device-0002', 'p256dh-0002', 'auth-0002'), ('https://web.push.apple.com/device-0003', 'p256dh-0003', 'auth-0003'), ('https://fcm.googleapis.com/fcm/send/device-0004', 'p256dh-0004', 'auth-0004'), ('https://fcm.googleapis.com/fcm/send/device-0005', 'p256dh-0005', 'auth-0005')`,
		`INSERT INTO app_settings (setting_key, setting_value) VALUES ('vapid_public_key', 'old-public-key'), ('vapid_private_key', 'old-private-key'), ('something_else', 'x')`,
	)
	env := map[string]string{
		"TZ": "Asia/Shanghai", "CONFIG_ENCRYPTION_KEY": "old passphrase",
		"WEB_API_KEY": "web-key", "WEBHOOK_URL": "https://open.feishu.cn/open-apis/bot/v2/hook/abc", "WEBHOOK_TYPE": "feishu",
		"DEEPSEEK_API_KEY": "sk-deepseek-0001", "DEEPSEEK_THRESHOLD": "50", "DEEPSEEK_OWNER_PROJECT": "AI 平台",
		"GLM_API_KEY": "id.secret", "GLM_THRESHOLD": "10",
		"VOLC_1_API_KEY": "AK1:SK1", "VOLC_2_API_KEY": "AK2:SK2", "VOLC_2_THRESHOLD": "500",
		"TIKHUB_API_KEY": "tikhub-key-0001",
		"XAI_API_KEY":    "xai-key", "XAI_THRESHOLD": "10",
		"FX_RATES":                         "USD=7.16, eur=7.8,junk",
		"BALANCE_REFRESH_INTERVAL_SECONDS": "3600", "RUNWAY_ALERT_DAYS": "5",
		"ALERT_SCHEDULE": "09:30,15:00", "WEEKLY_REPORT_SCHEDULE": "周五 18:00",
		"MCP_API_KEYS": "agent:key-agent-0001:all;hermes:key-hermes-0001:balance,alerts:2099-12-31;old:key-old:all:2020-01-01;precise:key-precise-0001:all:2099-01-02T03:04:05Z",
	}
	out, err := convert(source{db: db, env: env, now: now})
	if err != nil {
		t.Fatal(err)
	}
	notes := notesOf(out)

	// --- settings ---
	s := settingsOf(t, out)
	if s.Timezone != "Asia/Shanghai" || s.Language != "zh" || s.BaseCurrency != "CNY" || s.FX["USD"] != 7.16 || s.FX["EUR"] != 7.8 || len(s.FX) != 2 {
		t.Errorf("settings = %+v", s)
	}
	if s.SampleSeconds != 3600 || s.RunwayDays != 5 || s.ReminderTime != "09:30" || s.Digest.Weekday != 5 || s.Digest.Time != "18:00" || !s.Digest.Enabled {
		t.Errorf("settings = %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Errorf("the converted settings are not valid: %v", err)
	}

	// --- channel and tokens ---
	if len(out.Channels) != 6 || out.Channels[0].Type != "feishu" || out.Channels[0].Name != "飞书" || out.Channels[0].Config["url"] != env["WEBHOOK_URL"] {
		t.Fatalf("channels = %+v", out.Channels)
	}
	// Browsers that had notifications on come along as channels, with the
	// key pair their subscriptions were made for.
	if out.Push == nil || out.Push.Public != "old-public-key" || out.Push.Private != "old-private-key" {
		t.Errorf("push keys = %+v", out.Push)
	}
	if browser := out.Channels[2]; browser.Type != "webpush" || browser.Name != "浏览器 2" || browser.Config["endpoint"] != "https://push.example/send/device-0002" || browser.Config["auth"] != "auth-0002" {
		t.Errorf("browser channel = %+v", browser)
	}
	// The push service is the only clue to what a device is; use it.
	if names := []string{out.Channels[1].Name, out.Channels[3].Name, out.Channels[4].Name, out.Channels[5].Name}; strings.Join(names, ", ") != "浏览器, Safari, Chrome, Chrome 2" {
		t.Errorf("browser names = %v", names)
	}
	if len(out.Tokens) != 3 {
		t.Fatalf("tokens = %+v", out.Tokens)
	}
	if out.Tokens[0].Name != "agent" || out.Tokens[0].Token != "key-agent-0001" || out.Tokens[0].Scopes[0] != core.ScopeRead || out.Tokens[0].ExpiresAt != nil {
		t.Errorf("token = %+v", out.Tokens[0])
	}
	// A date expiry was valid through its last day.
	if got := out.Tokens[1].ExpiresAt.Format(time.RFC3339); got != "2100-01-01T00:00:00Z" {
		t.Errorf("hermes expires %s", got)
	}
	if got := out.Tokens[2].ExpiresAt.Format(time.RFC3339); out.Tokens[2].Name != "precise" || got != "2099-01-02T03:04:05Z" {
		t.Errorf("precise = %+v", out.Tokens[2])
	}
	for _, want := range []string{`MCP key "old" had expired`, "3 MCP key(s) keep working", "WEB_API_KEY is replaced by QUOTAPULSE_PASSWORD"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q", want)
		}
	}

	// --- accounts ---
	accounts := map[string]bundle.Account{}
	for _, a := range out.Accounts {
		accounts[a.Name] = a
	}
	if len(accounts) != 7 {
		t.Fatalf("accounts = %v", keysOf(accounts))
	}
	// A key sealed with the old passphrase is opened.
	if a := accounts["Relay main"]; a.Provider != "relay" || a.Credential["api_key"] != "relay-key-0001" || a.Group != "AI" {
		t.Errorf("relay = %+v", a)
	}
	// One sealed with something else cannot be, and says so instead of carrying garbage.
	if a := accounts["Sealed elsewhere"]; len(a.Credential) != 0 || !strings.Contains(notes, `account "Sealed elsewhere": its key is encrypted`) {
		t.Errorf("sealed = %+v", a)
	}
	// An empty key in the database meant "the one in the environment".
	if a := accounts["From env"]; a.Provider != "tikhub" || a.Credential["api_key"] != "tikhub-key-0001" {
		t.Errorf("from env = %+v", a)
	}
	// Discovered from the environment alone, as the old version did.
	if a := accounts["deepseek"]; a.Provider != "deepseek" || a.Credential["api_key"] != "sk-deepseek-0001" || a.Group != "AI 平台" || !a.Enabled {
		t.Errorf("deepseek = %+v", a)
	}
	if a := accounts["glm"]; a.Provider != "glm-coding" {
		t.Errorf("glm = %+v", a)
	}
	if a, b := accounts["volc-1"], accounts["volc-2"]; a.Credential["access_key_id"] != "AK1" || b.Credential["secret_access_key"] != "SK2" || a.Provider != "volcengine" {
		t.Errorf("volc = %+v %+v", a, b)
	}
	// tikhub is declared in the database, so its env key does not add a second account.
	if _, twice := accounts["tikhub"]; twice {
		t.Error("an account was both declared and discovered")
	}
	for _, want := range []string{
		`account "Unknown" used the platform "acme"`,
		"XAI_API_KEY is set, but the old version had no platform by that name",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q", want)
		}
	}
	if strings.Contains(notes, "XAI_THRESHOLD") || strings.Contains(notes, "WEB_API_KEY is set, but") || strings.Contains(notes, "TIKHUB_API_KEY is set") {
		t.Errorf("a key that is accounted for was reported as an orphan:\n%s", notes)
	}

	// --- meters ---
	meters := map[string]bundle.Meter{}
	for _, m := range out.Meters {
		meters[m.Account+"/"+m.Key] = m
	}
	if m := meters["Relay main/main"]; m.Unit != "USD" || *m.Rules.Low != 20 || *m.Rules.Budget != 300 || len(m.Readings) != 2 || m.Readings[1][1] != 75.5 {
		t.Errorf("relay meter = %+v", m)
	}
	if m := meters["deepseek/balance-cny"]; *m.Rules.Low != 50 || len(m.Readings) != 1 || m.Readings[0][1] != 547.67 {
		t.Errorf("deepseek meter = %+v", m)
	}
	if m := meters["glm/tokens-5h"]; m.Kind != core.Quota || m.Unit != "%" || *m.Rules.Low != 10 || m.Name != "5 小时窗口" {
		t.Errorf("glm meter = %+v", m)
	}
	if m := meters["volc-2/balance"]; *m.Rules.Low != 500 {
		t.Errorf("volc-2 meter = %+v", m)
	}
	if m := meters["volc-1/balance"]; m.Rules.Low != nil {
		t.Errorf("no threshold was set for volc-1: %+v", m.Rules)
	}

	// Reported usage: one row per day and model becomes a counter that
	// starts over each month.
	usage := meters["/claude-code"]
	if usage.Kind != core.Counter || usage.Unit != "USD" {
		t.Fatalf("usage meter = %+v", usage)
	}
	var values []float64
	for _, r := range usage.Readings {
		values = append(values, r[1])
	}
	if got, _ := json.Marshal(values); string(got) != "[0,4,10,0,2,5]" {
		t.Errorf("usage readings = %s", got)
	}
	// End of October 30th in Shanghai.
	if at := time.Unix(int64(usage.Readings[1][0]), 0).UTC().Format(time.RFC3339); at != "2026-10-30T15:59:59Z" {
		t.Errorf("the day's total is stamped %s", at)
	}

	// --- custom providers ---
	if len(out.Providers) != 3 {
		t.Fatalf("providers = %+v", out.Providers)
	}
	definitions := map[string]provider.Definition{}
	for _, def := range out.Providers {
		definitions[def.ID] = def
		if err := def.Validate(); err != nil {
			t.Errorf("%s: %v", def.ID, err)
		}
	}
	if _, seededRow := definitions["deepseek"]; seededRow {
		t.Error("a definition the old version seeded was converted; 1.0 has it built in")
	}
	relay := definitions["relay"]
	if relay.Request.Auth.Type != provider.AuthHeader || relay.Request.Auth.Name != "X-API-Key" || relay.Request.Query["unit"] != "cents" || relay.Request.Query["v"] != "2" {
		t.Errorf("relay request = %+v", relay.Request)
	}
	extract := func(def provider.Definition, body string) ([]provider.Sample, error) {
		var parsed any
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			t.Fatal(err)
		}
		return provider.Extract(ctx, def.Extract, parsed, nil, now)
	}
	// The same answers the old path engine gave: the USD account's grant
	// minus what was used, scaled from cents.
	samples, err := extract(relay, `{"code":0,"data":{"accounts":[{"currency":"EUR","granted":1},{"currency":"USD","granted":"12,500"}],"used":2500}}`)
	if err != nil || len(samples) != 1 || samples[0].Value != 100 || samples[0].Unit != "USD" || samples[0].Kind != core.Balance {
		t.Fatalf("relay extract = %+v, %v", samples, err)
	}
	// No USD account: the first one stands in, as it used to.
	samples, err = extract(relay, `{"code":200,"data":{"accounts":[{"currency":"EUR","granted":900}],"used":"100"}}`)
	if err != nil || samples[0].Value != 8 {
		t.Fatalf("relay fallback = %+v, %v", samples, err)
	}
	// The second path is tried when the first is absent.
	samples, err = extract(relay, `{"code":0,"data":{"total":5000,"used":0}}`)
	if err != nil || samples[0].Value != 50 {
		t.Fatalf("relay alternative = %+v, %v", samples, err)
	}
	// A code outside the accepted list fails with the platform's own message.
	if _, err := extract(relay, `{"code":401,"msg":"bad key","data":{}}`); err == nil || err.Error() != "bad key" {
		t.Fatalf("relay rejection = %v", err)
	}
	// The window mode reads the quota windows as 1.0's own definition does.
	samples, err = extract(definitions["window"], `{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":47,"nextResetTime":1794798055318}]}}`)
	if err != nil || samples[0].Key != "tokens-5h" || samples[0].Value != 53 {
		t.Fatalf("window extract = %+v, %v", samples, err)
	}

	// --- dates ---
	reminders := map[string]bundle.Reminder{}
	for _, r := range out.Reminders {
		reminders[r.Title] = r
	}
	if len(reminders) != 6 || !strings.Contains(notes, `reminder "Broken": its date could not be understood`) {
		t.Fatalf("reminders = %v", keysOf(reminders))
	}
	if r := reminders["Parent"]; r.Kind != core.ReminderBirthday || r.Rule.Calendar != recur.LunarCal || r.Rule.Month != 9 || r.Rule.Day != 20 || len(r.LeadDays) != 3 {
		t.Errorf("a lunar yearly date with no price is a birthday: %+v", r)
	}
	if reminders["春节"].Kind != core.ReminderHoliday || reminders["元旦"].Kind != core.ReminderHoliday || reminders["元旦"].Rule.Calendar != "" {
		t.Errorf("holidays = %+v %+v", reminders["春节"], reminders["元旦"])
	}
	vps := reminders["VPS"]
	if vps.Kind != core.ReminderRenewal || *vps.Amount != 52 || vps.Currency != "CNY" || vps.Group != "infra" || vps.Rule.Day != 20 {
		t.Errorf("vps = %+v", vps)
	}
	// Marked as renewed on the 18th, two days before the 20th: that occurrence is done.
	if vps.DoneThrough == nil || vps.DoneThrough.String() != "2026-11-20" || vps.SnoozedUntil.String() != "2026-11-30" {
		t.Errorf("vps marks = %v %v", vps.DoneThrough, vps.SnoozedUntil)
	}
	if weekly := reminders["Weekly sync"]; weekly.Enabled || weekly.Rule.Weekday != 5 || weekly.SnoozedUntil != nil || weekly.Kind != core.ReminderCustom {
		t.Errorf("weekly = %+v", weekly)
	}
	// "Thirty days of daily reminders" becomes a handful of well-placed ones.
	if got, _ := json.Marshal(reminders["Long lead"].LeadDays); string(got) != "[30,7,3,1,0]" {
		t.Errorf("long lead = %s", got)
	}
	if !strings.Contains(notes, "1 reminder(s) had a webhook of their own") {
		t.Errorf("notes lack the webhook override:\n%s", notes)
	}

	// --- history ---
	if len(out.Incidents) != 3 {
		t.Fatalf("incidents = %+v", out.Incidents)
	}
	byKind := map[string]bundle.Incident{}
	for _, inc := range out.Incidents {
		byKind[inc.Kind] = inc
	}
	if inc := byKind[core.IncidentLevel]; inc.Subject != "deepseek/balance-cny" || inc.Reason != core.ReasonBurst || inc.Data["imported"] != "Spending spike: today 2.53 is 25.3x the baseline" {
		t.Errorf("spike = %+v", inc)
	}
	if inc := byKind[core.IncidentFailing]; inc.SubjectType != core.SubjectAccount || inc.Subject != "deepseek" {
		t.Errorf("failing = %+v", inc)
	}
	if inc := byKind[core.IncidentDue]; inc.SubjectType != core.SubjectReminder || inc.Subject != "Parent" {
		t.Errorf("due = %+v", inc)
	}
	for _, want := range []string{"2 mailbox(es)", "5 browser(s) that had notifications turned on keep receiving them", "reported usage is now a pushed counter"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q", want)
		}
	}

	// --- and into 1.0 it goes ---
	e, report := importInto(t, out)
	if report.Created["accounts"] != 7 || report.Created["meters"] != 8 || report.Created["reminders"] != 6 || report.Created["tokens"] != 3 ||
		report.Created["providers"] != 3 || report.Created["channels"] != 6 || report.Created["incidents"] != 3 {
		t.Fatalf("import created %v\n%s", report.Created, strings.Join(report.Notes, "\n"))
	}
	if _, ok := e.Authenticate(ctx, "key-hermes-0001"); !ok {
		t.Error("a migrated MCP key does not authenticate")
	}
	all, err := e.Accounts(ctx, "zh")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range all {
		// Only the account whose key could not be opened comes in switched off.
		if a.Enabled == (a.Name == "Sealed elsewhere") {
			t.Errorf("account %q enabled = %v", a.Name, a.Enabled)
		}
	}
	dates, _ := e.Reminders(ctx, "zh")
	for _, d := range dates {
		if d.Title == "Parent" && (d.Lunar == nil || d.Lunar.Label != "九月二十" || d.Next.String() != "2027-10-19") {
			t.Errorf("Parent = %+v", d)
		}
		if d.Title == "VPS" && d.Next.String() != "2026-12-20" {
			t.Errorf("VPS is done for November, so next is %s", d.Next)
		}
	}
}

func keysOf[V any](m map[string]V) []string {
	var keys []string
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

func TestEnvironmentOnly(t *testing.T) {
	out, err := convert(source{env: map[string]string{"DEEPSEEK_API_KEY": "sk-0001", "WEBHOOK_URL": "https://hook.example/x"}, now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Accounts) != 1 || out.Accounts[0].Name != "deepseek" || out.Meters[0].Rules.Low != nil {
		t.Fatalf("accounts = %+v, meters = %+v", out.Accounts, out.Meters)
	}
	// No TZ meant the image's default, and no WEBHOOK_TYPE meant the generic payload.
	if s := settingsOf(t, out); s.Timezone != "Asia/Shanghai" || out.Channels[0].Type != "webhook" {
		t.Errorf("settings = %+v, channel = %+v", s, out.Channels[0])
	}
	notes := notesOf(out)
	if !strings.Contains(notes, "no database was read") || !strings.Contains(notes, "different JSON body") {
		t.Errorf("notes:\n%s", notes)
	}

	empty, err := convert(source{env: map[string]string{"TZ": "Europe/Berlin"}, now: now})
	if err != nil {
		t.Fatal(err)
	}
	if s := settingsOf(t, empty); s.Language != "en" || len(empty.Channels) != 0 || !strings.Contains(notesOf(empty), "WEBHOOK_URL was not set") {
		t.Errorf("empty = %+v\n%s", s, notesOf(empty))
	}
}

func TestOldEncryptionKeyRules(t *testing.T) {
	// A setting that is itself a Fernet key is used as the key…
	var key fernet.Key
	if err := key.Generate(); err != nil {
		t.Fatal(err)
	}
	token, _ := fernet.EncryptAndSign([]byte("secret"), &key)
	if got, ok := newDecrypter(key.Encode()).open(encryptedPrefix + string(token)); !ok || got != "secret" {
		t.Errorf("a Fernet key setting: %q %v", got, ok)
	}
	// …anything else is a passphrase.
	if got, ok := newDecrypter("old passphrase").open(oldSeal(t, "old passphrase", "secret")); !ok || got != "secret" {
		t.Errorf("a passphrase setting: %q %v", got, ok)
	}
	// 43 characters without padding look like a key but were always treated as a passphrase.
	unpadded := strings.TrimRight(key.Encode(), "=")
	if got, ok := newDecrypter(unpadded).open(oldSeal(t, unpadded, "secret")); !ok || got != "secret" {
		t.Errorf("an unpadded key is a passphrase: %q %v", got, ok)
	}
	// Plaintext passes through; a sealed value without a key does not.
	if got, ok := newDecrypter("").open("plain"); !ok || got != "plain" {
		t.Errorf("plaintext: %q %v", got, ok)
	}
	if _, ok := newDecrypter("").open(oldSeal(t, "x", "secret")); ok {
		t.Error("a sealed value opened without a key")
	}
	if _, ok := newDecrypter("wrong").open(oldSeal(t, "right", "secret")); ok {
		t.Error("a sealed value opened with the wrong key")
	}
}

func TestOldIdentifiers(t *testing.T) {
	// The values the old version computed for these names.
	if got := oldProjectID("deepseek", "deepseek"); got != "583276a5396571565636ca419969f306" {
		t.Errorf("project id = %s", got)
	}
	if oldProjectID("custom:deepseek", "deepseek") != oldProjectID("deepseek", "deepseek") {
		t.Error("the custom: prefix must not change the id")
	}
	if got := oldProjectID("volc", "火山-主账号"); got != "1d9d43a3c2da630e840af8d17cc4c1da" {
		t.Errorf("project id = %s", got)
	}
}

func TestDatabaseURL(t *testing.T) {
	if _, _, err := openDatabase("mongodb://x"); err == nil {
		t.Error("an unsupported database was accepted")
	}
	if _, _, err := openDatabase("no scheme"); err == nil {
		t.Error("a URL without a scheme was accepted")
	}
	if _, _, err := openDatabase("sqlite:///./does/not/exist.db"); err == nil {
		t.Error("a missing SQLite file was accepted")
	}
	// The old URL form: a "+driver" after the scheme, and a # in the
	// password that net/url would read as a fragment.
	db, engine, err := openDatabase("mysql+pymysql://app:p%40ss#word@10.0.0.5:3307/balance_alert?charset=utf8mb4")
	if err != nil || engine != "mysql" {
		t.Fatalf("%v %v", engine, err)
	}
	db.Close()
	m := connURL.FindStringSubmatch("app:p%40ss#word@10.0.0.5:3307/balance_alert?charset=utf8mb4")
	group := func(name string) string { return m[connURL.SubexpIndex(name)] }
	if unescape(group("pass")) != "p@ss#word" || group("host") != "10.0.0.5" || group("port") != "3307" || group("db") != "balance_alert" {
		t.Errorf("parsed = %v", m)
	}
	db, engine, err = openDatabase("postgresql+psycopg2://app:secret@db.internal/quotapulse?sslmode=disable")
	if err != nil || engine != "postgres" {
		t.Fatalf("%v %v", engine, err)
	}
	db.Close()
}

func TestEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# a comment\n\nexport A=1\nB = two words \nC=\"quoted # not a comment\"\nD='single'\nE=has=equals\nF=\n"
	if err := writeFile(path, content); err != nil {
		t.Fatal(err)
	}
	env, err := loadEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "1", "B": "two words", "C": "quoted # not a comment", "D": "single", "E": "has=equals", "F": ""}
	for key, value := range want {
		if env[key] != value {
			t.Errorf("%s = %q, want %q", key, env[key], value)
		}
	}
	if err := writeFile(path, "not a pair\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadEnvFile(path); err == nil {
		t.Error("a malformed line was accepted")
	}
}

package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"quotapulse/internal/core"
	"quotapulse/internal/provider"
	"quotapulse/internal/recur"
	"quotapulse/internal/secret"
)

var (
	ctx = context.Background()
	t0  = time.Date(2026, 11, 16, 2, 0, 0, 0, time.UTC)
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "quotapulse.db"), secret.FromPassphrase("test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newAccount(t *testing.T, s *Store, name string) core.Account {
	t.Helper()
	a := core.Account{Name: name, Provider: "deepseek", Credential: map[string]string{"api_key": "sk-test-0001"}, Enabled: true}
	must(t, s.CreateAccount(ctx, &a, t0))
	return a
}

func TestOpenIsRepeatableAndRefusesANewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "quotapulse.db")
	keys := secret.FromPassphrase("test")
	s, err := Open(path, keys)
	must(t, err)
	must(t, s.SetState(ctx, "marker", "kept"))
	must(t, s.Close())

	s, err = Open(path, keys)
	must(t, err)
	if got, ok, _ := s.State(ctx, "marker"); !ok || got != "kept" {
		t.Fatalf("state after reopen = %q, %v", got, ok)
	}
	_, err = s.db.Exec("PRAGMA user_version = 999")
	must(t, err)
	must(t, s.Close())

	// A database written by a newer build must not be opened and half understood.
	if _, err := Open(path, keys); err == nil {
		t.Fatal("a newer schema was opened")
	}
}

func TestSettings(t *testing.T) {
	s := open(t)
	got, err := s.Settings(ctx)
	must(t, err)
	if got.SampleSeconds != 1800 || got.ReminderTime != "09:00" || got.FX == nil {
		t.Fatalf("defaults = %+v", got)
	}
	got.Timezone, got.Language, got.BaseCurrency = "Asia/Shanghai", "zh", "CNY"
	got.FX = map[string]float64{"USD": 7.16}
	must(t, s.SaveSettings(ctx, got))
	back, err := s.Settings(ctx)
	must(t, err)
	if back.Timezone != "Asia/Shanghai" || back.FX["USD"] != 7.16 || back.Location().String() != "Asia/Shanghai" {
		t.Fatalf("saved = %+v", back)
	}
	bad := back
	bad.Timezone, bad.SampleSeconds = "Mars/Olympus", 5
	if err := s.SaveSettings(ctx, bad); err == nil {
		t.Fatal("invalid settings were saved")
	}
}

func TestAccountsAreSealedAndUnique(t *testing.T) {
	s := open(t)
	a := newAccount(t, s, "DeepSeek")
	if a.ID == "" || a.Status != core.StatusPending {
		t.Fatalf("created = %+v", a)
	}

	var raw []byte
	must(t, s.db.QueryRow("SELECT credential FROM accounts WHERE id = ?", a.ID).Scan(&raw))
	if bytes.Contains(raw, []byte("sk-test")) {
		t.Fatalf("the credential is stored in the clear: %s", raw)
	}

	dup := core.Account{Name: "deepseek", Provider: "deepseek", Credential: map[string]string{}}
	if err := s.CreateAccount(ctx, &dup, t0); !errors.Is(err, ErrConflict) {
		t.Fatalf("names differ only by case: %v", err)
	}

	got, err := s.Account(ctx, a.ID)
	must(t, err)
	if got.Credential["api_key"] != "sk-test-0001" || got.Unreadable {
		t.Fatalf("loaded = %+v", got)
	}
	got.Name, got.Group, got.Interval, got.Enabled = "DeepSeek main", "AI", 600, false
	got.Credential["api_key"] = "sk-test-0002"
	must(t, s.UpdateAccount(ctx, got, t0.Add(time.Minute)))
	back, _ := s.AccountByName(ctx, "deepseek MAIN")
	if back.Group != "AI" || back.Interval != 600 || back.Enabled || back.Credential["api_key"] != "sk-test-0002" {
		t.Fatalf("updated = %+v", back)
	}
	if !back.UpdatedAt.After(back.CreatedAt) {
		t.Error("updated_at did not move")
	}

	if _, err := s.Account(ctx, "acc_missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing account: %v", err)
	}
	if err := s.UpdateAccount(ctx, core.Account{ID: "acc_missing", Credential: map[string]string{}}, t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("update missing: %v", err)
	}
}

func TestCredentialSealedWithAnotherSecretIsFlaggedNotFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quotapulse.db")
	s, err := Open(path, secret.FromPassphrase("first"))
	must(t, err)
	a := core.Account{Name: "A", Provider: "deepseek", Credential: map[string]string{"api_key": "sk-test-0001"}, Enabled: true}
	must(t, s.CreateAccount(ctx, &a, t0))
	must(t, s.Close())

	s, err = Open(path, secret.FromPassphrase("second"))
	must(t, err)
	defer s.Close()
	accounts, err := s.Accounts(ctx)
	must(t, err)
	if len(accounts) != 1 || !accounts[0].Unreadable || len(accounts[0].Credential) != 0 {
		t.Fatalf("accounts = %+v", accounts)
	}
}

func sample(key string, value float64) provider.Sample {
	return provider.Sample{Key: key, Name: "Balance", NameZH: "余额", Kind: core.Balance, Unit: "CNY", Value: value}
}

func TestSaveOutcome(t *testing.T) {
	s := open(t)
	a := newAccount(t, s, "DeepSeek")

	due, err := s.DueAccounts(ctx, t0)
	must(t, err)
	if len(due) != 1 {
		t.Fatalf("a new account is due at once: %+v", due)
	}

	window := int64(18000)
	reset := t0.Add(3 * time.Hour)
	meters, err := s.SaveOutcome(ctx, a.ID, Outcome{
		At: t0, NextAt: t0.Add(30 * time.Minute), Latency: 439 * time.Millisecond, Language: "zh",
		Samples: []provider.Sample{
			sample("balance-cny", 547.67),
			{Key: "tokens-5h", Name: "5-hour window", Kind: core.Quota, Unit: "%", Value: 99, Limit: core.Ptr(100.0), ResetsAt: &reset, Window: &window},
		},
	})
	must(t, err)
	if len(meters) != 2 || meters[0].Name != "余额" || meters[1].Name != "5-hour window" {
		t.Fatalf("meters = %+v", meters)
	}
	if *meters[1].Limit != 100 || *meters[1].Window != 18000 || !meters[1].ResetsAt.Equal(reset) {
		t.Errorf("quota meter = %+v", meters[1])
	}

	got, _ := s.Account(ctx, a.ID)
	if got.Status != core.StatusOK || got.Failures != 0 || *got.LatencyMs != 439 || !got.NextAt.Equal(t0.Add(30*time.Minute)) {
		t.Fatalf("account = %+v", got)
	}
	if due, _ := s.DueAccounts(ctx, t0.Add(29*time.Minute)); len(due) != 0 {
		t.Errorf("not due yet: %+v", due)
	}
	if due, _ := s.DueAccounts(ctx, t0.Add(30*time.Minute)); len(due) != 1 {
		t.Errorf("due now: %+v", due)
	}

	// A second sample reuses the meters, and keeps a name the user changed.
	balance := meters[0]
	balance.Name = "主账户"
	must(t, s.UpdateMeter(ctx, balance, t0))
	meters, err = s.SaveOutcome(ctx, a.ID, Outcome{
		At: t0.Add(30 * time.Minute), NextAt: t0.Add(time.Hour),
		Samples: []provider.Sample{sample("balance-cny", 540)},
	})
	must(t, err)
	if len(meters) != 1 || meters[0].ID != balance.ID || meters[0].Name != "主账户" || *meters[0].Value != 540 {
		t.Fatalf("second sample = %+v", meters)
	}
	if all, _ := s.MetersOf(ctx, a.ID); len(all) != 2 {
		t.Errorf("meters of the account = %+v", all)
	}

	// Failures count up and keep the last good time.
	for i := 1; i <= 2; i++ {
		_, err = s.SaveOutcome(ctx, a.ID, Outcome{At: t0.Add(time.Duration(i) * time.Hour), NextAt: t0.Add(2 * time.Hour), Err: "HTTP 401: bad key"})
		must(t, err)
	}
	got, _ = s.Account(ctx, a.ID)
	if got.Status != core.StatusFailing || got.Failures != 2 || got.LastError != "HTTP 401: bad key" {
		t.Fatalf("failing account = %+v", got)
	}
	if !got.SucceededAt.Equal(t0.Add(30 * time.Minute)) {
		t.Errorf("succeeded_at moved on a failure: %v", got.SucceededAt)
	}

	// Disabled accounts are never due.
	got.Enabled = false
	must(t, s.UpdateAccount(ctx, got, t0))
	if due, _ := s.DueAccounts(ctx, t0.Add(24*time.Hour)); len(due) != 0 {
		t.Errorf("a disabled account was due: %+v", due)
	}

	if _, err := s.SaveOutcome(ctx, "acc_missing", Outcome{At: t0, NextAt: t0}); !errors.Is(err, ErrNotFound) {
		t.Errorf("outcome for a missing account: %v", err)
	}
}

func pushed(t *testing.T, s *Store, key string) core.Meter {
	t.Helper()
	m := core.Meter{Key: key, Name: key, Kind: core.Balance, Unit: "USD"}
	must(t, s.CreateMeter(ctx, &m, t0))
	return m
}

func rows(t *testing.T, s *Store, id string) []core.Reading {
	t.Helper()
	readings, err := s.Readings(ctx, id, time.Unix(0, 0))
	must(t, err)
	return readings
}

func TestFlatRunsAreStoredAsTwoRows(t *testing.T) {
	s := open(t)
	m := pushed(t, s, "flat")
	at := func(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }
	record := func(h int, value float64) {
		t.Helper()
		must(t, s.Record(ctx, &m, core.Reading{At: at(h), Value: value}, nil))
	}

	for h := 0; h < 10; h++ {
		record(h, 100)
	}
	got := rows(t, s, m.ID)
	if len(got) != 2 || !got[0].At.Equal(at(0)) || !got[1].At.Equal(at(9)) {
		t.Fatalf("ten identical samples should be two rows, got %+v", got)
	}

	// A change starts a new run; the run before it keeps both of its ends,
	// so the chart shows exactly when the drop happened.
	record(10, 90)
	record(11, 90)
	record(12, 90)
	record(13, 80)
	got = rows(t, s, m.ID)
	want := []struct {
		h int
		v float64
	}{{0, 100}, {9, 100}, {10, 90}, {12, 90}, {13, 80}}
	if len(got) != len(want) {
		t.Fatalf("rows = %+v", got)
	}
	for i, w := range want {
		if !got[i].At.Equal(at(w.h)) || got[i].Value != w.v {
			t.Errorf("row %d = %+v, want hour %d value %v", i, got[i], w.h, w.v)
		}
	}
	if *m.Value != 80 || !m.SampledAt.Equal(at(13)) {
		t.Errorf("meter = %+v", m)
	}
	stored, _ := s.Meter(ctx, m.ID)
	if *stored.Value != 80 || !stored.SampledAt.Equal(at(13)) {
		t.Errorf("stored meter = %+v", stored)
	}
}

func TestALimitChangeBreaksARun(t *testing.T) {
	s := open(t)
	m := pushed(t, s, "quota")
	for i, limit := range []float64{100, 100, 100, 200, 200} {
		must(t, s.Record(ctx, &m, core.Reading{At: t0.Add(time.Duration(i) * time.Hour), Value: 50, Limit: &limit}, nil))
	}
	// 100,100,100 folds to two rows; the limit change starts over.
	if got := rows(t, s, m.ID); len(got) != 4 || *got[1].Limit != 100 || *got[2].Limit != 200 {
		t.Fatalf("rows = %+v", got)
	}
}

func TestALateReadingDoesNotBecomeCurrent(t *testing.T) {
	s := open(t)
	m := pushed(t, s, "late")
	must(t, s.Record(ctx, &m, core.Reading{At: t0, Value: 50}, nil))
	must(t, s.Record(ctx, &m, core.Reading{At: t0.Add(-time.Hour), Value: 70}, nil))
	if *m.Value != 50 || !m.SampledAt.Equal(t0) {
		t.Fatalf("meter = %+v", m)
	}
	if got := rows(t, s, m.ID); len(got) != 2 || got[0].Value != 70 {
		t.Fatalf("history = %+v", got)
	}
	// The same second twice replaces, not duplicates.
	must(t, s.Record(ctx, &m, core.Reading{At: t0, Value: 49}, nil))
	if got := rows(t, s, m.ID); len(got) != 2 || got[1].Value != 49 {
		t.Fatalf("history = %+v", got)
	}
}

func TestReadingsIncludeTheOneBeforeTheWindow(t *testing.T) {
	s := open(t)
	m := pushed(t, s, "window")
	for h := 0; h < 6; h++ {
		must(t, s.Record(ctx, &m, core.Reading{At: t0.Add(time.Duration(h) * time.Hour), Value: float64(100 - h)}, nil))
	}
	got, err := s.Readings(ctx, m.ID, t0.Add(150*time.Minute))
	must(t, err)
	if len(got) != 4 || got[0].Value != 98 {
		t.Fatalf("want the reading before the window plus three inside, got %+v", got)
	}
	exact, _ := s.Readings(ctx, m.ID, t0.Add(3*time.Hour))
	if len(exact) != 4 || exact[0].Value != 98 {
		t.Fatalf("a reading exactly on the boundary still needs its predecessor: %+v", exact)
	}
	all, _ := s.Readings(ctx, m.ID, t0.Add(-time.Hour))
	if len(all) != 6 {
		t.Fatalf("nothing before the window: %+v", all)
	}
	bounded, _ := s.ReadingsBetween(ctx, m.ID, t0, t0.Add(2*time.Hour))
	if len(bounded) != 3 {
		t.Fatalf("bounded = %+v", bounded)
	}
	if n, _ := s.ReadingCount(ctx, m.ID); n != 6 {
		t.Errorf("count = %d", n)
	}
}

func TestMeterKeysAndCascade(t *testing.T) {
	s := open(t)
	pushed(t, s, "claude-code")
	dup := core.Meter{Key: "claude-code", Name: "x", Kind: core.Counter}
	if err := s.CreateMeter(ctx, &dup, t0); !errors.Is(err, ErrConflict) {
		t.Fatalf("two pushed meters with one key: %v", err)
	}
	if got, err := s.PushedMeter(ctx, "claude-code"); err != nil || got.AccountID != "" {
		t.Fatalf("pushed meter = %+v, %v", got, err)
	}

	// The same key under two accounts is fine: keys are per account.
	a, b := newAccount(t, s, "A"), newAccount(t, s, "B")
	for _, acc := range []core.Account{a, b} {
		_, err := s.SaveOutcome(ctx, acc.ID, Outcome{At: t0, NextAt: t0, Samples: []provider.Sample{sample("balance", 1)}})
		must(t, err)
	}
	if got, err := s.MeterByKey(ctx, a.ID, "balance"); err != nil || got.AccountID != a.ID {
		t.Fatalf("by key = %+v, %v", got, err)
	}

	rules := core.Rules{Low: core.Ptr(50.0), RunwayDays: core.Ptr(0.0), Anomaly: core.Ptr(false), Budget: core.Ptr(300.0), Pace: true}
	m, _ := s.MeterByKey(ctx, a.ID, "balance")
	m.Rules, m.MutedUntil, m.Hidden, m.Sort = rules, core.Ptr(t0.Add(time.Hour)), true, 7
	must(t, s.UpdateMeter(ctx, m, t0))
	back, _ := s.Meter(ctx, m.ID)
	if *back.Rules.Low != 50 || *back.Rules.RunwayDays != 0 || *back.Rules.Anomaly || *back.Rules.Budget != 300 || !back.Rules.Pace {
		t.Fatalf("rules = %+v", back.Rules)
	}
	if !back.Hidden || back.Sort != 7 || !back.MutedUntil.Equal(t0.Add(time.Hour)) {
		t.Fatalf("meter = %+v", back)
	}

	must(t, s.DeleteAccount(ctx, a.ID))
	if _, err := s.Meter(ctx, m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the meter outlived its account: %v", err)
	}
	if n, _ := s.ReadingCount(ctx, m.ID); n != 0 {
		t.Fatalf("readings outlived their meter: %d", n)
	}
	if all, _ := s.Meters(ctx); len(all) != 2 {
		t.Fatalf("other meters must stay: %+v", all)
	}
}

func TestReminders(t *testing.T) {
	s := open(t)
	r := core.Reminder{
		Title: "Domain", Kind: core.ReminderRenewal,
		Rule:     recur.Rule{Freq: recur.Yearly, Calendar: recur.LunarCal, Month: 12, Day: 30, Since: 1990},
		LeadDays: []int{7, 1, 0}, Amount: core.Ptr(68.5), Currency: "CNY", Group: "infra", Notes: "auto-renew is off",
		DoneThrough: core.Ptr(recur.NewDate(2026, 2, 16)), Enabled: true,
	}
	must(t, s.CreateReminder(ctx, &r, t0))
	got, err := s.Reminder(ctx, r.ID)
	must(t, err)
	if got.Rule != r.Rule || len(got.LeadDays) != 3 || *got.Amount != 68.5 || got.DoneThrough.String() != "2026-02-16" || got.SnoozedUntil != nil {
		t.Fatalf("loaded = %+v", got)
	}
	got.DoneThrough, got.SnoozedUntil, got.Amount, got.Enabled = nil, core.Ptr(recur.NewDate(2026, 12, 1)), nil, false
	must(t, s.UpdateReminder(ctx, got, t0))
	back, _ := s.Reminder(ctx, r.ID)
	if back.DoneThrough != nil || back.SnoozedUntil.String() != "2026-12-01" || back.Amount != nil || back.Enabled {
		t.Fatalf("updated = %+v", back)
	}
	if all, _ := s.Reminders(ctx); len(all) != 1 {
		t.Fatalf("list = %+v", all)
	}
	must(t, s.DeleteReminder(ctx, r.ID))
	if err := s.DeleteReminder(ctx, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func incident(id, subject, kind, dedup string, opened time.Time) core.Incident {
	subjectType := core.SubjectMeter
	if kind == core.IncidentDue {
		subjectType = core.SubjectReminder
	}
	return core.Incident{
		ID: id, SubjectType: subjectType, SubjectID: subject, Kind: kind, Dedup: dedup,
		Severity: core.Warning, State: core.StateOpen, Reason: core.ReasonRunway, Title: "DeepSeek",
		Data: map[string]any{"value": 12.5}, OpenedAt: opened, UpdatedAt: opened,
	}
}

func TestIncidents(t *testing.T) {
	s := open(t)
	first := incident("inc_1", "mtr_a", core.IncidentLevel, "", t0)
	must(t, s.SaveIncident(ctx, first))

	// One open incident per subject, kind and occurrence — enforced here,
	// whatever the caller believes.
	if err := s.SaveIncident(ctx, incident("inc_2", "mtr_a", core.IncidentLevel, "", t0)); !errors.Is(err, ErrConflict) {
		t.Fatalf("a duplicate open incident was stored: %v", err)
	}
	must(t, s.SaveIncident(ctx, incident("inc_3", "mtr_a", core.IncidentBudget, "", t0.Add(time.Minute))))
	must(t, s.SaveIncident(ctx, incident("inc_4", "mtr_b", core.IncidentLevel, "", t0.Add(2*time.Minute))))
	must(t, s.SaveIncident(ctx, incident("inc_5", "rem_a", core.IncidentDue, "2026-12-24", t0.Add(3*time.Minute))))
	must(t, s.SaveIncident(ctx, incident("inc_6", "rem_a", core.IncidentDue, "2027-12-13", t0.Add(4*time.Minute))))

	first.Severity, first.State, first.Reason = core.Critical, core.StateResolved, core.ReasonLow
	first.ResolvedAt, first.AckedAt, first.NotifiedAt, first.NotifyCount = core.Ptr(t0.Add(time.Hour)), core.Ptr(t0), core.Ptr(t0), 2
	first.Data["value"] = 3.0
	must(t, s.SaveIncident(ctx, first))
	got, err := s.Incident(ctx, "inc_1")
	must(t, err)
	if got.Severity != core.Critical || got.State != core.StateResolved || got.NotifyCount != 2 || got.Data["value"] != 3.0 || got.ResolvedAt == nil {
		t.Fatalf("updated = %+v", got)
	}

	// Once resolved, the same subject may open again.
	must(t, s.SaveIncident(ctx, incident("inc_7", "mtr_a", core.IncidentLevel, "", t0.Add(5*time.Minute))))

	open, _ := s.OpenIncidents(ctx)
	if len(open) != 5 || open[0].ID != "inc_3" {
		t.Fatalf("open = %d, first %s", len(open), open[0].ID)
	}
	of, _ := s.OpenIncidentsOf(ctx, core.SubjectMeter, "mtr_a")
	if len(of) != 2 {
		t.Fatalf("open on mtr_a = %+v", of)
	}

	newest, _ := s.Incidents(ctx, IncidentFilter{Limit: 2})
	if len(newest) != 2 || newest[0].ID != "inc_7" || newest[1].ID != "inc_6" {
		t.Fatalf("newest first: %s %s", newest[0].ID, newest[1].ID)
	}
	older, _ := s.Incidents(ctx, IncidentFilter{Before: newest[1].OpenedAt})
	if len(older) != 4 || older[0].ID != "inc_5" {
		t.Fatalf("paging back: %d, first %s", len(older), older[0].ID)
	}
	resolved, _ := s.Incidents(ctx, IncidentFilter{State: core.StateResolved})
	if len(resolved) != 1 || resolved[0].ID != "inc_1" {
		t.Fatalf("resolved = %+v", resolved)
	}
	subject, _ := s.Incidents(ctx, IncidentFilter{SubjectType: core.SubjectReminder, SubjectID: "rem_a"})
	if len(subject) != 2 {
		t.Fatalf("by subject = %+v", subject)
	}
}

func TestChannelsAndDeliveries(t *testing.T) {
	s := open(t)
	c := core.Channel{
		Name: "Feishu", Type: "feishu", Config: map[string]string{"url": "https://hook.example/secret-path-0001"},
		Topics: []string{core.TopicMeters, core.TopicDates}, MinSeverity: core.Warning, Enabled: true,
	}
	must(t, s.CreateChannel(ctx, &c, t0))
	var raw []byte
	must(t, s.db.QueryRow("SELECT config FROM channels WHERE id = ?", c.ID).Scan(&raw))
	if bytes.Contains(raw, []byte("secret-path")) {
		t.Fatalf("the webhook URL is stored in the clear: %s", raw)
	}
	got, err := s.Channel(ctx, c.ID)
	must(t, err)
	if got.Config["url"] != c.Config["url"] || len(got.Topics) != 2 || got.MinSeverity != core.Warning {
		t.Fatalf("loaded = %+v", got)
	}
	if !got.Wants(core.TopicMeters, core.Critical) || got.Wants(core.TopicMeters, core.Info) || got.Wants(core.TopicDigest, core.Critical) {
		t.Error("Wants must follow topics and the severity floor")
	}
	dup := core.Channel{Name: "FEISHU", Type: "feishu", Config: map[string]string{}}
	if err := s.CreateChannel(ctx, &dup, t0); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate channel name: %v", err)
	}

	enqueue := func(incidentID string, notBefore time.Time) core.Delivery {
		t.Helper()
		d := core.Delivery{
			IncidentID: incidentID, Event: core.EventOpened, ChannelID: c.ID, Channel: c.Name,
			Message: []byte(`{"title":"x"}`), NotBefore: notBefore, CreatedAt: t0,
		}
		must(t, s.Enqueue(ctx, &d))
		return d
	}
	now := enqueue("inc_1", t0)
	later := enqueue("inc_1", t0.Add(6*time.Hour)) // held back by quiet hours
	other := enqueue("inc_2", t0)
	if now.ID == 0 || later.ID <= now.ID {
		t.Fatalf("ids = %d %d", now.ID, later.ID)
	}

	due, err := s.DueDeliveries(ctx, t0, 10)
	must(t, err)
	if len(due) != 2 || due[0].ID != now.ID || due[1].ID != other.ID || string(due[0].Message) != `{"title":"x"}` {
		t.Fatalf("due = %+v", due)
	}

	now.Status, now.Attempts, now.SentAt = core.DeliverySent, 1, core.Ptr(t0.Add(time.Second))
	must(t, s.SaveDelivery(ctx, now))
	other.Status, other.Attempts, other.LastError = core.DeliveryFailed, 6, "HTTP 400"
	must(t, s.SaveDelivery(ctx, other))

	// The incident recovered before its held-back message went out.
	dropped, err := s.DropPendingFor(ctx, "inc_1", "resolved before it was sent")
	must(t, err)
	if dropped != 1 {
		t.Fatalf("dropped = %d", dropped)
	}
	back, _ := s.Delivery(ctx, later.ID)
	if back.Status != core.DeliveryDropped || back.LastError == "" {
		t.Fatalf("held-back delivery = %+v", back)
	}
	if sent, _ := s.Delivery(ctx, now.ID); sent.Status != core.DeliverySent {
		t.Fatalf("a sent delivery was touched: %+v", sent)
	}

	log, _ := s.Deliveries(ctx, DeliveryFilter{IncidentID: "inc_1"})
	if len(log) != 2 || log[0].ID != later.ID {
		t.Fatalf("log = %+v", log)
	}
	page, _ := s.Deliveries(ctx, DeliveryFilter{BeforeID: other.ID, Limit: 1})
	if len(page) != 1 || page[0].ID != later.ID {
		t.Fatalf("page = %+v", page)
	}

	health, err := s.ChannelsHealth(ctx)
	must(t, err)
	if h := health[c.ID]; h.LastStatus != core.DeliveryFailed || h.FailedInARow != 1 || h.LastError != "HTTP 400" {
		t.Fatalf("health = %+v", h)
	}

	// Deleting a channel drops what is queued for it and keeps the history.
	pending := enqueue("inc_3", t0.Add(time.Hour))
	must(t, s.DeleteChannel(ctx, c.ID))
	if d, _ := s.Delivery(ctx, pending.ID); d.Status != core.DeliveryDropped {
		t.Fatalf("queued delivery of a deleted channel = %+v", d)
	}
	if all, _ := s.Deliveries(ctx, DeliveryFilter{}); len(all) != 4 {
		t.Fatalf("history must survive the channel: %d", len(all))
	}
}

func TestChannelHealthCountsTheFailingStreak(t *testing.T) {
	s := open(t)
	for i, status := range []string{core.DeliverySent, core.DeliveryFailed, core.DeliverySent, core.DeliveryFailed, core.DeliveryFailed, core.DeliveryFailed} {
		d := core.Delivery{Event: core.EventOpened, ChannelID: "chn_a", Message: []byte(`{}`), Status: status, NotBefore: t0, CreatedAt: t0.Add(time.Duration(i) * time.Minute)}
		must(t, s.Enqueue(ctx, &d))
	}
	health, _ := s.ChannelsHealth(ctx)
	if h := health["chn_a"]; h.FailedInARow != 3 || h.LastStatus != core.DeliveryFailed {
		t.Fatalf("health = %+v", h)
	}
}

func TestTokens(t *testing.T) {
	s := open(t)
	tok := core.Token{Name: "hermes", Scopes: []string{core.ScopeRead}}
	plaintext, err := s.CreateToken(ctx, &tok, t0)
	must(t, err)

	var stored string
	must(t, s.db.QueryRow("SELECT hash FROM tokens WHERE id = ?", tok.ID).Scan(&stored))
	if stored == plaintext || stored != secret.HashToken(plaintext) {
		t.Fatal("only the hash may be stored")
	}

	got, ok := s.Authenticate(ctx, plaintext, t0.Add(time.Hour))
	if !ok || got.Name != "hermes" || !got.Allows(core.ScopeRead) || got.Allows(core.ScopeAct) {
		t.Fatalf("authenticated = %+v, %v", got, ok)
	}
	for _, wrong := range []string{"", "qp_wrong", plaintext + "x"} {
		if _, ok := s.Authenticate(ctx, wrong, t0); ok {
			t.Errorf("%q authenticated", wrong)
		}
	}

	// last_used_at moves, but not on every request.
	list, _ := s.Tokens(ctx)
	if list[0].LastUsedAt == nil || !list[0].LastUsedAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("last used = %v", list[0].LastUsedAt)
	}
	s.Authenticate(ctx, plaintext, t0.Add(time.Hour+10*time.Second))
	list, _ = s.Tokens(ctx)
	if !list[0].LastUsedAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("last used was rewritten within a minute: %v", list[0].LastUsedAt)
	}

	// A key that already exists elsewhere can be carried over as it is.
	legacy := core.Token{Name: "magpie", Scopes: []string{core.ScopeRead}, ExpiresAt: core.Ptr(t0.Add(24 * time.Hour))}
	must(t, s.ImportToken(ctx, &legacy, "legacy-key-0001", t0))
	if _, ok := s.Authenticate(ctx, "legacy-key-0001", t0); !ok {
		t.Fatal("an imported key must authenticate")
	}
	if _, ok := s.Authenticate(ctx, "legacy-key-0001", t0.Add(24*time.Hour)); ok {
		t.Fatal("an expired token authenticated")
	}
	if err := s.ImportToken(ctx, &core.Token{Name: "again"}, "legacy-key-0001", t0); !errors.Is(err, ErrConflict) {
		t.Fatalf("the same key twice: %v", err)
	}

	admin := core.Token{Scopes: []string{core.ScopeAdmin}}
	if !admin.Allows(core.ScopeIngest) || !admin.Allows(core.ScopeRead) {
		t.Error("admin carries every scope")
	}

	must(t, s.DeleteToken(ctx, tok.ID))
	if _, ok := s.Authenticate(ctx, plaintext, t0); ok {
		t.Fatal("a revoked token authenticated")
	}
}

func TestCustomProviders(t *testing.T) {
	s := open(t)
	def := provider.Definition{
		ID: "relay", Name: "My relay", Builtin: true, Checked: provider.CheckedLive,
		Fields:  []provider.Field{{Key: "api_key", Label: "API Key", Secret: true}},
		Request: provider.Request{URL: "https://relay.example/balance", Auth: provider.Auth{Type: provider.AuthBearer}},
		Extract: "{value: .balance}",
	}
	must(t, s.SaveProvider(ctx, def, t0))
	got, err := s.CustomProvider(ctx, "relay")
	must(t, err)
	// A saved definition can never pass itself off as a verified built-in.
	if got.Builtin || got.Checked != "" || got.Extract != def.Extract {
		t.Fatalf("loaded = %+v", got)
	}
	def.Name = "Renamed"
	must(t, s.SaveProvider(ctx, def, t0))
	list, _ := s.CustomProviders(ctx)
	if len(list) != 1 || list[0].Name != "Renamed" {
		t.Fatalf("list = %+v", list)
	}

	a := core.Account{Name: "A", Provider: "relay", Credential: map[string]string{}}
	must(t, s.CreateAccount(ctx, &a, t0))
	if n, _ := s.ProviderInUse(ctx, "relay"); n != 1 {
		t.Errorf("in use = %d", n)
	}
	must(t, s.DeleteProvider(ctx, "relay"))
	if _, err := s.CustomProvider(ctx, "relay"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestPruneKeepsEachMetersNewestReading(t *testing.T) {
	s := open(t)
	old, live := pushed(t, s, "old"), pushed(t, s, "live")
	long := t0.Add(-800 * 24 * time.Hour)
	must(t, s.Record(ctx, &old, core.Reading{At: long, Value: 1}, nil))
	must(t, s.Record(ctx, &old, core.Reading{At: long.Add(time.Hour), Value: 2}, nil))
	must(t, s.Record(ctx, &live, core.Reading{At: long, Value: 5}, nil))
	must(t, s.Record(ctx, &live, core.Reading{At: t0, Value: 4}, nil))

	closed := incident("inc_old", "mtr_x", core.IncidentLevel, "", long)
	closed.State, closed.ResolvedAt = core.StateResolved, core.Ptr(long.Add(time.Hour))
	must(t, s.SaveIncident(ctx, closed))
	must(t, s.SaveIncident(ctx, incident("inc_open", "mtr_y", core.IncidentLevel, "", long)))
	for _, status := range []string{core.DeliverySent, core.DeliveryPending} {
		d := core.Delivery{Event: core.EventOpened, ChannelID: "chn_a", Message: []byte(`{}`), Status: status, NotBefore: long, CreatedAt: long}
		must(t, s.Enqueue(ctx, &d))
	}

	removed, err := s.Prune(ctx, t0.Add(-730*24*time.Hour))
	must(t, err)
	if removed != 2 {
		t.Fatalf("removed %d readings, want 2", removed)
	}
	// A meter that has not moved in two years still has its current value.
	if got := rows(t, s, old.ID); len(got) != 1 || got[0].Value != 2 {
		t.Fatalf("old meter = %+v", got)
	}
	if got := rows(t, s, live.ID); len(got) != 1 || got[0].Value != 4 {
		t.Fatalf("live meter = %+v", got)
	}
	if _, err := s.Incident(ctx, "inc_old"); !errors.Is(err, ErrNotFound) {
		t.Error("an old closed incident was kept")
	}
	if _, err := s.Incident(ctx, "inc_open"); err != nil {
		t.Error("an open incident must never be pruned, however old")
	}
	if left, _ := s.Deliveries(ctx, DeliveryFilter{}); len(left) != 1 || left[0].Status != core.DeliveryPending {
		t.Fatalf("deliveries = %+v", left)
	}
}

func TestBackupStatsAndEmpty(t *testing.T) {
	s := open(t)
	if empty, err := s.Empty(ctx); err != nil || !empty {
		t.Fatalf("a new database is empty: %v, %v", empty, err)
	}
	a := newAccount(t, s, "A")
	_, err := s.SaveOutcome(ctx, a.ID, Outcome{At: t0, NextAt: t0, Samples: []provider.Sample{sample("balance", 9)}})
	must(t, err)
	if empty, _ := s.Empty(ctx); empty {
		t.Fatal("not empty any more")
	}
	stats, err := s.Stats(ctx)
	must(t, err)
	if stats.Accounts != 1 || stats.Meters != 1 || stats.Readings != 1 || stats.Bytes == 0 {
		t.Fatalf("stats = %+v", stats)
	}

	target := filepath.Join(t.TempDir(), "backup.db")
	must(t, s.Backup(ctx, target))
	must(t, s.Backup(ctx, target)) // overwriting yesterday's file must work

	copyDB, err := sql.Open("sqlite", "file:"+target)
	must(t, err)
	defer copyDB.Close()
	var n int
	must(t, copyDB.QueryRow("SELECT COUNT(*) FROM readings").Scan(&n))
	if n != 1 {
		t.Fatalf("backup holds %d readings", n)
	}
}

// The database holds what someone depends on and the dates in their life;
// nobody else on the machine should be able to read it.
func TestTheDatabaseFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes mean something else there")
	}
	path := filepath.Join(t.TempDir(), "data", "quotapulse.db")
	// Made under a permissive umask by an earlier version, say.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path, secret.FromPassphrase("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("the database is readable by others: %v", mode)
	}
}

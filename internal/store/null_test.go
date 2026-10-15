package store

import (
	"errors"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

// Without a database, upper layers call Store directly with no branching. These
// cases pin the degradation behavior: writes drop silently, queries return
// empty, cooldowns always pass; only "persisting dynamic config to the
// database" returns ErrDisabled (the HTTP layer maps it to 503).
func TestNullStoreDegrades(t *testing.T) {
	s := Null()

	if s.Enabled() {
		t.Error("Null 的 Enabled 应当是 false")
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close 不该报错: %v", err)
	}
}

func TestNullStoreQueriesReturnEmpty(t *testing.T) {
	s := Null()
	ctx := t.Context()

	projects, err := s.ListProjects(ctx)
	if err != nil || len(projects) != 0 {
		t.Errorf("ListProjects = %v, %v，期望空切片与 nil", projects, err)
	}
	subs, err := s.ListSubscriptions(ctx)
	if err != nil || len(subs) != 0 {
		t.Errorf("ListSubscriptions = %v, %v", subs, err)
	}
	boxes, err := s.ListMailboxes(ctx)
	if err != nil || len(boxes) != 0 {
		t.Errorf("ListMailboxes = %v, %v", boxes, err)
	}

	series, err := s.BalanceSeries(ctx, 7)
	if err != nil || len(series) != 0 {
		t.Errorf("BalanceSeries = %v, %v", series, err)
	}
	history, err := s.BalanceHistory(ctx, BalanceQuery{Days: 7})
	if err != nil || len(history) != 0 {
		t.Errorf("BalanceHistory = %v, %v", history, err)
	}
	alerts, err := s.RecentAlerts(ctx, AlertQuery{Days: 7})
	if err != nil || len(alerts) != 0 {
		t.Errorf("RecentAlerts = %v, %v", alerts, err)
	}
	emails, err := s.EmailAlerts(ctx, EmailAlertQuery{Days: 30})
	if err != nil || len(emails) != 0 {
		t.Errorf("EmailAlerts = %v, %v", emails, err)
	}

	// trends and stats return nil, which the upper layer maps to 404 or "database disabled".
	trend, err := s.BalanceTrend(ctx, "pid", 30)
	if err != nil || trend != nil {
		t.Errorf("BalanceTrend = %v, %v，期望 nil, nil", trend, err)
	}
	stats, err := s.AlertStats(ctx, 30)
	if err != nil || stats != nil {
		t.Errorf("AlertStats = %v, %v，期望 nil, nil", stats, err)
	}
}

func TestNullStoreWritesAreDiscarded(t *testing.T) {
	s := Null()
	ctx := t.Context()

	// history recording is optional; without a database nothing records, but the main flow must not fail
	if err := s.SaveBalance(ctx, BalanceRecord{ProjectID: "pid", Balance: 1}); err != nil {
		t.Errorf("SaveBalance 不该报错: %v", err)
	}
	if err := s.SaveAlert(ctx, AlertRecord{AlertID: "pid", AlertType: "low_balance"}); err != nil {
		t.Errorf("SaveAlert 不该报错: %v", err)
	}
	if err := s.SaveEmailAlert(ctx, EmailAlertRecord{Mailbox: "m"}); err != nil {
		t.Errorf("SaveEmailAlert 不该报错: %v", err)
	}
}

func TestNullStoreNeverCoolsDown(t *testing.T) {
	s := Null()
	ctx := t.Context()

	// no history means no cooldown basis — always pass: alerts still go out, that is the product requirement
	for _, within := range []time.Duration{0, time.Minute, 24 * time.Hour} {
		got, err := s.HasRecentAlert(ctx, "pid", "low_balance", within)
		if err != nil {
			t.Fatalf("HasRecentAlert 报错: %v", err)
		}
		if got {
			t.Errorf("within=%v 时应当放行", within)
		}
	}
	got, err := s.HasRecentEmailAlert(ctx, "m", "s", "subj", "date", 30)
	if err != nil {
		t.Fatalf("HasRecentEmailAlert 报错: %v", err)
	}
	if got {
		t.Error("没有数据库时邮件去重应当一律放行")
	}
}

func TestNullStoreRejectsDynamicConfigWrites(t *testing.T) {
	s := Null()
	ctx := t.Context()

	// these cannot be dropped silently: the user pressed save and deserves to hear the database is off
	cases := []struct {
		name string
		call func() error
	}{
		{"UpsertProject", func() error { return s.UpsertProject(ctx, model.Project{Name: "p"}) }},
		{"DeleteProject", func() error { return s.DeleteProject(ctx, "p") }},
		{"UpsertSubscription", func() error { return s.UpsertSubscription(ctx, model.Subscription{Name: "s"}) }},
		{"DeleteSubscription", func() error { return s.DeleteSubscription(ctx, "s") }},
		{"UpsertMailbox", func() error { return s.UpsertMailbox(ctx, model.Mailbox{Name: "m"}) }},
		{"DeleteMailbox", func() error { return s.DeleteMailbox(ctx, "m") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrDisabled) {
				t.Errorf("期望 ErrDisabled，得到 %v", err)
			}
		})
	}
}

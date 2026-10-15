package cost

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/store"
	// UsageSpend arrives from the store package.
)

type fakeUsageStore struct {
	store.Store
	usage   []store.UsageSpend
	balance []model.BalancePoint
	sent    []store.AlertRecord
}

func (f *fakeUsageStore) ListUsageSpend(context.Context, string) ([]store.UsageSpend, error) {
	return f.usage, nil
}

func (f *fakeUsageStore) BalanceSeries(context.Context, int) ([]model.BalancePoint, error) {
	return f.balance, nil
}

func (f *fakeUsageStore) HasRecentAlert(context.Context, string, string, time.Duration) (bool, error) {
	return false, nil
}

func (f *fakeUsageStore) SaveAlert(_ context.Context, rec store.AlertRecord) error {
	f.sent = append(f.sent, rec)
	return nil
}

type capturingNotifier struct{ messages []notify.Message }

func (n *capturingNotifier) Send(_ context.Context, msg notify.Message) error {
	n.messages = append(n.messages, msg)
	return nil
}

// Reported spend on a budgeted project alerts on the month-end projection;
// a name that also carries balance history is left to the balance path.
func TestCheckUsageAlertsOnProjection(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.Local)
	fake := &fakeUsageStore{
		usage: []store.UsageSpend{
			{Project: "claude-code", Source: "claude-code", Currency: "USD", Cost: 60},
			{Project: "balanced", Source: "x", Currency: "USD", Cost: 100},
		},
		balance: []model.BalancePoint{{ProjectName: "balanced", Balance: 5, Timestamp: now.Unix()}},
	}
	notifier := &capturingNotifier{}
	alerter := &Alerter{Store: fake, Notifier: notifier, Log: slog.New(slog.DiscardHandler), Now: func() time.Time { return now }}

	projects := []model.Project{
		{Name: "claude-code", Provider: "usage", MonthlyBudget: 100}, // 60 over 16 days → ~116 projected > 100
		{Name: "balanced", Provider: "deepseek", MonthlyBudget: 50},  // balance-derived: skipped here
	}
	if got := alerter.CheckUsage(context.Background(), projects, func(model.Project) string { return "USD" }, false); got != 1 {
		t.Fatalf("sent = %d, want 1", got)
	}
	if len(notifier.messages) != 1 || notifier.messages[0].Title != "Budget overrun: claude-code" {
		t.Fatalf("messages = %+v", notifier.messages)
	}
	if len(fake.sent) != 1 || fake.sent[0].AlertID != model.ProjectID("usage", "claude-code")+"-usage" {
		t.Fatalf("records = %+v", fake.sent)
	}
}

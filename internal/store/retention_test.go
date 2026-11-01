package store

import (
	"context"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

// PruneHistoryBefore is the only writer of DELETE against the history tables,
// so the test proves the sweep on every engine: rows older than the cutoff
// vanish, newer ones survive, and a second sweep is a no-op.
func TestPruneHistoryBefore(t *testing.T) {
	eachBackend(t, "prune", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := context.Background()
		now := time.Now().UTC()

		for range 2 {
			if err := s.SaveBalance(ctx, BalanceRecord{
				ProjectID: "p1", ProjectName: "one", Provider: "deepseek",
				Balance: 10, BalanceType: model.TypeBalance,
			}); err != nil {
				t.Fatalf("SaveBalance: %v", err)
			}
		}
		if err := s.SaveAlert(ctx, AlertRecord{
			AlertID: "p1", Name: "one", AlertType: "low_balance", Message: "low", Status: "sent",
		}); err != nil {
			t.Fatalf("SaveAlert: %v", err)
		}
		if err := s.SaveEmailAlert(ctx, EmailAlertRecord{
			Mailbox: "ops@example.com", Sender: "billing@loud.com", Subject: "invoice", Date: "2026-10-31",
			Keywords: []string{}, AlertSent: true,
		}); err != nil {
			t.Fatalf("SaveEmailAlert: %v", err)
		}
		if err := s.UpsertUsageEvents(ctx, []model.UsageEntry{{
			// Dated yesterday: day-granular entry_date compares against the
			// cutoff's date, so today's cutoff prunes it and yesterday's keeps it.
			Source: "claude", Project: "one", Date: now.AddDate(0, 0, -1).Format("2006-01-02"),
			Model: "all", Cost: 1.5, Currency: "USD",
		}}); err != nil {
			t.Fatalf("UpsertUsageEvents: %v", err)
		}

		// A cutoff in the past keeps everything, and reports zero rows.
		fresh, err := s.PruneHistoryBefore(ctx, now.Add(-24*time.Hour))
		if err != nil {
			t.Fatalf("prune past: %v", err)
		}
		if fresh.Total() != 0 {
			t.Fatalf("过去的截止时间不应删除任何行，删了 %d", fresh.Total())
		}
		if rows, err := s.RecentAlerts(ctx, AlertQuery{Days: 1, Limit: 10}); err != nil || len(rows) != 1 {
			t.Fatalf("保留后告警应仍在: %d rows, err %v", len(rows), err)
		}

		// A cutoff in the future sweeps every seeded row, per table.
		swept, err := s.PruneHistoryBefore(ctx, now.Add(time.Hour))
		if err != nil {
			t.Fatalf("prune future: %v", err)
		}
		if swept.BalanceHistory != 2 || swept.AlertHistory != 1 || swept.EmailHistory != 1 || swept.UsageEvents != 1 {
			t.Fatalf("清理计数 = %+v，想要 balance=2 alerts=1 emails=1 usage=1", swept)
		}
		if rows, err := s.RecentAlerts(ctx, AlertQuery{Days: 30, Limit: 10}); err != nil || len(rows) != 0 {
			t.Fatalf("清理后告警应为空: %d rows, err %v", len(rows), err)
		}
		if rows, err := s.EmailAlerts(ctx, EmailAlertQuery{Days: 30, Limit: 10}); err != nil || len(rows) != 0 {
			t.Fatalf("清理后邮件告警应为空: %d rows, err %v", len(rows), err)
		}
		if spend, err := s.ListUsageSpend(ctx, "2000-01-01"); err != nil || len(spend) != 0 {
			t.Fatalf("清理后用量应为空: %d rows, err %v", len(spend), err)
		}

		// Sweeping an already-swept database reports zeros, not errors.
		again, err := s.PruneHistoryBefore(ctx, now.Add(time.Hour))
		if err != nil {
			t.Fatalf("重复清理: %v", err)
		}
		if again.Total() != 0 {
			t.Fatalf("重复清理不应再删行，删了 %d", again.Total())
		}
	})
}

// A batch must land whole: repeat cells overwrite, and the whole set reads back.
func TestUpsertUsageEventsBatch(t *testing.T) {
	eachBackend(t, "usage_batch", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := context.Background()

		first := []model.UsageEntry{
			{Source: "claude", Project: "one", Date: "2026-10-01", Model: "opus", Cost: 3, Currency: "USD", Requests: 10},
			{Source: "claude", Project: "one", Date: "2026-10-01", Model: "sonnet", Cost: 1, Currency: "USD", Requests: 40},
			{Source: "claude", Project: "two", Date: "2026-10-02", Model: "all", Cost: 2, Currency: "USD", Requests: 7},
		}
		if err := s.UpsertUsageEvents(ctx, first); err != nil {
			t.Fatalf("第一批: %v", err)
		}
		// A reporter re-run refreshes the same cells instead of duplicating them.
		first[0].Cost = 9
		if err := s.UpsertUsageEvents(ctx, first[:1]); err != nil {
			t.Fatalf("重跑覆盖: %v", err)
		}

		spend, err := s.ListUsageSpend(ctx, "2026-10-01")
		if err != nil {
			t.Fatalf("ListUsageSpend: %v", err)
		}
		byProject := map[string]float64{}
		for _, row := range spend {
			byProject[row.Project] += row.Cost
		}
		if byProject["one"] != 10 || byProject["two"] != 2 {
			t.Fatalf("聚合 = %v，想要 one=10（9+1 覆盖后）two=2", byProject)
		}

		if err := s.UpsertUsageEvents(ctx, nil); err != nil {
			t.Fatalf("空批次应是无操作: %v", err)
		}
	})
}

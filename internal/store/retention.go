package store

import (
	"context"
	"time"
)

// PruneCounts breaks the retention sweep down per table; the scheduled task
// reports it verbatim as its run detail.
type PruneCounts struct {
	BalanceHistory int64 `json:"balance_history"`
	AlertHistory   int64 `json:"alert_history"`
	EmailHistory   int64 `json:"email_alert_history"`
	UsageEvents    int64 `json:"usage_events"`
}

// Total is how many rows the sweep deleted altogether.
func (p PruneCounts) Total() int64 {
	return p.BalanceHistory + p.AlertHistory + p.EmailHistory + p.UsageEvents
}

// PruneHistoryBefore deletes rows older than the cutoff from every append-only
// table: balance snapshots, sent alerts, email alerts, and reported usage
// cells. Nothing else writes a DELETE against these tables, so history would
// otherwise grow for as long as the deployment lives — hourly checks, six
// indexes on balance_history, and a year of mail add up.
//
// The API never serves windows beyond 365 days, so a cutoff a generous distance
// past that (see HISTORY_RETENTION_DAYS) keeps every query answerable while
// bounding the tables. usage_events has no timestamp column; its ISO entry_date
// compares the same way lexicographically.
func (s *sqlStore) PruneHistoryBefore(ctx context.Context, before time.Time) (PruneCounts, error) {
	cutoff := before.UTC()
	date := cutoff.Format("2006-01-02")
	sweeps := []struct {
		table string
		query string
		arg   any
	}{
		{"balance_history", "DELETE FROM balance_history WHERE timestamp < ?", cutoff},
		{"alert_history", "DELETE FROM alert_history WHERE timestamp < ?", cutoff},
		{"email_alert_history", "DELETE FROM email_alert_history WHERE timestamp < ?", cutoff},
		{"usage_events", "DELETE FROM usage_events WHERE entry_date < ?", date},
	}

	var counts PruneCounts
	for _, sweep := range sweeps {
		result, err := s.db.ExecContext(ctx, rebind(s.engine, sweep.query), sweep.arg)
		if err != nil {
			return counts, err
		}
		affected, _ := result.RowsAffected()
		switch sweep.table {
		case "balance_history":
			counts.BalanceHistory = affected
		case "alert_history":
			counts.AlertHistory = affected
		case "email_alert_history":
			counts.EmailHistory = affected
		default:
			counts.UsageEvents = affected
		}
	}
	return counts, nil
}

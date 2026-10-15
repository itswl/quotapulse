package store

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

// ---------- Usage ingestion ----------
//
// Local tools (the Claude Code reporter, cron scripts) push per-day usage
// aggregates; the cost view and budget alerts merge them with the
// balance-derived spend. Rows are keyed by source+project+date+model, so
// re-running a reporter is an idempotent refresh.

// UpsertUsageEvent records one aggregated day/model cell; a repeat report of
// the same cell overwrites it.
func (s *sqlStore) UpsertUsageEvent(ctx context.Context, e model.UsageEntry) error {
	now := time.Now().UTC()
	return s.q.upsertUsageEvent(ctx, upsertUsageEventParams{
		Source:       e.Source,
		ProjectName:  e.Project,
		EntryDate:    e.Date,
		Model:        e.Model,
		Cost:         e.Cost,
		Currency:     e.Currency,
		InputTokens:  e.InputTokens,
		OutputTokens: e.OutputTokens,
		Requests:     int32(e.Requests),
		UpdatedAt:    nullTime(now),
	})
}

// UsageSpend is one project's reported spend since a date.
type UsageSpend struct {
	Project  string
	Source   string
	Currency string
	Cost     float64
}

// sumUsageSQL aggregates reported usage per project since an ISO date. SUM
// types come back differently on every engine, so the query runs raw and the
// results scan as nullable strings.
const sumUsageSQL = `SELECT project_name, source, currency, SUM(cost),
	SUM(input_tokens), SUM(output_tokens), SUM(requests)
	FROM usage_events WHERE entry_date >= ? GROUP BY project_name, source, currency`

// ListUsageSpend aggregates reported usage per project since the ISO date.
// Entries whose project_name matches a balance-monitored project are skipped
// by callers: balance history wins, reports would double-count it.
func (s *sqlStore) ListUsageSpend(ctx context.Context, sinceDate string) ([]UsageSpend, error) {
	query := rebind(s.engine, sumUsageSQL)
	rows, err := s.db.QueryContext(ctx, query, sinceDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UsageSpend
	for rows.Next() {
		var project, source, currency sql.NullString
		var cost sql.NullString
		if err := rows.Scan(&project, &source, &currency, &cost, new(sql.NullString), new(sql.NullString), new(sql.NullString)); err != nil {
			return nil, err
		}
		value, _ := strconv.ParseFloat(cost.String, 64)
		out = append(out, UsageSpend{
			Project: project.String, Source: source.String,
			Currency: currency.String, Cost: value,
		})
	}
	return out, rows.Err()
}

// rebind rewrites ? placeholders into $N for Postgres; SQLite and MySQL take
// ? natively.
func rebind(engine Engine, query string) string {
	if engine != EnginePostgres {
		return query
	}
	out := ""
	n := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			n++
			out += "$" + strconv.Itoa(n)
			continue
		}
		out += string(query[i])
	}
	return out
}

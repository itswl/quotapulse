package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"quotapulse/internal/core"
)

const meterColumns = `id, account_id, key, name, renamed, kind, unit, value, limit_value, resets_at, window_s,
	sampled_at, rules, muted_until, hidden, sort, created_at, updated_at`

func scanMeter(row interface{ Scan(...any) error }) (core.Meter, error) {
	var (
		m                       core.Meter
		account                 sql.NullString
		kind, rules             string
		value, limit            sql.NullFloat64
		resets, window, sampled sql.NullInt64
		muted                   sql.NullInt64
		renamed, hidden         int
		created, updated        int64
	)
	err := row.Scan(&m.ID, &account, &m.Key, &m.Name, &renamed, &kind, &m.Unit, &value, &limit, &resets, &window,
		&sampled, &rules, &muted, &hidden, &m.Sort, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	m.AccountID = account.String
	m.Kind = core.Kind(kind)
	m.Value, m.Limit = floatPtr(value), floatPtr(limit)
	m.ResetsAt, m.Window, m.SampledAt = timePtr(resets), intPtr(window), timePtr(sampled)
	m.Rules = parseRules(rules)
	m.MutedUntil = timePtr(muted)
	m.Renamed, m.Hidden = renamed != 0, hidden != 0
	m.CreatedAt, m.UpdatedAt = timeOf(created), timeOf(updated)
	return m, nil
}

func (s *Store) queryMeters(ctx context.Context, where string, args ...any) ([]core.Meter, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+meterColumns+" FROM meters "+where+" ORDER BY sort, name", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	meters := []core.Meter{}
	for rows.Next() {
		m, err := scanMeter(rows)
		if err != nil {
			return nil, err
		}
		meters = append(meters, m)
	}
	return meters, rows.Err()
}

// Meters lists every meter.
func (s *Store) Meters(ctx context.Context) ([]core.Meter, error) {
	return s.queryMeters(ctx, "")
}

// MetersOf lists the meters of one account.
func (s *Store) MetersOf(ctx context.Context, accountID string) ([]core.Meter, error) {
	return s.queryMeters(ctx, "WHERE account_id = ?", accountID)
}

// Meter loads one meter.
func (s *Store) Meter(ctx context.Context, id string) (core.Meter, error) {
	return scanMeter(s.db.QueryRowContext(ctx, "SELECT "+meterColumns+" FROM meters WHERE id = ?", id))
}

// PushedMeter loads a pushed meter by its key.
func (s *Store) PushedMeter(ctx context.Context, key string) (core.Meter, error) {
	return scanMeter(s.db.QueryRowContext(ctx,
		"SELECT "+meterColumns+" FROM meters WHERE account_id IS NULL AND key = ?", key))
}

// MeterByKey loads a sampled meter by account and key.
func (s *Store) MeterByKey(ctx context.Context, accountID, key string) (core.Meter, error) {
	return scanMeter(s.db.QueryRowContext(ctx,
		"SELECT "+meterColumns+" FROM meters WHERE account_id = ? AND key = ?", accountID, key))
}

func insertMeter(ctx context.Context, q querier, m core.Meter) error {
	var account any
	if m.AccountID != "" {
		account = m.AccountID
	}
	_, err := q.ExecContext(ctx, `INSERT INTO meters
		(id, account_id, key, name, renamed, kind, unit, value, limit_value, resets_at, window_s, sampled_at,
		 rules, muted_until, hidden, sort, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, account, m.Key, m.Name, boolInt(m.Renamed), string(m.Kind), m.Unit, nullFloat(m.Value), nullFloat(m.Limit),
		unixPtr(m.ResetsAt), nullInt(m.Window), unixPtr(m.SampledAt),
		rulesJSON(m.Rules), unixPtr(m.MutedUntil), boolInt(m.Hidden), m.Sort, unix(m.CreatedAt), unix(m.UpdatedAt))
	return conflict(err)
}

// CreateMeter inserts a meter, assigning its ID and timestamps.
func (s *Store) CreateMeter(ctx context.Context, m *core.Meter, now time.Time) error {
	if m.ID == "" {
		m.ID = core.NewID("mtr")
	}
	m.CreatedAt, m.UpdatedAt = now.UTC().Truncate(time.Second), now.UTC().Truncate(time.Second)
	return insertMeter(ctx, s.db, *m)
}

// UpdateMeter saves what a person can edit: name, kind and unit of a pushed
// meter, rules, mute, visibility and order. Readings are left alone.
func (s *Store) UpdateMeter(ctx context.Context, m core.Meter, now time.Time) error {
	return affected(s.db.ExecContext(ctx, `UPDATE meters SET
		name = ?, renamed = ?, kind = ?, unit = ?, rules = ?, muted_until = ?, hidden = ?, sort = ?, updated_at = ?
		WHERE id = ?`,
		m.Name, boolInt(m.Renamed), string(m.Kind), m.Unit, rulesJSON(m.Rules), unixPtr(m.MutedUntil), boolInt(m.Hidden), m.Sort,
		unix(now), m.ID))
}

// DeleteMeter removes a meter and its history.
func (s *Store) DeleteMeter(ctx context.Context, id string) error {
	return affected(s.db.ExecContext(ctx, "DELETE FROM meters WHERE id = ?", id))
}

// Record appends a reading to a meter and updates its latest values.
func (s *Store) Record(ctx context.Context, meter *core.Meter, reading core.Reading, window *int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error { return record(ctx, tx, meter, reading, window) })
}

// record writes one reading.
//
// Most samples change nothing: a balance sits still for hours. Storing each
// would bury the few rows that matter, so a flat run is kept as two rows —
// where it began and where it currently ends — and a third identical sample
// just moves the end. Nothing is lost: a straight line between the two is
// exactly what was observed.
func record(ctx context.Context, tx *sql.Tx, meter *core.Meter, r core.Reading, window *int64) error {
	ts := unix(r.At)
	rows, err := tx.QueryContext(ctx,
		"SELECT ts, value, limit_value FROM readings WHERE meter_id = ? ORDER BY ts DESC LIMIT 2", meter.ID)
	if err != nil {
		return err
	}
	type last struct {
		ts    int64
		value float64
		limit sql.NullFloat64
	}
	var tail []last
	for rows.Next() {
		var l last
		if err := rows.Scan(&l.ts, &l.value, &l.limit); err != nil {
			rows.Close()
			return err
		}
		tail = append(tail, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	same := func(l last) bool {
		return l.value == r.Value && l.limit.Valid == (r.Limit != nil) && (r.Limit == nil || l.limit.Float64 == *r.Limit)
	}
	switch {
	case len(tail) == 2 && ts > tail[0].ts && same(tail[0]) && same(tail[1]):
		_, err = tx.ExecContext(ctx, "UPDATE readings SET ts = ?, resets_at = ? WHERE meter_id = ? AND ts = ?",
			ts, unixPtr(r.ResetsAt), meter.ID, tail[0].ts)
	default:
		_, err = tx.ExecContext(ctx, `INSERT INTO readings (meter_id, ts, value, limit_value, resets_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (meter_id, ts) DO UPDATE SET
				value = excluded.value, limit_value = excluded.limit_value, resets_at = excluded.resets_at`,
			meter.ID, ts, r.Value, nullFloat(r.Limit), unixPtr(r.ResetsAt))
	}
	if err != nil {
		return err
	}

	// A late reading — pushed with a timestamp in the past — joins the
	// history but does not become the meter's current value.
	if meter.SampledAt != nil && r.At.Before(*meter.SampledAt) {
		return nil
	}
	at := r.At.UTC().Truncate(time.Second)
	meter.Value, meter.Limit, meter.ResetsAt, meter.Window, meter.SampledAt = &r.Value, r.Limit, r.ResetsAt, window, &at
	_, err = tx.ExecContext(ctx, `UPDATE meters SET
		kind = ?, unit = ?, value = ?, limit_value = ?, resets_at = ?, window_s = ?, sampled_at = ?
		WHERE id = ?`,
		string(meter.Kind), meter.Unit, r.Value, nullFloat(r.Limit), unixPtr(r.ResetsAt), nullInt(window), ts, meter.ID)
	return err
}

// ImportReadings loads a meter's history in bulk, oldest first, applying the
// same flat-run folding as live recording.
func (s *Store) ImportReadings(ctx context.Context, meter *core.Meter, readings []core.Reading) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, r := range readings {
			if err := record(ctx, tx, meter, r, meter.Window); err != nil {
				return err
			}
		}
		return nil
	})
}

// Readings returns a meter's history from since onward, oldest first, plus
// the one reading just before since. Without that one, whatever was spent
// across the boundary would be missing from every sum.
func (s *Store) Readings(ctx context.Context, meterID string, since time.Time) ([]core.Reading, error) {
	return s.ReadingsBetween(ctx, meterID, since, time.Unix(1<<40, 0))
}

// ReadingsBetween is Readings with an upper bound.
func (s *Store) ReadingsBetween(ctx context.Context, meterID string, since, until time.Time) ([]core.Reading, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ts, value, limit_value, resets_at FROM readings
		WHERE meter_id = ?1
		  AND ts >= COALESCE((SELECT MAX(ts) FROM readings WHERE meter_id = ?1 AND ts < ?2), ?2)
		  AND ts <= ?3
		ORDER BY ts`, meterID, unix(since), unix(until))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var readings []core.Reading
	for rows.Next() {
		var (
			ts     int64
			value  float64
			limit  sql.NullFloat64
			resets sql.NullInt64
		)
		if err := rows.Scan(&ts, &value, &limit, &resets); err != nil {
			return nil, err
		}
		readings = append(readings, core.Reading{At: timeOf(ts), Value: value, Limit: floatPtr(limit), ResetsAt: timePtr(resets)})
	}
	return readings, rows.Err()
}

// ReadingCount is how many rows a meter's history holds.
func (s *Store) ReadingCount(ctx context.Context, meterID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM readings WHERE meter_id = ?", meterID).Scan(&n)
	return n, err
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"quotapulse/internal/core"
	"quotapulse/internal/recur"
)

const reminderColumns = `id, title, kind, rule, lead_days, amount, currency, grp, notes,
	done_through, snoozed_until, enabled, created_at, updated_at`

func scanReminder(row interface{ Scan(...any) error }) (core.Reminder, error) {
	var (
		r                core.Reminder
		rule, lead       string
		amount           sql.NullFloat64
		done, snoozed    sql.NullString
		enabled          int
		created, updated int64
	)
	err := row.Scan(&r.ID, &r.Title, &r.Kind, &rule, &lead, &amount, &r.Currency, &r.Group, &r.Notes,
		&done, &snoozed, &enabled, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(rule), &r.Rule); err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(lead), &r.LeadDays); err != nil || r.LeadDays == nil {
		r.LeadDays = []int{0}
	}
	r.Amount = floatPtr(amount)
	r.DoneThrough, r.SnoozedUntil = datePtr(done), datePtr(snoozed)
	r.Enabled = enabled != 0
	r.CreatedAt, r.UpdatedAt = timeOf(created), timeOf(updated)
	return r, nil
}

func datePtr(s sql.NullString) *recur.Date {
	if !s.Valid || s.String == "" {
		return nil
	}
	d, err := recur.ParseDate(s.String)
	if err != nil {
		return nil
	}
	return &d
}

func nullDate(d *recur.Date) any {
	if d == nil || d.IsZero() {
		return nil
	}
	return d.String()
}

// Reminders lists every reminder, oldest first. Callers sort by next
// occurrence, which only they can compute.
func (s *Store) Reminders(ctx context.Context) ([]core.Reminder, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+reminderColumns+" FROM reminders ORDER BY created_at, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reminders := []core.Reminder{}
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		reminders = append(reminders, r)
	}
	return reminders, rows.Err()
}

// Reminder loads one reminder.
func (s *Store) Reminder(ctx context.Context, id string) (core.Reminder, error) {
	return scanReminder(s.db.QueryRowContext(ctx, "SELECT "+reminderColumns+" FROM reminders WHERE id = ?", id))
}

// CreateReminder inserts a reminder, assigning its ID and timestamps.
func (s *Store) CreateReminder(ctx context.Context, r *core.Reminder, now time.Time) error {
	if r.ID == "" {
		r.ID = core.NewID("rem")
	}
	r.CreatedAt, r.UpdatedAt = now.UTC().Truncate(time.Second), now.UTC().Truncate(time.Second)
	_, err := s.db.ExecContext(ctx, `INSERT INTO reminders
		(id, title, kind, rule, lead_days, amount, currency, grp, notes, done_through, snoozed_until,
		 enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Title, r.Kind, mustJSON(r.Rule), mustJSON(r.LeadDays), nullFloat(r.Amount), r.Currency, r.Group,
		r.Notes, nullDate(r.DoneThrough), nullDate(r.SnoozedUntil), boolInt(r.Enabled),
		unix(r.CreatedAt), unix(r.UpdatedAt))
	return conflict(err)
}

// UpdateReminder saves every editable field of a reminder.
func (s *Store) UpdateReminder(ctx context.Context, r core.Reminder, now time.Time) error {
	return affected(s.db.ExecContext(ctx, `UPDATE reminders SET
		title = ?, kind = ?, rule = ?, lead_days = ?, amount = ?, currency = ?, grp = ?, notes = ?,
		done_through = ?, snoozed_until = ?, enabled = ?, updated_at = ?
		WHERE id = ?`,
		r.Title, r.Kind, mustJSON(r.Rule), mustJSON(r.LeadDays), nullFloat(r.Amount), r.Currency, r.Group, r.Notes,
		nullDate(r.DoneThrough), nullDate(r.SnoozedUntil), boolInt(r.Enabled), unix(now), r.ID))
}

// DeleteReminder removes a reminder.
func (s *Store) DeleteReminder(ctx context.Context, id string) error {
	return affected(s.db.ExecContext(ctx, "DELETE FROM reminders WHERE id = ?", id))
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/core"
)

const incidentColumns = `id, subject_type, subject_id, kind, dedup, severity, state, reason, title, data,
	opened_at, updated_at, resolved_at, acked_at, notified_at, notify_count`

func scanIncident(row interface{ Scan(...any) error }) (core.Incident, error) {
	var (
		i                         core.Incident
		severity, data            string
		opened, updated           int64
		resolved, acked, notified sql.NullInt64
	)
	err := row.Scan(&i.ID, &i.SubjectType, &i.SubjectID, &i.Kind, &i.Dedup, &severity, &i.State, &i.Reason,
		&i.Title, &data, &opened, &updated, &resolved, &acked, &notified, &i.NotifyCount)
	if errors.Is(err, sql.ErrNoRows) {
		return i, ErrNotFound
	}
	if err != nil {
		return i, err
	}
	i.Severity = core.Severity(severity)
	i.Data = map[string]any{}
	_ = json.Unmarshal([]byte(data), &i.Data)
	i.OpenedAt, i.UpdatedAt = timeOf(opened), timeOf(updated)
	i.ResolvedAt, i.AckedAt, i.NotifiedAt = timePtr(resolved), timePtr(acked), timePtr(notified)
	return i, nil
}

func (s *Store) queryIncidents(ctx context.Context, tail string, args ...any) ([]core.Incident, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+incidentColumns+" FROM incidents "+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	incidents := []core.Incident{}
	for rows.Next() {
		i, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		incidents = append(incidents, i)
	}
	return incidents, rows.Err()
}

// OpenIncidents lists everything currently open, oldest first.
func (s *Store) OpenIncidents(ctx context.Context) ([]core.Incident, error) {
	return s.queryIncidents(ctx, "WHERE state = 'open' ORDER BY opened_at, id")
}

// OpenIncidentsOf lists what is open on one subject.
func (s *Store) OpenIncidentsOf(ctx context.Context, subjectType, subjectID string) ([]core.Incident, error) {
	return s.queryIncidents(ctx,
		"WHERE state = 'open' AND subject_type = ? AND subject_id = ? ORDER BY opened_at, id", subjectType, subjectID)
}

// Incident loads one incident.
func (s *Store) Incident(ctx context.Context, id string) (core.Incident, error) {
	return scanIncident(s.db.QueryRowContext(ctx, "SELECT "+incidentColumns+" FROM incidents WHERE id = ?", id))
}

// IncidentFilter narrows a history listing.
type IncidentFilter struct {
	State       string // open, resolved, or empty for both
	SubjectType string
	SubjectID   string
	// Before pages backwards: only incidents opened before this moment.
	Before time.Time
	Limit  int
}

// Incidents lists history, newest first.
func (s *Store) Incidents(ctx context.Context, f IncidentFilter) ([]core.Incident, error) {
	var where []string
	var args []any
	if f.State != "" {
		where = append(where, "state = ?")
		args = append(args, f.State)
	}
	if f.SubjectType != "" {
		where = append(where, "subject_type = ?")
		args = append(args, f.SubjectType)
	}
	if f.SubjectID != "" {
		where = append(where, "subject_id = ?")
		args = append(args, f.SubjectID)
	}
	if !f.Before.IsZero() {
		where = append(where, "opened_at < ?")
		args = append(args, unix(f.Before))
	}
	tail := ""
	if len(where) > 0 {
		tail = "WHERE " + strings.Join(where, " AND ") + " "
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.queryIncidents(ctx, tail+"ORDER BY opened_at DESC, id DESC LIMIT ?", append(args, limit)...)
}

// SaveIncident inserts or updates an incident. Opening a second incident
// for a subject, kind and occurrence that already has one open fails with
// ErrConflict: the index is the last line of defence against a duplicate.
func (s *Store) SaveIncident(ctx context.Context, i core.Incident) error {
	if i.Data == nil {
		i.Data = map[string]any{}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO incidents
		(id, subject_type, subject_id, kind, dedup, severity, state, reason, title, data,
		 opened_at, updated_at, resolved_at, acked_at, notified_at, notify_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			severity = excluded.severity, state = excluded.state, reason = excluded.reason,
			title = excluded.title, data = excluded.data, updated_at = excluded.updated_at,
			resolved_at = excluded.resolved_at, acked_at = excluded.acked_at,
			notified_at = excluded.notified_at, notify_count = excluded.notify_count`,
		i.ID, i.SubjectType, i.SubjectID, i.Kind, i.Dedup, string(i.Severity), i.State, i.Reason, i.Title,
		mustJSON(i.Data), unix(i.OpenedAt), unix(i.UpdatedAt), unixPtr(i.ResolvedAt), unixPtr(i.AckedAt),
		unixPtr(i.NotifiedAt), i.NotifyCount)
	return conflict(err)
}

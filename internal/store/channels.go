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

const channelColumns = `id, name, type, config, topics, min_severity, enabled, created_at, updated_at`

func (s *Store) scanChannel(row interface{ Scan(...any) error }) (core.Channel, error) {
	var (
		c                core.Channel
		sealed           []byte
		topics, severity string
		enabled          int
		created, updated int64
	)
	err := row.Scan(&c.ID, &c.Name, &c.Type, &sealed, &topics, &severity, &enabled, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	c.Config = map[string]string{}
	// Unreadable config leaves the channel listed with nothing in it; a send
	// then fails with a message saying what to do.
	_ = s.keys.OpenJSON(sealed, &c.Config)
	_ = json.Unmarshal([]byte(topics), &c.Topics)
	if c.Topics == nil {
		c.Topics = []string{}
	}
	c.MinSeverity = core.Severity(severity)
	c.Enabled = enabled != 0
	c.CreatedAt, c.UpdatedAt = timeOf(created), timeOf(updated)
	return c, nil
}

// Channels lists every channel by name.
func (s *Store) Channels(ctx context.Context) ([]core.Channel, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+channelColumns+" FROM channels ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	channels := []core.Channel{}
	for rows.Next() {
		c, err := s.scanChannel(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, c)
	}
	return channels, rows.Err()
}

// Channel loads one channel.
func (s *Store) Channel(ctx context.Context, id string) (core.Channel, error) {
	return s.scanChannel(s.db.QueryRowContext(ctx, "SELECT "+channelColumns+" FROM channels WHERE id = ?", id))
}

// CreateChannel inserts a channel, assigning its ID and timestamps.
func (s *Store) CreateChannel(ctx context.Context, c *core.Channel, now time.Time) error {
	sealed, err := s.keys.SealJSON(c.Config)
	if err != nil {
		return err
	}
	if c.ID == "" {
		c.ID = core.NewID("chn")
	}
	c.CreatedAt, c.UpdatedAt = now.UTC().Truncate(time.Second), now.UTC().Truncate(time.Second)
	_, err = s.db.ExecContext(ctx, `INSERT INTO channels
		(id, name, type, config, topics, min_severity, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Type, sealed, mustJSON(c.Topics), string(c.MinSeverity), boolInt(c.Enabled),
		unix(c.CreatedAt), unix(c.UpdatedAt))
	return conflict(err)
}

// UpdateChannel saves every editable field of a channel.
func (s *Store) UpdateChannel(ctx context.Context, c core.Channel, now time.Time) error {
	sealed, err := s.keys.SealJSON(c.Config)
	if err != nil {
		return err
	}
	return affected(s.db.ExecContext(ctx, `UPDATE channels SET
		name = ?, type = ?, config = ?, topics = ?, min_severity = ?, enabled = ?, updated_at = ?
		WHERE id = ?`,
		c.Name, c.Type, sealed, mustJSON(c.Topics), string(c.MinSeverity), boolInt(c.Enabled), unix(now), c.ID))
}

// DeleteChannel removes a channel. Its past deliveries stay as history;
// anything still queued for it is dropped, since there is nowhere to send it.
func (s *Store) DeleteChannel(ctx context.Context, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if err := affected(tx.ExecContext(ctx, "DELETE FROM channels WHERE id = ?", id)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE deliveries SET status = ?, last_error = 'the channel was deleted'
			WHERE channel_id = ? AND status = ?`, core.DeliveryDropped, id, core.DeliveryPending)
		return err
	})
}

// ---------- deliveries ----------

const deliveryColumns = `id, incident_id, event, channel_id, channel, message, status, attempts,
	not_before, last_error, created_at, sent_at`

func scanDelivery(row interface{ Scan(...any) error }) (core.Delivery, error) {
	var (
		d                  core.Delivery
		message            string
		notBefore, created int64
		sent               sql.NullInt64
	)
	err := row.Scan(&d.ID, &d.IncidentID, &d.Event, &d.ChannelID, &d.Channel, &message, &d.Status, &d.Attempts,
		&notBefore, &d.LastError, &created, &sent)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.Message = json.RawMessage(message)
	d.NotBefore, d.CreatedAt, d.SentAt = timeOf(notBefore), timeOf(created), timePtr(sent)
	return d, nil
}

func (s *Store) queryDeliveries(ctx context.Context, tail string, args ...any) ([]core.Delivery, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+deliveryColumns+" FROM deliveries "+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deliveries := []core.Delivery{}
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, rows.Err()
}

// Enqueue writes a delivery to the outbox and sets its ID.
func (s *Store) Enqueue(ctx context.Context, d *core.Delivery) error {
	if d.Status == "" {
		d.Status = core.DeliveryPending
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO deliveries
		(incident_id, event, channel_id, channel, message, status, attempts, not_before, last_error, created_at, sent_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.IncidentID, d.Event, d.ChannelID, d.Channel, string(d.Message), d.Status, d.Attempts,
		unix(d.NotBefore), d.LastError, unix(d.CreatedAt), unixPtr(d.SentAt))
	if err != nil {
		return err
	}
	d.ID, err = result.LastInsertId()
	return err
}

// DueDeliveries lists pending deliveries whose time has come, oldest first.
func (s *Store) DueDeliveries(ctx context.Context, now time.Time, limit int) ([]core.Delivery, error) {
	return s.queryDeliveries(ctx, "WHERE status = 'pending' AND not_before <= ? ORDER BY id LIMIT ?", unix(now), limit)
}

// SaveDelivery records the outcome of an attempt.
func (s *Store) SaveDelivery(ctx context.Context, d core.Delivery) error {
	return affected(s.db.ExecContext(ctx, `UPDATE deliveries SET
		status = ?, attempts = ?, not_before = ?, last_error = ?, sent_at = ? WHERE id = ?`,
		d.Status, d.Attempts, unix(d.NotBefore), d.LastError, unixPtr(d.SentAt), d.ID))
}

// Delivery loads one delivery.
func (s *Store) Delivery(ctx context.Context, id int64) (core.Delivery, error) {
	return scanDelivery(s.db.QueryRowContext(ctx, "SELECT "+deliveryColumns+" FROM deliveries WHERE id = ?", id))
}

// DeliveryFilter narrows the notification log.
type DeliveryFilter struct {
	IncidentID string
	ChannelID  string
	// BeforeID pages backwards.
	BeforeID int64
	Limit    int
}

// Deliveries lists the notification log, newest first.
func (s *Store) Deliveries(ctx context.Context, f DeliveryFilter) ([]core.Delivery, error) {
	var where []string
	var args []any
	if f.IncidentID != "" {
		where = append(where, "incident_id = ?")
		args = append(args, f.IncidentID)
	}
	if f.ChannelID != "" {
		where = append(where, "channel_id = ?")
		args = append(args, f.ChannelID)
	}
	if f.BeforeID > 0 {
		where = append(where, "id < ?")
		args = append(args, f.BeforeID)
	}
	tail := ""
	if len(where) > 0 {
		tail = "WHERE " + strings.Join(where, " AND ") + " "
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.queryDeliveries(ctx, tail+"ORDER BY id DESC LIMIT ?", append(args, limit)...)
}

// DropPendingFor drops what is still queued for an incident. When something
// recovers before its "it broke" message left the quiet-hours queue, sending
// that message would be news of a problem that no longer exists.
func (s *Store) DropPendingFor(ctx context.Context, incidentID, reason string) (int, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE deliveries SET status = ?, last_error = ?
		WHERE incident_id = ? AND status = ?`, core.DeliveryDropped, reason, incidentID, core.DeliveryPending)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// ChannelHealth is how a channel's recent deliveries went.
type ChannelHealth struct {
	LastStatus string     `json:"last_status"`
	LastError  string     `json:"last_error"`
	LastAt     *time.Time `json:"last_at"`
	// FailedInARow counts the most recent consecutive failures.
	FailedInARow int `json:"failed_in_a_row"`
}

// ChannelsHealth summarizes each channel's last few finished deliveries.
func (s *Store) ChannelsHealth(ctx context.Context) (map[string]ChannelHealth, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT channel_id, status, last_error, COALESCE(sent_at, created_at)
		FROM (SELECT *, ROW_NUMBER() OVER (PARTITION BY channel_id ORDER BY id DESC) AS n
		      FROM deliveries WHERE status IN ('sent', 'failed'))
		WHERE n <= 10 ORDER BY channel_id, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	health := map[string]ChannelHealth{}
	streak := map[string]bool{} // still counting failures for this channel
	for rows.Next() {
		var id, status, lastError string
		var at int64
		if err := rows.Scan(&id, &status, &lastError, &at); err != nil {
			return nil, err
		}
		h, seen := health[id]
		if !seen {
			t := timeOf(at)
			h = ChannelHealth{LastStatus: status, LastError: lastError, LastAt: &t}
			streak[id] = true
		}
		if streak[id] {
			if status == core.DeliveryFailed {
				h.FailedInARow++
			} else {
				streak[id] = false
			}
		}
		health[id] = h
	}
	return health, rows.Err()
}

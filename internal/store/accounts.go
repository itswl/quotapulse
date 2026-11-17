package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/provider"
)

const accountColumns = `id, name, provider, credential, grp, interval_s, enabled, status, last_error,
	failures, sampled_at, succeeded_at, next_at, latency_ms, created_at, updated_at`

func (s *Store) scanAccount(row interface{ Scan(...any) error }) (core.Account, error) {
	var (
		a                               core.Account
		sealed                          []byte
		enabled                         int
		sampled, succeeded, next, lat   sql.NullInt64
		created, updated                int64
		interval, failures              int
		name, prov, group, status, last string
	)
	err := row.Scan(&a.ID, &name, &prov, &sealed, &group, &interval, &enabled, &status, &last,
		&failures, &sampled, &succeeded, &next, &lat, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.Name, a.Provider, a.Group, a.Status, a.LastError = name, prov, group, status, last
	a.Interval, a.Failures, a.Enabled = interval, failures, enabled != 0
	a.SampledAt, a.SucceededAt, a.NextAt = timePtr(sampled), timePtr(succeeded), timePtr(next)
	a.LatencyMs = intPtr(lat)
	a.CreatedAt, a.UpdatedAt = timeOf(created), timeOf(updated)
	a.Credential = map[string]string{}
	// A credential sealed with another secret is not an error to fail the
	// whole list on: the account is shown, flagged, and can be re-entered.
	if err := s.keys.OpenJSON(sealed, &a.Credential); err != nil {
		a.Credential = map[string]string{}
		a.Unreadable = true
	}
	return a, nil
}

// Accounts lists every account, by group and name.
func (s *Store) Accounts(ctx context.Context) ([]core.Account, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+accountColumns+" FROM accounts ORDER BY grp, name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []core.Account{}
	for rows.Next() {
		a, err := s.scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// Account loads one account.
func (s *Store) Account(ctx context.Context, id string) (core.Account, error) {
	return s.scanAccount(s.db.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM accounts WHERE id = ?", id))
}

// AccountByName loads one account by its name, which is unique.
func (s *Store) AccountByName(ctx context.Context, name string) (core.Account, error) {
	return s.scanAccount(s.db.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM accounts WHERE name = ?", name))
}

// CreateAccount inserts an account, assigning its ID and timestamps. A new
// account is due at once: next_at stays NULL until the first sample.
func (s *Store) CreateAccount(ctx context.Context, a *core.Account, now time.Time) error {
	sealed, err := s.keys.SealJSON(a.Credential)
	if err != nil {
		return err
	}
	if a.ID == "" {
		a.ID = core.NewID("acc")
	}
	if a.Status == "" {
		a.Status = core.StatusPending
	}
	a.CreatedAt, a.UpdatedAt = now.UTC().Truncate(time.Second), now.UTC().Truncate(time.Second)
	_, err = s.db.ExecContext(ctx, `INSERT INTO accounts
		(id, name, provider, credential, grp, interval_s, enabled, status, last_error, failures, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Name, a.Provider, sealed, a.Group, a.Interval, boolInt(a.Enabled), a.Status, a.LastError, a.Failures,
		unix(a.CreatedAt), unix(a.UpdatedAt))
	return conflict(err)
}

// UpdateAccount saves what a person can edit: name, provider, credential,
// group, interval and whether it is enabled. Sampling state is left alone.
func (s *Store) UpdateAccount(ctx context.Context, a core.Account, now time.Time) error {
	sealed, err := s.keys.SealJSON(a.Credential)
	if err != nil {
		return err
	}
	return affected(s.db.ExecContext(ctx, `UPDATE accounts SET
		name = ?, provider = ?, credential = ?, grp = ?, interval_s = ?, enabled = ?, updated_at = ?
		WHERE id = ?`,
		a.Name, a.Provider, sealed, a.Group, a.Interval, boolInt(a.Enabled), unix(now), a.ID))
}

// DeleteAccount removes an account with its meters and their history.
func (s *Store) DeleteAccount(ctx context.Context, id string) error {
	return affected(s.db.ExecContext(ctx, "DELETE FROM accounts WHERE id = ?", id))
}

// DueAccounts lists the enabled accounts whose next sample time has come.
func (s *Store) DueAccounts(ctx context.Context, now time.Time) ([]core.Account, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+accountColumns+` FROM accounts
		WHERE enabled = 1 AND (next_at IS NULL OR next_at <= ?) ORDER BY COALESCE(next_at, 0)`, unix(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []core.Account
	for rows.Next() {
		a, err := s.scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// ScheduleAccount moves an account's next sample time. Requesting an
// immediate sample is just scheduling it for now.
func (s *Store) ScheduleAccount(ctx context.Context, id string, at time.Time) error {
	return affected(s.db.ExecContext(ctx, "UPDATE accounts SET next_at = ? WHERE id = ?", unix(at), id))
}

// Outcome is the result of sampling an account once.
type Outcome struct {
	At      time.Time
	NextAt  time.Time
	Latency time.Duration
	// Err is the failure, already redacted; empty on success.
	Err string
	// Samples are the meters read; empty on failure.
	Samples []provider.Sample
	// Language picks the name of a meter seen for the first time.
	Language string
}

// SaveOutcome records one sampling attempt atomically: the account's state,
// and on success every meter's reading. It returns the meters that were read.
func (s *Store) SaveOutcome(ctx context.Context, accountID string, o Outcome) ([]core.Meter, error) {
	var meters []core.Meter
	err := s.tx(ctx, func(tx *sql.Tx) error {
		at := unix(o.At)
		if o.Err != "" {
			return affected(tx.ExecContext(ctx, `UPDATE accounts SET
				status = ?, last_error = ?, failures = failures + 1, sampled_at = ?, next_at = ?, latency_ms = ?
				WHERE id = ?`,
				core.StatusFailing, o.Err, at, unix(o.NextAt), o.Latency.Milliseconds(), accountID))
		}
		if err := affected(tx.ExecContext(ctx, `UPDATE accounts SET
			status = ?, last_error = '', failures = 0, sampled_at = ?, succeeded_at = ?, next_at = ?, latency_ms = ?
			WHERE id = ?`,
			core.StatusOK, at, at, unix(o.NextAt), o.Latency.Milliseconds(), accountID)); err != nil {
			return err
		}
		for i, sample := range o.Samples {
			meter, err := upsertSampled(ctx, tx, accountID, sample, i, o)
			if err != nil {
				return err
			}
			meters = append(meters, meter)
		}
		return nil
	})
	return meters, err
}

// upsertSampled finds or creates the meter a sample belongs to and records
// the reading.
func upsertSampled(ctx context.Context, tx *sql.Tx, accountID string, sample provider.Sample, position int, o Outcome) (core.Meter, error) {
	meter, err := scanMeter(tx.QueryRowContext(ctx,
		"SELECT "+meterColumns+" FROM meters WHERE account_id = ? AND key = ?", accountID, sample.Key))
	switch {
	case errors.Is(err, ErrNotFound):
		name := sample.Name
		if o.Language == "zh" && sample.NameZH != "" {
			name = sample.NameZH
		}
		if name == "" {
			name = sample.Key
		}
		meter = core.Meter{
			ID: core.NewID("mtr"), AccountID: accountID, Key: sample.Key, Name: name,
			Kind: sample.Kind, Unit: sample.Unit, Sort: position,
			CreatedAt: o.At.UTC().Truncate(time.Second), UpdatedAt: o.At.UTC().Truncate(time.Second),
		}
		if err := insertMeter(ctx, tx, meter); err != nil {
			return meter, err
		}
	case err != nil:
		return meter, err
	}
	// The definition is the authority on what a meter is. If it changed its
	// mind about the kind or the unit, follow it; the name is the user's.
	meter.Kind, meter.Unit = sample.Kind, sample.Unit
	reading := core.Reading{At: o.At, Value: sample.Value, Limit: sample.Limit, ResetsAt: sample.ResetsAt}
	if err := record(ctx, tx, &meter, reading, sample.Window); err != nil {
		return meter, err
	}
	return meter, nil
}

// rulesJSON keeps a meter's rules as JSON: they are read whole and never queried.
func rulesJSON(r core.Rules) string { return mustJSON(r) }

func parseRules(raw string) core.Rules {
	var rules core.Rules
	_ = json.Unmarshal([]byte(raw), &rules) // a damaged value reads as "no rules"
	return rules
}

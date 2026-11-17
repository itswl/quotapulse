package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"quotapulse/internal/core"
	"quotapulse/internal/provider"
	"quotapulse/internal/secret"
)

const tokenColumns = `id, name, prefix, scopes, expires_at, last_used_at, created_at`

func scanToken(row interface{ Scan(...any) error }) (core.Token, error) {
	var (
		t                 core.Token
		scopes            string
		expires, lastUsed sql.NullInt64
		created           int64
	)
	err := row.Scan(&t.ID, &t.Name, &t.Prefix, &scopes, &expires, &lastUsed, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	_ = json.Unmarshal([]byte(scopes), &t.Scopes)
	if t.Scopes == nil {
		t.Scopes = []string{}
	}
	t.ExpiresAt, t.LastUsedAt, t.CreatedAt = timePtr(expires), timePtr(lastUsed), timeOf(created)
	return t, nil
}

// Tokens lists every token by name.
func (s *Store) Tokens(ctx context.Context) ([]core.Token, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+tokenColumns+" FROM tokens ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tokens := []core.Token{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// CreateToken mints a token and returns its plaintext, which is shown once
// and never stored.
func (s *Store) CreateToken(ctx context.Context, t *core.Token, now time.Time) (string, error) {
	plaintext := secret.NewToken()
	return plaintext, s.ImportToken(ctx, t, plaintext, now)
}

// ImportToken stores a token whose plaintext is chosen by the caller. A
// migration uses it so that keys already handed to agents keep working.
func (s *Store) ImportToken(ctx context.Context, t *core.Token, plaintext string, now time.Time) error {
	if t.ID == "" {
		t.ID = core.NewID("tok")
	}
	runes := []rune(plaintext)
	t.Prefix = string(runes[:min(len(runes), 6)]) + "…"
	t.CreatedAt = now.UTC().Truncate(time.Second)
	_, err := s.db.ExecContext(ctx, `INSERT INTO tokens (id, name, hash, prefix, scopes, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, secret.HashToken(plaintext), t.Prefix, mustJSON(t.Scopes), unixPtr(t.ExpiresAt), unix(t.CreatedAt))
	return conflict(err)
}

// RestoreToken stores a token from its hash alone. A backup carries hashes,
// never plaintext, and restoring one must leave every issued token working.
func (s *Store) RestoreToken(ctx context.Context, t *core.Token, hash, prefix string, now time.Time) error {
	if t.ID == "" {
		t.ID = core.NewID("tok")
	}
	t.Prefix = prefix
	t.CreatedAt = now.UTC().Truncate(time.Second)
	_, err := s.db.ExecContext(ctx, `INSERT INTO tokens (id, name, hash, prefix, scopes, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, hash, t.Prefix, mustJSON(t.Scopes), unixPtr(t.ExpiresAt), unix(t.CreatedAt))
	return conflict(err)
}

// TokenHashes maps token IDs to their stored hashes, for a backup.
func (s *Store) TokenHashes(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, hash FROM tokens")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return nil, err
		}
		out[id] = hash
	}
	return out, rows.Err()
}

// DeleteToken revokes a token.
func (s *Store) DeleteToken(ctx context.Context, id string) error {
	return affected(s.db.ExecContext(ctx, "DELETE FROM tokens WHERE id = ?", id))
}

// lastUsedGranularity is how stale last_used_at may be. An agent polling
// every few seconds should not turn every read into a write.
const lastUsedGranularity = time.Minute

// Authenticate finds the token for a plaintext, if it exists and has not
// expired, and notes that it was used.
func (s *Store) Authenticate(ctx context.Context, plaintext string, now time.Time) (core.Token, bool) {
	if plaintext == "" {
		return core.Token{}, false
	}
	token, err := scanToken(s.db.QueryRowContext(ctx,
		"SELECT "+tokenColumns+" FROM tokens WHERE hash = ?", secret.HashToken(plaintext)))
	if err != nil || token.Expired(now) {
		return core.Token{}, false
	}
	if token.LastUsedAt == nil || now.Sub(*token.LastUsedAt) >= lastUsedGranularity {
		_, _ = s.db.ExecContext(ctx, "UPDATE tokens SET last_used_at = ? WHERE id = ?", unix(now), token.ID)
	}
	return token, true
}

// ---------- custom provider definitions ----------

// CustomProviders lists the definitions saved from the dashboard.
func (s *Store) CustomProviders(ctx context.Context) ([]provider.Definition, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT definition FROM providers ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	defs := []provider.Definition{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var def provider.Definition
		if err := json.Unmarshal([]byte(raw), &def); err != nil {
			continue // a damaged row must not take the whole catalog down
		}
		def.Builtin = false
		defs = append(defs, def)
	}
	return defs, rows.Err()
}

// CustomProvider loads one saved definition.
func (s *Store) CustomProvider(ctx context.Context, id string) (provider.Definition, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, "SELECT definition FROM providers WHERE id = ?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return provider.Definition{}, ErrNotFound
	}
	if err != nil {
		return provider.Definition{}, err
	}
	var def provider.Definition
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		return def, fmt.Errorf("stored definition %s is not valid JSON: %w", id, err)
	}
	def.Builtin = false
	return def, nil
}

// SaveProvider inserts or replaces a saved definition.
func (s *Store) SaveProvider(ctx context.Context, def provider.Definition, now time.Time) error {
	def.Builtin, def.Checked = false, ""
	_, err := s.db.ExecContext(ctx, `INSERT INTO providers (id, definition, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET definition = excluded.definition, updated_at = excluded.updated_at`,
		def.ID, mustJSON(def), unix(now), unix(now))
	return err
}

// DeleteProvider removes a saved definition.
func (s *Store) DeleteProvider(ctx context.Context, id string) error {
	return affected(s.db.ExecContext(ctx, "DELETE FROM providers WHERE id = ?", id))
}

// ProviderInUse counts the accounts that use a definition.
func (s *Store) ProviderInUse(ctx context.Context, id string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts WHERE provider = ?", id).Scan(&n)
	return n, err
}

// ---------- upkeep ----------

// Prune deletes readings and closed incidents older than the cutoff, and
// finished deliveries with them. It returns how many readings went.
//
// The newest reading of every meter is kept whatever its age: it is the
// meter's current value, and a meter that has not moved in two years still
// has one.
func (s *Store) Prune(ctx context.Context, cutoff time.Time) (int64, error) {
	var removed int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM readings WHERE ts < ?1
			AND ts < (SELECT MAX(ts) FROM readings AS newest WHERE newest.meter_id = readings.meter_id)`, unix(cutoff))
		if err != nil {
			return err
		}
		removed, _ = result.RowsAffected()
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM incidents WHERE state = 'resolved' AND resolved_at < ?", unix(cutoff)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			"DELETE FROM deliveries WHERE status != 'pending' AND created_at < ?", unix(cutoff))
		return err
	})
	return removed, err
}

// Backup writes a consistent copy of the database to path. VACUUM INTO
// takes a snapshot without stopping writers, and the copy comes out compact.
func (s *Store) Backup(ctx context.Context, path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err := s.db.ExecContext(ctx, "VACUUM INTO ?", path)
	return err
}

// Stats describes the size of the database.
type Stats struct {
	Bytes      int64 `json:"bytes"`
	Accounts   int   `json:"accounts"`
	Meters     int   `json:"meters"`
	Readings   int   `json:"readings"`
	Reminders  int   `json:"reminders"`
	Incidents  int   `json:"incidents"`
	Deliveries int   `json:"deliveries"`
}

// Stats counts what is stored.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM accounts), (SELECT COUNT(*) FROM meters), (SELECT COUNT(*) FROM readings),
		(SELECT COUNT(*) FROM reminders), (SELECT COUNT(*) FROM incidents), (SELECT COUNT(*) FROM deliveries),
		(SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size())`).
		Scan(&st.Accounts, &st.Meters, &st.Readings, &st.Reminders, &st.Incidents, &st.Deliveries, &st.Bytes)
	return st, err
}

// Empty reports whether nothing has been configured yet: no accounts, no
// reminders, no channels. A fresh database is offered the bootstrap bundle.
func (s *Store) Empty(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM accounts) + (SELECT COUNT(*) FROM reminders) + (SELECT COUNT(*) FROM channels)
		+ (SELECT COUNT(*) FROM meters) + (SELECT COUNT(*) FROM tokens)`).Scan(&n)
	return n == 0, err
}

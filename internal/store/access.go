package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"time"
)

// The dashboard's password, when it is chosen in the dashboard rather than
// set by QUOTAPULSE_PASSWORD, and the one-time code that lets someone choose
// it. Neither is configuration: a bundle neither carries nor restores them.

const (
	// passwordKey holds the password's verifier (secret.Keyring.Verifier),
	// base64. Absent: the environment sets the password, or nobody has yet.
	passwordKey = "password"
	// setupKey holds the one-time setup code, hashed, and when it lapses.
	setupKey = "setup"
)

// PasswordVerifier returns what the password is checked against, or nil when
// none has been chosen in the dashboard.
func (s *Store) PasswordVerifier(ctx context.Context) ([]byte, error) {
	raw, ok, err := s.State(ctx, passwordKey)
	if err != nil || !ok {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(raw)
}

// SetPasswordVerifier keeps a newly chosen password. Any setup code goes with
// it: there is nothing left to set up.
func (s *Store) SetPasswordVerifier(ctx context.Context, verifier []byte) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
			passwordKey, base64.StdEncoding.EncodeToString(verifier)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM kv WHERE key = ?", setupKey)
		return err
	})
}

// ForgetPassword drops the password: every session ends, and nobody can sign
// in until someone with a setup code chooses a new one.
func (s *Store) ForgetPassword(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM kv WHERE key = ?", passwordKey)
	return err
}

// SetupCode is a one-time code that lets its holder choose the password. Only
// its hash is kept.
type SetupCode struct {
	Hash    string    `json:"hash"`
	Expires time.Time `json:"expires"`
}

// SetSetupCode replaces the setup code: there is only ever one.
func (s *Store) SetSetupCode(ctx context.Context, code SetupCode) error {
	raw, err := json.Marshal(code)
	if err != nil {
		return err
	}
	return s.SetState(ctx, setupKey, string(raw))
}

// SetupCode returns the current setup code, if there is one.
func (s *Store) SetupCode(ctx context.Context) (SetupCode, bool, error) {
	var code SetupCode
	raw, ok, err := s.State(ctx, setupKey)
	if err != nil || !ok {
		return code, false, err
	}
	if err := json.Unmarshal([]byte(raw), &code); err != nil {
		return code, false, err
	}
	return code, true, nil
}

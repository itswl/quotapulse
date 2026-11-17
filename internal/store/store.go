// Package store is QuotaPulse's memory: one SQLite file.
//
// SQLite is a deliberate choice, not a default. This is a single-user tool
// that samples a handful of accounts every half hour; a database server
// would be the largest moving part of the deployment and add nothing. One
// file also makes backup a copy and migration a move.
//
// The package speaks core types on the outside and SQL on the inside, and
// holds no business rules beyond what keeps the data consistent.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/secret"
)

// Errors the rest of the program branches on.
var (
	ErrNotFound = errors.New("not found")
	// ErrConflict means a unique name or key is already taken.
	ErrConflict = errors.New("already exists")
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store is the database handle.
type Store struct {
	db   *sql.DB
	keys *secret.Keyring
	path string
}

// Open opens the database at path, creating it and bringing its schema up to
// date. keys seals the credentials stored in it.
func Open(path string, keys *secret.Keyring) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create data directory: %w", err)
		}
	}
	// WAL lets the dashboard read while the sampler writes. busy_timeout
	// makes the rare writer collision wait instead of fail; immediate
	// transactions take the write lock up front, so two of them cannot
	// deadlock upgrading from read to write.
	query := url.Values{}
	// temp_store(2) keeps SQLite's scratch space in memory: the runtime image
	// has no /tmp for it to fall back on.
	for _, pragma := range []string{
		"busy_timeout(10000)", "journal_mode(WAL)", "synchronous(NORMAL)", "foreign_keys(1)", "temp_store(2)",
	} {
		query.Add("_pragma", pragma)
	}
	query.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+path+"?"+query.Encode())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)

	s := &Store{db: db, keys: keys, path: path}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	// The database names what someone depends on and when the people in
	// their life were born. It is nobody else's to read, whatever the umask
	// was when it was made. SQLite gives its side files the same mode.
	for _, file := range []string{path, path + "-wal", path + "-shm"} {
		_ = os.Chmod(file, 0o600)
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// Path is where the database file lives.
func (s *Store) Path() string { return s.path }

// migrate applies every migration newer than the schema's user_version.
func (s *Store) migrate(ctx context.Context) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > len(names) {
		return fmt.Errorf("the database is at schema version %d, newer than this build understands (%d); run a newer QuotaPulse",
			version, len(names))
	}
	for i := version; i < len(names); i++ {
		script, err := migrations.ReadFile(names[i])
		if err != nil {
			return err
		}
		err = s.tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(script)); err != nil {
				return err
			}
			// PRAGMA takes no parameters; the value is a loop counter.
			_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1))
			return err
		})
		if err != nil {
			return fmt.Errorf("apply %s: %w", filepath.Base(names[i]), err)
		}
	}
	return nil
}

// tx runs fn in a transaction, committing when it returns nil.
//
// fn must use the transaction it is given and nothing else: calling back into
// the Store from inside would wait on the write lock the transaction holds.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// querier is what *sql.DB and *sql.Tx have in common.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ---------- settings and small state ----------

const settingsKey = "settings"

// Settings loads the settings, starting from the defaults so a field added
// in a later version has a sensible value in an older database.
func (s *Store) Settings(ctx context.Context) (core.Settings, error) {
	settings := core.DefaultSettings()
	raw, ok, err := s.State(ctx, settingsKey)
	if err != nil || !ok {
		return settings, err
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return core.DefaultSettings(), fmt.Errorf("stored settings are not valid JSON: %w", err)
	}
	if settings.FX == nil {
		settings.FX = map[string]float64{}
	}
	return settings, nil
}

// SaveSettings validates and stores the settings.
func (s *Store) SaveSettings(ctx context.Context, settings core.Settings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.SetState(ctx, settingsKey, string(raw))
}

// State reads a small named value: bookkeeping such as when the last digest
// went out.
func (s *Store) State(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM kv WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// SetState writes a small named value.
func (s *Store) SetState(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

// Secret reads a named value that is stored sealed, into v. It reports
// false when there is none.
func (s *Store) Secret(ctx context.Context, key string, v any) (bool, error) {
	sealed, ok, err := s.State(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	if err := s.keys.OpenJSON([]byte(sealed), v); err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return true, nil
}

// SetSecret seals and stores a named value.
func (s *Store) SetSecret(ctx context.Context, key string, v any) error {
	sealed, err := s.keys.SealJSON(v)
	if err != nil {
		return err
	}
	return s.SetState(ctx, key, string(sealed))
}

// ---------- conversions ----------

func unix(t time.Time) int64 { return t.Unix() }

func unixPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}

func timeOf(seconds int64) time.Time { return time.Unix(seconds, 0).UTC() }

func timePtr(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := timeOf(n.Int64)
	return &t
}

func floatPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	return &n.Float64
}

func nullFloat(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func intPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}

func nullInt(n *int64) any {
	if n == nil {
		return nil
	}
	return *n
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("store: cannot encode %T: %v", v, err)) // only reachable with a type that cannot be JSON
	}
	return string(raw)
}

// conflict turns a unique-constraint failure into ErrConflict.
func conflict(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrConflict
	}
	return err
}

// affected turns "no row matched" into ErrNotFound.
func affected(result sql.Result, err error) error {
	if err != nil {
		return conflict(err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

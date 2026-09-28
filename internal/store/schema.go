package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
)

// Implementation note.
// Implementation note.
//go:generate go tool sqlc generate -f ../../sqlc.yaml

//go:embed schema/*.sql
var schemaFS embed.FS

// Implementation note.
// Implementation note.
func createTables(ctx context.Context, db *sql.DB, engine Engine) error {
	raw, err := schemaFS.ReadFile("schema/" + string(engine) + ".sql")
	if err != nil {
		return fmt.Errorf("Not found %s operation SQL:%w", engine, err)
	}
	for _, stmt := range splitStatements(string(raw)) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("Failed to create schema(%.60s...):%w", stmt, err)
		}
	}
	return nil
}

// Implementation note.
//
// Implementation note.
// Implementation note.
// ensureColumns adds columns that deployments created before a schema change are
// missing. CREATE TABLE IF NOT EXISTS only covers fresh installs, so every column added
// to an existing table lands here as an idempotent ALTER; the "duplicate column"
// errors from all three engines mean it is already there.
func ensureColumns(ctx context.Context, db *sql.DB) error {
	for _, stmt := range []string{
		"ALTER TABLE subscription_config ADD COLUMN snoozed_until VARCHAR(20)",
		"ALTER TABLE subscription_config ADD COLUMN timezone VARCHAR(50)",
		"ALTER TABLE subscription_config ADD COLUMN webhook_url TEXT",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "already exists") {
				continue
			}
			return fmt.Errorf("migration(%.80s): %w", stmt, err)
		}
	}
	return nil
}

func splitStatements(script string) []string {
	var lines []string
	for _, line := range strings.Split(script, "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "--") {
			continue // operation,operation
		}
		lines = append(lines, line)
	}

	var out []string
	for _, stmt := range strings.Split(strings.Join(lines, "\n"), ";") {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			out = append(out, stmt)
		}
	}
	return out
}

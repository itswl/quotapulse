package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
)

//go:generate go tool sqlc generate -f ../../sqlc.yaml

//go:embed schema/*.sql
var schemaFS embed.FS

// createTables creates tables at startup: all CREATE ... IF NOT EXISTS, a no-op
// against an existing production database that never touches existing schema or
// data.
func createTables(ctx context.Context, db *sql.DB, engine Engine) error {
	raw, err := schemaFS.ReadFile("schema/" + string(engine) + ".sql")
	if err != nil {
		return fmt.Errorf("Schema SQL for %s not found: %w", engine, err)
	}
	for _, stmt := range splitStatements(string(raw)) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("Failed to create schema(%.60s...):%w", stmt, err)
		}
	}
	return nil
}

// ensureColumns adds columns that deployments created before a schema change are
// missing. CREATE TABLE IF NOT EXISTS only covers fresh installs, so every column added
// to an existing table lands here as an idempotent ALTER; the "duplicate column"
// errors from all three engines mean it is already there.
func ensureColumns(ctx context.Context, db *sql.DB) error {
	for _, stmt := range []string{
		"ALTER TABLE subscription_config ADD COLUMN snoozed_until VARCHAR(20)",
		"ALTER TABLE subscription_config ADD COLUMN timezone VARCHAR(50)",
		"ALTER TABLE subscription_config ADD COLUMN webhook_url TEXT",
		"ALTER TABLE project_config ADD COLUMN currency VARCHAR(10)",
		"ALTER TABLE project_config ADD COLUMN monthly_budget DOUBLE PRECISION",
		"ALTER TABLE custom_provider_config ADD COLUMN subtract_path VARCHAR(200)",
		"ALTER TABLE custom_provider_config ADD COLUMN extract_mode VARCHAR(20)",
		"ALTER TABLE custom_provider_config ADD COLUMN window_types VARCHAR(100)",
		"ALTER TABLE custom_provider_config ADD COLUMN window_reset_hours DOUBLE",
		"ALTER TABLE custom_provider_config ADD COLUMN extra_params VARCHAR(400)",
		"ALTER TABLE custom_provider_config ADD COLUMN sign_region VARCHAR(50)",
		"ALTER TABLE custom_provider_config ADD COLUMN sign_service VARCHAR(50)",
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
			continue // comments may hold Chinese characters; keeping them feeds them to the driver as part of the statement
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

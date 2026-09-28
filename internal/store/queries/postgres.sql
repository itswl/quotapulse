-- NOTE: keep this file ASCII-only. sqlc computes byte offsets from rune positions
-- when slicing query text, so non-ASCII comments corrupt the parse. Design notes in Chinese
-- live in internal/store/schema/*.sql and in the Go adapters.

-- name: ListProjectConfigs :many
SELECT * FROM project_config ORDER BY id;

-- name: UpsertProjectConfig :exec
INSERT INTO project_config (
    name, owner_project, provider, api_key, threshold, type, enabled, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (name) DO UPDATE SET
    owner_project = excluded.owner_project,
    provider = excluded.provider,
    api_key = excluded.api_key,
    threshold = excluded.threshold,
    type = excluded.type,
    enabled = excluded.enabled,
    updated_at = excluded.updated_at;

-- name: DeleteProjectConfig :exec
DELETE FROM project_config WHERE name = $1;

-- name: UpdateProjectAPIKey :exec
UPDATE project_config SET api_key = $1 WHERE name = $2;

-- name: ListSubscriptionConfigs :many
SELECT * FROM subscription_config ORDER BY id;

-- name: UpsertSubscriptionConfig :exec
INSERT INTO subscription_config (
    name, owner_project, cycle_type, renewal_day, alert_days_before, amount, enabled,
    last_renewed_date, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (name) DO UPDATE SET
    owner_project = excluded.owner_project,
    cycle_type = excluded.cycle_type,
    renewal_day = excluded.renewal_day,
    alert_days_before = excluded.alert_days_before,
    amount = excluded.amount,
    enabled = excluded.enabled,
    last_renewed_date = excluded.last_renewed_date,
    updated_at = excluded.updated_at;

-- name: DeleteSubscriptionConfig :exec
DELETE FROM subscription_config WHERE name = $1;

-- name: ListEmailConfigs :many
SELECT * FROM email_config ORDER BY id;

-- name: UpsertEmailConfig :exec
INSERT INTO email_config (
    name, host, port, username, password, use_ssl, enabled, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (name) DO UPDATE SET
    host = excluded.host,
    port = excluded.port,
    username = excluded.username,
    password = excluded.password,
    use_ssl = excluded.use_ssl,
    enabled = excluded.enabled,
    updated_at = excluded.updated_at;

-- name: DeleteEmailConfig :exec
DELETE FROM email_config WHERE name = $1;

-- name: UpdateEmailPassword :exec
UPDATE email_config SET password = $1 WHERE name = $2;

-- name: InsertBalanceHistory :exec
INSERT INTO balance_history (
    project_id, project_name, provider, balance, threshold, balance_type, need_alarm, "timestamp"
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListBalanceSeries :many
SELECT * FROM balance_history
WHERE "timestamp" >= sqlc.arg(since)
ORDER BY "timestamp";

-- name: ListBalanceTrend :many
SELECT * FROM balance_history
WHERE project_id = sqlc.arg(project_id) AND "timestamp" >= sqlc.arg(since)
ORDER BY "timestamp";

-- name: ListBalanceHistory :many
SELECT * FROM balance_history
WHERE "timestamp" >= sqlc.arg(since)
  AND (sqlc.arg(project_id)::text = '' OR project_id = sqlc.arg(project_id)::text)
  AND (sqlc.arg(provider)::text = '' OR provider = sqlc.arg(provider)::text)
ORDER BY "timestamp" DESC
LIMIT sqlc.arg(row_limit);

-- name: InsertAlertHistory :exec
INSERT INTO alert_history (
    project_id, project_name, alert_type, status, message, balance_value, threshold_value, "timestamp"
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: CountRecentAlerts :one
SELECT COUNT(*) FROM alert_history
WHERE project_id = sqlc.arg(project_id)
  AND alert_type = sqlc.arg(alert_type)
  AND status = sqlc.arg(status)
  AND "timestamp" >= sqlc.arg(since);

-- name: ListAlertHistory :many
SELECT * FROM alert_history
WHERE "timestamp" >= sqlc.arg(since)
  AND (sqlc.arg(project_id)::text = '' OR project_id = sqlc.arg(project_id)::text)
  AND (sqlc.arg(alert_type)::text = '' OR alert_type = sqlc.arg(alert_type)::text)
ORDER BY "timestamp" DESC
LIMIT sqlc.arg(row_limit);

-- name: CountAlerts :one
SELECT COUNT(*) FROM alert_history WHERE "timestamp" >= sqlc.arg(since);

-- name: CountAlertsByType :many
SELECT alert_type, COUNT(*) AS count FROM alert_history
WHERE "timestamp" >= sqlc.arg(since)
GROUP BY alert_type;

-- name: CountAlertsByProject :many
SELECT project_name, COUNT(*) AS count FROM alert_history
WHERE "timestamp" >= sqlc.arg(since)
GROUP BY project_name
ORDER BY count DESC
LIMIT 10;

-- name: InsertEmailAlertHistory :exec
INSERT INTO email_alert_history (
    mailbox, sender, subject, date, service_name, amount, matched_keywords, alert_sent, "timestamp"
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: CountRecentEmailAlerts :one
SELECT COUNT(*) FROM email_alert_history
WHERE mailbox = sqlc.arg(mailbox)
  AND sender = sqlc.arg(sender)
  AND subject = sqlc.arg(subject)
  AND date = sqlc.arg(date)
  AND alert_sent = TRUE
  AND "timestamp" >= sqlc.arg(since);

-- name: ListEmailAlertHistory :many
SELECT * FROM email_alert_history
WHERE "timestamp" >= sqlc.arg(since)
  AND (sqlc.arg(mailbox)::text = '' OR mailbox = sqlc.arg(mailbox)::text)
ORDER BY "timestamp" DESC
LIMIT sqlc.arg(row_limit);

-- name: SetSubscriptionSnooze :exec
UPDATE subscription_config SET snoozed_until = $1 WHERE name = $2;

-- name: UpdateSubscriptionTimezone :exec
UPDATE subscription_config SET timezone = $1 WHERE name = $2;

-- name: UpdateSubscriptionWebhook :exec
UPDATE subscription_config SET webhook_url = $1 WHERE name = $2;

-- name: AddEmailSuppression :exec
INSERT INTO email_suppressions (mailbox, sender, created_at) VALUES ($1, $2, $3)
ON CONFLICT (mailbox, sender) DO NOTHING;

-- name: DeleteEmailSuppression :exec
DELETE FROM email_suppressions WHERE mailbox = $1 AND sender = $2;

-- name: ListEmailSuppressions :many
SELECT mailbox, sender FROM email_suppressions ORDER BY id;

-- name: ListPushSubscriptions :many
SELECT endpoint, p256dh, auth FROM push_subscriptions ORDER BY id;

-- name: UpsertPushSubscription :exec
INSERT INTO push_subscriptions (endpoint, p256dh, auth, created_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (endpoint) DO UPDATE SET
    p256dh = excluded.p256dh,
    auth = excluded.auth,
    created_at = excluded.created_at;

-- name: DeletePushSubscription :exec
DELETE FROM push_subscriptions WHERE endpoint = $1;

-- name: GetAppSetting :one
SELECT setting_value FROM app_settings WHERE setting_key = $1;

-- name: SetAppSetting :exec
INSERT INTO app_settings (setting_key, setting_value) VALUES ($1, $2)
ON CONFLICT (setting_key) DO UPDATE SET setting_value = excluded.setting_value;


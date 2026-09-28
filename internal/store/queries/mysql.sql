-- NOTE: keep this file ASCII-only. sqlc computes byte offsets from rune positions
-- when slicing query text, so non-ASCII comments corrupt the parse. Design notes in Chinese
-- live in internal/store/schema/*.sql and in the Go adapters.

-- name: ListProjectConfigs :many
SELECT * FROM project_config ORDER BY id;

-- name: UpsertProjectConfig :exec
INSERT INTO project_config (
    name, owner_project, provider, api_key, threshold, type, enabled, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    owner_project = VALUES(owner_project),
    provider = VALUES(provider),
    api_key = VALUES(api_key),
    threshold = VALUES(threshold),
    type = VALUES(type),
    enabled = VALUES(enabled),
    updated_at = VALUES(updated_at);

-- name: DeleteProjectConfig :exec
DELETE FROM project_config WHERE name = ?;

-- name: UpdateProjectAPIKey :exec
UPDATE project_config SET api_key = ? WHERE name = ?;

-- name: ListSubscriptionConfigs :many
SELECT * FROM subscription_config ORDER BY id;

-- name: UpsertSubscriptionConfig :exec
INSERT INTO subscription_config (
    name, owner_project, cycle_type, renewal_day, alert_days_before, amount, enabled,
    last_renewed_date, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    owner_project = VALUES(owner_project),
    cycle_type = VALUES(cycle_type),
    renewal_day = VALUES(renewal_day),
    alert_days_before = VALUES(alert_days_before),
    amount = VALUES(amount),
    enabled = VALUES(enabled),
    last_renewed_date = VALUES(last_renewed_date),
    updated_at = VALUES(updated_at);

-- name: DeleteSubscriptionConfig :exec
DELETE FROM subscription_config WHERE name = ?;

-- name: ListEmailConfigs :many
SELECT * FROM email_config ORDER BY id;

-- name: UpsertEmailConfig :exec
INSERT INTO email_config (
    name, host, port, username, password, use_ssl, enabled, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    host = VALUES(host),
    port = VALUES(port),
    username = VALUES(username),
    password = VALUES(password),
    use_ssl = VALUES(use_ssl),
    enabled = VALUES(enabled),
    updated_at = VALUES(updated_at);

-- name: DeleteEmailConfig :exec
DELETE FROM email_config WHERE name = ?;

-- name: UpdateEmailPassword :exec
UPDATE email_config SET password = ? WHERE name = ?;

-- name: InsertBalanceHistory :exec
INSERT INTO balance_history (
    project_id, project_name, provider, balance, threshold, balance_type, need_alarm, `timestamp`
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListBalanceSeries :many
SELECT * FROM balance_history
WHERE `timestamp` >= sqlc.arg(since)
ORDER BY `timestamp`;

-- name: ListBalanceTrend :many
SELECT * FROM balance_history
WHERE project_id = sqlc.arg(project_id) AND `timestamp` >= sqlc.arg(since)
ORDER BY `timestamp`;

-- name: ListBalanceHistory :many
SELECT * FROM balance_history
WHERE `timestamp` >= sqlc.arg(since)
  AND (sqlc.arg(project_id) = '' OR project_id = sqlc.arg(project_id))
  AND (sqlc.arg(provider) = '' OR provider = sqlc.arg(provider))
ORDER BY `timestamp` DESC
LIMIT ?;

-- name: InsertAlertHistory :exec
INSERT INTO alert_history (
    project_id, project_name, alert_type, status, message, balance_value, threshold_value, `timestamp`
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: CountRecentAlerts :one
SELECT COUNT(*) FROM alert_history
WHERE project_id = sqlc.arg(project_id)
  AND alert_type = sqlc.arg(alert_type)
  AND status = sqlc.arg(status)
  AND `timestamp` >= sqlc.arg(since);

-- name: ListAlertHistory :many
SELECT * FROM alert_history
WHERE `timestamp` >= sqlc.arg(since)
  AND (sqlc.arg(project_id) = '' OR project_id = sqlc.arg(project_id))
  AND (sqlc.arg(alert_type) = '' OR alert_type = sqlc.arg(alert_type))
ORDER BY `timestamp` DESC
LIMIT ?;

-- name: CountAlerts :one
SELECT COUNT(*) FROM alert_history WHERE `timestamp` >= sqlc.arg(since);

-- name: CountAlertsByType :many
SELECT alert_type, COUNT(*) AS count FROM alert_history
WHERE `timestamp` >= sqlc.arg(since)
GROUP BY alert_type;

-- name: CountAlertsByProject :many
SELECT project_name, COUNT(*) AS count FROM alert_history
WHERE `timestamp` >= sqlc.arg(since)
GROUP BY project_name
ORDER BY count DESC
LIMIT 10;

-- name: InsertEmailAlertHistory :exec
INSERT INTO email_alert_history (
    mailbox, sender, subject, date, service_name, amount, matched_keywords, alert_sent, `timestamp`
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: CountRecentEmailAlerts :one
SELECT COUNT(*) FROM email_alert_history
WHERE mailbox = sqlc.arg(mailbox)
  AND sender = sqlc.arg(sender)
  AND subject = sqlc.arg(subject)
  AND date = sqlc.arg(date)
  AND alert_sent = TRUE
  AND `timestamp` >= sqlc.arg(since);

-- name: ListEmailAlertHistory :many
SELECT * FROM email_alert_history
WHERE `timestamp` >= sqlc.arg(since)
  AND (sqlc.arg(mailbox) = '' OR mailbox = sqlc.arg(mailbox))
ORDER BY `timestamp` DESC
LIMIT ?;

-- name: SetSubscriptionSnooze :exec
UPDATE subscription_config SET snoozed_until = ? WHERE name = ?;

-- name: UpdateSubscriptionTimezone :exec
UPDATE subscription_config SET timezone = ? WHERE name = ?;

-- name: UpdateSubscriptionWebhook :exec
UPDATE subscription_config SET webhook_url = ? WHERE name = ?;

-- name: AddEmailSuppression :exec
INSERT IGNORE INTO email_suppressions (mailbox, sender, created_at) VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE id = id;

-- name: DeleteEmailSuppression :exec
DELETE FROM email_suppressions WHERE mailbox = ? AND sender = ?;

-- name: ListEmailSuppressions :many
SELECT mailbox, sender FROM email_suppressions ORDER BY id;

-- name: ListPushSubscriptions :many
SELECT endpoint, p256dh, auth FROM push_subscriptions ORDER BY id;

-- name: UpsertPushSubscription :exec
INSERT INTO push_subscriptions (endpoint, p256dh, auth, created_at)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE p256dh = VALUES(p256dh), auth = VALUES(auth), created_at = VALUES(created_at);

-- name: DeletePushSubscription :exec
DELETE FROM push_subscriptions WHERE endpoint = ?;

-- name: GetAppSetting :one
SELECT setting_value FROM app_settings WHERE setting_key = ?;

-- name: SetAppSetting :exec
INSERT INTO app_settings (setting_key, setting_value) VALUES (?, ?)
ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value);


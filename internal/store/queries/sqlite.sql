-- name: ListProjectConfigs :many
SELECT * FROM project_config ORDER BY id;

-- name: UpsertProjectConfig :exec
INSERT INTO project_config (
    name, owner_project, provider, api_key, threshold, type, currency, monthly_budget, enabled, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET
    owner_project = excluded.owner_project,
    provider = excluded.provider,
    api_key = excluded.api_key,
    threshold = excluded.threshold,
    type = excluded.type,
    currency = excluded.currency,
    monthly_budget = excluded.monthly_budget,
    enabled = excluded.enabled,
    updated_at = excluded.updated_at;

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
DELETE FROM subscription_config WHERE name = ?;

-- name: ListEmailConfigs :many
SELECT * FROM email_config ORDER BY id;

-- name: UpsertEmailConfig :exec
INSERT INTO email_config (
    name, host, port, username, password, use_ssl, enabled, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET
    host = excluded.host,
    port = excluded.port,
    username = excluded.username,
    password = excluded.password,
    use_ssl = excluded.use_ssl,
    enabled = excluded.enabled,
    updated_at = excluded.updated_at;

-- name: DeleteEmailConfig :exec
DELETE FROM email_config WHERE name = ?;

-- name: UpdateEmailPassword :exec
UPDATE email_config SET password = ? WHERE name = ?;

-- name: InsertBalanceHistory :exec
INSERT INTO balance_history (
    project_id, project_name, provider, balance, threshold, balance_type, need_alarm, timestamp
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListBalanceSeries :many
SELECT * FROM balance_history
WHERE timestamp >= sqlc.arg(since)
ORDER BY timestamp;

-- name: ListBalanceTrend :many
SELECT * FROM balance_history
WHERE project_id = sqlc.arg(project_id) AND timestamp >= sqlc.arg(since)
ORDER BY timestamp;

-- name: ListBalanceHistory :many
SELECT * FROM balance_history
WHERE timestamp >= sqlc.arg(since)
  AND (CAST(sqlc.arg(project_id) AS TEXT) = '' OR project_id = sqlc.arg(project_id))
  AND (CAST(sqlc.arg(provider) AS TEXT) = '' OR provider = sqlc.arg(provider))
ORDER BY timestamp DESC
LIMIT sqlc.arg(row_limit);

-- name: InsertAlertHistory :exec
INSERT INTO alert_history (
    project_id, project_name, alert_type, status, message, balance_value, threshold_value, timestamp
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: CountRecentAlerts :one
SELECT COUNT(*) FROM alert_history
WHERE project_id = sqlc.arg(project_id)
  AND alert_type = sqlc.arg(alert_type)
  AND status = sqlc.arg(status)
  AND timestamp >= sqlc.arg(since);

-- name: ListAlertHistory :many
SELECT * FROM alert_history
WHERE timestamp >= sqlc.arg(since)
  AND (CAST(sqlc.arg(project_id) AS TEXT) = '' OR project_id = sqlc.arg(project_id))
  AND (CAST(sqlc.arg(alert_type) AS TEXT) = '' OR alert_type = sqlc.arg(alert_type))
ORDER BY timestamp DESC
LIMIT sqlc.arg(row_limit);

-- name: CountAlerts :one
SELECT COUNT(*) FROM alert_history WHERE timestamp >= sqlc.arg(since);

-- name: CountAlertsByType :many
SELECT alert_type, COUNT(*) AS count FROM alert_history
WHERE timestamp >= sqlc.arg(since)
GROUP BY alert_type;

-- name: CountAlertsByProject :many
SELECT project_name, COUNT(*) AS count FROM alert_history
WHERE timestamp >= sqlc.arg(since)
GROUP BY project_name
ORDER BY count DESC
LIMIT 10;

-- name: InsertEmailAlertHistory :exec
INSERT INTO email_alert_history (
    mailbox, sender, subject, date, service_name, amount, matched_keywords, alert_sent, timestamp
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: CountRecentEmailAlerts :one
SELECT COUNT(*) FROM email_alert_history
WHERE mailbox = sqlc.arg(mailbox)
  AND sender = sqlc.arg(sender)
  AND subject = sqlc.arg(subject)
  AND date = sqlc.arg(date)
  AND alert_sent = TRUE
  AND timestamp >= sqlc.arg(since);

-- name: ListEmailAlertHistory :many
SELECT * FROM email_alert_history
WHERE timestamp >= sqlc.arg(since)
  AND (CAST(sqlc.arg(mailbox) AS TEXT) = '' OR mailbox = sqlc.arg(mailbox))
ORDER BY timestamp DESC
LIMIT sqlc.arg(row_limit);

-- name: SetSubscriptionSnooze :exec
UPDATE subscription_config SET snoozed_until = ? WHERE name = ?;

-- name: UpdateSubscriptionTimezone :exec
UPDATE subscription_config SET timezone = ? WHERE name = ?;

-- name: UpdateSubscriptionWebhook :exec
UPDATE subscription_config SET webhook_url = ? WHERE name = ?;

-- name: AddEmailSuppression :exec
INSERT INTO email_suppressions (mailbox, sender, created_at) VALUES (?, ?, ?)
ON CONFLICT (mailbox, sender) DO NOTHING;

-- name: DeleteEmailSuppression :exec
DELETE FROM email_suppressions WHERE mailbox = ? AND sender = ?;

-- name: ListEmailSuppressions :many
SELECT mailbox, sender FROM email_suppressions ORDER BY id;

-- name: ListPushSubscriptions :many
SELECT endpoint, p256dh, auth FROM push_subscriptions ORDER BY id;

-- name: UpsertPushSubscription :exec
INSERT INTO push_subscriptions (endpoint, p256dh, auth, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (endpoint) DO UPDATE SET
    p256dh = excluded.p256dh,
    auth = excluded.auth,
    created_at = excluded.created_at;

-- name: DeletePushSubscription :exec
DELETE FROM push_subscriptions WHERE endpoint = ?;

-- name: GetAppSetting :one
SELECT setting_value FROM app_settings WHERE setting_key = ?;

-- name: SetAppSetting :exec
INSERT INTO app_settings (setting_key, setting_value) VALUES (?, ?)
ON CONFLICT (setting_key) DO UPDATE SET setting_value = excluded.setting_value;

-- name: ListCustomProviders :many
SELECT * FROM custom_provider_config ORDER BY id;

-- name: UpsertCustomProvider :exec
INSERT INTO custom_provider_config (
    name, display_name, url, auth_mode, auth_param, default_type, currency, balance_path, balance_scale, success_path, success_values, error_path, subtract_path, extract_mode, window_types, window_reset_hours, extra_params, sign_region, sign_service, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET
    display_name = excluded.display_name,
    url = excluded.url,
    auth_mode = excluded.auth_mode,
    auth_param = excluded.auth_param,
    default_type = excluded.default_type,
    currency = excluded.currency,
    balance_path = excluded.balance_path,
    balance_scale = excluded.balance_scale,
    success_path = excluded.success_path,
    success_values = excluded.success_values,
    error_path = excluded.error_path,
    subtract_path = excluded.subtract_path,
    extract_mode = excluded.extract_mode,
    window_types = excluded.window_types,
    window_reset_hours = excluded.window_reset_hours,
    extra_params = excluded.extra_params,
    sign_region = excluded.sign_region,
    sign_service = excluded.sign_service,
    updated_at = excluded.updated_at;

-- name: DeleteCustomProvider :exec
DELETE FROM custom_provider_config WHERE name = ?;

-- name: UpsertUsageEvent :exec
INSERT INTO usage_events (source, project_name, entry_date, model, cost, currency, input_tokens, output_tokens, requests, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (source, project_name, entry_date, model) DO UPDATE SET
cost = excluded.cost, currency = excluded.currency, input_tokens = excluded.input_tokens, output_tokens = excluded.output_tokens, requests = excluded.requests, updated_at = excluded.updated_at;


# API

All `/api/*` endpoints require `X-API-Key: <WEB_API_KEY>` or `Authorization: Bearer <key>`. `/health` and `/live` need no key. `/mcp` also accepts the keys in `MCP_API_KEYS`, which work nowhere else; see the README's MCP section. Responses are JSON. Errors use `{"status":"error","message":"..." }`; validation errors may also include an `errors` array.

| Status | Meaning |
| --- | --- |
| 400 | Invalid request parameters |
| 401 | Missing, invalid, or expired API key |
| 404 | Resource not found or the corresponding capability is disabled |
| 409 | The project a refresh names is disabled, so it is not checked |
| 429 | The same refresh or scan is already running, or is cooling down; on `/mcp`, the key exceeded `MCP_RATE_LIMIT_PER_MINUTE` (see `Retry-After`) |
| 503 | `WEB_API_KEY` is unset, a capability is disabled, or the service is not ready |

## Endpoints

| Method and path | Description | Required feature |
| --- | --- | --- |
| `GET /live` | Liveness check | — |
| `GET /health` | Readiness check; requires data, fresh results, and successful scheduled jobs | — |
| `GET /api/features` | Enabled optional capabilities | — |
| `GET /api/credits` | Current balance state for all projects | — |
| `GET/POST /api/refresh` | Run an immediate balance check; POST accepts `project_name` (404 if unknown, 409 if disabled); one run at a time, with a 30-second cooldown | — |
| `POST /api/notify/test` | Sends a canary message through the configured webhook and returns the delivery error verbatim, so the channel can be verified without waiting for a real alert | `WEB_API_KEY` |
| `POST /api/subscription/snooze` | Postpones one subscription's reminders by `days` (1-365) without marking it renewed | `ENABLE_DYNAMIC_CONFIG` |
| `POST /api/subscription/timezone` | Evaluates one subscription in `timezone` (IANA name); empty resets to server time | `ENABLE_DYNAMIC_CONFIG` |
| `POST /api/subscription/webhook` | Points one subscription at its own webhook URL; empty resets to the global webhook | `ENABLE_DYNAMIC_CONFIG` |
| `GET /api/email/suppressions` | Lists the mailbox+sender pairs muted as false positives | History API |
| `GET /api/push/config` | Returns the VAPID public key browsers subscribe with (generated and persisted on first use) | `WEB_API_KEY` |
| `POST /api/push/subscribe` | Registers or refreshes one browser's Web Push subscription (`endpoint`, `keys.p256dh`, `keys.auth`) | `WEB_API_KEY` |
| `POST /api/push/unsubscribe` | Removes one browser's push registration by `endpoint` | `WEB_API_KEY` |
| `POST /api/email/suppression` | Mutes a mailbox+sender pair; matched emails stay in history but stop notifying | History API |
| `POST /api/email/suppression/delete` | Removes a suppression so the sender notifies again | History API |
| `GET /api/events` | Unified event timeline merging alert history and email alerts, newest first; accepts `days`, `limit`, `type` | History API |
| `GET /api/jobs` | Scheduled job status and run details | — |
| `GET /api/subscriptions` | Current subscription status | Subscriptions |
| `GET /api/config/subscriptions` | Subscription configuration | Subscriptions |
| `POST /api/subscription/add` | Create a subscription; `cycle_type` supports `weekly`, `monthly`, `yearly`, and `lunar_yearly` | Subscriptions |
| `POST /api/config/subscription` | Update a subscription; `name` identifies it and `new_name` renames it | Subscriptions |
| `POST` or `DELETE /api/subscription/delete` | Delete `{"name":"..." }` | Subscriptions |
| `POST /api/subscription/mark_renewed` | Marks a renewal as paid; `renewed_date` defaults to today. A mark pays for one renewal: the one whose reminder window the date falls in, otherwise the nearer one. Returns `next_renewal_date`, the next renewal that will be reminded about | Subscriptions, dynamic configuration |
| `POST /api/subscription/clear_renewed` | Removes the renewal mark from `{"name":"..."}` | Subscriptions, dynamic configuration |
| `GET /api/providers` | Supported providers and default balance types | — |
| `GET /api/config/projects` | Project configuration with redacted keys | — |
| `GET /api/config/providers` | Custom (page-defined) platforms | — |
| `POST /api/config/provider` | Create or update a custom platform: one GET, a `balance_path` JSON path (dots for fields, numbers for array indexes, `{field:value}` picks an array element with first-element fallback, `a \| b` tries alternatives in order; a text value yields its first number), optional `subtract_path` (balance − field), `balance_scale`, `auth_mode` (`bearer`/`query`/`header`/`none`/`aliyun_rpc`/`volc_v4` + `auth_param`), `default_type`, `currency`, optional `success_path`/`success_values`/`error_path` validation, `extract_mode=min_window` (lowest remaining percent across quota windows resetting within `window_reset_hours`, GLM Coding Plan shape) with `window_types`, fixed `extra_params` (`k=v` CSV), and `sign_region`/`sign_service` for `volc_v4` | Dynamic configuration |
| `POST /api/config/provider/test` | Runs a (possibly unsaved) definition once against its real endpoint with the given `api_key`; returns the extracted balance or the failure verbatim | Dynamic configuration |
| `POST /api/config/provider/delete` | Delete `{"name":"..."}`; refused while a project still uses it | Dynamic configuration |
| `POST /api/usage/ingest` | Report local usage: `{source, project, entries:[{date, model, cost, currency, input_tokens, output_tokens, requests}]}`; cells key on source+project+date+model so re-posting refreshes | Database |
| `GET /api/costs/summary` | Monthly cost view: per-project month-to-date spend (native and base currency), budget pace and month-end projection; accepts `month=YYYY-MM` | History API |
| `POST /api/config/project` | Create or update a project; empty `api_key` preserves an existing key | Dynamic configuration |
| `POST /api/config/project/delete` | Delete `{"name":"..." }`; auto-discovered projects cannot be deleted | Dynamic configuration |
| `POST /api/config/threshold` | Update only a threshold: `{"project_name":"...","new_threshold":0}` | Dynamic configuration |
| `GET /api/config/emails` | Mailbox configuration with redacted passwords | — |
| `POST /api/config/email` | Create or update a mailbox; empty password preserves an existing password | Dynamic configuration |
| `POST /api/config/email/delete` | Delete `{"name":"..." }`; auto-discovered mailboxes cannot be deleted | Dynamic configuration |
| `GET /api/email/scan` | Most recent scan result; in-memory and cleared on restart | — |
| `POST /api/email/scan` | Scan `{"days":1}` through `{"days":30}`; one run at a time, with a 30-second cooldown | — |
| `GET /api/history/balance` | Balance history; accepts `days`, `limit`, `project_id`, and `provider` | History API |
| `GET /api/history/trend/<project_id>` | Trend summary for one project | History API |
| `GET /api/history/alerts` | Alert history; accepts `days` and `limit` | History API |
| `GET /api/history/stats` | History statistics | History API |
| `GET /api/history/email-alerts` | Email alert history; accepts `days`, `limit`, and `mailbox` | History API |
| `POST /mcp` | Read-only, stateless Streamable HTTP MCP endpoint: balances and runways, subscriptions, mailbox scans, redacted configuration views, history, trends, events, alert statistics, jobs, health, and the provider catalog. A key sees only the tools of its scopes; results are structured and never cacheable by intermediaries | `ENABLE_MCP` |

Subscriptions require `ENABLE_SUBSCRIPTIONS`. `/api/features` reports `email_scan` once a mailbox is configured (with a scan schedule), or always with `ENABLE_EMAIL_SCAN=true`. Dynamic writes require `ENABLE_DYNAMIC_CONFIG` and `ENABLE_DATABASE`. History endpoints require `ENABLE_HISTORY_API`. Dashboard-triggered refreshes and scans send real notifications only when `ENABLE_WEB_ALARM=true`.

## Examples

```bash
curl -H "X-API-Key: $WEB_API_KEY" http://localhost:8080/api/credits
curl -X POST -H "X-API-Key: $WEB_API_KEY" http://localhost:8080/api/refresh
curl -X POST -H "X-API-Key: $WEB_API_KEY" -H "Content-Type: application/json" \
  -d '{"days":3}' http://localhost:8080/api/email/scan
curl -X POST -H "X-API-Key: $WEB_API_KEY" -H "Content-Type: application/json" \
  -d '{"name":"Streaming","cycle_type":"monthly","renewal_day":15,"alert_days_before":3,"amount":99}' \
  http://localhost:8080/api/subscription/add
```

## Response examples

### `GET /api/credits`

Runway fields are present only when `ENABLE_DATABASE` is enabled and enough history exists. With `confidence: "none"`, estimates are unavailable; `low` means the data spans less than one day and is not used for alerts.

```json
{
  "last_update": "2026-09-14T03:35:17Z",
  "projects": [
    {
      "project": "example-project", "provider": "deepseek", "type": "balance",
      "success": true, "credits": 430.37, "threshold": 50,
      "need_alarm": false, "alarm_sent": false, "error": null, "cached": false,
      "runway": {
        "window_days": 7, "data_points": 168, "span_hours": 167,
        "confidence": "high", "consumed": 437.5, "topped_up": 0,
        "burn_per_day": 62.5, "runway_days": 6.89,
        "depletion_date": "2026-09-21", "today_consumed": 58.2,
        "baseline_consumed": 61, "spike_ratio": 0.95,
        "daily": [{"date":"2026-09-08","consumed":61}]
      }
    }
  ],
  "summary": {"total":1,"success":1,"failed":0,"need_alarm":0}
}
```

### `GET /health`

```json
{
  "status":"healthy", "has_data":true, "is_stale":false,
  "jobs_healthy":true, "failed_jobs":[],
  "last_update":"2026-09-14T03:35:17Z", "uptime_seconds":3600, "version":"1.0.0"
}
```

### `GET /api/jobs`

```json
{
  "healthy": true,
  "jobs": [{
    "name":"alert_check", "description":"Balance and subscription alert check",
    "schedule":"Daily at 09:00 / 15:00", "enabled":true,
    "next_run":"2026-09-15T01:00:00Z", "last_run":"2026-09-14T07:00:00Z",
    "last_success":"2026-09-14T07:00:00Z", "last_error":null,
    "last_duration_seconds":1.42,
    "last_detail":{"projects":8,"failed":0,"need_alarm":1,"subscriptions":2,"need_alert":0,"dry_run":false},
    "runs":12, "failures":0
  }]
}
```

### `GET /api/subscriptions`

```json
{
  "last_update": "2026-09-14T03:35:17Z",
  "subscriptions": [{
    "name": "Claude Max", "renewal_day": 15, "cycle_type": "monthly",
    "days_until_renewal": 3, "next_renewal_date": "2026-09-15",
    "need_alert": true, "alert_sent": false, "amount": 100,
    "already_renewed": false, "last_renewed_date": null
  }]
}
```

### `GET /api/config/projects`

```json
{
  "status": "success",
  "projects": [{
    "name": "deepseek", "provider": "deepseek", "type": "balance",
    "api_key": "sk-1***abcd", "threshold": 50,
    "owner_project": null, "enabled": true, "from_env": false
  }]
}
```

### `GET /api/history/trend/<project_id>`

`project_id` is the `md5("provider:name")` identity, or the raw `provider:name` (URL-encoded). Fewer than two points yield no `change` fields.

```json
{
  "status": "success",
  "project_id": "5f4dcc3b5aa765d61d8327deb882cf99",
  "project_name": "deepseek", "provider": "deepseek",
  "points": 20,
  "first": {"timestamp": "2026-09-13T00:00:00Z", "balance": 500},
  "last": {"timestamp": "2026-09-14T03:00:00Z", "balance": 437.5},
  "change": -62.5, "change_percent": -12.5,
  "history": [{"timestamp": "2026-09-14T03:00:00Z", "balance": 437.5}]
}
```

### `GET /api/email/scan`

```json
{
  "last_update":"2026-09-14T03:00:00Z", "days":3, "dry_run":true,
  "mailboxes":[{
    "name":"work-mailbox", "host":"imap.example.com", "port":993,
    "username":"me@example.com", "total_emails":12, "alert_count":1,
    "success":true, "error":null
  }],
  "alerts":[{
    "mailbox":"work-mailbox", "subject":"Low balance notification",
    "sender":"noreply@example.com", "date":"Mon, 01 Sep 2026 10:00:00 +0800",
    "keywords":["low balance"], "service_name":"Example Cloud",
    "amount":12.5, "alert_sent":false
  }],
  "summary":{"total_mailboxes":1,"failed_mailboxes":0,"total_emails":12,"total_alerts":1,"alerts_sent":0}
}
```

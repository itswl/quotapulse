# API

All `/api/*` endpoints require `X-API-Key: <WEB_API_KEY>` or `Authorization: Bearer <key>`. `/health`, `/live`, and the MCP endpoint use their documented authentication rules. Responses are JSON. Errors use `{"status":"error","message":"..." }`; validation errors may also include an `errors` array.

| Status | Meaning |
| --- | --- |
| 400 | Invalid request parameters |
| 401 | Missing or invalid API key |
| 404 | Resource not found or the corresponding capability is disabled |
| 429 | The same refresh or scan is already running, or is cooling down |
| 503 | `WEB_API_KEY` is unset, a capability is disabled, or the service is not ready |

## Endpoints

| Method and path | Description | Required feature |
| --- | --- | --- |
| `GET /live` | Liveness check | — |
| `GET /health` | Readiness check; requires data, fresh results, and successful scheduled jobs | — |
| `GET /api/features` | Enabled optional capabilities | — |
| `GET /api/credits` | Current balance state for all projects | — |
| `GET/POST /api/refresh` | Run an immediate balance check; POST accepts `project_name`; one run at a time, with a 30-second cooldown | — |
| `GET /api/jobs` | Scheduled job status and run details | — |
| `GET /api/subscriptions` | Current subscription status | Subscriptions |
| `GET /api/config/subscriptions` | Subscription configuration | Subscriptions |
| `POST /api/subscription/add` | Create a subscription; `cycle_type` supports `weekly`, `monthly`, `yearly`, and `lunar_yearly` | Subscriptions |
| `POST /api/config/subscription` | Update a subscription; `name` identifies it and `new_name` renames it | Subscriptions |
| `POST` or `DELETE /api/subscription/delete` | Delete `{"name":"..." }` | Subscriptions |
| `POST /api/subscription/mark_renewed` | Mark or clear renewal status; accepts optional `renewed_date` | Subscriptions |
| `GET /api/providers` | Supported providers and default balance types | — |
| `GET /api/config/projects` | Project configuration with redacted keys | — |
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
| `POST /mcp` | Read-only Streamable HTTP MCP endpoint | `ENABLE_MCP` |

Subscriptions require `ENABLE_SUBSCRIPTIONS`. Dynamic writes require `ENABLE_DYNAMIC_CONFIG` and `ENABLE_DATABASE`. History endpoints require `ENABLE_HISTORY_API`. Dashboard-triggered refreshes and scans send real notifications only when `ENABLE_WEB_ALARM=true`.

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

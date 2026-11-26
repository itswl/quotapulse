# The HTTP API, for agents without the MCP tools

Use the API when the MCP tools are not connected. Every request carries the
token:

```bash
qp() { curl -sS -H "Authorization: Bearer $QUOTAPULSE_TOKEN" -H "Content-Type: application/json" "$@"; }
qp "$QUOTAPULSE_URL/api/v1/overview?lang=zh" | jq .
```

How the API behaves:

- **Answers.** Everything answers JSON. Lists come as `{"items": [...]}`.
- **Errors** come as `{"error": {"code", "message"}}`. The `message` is a
  sentence meant for a person.
- **Language.** `?lang=zh` or `?lang=en` picks the language of headlines and
  details. The MCP tools are asked the same way, with the query on their URL.
- **Unknown fields are refused.** A misspelt field is a 400, not a setting that
  is silently ignored.
- **Partial updates.** `PATCH` changes only the fields that are sent.

## The MCP tools, over HTTP

Every tool is one of these requests, so the same token works either way. The
tool takes its scope's row; `admin` carries them all.

| Scope | MCP tool | Request |
| --- | --- | --- |
| `read` | `attention` | `GET /api/v1/overview` |
| `read` | `list_meters` | `GET /api/v1/meters` |
| `read` | `get_meter` | `GET /api/v1/meters/{id}`, then `GET /api/v1/meters/{id}/series?range=30d` for its spend per day |
| `read` | `list_dates` | `GET /api/v1/reminders`, soonest first |
| `read` | `get_spend` | `GET /api/v1/spend?month=YYYY-MM` |
| `read` | `list_activity` | `GET /api/v1/incidents?state=open&limit=20` |
| `read` | `list_accounts` | `GET /api/v1/accounts` |
| `read` | `list_deliveries` | `GET /api/v1/deliveries` |
| `read` | `get_settings` | `GET /api/v1/settings` |
| `read` | `preview_date` | `POST /api/v1/reminders/preview` |
| `read` | `list_channel_types` | `GET /api/v1/channel-types` |
| `read` | `list_providers` | `GET /api/v1/providers` |
| `act` | `add_date` | `POST /api/v1/reminders` |
| `act` | `update_date` | `PATCH /api/v1/reminders/{id}` |
| `act` | `delete_date` | `DELETE /api/v1/reminders/{id}` |
| `act` | `complete_date` | `POST /api/v1/reminders/{id}/done` |
| `act` | `reopen_date` | `DELETE /api/v1/reminders/{id}/done` |
| `act` | `snooze_date` | `PUT /api/v1/reminders/{id}/snooze` |
| `act` | `unsnooze_date` | `DELETE /api/v1/reminders/{id}/snooze` |
| `act` | `acknowledge` | `POST /api/v1/incidents/{id}/ack` |
| `act` | `unacknowledge` | `DELETE /api/v1/incidents/{id}/ack` |
| `act` | `mute_meter` | `PUT /api/v1/meters/{id}/mute` |
| `act` | `unmute_meter` | `DELETE /api/v1/meters/{id}/mute` |
| `act` | `sample_now` | `POST /api/v1/accounts/{id}/sample`, or `POST /api/v1/sample` for every account |
| `ingest` | `record_reading` | `POST /api/v1/ingest` with one reading |
| `ingest` | `record_readings` | `POST /api/v1/ingest` with `{"readings": […]}` |
| `admin` | `add_account` | `POST /api/v1/accounts` |
| `admin` | `update_account` | `PATCH /api/v1/accounts/{id}` |
| `admin` | `delete_account` | `DELETE /api/v1/accounts/{id}` |
| `admin` | `test_account` | `POST /api/v1/accounts/test` |
| `admin` | `update_meter` | `PATCH /api/v1/meters/{id}` |
| `admin` | `delete_meter` | `DELETE /api/v1/meters/{id}` |
| `admin` | `list_channels` | `GET /api/v1/channels` |
| `admin` | `add_channel` | `POST /api/v1/channels` |
| `admin` | `update_channel` | `PATCH /api/v1/channels/{id}` |
| `admin` | `delete_channel` | `DELETE /api/v1/channels/{id}` |
| `admin` | `test_channel` | `POST /api/v1/channels/{id}/test` |
| `admin` | `send_digest` | `POST /api/v1/digest` |
| `admin` | `retry_delivery` | `POST /api/v1/deliveries/{id}/retry` |
| `admin` | `update_settings` | `PATCH /api/v1/settings` |
| `admin` | `list_tokens` | `GET /api/v1/tokens` |
| `admin` | `create_token` | `POST /api/v1/tokens` |
| `admin` | `revoke_token` | `DELETE /api/v1/tokens/{id}` |
| `admin` | `save_provider` | `PUT /api/v1/providers/{id}` |
| `admin` | `delete_provider` | `DELETE /api/v1/providers/{id}` |
| `admin` | `get_system` | `GET /api/v1/system` |
| `admin` | `backup_now` | `POST /api/v1/system/backup` |
| `admin` | `export_bundle` | `GET /api/v1/export` |
| `admin` | `import_bundle` | `POST /api/v1/import` |

What the overview holds:

- `status`: the worst open incident, `ok`, `warning` or `critical`.
- `attention`: open incidents, worst first. Unlike the MCP tool, these include
  `due` incidents, which are dates that are near.
- `upcoming`: dates inside the look-ahead window.
- `meters`: every meter.
- `spend`: this month's spend.

The API can also do these, which have no MCP tool:

| Request | Scope | Does |
| --- | --- | --- |
| `GET /api/v1/push` | `admin` | `{public_key}` — what a browser subscribes with |
| `POST /api/v1/push/subscriptions` | `admin` | Turns a browser's push subscription into a channel |

Both are a browser's business: nobody else has a subscription to hand over.

## Headroom

The API has no `headroom` field. Work it out from the meter the same way the
MCP tool does:

| Meter | `headroom` |
| --- | --- |
| `value` is null, or `status` is `failing` or `paused` | `unknown` |
| A quota with `quota.remaining_pct <= 0` | `exhausted` |
| A quota with `quota.remaining_pct <= 10`, or with `quota.exhausts_at` set | `low` |
| Any other quota | `ok` |
| A balance with `value <= 0` | `exhausted` |
| A balance whose `status` is `warning` or `critical` | `low` |
| A balance with a `forecast` | `ok` |
| A counter, or a balance without a `forecast` | `unknown` |

As a jq filter over the meter list:

```bash
qp "$QUOTAPULSE_URL/api/v1/meters" | jq '.items[] | {id, display, kind, unit, value, status,
  sampled_at, runway_days: .forecast.runway_days, confidence: .forecast.confidence,
  remaining_pct: .quota.remaining_pct, resets_at, exhausts_at: .quota.exhausts_at,
  headroom: (
    if .value == null or .status == "failing" or .status == "paused" then "unknown"
    elif .quota then (if .quota.remaining_pct <= 0 then "exhausted"
      elif .quota.remaining_pct <= 10 or .quota.exhausts_at then "low" else "ok" end)
    elif .kind == "balance" and .forecast then (if .value <= 0 then "exhausted"
      elif .status == "warning" or .status == "critical" then "low" else "ok" end)
    else "unknown" end)}'
```

Hidden meters (`"hidden": true`) are left out of the MCP tool's list. Skip them
too.

## Add a date

```bash
qp -X POST "$QUOTAPULSE_URL/api/v1/reminders" -d '{
  "title": "Mom", "kind": "birthday",
  "rule": {"freq": "yearly", "calendar": "lunar", "month": 10, "day": 9, "since": 1962},
  "lead_days": [3, 1, 0]
}'
```

The fields are `title`, `kind`, `rule`, `lead_days`, `amount`, `currency`,
`group`, `notes` and `enabled`. The rule goes in a nested `rule` object, unlike
the MCP tool, which takes its fields flat. The answer is the saved date, with
`next`, `days`, `lunar` (`{year, month, day, leap, label}`) and `following` (the
three occurrences after `next`). Preview a rule first when you are unsure where it
lands. See [dates.md](dates.md).

## Report a reading

```bash
qp -X POST "$QUOTAPULSE_URL/api/v1/ingest" \
  -d '{"meter": "claude-code", "name": "Claude Code", "kind": "counter", "unit": "USD", "value": 61.75}'
```

Send one reading as an object, or up to 500 as `{"readings": [...]}`.

| Field | |
| --- | --- |
| `meter` | Required. The key |
| `value` | Required |
| `kind` | Required the first time a key is seen |
| `name`, `unit` | Optional |
| `limit`, `resets_at`, `window_seconds` | For a quota |
| `at` | When the value was read. Optional; it may be in the past, never in the future |

A script that reports regularly runs from cron with the same two variables. The
repository's `scripts/claude-code-usage.py` is a complete example: it reports what
Claude Code has cost this month as a counter.

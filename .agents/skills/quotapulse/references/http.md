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
  details. The MCP tools always answer in English.
- **Unknown fields are refused.** A misspelt field is a 400, not a setting that
  is silently ignored.
- **Partial updates.** `PATCH` changes only the fields that are sent.

## The MCP tools, over HTTP

| MCP tool | Request | Scope |
| --- | --- | --- |
| `attention` | `GET /api/v1/overview` | `read` |
| `list_meters` | `GET /api/v1/meters` | `read` |
| `get_meter` | `GET /api/v1/meters/{id}`, then `GET /api/v1/meters/{id}/series?range=30d` for its spend per day | `read` |
| `list_dates` | `GET /api/v1/reminders`, soonest first | `read` |
| `get_spend` | `GET /api/v1/spend?month=YYYY-MM` | `read` |
| `list_activity` | `GET /api/v1/incidents?state=open&limit=20` | `read` |
| `add_date` | `POST /api/v1/reminders` | `act` |
| `complete_date` | `POST /api/v1/reminders/{id}/done` | `act` |
| `acknowledge` | `POST /api/v1/incidents/{id}/ack` | `act` |
| `sample_now` | `POST /api/v1/accounts/{id}/sample`, or `POST /api/v1/sample` for every account | `act` |
| `record_reading` | `POST /api/v1/ingest` | `ingest` |

What the overview holds:

- `status`: the worst open incident, `ok`, `warning` or `critical`.
- `attention`: open incidents, worst first. Unlike the MCP tool, these include
  `due` incidents, which are dates that are near.
- `upcoming`: dates inside the look-ahead window.
- `meters`: every meter.
- `spend`: this month's spend.

The API can also do these, which the MCP tools cannot:

| Request | Scope | Does |
| --- | --- | --- |
| `POST /api/v1/reminders/preview` with `{"rule": …}` | `read` | Shows where a rule lands, without saving it |
| `PATCH /api/v1/reminders/{id}` | `act` | Changes a date |
| `DELETE /api/v1/reminders/{id}` | `act` | Removes a date |
| `PUT /api/v1/reminders/{id}/snooze` with `{"until": "YYYY-MM-DD"}` | `act` | No reminders until after that day |
| `DELETE /api/v1/reminders/{id}/done` | `act` | Takes back a "done" |
| `PUT /api/v1/meters/{id}/mute` with `{"until": "<RFC 3339>"}` | `act` | Nothing is sent about the meter until then |
| `DELETE /api/v1/meters/{id}/mute` | `act` | Unmutes it |

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

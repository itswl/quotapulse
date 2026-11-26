# HTTP API

Everything the dashboard does, it does through this API. All of it is under
`/api/v1`, speaks JSON, and can be used with a token from a script.

- [Conventions](#conventions) · [Authentication](#authentication)
- [Overview and live updates](#overview-and-live-updates)
- [Meters and accounts](#meters-and-accounts) · [Pushing readings](#pushing-readings)
- [Dates](#dates) · [Incidents and notifications](#incidents-and-notifications) · [Spend](#spend)
- [Channels, providers, tokens, settings](#channels-providers-tokens-settings)
- [Export, import, system](#export-import-system)
- [Probes and metrics](#probes-and-metrics) · [Webhook payload](#webhook-payload) · [MCP](#mcp)

## Conventions

- **Times** are RFC 3339 in UTC (`2026-11-16T02:00:00Z`). **Dates** are `YYYY-MM-DD`
  in the time zone configured under Settings, which every overview names.
- **Lists** answer `{"items": [...]}` — always an array, never `null`.
- **Errors** answer with a status and

  ```json
  {"error": {"code": "invalid", "message": "name is required"}}
  ```

  `code` is stable; `message` is a sentence for a person. Codes: `invalid` (400),
  `unauthorized` (401), `forbidden` (403), `cross_site` (403), `not_found` (404),
  `conflict` (409), `too_large` (413), `too_many_attempts` (429), `no_password`
  (401, or 503 when signing in), `internal` (500).
- **Unknown fields are refused.** A misspelt field name is a 400, not a silently
  ignored setting.
- **Language.** Incident headlines and details are written by the server. Ask for
  a language with `?lang=zh` or `?lang=en` (or the header `X-QuotaPulse-Lang`);
  without either, the notification language from Settings is used.
- **Partial updates.** `PATCH` changes the fields you send and leaves the rest.
  Exceptions are noted where they apply (a meter's `rules`, the settings' `fx`).

## Authentication

Two ways in:

**A token** — for scripts and agents. Create one under Settings → Tokens, then

```bash
curl -H "Authorization: Bearer qp_…" https://pulse.example/api/v1/overview
```

A token has scopes; each endpoint below names the one it needs.

| Scope | Allows |
| --- | --- |
| `read` | Reading everything except credentials, channels and tokens |
| `act` | Acknowledging incidents, asking for a sample, muting a meter, adding, changing and completing dates |
| `ingest` | Pushing readings |
| `admin` | Everything, including accounts, channels, tokens and settings |

**The dashboard's session** — `POST /api/v1/session` with `{"password": "…"}` sets
an HttpOnly cookie that carries every scope. Requests that change something must
then come from the same origin. Five wrong passwords from one client, or thirty
overall, within a minute lock sign-in for the rest of that minute.

The password is `QUOTAPULSE_PASSWORD` when that is set. Otherwise it is chosen
with `POST /api/v1/setup`: by anyone on a new install, within an hour of the
server starting; later, or once anything is configured, only with the one-time
`code` the server writes to its log, and that `quotapulse reset-password` prints.

| | |
| --- | --- |
| `GET /api/v1/session` | `{signed_in, password_set, version, demo, muted}` — open to anyone. With no password yet it says how one can be chosen: `setup` (`open`, or `code`) and `fresh` (nothing is configured). Signed in, it also carries `now` and `timezone` — the dashboard shows every time by the server's clock, in the configured zone — and `password_from_env` |
| `POST /api/v1/session` | Sign in |
| `DELETE /api/v1/session` | Sign out |
| `POST /api/v1/setup` | `{password, code?, timezone?, language?}` — choose the password where there is none, and sign in. A new install also takes its `timezone` and `language` (`en`, `zh`). Codes `password_short` (422: under 8 characters), `setup_code` (403), `password_set` (409) |
| `PUT /api/v1/password` | `{current, password}`, from a signed-in dashboard — change a password chosen in the dashboard; every other session ends. Codes `wrong_password` (403), `password_short` (422), `password_from_env` (409) |

## Overview and live updates

`GET /api/v1/overview` · `read` — the whole picture in one request:

```jsonc
{
  "now": "2026-11-16T02:00:00Z",
  "timezone": "Asia/Shanghai",
  "status": "warning",            // ok | warning | critical: the worst thing open
  "attention": [ /* open incidents, worst first */ ],
  "meters":    [ /* every meter with its forecast; those needing attention first */ ],
  "accounts":  [ /* accounts, credentials masked */ ],
  "upcoming":  [ /* dates within the look-ahead window, soonest first */ ],
  "spend":     {"month": "2026-11", "base": "CNY", "total": 61.2, "projected": 118.0, "fixed": 136.0, "unconverted": []},
  "counts":    {"accounts": 2, "meters": 3, "reminders": 11, "channels": 1},
  "muted": false                  // true while notifications are recorded but not sent
}
```

`GET /api/v1/stream` · `read` — server-sent events. Each `change` event carries a
comma-separated list of what is now stale: `meters`, `reminders`, `incidents`,
`activity`, `settings`. It carries no data; refetch what you are showing.

```
event: change
data: incidents,meters
```

## Meters and accounts

A **meter** as returned everywhere (`MeterView`):

```jsonc
{
  "id": "mtr_3f9k2m7q1x8d0",
  "account_id": "acc_…",          // "" for a pushed meter
  "key": "balance-cny",           // stable within the account
  "name": "Balance", "renamed": false,
  "display": "DeepSeek",          // the name to show: the account's, "account · meter" when it has several — or `name` itself once someone has renamed the meter
  "kind": "balance",              // balance | quota | counter
  "unit": "CNY",                  // a currency code, "%", or a label
  "value": 547.67, "limit": null, "resets_at": null, "window_seconds": null,
  "sampled_at": "2026-11-16T01:41:47Z",
  "status": "ok",                 // ok | warning | critical | failing | pending | paused
  "muted": false, "muted_until": null, "hidden": false,
  "rules": {"low": 50, "runway_days": null, "anomaly": null, "budget": null, "pace": false, "stale_after_seconds": null},
  "effective": {"runway_days": 7, "anomaly": true},   // rules with the defaults filled in
  "account": {"id": "acc_…", "name": "DeepSeek", "provider": "deepseek", "provider_name": "DeepSeek", "group": "AI", "status": "ok", "last_error": ""},
  "forecast": {                   // balance and counter
    "as_of": "2026-11-16T01:41:47Z", "coverage_days": 30, "confidence": "high",   // none | low | medium | high
    "burn_per_day": 0.78, "burn_7d": 0.78, "burn_30d": 0.81, "peak_per_day": 2.66, "spend_24h": 0,
    "runway_days": 702.1, "depletes_at": null, "runway_days_at_peak": 205.9,
    "bursts": [{"window_hours": 24, "spend": 2.53, "rate_per_day": 2.53, "runway_days": 216.5, "baseline_per_day": 0.1, "ratio": 25.3}],
    "last_top_up": {"at": "…", "amount": 500}
  },
  "quota": null,                  // quota: {remaining_pct, used_pct, elapsed_pct, projected_pct, exhausts_at}
  "month": {"month": "2026-11", "spend": 12.4, "projected": 23.5, "days_elapsed": 15.4, "days_in_month": 30},
  "incidents": [ /* open incidents on this meter */ ],
  "spark": [{"t": "…", "v": 548.1}, …]   // the last two weeks, thinned
}
```

| | Scope | |
| --- | --- | --- |
| `GET /api/v1/meters` | `read` | Every meter |
| `GET /api/v1/meters/{id}` | `read` | One meter |
| `GET /api/v1/meters/{id}/series?range=` | `read` | History for a chart. `range`: `24h`, `7d`, `30d` (default), `90d`, `1y`. Returns `points`, `daily` spend, `top_ups`, and `projection` (two points continuing the line at the expected burn) |
| `PATCH /api/v1/meters/{id}` | `admin` | `name` (a meter you name is shown under that name from then on), `hidden` (a hidden meter is not judged: nothing opens on it), `sort`, `unit` (pushed meters), and `rules` — **the whole object**: a rule you leave out is cleared |
| `PUT /api/v1/meters/{id}/mute` | `act` | `{"until": "…"}` — incidents still open and resolve on the dashboard; nothing is sent |
| `DELETE /api/v1/meters/{id}/mute` | `act` | Unmute |
| `DELETE /api/v1/meters/{id}` | `admin` | Pushed meters, and meters their provider no longer reports. Hide the others |

**Rules** (`null` means "use the default from Settings"; `0` turns a rule off):

| Field | Applies to | Opens an incident when |
| --- | --- | --- |
| `low` | balance, quota | the value is at or below it |
| `runway_days` | balance | the balance is forecast to run out within this many days |
| `anomaly` | balance | a recent burst would empty it far sooner than the usual pace |
| `budget` | balance, counter | this month's spend is past it, or on course to be |
| `pace` | quota | the quota is on course to run out before it resets |
| `stale_after_seconds` | pushed meters | nothing has been reported for this long |

A rule that cannot apply to the meter's kind is not kept: the rules in the answer
are the ones in force.

**Accounts**

| | Scope | |
| --- | --- | --- |
| `GET /api/v1/accounts`, `GET /api/v1/accounts/{id}` | `read` | Secret fields are masked (`sk-••••0001`) |
| `POST /api/v1/accounts` | `admin` | `{name, provider, credential: {field: value}, group?, interval_seconds?, enabled?}` |
| `PATCH /api/v1/accounts/{id}` | `admin` | Secret credential fields left empty keep their stored value. The provider cannot be changed |
| `DELETE /api/v1/accounts/{id}` | `admin` | Removes its meters and their history |
| `POST /api/v1/accounts/test` | `admin` | Try a credential without saving: `{provider, credential, account_id?}` or `{definition, credential}` → `{ok, error, status, latency_ms, samples, body}` |
| `POST /api/v1/accounts/{id}/sample` | `act` | Read it now. Answers 202; the result arrives on the stream |
| `POST /api/v1/sample` | `act` | Read every account now |

## Pushing readings

`POST /api/v1/ingest` · `ingest` — for anything QuotaPulse cannot read by itself:
what a local tool has cost, a balance scraped by your own script, a disk filling up.

```bash
curl -X POST https://pulse.example/api/v1/ingest \
  -H "Authorization: Bearer qp_…" -H "Content-Type: application/json" \
  -d '{"meter": "claude-code", "name": "Claude Code", "kind": "counter", "unit": "USD", "value": 61.75}'
```

One reading as an object, or several as `{"readings": [...]}` (at most 500).

| Field | |
| --- | --- |
| `meter` | Required. The meter's key: lowercase letters, digits, `.`, `-`, `_` |
| `value` | Required. The current value |
| `kind` | `balance`, `quota` or `counter`. Required the first time a key is seen |
| `name`, `unit` | Optional description. A name set in the dashboard is never overwritten |
| `limit`, `resets_at`, `window_seconds` | For a quota: its size, when it resets, how long its window is |
| `at` | When the reading was taken; now when omitted. May be in the past, not in the future |

A reading says "the value is now X", so sending the same one twice changes nothing.
A **counter** is a running total: send the total so far (this month's, say) and let
it start over when the period does — a drop is read as a restart, not as negative
spend. [`scripts/claude-code-usage.py`](../scripts/claude-code-usage.py) is a
complete reporter in a hundred lines.

## Dates

A date as returned (`ReminderView`) is the stored reminder plus where it lands:
`next` (the next occurrence), `days` (until it; 0 is today), `lunar` (for lunar
rules: `{year, month, day, leap, label}`), `ordinal` (the age at a birthday, when
`rule.since` is set), `state` (`upcoming`, `soon`, `today`, `snoozed`, `past`,
`disabled`), `handled` (the occurrence marked done, if one is being skipped),
`following` (the three after `next`), and `monthly_amount`.

| | Scope | |
| --- | --- | --- |
| `GET /api/v1/reminders`, `GET /api/v1/reminders/{id}` | `read` | Soonest first |
| `POST /api/v1/reminders` | `act` | See below |
| `PATCH /api/v1/reminders/{id}` | `act` | |
| `DELETE /api/v1/reminders/{id}` | `act` | |
| `POST /api/v1/reminders/preview` | `read` | `{rule}` → where it lands, without saving |
| `POST /api/v1/reminders/{id}/done` | `act` | Mark the upcoming occurrence as dealt with |
| `DELETE /api/v1/reminders/{id}/done` | `act` | Take the mark back |
| `PUT /api/v1/reminders/{id}/snooze` | `act` | `{"until": "YYYY-MM-DD"}` — no reminders until after that day |
| `DELETE /api/v1/reminders/{id}/snooze` | `act` | |

```jsonc
{
  "title": "Mom",
  "kind": "birthday",             // renewal | birthday | anniversary | holiday | expiry | custom
  "rule": {"freq": "yearly", "calendar": "lunar", "month": 10, "day": 9, "since": 1962},
  "lead_days": [3, 1, 0],         // remind three days before, the day before, and on the day
  "amount": null, "currency": "", // for renewals: what it costs
  "group": "", "notes": "", "enabled": true
}
```

**Rules**

| `freq` | Fields | |
| --- | --- | --- |
| `once` | `date` | A single day |
| `weekly` | `weekday` (1 Monday … 7 Sunday) | |
| `monthly` | `day` | The 31st lands on the last day of a shorter month |
| `yearly` | `month`, `day` | February 29 lands on the 28th in a common year |

- `calendar: "lunar"` (monthly and yearly) uses the Chinese lunisolar calendar. A day
  past the end of the month lands on its last day — 腊月三十 is New Year's Eve every
  year — and a rule names a regular month, never a leap one.
- `interval: N` with a start `date` repeats every N weeks, months or years (solar only).
- `since` is the year it started; it only feeds `ordinal`.

## Incidents and notifications

An incident (`IncidentView`): `id`, `kind`, `reason`, `severity` (`info`, `warning`,
`critical`), `state` (`open`, `resolved`), `subject_type` / `subject_id`, `title`
(the subject's name), `headline` and `detail` (the story in words), `facts` (the
numbers behind it), `path` (where on the dashboard to look), `opened_at`,
`resolved_at`, `acked_at`, `notified_at`, `notify_count`, and `data` (the raw facts).

| Kind | About | Reasons |
| --- | --- | --- |
| `level` | a meter running out | `empty`, `low`, `runway`, `burst`, `pace` |
| `budget` | a meter's monthly spend | `projected`, `exceeded` |
| `stale` | a pushed meter gone silent | |
| `failing` | an account that cannot be read | |
| `due` | a date inside its lead window | |
| `delivery` | a channel that keeps failing | |

| | Scope | |
| --- | --- | --- |
| `GET /api/v1/incidents` | `read` | Newest first. `state`, `subject_type`, `subject_id`, `limit` (≤ 500), `before` (an RFC 3339 time, to page back) |
| `POST /api/v1/incidents/{id}/ack` | `act` | Acknowledge: it stays open until its cause is gone, and stops repeating. Answers with the incident |
| `DELETE /api/v1/incidents/{id}/ack` | `act` | Take the acknowledgement back |
| `GET /api/v1/deliveries` | `read` | The notification log, newest first. `incident`, `channel`, `limit`, `before` (a delivery id). `status`: `pending`, `sent`, `failed`, `dropped` |
| `POST /api/v1/deliveries/{id}/retry` | `admin` | Queue a failed delivery again |
| `POST /api/v1/digest` | `admin` | Send the digest now |

## Spend

`GET /api/v1/spend?month=YYYY-MM` · `read` — a calendar month (the current one by
default): `total` and `projected` metered spend in the base currency, `fixed` (the
renewals that fall in the month), `rows` per meter, `renewals`, `daily` totals, the
`history` of the last six months, and `unconverted` — currencies with no exchange
rate, which are listed but never added in. Quotas are not money and do not appear.

## Channels, providers, tokens, settings

| | Scope | |
| --- | --- | --- |
| `GET /api/v1/channel-types` | `read` | The kinds of channel and the fields each needs |
| `GET /api/v1/channels` | `admin` | With recent delivery health. Secret settings are masked |
| `POST /api/v1/channels` | `admin` | `{name, type, config: {…}, topics?, min_severity?, enabled?}` |
| `PATCH /api/v1/channels/{id}` | `admin` | Secret settings left empty keep their stored value |
| `DELETE /api/v1/channels/{id}` | `admin` | |
| `POST /api/v1/channels/{id}/test` | `admin` | Sends a test message now → `{ok, error?}` |
| `GET /api/v1/providers` | `read` | The built-in catalog and your own definitions |
| `PUT /api/v1/providers/{id}` | `admin` | Save a definition — see [providers.md](providers.md) |
| `DELETE /api/v1/providers/{id}` | `admin` | Only when no account uses it |
| `GET /api/v1/tokens` | `admin` | Never includes a token itself |
| `POST /api/v1/tokens` | `admin` | `{name, scopes, expires_at?}` → the token, in `token`, this once |
| `DELETE /api/v1/tokens/{id}` | `admin` | Revoke |
| `GET /api/v1/settings` | `read` | |
| `PATCH /api/v1/settings` | `admin` | Partial, except `fx`, which is replaced whole when sent |

Channel `topics` are `meters`, `dates`, `system` and `digest`; `min_severity` is the
least severe incident the channel wants. Types: `feishu`, `dingtalk`, `wecom`,
`slack`, `telegram`, `ntfy`, `bark`, `webhook` — and `webpush`, a browser that
asked for notifications.

**Browser push.** A `webpush` channel is not created from a form: the dashboard's
"notify me on this device" asks the browser for a push subscription and hands it
over. Each device becomes a channel of its own, with its own topics and severity,
and is removed automatically when the browser unsubscribes.

| | Scope | |
| --- | --- | --- |
| `GET /api/v1/push` | `admin` | `{public_key}` — what a browser subscribes with |
| `POST /api/v1/push/subscriptions` | `admin` | `{name, subscription}` where `subscription` is the browser's `PushSubscription` as JSON, exactly as `subscription.toJSON()` gives it → the channel. Subscribing the same browser again refreshes its channel |

A `webpush` channel carries `device`: the first 32 hex digits of the SHA-256 of
its subscription's endpoint. A browser hashes its own endpoint the same way to
find which channel in the list is itself, without the server ever handing a
subscription back out.

**Settings**

| Field | Default | |
| --- | --- | --- |
| `timezone` | `UTC` | Decides what "today" is: day boundaries, the reminder time, quiet hours |
| `language` | `en` | `en` or `zh`: the language notifications are written in |
| `base_currency`, `fx` | `USD`, `{}` | Totals are converted to the base using `fx`: one unit of the key is worth this much base |
| `sample_seconds` | `1800` | How often accounts are read, unless one says otherwise |
| `runway_days` | `7` | Default lead time for "running out" |
| `anomaly` | `true` | Burst detection by default |
| `reminder_time` | `09:00` | When date reminders go out |
| `quiet` | `{enabled: true, start: "23:00", end: "08:00"}` | Holds back everything that is not critical |
| `digest` | `{enabled: true, weekday: 1, time: "09:00"}` | The summary; `weekday` 0 is every day. A digest missed because the server was down goes out when it is back, the same day; one whose hour had already passed when the schedule was set — a new install, a change of time — waits for its next turn |
| `renotify_hours` | `24` | How often an unacknowledged critical incident repeats; 0 never |
| `notify_resolved` | `true` | Send a message when something recovers |
| `retention_days` | `730` | How long readings and closed incidents are kept |
| `upcoming_days` | `60` | How far ahead the overview looks for dates |
| `public_url` | `""` | Where the dashboard is reachable; notifications link to it. While empty, it is learned from the first browser that signs in at an address other than `localhost` |

## Export, import, system

| | Scope | |
| --- | --- | --- |
| `GET /api/v1/export?secrets=1&history=0` | `admin` | A [bundle](bundle.md). Without `secrets=1` it carries no credentials |
| `POST /api/v1/import?settings=1` | `admin` | Merges a bundle; whatever already exists under the same name is skipped and reported |
| `GET /api/v1/system` | `admin` | Version, database size, what each background loop last did |
| `POST /api/v1/system/backup` | `admin` | Write a database backup now |

## Probes and metrics

| | |
| --- | --- |
| `GET /healthz` | 200 while the process is up |
| `GET /readyz` | 200 while the database answers and the background loops are turning; 503 otherwise. This is what the container's health check asks |
| `GET /metrics` | Prometheus text format; needs a token with `read` |

```yaml
scrape_configs:
  - job_name: quotapulse
    authorization: {credentials: qp_…}
    static_configs: [{targets: ["pulse.example:443"]}]
    scheme: https
```

`quotapulse_meter_value`, `quotapulse_meter_runway_days`, `quotapulse_meter_burn_per_day`,
`quotapulse_meter_remaining_ratio`, `quotapulse_meter_sampled_timestamp_seconds`
(labels `meter`, `kind`, `unit`), `quotapulse_account_up` (`account`, `provider`),
`quotapulse_incidents_open` (`severity`), `quotapulse_spend_month` (`currency`),
`quotapulse_build_info` (`version`).

## Webhook payload

A channel of type `webhook` receives a `POST` with this body. Fields are added over
time, never renamed or removed.

```json
{
  "version": 1,
  "event": "opened",
  "severity": "warning",
  "topic": "meters",
  "title": "Balance runs out in about 5.8 days · OpenRouter",
  "text": "The balance is 18.40 USD and about 3.15 USD goes a day. …",
  "facts": [{"label": "Balance", "value": "18.40 USD"}, {"label": "Runway", "value": "5.8 days"}],
  "link": "https://pulse.example/meters/mtr_…",
  "at": "2026-11-16T02:00:00Z",
  "subject": {"incident_id": "inc_…", "kind": "level", "reason": "runway", "type": "meter", "id": "mtr_…", "name": "OpenRouter"}
}
```

`event` is `opened`, `escalated`, `reminder`, `resolved`, `digest` or `test`. With a
signing secret set on the channel, each request carries
`X-QuotaPulse-Signature: sha256=<hex HMAC-SHA256 of the body>`.

## MCP

`/mcp` speaks the Model Context Protocol over streamable HTTP, for AI agents. It
accepts API tokens only and offers the tools the token's scopes allow — everything
the dashboard can do, shaped for reading by a model:

| Scope | Tools |
| --- | --- |
| `read` | `attention`, `list_meters`, `get_meter`, `list_dates`, `get_spend`, `list_activity`, `list_accounts`, `list_deliveries`, `get_settings`, `preview_date`, `list_channel_types`, `list_providers` |
| `act` | `add_date`, `update_date`, `complete_date`, `reopen_date`, `snooze_date`, `unsnooze_date`, `delete_date`, `acknowledge`, `unacknowledge`, `mute_meter`, `unmute_meter`, `sample_now` |
| `ingest` | `record_reading`, `record_readings` |
| `admin` | `add_account`, `update_account`, `delete_account`, `test_account`, `update_meter`, `delete_meter`, `list_channels`, `add_channel`, `update_channel`, `delete_channel`, `test_channel`, `send_digest`, `retry_delivery`, `update_settings`, `list_tokens`, `create_token`, `revoke_token`, `save_provider`, `delete_provider`, `get_system`, `backup_now`, `export_bundle`, `import_bundle` — and, since `admin` carries every scope, all of the tools above |

```json
{"mcpServers": {"quotapulse": {"type": "http", "url": "https://pulse.example/mcp", "headers": {"Authorization": "Bearer qp_…"}}}}
```

An agent should call `attention` first, and `list_meters` before starting work that
will lean on an account: each meter carries a one-word `headroom` — `ok`, `low`,
`exhausted` or `unknown`.

- **By id or by name.** A tool that takes a meter, an account, a date, a channel
  or a token accepts its `id`, or its name when exactly one has that name.
- **Deleting is marked.** A tool that removes something for good — `delete_date`,
  `delete_meter`, `delete_account`, `delete_channel`, `delete_provider`,
  `revoke_token` — carries the MCP `destructive` annotation, so a client can ask
  first. To silence something for a while, disable it, mute it or snooze it
  instead.
- **Rules are replaced as a whole.** `update_meter` takes a meter's `rules` the
  way the dashboard does: a rule left out goes back to the default, and `0` turns
  one off. `update_settings` is a partial update, except `fx`, `quiet` and
  `digest`, which are replaced whole when sent.
- **Secrets.** Credentials and channel settings are shown masked, as in the
  dashboard. `create_token` returns the token itself once; `export_bundle`
  carries credentials in the clear only when asked (`secrets: true`).
- **Language.** A request may name its language as the API's requests do, with
  `?lang=zh` or an `X-QuotaPulse-Lang` header. What the tools then say in words —
  headlines, summaries, a rule in words — is written in it; a request that asks
  for nothing is answered in English. Put it in the client's URL:
  `"url": "https://pulse.example/mcp?lang=zh"`.

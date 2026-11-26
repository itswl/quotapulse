---
name: quotapulse
description: >-
  Ask the user's QuotaPulse server what is running out, what is due and what has
  been spent, and act on the answer. QuotaPulse is a self-hosted watcher of API
  balances, plan quotas, spend and dates such as renewals and birthdays. It can
  add a date (lunar ones too), mark one done, acknowledge an incident or report a
  reading. Use it when the user asks how much is left on an API account or plan,
  how long a balance will last, what this month cost, or what is coming up. Use
  it when they want a renewal or birthday remembered, and before a long or costly
  job on a metered API, to check that the account can take it. Chinese requests
  count too: 余额还能用几天, 额度还剩多少, 这个月花了多少, 提醒我续费/生日, 农历生日.
  It works through a connected QuotaPulse MCP server, or through the HTTP API
  with QUOTAPULSE_URL and QUOTAPULSE_TOKEN.
---

# QuotaPulse

QuotaPulse watches two kinds of things for one person:

- **Meters** are things that run out. There are three kinds:
  - `balance`: a prepaid balance.
  - `quota`: an allowance that resets, often given in `%`.
  - `counter`: a running total of spend.

  Most are read from a platform every half hour through an **account**, and a
  script can push others in.
- **Dates** are things that come due, such as renewals, birthdays, anniversaries
  and expiries. They can be on the solar or the lunar calendar.

Anything wrong, or a date that is near, becomes an **incident**. Incidents are
the only thing the user is ever notified about.

## Connect

1. **MCP.** If the tools `attention`, `list_meters` and the rest are available,
   use them. The server is usually named `quotapulse`. It offers only the tools
   the token's scopes allow. If `add_date` is missing, the token lacks `act`. Say
   so; do not try another way around it.
2. **HTTP.** If there are no tools but `QUOTAPULSE_URL` and `QUOTAPULSE_TOKEN`
   are set, use the API as described in [references/http.md](references/http.md).
3. **Neither.** Tell the user how to connect:
   1. Create a token in the dashboard under **Settings → Tokens**. Pick its
      scopes:

      | Scope | Lets the agent |
      | --- | --- |
      | `read` | Ask anything: what needs attention, meters and headroom, dates, spend, incidents, accounts, the notification log, settings, providers |
      | `act` | Add, change, snooze and complete dates; acknowledge incidents; mute meters; ask for a fresh reading |
      | `ingest` | Report readings |
      | `admin` | Everything the dashboard can do: accounts, meters' rules, channels, tokens, settings, export and backup |

   2. Add the server to the agent's MCP config:

      ```json
      {"mcpServers": {"quotapulse": {"type": "http", "url": "https://<their server>/mcp",
        "headers": {"Authorization": "Bearer qp_…"}}}}
      ```

      Or export the two variables instead.

A token is a credential. Never print it, repeat it back, or write it into a file
that is committed anywhere.

An `admin` token lets an agent do everything the dashboard can — accounts,
channels, tokens, settings. These are the tools most likely to do harm by
accident, so the rule is the same as for everything here: do what was asked,
by name, and say what it will change before you call it. Never print a token or
a credential, and never store one anywhere.

## What needs attention

Call `attention` first. It returns:

- `status`: the worst open incident, `ok`, `warning` or `critical`.
- `open`: each open incident, with a `headline` written for a person. Quote or
  paraphrase the headline rather than recomputing it.
- `upcoming`: dates inside the user's look-ahead window. `days: 0` means today.
- `notifications_muted`: true while notifications are recorded but not sent.

When the question is why nobody was told, the answer is in `list_deliveries`:
whether the message went, and what the channel said when it did not.

For detail, use `list_meters`, then `get_meter` for a single meter (its rules,
open incidents and spend per day). `list_meters` matches `query` against any
part of a meter's name, account or provider. `get_meter` needs the meter's `id`,
or its whole `name` as `list_meters` gave it (case does not matter). An account
with several meters names each one "account · meter".

The MCP tools answer in English, unless the client's endpoint asks for Chinese
(`?lang=zh` on the MCP URL). Speak the user's language when you relay the
answer.

### Read a meter honestly

- **Units.** An amount is in its meter's own `unit`: a currency code, `%`, or a
  label such as `calls`.
  - Never add amounts that are in different units.
  - A quota is not money. Never add it to spend.
  - For a total across currencies, use `get_spend`. It converts with the user's
    own rates and lists in `unconverted` any currency it left out. Say so when
    that list is not empty.
- **Runway.** `runway_days` is a forecast from the recent pace, as of
  `sampled_at`, not as of now.
  - Say "about 12 days at the recent pace", not "12 days left".
  - When `forecast_confidence` is `none` or `low`, say the history is still thin.
  - When `runway_days` is absent, nothing is being spent. It does not mean
    "lasts forever".
- **Freshness.** Look at how old `sampled_at` is.
  - `status: failing`: the account cannot be read right now. The value is the
    last one seen; present it as such.
  - `sample_now` (needs `act`) asks for a fresh reading. It arrives within
    seconds; call `list_meters` again to see it.
- **Quota.** Read `remaining_pct` and `resets_at`. `exhausts_at` appears only when
  the quota will run out before it resets at the current pace.
- **Counter.** `value` is the total so far, usually for the current period.
  `month_spend` is what was spent this calendar month. A counter cannot run out,
  so its `headroom` is always `unknown`. What can go wrong is going over a monthly
  budget (`rules.budget` in `get_meter`), and that shows up as a `budget`
  incident.
- **Time.** Times are RFC 3339 in UTC. Dates are `YYYY-MM-DD` in the user's time
  zone, which every result gives in `timezone`. Tell the user times in that zone.

## Before a long or costly job

Before running something that will spend a metered API or plan, check the
account first. That means a batch of model calls, a long agent run on a coding
plan, or a large crawl through a paid API. Call `list_meters` with `query` set to
the platform or account name, and read `headroom`:

| `headroom` | What to do |
| --- | --- |
| `ok` | Go ahead |
| `low` | Before starting, tell the user what is left and when it runs out or resets. If another account says `ok` and the job could use either, prefer it |
| `exhausted` | Do not start on this account. Say so |
| `unknown` | It is not known to be fine. The account cannot be read, has never been read, or this is a counter. Say which. `sample_now` may settle it |

A quota with `exhausts_at` set is already on course to run out before it resets,
and a large job brings that closer.

An account that has never been read has no meters, so the query finds nothing.
Before concluding the account is not watched, look in `attention` for a
`failing` incident about it. Its headline names the account and the detail says
why it cannot be read.

## Change things

Do these when the user asks. Repeat back what was done. Where a request is
ambiguous, check first. A tool that takes a meter, an account, a date, a channel
or a token accepts its `id`, or its name when exactly one has that name.

### With `act`

- **Dates.** The rules are in [references/dates.md](references/dates.md).
  1. Search first with `list_dates` and a `query`. If a date with that title
     already exists, ask before adding another.
  2. Unsure where a rule lands? `preview_date` (needs only `read`) takes the same
     fields as `add_date` and answers without saving anything.
  3. `add_date`. After adding, read back where it landed: `next`, `days` and
     `lunar` for a lunar date. Make sure that is the day the user meant.
  4. `update_date` changes what it is told to and leaves the rest: send only
     what should change. Its `rule` is a date's `schedule` field from
     `list_dates`, edited. `enabled: false` switches a date off without losing
     it. If one lands wrong, change it; never add a second copy.
  5. `delete_date` removes a date for good. Prefer `update_date` with
     `enabled: false` unless the user asks for it to be gone.
- **`complete_date`** marks the upcoming occurrence as dealt with: paid, renewed,
  celebrated. Its reminders stop and the date moves on to the next occurrence.
  Use it only for an occurrence that has actually been dealt with.
  **`reopen_date`** takes the mark back.
- **`snooze_date`** holds a date's reminders until a day (`until`, `YYYY-MM-DD`);
  the date still shows as upcoming. **`unsnooze_date`** wakes it.
- **`acknowledge`** stops an open incident from repeating its notification. It
  stays open until its cause is gone. Acknowledge only when the user says they
  have seen it or are handling it, never to tidy the list. It fixes nothing.
  **`unacknowledge`** takes it back.
- **`mute_meter`** holds back a meter's notifications until a moment (`until`,
  RFC 3339); **`unmute_meter`** lifts it.
- **`sample_now`** reads one account, or every account, now.

### With `ingest`

- **`record_reading`** reports a value QuotaPulse cannot read by itself, and
  **`record_readings`** reports up to 500 at once.
  - The key is lowercase letters, digits, `.`, `-` and `_`.
  - The first reading for a key must give its `kind`.
  - A counter takes the running total so far, never the increase since last
    time. A drop in a counter means it started over.
  - A quota may give `limit`, `resets_at` and `window_seconds`; a reading may
    give `at` when it was taken earlier.
  - Sending the same reading twice changes nothing.
  - For a value that should be reported regularly, write a script that posts to
    `/api/v1/ingest` from cron (see [references/http.md](references/http.md)),
    rather than reporting it by hand once.

### With `admin`

Everything else the dashboard can do. Each of these carries a secret, changes
what everyone is told, or can lock somebody out, so do one only when the user
asks for it by name, and say what it will change before you call it.

- **Accounts.** `list_accounts` (needs only `read`) shows them with the
  credential masked. `test_account` tries a credential without saving it: use
  it before `add_account`. `update_account` edits one; a secret left out keeps
  its stored value, and `enabled: false` stops the reading without losing
  anything. `delete_account` removes it with its meters and their history.
- **Meters.** `update_meter` renames a meter, hides it (`hidden`), sorts it,
  corrects the unit of a meter reported from outside, or replaces its `rules` as
  a whole (a rule left out goes back to the default; `0` turns one off).
  `delete_meter` removes a meter and its history.
- **Notifications.** `list_deliveries` (needs only `read`) is the log of what
  was sent and what failed: look there when someone was not told.
  `retry_delivery` queues a failed message again; `send_digest` sends the digest
  now. Channels: `list_channel_types` (needs only `read`), `list_channels`,
  `add_channel`, `update_channel`, `delete_channel` and `test_channel`, which
  sends a test message and answers with the platform's refusal when it has one.
- **Settings.** `get_settings` (needs only `read`), and `update_settings`, which
  takes only what should change. `fx`, `quiet` and `digest` are replaced as a
  whole when sent.
- **Tokens.** `list_tokens`, `create_token` and `revoke_token`. The answer of
  `create_token` carries the token itself once: hand it to the user, and store
  it nowhere.
- **Providers.** `list_providers` (needs only `read`), `save_provider` for a
  definition of the user's own, and `delete_provider`.
- **System.** `get_system` (the version and what the background loops last
  did), `backup_now`, `export_bundle` and `import_bundle`. A bundle carries
  credentials in the clear only when exported with `secrets: true`; an import
  never overwrites what already exists.

## When something fails

- **A tool error with a sentence**, such as "day must be between 1 and 30": the
  input was wrong. Fix it and call again.
- **`unknown tool`**: the token lacks the scope that offers it (see Connect).
  Say which scope is missing; do not try another way around it.
- **A name matches several things**, as in "2 meters match …; use the id": list
  them and use the `id`.
- **HTTP 400 `invalid`**: the body was refused, and `message` says why. A
  misspelt field name is refused too; it is not silently ignored.
- **HTTP 401**: the token is wrong, expired or revoked.
- **HTTP 403 `forbidden`**: the token lacks a scope, and the message names it.
  Tell the user. Do not retry.

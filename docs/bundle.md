# Bundles: export, import, and starting from a file

A **bundle** is one JSON document that holds a QuotaPulse install by name:
accounts, meters with their rules and history, dates, channels, tokens, settings.
The same format is

- what **Export** writes — a backup you can read;
- what **Import** merges in;
- what `QUOTAPULSE_BOOTSTRAP` loads into an empty database — configuration as a file;
- what [the migration tool](migrating-from-0.x.md) produces from a pre-1.0 install.

```bash
quotapulse export --secrets -o backup.json    # everything, credentials included
quotapulse export --no-history                # configuration only, no credentials, to stdout
quotapulse import backup.json                 # merge into the database in $QUOTAPULSE_DATA
quotapulse import --settings backup.json      # …and apply its settings too
```

In a container: `docker compose exec quotapulse /quotapulse export --secrets > backup.json`.
The dashboard does the same under **Settings → System**.

## What import does

Import **merges and never overwrites**. Things are matched by name — an account by
its name, a pushed meter by its key, a channel and a token by their names, a date
by its title and rule. Whatever already exists is left alone and listed in the
report. Importing the same bundle twice changes nothing.

Settings are applied only when asked for (`--settings`, or `?settings=1`): merging
a bundle into a running install should not move its time zone. A bootstrap bundle
always applies its settings — the database is empty.

A bundle exported **without** `--secrets` carries no credentials. Its accounts and
channels are imported *disabled*, with a note saying what to fill in, rather than
as broken entries that fail at the next sample.

## The format

Every section is optional. This is a complete, valid bootstrap file:

```json
{
  "format": "quotapulse-bundle",
  "version": 1,
  "settings": {"timezone": "Asia/Shanghai", "language": "zh", "base_currency": "CNY", "fx": {"USD": 7.16}},
  "channels": [
    {"name": "Feishu", "type": "feishu", "config": {"url": "https://open.feishu.cn/open-apis/bot/v2/hook/…"},
     "topics": ["meters", "dates", "system", "digest"], "min_severity": "info", "enabled": true}
  ],
  "accounts": [
    {"name": "DeepSeek", "provider": "deepseek", "credential": {"api_key": "sk-…"}, "group": "AI", "enabled": true}
  ],
  "meters": [
    {"account": "DeepSeek", "key": "balance-cny", "kind": "balance", "unit": "CNY", "rules": {"low": 50}}
  ],
  "reminders": [
    {"title": "Mom", "kind": "birthday", "rule": {"freq": "yearly", "calendar": "lunar", "month": 10, "day": 9},
     "lead_days": [3, 0], "enabled": true},
    {"title": "iCloud+", "kind": "renewal", "rule": {"freq": "monthly", "day": 19},
     "lead_days": [3, 0], "amount": 68, "currency": "CNY", "enabled": true}
  ]
}
```

| Section | Notes |
| --- | --- |
| `settings` | Any subset of the settings — see [API.md](API.md#channels-providers-tokens-settings). What you leave out keeps its default |
| `providers` | Your own [definitions](providers.md). Built-in ones are never replaced |
| `channels` | `config` holds the type's settings; without it the channel comes in disabled |
| `accounts` | `credential` holds the provider's fields; without it the account comes in disabled |
| `meters` | `account` names the account; leave it out for a pushed meter. Listing a sampled meter before its account has ever been read is how a threshold is in place from the first sample. `readings` are `[unix seconds, value]` or `[unix seconds, value, limit]`, oldest first |
| `reminders` | As in the [API](API.md#dates), plus `done_through` and `snoozed_until` |
| `tokens` | `{name, scopes, expires_at?}` with either `token` (a key already in use, kept as it is) or `hash` (what an export with secrets carries: enough to keep an issued token working, useless without the token itself) |
| `incidents` | Closed incidents, for history: `{subject_type, subject, kind, severity, reason, title, data, opened_at, resolved_at}`. `subject` names an account, a meter (`account/key`, or a pushed meter's key), a reminder or a channel |
| `notes` | Remarks from whatever wrote the bundle; shown on import |

Unknown fields are refused, so a typo is an error rather than a setting that
quietly did nothing.

## Handle with care

A bundle exported with secrets — and any bundle from the migration tool — holds API
keys and webhook addresses in the clear. It is as sensitive as the database. Write
it with `-o` (the file is created readable only by you), keep it out of version
control (`*bundle*.json` is in this repository's `.gitignore` for that reason), and
delete it once it has served its purpose.

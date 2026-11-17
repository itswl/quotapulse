# Moving from 0.x to 1.0

1.0 is a redesign, not an upgrade in place. It has a new data model, a new API, its
own database file, and far fewer settings. Nothing from 0.x is read at startup;
instead, a small tool reads the old install once and writes a
[bundle](bundle.md) that 1.0 imports. The old database is never written to, so
going back is a matter of starting the old container again.

## What carries over

| 0.x | 1.0 |
| --- | --- |
| Projects — from the database, and from `{PROVIDER}_API_KEY` in the environment | Accounts, with the same names and groups |
| A project's threshold and monthly budget | The meter's `low` and `budget` rules |
| Balance history | The meter's readings |
| Subscriptions | Dates. One with an amount becomes a renewal; a yearly one without becomes a birthday, unless its name is a holiday's. "Start N days before, then daily" becomes the list of days N … 0 |
| The alert log | Closed incidents, each with the text it had at the time |
| `WEBHOOK_URL` / `WEBHOOK_TYPE` | A channel |
| `MCP_API_KEYS` | API tokens with the `read` scope. **The keys themselves keep working** |
| Custom platforms | Provider definitions, their paths translated to jq |
| Reported usage (`/api/usage/ingest`) | A pushed counter |
| Browsers with notifications turned on | A channel per device, with the same push keys — **they keep receiving without being asked again** |
| `TZ`, `FX_RATES`, the refresh interval, the runway lead time, the weekly report time | Settings |
| API keys encrypted with `CONFIG_ENCRYPTION_KEY` | Decrypted during migration, and encrypted again by 1.0 under its own key |

## What does not

- **Mailbox scanning.** Reading mail for billing notices is gone. If something
  else already reads your mail, it can report into QuotaPulse through
  [`POST /api/v1/ingest`](API.md#pushing-readings).
- **Per-subscription webhooks.** A channel now chooses which topics it wants;
  dates go to every channel that takes dates.
- **MySQL and PostgreSQL.** 1.0 keeps everything in one SQLite file in its data
  directory. [Why](ARCHITECTURE.md#storage).
- **The old API, the old MCP tool names, and the old webhook JSON.** Anything that
  called `/api/credits` or read the `AlarmNotification` envelope needs to move to
  [the new API](API.md) and [the new payload](API.md#webhook-payload).
- **Most environment variables.** The `ENABLE_*` switches are gone — nothing needs
  switching on. See the table below for the few that remain.

## What behaves differently

- **Notifications are about change.** 0.x checked at fixed times of day and
  repeated an alert daily while it held. 1.0 says when something opens, gets
  worse, or recovers, as it happens — held back overnight by quiet hours unless
  it is critical. Only an unacknowledged critical incident repeats.
- **"Spending spike" is stricter.** It now also requires that the balance would
  actually run out soon at the burst's rate. Small sums no longer trigger it.
- **A coding-plan quota is several meters**, each with its reset time, instead of
  one percentage. The old threshold lands on the token window.
- **Quotas are not money.** They no longer appear in monthly spend.

## Doing it

You need the old `.env` and network access to the old database from wherever you
run the tool.

**1. Convert.** From a checkout of this repository:

```bash
cd tools/legacy-import
go run -mod=mod . --env-file /path/to/old/.env -o /path/to/bundle.json
```

It prints what it converted and a note for everything that needs your attention.
Read the notes. The bundle holds your API keys in the clear: keep it next to the
`.env`, not in a repository.

To run it on a server without Go, build it once for that machine and copy the
binary over:

```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -mod=mod -o legacy-import .
```

**2. Write the new `.env`.** It needs one line; set the password to your old
`WEB_API_KEY` to keep signing in with what you know.

| 0.x | 1.0 |
| --- | --- |
| `WEB_API_KEY` | `QUOTAPULSE_PASSWORD` |
| `CONFIG_ENCRYPTION_KEY` | `QUOTAPULSE_SECRET` (optional; a key file is created otherwise) |
| `WEB_PORT` | `QUOTAPULSE_LISTEN` |
| everything else | set in the dashboard, or carried by the bundle |

**3. Rehearse, muted.** Start 1.0 next to the old version, on another port, with
notifications recorded instead of sent:

```bash
QUOTAPULSE_MUTE=1 QUOTAPULSE_PASSWORD=… QUOTAPULSE_DATA=./data-rehearsal QUOTAPULSE_LISTEN=:8081 \
  quotapulse import --settings bundle.json
QUOTAPULSE_MUTE=1 QUOTAPULSE_PASSWORD=… QUOTAPULSE_DATA=./data-rehearsal QUOTAPULSE_LISTEN=:8081 quotapulse
```

Look at the dashboard: are the accounts being read, do the dates land where they
should? **Activity → Notifications** shows what *would* have been sent.

**4. Switch.** Stop the old version, import into the real data directory, start
1.0 without `QUOTAPULSE_MUTE`, and send a test message from
**Settings → Channels**.

**5. Clean up.** Delete the bundle. Point anything that used the old API — a usage
reporter, a webhook receiver, an agent's prompt that names old tools — at the new
one. Keep the old database until you are sure.

Going back at any point before step 5: stop 1.0 and start the old container. The
old database was only ever read.

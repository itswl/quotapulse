# How QuotaPulse is built, and why

QuotaPulse does one thing: **it tells you before something you depend on runs out
or comes due.** Every design decision below follows from taking that sentence
literally.

## The model

Two kinds of things are watched.

| | What it is | Where it comes from | What is asked of it |
| --- | --- | --- | --- |
| **Meter** | Something that runs out: a prepaid balance, a quota that resets, a running total of spend | Sampled from a platform through an *account*, or pushed in by a script | How long will it last? |
| **Reminder** (a *date*) | Something that comes due: a renewal, a birthday, an expiry | A recurrence rule, on the solar or the lunar calendar | How many days are left? |

Both feed one stream of **incidents**, and incidents are the only thing anyone is
ever notified about.

```
 account ──sample──▶ meter ──┐
 script  ──push────▶ meter ──┼─ evaluate ─▶ conditions ─ reconcile ─▶ incidents ─ announce ─▶ outbox ─▶ channels
 rule    ──calendar▶ date  ──┘                                            ▲
                                                             the dashboard and MCP read these
```

### A meter has a kind, and the kind decides how its history is read

A meter is only ever *sampled*: there is no ledger of transactions, just a number
every half hour. Everything else is reconstructed from the difference between
neighbouring readings, and what a difference means depends on the kind.

- **balance** — a stock. A drop is spend; a rise is a top-up.
- **quota** — an allowance that resets. The value is what is left of `limit` until
  `resets_at`. A rise is a reset, never income.
- **counter** — an odometer of spend. A rise is spend; a drop means it started
  over (a new month), and whatever it reads now was spent since.

Keeping the kinds apart is what keeps the arithmetic honest. An earlier version
stored one number per account and treated every drop as money, so a coding plan's
percentage bouncing between 100 and 0 showed up as "189 spent this month".
A quota is not money and is never added to it.

### One account, several meters

A platform's answer often holds more than one thing worth watching: a five-hour
token window *and* a monthly tool quota; a balance per currency. A provider
definition yields a list of meters, each with its own history, forecast and rules.
Collapsing them into one number throws away the reset time, which is the most
useful thing a quota knows about itself.

### Incidents, not alerts

A check that fires every time it runs produces a message a day for a balance that
has been low for a week, and people learn to stop reading. So QuotaPulse does not
"send alerts". It evaluates **conditions** — plain facts about the present, such as
*runs out in five days* — and reconciles them with the incidents already open:

- a new condition **opens** an incident;
- a worse one (higher severity, or a more pressing reason at the same severity)
  **escalates** it;
- a condition that no longer holds **resolves** it.

Those edges are announced. In between, nothing is said — with one exception: a
*critical* incident nobody has acknowledged repeats once a day. A warning is said
once; if it gets worse, that is an escalation and a message of its own.

Three details make this behave well in practice:

- **Stickiness.** An open incident closes a little past where it opened. A runway
  hovering at exactly the seven-day line would otherwise open and resolve on
  alternate samples.
- **Quiet hours hold back what can wait.** Anything that is not critical is queued
  until morning. If the problem goes away overnight, the queued message is
  dropped: nobody should wake up to news of a problem that no longer exists.
- **Recoveries go to whoever was told.** "It's fine again" is sent to the channels
  that actually received the "it broke" message, and to nobody else.

Dates take the same path. A reminder whose day is inside its lead window is a
condition; the incident it opens carries the notifications — one per lead day, at
the time of day the user chose — and remembers which have been sent, so a restart
neither repeats one nor loses it.

## The forecast

`internal/forecast` is pure: readings in, numbers out, no clock. A forecast is
"as of the last reading", so a meter that stopped reporting does not quietly grow a
longer runway.

**Burn rate.** Spend per day over the last 30 days, with each day's weight halving
every 7 days. A one-off batch job fades out instead of dragging the average for a
month, and a new habit shows up within days. The plain 7- and 30-day averages are
reported next to it so the estimate can be checked by eye.

**Runway** is the balance divided by that burn. It is only acted on once there is a
full day of history; before that it is shown, marked as thin.

**Bursts.** The same history is read a second way: how fast did money leave in the
last 3 hours, and in the last 24? A burst becomes an incident only if it is *both*

1. a real jump — at least three times the pace before it — **and**
2. soon enough to matter — at that rate the balance would be gone within days.

Either test alone is useless. A jump alone is noise: spending 2.50 after a week of
0.10 a day is "25 times the baseline" on a balance that still lasts two hundred
days (that exact false alarm is a test case). Soon alone is the runway rule's
business. Together they are what a leaked key looks like.

**Quota pace.** For an allowance with a known window: the share used so far divided
by the share of the window that has passed. Above 1, it runs out before it resets,
and the forecast says when.

## Reading a platform

A platform is a **definition**: one HTTP request and one [jq] program.

```jq
.balance_infos[]? | {
  key:  ("balance-" + (.currency | ascii_downcase)),
  kind: "balance", unit: .currency,
  value: (.total_balance | num)
}
```

The program receives the response and outputs meters. Calling `error("…")` fails the
sample with that message, which is how an API that answers 200 with an error inside
is handled. jq was chosen over a home-grown path syntax because platforms differ in
ways a path cannot express — a balance that is the difference of two fields, a list
of windows, a number hidden in a sentence — and because the people who run this
already know it.

The built-in catalog (`internal/provider/catalog.go`) is nothing but definitions;
it can do nothing a definition saved from the dashboard cannot. Each entry says how
it was verified: against the live API, by replaying a recorded response, or from
documentation only.

Things the fetcher is strict about, because it handles credentials:

- **Redirects are not followed.** A redirect to another host would carry the
  credential with it.
- **Errors are redacted.** Platforms like to echo the key they rejected; every
  error is scrubbed of the account's field values before it is stored or shown.
- **The jq program sees the response and nothing else** — no environment, no
  files, a two-second budget, and only the account's non-secret fields.

[jq]: https://jqlang.org/

## Notifications

`internal/notify` turns a channel-independent `Message` into each platform's
dialect and reports honestly whether the platform took it. Several chat platforms
answer HTTP 200 to a message they have thrown away (wrong keyword, bad signature);
their verdict is in the response body and each driver reads it there.

A browser is a channel too. When someone turns on notifications on a device, its
push subscription becomes a channel of type `webpush`, so a phone gets the same
controls as a chat group: which topics, how severe, a test button, a delivery
log. When the browser unsubscribes, the push service says so and the channel is
removed — an ended subscription is not a failing channel.

Delivery goes through an **outbox** in the database: a row is written before
anything is sent, retried with backoff for about three hours, and kept afterwards
as the notification log. A channel that fails three deliveries in a row gets an
incident of its own, announced over the *other* channels.

## Storage

One SQLite file (`modernc.org/sqlite`, pure Go, WAL).

This is a deliberate choice. QuotaPulse is a single-user tool that samples a
handful of accounts every half hour; a database server would be the largest moving
part of the deployment and add nothing. One file makes backup a copy and migration
a move. Earlier versions supported three database engines with three sets of
generated queries, and every feature paid for it three times.

Two things worth knowing about the schema:

- **Flat runs are folded.** Most samples change nothing — a balance sits still for
  hours. A flat run is stored as two rows, where it began and where it currently
  ends; a third identical sample just moves the end. Nothing is lost (a straight
  line between the two is exactly what was observed) and a year of history stays
  small.
- **The database enforces the incident invariant.** A partial unique index allows
  at most one open incident per subject, kind and occurrence.

Credentials and channel settings are sealed with AES-256-GCM under a key derived
from `QUOTAPULSE_SECRET`, or from a key file created next to the database on first
start. The key file means a stolen database backup is useless on its own; it does
not protect against someone who can read the whole data directory, and nothing
here pretends otherwise.

## Configuration

The database is the single source of truth, edited from the dashboard. The
environment says only how to start — a password, a data directory, a port. There
are no feature flags: an unused feature is an empty list.

For those who want configuration in a file there is the **bundle**
(`internal/bundle`): one JSON document holding accounts, dates, channels and
settings by name. It is the export format, the import format, what
`QUOTAPULSE_BOOTSTRAP` loads into an empty database, and what the tool that
migrates a pre-1.0 install produces.

## Access

- **The dashboard** signs in with one password and gets an HttpOnly, SameSite=Strict
  session cookie. The cookie is a signed expiry, not a server-side session; the
  password is mixed into the signing key, so changing it signs everyone out.
  Requests that change something must also come from the dashboard's own origin.
- **Scripts and agents** use API tokens, of which only a hash is stored, with
  scopes: `read`, `act` (acknowledge, complete, manage dates), `ingest` (push
  readings), `admin`.
- **MCP** (`/mcp`) accepts tokens only, and offers exactly the tools the token's
  scopes allow.

Without `QUOTAPULSE_PASSWORD` nobody can sign in. Something that holds API keys
should never be open by default.

## Layout

```
cmd/quotapulse/      entry point: serve, demo, export, import, healthcheck
internal/
  core/              the vocabulary: Meter, Reminder, Incident, Channel, Settings
  recur/             recurrence rules, solar and lunar — pure
  forecast/          burn rate, runway, bursts, quota pace — pure
  provider/          definitions, the built-in catalog, the fetcher, request signing
  notify/            messages and the channel drivers
  secret/            encryption at rest, sessions, tokens
  store/             SQLite: schema, queries, nothing else
  engine/            sample -> evaluate -> reconcile -> announce; the views; every write
  runtime/           the background loops
  api/               HTTP: JSON API, event stream, metrics, the dashboard's files
  mcp/               tools for AI agents
  bundle/            export and import
  i18n/              the sentences that leave the server, in English and Chinese
  demo/              generated data and a fake internet, for trying it and for screenshots
web/                 the dashboard: Preact + TypeScript, built into the binary
tools/legacy-import/ migrates a pre-1.0 database; a module of its own
```

Dependencies point one way: `core`, `recur` and `forecast` know nothing of storage
or HTTP; `engine` is the only package that writes; `api` and `mcp` translate and
decide nothing. Anything the dashboard can do, a token or an MCP tool does through
the same engine call, under the same rules.

## Working on it

```bash
npm --prefix web ci && npm --prefix web run build   # the dashboard; the binary embeds it
go build ./cmd/quotapulse
go test -race ./...                                  # the engine tests run on a clock that only moves when told to
(cd tools/legacy-import && go test ./...)

./quotapulse demo --lang zh                          # realistic data, no real credentials; password: demo
QUOTAPULSE_WEB_DIR=web/dist ./quotapulse demo        # serve the dashboard from disk while working on it
```

The demo runs the real engine against a fake internet (`internal/demo`): every
platform it samples and every chat app it notifies is answered locally. It is the
quickest way to see a change, and it is what the screenshots are taken from.

# QuotaPulse

**Tells you before something you depend on runs out or comes due.**

A small self-hosted service that watches two kinds of things:

- **things that run out** — prepaid API balances, plan quotas that reset, what a
  tool has cost this month — and works out *how long each will last*;
- **things that come due** — renewals, expiries, birthdays, on the solar or the
  Chinese lunar calendar.

It says something when that changes: when a balance is on course to run out, when
it gets worse, when it recovers, when a date is near. Not every morning. One
binary, one SQLite file, a dashboard, and an MCP endpoint so your agents can ask it
too.

[简体中文](README.zh-CN.md)

![The overview: what needs attention, how long each meter lasts, what is coming up](docs/images/overview-light.png)

## What it does

**It forecasts instead of comparing to a threshold.** "430 left" means little; "at
this pace, gone on the 22nd" is something to act on. The burn rate weights recent
days more, so a one-off job fades and a new habit shows up within days. A
threshold is still there as a last line.

**It knows a quota from a balance.** A coding plan's five-hour window and its
monthly tool quota are separate meters, each with its reset time — and with the
window's length known, whether it will last until it resets.

**It notices a leak without crying wolf.** A burst of spend becomes an incident
only if it is both a real jump *and* fast enough to empty the balance within days.
Spending 2.50 after a quiet week is not news on a balance of 500.

**It speaks on change.** An incident opens, may escalate, and resolves; those are
the messages. A warning is said once. Only an unacknowledged *critical* incident
repeats. Quiet hours hold back what can wait, and a problem that fixes itself
overnight is never announced.

**Dates, with the lunar calendar done properly.** Weekly, monthly, yearly, every
N, one-off. A lunar birthday lands on the right day every year, including the
ones whose month is a day short.

| | |
| --- | --- |
| ![A meter: its history, where it is heading, and the rules that watch it](docs/images/meter-light.png) | ![Dates: each one with the day it lands on this time](docs/images/dates-light.png) |

**It tells you where you are.** Feishu/Lark, DingTalk, WeCom, Slack, Telegram,
ntfy, Bark, a plain webhook, or the browser itself. Each channel picks its topics
and how severe something must be. Delivery is retried from an outbox, and a
channel that keeps failing is itself reported — through the others.

**Agents can ask.** `/mcp` gives an AI agent "what needs attention" and "does this
account have headroom" before it starts a large job, under a scoped token.

**Anything can report in.** No API for it? Push the number:
`POST /api/v1/ingest`. A pushed meter gets the same forecast and rules.

## Try it

No credentials needed — the demo runs the real engine against a fake internet:

```bash
docker compose run --rm -p 8080:8080 quotapulse demo --lang en
# http://localhost:8080, password: demo
```

## Run it

```bash
cp .env.example .env          # set QUOTAPULSE_PASSWORD
mkdir -p data
docker compose up -d          # builds the image from this checkout the first time
```

Open http://localhost:8080, sign in, then add an account, a notification channel,
and a date. Everything is configured in the dashboard.

- **A published image instead of building.** Each release publishes a multi-arch
  image (amd64, arm64) to the GitHub Container Registry under this repository's
  name: `ghcr.io/<owner>/quotapulse:<version>`. Put that in `.env` as
  `QUOTAPULSE_IMAGE`, then `docker compose pull && docker compose up -d --no-build`.
- **The data directory must be writable** by the container's user, 65532:
  `sudo chown 65532:65532 data` — or run the container as yourself by setting
  `QUOTAPULSE_USER=$(id -u):$(id -g)` in `.env`.
- **Without Docker:** `npm --prefix web ci && npm --prefix web run build`, then
  `go build ./cmd/quotapulse` and `QUOTAPULSE_PASSWORD=… ./quotapulse`.
- **Put TLS in front** before exposing it beyond the machine; any reverse proxy
  will do. The dashboard's live updates are a long-lived response, so do not
  buffer it.

### Settings from the environment

Only what is needed to start. Everything else lives in the database and is edited
in the dashboard.

| Variable | Default | |
| --- | --- | --- |
| `QUOTAPULSE_PASSWORD` | — | Signs you in. Without it nobody can |
| `QUOTAPULSE_DATA` | `./data` (`/data` in the image) | The database, its daily backups, and the key file |
| `QUOTAPULSE_LISTEN` | `:8080` | |
| `QUOTAPULSE_SECRET` | a key file in the data directory | Encrypts stored credentials. Set once; never change it |
| `QUOTAPULSE_BOOTSTRAP` | — | A [bundle](docs/bundle.md) to import into an empty database |
| `QUOTAPULSE_MUTE` | — | `1` records notifications without sending them |
| `QUOTAPULSE_LOG`, `QUOTAPULSE_LOG_LEVEL` | `text`, `info` | `json` for structured logs |
| `TZ`, `QUOTAPULSE_LANG` | `UTC`, `en` | Time zone and language (`en`, `zh`) of a **new** install; change them later in the dashboard |

## Platforms

Built in: DeepSeek, Zhipu GLM Coding Plan, OpenRouter, SiliconFlow, Moonshot
(Kimi), Volcengine, Alibaba Cloud, TikHub, UniAPI, WxRank.

A platform is one HTTP request and one jq expression, so adding yours takes a few
lines and no code — in the dashboard, with a live test against your own key. See
[docs/providers.md](docs/providers.md), which also says how each built-in
definition was verified.

## Agents

Create a token under **Settings → Tokens** (`read` is enough to ask; add `act` to
let an agent add dates or acknowledge incidents) and point an MCP client at `/mcp`:

```json
{"mcpServers": {"quotapulse": {"type": "http", "url": "https://pulse.example/mcp",
  "headers": {"Authorization": "Bearer qp_…"}}}}
```

| | |
| --- | --- |
| `attention` | What is open right now and what is coming up. Start here |
| `list_meters`, `get_meter` | Every meter with its forecast and a one-word `headroom`: `ok`, `low`, `exhausted`, `unknown` |
| `list_dates`, `get_spend`, `list_activity` | Dates, a month of spend, incident history |
| `add_date`, `complete_date`, `acknowledge`, `sample_now` | With the `act` scope |
| `record_reading` | With the `ingest` scope |

## Reporting from a script

```bash
curl -X POST https://pulse.example/api/v1/ingest \
  -H "Authorization: Bearer qp_…" -H "Content-Type: application/json" \
  -d '{"meter": "claude-code", "kind": "counter", "unit": "USD", "value": 61.75}'
```

[`scripts/claude-code-usage.py`](scripts/claude-code-usage.py) reports what Claude
Code has cost this month; run it from cron. Give the meter a monthly budget to be
warned when the month is heading past it, and a "silent for" limit to hear when the
script stops running.

## Backups

The database is backed up into `data/backups/` once a day; the last seven are
kept. For a copy you can read, or to move to another machine:

```bash
docker compose exec quotapulse /quotapulse export --secrets > backup.json
```

Everything is in the data directory. Back it up as a whole — the key file with
it, or the stored credentials cannot be read.

## More

- [docs/API.md](docs/API.md) — the HTTP API, the webhook payload, Prometheus metrics
- [docs/providers.md](docs/providers.md) — reading a platform
- [docs/bundle.md](docs/bundle.md) — export, import, and starting from a file
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — how it is built, and why
- [docs/migrating-from-0.x.md](docs/migrating-from-0.x.md) — coming from an earlier version

## Development

```bash
npm --prefix web ci && npm --prefix web run build
go test -race ./...
(cd tools/legacy-import && go test -mod=mod ./...)
npm --prefix web run typecheck && npm --prefix web test

go run ./cmd/quotapulse demo --lang zh                        # try a change
QUOTAPULSE_WEB_DIR=web/dist go run ./cmd/quotapulse demo      # with `npm --prefix web run watch` alongside
```

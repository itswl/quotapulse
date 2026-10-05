# QuotaPulse

API balance and credit monitoring with subscription lifecycle management, alerts, history, and a read-only MCP interface.

QuotaPulse is a single Go binary that provides a web dashboard, HTTP API, scheduled checks, Webhook notifications, IMAP email scanning, Prometheus metrics, and optional persistent history. It monitors multiple providers, estimates runway from balance history, detects spending spikes, and tracks subscription renewals.

See [docs/overview.md](docs/overview.md) for a screenshot tour of the dashboard, and [docs/icons.md](docs/icons.md) for the icon system.

中文文档请参阅 [README.zh-CN.md](README.zh-CN.md)。

## Quick start

```bash
cp .env.example .env
# Edit .env and set WEB_API_KEY, WEBHOOK_URL, and provider API keys.
go build -o quotapulse ./cmd/quotapulse
./quotapulse -show-config
./quotapulse
```

The dashboard and API are available at `http://localhost:8080`. Docker users can run `docker compose up -d` instead.

By default, QuotaPulse discovers any provider configured with `{PROVIDER}_API_KEY`. Set `{PROVIDER}_THRESHOLD` to enable low-balance alerts for that provider. Use database-backed dynamic configuration when you need to manage many accounts, subscriptions, or mailboxes from the dashboard.

## MCP

Set `ENABLE_MCP=true` to expose a read-only Streamable HTTP MCP endpoint at `/mcp`. Give each agent its own key in `MCP_API_KEYS` instead of sharing `WEB_API_KEY`: an MCP key works only on `/mcp`, sees only the tools of its scopes, has its own rate limit, and can expire. Entries are `name:key[:scopes[:expires]]`, separated by `;`:

```
MCP_API_KEYS=claude:qp_7f3a91c2d4e5f6a7b8c9:balance,alerts;ops:qp_0123456789abcdef01:all:2026-12-31
```

The scopes are `balance` (balances, runways, history, trends, spending, and provider status), `alerts` (alert history, the event timeline, and alert statistics), `subscriptions` (renewal status and upcoming renewals), `email` (mailbox scans and email alerts), and `config`, which adds the configuration views of a key's other scopes; `all` is the default. Health, capabilities, jobs, and the provider catalog need no scope. An expiry is a date, valid through that day in UTC, or an RFC 3339 time. `WEB_API_KEY` keeps working on `/mcp` with every scope until `MCP_REQUIRE_SCOPED_KEY=true`. Each key gets `MCP_RATE_LIMIT_PER_MINUTE` requests a minute, and every tool call is logged with the key's name; `./quotapulse -show-config` lists the keys and flags expired ones.

Agents should start with `dashboard_summary`, which lists what needs attention, critical first: failed checks, low balances, short runways, renewals due, alert emails, and failing jobs. Every tool returns structured content with an output schema, and every result carries a `meta` block that says when its data was last updated and whether it is stale. History lists are newest first, take `since`/`until` (RFC 3339 or a date) and page with `next_cursor`; `balance_trend` returns every snapshot or one point per hour, day, or week, with the window's change, spending, and top-ups. `spend_summary` breaks spending and top-ups down per project and day, with totals per provider but never across providers, whose units and currencies differ; `provider_status` shows which providers fail and when each last answered; `upcoming_renewals` lists the renewals due soon. `balance_status` filters by project IDs or names, provider, owner project, and status, and `include_disabled` adds disabled projects. Resources mirror the main views: `quotapulse://dashboard/summary`, `quotapulse://alerts/recent`, `quotapulse://subscriptions/upcoming`, `quotapulse://projects/{project_id}/status`, and `quotapulse://state/{kind}`. Failures come back as tool errors with a category and whether retrying helps. The MCP interface never writes configuration, triggers a refresh or scan, or calls a provider, and results leave secrets out: no API keys, passwords, or webhook URLs, masked mailbox addresses, and provider errors with credentials redacted.

## Providers

| Provider | Key | Credential format |
| --- | --- | --- |
| OpenRouter, UniAPI, WeChat Rank, TikHub, DeepSeek | `openrouter`, `uniapi`, `wxrank`, `tikhub`, `deepseek` | API key |
| GLM Coding Plan | `glm` | `id.secret`; threshold is a percentage |
| Volcengine | `volc` | `AccessKeyId:SecretAccessKey` |
| Alibaba Cloud | `aliyun` | `AccessKeyId:AccessKeySecret` |

To monitor multiple accounts for one provider, use `{PROVIDER}_1_API_KEY`, `{PROVIDER}_2_API_KEY`, and matching threshold variables. For example, `VOLC_1_API_KEY` becomes the `volc-1` project.

Adding a simple provider usually requires a `RegisterSpec` declaration in `internal/provider/`. Providers that require request signing can implement the `Provider` interface directly.

## Configuration

Configuration has two sources:

| Source | Use | Activation |
| --- | --- | --- |
| Environment variables or Kubernetes Secrets | Credentials, Webhooks, database connection, feature flags, schedules | Restart after changes |
| Database dynamic configuration | Projects, subscriptions, and mailboxes | `ENABLE_DATABASE=true` and `ENABLE_DYNAMIC_CONFIG=true` |

Database entries take precedence over automatically discovered environment entries. Do not put inline comments after `.env` values; use a separate comment line.

The most important variables are:

| Variable | Default | Purpose |
| --- | --- | --- |
| `WEB_API_KEY` | unset | Authentication for `/api/*`; comma-separated keys enable rotation; requests return 503 when unset |
| `ENABLE_MCP` | `false` | Enable the read-only `/mcp` endpoint |
| `MCP_API_KEYS` | unset | Read-only agent keys for `/mcp`, as `name:key[:scopes[:expires]]` separated by `;`; see [MCP](#mcp) |
| `MCP_REQUIRE_SCOPED_KEY` | `false` | Stop accepting `WEB_API_KEY` on `/mcp` |
| `MCP_RATE_LIMIT_PER_MINUTE` | `120` | Requests a minute for each MCP key; `0` disables the limit |
| `WEBHOOK_URL` / `WEBHOOK_TYPE` | unset / `custom` | Alert destination; supported types include `feishu`, `dingtalk`, `wecom`, and `custom` |
| `BALANCE_REFRESH_INTERVAL_SECONDS` | `3600` | Dashboard refresh interval |
| `ALERT_SCHEDULE` | `09:00,15:00` | Scheduled balance and subscription checks; `off` disables them |
| `ENABLE_DATABASE` / `DATABASE_URL` | `false` / SQLite URL | Enable history and dynamic configuration |
| `ENABLE_HISTORY_API` | `false` | Enable history endpoints and trend charts |
| `ENABLE_SUBSCRIPTIONS` | `false` | Enable renewal reminders |
| `ENABLE_EMAIL_SCAN` | `false` | Show the Email scanning view before any mailbox is configured, so the first one can be added from the dashboard; a configured mailbox shows it anyway |
| `ENABLE_PROMETHEUS` / `METRICS_PORT` | `false` / `9100` | Expose Prometheus metrics |
| `BURN_RATE_WINDOW_DAYS` / `RUNWAY_ALERT_DAYS` | `7` / `7` | Runway calculation and alert threshold |
| `SPEND_SPIKE_RATIO` | `3` | Spending-spike multiplier; `0` disables the alert |
| `ALERT_COOLDOWN_SECONDS` | `86400` | Per-alert notification cooldown |
| `JOB_FAILURE_ALERT_THRESHOLD` | `3` | Consecutive failures before a job escalates to the webhook; `0` disables |
| `JOB_TIMEOUT_SECONDS` | `600` | Per-run timeout for scheduled jobs; `0` disables |
| `DB_BACKUP_SCHEDULE` | unset | Daily times for SQLite backups, e.g. `03:00`; unset disables backups |
| `DB_BACKUP_DIR` / `DB_BACKUP_KEEP` | `backups` / `7` | Where SQLite backups live and how many are kept |

See [.env.example](.env.example) for the complete reference.

## Scheduled jobs

All jobs run inside the web process; the container does not require cron.

| Job | Schedule | Notifications |
| --- | --- | --- |
| `dashboard_refresh` | At startup and every refresh interval | Only when `ENABLE_WEB_ALARM=true` |
| `alert_check` | `ALERT_SCHEDULE` | Balance and subscription alerts |
| `email_scan` | `EMAIL_SCAN_SCHEDULE` | Email alerts |
| `weekly_report` | `WEEKLY_REPORT_SCHEDULE` | Weekly spending, runway, and renewal summary |

`/health` returns 503 when data is missing, stale, or an enabled job failed. `GET /api/jobs` provides the job details.

## Runway and history

Balance history is stored as snapshots. A decrease between adjacent snapshots is spending; an increase is a top-up. With at least four points spanning six hours, QuotaPulse estimates daily burn rate and runway, and can detect spending spikes. Results covering less than one day are marked low confidence and are not used for alerts.

The dashboard shows the estimated days remaining for each account. Weekly reports summarize spending, runway ranking, and subscription costs for the next 30 days. History requires `ENABLE_DATABASE=true`; when there is not enough history, the service falls back to threshold alerts.

## Dashboard, API, and deployment

The dashboard has project, subscription, and email views; an Alerts only filter on the projects view narrows it to accounts below their threshold or failing their check. The interface is available in English and Simplified Chinese: it follows the browser language, and the globe button in the top bar switches between the two and remembers the choice in that browser. When installed as a web app it can deliver balance, subscription, and job-failure alerts as browser push notifications (enable under Settings → Notifications). Enable dynamic configuration to create, update, and delete projects, subscriptions, and mailboxes from the UI. Enable the history API for trend charts and alert history. See [docs/API.md](docs/API.md) for the endpoint contract.

The TypeScript frontend is bundled with esbuild and embedded into the binary, so the runtime does not need a separate static-file directory.

The published image is `ghcr.io/itswl/quotapulse` and supports amd64 and arm64. The runtime image is based on `scratch` and contains only the static binary and CA certificates.

```bash
docker compose up -d
docker compose pull && docker compose up -d

# Add Prometheus and Grafana.
docker compose -f docker-compose.yml -f docker-compose.monitoring.yml up -d

# Run the current checkout with a local build.
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

Pin `QUOTAPULSE_VERSION` in production instead of relying on `latest`. Kubernetes manifests are in `k8s/common-prod.yaml`; replace `YOUR_REGISTRY` and `YOUR_DOMAIN` before applying them.

## Monitoring

Set `ENABLE_PROMETHEUS=true` to expose balance, subscription, email, job, and notification metrics at `:9100/metrics`. See [grafana/README.md](grafana/README.md) for the metric contract, dashboard, PromQL examples, and self-monitoring rules.

## CLI

```bash
./quotapulse
./quotapulse -show-config
./quotapulse -check -dry-run
./quotapulse -check -project PROJECT_NAME
./quotapulse -check-subscriptions
./quotapulse -check-email -email-days 3
./quotapulse -healthcheck
```

## Development

```bash
go test ./...
go test -race ./...
go build ./cmd/quotapulse
go vet ./...
gofmt -l .
npm --prefix ui run typecheck
npm --prefix ui test
npm --prefix ui run build
```

SQL queries are generated with [sqlc](https://sqlc.dev). After changing `internal/store/queries/*.sql`, run `sqlc generate`.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for package boundaries and data contracts.

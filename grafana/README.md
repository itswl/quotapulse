# Prometheus and Grafana

Set `ENABLE_PROMETHEUS=true` to expose `/metrics` on `METRICS_PORT` (default `9100`). Scheduled jobs, dashboard refreshes, and immediate scans all update the same process-local metrics; CLI-only runs do not.

## Metrics

| Metric | Labels | Meaning |
| --- | --- | --- |
| `quotapulse_balance`, `_threshold`, `_ratio`, `_status` | `project`, `provider`, `type` | Current balance, threshold, balance/threshold, and 1 for healthy or 0 for alert |
| `quotapulse_check_status` | `project`, `provider`, `type` | Last check status; failed checks retain the last successful balance |
| `quotapulse_burn_rate_per_day`, `_runway_days` | `project`, `provider`, `type` | Daily burn rate and estimated days remaining; emitted only with enough history |
| `quotapulse_subscription_days`, `_amount`, `_status` | `name`, `cycle_type` | Days to renewal, amount, and 1 healthy, 0 due, or -1 already renewed |
| `quotapulse_email_mailbox_status` | `mailbox` | Last mailbox scan status |
| `quotapulse_email_last_scan_emails`, `_alerts` | `mailbox` | Email and alert counts from the last scan |
| `quotapulse_email_scan_total`, `_alerts_total` | `mailbox` | Cumulative email and alert counts |
| `quotapulse_job_last_run_timestamp`, `_last_success_timestamp`, `_last_status`, `_last_duration_seconds` | `task` | Last run, last success, status, and duration |
| `quotapulse_job_runs_total` | `task`, `status` | Job run count |
| `quotapulse_notifications_total` | `kind`, `status` | Webhook count; kinds include `balance`, `subscription`, `email`, `mailbox_error`, `runway`, `spend_spike`, and `weekly_report` |
| `quotapulse_last_check_timestamp` | `check_type` | Last balance, subscription, or email check |
| `quotapulse_http_requests_total` | `route`, `method`, `status` | Requests handled, keyed by the matched route pattern (`/api/credits`), never the raw path; unmatched requests collapse into `route="unmatched"` |
| `quotapulse_http_request_duration_seconds` | `route`, `method` | Request latency histogram (5 ms to 10 s buckets) |

When a project, subscription, or mailbox is renamed or deleted, obsolete gauge series are removed on the next update. The job label is `task`; Prometheus may rename it to `exported_job` to avoid its built-in `job` label.

HTTP series are counters: they reset to zero when the process restarts, which is why every panel and rule uses `rate(...)`. Requests rejected before routing — a missing or wrong API key returns 401, an unset `WEB_API_KEY` returns 503 — count under `route="unmatched"`; the status code still separates them from 404 noise.

## Dashboard

`docker compose -f docker-compose.yml -f docker-compose.monitoring.yml up -d` provisions Prometheus, Grafana, the data source, and the `quotapulse` dashboard. The dashboard contains:

- Overview: project totals, healthy and alerting projects, failed checks, renewals due within seven days, and the last balance check.
- Balances: ratio gauges, trends, and a type-filtered detail table.
- Renewals: countdown and status table.
- Email scanning: mailbox totals, failures, latest hits, details, and daily alert counts.
- Jobs and notifications: failed jobs, recent sends and failures, job status, and notification types.
- HTTP endpoints: request rate and p95 latency per route, plus 5xx share and latency totals.

Filters are `project`, `type`, `subscription`, and `mailbox`. Balance units differ between providers, so compare projects using the ratio panels.

To import the dashboard into an existing Grafana instance:

```bash
cd grafana && ./import-dashboard.sh http://localhost:3000 admin <password>
```

The provisioned dashboard is restored from disk after a Compose restart. Export UI changes from Settings → JSON Model and replace the checked-in dashboard file.

## PromQL examples

```promql
quotapulse_balance and on(project, provider, type) quotapulse_status == 0
bottomk(5, quotapulse_ratio)
quotapulse_check_status == 0
bottomk(5, quotapulse_runway_days)
quotapulse_runway_days < 7
quotapulse_burn_rate_per_day > 1.5 * avg_over_time(quotapulse_burn_rate_per_day[7d])
quotapulse_email_mailbox_status == 0
increase(quotapulse_email_alerts_total[7d])
time() - quotapulse_job_last_success_timestamp > 26 * 3600
increase(quotapulse_notifications_total{status="failed"}[1h]) > 0
histogram_quantile(0.95, sum by (le, route) (rate(quotapulse_http_request_duration_seconds_bucket[5m]))) > 0.5
sum(rate(quotapulse_http_requests_total{status=~"5.."}[5m])) / sum(rate(quotapulse_http_requests_total[5m])) > 0.01
```

## Self-monitoring rules

QuotaPulse sends its own balance alerts through the configured Webhook. These optional rules monitor the monitor:

```yaml
groups:
  - name: quotapulse-self
    rules:
      - alert: BalanceAlertJobFailed
        expr: quotapulse_job_last_status == 0
        for: 10m
        annotations: { summary: "QuotaPulse task {{ $labels.task }} failed" }
      - alert: BalanceAlertJobStale
        expr: time() - quotapulse_job_last_success_timestamp{task="alert_check"} > 26 * 3600
        annotations: { summary: "alert_check has not succeeded for 26 hours" }
      - alert: BalanceAlertRunwayShort
        expr: quotapulse_runway_days < 3
        for: 1h
        annotations: { summary: "{{ $labels.project }} has less than three days of runway" }
      - alert: BalanceAlertMailboxDown
        expr: quotapulse_email_mailbox_status == 0
        for: 1h
        annotations: { summary: "Mailbox {{ $labels.mailbox }} is unavailable" }
      - alert: BalanceAlertNotificationFailed
        expr: increase(quotapulse_notifications_total{status="failed"}[1h]) > 0
        annotations: { summary: "Webhook notification failed ({{ $labels.kind }})" }
```

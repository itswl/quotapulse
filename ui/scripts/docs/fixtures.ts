/**
 * Demo data for the documentation screenshots (ui/scripts/docs-screenshots.mjs).
 *
 * Everything is derived the way the real server derives it, from a pinned clock, so the
 * screenshots are reproducible and never show a state the product can't produce:
 * - each project has one hourly balance series; its card's runway uses the server's
 *   formula (internal/runway) over the last 7 days, and its trend dialog shows the same
 *   series;
 * - renewal dates come from each subscription's renewal day and cycle, and a renewal mark
 *   pays for one renewal (internal/subscription);
 * - errors are the messages the real adapters and handlers return.
 *
 * Typed against src/api/types.ts, so `npm run typecheck` catches drift from the API.
 * Runs directly under Node's type stripping: keep to erasable syntax.
 */

import type {
  CheckResult,
  CreditsResponse,
  EmailAlert,
  EmailAlertRecord,
  EmailScanState,
  EmailSuppression,
  FeaturesResponse,
  HealthResponse,
  JobsResponse,
  MailboxConfig,
  ProjectConfig,
  ProvidersResponse,
  Runway,
  SubscriptionConfig,
  SubscriptionResult,
  SubscriptionsResponse,
  TrendResponse,
} from '../../src/api/types.js';

/* The pinned clock: Tuesday 2026-09-29 10:00 in Asia/Shanghai (UTC+8, no DST). */
export const NOW = Date.parse('2026-09-29T10:00:00+08:00');
export const TIMEZONE = 'Asia/Shanghai';
const OFFSET_MS = 8 * 3600_000;
const HOUR = 3600_000;
const DAY = 24 * HOUR;

const isoZ = (ms: number): string => new Date(ms).toISOString();
/* YYYY-MM-DD of an instant in UTC+8. */
const localDate = (ms: number): string => new Date(ms + OFFSET_MS).toISOString().slice(0, 10);
const round = (value: number, places: number): number => Math.round(value * 10 ** places) / 10 ** places;

// ==================== Balances ====================

interface DemoProject {
  name: string;
  provider: string;
  owner: string | null;
  type: 'balance' | 'credits' | 'quota';
  threshold: number;
  /* Hourly balances, oldest first, ending at NOW. */
  series: number[];
  error?: string;
}

/* A steady hourly spend ending at `current`, with an optional top-up `daysAgo` back. */
function spending(current: number, perDay: number, topUp?: { daysAgo: number; amount: number }): number[] {
  const hours = 30 * 24;
  const values: number[] = [];
  for (let h = hours; h >= 0; h -= 1) {
    // Working hours burn more than nights, like real API traffic.
    const at = NOW - h * HOUR;
    const localHour = new Date(at + OFFSET_MS).getUTCHours();
    const weight = localHour >= 9 && localHour < 21 ? 1.5 : 0.5;
    values.push(weight);
  }
  // values[i] weighs the step into point i, so values[0] never counts.
  const total = values.slice(1).reduce((sum, w) => sum + w, 0);
  const perWeight = (perDay * 30) / total;
  const series: number[] = new Array(values.length);
  let balance = current;
  series[values.length - 1] = current;
  for (let i = values.length - 1; i > 0; i -= 1) {
    balance += (values[i] ?? 0) * perWeight;
    if (topUp && i === values.length - 1 - topUp.daysAgo * 24) balance -= topUp.amount;
    series[i - 1] = round(balance, 2);
  }
  return series;
}

const flat = (value: number): number[] => new Array(30 * 24 + 1).fill(value);

const PROJECTS: DemoProject[] = [
  { name: 'deepseek-prod', provider: 'deepseek', owner: 'Assistant platform', type: 'balance', threshold: 50, series: spending(430.37, 62.5, { daysAgo: 9, amount: 800 }) },
  { name: 'openrouter-main', provider: 'openrouter', owner: 'Assistant platform', type: 'credits', threshold: 200, series: spending(1284.12, 31.2) },
  { name: 'volc-1', provider: 'volc', owner: 'Speech pipeline', type: 'balance', threshold: 50, series: spending(18.4, 15.3, { daysAgo: 18, amount: 250 }) },
  { name: 'glm-coding', provider: 'glm', owner: null, type: 'quota', threshold: 10, series: flat(42.35) },
  {
    name: 'aliyun-ops', provider: 'aliyun', owner: 'Infrastructure', type: 'balance', threshold: 100, series: [],
    error: 'API returned an error: Specified access key is disabled. (Code: InvalidAccessKeyId.Inactive)',
  },
  { name: 'uniapi-batch', provider: 'uniapi', owner: 'Data labelling', type: 'credits', threshold: 100, series: spending(96.5, 6.9) },
  { name: 'tikhub-scraper', provider: 'tikhub', owner: 'Data labelling', type: 'balance', threshold: 500, series: spending(7320, 12.1) },
  { name: 'wxrank', provider: 'wxrank', owner: null, type: 'credits', threshold: 20, series: flat(55.2) },
];

/* internal/runway Compute: flows between consecutive snapshots over the window. */
function computeRunway(p: DemoProject, windowDays = 7): Runway | null {
  if (p.type === 'quota' || p.series.length === 0) return null; // quota plans are never estimated
  const points = p.series.slice(-(windowDays * 24 + 1));
  let consumed = 0;
  let toppedUp = 0;
  const perDay = new Map<string, number>();
  for (let i = 1; i < points.length; i += 1) {
    const delta = (points[i - 1] ?? 0) - (points[i] ?? 0);
    const day = localDate(NOW - (points.length - 1 - i) * HOUR);
    if (delta > 0) {
      consumed += delta;
      perDay.set(day, (perDay.get(day) ?? 0) + delta);
    } else if (delta < 0) {
      toppedUp -= delta;
    }
  }
  const spanHours = points.length - 1;
  const burn = round(consumed / Math.max(spanHours / 24, 1 / 24), 4);
  const current = points[points.length - 1] ?? 0;
  const days = burn > 0 ? round(Math.max(current, 0) / burn, 2) : null;
  const daily = [...Array(windowDays + 1).keys()].map((i) => {
    const date = localDate(NOW - (windowDays - i) * DAY);
    return { date, consumed: round(perDay.get(date) ?? 0, 4) };
  });
  const earlier = daily.slice(0, -1).map((d) => d.consumed).sort((a, b) => a - b);
  const baseline = earlier.length ? (earlier[Math.floor(earlier.length / 2)] ?? null) : null;
  const today = daily[daily.length - 1]?.consumed ?? null;
  return {
    project_id: `${p.provider}:${p.name}`,
    project_name: p.name,
    provider: p.provider,
    balance_type: p.type,
    current_balance: current,
    window_days: windowDays,
    data_points: points.length,
    span_hours: spanHours,
    consumed: round(consumed, 4),
    topped_up: round(toppedUp, 4),
    burn_per_day: burn,
    runway_days: days,
    depletion_date: days === null ? null : localDate(NOW + days * DAY),
    confidence: 'high',
    daily,
    today_consumed: today,
    baseline_consumed: baseline,
    spike_ratio: today !== null && baseline ? round(today / baseline, 2) : null,
    monthly_projection: burn * 30,
  };
}

function checkResult(p: DemoProject): CheckResult {
  if (p.error) {
    return {
      project: p.name, owner_project: p.owner, provider: p.provider, type: p.type, success: false,
      credits: null, threshold: null, need_alarm: false, alarm_sent: true, error: p.error, cached: false,
    };
  }
  const credits = p.series[p.series.length - 1] ?? 0;
  return {
    project: p.name, owner_project: p.owner, provider: p.provider, type: p.type, success: true,
    credits, threshold: p.threshold, need_alarm: credits < p.threshold, alarm_sent: credits < p.threshold,
    error: null, cached: false, runway: computeRunway(p),
  };
}

export function credits(empty = false): CreditsResponse {
  const projects = empty ? [] : PROJECTS.map(checkResult);
  return {
    last_update: isoZ(NOW - 3 * 60_000),
    projects,
    summary: {
      total: projects.length,
      success: projects.filter((p) => p.success).length,
      failed: projects.filter((p) => !p.success).length,
      need_alarm: projects.filter((p) => p.need_alarm).length,
    },
  };
}

/* /api/history/trend: the project's own hourly series over 30 days, as the store keeps it. */
export function trend(id: string): TrendResponse | null {
  const p = PROJECTS.find((x) => `${x.provider}:${x.name}` === id);
  if (!p || p.series.length === 0) return null;
  const first = p.series[0] ?? 0;
  const last = p.series[p.series.length - 1] ?? 0;
  const history = p.series.map((balance, i) => ({
    timestamp: isoZ(NOW - (p.series.length - 1 - i) * HOUR),
    balance,
    need_alarm: balance < p.threshold,
  }));
  return {
    status: 'success',
    data: {
      project_id: id, project_name: p.name, days: 30, data_points: history.length,
      current_balance: last,
      min_balance: Math.min(...p.series), max_balance: Math.max(...p.series),
      avg_balance: p.series.reduce((sum, v) => sum + v, 0) / p.series.length,
      threshold: p.threshold,
      first_timestamp: history[0]?.timestamp ?? isoZ(NOW), last_timestamp: isoZ(NOW),
      history,
      change: last - first,
      change_percent: first ? ((last - first) / first) * 100 : 0,
    },
  };
}

export function projectsConfig(empty = false): ProjectConfig[] {
  return empty
    ? []
    : PROJECTS.map((p) => ({
        name: p.name, provider: p.provider, api_key: '********', threshold: p.threshold,
        type: p.type, owner_project: p.owner, enabled: true,
      }));
}

// ==================== Subscriptions ====================

interface DemoSubscription {
  config: SubscriptionConfig;
  /* The notification outcome of the last check, for those inside their window. */
  state?: { alert_state: string; next_eligible_hours?: number; last_error?: string };
}

const SUBSCRIPTIONS: DemoSubscription[] = [
  { config: sub('GitHub Copilot', 'Assistant platform', 'monthly', 4, 7, 10), state: { alert_state: 'sent', next_eligible_hours: 19 } },
  { config: sub('Tencent Cloud CVM', 'Speech pipeline', 'monthly', 1, 7, 68), state: { alert_state: 'cooldown_skipped', next_eligible_hours: 9 } },
  { config: sub('VPS traffic pack', 'Infrastructure', 'weekly', 1, 7, 52), state: { alert_state: 'failed', last_error: 'Webhook returned HTTP 500: internal error' } },
  { config: { ...sub('iCloud 2 TB', null, 'monthly', 11, 7, 21), last_renewed_date: '2026-09-08' } },
  { config: sub('Netflix', null, 'monthly', 23, 3, 99) },
  { config: sub('example.com domain', 'Infrastructure', 'yearly', 1225, 14, 68.5) },
  { config: sub('Tencent Cloud CVM annual', 'Speech pipeline', 'lunar_yearly', 503, 30, 1420) },
];

function sub(name: string, owner: string | null, cycle: SubscriptionConfig['cycle_type'], day: number, alertDays: number, amount: number): SubscriptionConfig {
  return { name, owner_project: owner, cycle_type: cycle, renewal_day: day, alert_days_before: alertDays, amount, enabled: true, last_renewed_date: null };
}

/* Lunar dates as of NOW, precomputed with internal/subscription (there's no lunar calendar here). */
const LUNAR_NEXT: Record<number, string> = { 503: '2027-06-07' };

const toUTC = (ymd: string): number => Date.parse(`${ymd}T00:00:00Z`);
const daysBetween = (from: string, to: string): number => Math.round((toUTC(to) - toUTC(from)) / DAY);
const clampDay = (year: number, month: number, day: number): string => {
  const last = new Date(Date.UTC(year, month + 1, 0)).getUTCDate();
  return new Date(Date.UTC(year, month, Math.min(day, last))).toISOString().slice(0, 10);
};

/* First renewal on or after `from` (YYYY-MM-DD): internal/subscription schedule.onOrAfter. */
function onOrAfter(c: SubscriptionConfig, from: string): string {
  const d = new Date(toUTC(from));
  const [y, m, dom] = [d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()];
  switch (c.cycle_type) {
    case 'weekly': {
      const iso = d.getUTCDay() === 0 ? 7 : d.getUTCDay();
      return localDate(toUTC(from) + ((c.renewal_day - iso + 7) % 7) * DAY - OFFSET_MS);
    }
    case 'yearly': {
      const [month, day] = [Math.floor(c.renewal_day / 100) - 1, c.renewal_day % 100];
      const thisYear = clampDay(y, month, day);
      return thisYear >= from ? thisYear : clampDay(y + 1, month, day);
    }
    case 'lunar_yearly':
      return LUNAR_NEXT[c.renewal_day] ?? from;
    default: {
      const thisMonth = clampDay(y, m, c.renewal_day);
      return dom <= c.renewal_day ? thisMonth : clampDay(y, m + 1, c.renewal_day);
    }
  }
}

function subscriptionResult({ config: c, state }: DemoSubscription): SubscriptionResult {
  const today = localDate(NOW);
  let next = onOrAfter(c, today);
  let renewed = false;
  if (c.last_renewed_date) {
    // The mark covers the renewal in whose window it was made (iCloud: 09-08 covers 09-11).
    const covered = onOrAfter(c, c.last_renewed_date);
    if (covered >= next) {
      renewed = true;
      next = onOrAfter(c, localDate(toUTC(covered) + DAY - OFFSET_MS));
    } else {
      renewed = daysBetween(today, next) > c.alert_days_before;
    }
  }
  const days = daysBetween(today, next);
  const needAlert = !renewed && days <= c.alert_days_before;
  return {
    name: c.name, owner_project: c.owner_project, renewal_day: c.renewal_day, cycle_type: c.cycle_type,
    days_until_renewal: days, next_renewal_date: next, need_alert: needAlert,
    alert_sent: needAlert && state?.alert_state === 'sent', amount: c.amount,
    already_renewed: renewed, last_renewed_date: c.last_renewed_date,
    ...(needAlert && state
      ? {
          alert_state: state.alert_state,
          next_eligible_at: state.next_eligible_hours ? isoZ(NOW + state.next_eligible_hours * HOUR) : null,
          last_error: state.last_error ?? null,
        }
      : {}),
  };
}

export function subscriptions(empty = false): SubscriptionsResponse {
  const list = empty ? [] : SUBSCRIPTIONS.map(subscriptionResult);
  return {
    last_update: isoZ(NOW - 3 * 60_000),
    subscriptions: list,
    summary: { total: list.length, need_alert: list.filter((s) => s.need_alert).length },
  };
}

export function subscriptionsConfig(empty = false): SubscriptionConfig[] {
  return empty ? [] : SUBSCRIPTIONS.map((s) => s.config);
}

// ==================== Email ====================

export function mailboxes(empty = false): MailboxConfig[] {
  if (empty) return [];
  return [
    { name: 'Ops mailbox', host: 'imap.exmail.qq.com', port: 993, username: 'ops@example.com', password: '', use_ssl: true, enabled: true },
    { name: 'Finance', host: 'imap.gmail.com', port: 993, username: 'finance@example.com', password: '', use_ssl: true, enabled: true },
    { name: 'Legacy account', host: 'imap.163.com', port: 143, username: 'legacy@example.com', password: '', use_ssl: false, enabled: false },
  ];
}

/* As the scanner reports them: the raw From and Date headers, the service from a bracket in
   the subject, the amount from the body, keywords in the default list's order. */
const ALERTS: EmailAlert[] = [
  {
    mailbox: 'Ops mailbox',
    subject: '[Volcengine] Your account balance is below 50 CNY',
    sender: 'Volcengine Billing <billing@volcengine.com>',
    date: 'Tue, 29 Sep 2026 07:41:03 +0800',
    keywords: ['top up', 'balance is below'],
    service_name: 'Volcengine',
    amount: 18.4,
    alert_sent: true,
  },
  {
    mailbox: 'Ops mailbox',
    subject: '【DeepSeek】余额不足提醒',
    sender: 'DeepSeek <noreply@deepseek.com>',
    date: 'Mon, 28 Sep 2026 16:05:12 +0800',
    keywords: ['余额不足'],
    service_name: 'DeepSeek',
    amount: 98.5,
    alert_sent: false,
    duplicate: true,
  },
];

export function emailScan(empty = false): EmailScanState {
  if (empty) return { last_update: null, days: null, dry_run: null, mailboxes: [], alerts: [], summary: {} };
  return {
    last_update: isoZ(NOW - 42 * 60_000),
    days: 7,
    dry_run: false,
    mailboxes: [
      { name: 'Ops mailbox', host: 'imap.exmail.qq.com', port: 993, username: 'ops@example.com', total_emails: 213, alert_count: 2, success: true, error: null },
      {
        name: 'Finance', host: 'imap.gmail.com', port: 993, username: 'finance@example.com', total_emails: 0, alert_count: 0, success: false,
        error: 'Failed to log in as finance@example.com: [AUTHENTICATIONFAILED] Invalid credentials (Failure)',
      },
    ],
    alerts: ALERTS,
    summary: { total_mailboxes: 2, failed_mailboxes: 1, total_emails: 213, total_alerts: 2, alerts_sent: 1 },
  };
}

export function emailHistory(empty = false): EmailAlertRecord[] {
  if (empty) return [];
  return [
    {
      id: 12, mailbox: 'Ops mailbox', sender: 'Volcengine Billing <billing@volcengine.com>',
      subject: '[Volcengine] Your account balance is below 50 CNY', date: 'Tue, 29 Sep 2026 07:41:03 +0800',
      service_name: 'Volcengine', amount: 18.4, matched_keywords: ['top up', 'balance is below'], alert_sent: true,
      timestamp: isoZ(NOW - 42 * 60_000),
    },
    {
      id: 9, mailbox: 'Ops mailbox', sender: 'DeepSeek <noreply@deepseek.com>',
      subject: '【DeepSeek】余额不足提醒', date: 'Mon, 28 Sep 2026 16:05:12 +0800',
      service_name: 'DeepSeek', amount: 98.5, matched_keywords: ['余额不足'], alert_sent: true,
      timestamp: isoZ(NOW - 18 * HOUR),
    },
  ];
}

export function suppressions(empty = false): EmailSuppression[] {
  return empty ? [] : [{ mailbox: 'Ops mailbox', sender: 'Cloud Deals <promo@cloud-deals.example>' }];
}

// ==================== Service ====================

export function features(): FeaturesResponse {
  return { status: 'success', features: { subscriptions: true, dynamic_config: true, history: true, email_scan: true, database: true } };
}

/* provider.All(): every registered adapter, sorted by key. */
export function providers(): ProvidersResponse {
  const list: Array<[string, string, 'balance' | 'credits' | 'quota']> = [
    ['aliyun', 'Alibaba Cloud', 'balance'], ['deepseek', 'DeepSeek', 'balance'], ['glm', 'GLM', 'quota'],
    ['openrouter', 'OpenRouter', 'credits'], ['tikhub', 'TikHub', 'balance'], ['uniapi', 'UniAPI', 'credits'],
    ['volc', 'Volcengine', 'balance'], ['wxrank', 'WxRank', 'credits'],
  ];
  return { status: 'success', providers: list.map(([value, label, type]) => ({ value, label, default_type: type })) };
}

export function jobs(): JobsResponse {
  const job = (name: string, description: string, schedule: string, lastMinutesAgo: number, nextMinutes: number) => ({
    name, description, schedule, enabled: true,
    next_run: isoZ(NOW + nextMinutes * 60_000), last_run: isoZ(NOW - lastMinutesAgo * 60_000),
    last_success: isoZ(NOW - lastMinutesAgo * 60_000), last_error: null,
    last_duration_seconds: 1.2, last_detail: null, runs: 168, failures: 0,
  });
  return {
    healthy: true,
    jobs: [
      job('dashboard_refresh', "Refresh the dashboard's balances and subscriptions (Check only; no alerts)", 'Every 3600 seconds', 3, 57),
      job('alert_check', 'Balance and subscription alert check; sends real notifications', 'Daily 09:00 / 15:00', 60, 300),
      job('email_scan', 'Scan mailboxes for billing and renewal emails from the last 7 days; sends real notifications', 'Daily 10:00', 42, 1398),
      job('weekly_report', 'Send the weekly summary of spending, runway, and upcoming renewals', 'Weekly Mon 09:00', 1 * 24 * 60 + 60, 6 * 24 * 60 - 60),
    ],
  };
}

export function health(version: string): HealthResponse {
  return {
    status: 'healthy', has_data: true, is_stale: false, jobs_healthy: true, failed_jobs: [],
    last_update: isoZ(NOW - 3 * 60_000), uptime_seconds: 86_400, version,
  };
}

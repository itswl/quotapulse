/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */

import type { BalanceType, CheckResult, Runway, SubscriptionResult } from './api/types.js';
import { t, type MessageKey } from './i18n/index.js';

/* Implementation note. */
export type RunwayLevel = 'danger' | 'warning' | 'normal' | 'unknown';

/* Implementation note. */
export type BalanceStatus = 'normal' | 'warning' | 'danger';

export interface RunwayDisplay {
  text: string;
  level: RunwayLevel;
  /* Implementation note. */
  hint: string;
}

/* Implementation note. */
function toNumber(value: unknown): number {
  if (typeof value === 'number') return value;
  return Number.parseFloat(String(value ?? ''));
}

/* "7 days" in the active language; the dictionary decides the singular. Accepts a
   preformatted number such as "6.9" so decimals survive. */
export function formatDays(count: number | string): string {
  return t('count.days', { count });
}

/* Two decimals with digit grouping, like balances: 1,420.00. Display only. */
export function formatCurrency(value: unknown): string {
  const num = toNumber(value);
  if (Number.isNaN(num)) return '-';
  return num.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

/* Implementation note. */
export function formatNumber(num: number): string {
  return new Intl.NumberFormat('en-US').format(num);
}

/* Implementation note. */
export function typeLabel(type: string | null | undefined): string {
  const keys: Record<string, MessageKey> = { credits: 'type.credits', balance: 'type.balance', quota: 'type.quota' };
  const key = type ? keys[type] : undefined;
  return key ? t(key) : type || t('type.balance');
}

/**
 * Implementation note.
 * Implementation note.
 */
export function formatBalance(value: unknown, type: BalanceType | string | null | undefined): string {
  const num = toNumber(value);
  if (Number.isNaN(num)) return '-';
  if (type === 'quota') {
    return `${num.toFixed(1)}<span class="unit">%</span>`;
  }
  return num.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

/* What formatRunway needs to say why a runway is missing. */
export interface RunwayContext {
  /* Quota plans reset on their own schedule, so the server never estimates them. */
  balanceType?: string | null;
  /* false: no database, so no balance history to estimate from. undefined: unknown. */
  database?: boolean;
}

/**
 * The runway for a card, or why there is none: a quota plan, no database, or a history
 * that is still too short. Never shows a missing estimate as 0 days.
 */
export function formatRunway(runway: Runway | null | undefined, context: RunwayContext = {}): RunwayDisplay {
  if (context.balanceType === 'quota') {
    return { text: t('runway.not_estimated'), level: 'unknown', hint: t('runway.not_estimated_hint') };
  }
  if (!runway || runway.confidence === 'none') {
    if (context.database === false) {
      return { text: t('runway.needs_history'), level: 'unknown', hint: t('runway.needs_history_hint') };
    }
    return { text: t('runway.accumulating'), level: 'unknown', hint: t('runway.accumulating_hint') };
  }
  if (!runway.burn_per_day) {
    return { text: t('runway.no_spending'), level: 'normal', hint: t('runway.no_spending_hint', { period: formatDays(runway.window_days) }) };
  }
  const days = runway.runway_days;
  if (days === null || days === undefined) {
    return { text: '—', level: 'unknown', hint: '' };
  }
  const hint = runway.depletion_date
    ? t('runway.depletion_hint', {
        burn: formatCurrency(runway.burn_per_day),
        monthly:
          runway.monthly_projection != null
            ? t('runway.monthly_projection', { amount: formatNumber(Math.round(runway.monthly_projection)) })
            : '',
        date: runway.depletion_date,
      })
    : '';
  if (days > 365) {
    return { text: t('runway.over_year'), level: 'normal', hint };
  }
  const text = days < 1 ? t('runway.under_day') : formatDays(days < 10 ? days.toFixed(1) : Math.round(days));
  // Implementation note.
  const level: RunwayLevel = days <= 3 ? 'danger' : days <= 7 ? 'warning' : 'normal';
  return { text, level, hint };
}

/* Implementation note. */
export function escapeHTML(value: unknown): string {
  const map: Record<string, string> = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  };
  return String(value ?? '').replace(/[&<>"']/g, (char) => map[char] ?? char);
}

/* Implementation note. */
export function escapeAttr(value: unknown): string {
  return escapeHTML(value);
}

/* A local date and time in the ISO order the rest of the dashboard uses: 2026-09-28 06:00. */
export function formatDate(dateString: string | null | undefined): string {
  if (!dateString) return '-';
  const date = new Date(dateString);
  if (Number.isNaN(date.getTime())) return '-';
  const pad = (n: number): string => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/* Implementation note. */
export function getRelativeTime(dateString: string | null | undefined, now: Date = new Date()): string {
  if (!dateString) return t('common.unknown');
  const date = new Date(dateString);
  const diff = now.getTime() - date.getTime();
  const minutes = Math.floor(diff / 60000);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (minutes < 1) return t('time.just_now');
  if (minutes < 60) return t('time.min_ago', { count: minutes });
  if (hours < 24) return t('time.hr_ago', { count: hours });
  if (days < 7) return t('count.days_ago', { count: days });
  return formatDate(dateString);
}

/* An account needs attention when it is below its threshold or its check failed: a
   revoked key must not drop out of the Alerts only view and its count. */
export function needsAttention(project: Pick<CheckResult, 'need_alarm' | 'success'>): boolean {
  return project.need_alarm || !project.success;
}

/* Implementation note. */
export function getBalancePercentage(balance: number, threshold: number): number {
  if (threshold === 0) return 100;
  return (balance / threshold) * 100;
}

/* The bar's colour: red below the alert threshold, matching the card's Alert status;
   amber within 1.5x of it, as the balance closes in; green otherwise. A threshold of 0
   never alerts. */
export function getBalanceStatus(balance: number, threshold: number): BalanceStatus {
  if (threshold <= 0) return 'normal';
  if (balance < threshold) return 'danger';
  if (balance < threshold * 1.5) return 'warning';
  return 'normal';
}

/* Implementation note. */
export function cycleLabel(cycle: string | null | undefined): string {
  if (cycle === 'monthly') return t('cycle.monthly');
  if (cycle === 'yearly') return t('cycle.yearly');
  if (cycle === 'lunar_yearly') return t('cycle.lunar_yearly');
  return t('cycle.weekly');
}

/* Red while the renewal is inside its reminder window and unpaid; amber under 14 days
   out. A renewed item is settled, and a weekly cycle is always under 14 days, so
   neither gets the amber warning. */
export function renewalUrgency(
  sub: Pick<SubscriptionResult, 'days_until_renewal' | 'need_alert' | 'already_renewed' | 'cycle_type'>,
): '' | 'warning' | 'danger' {
  if (sub.already_renewed) return '';
  if (sub.need_alert) return 'danger';
  if (sub.cycle_type !== 'weekly' && sub.days_until_renewal <= 14) return 'warning';
  return '';
}

/* ==================== Server cadence ====================
   The dashboard polls cached data on its own cadence; the server's own check schedule
   is a different knob (BALANCE_REFRESH_INTERVAL_SECONDS). Showing both side by side in
   Settings keeps that distinction visible. Returns null when there is nothing to show. */

export function formatServerCadence(
  job: { schedule: string; next_run?: string | null } | null | undefined,
): string | null {
  if (!job || !job.schedule) return null;
  const every = /^Every (\d+) seconds$/i.exec(job.schedule.trim());
  const cadence = every
    ? (() => {
        const seconds = Number(every[1]);
        if (seconds % 3600 === 0) return t('cadence.every_hr', { count: seconds / 3600 });
        if (seconds % 60 === 0) return t('cadence.every_min', { count: seconds / 60 });
        return t('cadence.every_s', { count: seconds });
      })()
    : job.schedule.toLowerCase();
  const next = job.next_run ? t('cadence.next_in', { until: formatUntil(job.next_run) }) : '';
  return t('cadence.server_checks', { cadence, next });
}

export function formatUntil(isoDate: string): string {
  const ms = new Date(isoDate).getTime() - Date.now();
  if (!Number.isFinite(ms) || ms <= 30_000) return t('time.under_minute');
  const minutes = Math.round(ms / 60_000);
  if (minutes < 60) return t('time.min', { count: minutes });
  const hours = Math.floor(minutes / 60);
  return minutes % 60 === 0 ? t('time.hr', { count: hours }) : t('time.hr_min', { hours, minutes: minutes % 60 });
}

/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */

import type { BalanceType, CheckResult, Runway, SubscriptionResult } from './api/types.js';

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

/* A count with its noun, singular for exactly one: "1 day", "3 days". */
export function pluralize(count: number, one: string, many = `${one}s`): string {
  return `${count} ${count === 1 ? one : many}`;
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
  const labels: Record<string, string> = { credits: 'Credits', balance: 'Balance', quota: 'Quota' };
  return (type ? labels[type] : undefined) ?? (type || 'Balance');
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
    return { text: 'Not estimated', level: 'unknown', hint: 'Quota plans reset on their own schedule, so they only use the alert threshold' };
  }
  if (!runway || runway.confidence === 'none') {
    if (context.database === false) {
      return { text: 'Needs history', level: 'unknown', hint: 'Runway estimates need balance history; set ENABLE_DATABASE=true' };
    }
    return { text: 'Accumulating data', level: 'unknown', hint: 'Estimates appear after several hours of balance history' };
  }
  if (!runway.burn_per_day) {
    return { text: 'No spending', level: 'normal', hint: `Balance did not decrease in the last ${pluralize(runway.window_days, 'day')}` };
  }
  const days = runway.runway_days;
  if (days === null || days === undefined) {
    return { text: '—', level: 'unknown', hint: '' };
  }
  const hint = runway.depletion_date
    ? `At an average daily spend of ${formatCurrency(runway.burn_per_day)}${runway.monthly_projection != null ? ` (≈${formatNumber(Math.round(runway.monthly_projection))}/mo)` : ''}, estimated to deplete around ${runway.depletion_date}`
    : '';
  if (days > 365) {
    return { text: 'More than 1 year', level: 'normal', hint };
  }
  const text = days < 1 ? 'Less than 1 day' : `${days < 10 ? days.toFixed(1) : Math.round(days)} days`;
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
  if (!dateString) return 'Unknown';
  const date = new Date(dateString);
  const diff = now.getTime() - date.getTime();
  const minutes = Math.floor(diff / 60000);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (minutes < 1) return 'Just now';
  if (minutes < 60) return `${minutes} min ago`;
  if (hours < 24) return `${hours} hr ago`;
  if (days < 7) return `${pluralize(days, 'day')} ago`;
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
  if (cycle === 'monthly') return 'Monthly';
  if (cycle === 'yearly') return 'Yearly';
  if (cycle === 'lunar_yearly') return 'Yearly (lunar)';
  return 'Weekly';
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
        if (seconds % 3600 === 0) return `every ${seconds / 3600} hr`;
        if (seconds % 60 === 0) return `every ${seconds / 60} min`;
        return `every ${seconds} s`;
      })()
    : job.schedule.toLowerCase();
  const next = job.next_run ? ` · next in ${formatUntil(job.next_run)}` : '';
  return `Server checks ${cadence}${next}`;
}

export function formatUntil(isoDate: string): string {
  const ms = new Date(isoDate).getTime() - Date.now();
  if (!Number.isFinite(ms) || ms <= 30_000) return 'less than a minute';
  const minutes = Math.round(ms / 60_000);
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  return minutes % 60 === 0 ? `${hours} hr` : `${hours} hr ${minutes % 60} min`;
}

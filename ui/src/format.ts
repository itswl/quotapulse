/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */

import type { BalanceType, Runway } from './api/types.js';

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

/* Implementation note. */
export function formatCurrency(value: unknown): string {
  const num = toNumber(value);
  if (Number.isNaN(num)) return '-';
  return num.toFixed(2);
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

/**
 * Implementation note.
 * Implementation note.
 */
export function formatRunway(runway: Runway | null | undefined): RunwayDisplay {
  if (!runway || runway.confidence === 'none') {
    return { text: 'Accumulating data', level: 'unknown', hint: 'Estimates appear after several hours of balance history' };
  }
  if (!runway.burn_per_day) {
    return { text: 'No spending', level: 'normal', hint: `Balance did not decrease in the last ${runway.window_days} days` };
  }
  const days = runway.runway_days;
  if (days === null || days === undefined) {
    return { text: '—', level: 'unknown', hint: '' };
  }
  const hint = runway.depletion_date
    ? `At an average daily spend of ${formatCurrency(runway.burn_per_day)}, estimated to deplete around ${runway.depletion_date}`
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

/* Implementation note. */
export function formatDate(dateString: string | null | undefined): string {
  if (!dateString) return '-';
  const date = new Date(dateString);
  return date.toLocaleString('en-US', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
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
  if (days < 7) return `${days} days ago`;
  return formatDate(dateString);
}

/* Implementation note. */
export function getBalancePercentage(balance: number, threshold: number): number {
  if (threshold === 0) return 100;
  return (balance / threshold) * 100;
}

/* Implementation note. */
export function getBalanceStatus(balance: number, threshold: number): BalanceStatus {
  const percentage = getBalancePercentage(balance, threshold);
  if (percentage >= 50) return 'normal';
  if (percentage >= 20) return 'warning';
  return 'danger';
}

/* Implementation note. */
export function cycleLabel(cycle: string | null | undefined): string {
  if (cycle === 'monthly') return 'Monthly';
  if (cycle === 'yearly') return 'Yearly';
  if (cycle === 'lunar_yearly') return 'Yearly (lunar)';
  return 'Weekly';
}

/* Implementation note. */
export function renewalUrgency(daysUntilRenewal: number): '' | 'warning' | 'danger' {
  if (daysUntilRenewal <= 7) return 'danger';
  if (daysUntilRenewal <= 14) return 'warning';
  return '';
}

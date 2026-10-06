/* Implementation note. */

import { byId, setText } from '../dom.js';
import { formatCurrency, formatDays, formatRunway, getRelativeTime, needsAttention } from '../format.js';
import { t } from '../i18n/index.js';
import { AppState } from '../state.js';
import type { CheckResult, CreditsResponse, SubscriptionsResponse, SubscriptionResult } from '../api/types.js';
import { sortSubscriptionsByNextDate } from './subscriptions.js';

/* Implementation note. */
export function shortestRunway(projects: CheckResult[]): CheckResult | null {
  const ranked = projects
    .filter((p) => p.success && p.runway && p.runway.runway_days !== null && p.runway.runway_days !== undefined)
    .sort((a, b) => (a.runway?.runway_days ?? 0) - (b.runway?.runway_days ?? 0));
  return ranked[0] ?? null;
}

export function updateStats(data: CreditsResponse): void {
  const projects = data.projects || [];

  updateNavFreshness(data);
  setText('total-projects', String(projects.length));
  setText('normal-projects', String(projects.filter((p) => !p.need_alarm && p.success).length));
  setText('alert-projects', String(projects.filter((p) => p.need_alarm).length));
  updateFailedHint(projects);
  updateRunwayStat(projects);
}

/**
 * Implementation note.
 * Implementation note.
 */
export function updateFailedHint(projects: CheckResult[]): void {
  const label = byId('alert-projects-label');
  if (!label) return;

  const failed = projects.filter((p) => !p.success);
  if (failed.length === 0) {
    label.textContent = t('stats.alerting_projects');
    label.title = '';
    return;
  }
  label.textContent = t('stats.alerting_unavailable', { count: failed.length });
  label.title = t('stats.unavailable_list', { names: failed.map((p) => p.project).join(', ') });
}

/* Why the band has no shortest runway to show. */
export function runwayUnavailableReason(projects: CheckResult[]): string {
  if (projects.length === 0) return t('stats.runway_no_projects');
  if (AppState.features.database === false) return t('stats.runway_no_database');
  if (!projects.some((p) => p.success && p.type !== 'quota')) return t('stats.runway_quota_only');
  return t('stats.runway_collecting');
}

export function updateRunwayStat(projects: CheckResult[]): void {
  const value = byId('shortest-runway');
  const label = byId('shortest-runway-label');
  const hint = byId('shortest-runway-hint');
  if (!value || !label) return;

  const first = shortestRunway(projects);
  if (!first) {
    const reason = runwayUnavailableReason(projects);
    value.textContent = '—';
    value.className = 'stat-value';
    label.textContent = t('stats.shortest_runway');
    label.title = reason;
    if (hint) hint.textContent = reason;
    return;
  }

  const runway = formatRunway(first.runway);
  value.textContent = runway.text;
  value.className = `stat-value runway-${runway.level}`;
  label.textContent = t('stats.shortest_runway_of', { project: first.project });
  label.title = runway.hint;
  // The hero card has room to say why, not just how many days.
  if (hint) {
    hint.textContent =
      runway.hint || t('stats.estimated_from', { period: formatDays(first.runway?.window_days ?? 7) });
  }
}

/* ==================== Subscription overview ====================
   The band's second set of blocks. A subscription counts as "due" while it is not
   already marked renewed and its next renewal falls inside the window. */

/* Average a subscription into a monthly figure. Weekly multiplies out to 52/12;
   lunar-yearly cycles are approximated as one twelfth of the yearly amount. */
export function monthlyCost(subscription: SubscriptionResult): number {
  const amount = Number(subscription.amount) || 0;
  if (subscription.cycle_type === 'weekly') return (amount * 52) / 12;
  if (subscription.cycle_type === 'yearly' || subscription.cycle_type === 'lunar_yearly') return amount / 12;
  return amount;
}

/* data is null when subscriptions are disabled or failed to load; the hint says which. */
export function updateSubscriptionStats(data: SubscriptionsResponse | null, unavailable = false): void {
  const value = byId('sub-due-soon');
  const hint = byId('sub-due-hint');
  if (!value || !hint) return;

  if (!data) {
    value.textContent = '—';
    value.className = 'stat-value';
    hint.textContent = unavailable ? t('stats.subs_unavailable') : t('stats.subs_disabled');
    setText('sub-due-30', '—');
    setText('sub-renewed', '—');
    setText('sub-cost', '—');
    return;
  }

  const subs = data.subscriptions || [];
  const active = subs.filter((s) => !s.already_renewed);
  const due7 = active.filter((s) => s.days_until_renewal <= 7);
  const due30 = active.filter((s) => s.days_until_renewal <= 30);
  const renewed = subs.filter((s) => s.already_renewed).length;
  const cost = subs.reduce((sum, s) => sum + monthlyCost(s), 0);

  value.textContent = String(due7.length);
  value.className = `stat-value${due7.length > 0 ? ' runway-danger' : ''}`;
  const next = sortSubscriptionsByNextDate(active)[0];
  if (next) {
    const params = { name: next.name, date: next.next_renewal_date };
    hint.textContent = due7.length > 0 ? t('stats.next_reminder', params) : t('stats.nothing_due', params);
  } else {
    hint.textContent = t('stats.no_active_subs');
  }

  const due30Value = byId('sub-due-30');
  if (due30Value) {
    due30Value.textContent = String(due30.length);
    due30Value.className = `stat-value${due30.length > 0 ? ' runway-warning' : ''}`;
  }
  setText('sub-renewed', String(renewed));
  setText('sub-cost', formatCurrency(cost));
}

/* ==================== Switcher badges ==================== */

function setBadge(id: string, count: number, tone: 'danger' | 'warning'): void {
  const badge = byId(id);
  if (!badge) return;
  badge.textContent = String(count);
  badge.hidden = count === 0;
  badge.classList.toggle('danger', tone === 'danger');
  badge.classList.toggle('warning', tone === 'warning');
}

/* Live counts: accounts that need attention (below threshold or failing their check) on
   the Alerts only chip, and renewals inside their reminder window on the view switcher.
   A badge hides itself at zero so a healthy system stays quiet. */
export function updateBadges(): void {
  setBadge('alerts-badge', (AppState.balanceData?.projects || []).filter(needsAttention).length, 'danger');
  const subs = AppState.features.subscriptions ? AppState.subscriptionData?.subscriptions || [] : [];
  setBadge('subs-badge', subs.filter((s) => s.need_alert).length, 'warning');
}

/* ==================== Overview orchestration ==================== */

/**
 * Fill the overview band and the switcher badges from AppState, then point the band at
 * the active view. Both sets of blocks are always refreshed (hidden ones included), so
 * switching views never shows stale figures; the CSS decides visibility.
 */
export function refreshOverview(): void {
  const grid = byId('stats-grid');
  if (!grid) return;
  if (AppState.balanceData) updateStats(AppState.balanceData);
  if (AppState.features.subscriptions) {
    // A failed reload keeps showing the last good figures; only a first failure has none.
    updateSubscriptionStats(AppState.subscriptionData, AppState.subscriptionLoadError !== null);
  } else {
    updateSubscriptionStats(null);
  }
  updateBadges();
  grid.dataset['view'] =
    AppState.currentView === 'email'
      ? 'email'
      : AppState.currentView === 'subscriptions' && AppState.features.subscriptions
        ? 'subscriptions'
        : 'projects';
}

/* ==================== Navigation status ====================
   The stamp and the dot in the top bar. Green means the data is fresh; amber that it
   has aged past a typical refresh cycle; red that the load failed or the API has gone
   quiet for a day. The dot used to pulse unconditionally, which lied on both counts. */

const FRESH_MS = 90 * 60 * 1000;
const STALE_MS = 24 * 60 * 60 * 1000;

export function updateNavFreshness(data: CreditsResponse | null, failed = AppState.balanceLoadFailed): void {
  const stamp = byId('last-update');
  const dot = byId('nav-live-dot');
  if (!stamp && !dot) return;

  const last = data?.last_update ?? null;
  const text = last ? getRelativeTime(last) : failed ? '—' : t('common.unknown');
  const age = last ? Date.now() - new Date(last).getTime() : Number.POSITIVE_INFINITY;
  // Every account failing its check is an outage even when the server itself answers.
  const projects = data?.projects ?? [];
  const outage = projects.length > 0 && projects.every((p) => !p.success);
  const state = failed || outage || !last || age > STALE_MS ? ' down' : age > FRESH_MS ? ' stale' : '';

  if (stamp) stamp.textContent = text;
  if (dot) dot.className = `live-dot${state}`;
}

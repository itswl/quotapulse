/* Implementation note. */

import { byId, setText } from '../dom.js';
import { formatRunway, getRelativeTime } from '../format.js';
import type { CheckResult, CreditsResponse } from '../api/types.js';

/* Implementation note. */
export function shortestRunway(projects: CheckResult[]): CheckResult | null {
  const ranked = projects
    .filter((p) => p.success && p.runway && p.runway.runway_days !== null && p.runway.runway_days !== undefined)
    .sort((a, b) => (a.runway?.runway_days ?? 0) - (b.runway?.runway_days ?? 0));
  return ranked[0] ?? null;
}

export function updateStats(data: CreditsResponse): void {
  const projects = data.projects || [];

  setText('total-projects', String(projects.length));
  setText('normal-projects', String(projects.filter((p) => !p.need_alarm && p.success).length));
  setText('alert-projects', String(projects.filter((p) => p.need_alarm).length));
  setText('last-update', getRelativeTime(data.last_update));
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
    label.textContent = 'Alerting projects';
    label.title = '';
    return;
  }
  label.textContent = `Alerting projects · ${failed.length} unavailable`;
  label.title = `Unavailable balances: ${failed.map((p) => p.project).join(', ')}`;
}

export function updateRunwayStat(projects: CheckResult[]): void {
  const value = byId('shortest-runway');
  const label = byId('shortest-runway-label');
  const hint = byId('shortest-runway-hint');
  if (!value || !label) return;

  const first = shortestRunway(projects);
  if (!first) {
    const reason = 'Enable the database and collect balance history to estimate runway';
    value.textContent = '—';
    value.className = 'stat-value';
    label.textContent = 'Shortest runway';
    label.title = reason;
    if (hint) hint.textContent = reason;
    return;
  }

  const runway = formatRunway(first.runway);
  value.textContent = runway.text;
  value.className = `stat-value runway-${runway.level}`;
  label.textContent = `Shortest runway · ${first.project}`;
  label.title = runway.hint;
  // The hero card has room to say why, not just how many days.
  if (hint) {
    hint.textContent =
      runway.hint || `Estimated from the last ${first.runway?.window_days ?? 7} days of balance history`;
  }
}

/* Implementation note. */

import { requireById } from '../dom.js';
import { cycleLabel, escapeAttr, escapeHTML, formatCurrency, formatUntil, renewalUrgency } from '../format.js';
import { AppState } from '../state.js';
import type { SubscriptionConfig, SubscriptionResult, SubscriptionsResponse } from '../api/types.js';
import { emptyState, loadErrorDetail } from './empty.js';
import { ICON_CALENDAR, ICON_CHECK, ICON_DELETE, ICON_EDIT, ICON_UNDO } from './icons.js';

export /**
 * Notification outcome for subscriptions inside their reminder window. One boolean
 * (alert_sent) could not tell "sent" from "suppressed by cooldown" from "failed".
 */
function renderAlertBadge(sub: SubscriptionResult): string {
  if (!sub.need_alert) return '';
  switch (sub.alert_state) {
    case 'sent':
      return '<span class="status-badge success" title="Reminder sent for this cycle">Notified</span>';
    case 'cooldown_skipped': {
      const eta = sub.next_eligible_at ? formatUntil(sub.next_eligible_at) : 'soon';
      return `<span class="status-badge muted" title="Suppressed by the notification cooldown">Cooldown · next in ${escapeHTML(eta)}</span>`;
    }
    case 'failed': {
      const reason = sub.last_error ? ` title="${escapeAttr(sub.last_error)}"` : '';
      return `<span class="status-badge danger"${reason}>Send failed</span>`;
    }
    case 'dry_run':
      return '<span class="status-badge info">Dry run · nothing sent</span>';
    case 'snoozed':
      return `<span class="status-badge muted" title="Reminders snoozed">Snoozed${sub.snoozed_until ? ` until ${escapeHTML(sub.snoozed_until)}` : ''}</span>`;
    default:
      return '';
  }
}

export function renderSubscriptionCard(sub: SubscriptionResult): string {
  const daysClass = renewalUrgency(sub);
  const amount = Number(sub.amount) || 0;
  const ownerProject = sub.owner_project || 'No owner project';
  const subName = sub.name || 'Unknown subscription';
  const subNameAttr = escapeAttr(subName);

  // Implementation note.
  const renewalAction = sub.already_renewed
    ? `
                        <button class="action-icon-btn js-clear-renewed" data-name="${subNameAttr}" title="Clear renewal mark">
                            ${ICON_UNDO}
                        </button>
                        `
    : `
                        <button class="action-icon-btn success js-mark-renewed" data-name="${subNameAttr}" title="Mark renewed">
                            ${ICON_CHECK}
                        </button>
                        `;

  return `
            <div class="subscription-card">
                <div class="subscription-info">
                    <h3>${escapeHTML(subName)}</h3>
                    <div class="subscription-meta">
                        <span class="meta-item project-meta">${escapeHTML(ownerProject)}</span>
                        <span class="meta-item"><span class="k">Amount</span>${formatCurrency(amount)}</span>
                        <span class="meta-item">${cycleLabel(sub.cycle_type)}</span>
                        ${sub.next_renewal_date ? `<span class="meta-item"><span class="k">Next renewal</span>${escapeHTML(sub.next_renewal_date)}</span>` : ''}
                        ${sub.already_renewed ? '<span class="meta-item"><span class="status-badge success">Renewed</span></span>' : ''}
                    </div>
                </div>
                <div class="subscription-status">
                    <div class="subscription-actions">
                        ${renewalAction}
                        <button class="action-icon-btn js-snooze-subscription" data-name="${subNameAttr}" title="Snooze reminders for 7 days">
                            ${ICON_CALENDAR}
                        </button>
                        <button class="action-icon-btn js-edit-subscription" data-name="${subNameAttr}" title="Edit">
                            ${ICON_EDIT}
                        </button>
                        <button class="action-icon-btn danger js-delete-subscription" data-name="${subNameAttr}" title="Delete">
                            ${ICON_DELETE}
                        </button>
                    </div>
                    ${renderAlertBadge(sub)}
                    <div class="days-remaining ${daysClass}">${sub.days_until_renewal}<span class="unit"> days</span></div>
                </div>
            </div>
        `;
}

export function sortSubscriptionsByNextDate(subscriptions: SubscriptionResult[]): SubscriptionResult[] {
  return [...subscriptions].sort((a, b) => {
    const dateOrder = (a.next_renewal_date || '9999-12-31').localeCompare(b.next_renewal_date || '9999-12-31');
    if (dateOrder !== 0) return dateOrder;
    if (a.days_until_renewal !== b.days_until_renewal) {
      return a.days_until_renewal - b.days_until_renewal;
    }
    return a.name.localeCompare(b.name, 'zh-CN');
  });
}

/* A disabled subscription isn't checked, so the status API leaves it out; its card comes
   from the configuration so it can be edited and turned back on. */
export function renderDisabledSubscriptionCard(sub: SubscriptionConfig): string {
  const subNameAttr = escapeAttr(sub.name);
  return `
            <div class="subscription-card disabled">
                <div class="subscription-info">
                    <h3>${escapeHTML(sub.name)}</h3>
                    <div class="subscription-meta">
                        <span class="meta-item project-meta">${escapeHTML(sub.owner_project || 'No owner project')}</span>
                        <span class="meta-item"><span class="k">Amount</span>${formatCurrency(Number(sub.amount) || 0)}</span>
                        <span class="meta-item">${cycleLabel(sub.cycle_type)}</span>
                        <span class="meta-item"><span class="status-badge muted" title="No reminders are sent">Disabled</span></span>
                    </div>
                </div>
                <div class="subscription-status">
                    <div class="subscription-actions">
                        <button class="action-icon-btn js-edit-subscription" data-name="${subNameAttr}" title="Edit">
                            ${ICON_EDIT}
                        </button>
                        <button class="action-icon-btn danger js-delete-subscription" data-name="${subNameAttr}" title="Delete">
                            ${ICON_DELETE}
                        </button>
                    </div>
                </div>
            </div>
        `;
}

/* A failed load says so, with a retry, instead of looking like an empty list. */
export function renderSubscriptionsError(error: unknown): void {
  requireById('subscriptions-container').innerHTML = emptyState(
    'Subscriptions could not be loaded',
    loadErrorDetail(error),
    'error',
    false,
    '<button type="button" class="btn-primary js-retry-load">Try again</button>',
  );
}

export function renderSubscriptions(data: SubscriptionsResponse): void {
  const container = requireById('subscriptions-container');
  const subscriptions = data.subscriptions || [];
  const sortedSubscriptions = sortSubscriptionsByNextDate(subscriptions);

  const disabled = AppState.disabledSubscriptions;

  container.innerHTML =
    subscriptions.length === 0 && disabled.length === 0
      ? emptyState('No subscriptions', 'No subscription reminders yet', 'calendar')
      : [...sortedSubscriptions.map((s) => renderSubscriptionCard(s)), ...disabled.map(renderDisabledSubscriptionCard)].join('');
}

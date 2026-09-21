/* Composed empty, error and getting-started states. */

import { escapeHTML } from '../format.js';
import { ICON_CALENDAR, ICON_CHECK_CIRCLE, ICON_CLOUD_OFF, ICON_INFO_CIRCLE, ICON_MAIL, ICON_SEARCH } from './icons.js';

export type EmptyIcon = 'info' | 'calendar' | 'mail' | 'search' | 'check' | 'error';

const ICONS: Record<EmptyIcon, string> = {
  info: ICON_INFO_CIRCLE,
  calendar: ICON_CALENDAR,
  mail: ICON_MAIL,
  search: ICON_SEARCH,
  check: ICON_CHECK_CIRCLE,
  error: ICON_CLOUD_OFF,
};

/**
 * Render an empty state with an optional call to action.
 * `title` and `text` are escaped; `action` is trusted markup built by the caller
 * (buttons carrying js-* hooks), never user data.
 */
export function emptyState(title: string, text: string, icon: EmptyIcon = 'info', compact = false, action = ''): string {
  const classes = ['empty-state', compact ? 'compact' : '', icon === 'error' ? 'error' : ''].filter(Boolean).join(' ');
  return `
            <div class="${classes}">
                <div class="empty-icon" aria-hidden="true">${ICONS[icon]}</div>
                <h3>${escapeHTML(title)}</h3>
                <p>${escapeHTML(text)}</p>${action ? `\n                <div class="empty-actions">${action}</div>` : ''}
            </div>
        `;
}

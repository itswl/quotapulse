/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */

import type { CreditsResponse, Features, SubscriptionsResponse } from './api/types.js';

/* Implementation note. */
export type ViewName = 'all' | 'alerts' | 'subscriptions' | 'email';

export const VIEW_NAMES: readonly ViewName[] = ['all', 'alerts', 'subscriptions', 'email'];

export type Theme = 'light' | 'dark';
export type ProjectViewStyle = 'grid' | 'list';

export interface AppStateShape {
  currentTheme: Theme;
  currentView: ViewName;
  projectViewStyle: ProjectViewStyle;
  /* Implementation note. */
  currentFilter: string;
  searchQuery: string;
  balanceData: CreditsResponse | null;
  subscriptionData: SubscriptionsResponse | null;
  features: Features;
  lastUpdate: Date | null;
  autoRefreshTimer: ReturnType<typeof setInterval> | null;
}

/* Implementation note. */
export function readStorage(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function writeStorage(key: string, value: string | null): void {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    /* Implementation note. */
  }
}

export const AppState: AppStateShape = {
  currentTheme: readStorage('theme') === 'dark' ? 'dark' : 'light',
  currentView: 'all',
  projectViewStyle: readStorage('projectViewStyle') === 'list' ? 'list' : 'grid',
  currentFilter: 'all',
  searchQuery: '',
  balanceData: null,
  subscriptionData: null,
  // Implementation note.
  features: { subscriptions: false, dynamic_config: false, history: false, email_scan: false },
  lastUpdate: null,
  autoRefreshTimer: null,
};

export function isViewName(value: string): value is ViewName {
  return (VIEW_NAMES as readonly string[]).includes(value);
}

/**
 * Shared application state and persistence helpers.
 *
 * Everything the render modules need to agree on lives here: current view, theme,
 * filters, the latest API payloads and feature flags.
 */

import type { CreditsResponse, Features, SubscriptionsResponse } from './api/types.js';

export type ViewName = 'all' | 'alerts' | 'subscriptions' | 'email';

export const VIEW_NAMES: readonly ViewName[] = ['all', 'alerts', 'subscriptions', 'email'];

export type Theme = 'light' | 'dark';
export type ProjectViewStyle = 'grid' | 'list';

export interface AppStateShape {
  currentTheme: Theme;
  currentView: ViewName;
  projectViewStyle: ProjectViewStyle;
  /* Provider filter value; 'all' disables the filter. */
  currentFilter: string;
  searchQuery: string;
  balanceData: CreditsResponse | null;
  subscriptionData: SubscriptionsResponse | null;
  features: Features;
  lastUpdate: Date | null;
  autoRefreshTimer: ReturnType<typeof setInterval> | null;
}

/* localStorage can throw (private mode, disabled storage); treat it as best effort. */
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
    /* storage unavailable */
  }
}

/* The operating system preference is the default theme; an explicit toggle is stored and wins. */
export function systemTheme(): Theme {
  try {
    if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
      return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }
  } catch {
    /* matchMedia unavailable */
  }
  return 'light';
}

export function initialTheme(): Theme {
  const stored = readStorage('theme');
  if (stored === 'dark' || stored === 'light') return stored;
  return systemTheme();
}

export const AppState: AppStateShape = {
  currentTheme: initialTheme(),
  currentView: 'all',
  projectViewStyle: readStorage('projectViewStyle') === 'list' ? 'list' : 'grid',
  currentFilter: 'all',
  searchQuery: '',
  balanceData: null,
  subscriptionData: null,
  // Conservative defaults until /api/features answers: optional views stay hidden.
  features: { subscriptions: false, dynamic_config: false, history: false, email_scan: false },
  lastUpdate: null,
  autoRefreshTimer: null,
};

export function isViewName(value: string): value is ViewName {
  return (VIEW_NAMES as readonly string[]).includes(value);
}

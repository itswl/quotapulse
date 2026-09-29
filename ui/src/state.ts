/**
 * Shared application state and persistence helpers.
 *
 * Everything the render modules need to agree on lives here: current view, theme,
 * filters, the latest API payloads and feature flags.
 */

import type { CreditsResponse, Features, SubscriptionsResponse } from './api/types.js';

export type ViewName = 'all' | 'subscriptions' | 'email';

export const VIEW_NAMES: readonly ViewName[] = ['all', 'subscriptions', 'email'];

export type Theme = 'light' | 'dark';
export type ProjectViewStyle = 'grid' | 'list';

export const DEFAULT_REFRESH_MINUTES = 5;
export const MIN_REFRESH_MINUTES = 1;
export const MAX_REFRESH_MINUTES = 1440;

/* Parse a user-supplied interval in minutes, rounded and clamped to sane bounds. */
export function parseRefreshMinutes(value: unknown): number {
  const parsed = Math.round(Number(value));
  if (!Number.isFinite(parsed)) return DEFAULT_REFRESH_MINUTES;
  return Math.min(MAX_REFRESH_MINUTES, Math.max(MIN_REFRESH_MINUTES, parsed));
}

function readRefreshMinutes(): number {
  const stored = readStorage('autoRefreshMinutes');
  return stored === null ? DEFAULT_REFRESH_MINUTES : parseRefreshMinutes(stored);
}

export interface AppStateShape {
  currentTheme: Theme;
  currentView: ViewName;
  /* Projects toolbar filter: show only accounts below their alert threshold or failing their check. */
  alertsOnly: boolean;
  projectViewStyle: ProjectViewStyle;
  /* Provider filter value; 'all' disables the filter. */
  currentFilter: string;
  searchQuery: string;
  balanceData: CreditsResponse | null;
  subscriptionData: SubscriptionsResponse | null;
  /* The latest balance load failed; the nav dot stays red until one succeeds. */
  balanceLoadFailed: boolean;
  /* Why the latest subscription load failed, or null. Tells "could not load" apart from "disabled". */
  subscriptionLoadError: unknown;
  features: Features;
  lastUpdate: Date | null;
  autoRefreshTimer: ReturnType<typeof setInterval> | null;
  /* Client polling interval in minutes; the server job cadence is separate. */
  autoRefreshMinutes: number;
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
  alertsOnly: false,
  projectViewStyle: readStorage('projectViewStyle') === 'list' ? 'list' : 'grid',
  currentFilter: 'all',
  searchQuery: '',
  balanceData: null,
  subscriptionData: null,
  balanceLoadFailed: false,
  subscriptionLoadError: null,
  // Conservative defaults until /api/features answers: optional views stay hidden.
  features: { subscriptions: false, dynamic_config: false, history: false, email_scan: false },
  lastUpdate: null,
  autoRefreshTimer: null,
  autoRefreshMinutes: readRefreshMinutes(),
};

export function isViewName(value: string): value is ViewName {
  return (VIEW_NAMES as readonly string[]).includes(value);
}

// The map from addresses to pages.
//
// Three places to go, and two that are reached from them:
//
//   /                     now: what asks for you, and what is ahead
//   /meters[/:id]         watching: everything watched; :id opens a meter
//   /dates                watching, the dates
//   /spend[/…]            money
//   /activity[/…]         history: what happened, and what was sent
//   /settings[/:section]  settings; params.rest for anything deeper
//
// The addresses are the ones notifications have always linked to
// (/meters/:id, /meters#account, /dates, /settings/channels), so a link in
// an old message still lands on the right thing.
import type { ComponentType } from 'preact';
import ActivityPage from './pages/activity';
import NowPage from './pages/now';
import SettingsPage from './pages/settings';
import SpendPage from './pages/spend';
import WatchingPage from './pages/watching';
import { matchPath } from './router';

/** What every page component receives. */
export interface PageProps {
  /** The named parts of the address: { id }, { section }, { rest }. */
  params: Record<string, string>;
}

export type Area = 'now' | 'watching' | 'money' | 'history' | 'settings';

interface RouteDef {
  pattern: string;
  area: Area;
  page: ComponentType<PageProps>;
}

const routes: RouteDef[] = [
  { pattern: '/', area: 'now', page: NowPage },
  { pattern: '/meters', area: 'watching', page: WatchingPage },
  { pattern: '/meters/:id', area: 'watching', page: WatchingPage },
  { pattern: '/dates', area: 'watching', page: WatchingPage },
  { pattern: '/dates/*', area: 'watching', page: WatchingPage },
  { pattern: '/spend', area: 'money', page: SpendPage },
  { pattern: '/spend/*', area: 'money', page: SpendPage },
  { pattern: '/activity', area: 'history', page: ActivityPage },
  { pattern: '/activity/*', area: 'history', page: ActivityPage },
  { pattern: '/settings', area: 'settings', page: SettingsPage },
  { pattern: '/settings/:section', area: 'settings', page: SettingsPage },
  { pattern: '/settings/:section/*', area: 'settings', page: SettingsPage },
];

export interface Resolved {
  area: Area;
  page: ComponentType<PageProps>;
  params: Record<string, string>;
}

/** Finds the page for a path; null when there is none. */
export function resolve(path: string): Resolved | null {
  for (const def of routes) {
    const params = matchPath(def.pattern, path);
    if (params) return { area: def.area, page: def.page, params };
  }
  return null;
}

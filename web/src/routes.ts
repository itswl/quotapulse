// The map from addresses to pages. Every area of the product is already
// here; adding a page inside an area never means editing this file — the
// area's own index.tsx reads `params` and decides what to show.
//
//   /                    overview
//   /meters              the list            /meters/:id      one meter
//   /dates[/…]           params.rest holds anything after /dates
//   /spend[/…]           likewise
//   /activity[/…]        likewise
//   /settings            params.section is undefined
//   /settings/:section   params.section, and params.rest for anything deeper
import type { ComponentType } from 'preact';
import ActivityPage from './pages/activity';
import DatesPage from './pages/dates';
import MeterPage from './pages/meters/meter';
import MetersPage from './pages/meters';
import OverviewPage from './pages/overview';
import SettingsPage from './pages/settings';
import SpendPage from './pages/spend';
import { matchPath } from './router';

/** What every page component receives. */
export interface PageProps {
  /** The named parts of the address: { id }, { section }, { rest }. */
  params: Record<string, string>;
}

export type Area = 'overview' | 'meters' | 'dates' | 'spend' | 'activity' | 'settings';

interface RouteDef {
  pattern: string;
  area: Area;
  page: ComponentType<PageProps>;
}

const routes: RouteDef[] = [
  { pattern: '/', area: 'overview', page: OverviewPage },
  { pattern: '/meters', area: 'meters', page: MetersPage },
  { pattern: '/meters/:id', area: 'meters', page: MeterPage },
  { pattern: '/dates', area: 'dates', page: DatesPage },
  { pattern: '/dates/*', area: 'dates', page: DatesPage },
  { pattern: '/spend', area: 'spend', page: SpendPage },
  { pattern: '/spend/*', area: 'spend', page: SpendPage },
  { pattern: '/activity', area: 'activity', page: ActivityPage },
  { pattern: '/activity/*', area: 'activity', page: ActivityPage },
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

// A history router small enough to read in one sitting.
//
//   route.value          where we are: { path, segments, query }
//   navigate('/meters')  go somewhere (push), or { replace: true }
//   <Link href="/meters">  an ordinary <a> that navigates without a reload
//   matchPath('/meters/:id', route.value.path)   → { id } or null
//
// The server answers every non-file path with index.html, so any address
// here can be opened directly or reloaded.
import { signal } from '@preact/signals';
import { createElement, type AnchorHTMLAttributes, type ComponentChildren, type TargetedMouseEvent } from 'preact';

export interface Route {
  /** The path without a trailing slash; "/" for the root. */
  path: string;
  /** The path's parts: "/meters/abc" is ["meters", "abc"]. */
  segments: string[];
  query: URLSearchParams;
}

function read(): Route {
  const raw = typeof location === 'undefined' ? '/' : location.pathname;
  const segments = raw.split('/').filter(Boolean).map(safeDecode);
  return {
    path: '/' + segments.map(encodeURIComponent).join('/'),
    segments,
    query: new URLSearchParams(typeof location === 'undefined' ? '' : location.search),
  };
}

function safeDecode(part: string): string {
  try {
    return decodeURIComponent(part);
  } catch {
    return part;
  }
}

/** The current location. Reading it in a component subscribes the component. */
export const route = signal<Route>(read());

export interface NavigateOptions {
  /** Replace the current history entry instead of adding one: for filters
   *  and tabs, which should not pile up behind the back button. */
  replace?: boolean;
  /** Leave the scroll position alone. Implied by replace. */
  keepScroll?: boolean;
  /** Hand the old page over to the new one this way even though the entry
   *  is replaced: going up from a page opened straight from a link. */
  direction?: Direction;
}

/** Which way a change of page goes: deeper (a meter from the list), back
 *  up, or across (one tab to another). The page transition follows it. */
export type Direction = 'forward' | 'back' | 'swap';

function depth(path: string): number {
  return path.split('/').filter(Boolean).length;
}

export function directionOf(from: string, to: string): Direction {
  const a = depth(from);
  const b = depth(to);
  return b > a ? 'forward' : b < a ? 'back' : 'swap';
}

/** Preact renders on the next microtask: after this, the page is the new one. */
const rendered = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

/**
 * Changes what is on screen. Where the browser has view transitions and the
 * user has not asked for less motion, the old page hands over to the new one
 * (styles/shell.css draws it, by direction); elsewhere it simply changes.
 * `after` runs once the new page is rendered.
 */
function commit(direction: Direction | null, update: () => void, after?: () => void): void {
  const run = async () => {
    update();
    await rendered();
    after?.();
  };
  const doc = document as Document & { startViewTransition?: (update: () => Promise<void>) => unknown };
  const still = typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
  if (!direction || !doc.startViewTransition || still || document.visibilityState !== 'visible') {
    void run();
    return;
  }
  document.documentElement.dataset.nav = direction;
  doc.startViewTransition(run);
}

/** The state each history entry carries: where the page was scrolled to, so
 *  going back returns there; how many steps into the app it is; and the page
 *  it was opened from. */
interface EntryState {
  y?: number;
  depth?: number;
  from?: string;
}

function entryState(): EntryState {
  const state: unknown = typeof history === 'undefined' ? null : history.state;
  return state && typeof state === 'object' ? (state as EntryState) : {};
}

/** The page the current one was opened from inside the app, or null when it
 *  was opened straight from a link — a notification, a bookmark — and going
 *  back would leave the app. */
export function openedFrom(): string | null {
  const state = entryState();
  return state.depth && state.depth > 0 && state.from ? state.from : null;
}

/** Goes back to where the page was opened from; for a page opened straight
 *  from a link, up to `parent` instead of out of the app. */
export function goBack(parent: string): void {
  if (openedFrom() !== null) history.back();
  else navigate(parent, { replace: true, direction: 'back' });
}

export function navigate(to: string, options: NavigateOptions = {}): void {
  const target = new URL(to, location.href);
  if (target.origin !== location.origin) {
    location.assign(target.href);
    return;
  }
  const same = target.pathname === location.pathname && target.search === location.search;
  if (options.replace || same) {
    history.replaceState(entryState(), '', target.href);
    const next = read();
    commit(
      options.direction ?? null,
      () => {
        route.value = next;
      },
      options.direction ? () => scrollTo(0, 0) : undefined,
    );
    return;
  }
  // Remember where this page was, for the way back.
  const here = entryState();
  history.replaceState({ ...here, y: scrollY }, '');
  const from = route.peek().path;
  history.pushState({ depth: (here.depth ?? 0) + 1, from: location.pathname + location.search }, '', target.href);
  const next = read();
  commit(
    // The same page with another query (a sheet over the list) is not a
    // change of page: nothing to hand over.
    next.path === from ? null : directionOf(from, next.path),
    () => {
      route.value = next;
    },
    () => {
      if (options.keepScroll) return;
      scrollTo(0, 0);
      // Tell assistive technology the page changed: move focus to the new
      // page's top, the way a full page load would.
      document.getElementById('main')?.focus({ preventScroll: true });
    },
  );
}

/** Sets or clears query parameters on the current page, replacing history. */
export function setQuery(changes: Record<string, string | null | undefined>): void {
  const query = new URLSearchParams(location.search);
  for (const [key, value] of Object.entries(changes)) {
    if (value === null || value === undefined || value === '') query.delete(key);
    else query.set(key, value);
  }
  const tail = query.toString();
  navigate(location.pathname + (tail ? '?' + tail : ''), { replace: true });
}

if (typeof window !== 'undefined') {
  // The way back restores the scroll position itself (EntryState), after the
  // page it returns to has rendered.
  history.scrollRestoration = 'manual';
  addEventListener('popstate', () => {
    const from = route.peek().path;
    const next = read();
    const y = entryState().y ?? 0;
    commit(
      next.path === from ? null : directionOf(from, next.path),
      () => {
        route.value = next;
      },
      () => restoreScroll(y),
    );
  });
}

/** Scrolls back to where a page was left. The page may not be as tall yet
 *  as it was then — a list still filling in — so it tries again for a few
 *  frames until it gets there. */
function restoreScroll(y: number, frames = 12): void {
  scrollTo(0, y);
  if (frames > 0 && Math.abs(scrollY - y) > 1) requestAnimationFrame(() => restoreScroll(y, frames - 1));
}

/**
 * Matches a path against a pattern. `:name` captures one segment; a trailing
 * `*` accepts anything after it, captured as `rest`.
 *
 *   matchPath('/meters/:id', '/meters/abc')        → { id: 'abc' }
 *   matchPath('/settings/*', '/settings/a/b')      → { rest: 'a/b' }
 */
export function matchPath(pattern: string, path: string): Record<string, string> | null {
  const want = pattern.split('/').filter(Boolean);
  const have = path.split('/').filter(Boolean).map(safeDecode);
  const params: Record<string, string> = {};
  for (let i = 0; i < want.length; i++) {
    const part = want[i]!;
    if (part === '*') {
      params.rest = have.slice(i).join('/');
      return params;
    }
    const value = have[i];
    if (value === undefined) return null;
    if (part.startsWith(':')) params[part.slice(1)] = value;
    else if (part !== value) return null;
  }
  return have.length === want.length ? params : null;
}

export interface LinkProps extends Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'href'> {
  href: string;
  /** Replace the history entry instead of pushing one. */
  replace?: boolean;
  children?: ComponentChildren;
}

/** An <a> that navigates in place. Modified clicks, new tabs and other
 *  origins are left to the browser. */
export function Link({ href, replace, onClick, children, ...rest }: LinkProps) {
  const handle = (event: TargetedMouseEvent<HTMLAnchorElement>) => {
    if (typeof onClick === 'function') onClick(event);
    if (event.defaultPrevented || event.button !== 0) return;
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    const anchor = event.currentTarget;
    if (anchor.target && anchor.target !== '_self') return;
    if (anchor.origin !== location.origin || anchor.hasAttribute('download')) return;
    event.preventDefault();
    navigate(href, replace ? { replace: true } : {});
  };
  return createElement('a', { ...rest, href, onClick: handle }, children);
}

/** True when `path` is the page at `href` or one beneath it. "/" only matches
 *  itself. */
export function isActive(href: string, path: string): boolean {
  if (href === '/') return path === '/';
  return path === href || path.startsWith(href + '/');
}

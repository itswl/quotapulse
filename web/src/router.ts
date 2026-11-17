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
import { createElement, type ComponentChildren, type JSX } from 'preact';

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
}

export function navigate(to: string, options: NavigateOptions = {}): void {
  const target = new URL(to, location.href);
  if (target.origin !== location.origin) {
    location.assign(target.href);
    return;
  }
  const same = target.pathname === location.pathname && target.search === location.search;
  if (options.replace || same) history.replaceState(null, '', target.href);
  else history.pushState(null, '', target.href);
  route.value = read();
  if (!options.replace && !options.keepScroll && !same) {
    scrollTo(0, 0);
    // Tell assistive technology the page changed: move focus to the new
    // page's top, the way a full page load would.
    requestAnimationFrame(() => document.getElementById('main')?.focus({ preventScroll: true }));
  }
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
  addEventListener('popstate', () => {
    route.value = read();
  });
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

export interface LinkProps extends Omit<JSX.AnchorHTMLAttributes<HTMLAnchorElement>, 'href'> {
  href: string;
  /** Replace the history entry instead of pushing one. */
  replace?: boolean;
  children?: ComponentChildren;
}

/** An <a> that navigates in place. Modified clicks, new tabs and other
 *  origins are left to the browser. */
export function Link({ href, replace, onClick, children, ...rest }: LinkProps) {
  const handle = (event: JSX.TargetedMouseEvent<HTMLAnchorElement>) => {
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

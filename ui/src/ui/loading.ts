/**
 * Full-screen loading overlay.
 *
 * Counted, not boolean: mailbox scanning nests setLoading(true) within one
 * operation, and a boolean would let the inner call drop the overlay while
 * the outer one is still running.
 */

import { byId } from '../dom.js';

let depth = 0;

export function setLoading(show: boolean): void {
  depth = show ? depth + 1 : Math.max(0, depth - 1);
  const overlay = byId('loading-overlay');
  if (!overlay) return;
  overlay.classList.toggle('active', depth > 0);
}

/** Failure fallback: force the overlay away so the page never sticks in loading */
export function resetLoading(): void {
  depth = 0;
  byId('loading-overlay')?.classList.remove('active');
}

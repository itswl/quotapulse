// The server's idea of "now" and of the time zone.
//
// Every date and duration on screen is worked out from these two, never from
// the browser's own clock or zone: the user configured a zone on the server,
// and a demo may run on a pinned clock. The overview carries both, and the
// overview store keeps this module in step; between fetches the clock ticks
// on from the last known server time.
import { computed, signal } from '@preact/signals';
import type { Instant } from '../api/types';

const TICK = 30_000;

/** Server time minus browser time, in milliseconds. */
const offset = signal(0);
const browserNow = signal(Date.now());

/** The current moment by the server's clock, in epoch milliseconds. It moves
 *  every half minute, so anything derived from it stays fresh on its own. */
export const now = computed(() => browserNow.value + offset.value);

/** The user's configured IANA zone. Until the server has said, the browser's. */
export const timezone = signal(browserZone());

/** Takes the server's `now` from a response and re-anchors the clock on it. */
export function syncClock(serverNow: Instant): void {
  const at = Date.parse(serverNow);
  if (Number.isNaN(at)) return;
  const local = Date.now();
  browserNow.value = local;
  offset.value = at - local;
}

/** Sets the zone, ignoring a name this browser does not know. */
export function setTimezone(zone: string): void {
  if (zone === timezone.peek() || !validZone(zone)) return;
  timezone.value = zone;
}

function browserZone(): string {
  try {
    return new Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

function validZone(zone: string): boolean {
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: zone });
    return true;
  } catch {
    return false;
  }
}

if (typeof window !== 'undefined') {
  const tick = () => {
    browserNow.value = Date.now();
  };
  setInterval(() => {
    if (document.visibilityState === 'visible') tick();
  }, TICK);
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') tick();
  });
}

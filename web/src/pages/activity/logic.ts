// The parts of the Activity page that have nothing to do with drawing: how
// the two lists page backwards, how a list falls into days, what state a
// notification is in. Pure functions, tested in test/activity.test.ts.
import type { DeliveryQuery, IncidentQuery } from '../../api/endpoints';
import type { Day, Delivery, IncidentState, IncidentView, Instant } from '../../api/types';
import { dayOf } from '../../format';

// ---------- paging ----------

export interface Page<T> {
  items: T[];
  /** There is more beyond these. */
  more: boolean;
}

/** Fetches up to `limit` items that come after the ones already shown — the
 *  newest ones when nothing is shown yet. */
export type FetchOlder<T> = (shown: readonly T[], limit: number) => Promise<Page<T>>;

/** The most that can be asked for at once: the server answers with at most
 *  500, and treats a larger limit as 100. */
const MAX_LIMIT = 500;

/** Pages back through the notification log by id, which is unique: "before
 *  the last one shown" misses nothing. */
export function deliveryPager(list: (query: DeliveryQuery) => Promise<Delivery[]>): FetchOlder<Delivery> {
  return async (shown, limit) => {
    const last = shown[shown.length - 1];
    // One more than wanted: whether it comes back says whether there is a next page.
    const page = await list({ limit: Math.min(MAX_LIMIT, limit + 1), ...(last ? { before: last.id } : {}) });
    return { items: page.slice(0, limit), more: page.length > limit };
  };
}

/**
 * Pages back through incidents. The API's cursor is `before`, an instant it
 * compares with opened_at alone — and several incidents can open in the same
 * second. "Before the last one shown" would skip those of its second that did
 * not fit on the page, so this asks from one second later and leaves out what
 * is already shown.
 */
export function incidentPager(list: (query: IncidentQuery) => Promise<IncidentView[]>, state?: IncidentState): FetchOlder<IncidentView> {
  const filter: IncidentQuery = state ? { state } : {};
  return async (shown, limit) => {
    const last = shown[shown.length - 1];
    if (!last) {
      const page = await list({ ...filter, limit: Math.min(MAX_LIMIT, limit + 1) });
      return { items: page.slice(0, limit), more: page.length > limit };
    }
    // The ones shown that share the last one's second come back again.
    let overlap = 0;
    for (let i = shown.length - 1; i >= 0 && shown[i]!.opened_at === last.opened_at; i--) overlap += 1;
    const asked = Math.min(MAX_LIMIT, limit + 1 + overlap);
    const before = new Date(Date.parse(last.opened_at) + 1000).toISOString();
    const page = await list({ ...filter, limit: asked, before });
    const have = new Set(shown.map((each) => each.id));
    const fresh = page.filter((each) => !have.has(each.id));
    return { items: fresh.slice(0, limit), more: fresh.length > limit || (page.length >= asked && fresh.length > 0) };
  };
}

/** Loads the first `count` items of a list, a chunk at a time: what a
 *  refresh does when several pages are on screen. */
export async function loadWindow<T>(fetchOlder: FetchOlder<T>, count: number, chunk = 200): Promise<Page<T>> {
  let items: T[] = [];
  let more = true;
  while (more && items.length < count) {
    const page = await fetchOlder(items, Math.min(chunk, count - items.length));
    if (page.items.length === 0) return { items, more: false };
    items = items.concat(page.items);
    more = page.more;
  }
  return { items, more };
}

// ---------- days ----------

/**
 * The events as one feed, newest first: the open ones among the resolved,
 * each where it opened. Resolved events are loaded a page at a time going
 * back; while there are more to load, an open event older than every one
 * loaded so far waits with them, so the feed never jumps over a stretch of
 * time it has not loaded yet.
 */
export function eventFeed(open: readonly IncidentView[], resolved: readonly IncidentView[], more: boolean): IncidentView[] {
  const oldest = resolved.reduce((least, each) => Math.min(least, Date.parse(each.opened_at)), Infinity);
  const shown = more ? open.filter((each) => Date.parse(each.opened_at) >= oldest) : open;
  return [...shown, ...resolved].sort((a, b) => Date.parse(b.opened_at) - Date.parse(a.opened_at) || a.id.localeCompare(b.id));
}

export interface DayGroup<T> {
  day: Day;
  items: T[];
}

/** Splits a list that is already in order into runs that share a calendar
 *  day in the zone. */
export function groupByDay<T>(items: readonly T[], at: (item: T) => Instant, zone: string): DayGroup<T>[] {
  const groups: DayGroup<T>[] = [];
  for (const item of items) {
    const day = dayOf(at(item), zone);
    const last = groups[groups.length - 1];
    if (last && last.day === day) last.items.push(item);
    else groups.push({ day, items: [item] });
  }
  return groups;
}

// ---------- notifications ----------

/** What became of a notification. The API has four statuses; "pending"
 *  hides three different situations a reader wants told apart. */
export type DeliveryState = 'sent' | 'failed' | 'dropped' | 'held' | 'retrying' | 'queued';

/** The clock on screen moves in half-minute steps, so a notification due
 *  within that is not being held back: it is simply about to go. */
const HELD_MARGIN = 45_000;

export function deliveryState(delivery: Pick<Delivery, 'status' | 'attempts' | 'not_before'>, nowMs: number): DeliveryState {
  if (delivery.status !== 'pending') return delivery.status;
  // An attempt was made and failed; not_before is when the next one is due.
  if (delivery.attempts > 0) return 'retrying';
  // Not tried yet, and not to be tried for a while: quiet hours.
  return Date.parse(delivery.not_before) > nowMs + HELD_MARGIN ? 'held' : 'queued';
}

export type DropReason = 'muted' | 'resolved' | 'removed' | 'channelDeleted' | 'channelOff';

// The server's reasons for not sending (internal/engine/incidents.go,
// dispatch.go; internal/store/channels.go). They arrive in last_error as
// English sentences, not as codes.
const DROP_REASONS = new Map<string, DropReason>([
  ['muted: recorded, not sent', 'muted'],
  ['resolved before it was sent', 'resolved'],
  ['the subject was removed', 'removed'],
  ['the channel was deleted', 'channelDeleted'],
  ['the channel is gone or disabled', 'channelOff'],
]);

/** Recognises why a notification was not sent. Undefined for anything else —
 *  a browser that stopped listening, say — which is shown as it came. */
export function dropReason(lastError: string): DropReason | undefined {
  return DROP_REASONS.get(lastError);
}

export interface Run {
  text: string;
  bold: boolean;
}

/** Splits a line at its **bold** markers: the one piece of Markdown the
 *  server writes into a message (the digest's lines). */
export function boldRuns(line: string): Run[] {
  const parts = line.split('**');
  // A marker without its partner is not a marker: the line is shown as it is.
  if (parts.length % 2 === 0) return [{ text: line, bold: false }];
  const runs: Run[] = [];
  parts.forEach((text, index) => {
    if (text !== '') runs.push({ text, bold: index % 2 === 1 });
  });
  return runs;
}

/** True for a path inside the dashboard, which is the only kind a message's
 *  `path` is followed to. */
export function isLocalPath(path: string | undefined): path is string {
  return typeof path === 'string' && path.startsWith('/') && !path.startsWith('//');
}

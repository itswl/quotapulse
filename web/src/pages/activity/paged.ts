// A list that pages backwards — the newest first, older ones on request —
// and stays current while it is on screen.
//
//   const log = usePaged('deliveries', deliveryPager(listDeliveries), ['activity']);
//   log.items          undefined until the first answer
//   log.more           there are older ones: show "Load more"
//   log.loadMore()     fetches the next page and appends it
//
// A live change, or a change of language, fetches again everything that is
// shown, however many pages that is. Fetches run one at a time, in the order
// they were asked for, so a refresh and a "load more" cannot cross.
//
// What a list shows outlasts the page (state/kept.ts): coming back to it
// shows as much of it at once as there was, while it is fetched again. The
// key is app-wide: name the list.
import { useEffect, useRef, useState } from 'preact/hooks';
import { ApiError } from '../../api/client';
import { lang } from '../../i18n';
import { keep, recall } from '../../state/kept';
import { onChange, type LiveTopic } from '../../state/live';
import { loadWindow, type FetchOlder } from './logic';

/** How many a page holds. */
export const PAGE = 50;

export interface Paged<T> {
  /** What is loaded so far; undefined before the first answer. */
  items: T[] | undefined;
  /** Why the list could not be loaded or refreshed. */
  error: ApiError | undefined;
  /** There are older items to load. */
  more: boolean;
  loadingMore: boolean;
  /** Why the last "load more" failed; cleared by the next try. */
  moreError: ApiError | undefined;
  loadMore: () => void;
  reload: () => void;
  /** Swaps one item for a newer version of itself, as after a write that
   *  returned it. */
  replace: (same: (item: T) => boolean, next: T) => void;
}

interface State<T> {
  key: string | null;
  items: T[] | undefined;
  error: ApiError | undefined;
  more: boolean;
  loadingMore: boolean;
  moreError: ApiError | undefined;
}

interface Control<T> {
  refresh(): void;
  more(): void;
  replace(same: (item: T) => boolean, next: T): void;
}

/** What is kept of a list for coming back to it. */
interface Kept<T> {
  items: T[];
  more: boolean;
}

/** Where a list starts: what it showed when it was last on screen, if it was. */
function seeded<T>(key: string): State<T> {
  const last = recall<Kept<T>>(key);
  return { key: last ? key : null, items: last?.items, error: undefined, more: last?.more ?? false, loadingMore: false, moreError: undefined };
}

const toError = (cause: unknown) => (cause instanceof ApiError ? cause : new ApiError(0, 'unknown', String(cause)));

export function usePaged<T>(key: string, fetchOlder: FetchOlder<T>, topics: readonly LiveTopic[]): Paged<T> {
  const [state, setState] = useState<State<T>>(() => seeded<T>(key));
  const latest = useRef(fetchOlder);
  latest.current = fetchOlder;
  const control = useRef<Control<T> | null>(null);
  // How much of this list has been loaded, so that a change of language
  // brings back all of it rather than the first page.
  const depth = useRef({ key, count: 0 });
  const language = lang.value;

  useEffect(() => {
    let alive = true;
    // As much as was shown last time is fetched again, and "load more" goes
    // on from there.
    const last = recall<Kept<T>>(key);
    let items: T[] | undefined = last?.items;
    let older = last?.more ?? false;
    let refreshWaiting = false;
    let queue: Promise<void> = Promise.resolve();
    if (depth.current.key !== key) depth.current = { key, count: 0 };

    const enqueue = (job: () => Promise<void>) => {
      queue = queue.then(() => (alive ? job() : undefined)).catch(() => undefined);
    };

    const refresh = () => {
      if (refreshWaiting) return; // one in the queue is enough
      refreshWaiting = true;
      enqueue(async () => {
        refreshWaiting = false;
        try {
          const next = await loadWindow<T>((shown, limit) => latest.current(shown, limit), Math.max(PAGE, items?.length ?? depth.current.count));
          if (!alive) return;
          items = next.items;
          older = next.more;
          depth.current.count = next.items.length;
          keep(key, { items, more: older });
          setState((was) => ({ ...was, key, items: next.items, error: undefined, more: next.more, moreError: undefined }));
        } catch (cause) {
          if (!alive) return;
          // Keep what is on screen if it is this list's; another list's
          // would be shown under the wrong heading.
          setState((was) => ({ ...was, key, items: was.key === key ? was.items : undefined, more: was.key === key && was.more, error: toError(cause) }));
        }
      });
    };

    const more = () => {
      enqueue(async () => {
        const shown = items;
        if (!shown) return;
        setState((was) => ({ ...was, loadingMore: true, moreError: undefined }));
        try {
          const page = await latest.current(shown, PAGE);
          if (!alive) return;
          const next = shown.concat(page.items);
          items = next;
          older = page.more && page.items.length > 0;
          depth.current.count = next.length;
          keep(key, { items, more: older });
          setState((was) => ({ ...was, items: next, more: older, loadingMore: false }));
        } catch (cause) {
          if (alive) setState((was) => ({ ...was, loadingMore: false, moreError: toError(cause) }));
        }
      });
    };

    const replace = (same: (item: T) => boolean, next: T) => {
      if (!items) return;
      const swapped = items.map((each) => (same(each) ? next : each));
      items = swapped;
      keep(key, { items, more: older });
      setState((was) => ({ ...was, items: swapped }));
    };

    control.current = { refresh, more, replace };
    refresh();
    return () => {
      alive = false;
      if (control.current?.refresh === refresh) control.current = null;
    };
  }, [key, language]);

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const stop = onChange((changed) => {
      if (!topics.some((topic) => changed.has(topic))) return;
      clearTimeout(timer);
      timer = setTimeout(() => control.current?.refresh(), 150);
    });
    return () => {
      clearTimeout(timer);
      stop();
    };
  }, [topics.join(',')]);

  const shown = state.key === key ? state : seeded<T>(key);
  const mine = shown.key === key;
  return {
    items: mine ? shown.items : undefined,
    error: mine ? shown.error : undefined,
    more: mine && shown.more,
    loadingMore: mine && shown.loadingMore,
    moreError: mine ? shown.moreError : undefined,
    loadMore: () => control.current?.more(),
    reload: () => control.current?.refresh(),
    replace: (same, next) => control.current?.replace(same, next),
  };
}

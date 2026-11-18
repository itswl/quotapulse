// Small hooks every page can use.
import { useEffect, useRef, useState } from 'preact/hooks';
import { ApiError } from './api/client';
import { lang } from './i18n';
import { onChange, type LiveTopic } from './state/live';

/** Names the browser tab after the page: "Meters · QuotaPulse". */
export function useTitle(title: string | undefined): void {
  useEffect(() => {
    document.title = title ? `${title} · QuotaPulse` : 'QuotaPulse';
  }, [title]);
}

export interface Query<T> {
  /** The last answer. While a new key loads, this is still the old key's
   *  answer — see `settled` — so the page can keep its frame. */
  data: T | undefined;
  error: ApiError | undefined;
  /** A fetch is in flight. */
  loading: boolean;
  /** True when `data` is the answer for the current key. */
  settled: boolean;
  reload: () => void;
  /** Replaces the data without fetching, as after a write that returned it. */
  set: (next: T) => void;
}

/**
 * Fetches something that depends on a key — one meter's series, one month's
 * spend — and keeps it current: it fetches again when the key changes, when
 * one of the live topics changes, and when the language does.
 *
 * For data with no key, shared between pages, use a resource (state/resource.ts).
 */
export function useQuery<T>(key: string, fetcher: (signal: AbortSignal) => Promise<T>, topics: readonly LiveTopic[]): Query<T> {
  const [state, setState] = useState<{ data: T | undefined; error: ApiError | undefined; loading: boolean; key: string | null }>({
    data: undefined,
    error: undefined,
    loading: true,
    key: null,
  });
  const [tick, setTick] = useState(0);
  const latest = useRef(fetcher);
  latest.current = fetcher;
  const language = lang.value;

  useEffect(() => {
    const abort = new AbortController();
    setState((was) => ({ ...was, loading: true }));
    latest
      .current(abort.signal)
      .then((data) => {
        if (!abort.signal.aborted) setState({ data, error: undefined, loading: false, key });
      })
      .catch((cause: unknown) => {
        if (abort.signal.aborted) return;
        const error = cause instanceof ApiError ? cause : new ApiError(0, 'unknown', String(cause));
        // An answer for another key would be shown under the wrong heading.
        setState((was) => ({ data: was.key === key ? was.data : undefined, error, loading: false, key }));
      });
    return () => abort.abort();
  }, [key, tick, language]);

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const stop = onChange((changed) => {
      if (!topics.some((topic) => changed.has(topic))) return;
      clearTimeout(timer);
      timer = setTimeout(() => setTick((n) => n + 1), 150);
    });
    return () => {
      clearTimeout(timer);
      stop();
    };
  }, [topics.join(',')]);

  return {
    data: state.data,
    error: state.error,
    loading: state.loading,
    settled: state.key === key,
    reload: () => setTick((n) => n + 1),
    set: (next) => setState({ data: next, error: undefined, loading: false, key }),
  };
}

/** Runs an async action and tracks it: `busy` while it runs, and the error
 *  it ended with, if any. A second call while busy is ignored. */
export function useAction<A extends unknown[]>(action: (...args: A) => Promise<unknown>): {
  run: (...args: A) => Promise<boolean>;
  busy: boolean;
  error: unknown;
  clear: () => void;
} {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(undefined);
  const running = useRef(false);
  const latest = useRef(action);
  latest.current = action;
  const mounted = useRef(true);
  useEffect(() => () => void (mounted.current = false), []);
  return {
    busy,
    error,
    clear: () => setError(undefined),
    run: async (...args: A) => {
      if (running.current) return false;
      running.current = true;
      setBusy(true);
      setError(undefined);
      try {
        await latest.current(...args);
        return true;
      } catch (cause) {
        if (mounted.current) setError(cause);
        return false;
      } finally {
        running.current = false;
        if (mounted.current) setBusy(false);
      }
    },
  };
}

/** Whether a media query matches, kept current as the window changes:
 *  `useMedia('(min-width: 1024px)')`. For what CSS cannot decide alone, such
 *  as whether a list and its detail fit side by side. */
export function useMedia(query: string): boolean {
  const [matches, setMatches] = useState(() => typeof matchMedia === 'function' && matchMedia(query).matches);
  useEffect(() => {
    const list = matchMedia(query);
    const update = () => setMatches(list.matches);
    update();
    list.addEventListener('change', update);
    return () => list.removeEventListener('change', update);
  }, [query]);
  return matches;
}

// A resource is server data held in signals: what was fetched, whether a
// fetch is running, and what went wrong. It knows which live topics make it
// stale and refetches by itself while something on screen is using it.
//
//   export const meters = resource(listMeters, ['meters', 'incidents', 'settings']);
//
//   function Page() {
//     useResource(meters);                 // fetch now, stay current while mounted
//     const list = meters.data.value;      // undefined until the first answer
//   }
//
// Data survives a page change, so coming back shows the last answer at once
// and refreshes it behind the scenes.
import { batch, effect, signal, type ReadonlySignal } from '@preact/signals';
import { useEffect } from 'preact/hooks';
import { ApiError } from '../api/client';
import { lang } from '../i18n';
import { onChange, type LiveTopic } from './live';

export interface Resource<T> {
  /** The last answer; undefined before the first one. */
  readonly data: ReadonlySignal<T | undefined>;
  /** Why the last fetch failed; cleared by the next success. */
  readonly error: ReadonlySignal<ApiError | undefined>;
  /** True while a fetch is in flight. */
  readonly loading: ReadonlySignal<boolean>;
  /** Fetches now. Calls made while one is in flight share it. */
  load(): Promise<void>;
  /** Replaces the data without fetching: an optimistic edit, or the answer
   *  of a write that returned the new state. */
  set(next: T | ((current: T | undefined) => T | undefined)): void;
  /** Marks a user. While there is at least one, live changes refetch. */
  retain(): () => void;
  /** Forgets everything, as on sign-out. */
  reset(): void;
}

const all = new Set<{ reset(): void; invalidate(): void }>();

/** Clears every resource. Called when the session ends. */
export function resetResources(): void {
  for (const each of all) each.reset();
}

export function resource<T>(fetcher: () => Promise<T>, topics: readonly LiveTopic[], onData?: (data: T) => void): Resource<T> {
  const data = signal<T | undefined>(undefined);
  const error = signal<ApiError | undefined>(undefined);
  const loading = signal(false);
  let users = 0;
  let stale = true;
  let generation = 0;
  let inFlight: Promise<void> | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const load = (): Promise<void> => {
    if (inFlight) return inFlight;
    const mine = ++generation;
    loading.value = true;
    stale = false;
    const run = fetcher()
      .then((value) => {
        if (mine !== generation) return;
        batch(() => {
          data.value = value;
          error.value = undefined;
        });
        onData?.(value);
      })
      .catch((cause: unknown) => {
        if (mine !== generation) return;
        error.value = cause instanceof ApiError ? cause : new ApiError(0, 'unknown', String(cause));
      })
      .finally(() => {
        if (inFlight === run) inFlight = null;
        if (mine === generation) loading.value = false;
      });
    inFlight = run;
    return run;
  };

  // A change that arrives while a fetch is running must not be swallowed by
  // it: the answer in flight may predate the change.
  const invalidate = () => {
    stale = true;
    if (users === 0) return;
    clearTimeout(timer);
    timer = setTimeout(() => {
      if (inFlight) void inFlight.then(() => (stale && users > 0 ? load() : undefined));
      else void load();
    }, 120);
  };

  onChange((changed) => {
    if (topics.some((topic) => changed.has(topic))) invalidate();
  });

  const self: Resource<T> = {
    data,
    error,
    loading,
    load,
    set(next) {
      data.value = typeof next === 'function' ? (next as (current: T | undefined) => T | undefined)(data.peek()) : next;
    },
    retain() {
      users += 1;
      if (stale) void load();
      let released = false;
      return () => {
        if (released) return;
        released = true;
        users -= 1;
      };
    },
    reset() {
      generation += 1;
      inFlight = null;
      clearTimeout(timer);
      stale = true;
      batch(() => {
        data.value = undefined;
        error.value = undefined;
        loading.value = false;
      });
    },
  };
  all.add({ reset: self.reset, invalidate });
  return self;
}

/** Uses a resource for as long as the component is mounted. */
export function useResource<T>(target: Resource<T>): Resource<T> {
  useEffect(() => target.retain(), [target]);
  return target;
}

// The server writes incident text and provider names in the language it is
// asked for, so a change of language makes every answer out of date.
let first = true;
effect(() => {
  void lang.value;
  if (first) {
    first = false;
    return;
  }
  for (const each of all) each.invalidate();
});

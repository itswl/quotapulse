// The event stream that keeps an open dashboard current.
//
// GET /api/v1/stream sends `change` events whose data is a comma-separated
// list of topics. The stream carries no data of its own: a store that hears
// one of its topics simply fetches again. Missing an event therefore costs
// nothing that the next one, or a reconnect, does not repair.
import { signal } from '@preact/signals';
import { API } from '../api/client';

/** What the server announces changes to (engine.Changed*). */
export type LiveTopic = 'meters' | 'reminders' | 'incidents' | 'activity' | 'settings';

export const ALL_TOPICS: readonly LiveTopic[] = ['meters', 'reminders', 'incidents', 'activity', 'settings'];

type Listener = (topics: ReadonlySet<LiveTopic>) => void;

const listeners = new Set<Listener>();
let source: EventSource | null = null;
let retry: ReturnType<typeof setTimeout> | undefined;
let lostTimer: ReturnType<typeof setTimeout> | undefined;
let wasOpen = false;
let failures = 0;
let sessionCheck: () => Promise<boolean> = async () => true;

/** False once the stream has been down for a few seconds: what is on screen
 *  may be stale, and the shell says so. */
export const online = signal(true);

/** Calls back with the topics of every change. Returns how to stop. */
export function onChange(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Resolves on the next change to a topic, or after a time limit — whichever
 *  comes first. True when the change arrived. */
export function nextChange(topic: LiveTopic, timeoutMs: number): Promise<boolean> {
  return new Promise((resolve) => {
    const stop = onChange((topics) => {
      if (!topics.has(topic)) return;
      clearTimeout(timer);
      stop();
      resolve(true);
    });
    const timer = setTimeout(() => {
      stop();
      resolve(false);
    }, timeoutMs);
  });
}

function emit(topics: ReadonlySet<LiveTopic>): void {
  for (const listener of [...listeners]) listener(topics);
}

/** Opens the stream. stillSignedIn is asked when the server closes it for
 *  good, to tell "signed out" from "the server restarted". */
export function startLive(stillSignedIn: () => Promise<boolean>): void {
  sessionCheck = stillSignedIn;
  if (source || typeof EventSource === 'undefined') return;
  connect();
}

export function stopLive(): void {
  clearTimeout(retry);
  clearTimeout(lostTimer);
  source?.close();
  source = null;
  wasOpen = false;
  failures = 0;
  online.value = true;
}

function connect(): void {
  const stream = new EventSource(API + '/stream');
  source = stream;

  stream.addEventListener('open', () => {
    clearTimeout(lostTimer);
    online.value = true;
    failures = 0;
    // Whatever happened while the stream was down went unannounced.
    if (wasOpen) emit(new Set(ALL_TOPICS));
    wasOpen = true;
  });

  stream.addEventListener('change', (event) => {
    const names = String((event as MessageEvent).data ?? '').split(',');
    const topics = new Set(names.map((name) => name.trim()).filter(isTopic));
    if (topics.size > 0) emit(topics);
  });

  stream.addEventListener('error', () => {
    if (source !== stream) return;
    // A blip reconnects by itself within seconds; only say so if it lasts.
    clearTimeout(lostTimer);
    lostTimer = setTimeout(() => {
      online.value = false;
    }, 4000);
    if (stream.readyState !== EventSource.CLOSED) return; // the browser is retrying
    // Closed for good: the server answered with something other than a
    // stream. Find out whether that was a 401 before trying again.
    stream.close();
    source = null;
    failures += 1;
    void sessionCheck().then((signedIn) => {
      if (!signedIn || source) return;
      retry = setTimeout(connect, Math.min(30_000, 1000 * 2 ** Math.min(failures, 5)));
    });
  });
}

function isTopic(name: string): name is LiveTopic {
  return (ALL_TOPICS as readonly string[]).includes(name);
}

if (typeof document !== 'undefined') {
  // A laptop that slept, a phone that left the app: the stream may have
  // been cut without an error. Coming back, refresh what is on screen.
  let hiddenAt = 0;
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      hiddenAt = Date.now();
    } else if (source && hiddenAt > 0 && Date.now() - hiddenAt > 60_000) {
      emit(new Set(ALL_TOPICS));
    }
  });
}

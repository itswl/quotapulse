// The last answer to each question a page asks by key — one meter's series,
// one month's spend, the pages of History — kept once the page is gone.
// Coming back to it, by the way back or a swipe that has already slid it
// in, shows at once what was there when it was left, while the page asks
// again behind the scenes. Resources (resource.ts) do the same for what
// every page shares.
//
//   const last = recall<Spend>('spend:2026-11');   // undefined if not asked yet
//   keep('spend:2026-11', answer);
//
// Keys are app-wide: name what is asked.
import { effect } from '@preact/signals';
import { lang } from '../i18n';

/** Enough for the pages one goes back and forth between. */
const LIMIT = 40;

const kept = new Map<string, unknown>();

/** The last answer for a key, if it has been asked in this session. */
export function recall<T>(key: string): T | undefined {
  return kept.get(key) as T | undefined;
}

/** Keeps the answer for a key. The ones longest unanswered go first. */
export function keep(key: string, answer: unknown): void {
  kept.delete(key);
  kept.set(key, answer);
  if (kept.size > LIMIT) kept.delete(kept.keys().next().value!);
}

/** Forgets every answer, as when the session ends. */
export function forget(): void {
  kept.clear();
}

// The server words its answers in the language it is asked in, so a change
// of language leaves none worth showing.
effect(() => {
  void lang.value;
  kept.clear();
});

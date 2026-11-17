// "Read now": asking the server for a fresh sample, and knowing when it has
// arrived. The request answers at once; the result comes as a `meters`
// change on the event stream.
import { signal } from '@preact/signals';
import { requestSample } from '../api/endpoints';
import { nextChange } from './live';

const ALL = '*';
const running = signal<ReadonlySet<string>>(new Set());

/** True while a fresh sample of the account — or, with no id, of all
 *  accounts — has been asked for and not yet seen. */
export function isSampling(accountId?: string): boolean {
  return running.value.has(accountId ?? ALL) || running.value.has(ALL);
}

/**
 * Asks for a fresh sample and resolves once it has landed, or after a few
 * seconds if nothing changes — a platform that is slow to answer, or a
 * stream that is down. Rejects only if the request itself is refused.
 */
export async function sampleNow(accountId?: string): Promise<void> {
  const key = accountId ?? ALL;
  if (running.peek().has(key)) return;
  running.value = new Set(running.peek()).add(key);
  // Listen before asking, so an answer that beats the response is not missed.
  const landed = nextChange('meters', 8000);
  // Long enough for the spinner to be seen at all.
  const shown = new Promise((resolve) => setTimeout(resolve, 600));
  try {
    await requestSample(accountId);
    await Promise.all([landed, shown]);
  } finally {
    const next = new Set(running.peek());
    next.delete(key);
    running.value = next;
  }
}

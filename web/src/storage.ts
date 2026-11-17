// localStorage that cannot throw. A browser may refuse storage (private
// mode, a blocked site): a preference then lasts for the visit, and nothing
// breaks.
export function readStored(key: string): string | null {
  // Outside a browser (the unit tests) there is nothing stored.
  if (typeof window === 'undefined') return null;
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

/** Stores a value; null removes it. */
export function writeStored(key: string, value: string | null): void {
  if (typeof window === 'undefined') return;
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    // not stored; see above
  }
}

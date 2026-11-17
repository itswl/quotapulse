// The one place the dashboard talks HTTP. Everything else calls the typed
// functions in endpoints.ts, which call request() here.
//
// What request() takes care of, so no caller has to:
//   - the session cookie travels with every call (same origin only)
//   - X-QuotaPulse-Lang tells the server which language to write incident
//     text in
//   - a failure becomes an ApiError carrying the server's code and message
//   - a 401 means the session is gone: the app is told, and shows the sign-in
//     screen in place
import { lang } from '../i18n';

export const API = '/api/v1';

/** A request the server refused, or one that never reached it (status 0). */
export class ApiError extends Error {
  override readonly name = 'ApiError';
  constructor(
    /** The HTTP status; 0 when the server could not be reached. */
    readonly status: number,
    /** The server's stable code: not_found, invalid, too_many_attempts, … or
     *  "network" when there was no answer. */
    readonly code: string,
    message: string,
    /** Seconds to wait, when the server said so (429). */
    readonly retryAfter?: number,
  ) {
    super(message);
  }
}

let unauthorized: () => void = () => {};

/** Registers what happens when the server says the session is gone. */
export function onUnauthorized(handler: () => void): void {
  unauthorized = handler;
}

export interface RequestOptions {
  query?: Record<string, string | number | boolean | undefined | null>;
  signal?: AbortSignal;
  /** Treat a 401 as an ordinary error rather than as the end of the session. */
  expect401?: boolean;
}

export async function request<T>(method: string, path: string, body?: unknown, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json', 'X-QuotaPulse-Lang': lang.peek() };
  const init: RequestInit = { method, headers, credentials: 'same-origin', cache: 'no-store' };
  if (options.signal) init.signal = options.signal;
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }

  let response: Response;
  try {
    response = await fetch(url(path, options.query), init);
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'AbortError') throw cause;
    throw new ApiError(0, 'network', 'the server could not be reached');
  }

  const text = response.status === 204 ? '' : await response.text();
  let parsed: unknown;
  try {
    parsed = text === '' ? undefined : JSON.parse(text);
  } catch {
    parsed = undefined;
  }

  if (!response.ok) {
    const detail = (parsed as { error?: { code?: unknown; message?: unknown } } | undefined)?.error;
    const code = typeof detail?.code === 'string' ? detail.code : fallbackCode(response.status);
    const message = typeof detail?.message === 'string' ? detail.message : response.statusText;
    const retryAfter = Number(response.headers.get('Retry-After'));
    if (response.status === 401 && !options.expect401) unauthorized();
    throw new ApiError(response.status, code, message, Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter : undefined);
  }
  return parsed as T;
}

/** Builds an API URL, leaving out query values that are empty. */
export function url(path: string, query?: RequestOptions['query']): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(query ?? {})) {
    if (value !== undefined && value !== null && value !== '') search.set(key, String(value));
  }
  const tail = search.toString();
  return API + path + (tail ? '?' + tail : '');
}

// A proxy in front of QuotaPulse can answer with a page of its own, which has
// no error envelope; the status still says roughly what happened.
function fallbackCode(status: number): string {
  switch (status) {
    case 401:
      return 'unauthorized';
    case 403:
      return 'forbidden';
    case 404:
      return 'not_found';
    case 409:
      return 'conflict';
    case 413:
      return 'too_large';
    case 429:
      return 'too_many_attempts';
    default:
      return status >= 500 ? 'internal' : 'unknown';
  }
}

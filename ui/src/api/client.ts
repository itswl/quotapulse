/**
 * HTTP client: API key handling, 401 retry, and unified write feedback.
 *
 * The browser trades the key for an HttpOnly session cookie once, so a reload
 * does not ask again and no script on the page can read the credential. The
 * key itself stays in memory for this tab and is only sent as a header while
 * it is known; after a reload requests ride the cookie alone.
 */

import { byId } from '../dom.js';
import { t } from '../i18n/index.js';
import { setLoading } from '../ui/loading.js';
import { showToast } from '../ui/toast.js';
import type { ApiPayload, ErrorResponse, MutationResponse } from './types.js';

export interface FetchResult<T> {
  response: Response;
  data: ApiPayload<T>;
}

/** In-memory for this tab only; the server keeps the durable credential. */
let apiKey = '';

/** Only one API key prompt may exist at a time */
let pendingPrompt: Promise<string | null> | null = null;

export function getApiKey(): string {
  return apiKey;
}

export function setApiKey(value: string | null | undefined): void {
  apiKey = (value ?? '').trim();
}

/**
 * Trade the key for the HttpOnly session cookie the server sets. This both
 * validates the key and is what stops the prompt from returning on reload.
 */
async function establishSession(key: string): Promise<boolean> {
  try {
    const response = await fetch(`${window.location.origin}/api/session`, {
      method: 'POST',
      headers: { 'X-API-Key': key },
    });
    return response.ok;
  } catch {
    return false;
  }
}

/**
 * Save a key everywhere it belongs: validated by the server, cookie minted,
 * and kept in memory for requests made before the cookie round-trips. Clearing
 * deletes the cookie too, so the next load asks again.
 */
export async function saveApiKey(value: string | null | undefined): Promise<boolean> {
  const key = (value ?? '').trim();
  if (!key) {
    try {
      await fetch(`${window.location.origin}/api/session`, { method: 'DELETE', headers: authHeaders() });
    } catch {
      // Offline, or the session already expired: forgetting it locally is still right.
    }
    setApiKey('');
    return true;
  }
  if (!(await establishSession(key))) return false;
  setApiKey(key);
  return true;
}

function authHeaders(): Record<string, string> {
  const key = getApiKey();
  return key ? { 'X-API-Key': key } : {};
}

/** Dig a human-readable error message out of any response body */
function errorMessage(data: unknown, fallback: string): string {
  if (data && typeof data === 'object') {
    const payload = data as Partial<ErrorResponse> & { error?: string };
    if (typeof payload.message === 'string' && payload.message) return payload.message;
    if (Array.isArray(payload.errors) && payload.errors.length) return payload.errors.join(t('common.separator'));
    if (typeof payload.error === 'string' && payload.error) return payload.error;
  }
  return fallback;
}

/**
/**
 * Prompt for the API key. The missing cancel button is deliberate: without a
 * key and without a session the page can fetch nothing, and a "cancel" would
 * just strand the user in front of an empty dashboard.
 */
export function promptForApiKey(message = ''): Promise<string | null> {
  if (pendingPrompt) return pendingPrompt;

  pendingPrompt = new Promise<string | null>((resolve) => {
    const modal = byId('auth-modal');
    const form = byId<HTMLFormElement>('auth-form');
    const input = byId<HTMLInputElement>('auth-api-key');
    const error = byId('auth-error');
    let checking = false;

    // fall back to the browser's native input when the template is trimmed away; still usable
    if (!modal || !form || !input) {
      const value = window.prompt(message || t('auth.title'), getApiKey());
      const key = (value ?? '').trim();
      if (!key) {
        resolve(null);
        return;
      }
      void establishSession(key).then((ok) => {
        if (ok) setApiKey(key);
        resolve(ok ? key : null);
      });
      return;
    }

    if (error) {
      error.textContent = message;
      error.style.display = message ? 'block' : 'none';
    }
    input.value = getApiKey();
    modal.classList.add('active');
    input.focus();

    const onSubmit = (event: Event): void => {
      event.preventDefault();
      const value = input.value.trim();
      if (!value || checking) return;
      checking = true;
      void establishSession(value).then((ok) => {
        checking = false;
        if (!ok) {
          if (error) {
            error.textContent = t('auth.invalid');
            error.style.display = 'block';
          }
          input.select();
          return;
        }
        setApiKey(value);
        modal.classList.remove('active');
        form.removeEventListener('submit', onSubmit);
        resolve(value);
      });
    };

    form.addEventListener('submit', onSubmit);
  });

  // clear the shared promise whether it succeeded or failed, or later requests keep awaiting a finished prompt
  return pendingPrompt.finally(() => {
    pendingPrompt = null;
  });
}

/**
/**
 * Send one request and try hard to parse the body as JSON.
 * When it will not parse, data is null — exactly what a proxy returning an
 * HTML error page produces; callers must tolerate it.
 */
export async function fetchJson<T>(
  endpoint: string,
  options: RequestInit = {},
  retried = false,
): Promise<FetchResult<T>> {
  const isApi = endpoint.startsWith('/api/');

  const url = endpoint.startsWith('http') ? endpoint : `${window.location.origin}${endpoint}`;
  const response = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...authHeaders(),
      ...(options.headers as Record<string, string> | undefined),
    },
  });

  let data: ApiPayload<T> = null;
  try {
    data = (await response.json()) as ApiPayload<T>;
  } catch {
    data = null;
  }

  if (response.status === 401 && isApi && !retried) {
    const key = await promptForApiKey(errorMessage(data, t('auth.invalid')));
    if (key) return fetchJson<T>(endpoint, options, true);
  }

  return { response, data };
}

/** Reads: throw on failure; callers decide how to surface it */
export async function request<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
  const { response, data } = await fetchJson<T>(endpoint, options);
  if (!response.ok) {
    throw new Error(errorMessage(data, response.statusText || `HTTP ${response.status}`));
  }
  if (data === null) {
    throw new Error(t('api.invalid_json'));
  }
  return data as T;
}

export interface MutateOptions {
  success?: string;
  fail?: string;
}

/**
/**
 * Writes: POST JSON, toast once for success and once for failure, null on
 * failure. Toasting uniformly here keeps managers from each deciding
 * differently.
 */
export async function mutate(
  endpoint: string,
  body: unknown,
  { success = '', fail = t('api.operation_failed') }: MutateOptions = {},
): Promise<MutationResponse | null> {
  setLoading(true);
  try {
    const { response, data } = await fetchJson<MutationResponse>(endpoint, {
      method: 'POST',
      body: JSON.stringify(body),
    });
    if (response.ok && data && (data as MutationResponse).status === 'success') {
      if (success) showToast(success, 'success');
      return data as MutationResponse;
    }
    showToast(errorMessage(data, fail), 'error');
    return null;
  } catch (error) {
    console.error(`${fail}:`, error);
    showToast(t('api.failed_retry', { fail }), 'error');
    return null;
  } finally {
    setLoading(false);
  }
}

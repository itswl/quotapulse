/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */

import { byId } from '../dom.js';
import { t } from '../i18n/index.js';
import { readStorage, writeStorage } from '../state.js';
import { setLoading } from '../ui/loading.js';
import { showToast } from '../ui/toast.js';
import type { ApiPayload, ErrorResponse, MutationResponse } from './types.js';

export interface FetchResult<T> {
  response: Response;
  data: ApiPayload<T>;
}

const STORAGE_KEY = 'apiKey';

/* Implementation note. */
let pendingPrompt: Promise<string | null> | null = null;

export function getApiKey(): string {
  return (readStorage(STORAGE_KEY) ?? '').trim();
}

export function setApiKey(value: string | null | undefined): void {
  const key = (value ?? '').trim();
  writeStorage(STORAGE_KEY, key || null);
}

function authHeaders(): Record<string, string> {
  const key = getApiKey();
  return key ? { 'X-API-Key': key } : {};
}

/* Implementation note. */
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
 * Implementation note.
 * Implementation note.
 */
export function promptForApiKey(message = ''): Promise<string | null> {
  if (pendingPrompt) return pendingPrompt;

  pendingPrompt = new Promise<string | null>((resolve) => {
    const modal = byId('auth-modal');
    const form = byId<HTMLFormElement>('auth-form');
    const input = byId<HTMLInputElement>('auth-api-key');
    const error = byId('auth-error');

    // Implementation note.
    if (!modal || !form || !input) {
      const value = window.prompt(message || t('auth.title'), getApiKey());
      if (value !== null) setApiKey(value);
      resolve(getApiKey() || null);
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
      if (!value) {
        if (error) {
          error.textContent = t('auth.title');
          error.style.display = 'block';
        }
        return;
      }
      setApiKey(value);
      modal.classList.remove('active');
      form.removeEventListener('submit', onSubmit);
      resolve(value);
    };

    form.addEventListener('submit', onSubmit);
  });

  // Implementation note.
  return pendingPrompt.finally(() => {
    pendingPrompt = null;
  });
}

async function ensureApiKey(): Promise<string | null> {
  const current = getApiKey();
  if (current) return current;
  return promptForApiKey();
}

/**
 * Implementation note.
 * Implementation note.
 */
export async function fetchJson<T>(
  endpoint: string,
  options: RequestInit = {},
  retried = false,
): Promise<FetchResult<T>> {
  const isApi = endpoint.startsWith('/api/');
  if (isApi) await ensureApiKey();

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

/* Implementation note. */
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
 * Implementation note.
 * Implementation note.
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

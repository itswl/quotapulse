/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */

import { fetchJson, getApiKey, mutate, setApiKey } from '../api/client.js';
import { ENDPOINTS, getJobs } from '../api/endpoints.js';
import type { HealthResponse } from '../api/types.js';
import { byId, inputById, onClick } from '../dom.js';
import { restartAutoRefresh, startAutoRefresh, stopAutoRefresh } from '../data.js';
import { AppState, parseRefreshMinutes, writeStorage } from '../state.js';
import { bindModalClose, openModal } from '../ui/modal.js';
import { formatServerCadence } from '../format.js';
import { disablePush, enablePush, pushState } from '../ui/push.js';
import { showToast } from '../ui/toast.js';

const MODAL_ID = 'settings-modal';

/* Implementation note. */
function syncSettingsForm(): void {
  const autoRefresh = byId<HTMLInputElement>('setting-auto-refresh');
  if (autoRefresh) autoRefresh.checked = AppState.autoRefreshTimer !== null;

  const minutes = byId<HTMLInputElement>('setting-refresh-minutes');
  if (minutes) {
    minutes.value = String(AppState.autoRefreshMinutes);
    minutes.disabled = !(autoRefresh?.checked ?? false);
  }

  const apiKeyInput = byId<HTMLInputElement>('setting-api-key');
  if (apiKeyInput) apiKeyInput.value = getApiKey();
}

export function openSettingsModal(): void {
  syncSettingsForm();
  openModal(MODAL_ID);
  void loadServerCadence();
  void loadAboutVersion();
  void syncPushButton();
}

/**
 * Show the deployed version in About. The health endpoint answers without the API key
 * and returns 503 when degraded, so read the body regardless of status and hide the
 * line when it says nothing.
 */
async function loadAboutVersion(): Promise<void> {
  const line = byId('about-version');
  if (!line) return;
  try {
    const { data } = await fetchJson<HealthResponse>('/health');
    const version = data && 'version' in data ? String(data.version) : '';
    line.textContent = version;
    line.hidden = !version;
  } catch (error) {
    console.warn('Version unavailable:', error);
    line.hidden = true;
  }
}

/** Reflect the browser push state on the toggle button and status line. */
async function syncPushButton(): Promise<void> {
  const button = byId<HTMLButtonElement>('push-toggle-btn');
  const status = byId('push-status');
  if (!button || !status) return;

  let state;
  try {
    state = await pushState();
  } catch {
    return;
  }
  if (!state.supported) {
    button.hidden = true;
    status.hidden = false;
    status.textContent = 'Push notifications are not supported in this browser';
    return;
  }
  button.hidden = false;
  button.textContent = state.enabled ? 'Disable browser push' : 'Enable browser push';
  if (state.permission === 'denied') {
    status.hidden = false;
    status.textContent = 'Notifications are blocked for this site in the browser settings';
  } else {
    status.hidden = true;
  }
}

async function togglePush(): Promise<void> {
  const button = byId<HTMLButtonElement>('push-toggle-btn');
  if (!button) return;
  const enabling = (button.textContent ?? '').startsWith('Enable');
  button.disabled = true;
  try {
    const state = enabling ? await enablePush() : await disablePush();
    showToast(state.enabled ? 'Browser push enabled' : 'Browser push disabled', 'success');
  } catch (error) {
    showToast(error instanceof Error && error.message ? error.message : 'Push setup failed', 'error');
  } finally {
    button.disabled = false;
    await syncPushButton();
  }
}

/** Send a canary webhook through the backend so the operator can verify the channel. */
async function sendTestNotification(): Promise<void> {
  const button = byId<HTMLButtonElement>('test-notify-btn');
  if (!button) return;
  button.disabled = true;
  try {
    await mutate(ENDPOINTS.testNotify, {}, { success: 'Test notification sent', fail: 'Test notification failed' });
  } finally {
    button.disabled = false;
  }
}

/**
 * Show the server's own check cadence next to the display-polling setting, so the two
 * numbers can be compared. The jobs endpoint is read-only; on failure the line stays
 * hidden instead of showing something stale.
 */
async function loadServerCadence(): Promise<void> {
  const line = byId('setting-server-cadence');
  if (!line) return;
  try {
    const jobs = await getJobs();
    const job = (jobs.jobs || []).find((j) => j.name === 'dashboard_refresh');
    const text = formatServerCadence(job);
    line.textContent = text ?? '';
    line.hidden = !text;
  } catch (error) {
    console.warn('Job schedule unavailable:', error);
    line.hidden = true;
  }
}

export function bindSettingsManager(): void {
  onClick('settings-btn', openSettingsModal);
  onClick('test-notify-btn', () => void sendTestNotification());
  onClick('push-toggle-btn', () => void togglePush());
  bindModalClose(MODAL_ID, '.js-close-settings-modal');

  byId('setting-auto-refresh')?.addEventListener('change', (event) => {
    const checked = (event.target as HTMLInputElement).checked;
    const minutesInput = byId<HTMLInputElement>('setting-refresh-minutes');
    if (minutesInput) minutesInput.disabled = !checked;
    if (checked) {
      startAutoRefresh();
      showToast(`Auto-refresh every ${AppState.autoRefreshMinutes} min`, 'success');
    } else {
      stopAutoRefresh();
      showToast('Auto-refresh disabled', 'info');
    }
  });

  byId('setting-refresh-minutes')?.addEventListener('change', (event) => {
    const input = event.target as HTMLInputElement;
    const minutes = parseRefreshMinutes(input.value);
    input.value = String(minutes);
    AppState.autoRefreshMinutes = minutes;
    writeStorage('autoRefreshMinutes', String(minutes));
    if (AppState.autoRefreshTimer) restartAutoRefresh();
    showToast(`Auto-refresh every ${minutes} min`, 'success');
  });

  byId('setting-api-key')?.addEventListener('change', (event) => {
    setApiKey((event.target as HTMLInputElement).value);
    showToast(getApiKey() ? 'API key saved' : 'API key cleared', 'info');
  });

  // Implementation note.
  byId('auth-form')?.addEventListener('submit', () => {
    const settingsInput = byId<HTMLInputElement>('setting-api-key');
    if (settingsInput) settingsInput.value = inputById('auth-api-key').value.trim();
  });
}

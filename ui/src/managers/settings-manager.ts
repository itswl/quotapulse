/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */

import { getApiKey, setApiKey } from '../api/client.js';
import { byId, inputById, onClick } from '../dom.js';
import { restartAutoRefresh, startAutoRefresh, stopAutoRefresh } from '../data.js';
import { AppState, parseRefreshMinutes, writeStorage } from '../state.js';
import { bindModalClose, openModal } from '../ui/modal.js';
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
}

export function bindSettingsManager(): void {
  onClick('settings-btn', openSettingsModal);
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

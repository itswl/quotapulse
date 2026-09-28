/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */

import { getApiKey, setApiKey } from '../api/client.js';
import { byId, inputById, onClick } from '../dom.js';
import { startAutoRefresh, stopAutoRefresh } from '../data.js';
import { AppState } from '../state.js';
import { bindModalClose, openModal } from '../ui/modal.js';
import { showToast } from '../ui/toast.js';

const MODAL_ID = 'settings-modal';

/* Implementation note. */
function syncSettingsForm(): void {
  const autoRefresh = byId<HTMLInputElement>('setting-auto-refresh');
  if (autoRefresh) autoRefresh.checked = AppState.autoRefreshTimer !== null;

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
    if ((event.target as HTMLInputElement).checked) {
      startAutoRefresh();
      showToast('Auto-refresh enabled', 'success');
    } else {
      stopAutoRefresh();
      showToast('Auto-refresh disabled', 'info');
    }
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

/**
 * Custom platform management: define zero-code providers on the page.
 * One GET, a JSON path to the balance, and how the API key travels; the
 * "Test wiring" button proves it against the real endpoint before saving.
 */

import { mutate } from '../api/client.js';
import { ENDPOINTS, getCustomProviders } from '../api/endpoints.js';
import type { BalanceType, CustomProviderConfig, CustomProviderPayload, CustomProviderTestPayload, CustomProviderTestResponse } from '../api/types.js';
import { byId, inputById, inputValue, onClick, selectById, setInputValue } from '../dom.js';
import { t } from '../i18n/index.js';
import { clearFieldErrors, requireFields } from '../ui/forms.js';
import { bindModalClose, openModal } from '../ui/modal.js';
import { showToast } from '../ui/toast.js';

const MODAL_ID = 'custom-provider-modal';

let known: CustomProviderConfig[] = [];

/** invalidateProviderCache is injected by the project manager so saved
 * platforms appear in its dropdown without a reload. */
let invalidateProviderCache: () => void = () => {};

export function setProviderCacheInvalidator(fn: () => void): void {
  invalidateProviderCache = fn;
}

async function loadKnown(): Promise<CustomProviderConfig[]> {
  try {
    const result = await getCustomProviders();
    known = result.providers || [];
  } catch {
    known = [];
  }
  return known;
}

function renderChips(activeName: string): void {
  const list = byId('custom-provider-list');
  if (!list) return;
  list.innerHTML = '';
  for (const provider of known) {
    const chip = document.createElement('button');
    chip.type = 'button';
    chip.className = 'chip' + (provider.name === activeName ? ' active' : '');
    chip.textContent = provider.display_name || provider.name;
    chip.addEventListener('click', () => fillForm(provider));
    list.appendChild(chip);
  }
  const add = document.createElement('button');
  add.type = 'button';
  add.className = 'chip';
  add.textContent = '+' + t('providers.new');
  add.addEventListener('click', () => fillForm(null));
  list.appendChild(add);
}

function fillForm(provider: CustomProviderConfig | null): void {
  clearFieldErrors('custom-provider-form');
  setInputValue('custom-provider-edit-mode', provider ? 'true' : 'false');
  const deleteBtn = byId('delete-custom-provider-btn');
  if (deleteBtn) deleteBtn.style.display = provider ? 'inline-flex' : 'none';
  inputById('custom-provider-name').value = provider?.name ?? '';
  inputById('custom-provider-name').readOnly = Boolean(provider); // the key is stable; delete and recreate to rename
  setInputValue('custom-provider-display', provider?.display_name ?? '');
  setInputValue('custom-provider-url', provider?.url ?? '');
  selectById('custom-provider-auth').value = provider?.auth_mode ?? 'bearer';
  setInputValue('custom-provider-auth-param', provider?.auth_param ?? '');
  selectById('custom-provider-type').value = provider?.default_type ?? 'balance';
  setInputValue('custom-provider-currency', provider?.currency ?? '');
  setInputValue('custom-provider-path', provider?.balance_path ?? '');
  setInputValue('custom-provider-scale', provider?.balance_scale ? String(provider.balance_scale) : '');
  setInputValue('custom-provider-success-path', provider?.success_path ?? '');
  setInputValue('custom-provider-success-values', provider?.success_values ?? '');
  setInputValue('custom-provider-error-path', provider?.error_path ?? '');
  renderChips(provider?.name ?? '');
}

export async function openCustomProviderModal(): Promise<void> {
  await loadKnown();
  fillForm(null);
  openModal(MODAL_ID);
}

function collectPayload(): CustomProviderPayload | null {
  const isEdit = inputById('custom-provider-edit-mode').value === 'true';
  const required: Array<[string, string]> = [
    ['custom-provider-name', t('providers.name_required')],
    ['custom-provider-url', t('providers.url_required')],
    ['custom-provider-path', t('providers.path_required')],
  ];
  if (!requireFields(required)) return null;

  const data: CustomProviderPayload = {
    name: inputValue('custom-provider-name').trim().toLowerCase(),
    display_name: inputValue('custom-provider-display'),
    url: inputValue('custom-provider-url'),
    auth_mode: selectById('custom-provider-auth').value,
    auth_param: inputValue('custom-provider-auth-param') || '',
    default_type: selectById('custom-provider-type').value as BalanceType,
    balance_path: inputValue('custom-provider-path'),
  };
  const currency = inputValue('custom-provider-currency').trim().toUpperCase();
  if (currency) data.currency = currency;
  const scale = inputValue('custom-provider-scale');
  if (scale !== '') data.balance_scale = Number.parseFloat(scale);
  const successPath = inputValue('custom-provider-success-path');
  if (successPath) data.success_path = successPath;
  const successValues = inputValue('custom-provider-success-values');
  if (successValues) data.success_values = successValues;
  const errorPath = inputValue('custom-provider-error-path');
  if (errorPath) data.error_path = errorPath;
  void isEdit;
  return data;
}

async function saveProvider(): Promise<void> {
  const data = collectPayload();
  if (!data) return;
  const result = await mutate(ENDPOINTS.saveCustomProvider, data, {
    success: t('providers.saved', { name: data.name }),
    fail: t('common.save_failed'),
  });
  if (result) {
    await loadKnown();
    fillForm(null);
    invalidateProviderCache();
  }
}

async function removeProvider(): Promise<void> {
  const name = inputValue('custom-provider-name').trim().toLowerCase();
  if (!name) return;
  if (!(await mutate(ENDPOINTS.deleteCustomProvider, { name }, { success: t('providers.deleted', { name }), fail: t('common.delete_failed') }))) return;
  await loadKnown();
  fillForm(null);
  invalidateProviderCache();
}

async function testWiring(): Promise<void> {
  const data = collectPayload();
  if (!data) return;
  const payload: CustomProviderTestPayload = { ...data, api_key: promptApiKey() };
  const result = await mutate(ENDPOINTS.testCustomProvider, payload, { fail: t('providers.test_failed') });
  if (result) {
    const tested = result as unknown as CustomProviderTestResponse;
    showToast(t('providers.test_ok', { value: String(tested.value) }), 'success');
  }
}

/** The test call needs a key for bearer/query/header modes; ask once. */
function promptApiKey(): string {
  const mode = selectById('custom-provider-auth').value;
  if (mode === 'none') return '';
  return globalThis.prompt(t('providers.test_key_prompt')) ?? '';
}

export function bindCustomProviderManager(): void {
  bindModalClose(MODAL_ID, '.js-close-custom-provider-modal');
  onClick('manage-providers-btn', () => void openCustomProviderModal());
  byId('test-custom-provider-btn')?.addEventListener('click', () => void testWiring());
  document.querySelector('.js-save-custom-provider')?.addEventListener('click', () => void saveProvider());
  byId('custom-provider-form')?.addEventListener('submit', (e) => {
    e.preventDefault();
    void saveProvider();
  });
  onClick('delete-custom-provider-btn', () => void removeProvider());
  byId('custom-provider-type')?.addEventListener('change', () => {
    const quota = selectById('custom-provider-type').value === 'quota';
    const currencyInput = inputById('custom-provider-currency');
    if (quota) currencyInput.value = '';
    currencyInput.disabled = quota;
  });
}

export async function editCustomProviderByName(name: string): Promise<boolean> {
  await loadKnown();
  const provider = known.find((p) => p.name === name);
  if (!provider) return false;
  fillForm(provider);
  openModal(MODAL_ID);
  return true;
}

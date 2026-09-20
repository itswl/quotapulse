/* Implementation note. */

import { mutate } from '../api/client.js';
import { ENDPOINTS, getSubscriptionsConfig } from '../api/endpoints.js';
import type { CycleType, SubscriptionConfig, SubscriptionPayload, SubscriptionResult } from '../api/types.js';
import { byId, inputById, inputValue, isChecked, onClick, selectById, setChecked, setInputValue } from '../dom.js';
import { reloadSubscriptions } from '../data.js';
import { AppState } from '../state.js';
import { bindModalClose, closeModal, openModal } from '../ui/modal.js';
import { showToast } from '../ui/toast.js';

const MODAL_ID = 'subscription-modal';

/* Implementation note. */
function populateProjectOptions(selectedProject = ''): void {
  const select = byId<HTMLSelectElement>('sub-owner-project');
  if (!select) return;

  const known = [...new Set((AppState.balanceData?.projects || []).map((p) => p.owner_project).filter(Boolean))] as string[];

  select.innerHTML = '';
  const emptyOption = document.createElement('option');
  emptyOption.value = '';
  emptyOption.textContent = 'No owner project';
  select.appendChild(emptyOption);

  for (const project of known) {
    const option = document.createElement('option');
    option.value = project;
    option.textContent = project;
    option.selected = project === selectedProject;
    select.appendChild(option);
  }

  // Implementation note.
  if (selectedProject && !known.includes(selectedProject)) {
    const option = document.createElement('option');
    option.value = selectedProject;
    option.textContent = selectedProject;
    option.selected = true;
    select.appendChild(option);
  }
}

/* Implementation note. */
function updateRenewalDayInputForCycle(): void {
  const cycle = byId<HTMLSelectElement>('sub-cycle')?.value || 'monthly';
  const input = byId<HTMLInputElement>('sub-renewal-day');
  if (!input) return;

  if (cycle === 'weekly') {
    input.min = '1';
    input.max = '7';
    input.placeholder = '1-7';
  } else if (cycle === 'yearly' || cycle === 'lunar_yearly') {
    input.min = '101';
    input.max = '1231';
    input.placeholder = cycle === 'lunar_yearly' ? 'Lunar MMDD, for example 0503' : 'MMDD, for example 0315';
  } else {
    input.min = '1';
    input.max = '31';
    input.placeholder = '1-31';
  }
}

type EditableSubscription = SubscriptionConfig | SubscriptionResult;

export function openSubscriptionModal(subscription: EditableSubscription | null = null): void {
  byId<HTMLFormElement>('subscription-form')?.reset();
  populateProjectOptions(subscription?.owner_project ?? '');

  const title = byId('modal-title');
  if (subscription) {
    if (title) title.textContent = 'Edit subscription';
    setInputValue('edit-mode', 'true');
    setInputValue('original-name', subscription.name);
    setInputValue('sub-name', subscription.name);
    selectById('sub-owner-project').value = subscription.owner_project ?? '';
    setInputValue('sub-amount', subscription.amount || '');
    selectById('sub-cycle').value = subscription.cycle_type || 'monthly';
    setInputValue('sub-renewal-day', subscription.renewal_day || 1);
    setInputValue('sub-alert-days', 'alert_days_before' in subscription ? subscription.alert_days_before || 7 : 7);
    if (subscription.last_renewed_date) {
      setInputValue('sub-last-renewed', subscription.last_renewed_date);
    }
    setChecked('sub-enabled', 'enabled' in subscription ? subscription.enabled !== false : true);
  } else {
    if (title) title.textContent = 'Add subscription';
    setInputValue('edit-mode', 'false');
    setChecked('sub-enabled', true);
  }

  updateRenewalDayInputForCycle();
  openModal(MODAL_ID);
}

function closeSubscriptionModal(): void {
  closeModal(MODAL_ID);
}

async function saveSubscription(event: Event): Promise<void> {
  event.preventDefault();

  const isEdit = inputById('edit-mode').value === 'true';
  const originalName = inputById('original-name').value;

  const data: SubscriptionPayload = {
    name: inputValue('sub-name'),
    owner_project: selectById('sub-owner-project').value || null,
    amount: Number.parseFloat(inputById('sub-amount').value) || 0,
    cycle_type: selectById('sub-cycle').value as CycleType,
    renewal_day: ['yearly', 'lunar_yearly'].includes(selectById('sub-cycle').value)
      ? inputById('sub-renewal-day').value.trim()
      : Number.parseInt(inputById('sub-renewal-day').value, 10),
    alert_days_before: Number.parseInt(inputById('sub-alert-days').value, 10),
    enabled: isChecked('sub-enabled'),
  };

  const lastRenewed = inputById('sub-last-renewed').value;
  if (lastRenewed) data.last_renewed_date = lastRenewed;

  let endpoint: string = ENDPOINTS.addSubscription;
  if (isEdit) {
    endpoint = ENDPOINTS.updateSubscription;
    // Implementation note.
    if (data.name !== originalName) {
      data.new_name = data.name;
      data.name = originalName;
    }
  }

  const result = await mutate(endpoint, data, { success: isEdit ? 'Subscription updated' : 'Subscription added', fail: 'Save failed' });
  if (result) {
    closeSubscriptionModal();
    await reloadSubscriptions();
  }
}

/* Implementation note. */
export async function editSubscription(name: string): Promise<void> {
  try {
    const current = (AppState.subscriptionData?.subscriptions || []).find((s) => s.name === name);
    if (!current) {
      showToast('Subscription not found', 'error');
      return;
    }
    const result = await getSubscriptionsConfig();
    const full = (result.subscriptions || []).find((s) => s.name === name);
    openSubscriptionModal(full ?? current);
  } catch (error) {
    console.error('Failed to load subscription:', error);
    showToast('Load failed', 'error');
  }
}

export async function deleteSubscription(name: string): Promise<void> {
  if (!confirm(`Delete subscription "${name}"?\n\nThis action cannot be undone.`)) return;
  if (await mutate(ENDPOINTS.deleteSubscription, { name }, { success: 'Subscription deleted', fail: 'Delete failed' })) {
    await reloadSubscriptions();
  }
}

export async function markSubscriptionRenewed(name: string): Promise<void> {
  if (await mutate(ENDPOINTS.markRenewed, { name }, { success: 'Marked as renewed' })) {
    await reloadSubscriptions();
  }
}

export async function clearSubscriptionRenewed(name: string): Promise<void> {
  if (await mutate(ENDPOINTS.clearRenewed, { name }, { success: 'Renewal mark cleared' })) {
    await reloadSubscriptions();
  }
}

export function bindSubscriptionManager(): void {
  onClick('add-subscription-btn', () => openSubscriptionModal());
  bindModalClose(MODAL_ID, '.js-close-subscription-modal');
  document.querySelector('.js-save-subscription')?.addEventListener('click', (e) => void saveSubscription(e));
  byId('subscription-form')?.addEventListener('submit', (e) => void saveSubscription(e));
  byId('sub-cycle')?.addEventListener('change', updateRenewalDayInputForCycle);
}

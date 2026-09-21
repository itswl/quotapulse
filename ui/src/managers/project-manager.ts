/**
 * Implementation note.
 * Implementation note.
 */

import { mutate, request } from '../api/client.js';
import { ENDPOINTS, getProviders } from '../api/endpoints.js';
import type { BalanceType, ProjectConfig, ProjectPayload, ProjectsConfigResponse, ProviderOption } from '../api/types.js';
import { byId, fillSelect, inputById, inputValue, isChecked, onClick, selectById, setChecked, setInputValue } from '../dom.js';
import { reloadProjects } from '../data.js';
import { confirmDialog } from '../ui/confirm.js';
import { clearFieldErrors, requireFields } from '../ui/forms.js';
import { bindModalClose, closeModal, openModal } from '../ui/modal.js';
import { showToast } from '../ui/toast.js';

const MODAL_ID = 'project-modal';

/* Implementation note. */
let providers: ProviderOption[] = [];

async function loadProviders(): Promise<ProviderOption[]> {
  if (providers.length) return providers;
  try {
    const result = await getProviders();
    providers = result.providers || [];
  } catch (error) {
    console.warn('Failed to load provider list:', error);
  }
  return providers;
}

function fillProviderOptions(selected = ''): void {
  const select = byId<HTMLSelectElement>('project-provider');
  if (!select) return;
  fillSelect(
    select,
    providers.map(({ value, label }) => ({ value, label: `${label} (${value})` })),
    selected,
  );
}

/* Implementation note. */
function syncTypeHint(): void {
  const provider = byId<HTMLSelectElement>('project-provider')?.value;
  const known = providers.find((p) => p.value === provider);
  const typeSelect = byId<HTMLSelectElement>('project-type');
  const hint = byId('project-threshold-hint');

  // Implementation note.
  if (known && typeSelect && !typeSelect.dataset['touched']) {
    typeSelect.value = known.default_type;
  }
  if (hint) {
    hint.textContent =
      typeSelect?.value === 'quota'
        ? 'Enter a remaining percentage for quota providers; for example, 10 alerts below 10%.'
        : 'Alert when the balance is below this value; leave empty to disable alerts.';
  }
}

export async function openProjectModal(project: ProjectConfig | null = null): Promise<void> {
  await loadProviders();
  byId<HTMLFormElement>('project-form')?.reset();
  clearFieldErrors('project-form');

  const typeSelect = selectById('project-type');
  typeSelect.dataset['touched'] = project ? 'true' : '';

  const isEdit = Boolean(project);
  const title = byId('project-modal-title');
  if (title) title.textContent = isEdit ? 'Edit project' : 'Add project';
  setInputValue('project-edit-mode', String(isEdit));

  const nameInput = inputById('project-name');
  nameInput.value = project?.name ?? '';
  nameInput.readOnly = isEdit; // The name is the stable key; delete and recreate it to rename.
  fillProviderOptions(project?.provider ?? '');
  selectById('project-provider').disabled = isEdit;
  setInputValue('project-threshold', project?.threshold ?? '');
  typeSelect.value = project?.type ?? '';
  setInputValue('project-owner', project?.owner_project ?? '');
  setChecked('project-enabled', project ? project.enabled !== false : true);
  setInputValue('project-api-key', '');

  const keyHint = byId('project-api-key-hint');
  if (keyHint) keyHint.textContent = isEdit ? 'Leave empty to keep the existing API key' : '';

  // Implementation note.
  const envNote = byId('project-env-note');
  if (envNote) envNote.style.display = project?.from_env ? 'block' : 'none';

  syncTypeHint();
  openModal(MODAL_ID);
}

function closeProjectModal(): void {
  closeModal(MODAL_ID);
}

async function saveProject(event: Event): Promise<void> {
  event.preventDefault();

  const isEdit = inputById('project-edit-mode').value === 'true';

  clearFieldErrors('project-form');
  const required: Array<[string, string]> = [['project-name', 'Project name is required']];
  if (!isEdit) required.push(['project-api-key', 'An API key is required for a new project']);
  if (!requireFields(required)) return;

  const threshold = inputById('project-threshold').value;
  const typeValue = selectById('project-type').value;

  const data: ProjectPayload = {
    name: inputValue('project-name'),
    provider: selectById('project-provider').value,
    type: (typeValue || null) as BalanceType | null,
    owner_project: inputValue('project-owner') || null,
    enabled: isChecked('project-enabled'),
  };
  if (threshold !== '') data.threshold = Number.parseFloat(threshold);
  const apiKey = inputValue('project-api-key');
  if (apiKey) data.api_key = apiKey;

  const result = await mutate(ENDPOINTS.saveProject, data, {
    success: isEdit ? 'Project updated' : 'Project added',
    fail: 'Save failed',
  });
  if (result) {
    closeProjectModal();
    await reloadProjects();
  }
}

export async function deleteProject(name: string): Promise<void> {
  const confirmed = await confirmDialog({
    title: `Delete project "${name}"?`,
    message: 'History is retained, but the balance will no longer be checked.',
    confirmLabel: 'Delete project',
  });
  if (!confirmed) return;
  if (await mutate(ENDPOINTS.deleteProject, { name }, { success: 'Project deleted', fail: 'Delete failed' })) {
    await reloadProjects();
  }
}

/* Implementation note. */
export async function editProject(name: string): Promise<void> {
  try {
    const result = await request<ProjectsConfigResponse>('/api/config/projects');
    const project = (result.projects || []).find((p) => p.name === name);
    if (!project) {
      showToast('Project configuration not found', 'error');
      return;
    }
    await openProjectModal(project);
  } catch (error) {
    console.error('Failed to load project configuration:', error);
    showToast('Load failed', 'error');
  }
}

export function bindProjectManager(): void {
  onClick('add-project-btn', () => void openProjectModal());
  bindModalClose(MODAL_ID, '.js-close-project-modal');
  document.querySelector('.js-save-project')?.addEventListener('click', (e) => void saveProject(e));
  byId('project-form')?.addEventListener('submit', (e) => void saveProject(e));
  byId('project-provider')?.addEventListener('change', syncTypeHint);
  byId('project-type')?.addEventListener('change', (e) => {
    (e.target as HTMLSelectElement).dataset['touched'] = 'true';
    syncTypeHint();
  });
}

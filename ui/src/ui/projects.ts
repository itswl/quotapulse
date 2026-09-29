/* Implementation note. */

import { byId, requireById, selectById } from '../dom.js';
import {
  escapeAttr,
  escapeHTML,
  formatBalance,
  formatRunway,
  getBalancePercentage,
  getBalanceStatus,
  needsAttention,
  typeLabel,
} from '../format.js';
import { AppState } from '../state.js';
import type { CheckResult, CreditsResponse, Features, ProjectConfig } from '../api/types.js';
import { emptyState } from './empty.js';
import { ICON_ARROW_RIGHT, ICON_DELETE, ICON_EDIT, ICON_PLUS } from './icons.js';

/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */
export function renderProjectCard(project: CheckResult, features: Features): string {
  // Implementation note.
  // Implementation note.
  if (!project.success) {
    return renderFailedCard(project, features);
  }
  const balance = project.credits || 0;
  const threshold = project.threshold || 0;
  const status = getBalanceStatus(balance, threshold);
  const percentage = getBalancePercentage(balance, threshold);
  const projectStatus = project.need_alarm ? 'alert' : 'normal';
  const ownerProject = project.owner_project || 'No owner project';
  const projectName = project.project || 'Unknown project';
  const provider = project.provider || 'unknown';
  const type = project.type || 'balance';
  const label = typeLabel(type);
  const runway = formatRunway(project.runway, { balanceType: project.type, database: features.database });
  const burn = project.runway ? project.runway.burn_per_day : null;

  const projectNameAttr = escapeAttr(projectName);
  const providerAttr = escapeAttr(provider);

  const optionalActions = projectActions(projectNameAttr, features);

  const trendAction = features.history
    ? `<div class="project-actions">
                    <button class="btn-link js-show-trend" data-project="${projectNameAttr}" data-provider="${providerAttr}">
                        View trend
                        ${ICON_ARROW_RIGHT}
                    </button>
                </div>`
    : '';

  return `
            <div class="project-card" data-provider="${providerAttr}" data-status="${projectStatus}">
                <div class="project-header">
                    <div class="project-info">
                        <h3>${escapeHTML(projectName)}</h3>
                        <div class="project-meta-row">
                            <span class="project-provider">${escapeHTML(provider)}</span>
                            <span class="owner-project-badge">${escapeHTML(ownerProject)}</span>
                            <span class="owner-project-badge">${escapeHTML(label)}</span>
                        </div>
                    </div>
                    <div class="project-header-side">
                        ${optionalActions}
                        <div class="project-status ${projectStatus}"></div>
                    </div>
                </div>
                <div class="project-balance">
                    <div class="balance-label">${type === 'quota' ? 'Remaining quota' : `Current ${label}`}</div>
                    <div class="balance-value">${formatBalance(balance, type)}</div>
                    <div class="balance-progress">
                        <div class="balance-progress-bar ${status}" style="width: ${Math.min(100, percentage)}%"></div>
                    </div>
                </div>
                <div class="project-details">
                    <div class="detail-item">
                        <span class="detail-label">Alert threshold</span>
                        <span class="detail-value">${formatBalance(threshold, type)}</span>
                    </div>
                    <div class="detail-item">
                        <span class="detail-label">Daily spend</span>
                        <span class="detail-value">${burn === null || burn === undefined ? '—' : formatBalance(burn, type)}</span>
                    </div>
                    <div class="detail-item" title="${escapeAttr(runway.hint)}">
                        <span class="detail-label">Remaining</span>
                        <span class="detail-value runway-${runway.level}">${escapeHTML(runway.text)}</span>
                    </div>
                    <div class="detail-item">
                        <span class="detail-label">Status</span>
                        <span class="detail-value status-text ${projectStatus}">${projectStatus === 'normal' ? 'Healthy' : 'Alert'}</span>
                    </div>
                </div>
                ${trendAction}
            </div>
        `;
}

/* Edit and delete need dynamic configuration; nameAttr is already escaped. */
function projectActions(nameAttr: string, features: Features): string {
  if (!features.dynamic_config) return '';
  return `
                        <button class="action-icon-btn js-edit-project" data-project="${nameAttr}" title="Edit project">
                            ${ICON_EDIT}
                        </button>
                        <button class="action-icon-btn danger js-delete-project" data-project="${nameAttr}" title="Delete project">
                            ${ICON_DELETE}
                        </button>`;
}

/* A disabled project isn't checked, so the balance list leaves it out. Its card comes
   from the configuration instead, or it could never be edited and turned back on. */
export function renderDisabledProjectCard(project: ProjectConfig, features: Features): string {
  const projectName = project.name || 'Unknown project';
  const provider = project.provider || 'unknown';
  const ownerProject = project.owner_project || 'No owner project';

  return `
            <div class="project-card disabled" data-provider="${escapeAttr(provider)}" data-status="disabled">
                <div class="project-header">
                    <div class="project-info">
                        <h3>${escapeHTML(projectName)}</h3>
                        <div class="project-meta-row">
                            <span class="project-provider">${escapeHTML(provider)}</span>
                            <span class="owner-project-badge">${escapeHTML(ownerProject)}</span>
                        </div>
                    </div>
                    <div class="project-header-side">
                        ${projectActions(escapeAttr(projectName), features)}
                        <div class="project-status disabled"></div>
                    </div>
                </div>
                <div class="project-balance">
                    <div class="balance-label">Current balance</div>
                    <div class="balance-value unavailable">Disabled</div>
                </div>
                <div class="project-details">
                    <div class="detail-item full-width">
                        <span class="detail-label">Status</span>
                        <span class="detail-value">Not checked. Edit the project and enable it to resume monitoring.</span>
                    </div>
                </div>
            </div>
        `;
}

/* Implementation note. */
function renderFailedCard(project: CheckResult, features: Features): string {
  const projectName = project.project || 'Unknown project';
  const provider = project.provider || 'unknown';
  const ownerProject = project.owner_project || 'No owner project';
  const reason = project.error || 'Unknown reason';

  const projectNameAttr = escapeAttr(projectName);
  const providerAttr = escapeAttr(provider);

  const optionalActions = projectActions(projectNameAttr, features);

  return `
            <div class="project-card failed" data-provider="${providerAttr}" data-status="failed">
                <div class="project-header">
                    <div class="project-info">
                        <h3>${escapeHTML(projectName)}</h3>
                        <div class="project-meta-row">
                            <span class="project-provider">${escapeHTML(provider)}</span>
                            <span class="owner-project-badge">${escapeHTML(ownerProject)}</span>
                        </div>
                    </div>
                    <div class="project-header-side">
                        ${optionalActions}
                        <div class="project-status failed"></div>
                    </div>
                </div>
                <div class="project-balance">
                    <div class="balance-label">Current balance</div>
                    <div class="balance-value unavailable">Unavailable</div>
                </div>
                <div class="project-details">
                    <div class="detail-item full-width">
                        <span class="detail-label">Failure reason</span>
                        <span class="detail-value failure-reason" title="${escapeAttr(reason)}">${escapeHTML(reason)}</span>
                    </div>
                </div>
            </div>
        `;
}

/* Implementation note. */
export function filterProjects(
  projects: CheckResult[],
  { search, provider, alertsOnly }: { search: string; provider: string; alertsOnly: boolean },
): CheckResult[] {
  let result = projects;
  if (search) {
    const query = search.toLowerCase();
    result = result.filter(
      (p) => (p.project || '').toLowerCase().includes(query) || (p.provider || '').toLowerCase().includes(query),
    );
  }
  if (provider !== 'all') {
    result = result.filter((p) => p.provider === provider);
  }
  if (alertsOnly) {
    result = result.filter(needsAttention);
  }
  return result;
}

/* Empty states say what happened and offer the next step instead of leaving a blank grid. */
function renderProjectsEmpty(total: number, features: Features): string {
  if (total === 0) {
    const action = features.dynamic_config
      ? `<button type="button" class="btn-primary js-open-add-project">${ICON_PLUS}Add project</button>`
      : '';
    const text = features.dynamic_config
      ? 'Add a provider account here, or set its key in the environment (DEEPSEEK_API_KEY, OPENROUTER_API_KEY, …) and restart the service.'
      : 'Set a provider key in the environment (DEEPSEEK_API_KEY, OPENROUTER_API_KEY, …) and restart the service, or enable ENABLE_DYNAMIC_CONFIG to add projects from the dashboard.';
    return emptyState('No projects yet', text, 'info', false, action);
  }

  const filtering = Boolean(AppState.searchQuery) || AppState.currentFilter !== 'all';
  if (AppState.alertsOnly && !filtering) {
    return emptyState('No alerts', 'Every account was checked and is above its alert threshold.', 'check');
  }
  return emptyState(
    'No matching projects',
    'No projects match the filters',
    'search',
    false,
    '<button type="button" class="btn-secondary js-clear-filters">Clear filters</button>',
  );
}

export function renderProjects(data: CreditsResponse): void {
  const container = requireById('projects-container');

  const isList = AppState.projectViewStyle === 'list';
  const projects = data.projects || [];
  const filtered = filterProjects(projects, {
    search: AppState.searchQuery,
    provider: AppState.currentFilter,
    alertsOnly: AppState.alertsOnly,
  });
  const disabled = filterDisabledProjects(AppState.disabledProjects);

  // Cards cascade in on the first paint only; refreshes and filter changes swap in place.
  const firstPaint = !container.innerHTML.includes('project-card');
  const stagger = firstPaint && filtered.length > 0 ? ' stagger' : '';
  container.className = (isList ? 'projects-list' : 'projects-grid') + stagger;
  container.removeAttribute?.('aria-busy');
  byId('view-list-btn')?.classList.toggle('active', isList);
  byId('view-grid-btn')?.classList.toggle('active', !isList);

  container.innerHTML =
    filtered.length === 0 && disabled.length === 0
      ? renderProjectsEmpty(projects.length + AppState.disabledProjects.length, AppState.features)
      : [
          ...filtered.map((p) => renderProjectCard(p, AppState.features)),
          ...disabled.map((p) => renderDisabledProjectCard(p, AppState.features)),
        ].join('');
}

/* Disabled cards follow the same search and provider filter, and never count as alerts. */
function filterDisabledProjects(projects: ProjectConfig[]): ProjectConfig[] {
  if (AppState.alertsOnly) return [];
  const query = AppState.searchQuery.toLowerCase();
  return projects.filter(
    (p) =>
      (AppState.currentFilter === 'all' || p.provider === AppState.currentFilter) &&
      (!query || p.name.toLowerCase().includes(query) || p.provider.toLowerCase().includes(query)),
  );
}

/* Rebuild the provider options, keeping the current choice selected. A provider that is
   gone resets the filter too, so the list never stays filtered by an invisible option. */
export function updateProviderFilter(data: CreditsResponse): void {
  const select = selectById('provider-filter');
  const providers = [
    ...new Set([...(data.projects || []).map((p) => p.provider), ...AppState.disabledProjects.map((p) => p.provider)]),
  ];

  select.innerHTML = '';
  const allOption = document.createElement('option');
  allOption.value = 'all';
  allOption.textContent = 'All providers';
  select.appendChild(allOption);

  for (const provider of providers) {
    const option = document.createElement('option');
    option.value = provider;
    option.textContent = provider;
    select.appendChild(option);
  }

  if (AppState.currentFilter !== 'all' && !providers.includes(AppState.currentFilter)) {
    AppState.currentFilter = 'all';
  }
  select.value = AppState.currentFilter;
}

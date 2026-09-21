/* Implementation note. */

import { byId, requireById, selectById } from '../dom.js';
import {
  escapeAttr,
  escapeHTML,
  formatBalance,
  formatRunway,
  getBalancePercentage,
  getBalanceStatus,
  typeLabel,
} from '../format.js';
import { AppState } from '../state.js';
import type { CheckResult, CreditsResponse, Features } from '../api/types.js';
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
  const runway = formatRunway(project.runway);
  const burn = project.runway ? project.runway.burn_per_day : null;

  const projectNameAttr = escapeAttr(projectName);
  const providerAttr = escapeAttr(provider);

  // Implementation note.
  const optionalActions = features.dynamic_config
    ? `
                        <button class="action-icon-btn js-edit-project" data-project="${projectNameAttr}" title="Edit project">
                            ${ICON_EDIT}
                        </button>
                        <button class="action-icon-btn danger js-delete-project" data-project="${projectNameAttr}" title="Delete project">
                            ${ICON_DELETE}
                        </button>`
    : '';

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

/* Implementation note. */
function renderFailedCard(project: CheckResult, features: Features): string {
  const projectName = project.project || 'Unknown project';
  const provider = project.provider || 'unknown';
  const ownerProject = project.owner_project || 'No owner project';
  const reason = project.error || 'Unknown reason';

  const projectNameAttr = escapeAttr(projectName);
  const providerAttr = escapeAttr(provider);

  const optionalActions = features.dynamic_config
    ? `
                        <button class="action-icon-btn js-edit-project" data-project="${projectNameAttr}" title="Edit project">
                            ${ICON_EDIT}
                        </button>
                        <button class="action-icon-btn danger js-delete-project" data-project="${projectNameAttr}" title="Delete project">
                            ${ICON_DELETE}
                        </button>`
    : '';

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
    result = result.filter((p) => p.need_alarm);
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
      ? 'Add a provider account here, or set {PROVIDER}_API_KEY in the environment and restart the service.'
      : 'Set {PROVIDER}_API_KEY in the environment and restart the service, or enable ENABLE_DYNAMIC_CONFIG to add projects from the dashboard.';
    return emptyState('No projects yet', text, 'info', false, action);
  }

  const filtering = Boolean(AppState.searchQuery) || AppState.currentFilter !== 'all';
  if (!filtering && AppState.currentView === 'alerts') {
    return emptyState('No alerts', 'Every monitored account is above its alert threshold.', 'check');
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
    alertsOnly: AppState.currentView === 'alerts',
  });

  // Cards cascade in on the first paint only; refreshes and filter changes swap in place.
  const firstPaint = !container.innerHTML.includes('project-card');
  const stagger = firstPaint && filtered.length > 0 ? ' stagger' : '';
  container.className = (isList ? 'projects-list' : 'projects-grid') + stagger;
  container.removeAttribute?.('aria-busy');
  byId('view-list-btn')?.classList.toggle('active', isList);
  byId('view-grid-btn')?.classList.toggle('active', !isList);

  container.innerHTML =
    filtered.length === 0
      ? renderProjectsEmpty(projects.length, AppState.features)
      : filtered.map((p) => renderProjectCard(p, AppState.features)).join('');
}

/* Implementation note. */
export function updateProviderFilter(data: CreditsResponse): void {
  const select = selectById('provider-filter');
  const providers = [...new Set((data.projects || []).map((p) => p.provider))];

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
}

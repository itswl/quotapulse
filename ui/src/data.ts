/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */

import { byId, toggleDisplay } from './dom.js';
import { getCredits, getFeatures, getSubscriptions, refresh as refreshApi } from './api/endpoints.js';
import type { SubscriptionsResponse } from './api/types.js';
import { AppState } from './state.js';
import { emptyState, loadErrorDetail } from './ui/empty.js';
import { setLoading } from './ui/loading.js';
import { renderProjects, updateProviderFilter } from './ui/projects.js';
import { renderSubscriptions, renderSubscriptionsError } from './ui/subscriptions.js';
import { refreshOverview, updateNavFreshness } from './ui/stats.js';
import { showToast } from './ui/toast.js';

/* Implementation note. */
export async function loadFeatures(): Promise<void> {
  try {
    const result = await getFeatures();
    AppState.features = { ...AppState.features, ...(result.features || {}) };
  } catch (error) {
    console.warn('Feature flag loading failed; using core defaults:', error);
  }

  if (!AppState.features.subscriptions) {
    toggleDisplay('view-subscriptions-btn', false);
    toggleDisplay('add-subscription-btn', false);
  }
  if (!AppState.features.email_scan) {
    toggleDisplay('view-email-btn', false);
  }
  // Implementation note.
  toggleDisplay('add-project-btn', AppState.features.dynamic_config);
}

const NO_SUBSCRIPTIONS: SubscriptionsResponse = { last_update: null, subscriptions: [], summary: {} };

/**
 * Load balances and subscriptions and render the active view. The two load independently:
 * a failing /api/credits must not leave the subscription view, its badge and its counters
 * empty, or the other way round. Each view renders from whatever arrived, and the first
 * failure is rethrown for the caller to report.
 */
export async function fetchAndRender(rebuildFilter = false): Promise<void> {
  const [balance, subscriptions] = await Promise.allSettled([
    getCredits(),
    AppState.features.subscriptions ? getSubscriptions() : Promise.resolve(NO_SUBSCRIPTIONS),
  ]);

  AppState.balanceLoadFailed = balance.status === 'rejected';
  AppState.subscriptionLoadError = subscriptions.status === 'rejected' ? (subscriptions.reason ?? 'error') : null;
  if (balance.status === 'fulfilled') {
    AppState.balanceData = balance.value;
    AppState.lastUpdate = new Date();
    if (rebuildFilter) updateProviderFilter(balance.value);
  }
  if (subscriptions.status === 'fulfilled') {
    AppState.subscriptionData = subscriptions.value;
  }

  if (AppState.currentView === 'subscriptions') {
    if (subscriptions.status === 'fulfilled') renderSubscriptions(subscriptions.value);
    else renderSubscriptionsError(subscriptions.reason);
  } else if (balance.status === 'fulfilled') {
    renderProjects(balance.value);
  }
  refreshOverview();

  if (balance.status === 'rejected') throw balance.reason;
  if (subscriptions.status === 'rejected') throw subscriptions.reason;
}

/* Re-fetch balances after a project change. */
export async function reloadProjects(): Promise<void> {
  const balanceData = await getCredits();
  AppState.balanceData = balanceData;
  AppState.balanceLoadFailed = false;
  renderProjects(balanceData);
  refreshOverview();
}

/* Re-fetch subscriptions after a subscription change, bypassing the ETag cache. */
export async function reloadSubscriptions(): Promise<void> {
  const subscriptionData = await getSubscriptions(true);
  AppState.subscriptionData = subscriptionData;
  AppState.subscriptionLoadError = null;
  renderSubscriptions(subscriptionData);
  refreshOverview();
}

export async function loadData(): Promise<void> {
  try {
    setLoading(true);
    await fetchAndRender(true);
  } catch (error) {
    console.error('Failed to load data:', error);
    showToast('Failed to load data; please try again', 'error');
    renderLoadError(error);
  } finally {
    setLoading(false);
  }
}

/* First-load failure: swap the skeletons for an explanation with a retry, and settle the counters. */
function renderLoadError(error: unknown): void {
  const container = byId('projects-container');
  if (AppState.balanceLoadFailed && container && !container.innerHTML.includes('project-card')) {
    container.removeAttribute?.('aria-busy');
    container.innerHTML = emptyState(
      'Balances could not be loaded',
      loadErrorDetail(error),
      'error',
      false,
      '<button type="button" class="btn-primary js-retry-load">Try again</button>',
    );
  }
  for (const id of ['total-projects', 'normal-projects', 'alert-projects', 'shortest-runway']) {
    const node = byId(id);
    if (node && node.innerHTML.includes('skeleton')) node.textContent = '—';
  }
  updateNavFreshness(AppState.balanceData);
}

/* Implementation note. */
export async function refreshNow(): Promise<void> {
  const btn = byId('refresh-btn');
  try {
    btn?.classList.add('rotating');
    showToast('Refreshing data...', 'info');
    await refreshApi();
    await loadData();
    showToast('Data refreshed', 'success');
  } catch (error) {
    console.error('Refresh failed:', error);
    showToast(error instanceof Error && error.message ? error.message : 'Refresh failed; please try again', 'error');
  } finally {
    btn?.classList.remove('rotating');
  }
}

/* Implementation note. */
export function startAutoRefresh(): void {
  if (AppState.autoRefreshTimer) return;
  AppState.autoRefreshTimer = setInterval(() => {
    const wasFailing = AppState.balanceLoadFailed;
    fetchAndRender().catch((error: unknown) => {
      console.error('Auto-refresh failed:', error);
      // The nav dot turns red on its own; say it in words once per failure streak.
      if (AppState.balanceLoadFailed && !wasFailing) {
        showToast('Background refresh failed; the balances shown may be out of date', 'error');
      }
    });
  }, AppState.autoRefreshMinutes * 60_000);
}

/* Apply a new interval to a running timer. */
export function restartAutoRefresh(): void {
  stopAutoRefresh();
  startAutoRefresh();
}

export function stopAutoRefresh(): void {
  if (!AppState.autoRefreshTimer) return;
  clearInterval(AppState.autoRefreshTimer);
  AppState.autoRefreshTimer = null;
}

/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */

import { byId, toggleDisplay } from './dom.js';
import { getCredits, getFeatures, getSubscriptions, refresh as refreshApi } from './api/endpoints.js';
import { AppState } from './state.js';
import { setLoading } from './ui/loading.js';
import { renderProjects, updateProviderFilter } from './ui/projects.js';
import { renderSubscriptions } from './ui/subscriptions.js';
import { updateStats } from './ui/stats.js';
import { showToast } from './ui/toast.js';

const AUTO_REFRESH_MS = 5 * 60 * 1000;

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

/* Implementation note. */
export async function fetchAndRender(rebuildFilter = false): Promise<void> {
  const balanceData = await getCredits();
  const subscriptionData = AppState.features.subscriptions
    ? await getSubscriptions()
    : { last_update: null, subscriptions: [], summary: {} };

  AppState.balanceData = balanceData;
  AppState.subscriptionData = subscriptionData;
  AppState.lastUpdate = new Date();

  updateStats(balanceData);
  if (rebuildFilter) {
    updateProviderFilter(balanceData);
  }
  if (AppState.currentView === 'subscriptions') {
    renderSubscriptions(subscriptionData);
  } else {
    renderProjects(balanceData);
  }
}

/* Implementation note. */
export async function reloadProjects(): Promise<void> {
  const balanceData = await getCredits();
  AppState.balanceData = balanceData;
  updateStats(balanceData);
  renderProjects(balanceData);
}

/* Implementation note. */
export async function reloadSubscriptions(): Promise<void> {
  const subscriptionData = await getSubscriptions(true);
  AppState.subscriptionData = subscriptionData;
  renderSubscriptions(subscriptionData);
}

export async function loadData(): Promise<void> {
  try {
    setLoading(true);
    await fetchAndRender(true);
  } catch (error) {
    console.error('Failed to load data:', error);
    showToast('Failed to load data; please try again', 'error');
  } finally {
    setLoading(false);
  }
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
    fetchAndRender().catch((error: unknown) => console.error('Auto-refresh failed:', error));
  }, AUTO_REFRESH_MS);
}

export function stopAutoRefresh(): void {
  if (!AppState.autoRefreshTimer) return;
  clearInterval(AppState.autoRefreshTimer);
  AppState.autoRefreshTimer = null;
}

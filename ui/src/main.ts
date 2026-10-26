/**
 * Application entry: binds events, fetches capability switches, loads data,
 * and enters the view matching the hash.
 *
 * The native-JS version scattered bindings across six files' DOMContentLoaded
 * handlers, ordered by whichever <script> loaded first. Bundled into one
 * file, assembly happens here in a fixed order that is easy to trace.
 */

import './styles/index.css';

import { byId, debounce, onClick, selectById } from './dom.js';
import { loadData, loadFeatures, refreshNow, startAutoRefresh, stopAutoRefresh } from './data.js';
import { bindEmailManager, deleteEmail, editEmail, suppressEmail, unsuppressEmail } from './managers/email-manager.js';
import { bindProjectManager, deleteProject, editProject } from './managers/project-manager.js';
import { bindCustomProviderManager, setProviderCacheInvalidator } from './managers/customprovider-manager.js';
import { invalidateProviderCache } from './managers/project-manager.js';
import { bindCostModal } from './ui/costs.js';
import { bindCostTile } from './ui/stats.js';
import { bindSettingsManager } from './managers/settings-manager.js';
import {
  bindSubscriptionManager,
  snoozeSubscription,
  clearSubscriptionRenewed,
  deleteSubscription,
  editSubscription,
  markSubscriptionRenewed,
} from './managers/subscription-manager.js';
import { bindTrendManager, showProjectTrend } from './managers/trend-manager.js';
import { AppState, isViewName, writeStorage, type ProjectViewStyle } from './state.js';
import { updateNavFreshness } from './ui/stats.js';
import { renderProjects } from './ui/projects.js';
import { showToast } from './ui/toast.js';
import { initLocale, initTheme, setAlertsFilter, switchView, syncAlertsChip, toggleLocale, toggleTheme } from './views.js';
import { t } from './i18n/index.js';

const SEARCH_DEBOUNCE_MS = 300;
const NAV_TICK_MS = 30_000;

/**
/**
 * Cards repaint wholesale via innerHTML, so per-element listeners die on
 * every repaint. Event delegation instead: locate the target by data-* and
 * hand it to the right manager.
 */
function bindCardActions(): void {
  document.addEventListener('click', (event) => {
    const target = event.target as HTMLElement | null;
    if (!target) return;

    const handlers: Array<[string, (el: HTMLElement) => void]> = [
      ['.js-edit-project', (el) => void editProject(el.dataset['project'] ?? '')],
      ['.js-delete-project', (el) => void deleteProject(el.dataset['project'] ?? '')],
      ['.js-show-trend', (el) => void showProjectTrend(el.dataset['project'] ?? '', el.dataset['provider'] ?? '')],
      ['.js-edit-email', (el) => editEmail(el.dataset['name'] ?? '')],
      ['.js-delete-email', (el) => void deleteEmail(el.dataset['name'] ?? '')],
      ['.js-email-suppress', (el) => void suppressEmail(el.dataset['mailbox'] ?? '', el.dataset['sender'] ?? '')],
      ['.js-email-unsuppress', (el) => void unsuppressEmail(el.dataset['mailbox'] ?? '', el.dataset['sender'] ?? '')],
      ['.js-mark-renewed', (el) => void markSubscriptionRenewed(el.dataset['name'] ?? '')],
      ['.js-clear-renewed', (el) => void clearSubscriptionRenewed(el.dataset['name'] ?? '')],
      ['.js-snooze-subscription', (el) => void snoozeSubscription(el.dataset['name'] ?? '')],
      ['.js-edit-subscription', (el) => void editSubscription(el.dataset['name'] ?? '')],
      ['.js-delete-subscription', (el) => void deleteSubscription(el.dataset['name'] ?? '')],
      ['.js-clear-filters', () => clearProjectFilters()],
      ['.js-retry-load', () => void loadData()],
      ['.js-open-add-project', () => byId('add-project-btn')?.click()],
    ];

    for (const [selector, handler] of handlers) {
      const el = target.closest<HTMLElement>(selector);
      if (el) {
        handler(el);
        return;
      }
    }
  });
}

/* Reset search, the provider filter and Alerts only; the current view stays. */
function clearProjectFilters(): void {
  AppState.searchQuery = '';
  AppState.currentFilter = 'all';
  AppState.alertsOnly = false;
  syncAlertsChip();
  const search = byId<HTMLInputElement>('search-input');
  if (search) search.value = '';
  const filter = byId<HTMLSelectElement>('provider-filter');
  if (filter) filter.value = 'all';
  if (AppState.balanceData) renderProjects(AppState.balanceData);
}

function setProjectViewStyle(style: ProjectViewStyle): void {
  AppState.projectViewStyle = style;
  writeStorage('projectViewStyle', style);
  if (AppState.balanceData) renderProjects(AppState.balanceData);
}

function bindEvents(): void {
  onClick('theme-toggle', toggleTheme);
  onClick('locale-toggle', toggleLocale);
  onClick('refresh-btn', () => void refreshNow());

  onClick('view-all-btn', () => switchView('all'));
  onClick('filter-alerts-btn', () => setAlertsFilter(!AppState.alertsOnly));
  onClick('view-subscriptions-btn', () => {
    if (!AppState.features.subscriptions) {
      showToast(t('views.subscriptions_disabled'), 'info');
      return;
    }
    switchView('subscriptions');
  });
  onClick('view-email-btn', () => switchView('email'));

  onClick('view-grid-btn', () => setProjectViewStyle('grid'));
  onClick('view-list-btn', () => setProjectViewStyle('list'));

  byId('search-input')?.addEventListener(
    'input',
    debounce((event: Event) => {
      AppState.searchQuery = (event.target as HTMLInputElement).value;
      if (AppState.balanceData) renderProjects(AppState.balanceData);
    }, SEARCH_DEBOUNCE_MS) as EventListener,
  );

  byId('provider-filter')?.addEventListener('change', () => {
    AppState.currentFilter = selectById('provider-filter').value;
    if (AppState.balanceData) renderProjects(AppState.balanceData);
  });

  bindCardActions();
  bindProjectManager();
  bindCustomProviderManager();
  setProviderCacheInvalidator(invalidateProviderCache);
  bindCostModal();
  bindCostTile();
  bindSubscriptionManager();
  bindEmailManager();
  bindSettingsManager();
  bindTrendManager();

  // Relative times freeze between fetches; tick them, then clean up on unload.
  const navTick = window.setInterval(() => updateNavFreshness(AppState.balanceData), NAV_TICK_MS);
  window.addEventListener('beforeunload', () => {
    stopAutoRefresh();
    window.clearInterval(navTick);
  });
}

async function init(): Promise<void> {
  // Releases up to 0.1.51 kept the API key in localStorage. It is traded for an
  // HttpOnly cookie now, so drop the stale copy instead of leaving a readable
  // credential behind on an upgraded browser.
  try {
    localStorage.removeItem('apiKey');
  } catch {
    /* storage disabled: there was nothing to remove */
  }

  initTheme();
  initLocale();
  bindEvents();

  // Register the service worker up front: it carries the runtime cache that
  // lets the installed PWA open offline; push subscription reuses it later.
  navigator.serviceWorker?.register('/sw.js').catch(() => {
    /* no notifications or offline shell; the app still works */
  });

  await loadFeatures();
  await loadData();

  // #alerts / #subscriptions / #email in the address bar enters that view directly
  const initialView = window.location.hash.slice(1);
  if (initialView === 'alerts') {
    // Legacy hash: "Alerts only" is a projects filter now, not a view.
    setAlertsFilter(true);
  } else if (isViewName(initialView) && initialView !== 'all') {
    switchView(initialView);
  }

  startAutoRefresh();
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', () => void init());
} else {
  void init();
}

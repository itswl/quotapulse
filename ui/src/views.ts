/* Theme, interface language, and top-level view routing. */

import { byId } from './dom.js';
import { getLocale, setLocale, t, translateDocument, type Locale } from './i18n/index.js';
import { AppState, writeStorage, type Theme, type ViewName } from './state.js';
import { EmailManager } from './managers/email-manager.js';
import { renderProjects, updateProviderFilter } from './ui/projects.js';
import { renderSubscriptions, renderSubscriptionsError } from './ui/subscriptions.js';
import { refreshOverview } from './ui/stats.js';

const VIEW_BUTTONS: Record<ViewName, string> = {
  all: 'view-all-btn',
  subscriptions: 'view-subscriptions-btn',
  email: 'view-email-btn',
};

/**
 * Toggle the "Alerts only" filter on the projects toolbar. It is a filter, not a view:
 * the projects section stays put and the cards re-render.
 */
export function setAlertsFilter(on: boolean): void {
  AppState.alertsOnly = on;
  syncAlertsChip();
  if (AppState.balanceData) renderProjects(AppState.balanceData);
}

/** Reflect AppState.alertsFilter on the chip's pressed state. */
export function syncAlertsChip(): void {
  const chip = byId('filter-alerts-btn');
  if (!chip) return;
  chip.classList.toggle('active', AppState.alertsOnly);
  chip.setAttribute('aria-pressed', AppState.alertsOnly ? 'true' : 'false');
}

export function applyTheme(theme: Theme): void {
  document.documentElement.setAttribute('data-theme', theme);
  syncThemeColor();
  // The toggle describes the action it offers, not the state you are in.
  const toggle = typeof document.getElementById === 'function' ? document.getElementById('theme-toggle') : null;
  const label = theme === 'dark' ? t('theme.to_light') : t('theme.to_dark');
  toggle?.setAttribute('title', label);
  toggle?.setAttribute('aria-label', label);
}

/* Keep the browser chrome (mobile address bar, PWA title bar) in step with the page background. */
function syncThemeColor(): void {
  if (typeof document.querySelector !== 'function' || typeof getComputedStyle !== 'function') return;
  const meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
  if (!meta) return;
  const bg = getComputedStyle(document.documentElement).getPropertyValue('--bg').trim();
  if (bg) meta.content = bg;
}

export function initTheme(): void {
  applyTheme(AppState.currentTheme);
}

export function toggleTheme(): void {
  AppState.currentTheme = AppState.currentTheme === 'light' ? 'dark' : 'light';
  writeStorage('theme', AppState.currentTheme);
  applyTheme(AppState.currentTheme);
}

/* ==================== Interface language ==================== */

/**
 * Put the whole page in `locale`: the static markup, the controls whose labels are set
 * in code, and whatever rendered content is on screen, redrawn from the data already
 * loaded. Dialogs fill their dynamic text when they open, so they need nothing here.
 */
export function applyLocale(locale: Locale): void {
  setLocale(locale);
  document.documentElement.lang = locale;
  translateDocument();
  syncLocaleToggle();
  applyTheme(AppState.currentTheme); // its label is a sentence

  if (AppState.balanceData) {
    updateProviderFilter(AppState.balanceData);
    renderProjects(AppState.balanceData);
  }
  if (AppState.subscriptionLoadError !== null) renderSubscriptionsError(AppState.subscriptionLoadError);
  else if (AppState.subscriptionData) renderSubscriptions(AppState.subscriptionData);
  if (EmailManager.state.loaded) EmailManager.renderAll();
  // Before the first load the band is skeletons and dashes, which need no words.
  if (AppState.balanceData || AppState.subscriptionData) refreshOverview();
}

/* Like the theme toggle, the control names what it offers: the other language, in that language. */
function syncLocaleToggle(): void {
  const toggle = byId('locale-toggle');
  toggle?.setAttribute('title', t('locale.switch'));
  toggle?.setAttribute('aria-label', t('locale.switch_aria'));
}

export function initLocale(): void {
  applyLocale(getLocale());
}

/* An explicit choice is remembered; the start-up detection is not. */
export function toggleLocale(): void {
  const next: Locale = getLocale() === 'en' ? 'zh-CN' : 'en';
  setLocale(next, { persist: true });
  applyLocale(next);
}

export function switchView(view: ViewName): void {
  if (view === 'email' && !AppState.features.email_scan) {
    view = 'all';
  }
  AppState.currentView = view;

  // Mirror the view in the URL hash so a reload lands on the same tab.
  if (window.history?.replaceState) {
    window.history.replaceState(null, '', window.location.pathname + (view === 'all' ? '' : `#${view}`));
  }

  document.querySelectorAll('.action-btn').forEach((btn) => {
    btn.classList.remove('active');
    btn.setAttribute('aria-pressed', 'false');
  });
  const active = byId(VIEW_BUTTONS[view]);
  active?.classList.add('active');
  active?.setAttribute('aria-pressed', 'true');

  // Subscriptions and email have their own sections; everything else is the projects section.
  const visible = view === 'subscriptions' || view === 'email' ? view : 'projects';
  for (const [name, id] of [
    ['projects', 'projects-section'],
    ['subscriptions', 'subscriptions-section'],
    ['email', 'email-section'],
  ] as const) {
    const section = byId(id);
    if (section) section.style.display = name === visible ? 'block' : 'none';
  }

  if (view === 'all') {
    if (AppState.balanceData) renderProjects(AppState.balanceData);
  } else if (view === 'subscriptions') {
    if (AppState.subscriptionLoadError !== null) renderSubscriptionsError(AppState.subscriptionLoadError);
    else if (AppState.subscriptionData) renderSubscriptions(AppState.subscriptionData);
  } else {
    void EmailManager.load();
  }

  // Band content and switcher badges follow the active view; the filter chip follows state.
  syncAlertsChip();
  refreshOverview();
}

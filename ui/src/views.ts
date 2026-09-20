/* Implementation note. */

import { byId } from './dom.js';
import { AppState, writeStorage, type Theme, type ViewName } from './state.js';
import { EmailManager } from './managers/email-manager.js';
import { renderProjects } from './ui/projects.js';
import { renderSubscriptions } from './ui/subscriptions.js';

/* Implementation note. */
const VIEW_BUTTONS: Record<ViewName, string> = {
  all: 'view-all-btn',
  alerts: 'view-alerts-btn',
  subscriptions: 'view-subscriptions-btn',
  email: 'view-email-btn',
};

export function applyTheme(theme: Theme): void {
  document.documentElement.setAttribute('data-theme', theme);
}

export function initTheme(): void {
  applyTheme(AppState.currentTheme);
}

export function toggleTheme(): void {
  AppState.currentTheme = AppState.currentTheme === 'light' ? 'dark' : 'light';
  writeStorage('theme', AppState.currentTheme);
  applyTheme(AppState.currentTheme);
}

export function switchView(view: ViewName): void {
  if (view === 'email' && !AppState.features.email_scan) {
    view = 'all';
  }
  AppState.currentView = view;

  // Implementation note.
  if (window.history?.replaceState) {
    window.history.replaceState(null, '', window.location.pathname + (view === 'all' ? '' : `#${view}`));
  }

  document.querySelectorAll('.action-btn').forEach((btn) => btn.classList.remove('active'));
  byId(VIEW_BUTTONS[view])?.classList.add('active');

  // Implementation note.
  const visible = view === 'subscriptions' || view === 'email' ? view : 'projects';
  for (const [name, id] of [
    ['projects', 'projects-section'],
    ['subscriptions', 'subscriptions-section'],
    ['email', 'email-section'],
  ] as const) {
    const section = byId(id);
    if (section) section.style.display = name === visible ? 'block' : 'none';
  }

  if (view === 'all' || view === 'alerts') {
    if (AppState.balanceData) renderProjects(AppState.balanceData);
  } else if (view === 'subscriptions') {
    if (AppState.subscriptionData) renderSubscriptions(AppState.subscriptionData);
  } else {
    void EmailManager.load();
  }
}

// The theme follows the system until the user picks one; the pick persists.
// src/theme-boot.ts applies the same decision before the first paint.
import { computed, effect, signal } from '@preact/signals';
import { readStored, writeStored } from './storage';

export type Theme = 'light' | 'dark';
export type ThemeChoice = Theme | 'system';

const KEY = 'qp.theme';
const systemQuery = typeof matchMedia === 'function' ? matchMedia('(prefers-color-scheme: dark)') : null;
const systemDark = signal(systemQuery?.matches ?? false);
systemQuery?.addEventListener('change', (event) => {
  systemDark.value = event.matches;
});

function storedChoice(): ThemeChoice {
  const stored = readStored(KEY);
  return stored === 'light' || stored === 'dark' ? stored : 'system';
}

/** What the user asked for: a theme, or to follow the system. */
export const themeChoice = signal<ThemeChoice>(storedChoice());

/** The theme on screen. */
export const theme = computed<Theme>(() => {
  if (themeChoice.value !== 'system') return themeChoice.value;
  return systemDark.value ? 'dark' : 'light';
});

export function setTheme(choice: ThemeChoice): void {
  themeChoice.value = choice;
  writeStored(KEY, choice === 'system' ? null : choice);
}

/** Flips light and dark. The result is an explicit choice from then on. */
export function toggleTheme(): void {
  setTheme(theme.value === 'dark' ? 'light' : 'dark');
}

const THEME_COLOR: Record<Theme, string> = { light: '#f6f7f9', dark: '#0f1114' };

if (typeof document !== 'undefined') {
  effect(() => {
    document.documentElement.dataset.theme = theme.value;
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', THEME_COLOR[theme.value]);
  });
}

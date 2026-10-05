/**
 * Interface language.
 *
 * Two dictionaries, one active at a time. `t()` looks a key up in the active one and
 * falls back to English, fills `{placeholders}`, and picks the plural form from the
 * `count` parameter. The rendered markup calls `t()` as it renders; the static markup in
 * index.html carries `data-i18n*` attributes that `translateDocument()` fills in.
 *
 * The language is decided here, not in AppState, because every render module needs it
 * at call time and none of them should depend on the application state for a label.
 */

import { readStorage, writeStorage } from '../state.js';
import { en, type Message, type MessageKey, type Plural } from './en.js';
import { zhCN } from './zh-CN.js';

export type { MessageKey } from './en.js';

export type Locale = 'en' | 'zh-CN';

export const LOCALES: readonly Locale[] = ['en', 'zh-CN'];

export const LOCALE_STORAGE_KEY = 'locale';

export type Params = Record<string, string | number | null | undefined>;

const MESSAGES: Record<Locale, Partial<Record<MessageKey, Message>>> = { en, 'zh-CN': zhCN };

export function isLocale(value: unknown): value is Locale {
  return (LOCALES as readonly unknown[]).includes(value);
}

/**
 * The stored choice wins. Otherwise the browser's language: Chinese of any region lands
 * on Simplified Chinese, the only variant shipped; everything else is English. The
 * language is read through `window` so the Node test stub, which has no navigator, always
 * starts in English whatever the machine's locale is.
 */
export function detectLocale(): Locale {
  const stored = readStorage(LOCALE_STORAGE_KEY);
  if (isLocale(stored)) return stored;
  const language = typeof window !== 'undefined' ? (window.navigator?.language ?? '') : '';
  return /^zh(?:[-_]|$)/i.test(language) ? 'zh-CN' : 'en';
}

let current: Locale = detectLocale();

export function getLocale(): Locale {
  return current;
}

/**
 * Make `locale` the active dictionary. Only an explicit choice is persisted: the
 * start-up detection is not, so a changed browser language is honoured next time.
 */
export function setLocale(locale: Locale, { persist = false } = {}): void {
  current = locale;
  if (persist) writeStorage(LOCALE_STORAGE_KEY, locale);
}

function pluralForm(message: Plural, count: unknown): string {
  return Number(count) === 1 ? message.one : message.other;
}

/* The message for `key` in the active language, with `{name}` placeholders filled from
   `params`. A placeholder without a value is left as it is, so a missing parameter shows
   up in the interface instead of silently disappearing. */
export function t(key: MessageKey, params: Params = {}): string {
  const message = MESSAGES[current][key] ?? en[key];
  const template = typeof message === 'string' ? message : pluralForm(message, params['count']);
  return template.replace(/\{(\w+)\}/g, (placeholder: string, name: string) => {
    const value = params[name];
    return value === undefined || value === null ? placeholder : String(value);
  });
}

/* How the static markup names its messages. */
const DATA_ATTRIBUTES: ReadonlyArray<[dataKey: string, attribute: string | null]> = [
  ['i18n', null], // text content
  ['i18nTitle', 'title'],
  ['i18nAriaLabel', 'aria-label'],
  ['i18nPlaceholder', 'placeholder'],
];

const SELECTOR = '[data-i18n], [data-i18n-title], [data-i18n-aria-label], [data-i18n-placeholder]';

/**
 * Fill every `data-i18n*` element under `root` from the active dictionary:
 * `data-i18n` sets the text, `data-i18n-title`, `data-i18n-aria-label` and
 * `data-i18n-placeholder` set that attribute, and `data-i18n-count` supplies the count
 * for a plural message. An element with `data-i18n` must contain only text; the icon
 * next to a button label lives outside the labelled span for that reason.
 */
export function translateDocument(root: ParentNode = document): void {
  if (typeof root.querySelectorAll !== 'function') return; // the test stub has no selectors
  for (const element of root.querySelectorAll<HTMLElement>(SELECTOR)) {
    const count = element.dataset['i18nCount'];
    const params: Params = count === undefined ? {} : { count: Number(count) };
    for (const [dataKey, attribute] of DATA_ATTRIBUTES) {
      const key = element.dataset[dataKey];
      if (!key) continue;
      const text = t(key as MessageKey, params);
      if (attribute === null) element.textContent = text;
      else element.setAttribute(attribute, text);
    }
  }
}

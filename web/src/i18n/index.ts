// Everything the dashboard says goes through t(). The words live in
// i18n/areas, one file per area of the product; this file only looks them up.
//
//   t('overview.verdict.clear')                  a plain string
//   t('common.ago.minutes', { n: 5 })            {n} is filled in
//   'one thing | {n} things'                     the form is picked by n: the
//                                                first when n is 1, else the second
//
// A key is `<area>.<key>`, and both are checked by the compiler: a typo, or a
// key that exists in one language only, does not build.
import { effect, signal } from '@preact/signals';
import { readStored, writeStored } from '../storage';
import activity from './areas/activity';
import common from './areas/common';
import dates from './areas/dates';
import meters from './areas/meters';
import overview from './areas/overview';
import settings from './areas/settings';
import spend from './areas/spend';

export type Lang = 'zh' | 'en';

const areas = { common, overview, meters, dates, spend, activity, settings };

type Areas = typeof areas;
export type TKey = { [A in keyof Areas]: `${A & string}.${keyof Areas[A]['en'] & string}` }[keyof Areas];
export type TParams = Record<string, string | number>;

const KEY = 'qp.lang';

function initialLang(): Lang {
  const stored = readStored(KEY);
  if (stored === 'zh' || stored === 'en') return stored;
  const preferred = typeof navigator === 'undefined' ? '' : (navigator.language ?? '');
  return preferred.toLowerCase().startsWith('zh') ? 'zh' : 'en';
}

/** The language on screen. Reading it in a component subscribes the component. */
export const lang = signal<Lang>(initialLang());

/** Switches language and remembers the choice. */
export function setLang(next: Lang): void {
  lang.value = next;
  writeStored(KEY, next);
}

if (typeof document !== 'undefined') {
  effect(() => {
    document.documentElement.lang = lang.value === 'zh' ? 'zh-CN' : 'en';
  });
}

const flat: Record<Lang, Map<string, string>> = { en: new Map(), zh: new Map() };
for (const [area, words] of Object.entries(areas)) {
  for (const language of ['en', 'zh'] as const) {
    for (const [key, text] of Object.entries(words[language])) {
      flat[language].set(`${area}.${key}`, text as string);
    }
  }
}

/** Translates a key into the current language. */
export function t(key: TKey, params?: TParams): string {
  return translate(lang.value, key, params);
}

/** Translates into a given language; t() is this with the current one. */
export function translate(language: Lang, key: string, params?: TParams): string {
  let text = flat[language].get(key) ?? flat.en.get(key);
  if (text === undefined) return key;
  if (params && typeof params.n === 'number' && text.includes(' | ')) {
    const [one, other] = text.split(' | ');
    text = params.n === 1 ? one! : (other ?? one!);
  }
  if (!params) return text;
  return text.replace(/\{(\w+)\}/g, (whole, name: string) => (name in params ? String(params[name]) : whole));
}

/** Picks between two strings by the current language, for data that carries
 *  both — a provider's name and name_zh. Not for interface text: that is t(). */
export function pick(en: string, zh: string | undefined): string {
  return lang.value === 'zh' && zh ? zh : en;
}

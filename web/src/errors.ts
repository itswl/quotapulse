// Turns a failure into a sentence for the user.
import { ApiError } from './api/client';
import { lang, t, translate } from './i18n';

/**
 * Says what went wrong, in the current language where the dashboard knows
 * the words. The server's own sentence is used for `invalid`: it names the
 * exact field and limit, and it is only ever written in English.
 */
export function errorText(error: unknown): string {
  if (!(error instanceof ApiError)) return t('common.error.unknown');
  if (error.code === 'invalid' && error.message) {
    const sentence = error.message.charAt(0).toUpperCase() + error.message.slice(1);
    return /[.!?。]$/.test(sentence) ? sentence : sentence + '.';
  }
  const key = `common.error.${error.code}`;
  const known = translate(lang.value, key);
  if (known !== key) return known;
  return error.message || t('common.error.unknown');
}

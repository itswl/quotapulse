/* Toast notifications: status dot, message, dismiss button. */

import { byId } from '../dom.js';
import { ICON_CLOSE } from './icons.js';

export type ToastType = 'info' | 'success' | 'warning' | 'error';

const AUTO_DISMISS_MS = 3200;
const FADE_OUT_MS = 220;

export function showToast(message: unknown, type: ToastType = 'info'): void {
  const container = byId('toast-container');
  if (!container) return;

  const toast = document.createElement('div');
  toast.className = `toast ${type}`;
  toast.setAttribute('role', type === 'error' || type === 'warning' ? 'alert' : 'status');

  const dot = document.createElement('span');
  dot.className = 'toast-dot';
  dot.setAttribute('aria-hidden', 'true');

  const text = document.createElement('div');
  text.className = 'toast-message';
  // Strip any leading emoji a server message might carry; the dot already signals the type.
  text.textContent = String(message).replace(/^[\p{Extended_Pictographic}️\s]+/u, '');

  const closeBtn = document.createElement('button');
  closeBtn.type = 'button';
  closeBtn.className = 'toast-close';
  closeBtn.setAttribute('aria-label', 'Dismiss');
  closeBtn.innerHTML = ICON_CLOSE;

  let dismissed = false;
  const dismiss = (): void => {
    if (dismissed) return;
    dismissed = true;
    toast.classList.add('leaving');
    setTimeout(() => toast.remove(), FADE_OUT_MS);
  };
  closeBtn.addEventListener('click', dismiss);

  toast.append(dot, text, closeBtn);
  container.appendChild(toast);
  setTimeout(dismiss, AUTO_DISMISS_MS);
}

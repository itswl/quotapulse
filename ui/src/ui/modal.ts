/* Dialog open/close helpers shared by every modal on the page. */

import { byId, onClickAll } from '../dom.js';

/* First editable control inside a dialog; hidden, read-only and disabled fields are skipped. */
const FIRST_FIELD = 'input:not([type="hidden"]):not([readonly]):not([disabled]), select:not([disabled]), textarea';

export function openModal(id: string): void {
  const modal = byId(id);
  if (!modal) return;
  modal.classList.add('active');
  // Move keyboard focus into the dialog. The test stub has no querySelector, hence the guard.
  if (typeof modal.querySelector === 'function') {
    modal.querySelector<HTMLElement>(FIRST_FIELD)?.focus();
  }
}

export function closeModal(id: string): void {
  byId(id)?.classList.remove('active');
}

export function isModalOpen(id: string): boolean {
  return byId(id)?.classList.contains('active') ?? false;
}

/**
 * Close on the close buttons, on a backdrop click, and on Escape.
 * `onClose` runs after every close so callers can release resources (for example the chart).
 */
export function bindModalClose(id: string, closeButtonSelector: string, onClose?: () => void): void {
  const close = (): void => {
    closeModal(id);
    onClose?.();
  };

  onClickAll(closeButtonSelector, close);

  byId(id)?.addEventListener('click', (event) => {
    if ((event.target as HTMLElement | null)?.id === id) close();
  });

  document.addEventListener('keydown', (event: KeyboardEvent) => {
    if (event.key === 'Escape' && isModalOpen(id)) close();
  });
}

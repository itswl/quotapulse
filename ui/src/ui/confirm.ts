/**
 * Confirmation dialog for destructive actions.
 *
 * Replaces window.confirm() with the page's own dialog so the copy, focus order and
 * theme match the rest of the interface. Falls back to window.confirm() when the
 * markup is missing (for example in the test stub).
 */

import { byId } from '../dom.js';

export interface ConfirmOptions {
  title: string;
  message?: string;
  confirmLabel?: string;
  cancelLabel?: string;
}

const MODAL_ID = 'confirm-modal';

let settlePending: ((value: boolean) => void) | null = null;

export function confirmDialog(options: ConfirmOptions): Promise<boolean> {
  const modal = byId(MODAL_ID);
  const title = byId('confirm-title');
  const message = byId('confirm-message');
  const accept = byId<HTMLButtonElement>('confirm-accept');
  const cancel = byId<HTMLButtonElement>('confirm-cancel');

  if (!modal || !title || !message || !accept || !cancel) {
    return Promise.resolve(window.confirm([options.title, options.message].filter(Boolean).join('\n\n')));
  }

  // A second dialog while one is open cancels the first one.
  settlePending?.(false);

  return new Promise<boolean>((resolve) => {
    title.textContent = options.title;
    message.textContent = options.message ?? '';
    message.style.display = options.message ? '' : 'none';
    accept.textContent = options.confirmLabel ?? 'Confirm';
    cancel.textContent = options.cancelLabel ?? 'Cancel';

    const finish = (value: boolean): void => {
      settlePending = null;
      modal.classList.remove('active');
      accept.removeEventListener('click', onAccept);
      cancel.removeEventListener('click', onCancel);
      modal.removeEventListener('click', onBackdrop);
      document.removeEventListener('keydown', onKey);
      resolve(value);
    };
    const onAccept = (): void => finish(true);
    const onCancel = (): void => finish(false);
    const onBackdrop = (event: Event): void => {
      if ((event.target as HTMLElement | null)?.id === MODAL_ID) finish(false);
    };
    const onKey = (event: KeyboardEvent): void => {
      if (event.key === 'Escape') finish(false);
    };

    settlePending = finish;
    accept.addEventListener('click', onAccept);
    cancel.addEventListener('click', onCancel);
    modal.addEventListener('click', onBackdrop);
    document.addEventListener('keydown', onKey);

    modal.classList.add('active');
    // Focus lands on Cancel: Enter never destroys anything by accident.
    cancel.focus();
  });
}

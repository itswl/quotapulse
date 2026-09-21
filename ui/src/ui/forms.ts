/**
 * Inline form validation.
 *
 * Errors are shown under the field they belong to (and announced to screen readers)
 * instead of as a toast in the corner. An error clears as soon as the user edits the field.
 */

import { byId } from '../dom.js';

function fieldHost(input: HTMLElement): HTMLElement | null {
  const group = typeof input.closest === 'function' ? input.closest<HTMLElement>('.form-group') : null;
  return group ?? input.parentElement;
}

export function setFieldError(inputId: string, message: string | null): void {
  const input = byId<HTMLInputElement>(inputId);
  if (!input) return;
  const host = fieldHost(input);
  if (!host || typeof host.querySelector !== 'function') return;

  let note = host.querySelector<HTMLElement>('.field-error');
  if (message) {
    if (!note) {
      note = document.createElement('p');
      note.className = 'field-error';
      note.setAttribute('role', 'alert');
      host.appendChild(note);
    }
    note.textContent = message;
    host.classList.add('invalid');
    input.setAttribute('aria-invalid', 'true');
    input.addEventListener('input', () => setFieldError(inputId, null), { once: true });
  } else {
    note?.remove();
    host.classList.remove('invalid');
    input.removeAttribute('aria-invalid');
  }
}

export function clearFieldErrors(formId: string): void {
  const form = byId(formId);
  if (!form || typeof form.querySelectorAll !== 'function') return;
  form.querySelectorAll<HTMLElement>('.field-error').forEach((node) => node.remove());
  form.querySelectorAll<HTMLElement>('.invalid').forEach((node) => node.classList.remove('invalid'));
  form.querySelectorAll<HTMLElement>('[aria-invalid]').forEach((node) => node.removeAttribute('aria-invalid'));
}

/**
 * Check that each listed field has a non-blank value. Marks every failing field, focuses
 * the first one and returns false; returns true when all are present.
 */
export function requireFields(fields: Array<[inputId: string, message: string]>): boolean {
  let first: HTMLInputElement | null = null;
  for (const [id, message] of fields) {
    const input = byId<HTMLInputElement>(id);
    if (!input) continue;
    if (input.value.trim()) {
      setFieldError(id, null);
      continue;
    }
    setFieldError(id, message);
    first ??= input;
  }
  first?.focus();
  return first === null;
}

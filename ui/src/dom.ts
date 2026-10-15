/**
 * The single funnel for DOM access.
 *
 * The native-JS version scattered `document.getElementById('x').value`, and a
 * renamed element died with "Cannot read properties of null" naming no id.
 * Here the two classes split: guaranteed skeleton elements use require* and
 * fail with the id; possibly-absent ones return null for the caller to check.
 */

/** Elements that may not exist (buttons behind capability switches) */
export function byId<T extends HTMLElement = HTMLElement>(id: string): T | null {
  return document.getElementById(id) as T | null;
}

/** Elements the index.html skeleton guarantees; a missing one means a broken template — fail early rather than silently mis-render */
export function requireById<T extends HTMLElement = HTMLElement>(id: string): T {
  const node = document.getElementById(id);
  if (!node) throw new Error(`Page is missing element #${id}`);
  return node as T;
}

export function inputById(id: string): HTMLInputElement {
  return requireById<HTMLInputElement>(id);
}

export function selectById(id: string): HTMLSelectElement {
  return requireById<HTMLSelectElement>(id);
}

/** Read an input's value with surrounding whitespace trimmed */
export function inputValue(id: string): string {
  return inputById(id).value.trim();
}

export function setInputValue(id: string, value: string | number | null | undefined): void {
  inputById(id).value = value === null || value === undefined ? '' : String(value);
}

export function isChecked(id: string): boolean {
  return inputById(id).checked;
}

export function setChecked(id: string, checked: boolean): void {
  inputById(id).checked = checked;
}

export function setText(id: string, text: string): void {
  const node = byId(id);
  if (node) node.textContent = text;
}

/** Show / hide a button per capability switch; keep the original display value or the layout collapses */
export function toggleDisplay(id: string, visible: boolean, shown = 'inline-flex'): void {
  const node = byId(id);
  if (node) node.style.display = visible ? shown : 'none';
}

export function onClick(id: string, handler: (event: MouseEvent) => void): void {
  byId(id)?.addEventListener('click', handler as EventListener);
}

/** A batch of same-class buttons (a dialog's "close" appears in both the title bar and footer) */
export function onClickAll(selector: string, handler: (event: MouseEvent) => void): void {
  document.querySelectorAll<HTMLElement>(selector).forEach((node) => {
    node.addEventListener('click', handler as EventListener);
  });
}

/** Fill a dropdown; the selected entry gets selected */
export function fillSelect(
  select: HTMLSelectElement,
  options: Array<{ value: string; label: string }>,
  selected = '',
): void {
  select.innerHTML = '';
  for (const { value, label } of options) {
    const option = document.createElement('option');
    option.value = value;
    option.textContent = label;
    option.selected = value === selected;
    select.appendChild(option);
  }
}

export function debounce<A extends unknown[]>(fn: (...args: A) => void, wait: number): (...args: A) => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  return (...args: A): void => {
    if (timer !== undefined) clearTimeout(timer);
    timer = setTimeout(() => fn(...args), wait);
  };
}

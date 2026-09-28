/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 * Implementation note.
 */

export interface StubElement {
  tagName: string;
  id: string;
  className: string;
  textContent: string;
  innerHTML: string;
  title: string;
  hidden: boolean;
  value: string;
  checked: boolean;
  disabled: boolean;
  readOnly: boolean;
  style: Record<string, string>;
  dataset: Record<string, string>;
  classList: {
    add(...names: string[]): void;
    remove(...names: string[]): void;
    toggle(name: string, force?: boolean): void;
    contains(name: string): boolean;
  };
  children: StubElement[];
  appendChild(child: StubElement): StubElement;
  append(...children: StubElement[]): void;
  remove(): void;
  addEventListener(): void;
  removeEventListener(): void;
  closest(): null;
  querySelectorAll(): StubElement[];
}

export function createStubElement(tagName = 'div'): StubElement {
  const classes = new Set<string>();
  const el: StubElement = {
    tagName: tagName.toUpperCase(),
    id: '',
    className: '',
    textContent: '',
    innerHTML: '',
    title: '',
    hidden: false,
    value: '',
    checked: false,
    disabled: false,
    readOnly: false,
    style: {},
    dataset: {},
    classList: {
      add: (...names) => names.forEach((n) => classes.add(n)),
      remove: (...names) => names.forEach((n) => classes.delete(n)),
      toggle: (name, force) => {
        const on = force ?? !classes.has(name);
        if (on) classes.add(name);
        else classes.delete(name);
      },
      contains: (name) => classes.has(name),
    },
    children: [],
    appendChild(child) {
      el.children.push(child);
      return child;
    },
    append(...children) {
      el.children.push(...children);
    },
    remove() {
      /* Implementation note. */
    },
    addEventListener() {},
    removeEventListener() {},
    closest: () => null,
    querySelectorAll: () => [],
  };
  return el;
}

const registry = new Map<string, StubElement>();

/* Implementation note. */
export function stubElement(id: string): StubElement {
  const existing = registry.get(id);
  if (existing) return existing;
  const el = createStubElement();
  el.id = id;
  registry.set(id, el);
  return el;
}

export function resetStubDom(): void {
  registry.clear();
}

const storage = new Map<string, string>();

export function installStubDom(): void {
  const documentStub = {
    documentElement: createStubElement('html'),
    getElementById: (id: string): StubElement | null => registry.get(id) ?? null,
    createElement: (tag: string): StubElement => createStubElement(tag),
    querySelector: (): StubElement | null => null,
    querySelectorAll: (): StubElement[] => [],
    addEventListener: (): void => {},
    readyState: 'complete',
  };

  const globals = globalThis as unknown as Record<string, unknown>;
  globals['document'] = documentStub;
  globals['localStorage'] = {
    getItem: (key: string): string | null => storage.get(key) ?? null,
    setItem: (key: string, value: string): void => {
      storage.set(key, value);
    },
    removeItem: (key: string): void => {
      storage.delete(key);
    },
  };
  globals['window'] = {
    location: { origin: 'http://localhost:8080', pathname: '/', hash: '' },
    history: {},
    addEventListener: (): void => {},
    devicePixelRatio: 1,
  };
}

installStubDom();

import type { JSX } from 'preact';

export interface Tab<V extends string> {
  id: V;
  label: string;
  /** A fuller name for assistive technology when the label is terse ("7d"). */
  name?: string;
}

export interface TabsProps<V extends string> {
  tabs: readonly Tab<V>[];
  value: V;
  onChange: (value: V) => void;
  /** Names the set: "Range". */
  label: string;
  /** The id of the element the tabs switch, when there is one. */
  controls?: string;
}

/** A row of tabs that switch what is shown below. Arrow keys move between
 *  them, as the tab pattern asks. */
export function Tabs<V extends string>({ tabs, value, onChange, label, controls }: TabsProps<V>) {
  const onKeyDown = (event: JSX.TargetedKeyboardEvent<HTMLDivElement>) => {
    const index = tabs.findIndex((tab) => tab.id === value);
    let next = index;
    if (event.key === 'ArrowRight') next = (index + 1) % tabs.length;
    else if (event.key === 'ArrowLeft') next = (index - 1 + tabs.length) % tabs.length;
    else if (event.key === 'Home') next = 0;
    else if (event.key === 'End') next = tabs.length - 1;
    else return;
    event.preventDefault();
    const target = tabs[next];
    if (!target) return;
    onChange(target.id);
    event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
  };
  return (
    <div class="tabs" role="tablist" aria-label={label} onKeyDown={onKeyDown}>
      {tabs.map((tab) => (
        <button
          key={tab.id}
          type="button"
          role="tab"
          class="tabs__tab"
          aria-selected={tab.id === value}
          aria-controls={controls}
          aria-label={tab.name}
          tabIndex={tab.id === value ? 0 : -1}
          onClick={() => onChange(tab.id)}
        >
          {tab.label}
        </button>
      ))}
    </div>
  );
}

import { Ellipsis, type LucideIcon } from 'lucide-preact';
import type { TargetedKeyboardEvent } from 'preact';
import { useEffect, useId, useRef, useState } from 'preact/hooks';
import { IconButton } from './Button';
import { cx } from './cx';

export interface MenuItem {
  label: string;
  icon?: LucideIcon;
  onSelect: () => void;
  /** Destroys something: shown in the critical colour. */
  danger?: boolean;
  disabled?: boolean;
}

/** An item, a divider, or a heading over the items that follow. */
export type MenuEntry = MenuItem | { separator: true } | { heading: string };

export interface MenuProps {
  /** Names the menu button. */
  label: string;
  icon?: LucideIcon;
  entries: readonly MenuEntry[];
  /** Which edge of the button the menu lines up with. */
  align?: 'start' | 'end';
  outlined?: boolean;
}

/**
 * A button that opens a short list of actions. Arrow keys move through the
 * items, Enter picks one, Escape closes and hands focus back to the button.
 */
export function Menu({ label, icon = Ellipsis, entries, align = 'end', outlined }: MenuProps) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const menuId = useId();

  const items = () => Array.from(root.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)') ?? []);
  const close = (refocus: boolean) => {
    setOpen(false);
    if (refocus) root.current?.querySelector<HTMLButtonElement>('.icon-btn')?.focus();
  };

  useEffect(() => {
    if (!open) return;
    items()[0]?.focus();
    const outside = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', outside, true);
    return () => document.removeEventListener('pointerdown', outside, true);
  }, [open]);

  const onKeyDown = (event: TargetedKeyboardEvent<HTMLDivElement>) => {
    if (!open) return;
    const list = items();
    const index = list.indexOf(document.activeElement as HTMLButtonElement);
    const focus = (next: number) => {
      event.preventDefault();
      list[(next + list.length) % list.length]?.focus();
    };
    switch (event.key) {
      case 'ArrowDown':
        return focus(index + 1);
      case 'ArrowUp':
        return focus(index - 1);
      case 'Home':
        return focus(0);
      case 'End':
        return focus(list.length - 1);
      case 'Escape':
        event.preventDefault();
        event.stopPropagation();
        return close(true);
      case 'Tab':
        return close(false);
    }
  };

  return (
    <div class="menu" ref={root} onKeyDown={onKeyDown}>
      <IconButton
        icon={icon}
        label={label}
        outlined={outlined ?? false}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        onClick={() => setOpen(!open)}
      />
      {open && (
        <div class={cx('menu__list', align === 'start' && 'menu__list--start')} role="menu" id={menuId} aria-label={label}>
          {entries.map((entry, index) => {
            if ('separator' in entry) return <div key={index} class="menu__separator" role="separator" />;
            if ('heading' in entry) {
              return (
                <div key={index} class="menu__heading" role="presentation">
                  {entry.heading}
                </div>
              );
            }
            const Icon = entry.icon;
            return (
              <button
                key={index}
                type="button"
                role="menuitem"
                class={cx('menu__item', entry.danger && 'menu__item--danger')}
                disabled={entry.disabled}
                onClick={() => {
                  close(true);
                  entry.onSelect();
                }}
              >
                {Icon ? <Icon /> : <span class="menu__no-icon" />}
                <span>{entry.label}</span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

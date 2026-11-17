import type { LucideIcon } from 'lucide-preact';
import type { ComponentChildren } from 'preact';
import { cx } from './cx';

export type Tone = 'neutral' | 'accent' | 'ok' | 'warning' | 'critical' | 'info';

/** A small label: a state, a tag, a count. Not clickable — see FilterChip. */
export function Chip({ tone = 'neutral', icon: Icon, children, title, class: extra }: { tone?: Tone; icon?: LucideIcon; children: ComponentChildren; title?: string; class?: string }) {
  return (
    <span class={cx('chip', `chip--${tone}`, extra)} title={title}>
      {Icon ? <Icon size={12} /> : null}
      {children}
    </span>
  );
}

/** A chip that toggles a filter. Use a row of them inside role="group". */
export function FilterChip({ pressed, onClick, count, children }: { pressed: boolean; onClick: () => void; count?: number; children: ComponentChildren }) {
  return (
    <button type="button" class="chip chip--filter" aria-pressed={pressed} onClick={onClick}>
      {children}
      {count !== undefined && <span class="chip__count num">{count}</span>}
    </button>
  );
}

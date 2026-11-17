import type { LucideIcon } from 'lucide-preact';
import type { ComponentChildren } from 'preact';
import { cx } from './cx';

export interface EmptyStateProps {
  icon?: LucideIcon;
  title: string;
  /** Why it is empty, and what to do about it. */
  children?: ComponentChildren;
  /** Buttons or links: the way out of the empty state. */
  action?: ComponentChildren;
  /** Less padding, for an empty part of a page rather than an empty page. */
  compact?: boolean;
  tone?: 'neutral' | 'critical';
}

/** What a page or a section shows when it has nothing to show. */
export function EmptyState({ icon: Icon, title, children, action, compact, tone = 'neutral' }: EmptyStateProps) {
  return (
    <div class={cx('empty', compact && 'empty--compact', tone === 'critical' && 'empty--critical')}>
      {Icon ? (
        <span class="empty__icon">
          <Icon size={20} />
        </span>
      ) : null}
      <h2 class="empty__title">{title}</h2>
      {children != null && <p class="empty__body">{children}</p>}
      {action != null && <div class="empty__action">{action}</div>}
    </div>
  );
}

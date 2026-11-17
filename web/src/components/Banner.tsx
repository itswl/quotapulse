import { Info, OctagonAlert, TriangleAlert, type LucideIcon } from 'lucide-preact';
import type { ComponentChildren } from 'preact';
import { cx } from './cx';

const ICONS: Record<string, LucideIcon> = { info: Info, warning: TriangleAlert, critical: OctagonAlert };

export interface BannerProps {
  tone?: 'info' | 'warning' | 'critical';
  icon?: LucideIcon;
  /** A link or button at the end. */
  action?: ComponentChildren;
  /** Thinner, for a standing notice at the top of the app. */
  slim?: boolean;
  class?: string;
  children: ComponentChildren;
}

/** A standing notice: something about the whole page that stays true until
 *  it is dealt with. For a passing confirmation, use toast(). */
export function Banner({ tone = 'info', icon, action, slim, class: extra, children }: BannerProps) {
  const Icon = icon ?? ICONS[tone] ?? Info;
  return (
    <div class={cx('banner', `banner--${tone}`, slim && 'banner--slim', extra)} role={tone === 'critical' ? 'alert' : 'status'}>
      <Icon class="banner__icon" />
      <div class="banner__body">{children}</div>
      {action != null && <div class="banner__action">{action}</div>}
    </div>
  );
}

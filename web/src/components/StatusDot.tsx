import { CircleCheck, CirclePause, Clock, Info, OctagonAlert, TriangleAlert, Unplug, type LucideIcon } from 'lucide-preact';
import type { MeterStatus, Severity } from '../api/types';
import { t, type TKey } from '../i18n';
import { cx } from './cx';

/** Every state something can be in: a meter's status or an incident's severity. */
export type Status = MeterStatus | Severity;

const LOOK: Record<Status, { icon: LucideIcon; label: TKey; tone: string }> = {
  ok: { icon: CircleCheck, label: 'common.status.ok', tone: 'ok' },
  warning: { icon: TriangleAlert, label: 'common.status.warning', tone: 'warning' },
  critical: { icon: OctagonAlert, label: 'common.status.critical', tone: 'critical' },
  failing: { icon: Unplug, label: 'common.status.failing', tone: 'warning' },
  pending: { icon: Clock, label: 'common.status.pending', tone: 'neutral' },
  paused: { icon: CirclePause, label: 'common.status.paused', tone: 'neutral' },
  info: { icon: Info, label: 'common.status.info', tone: 'info' },
};

export function statusIcon(status: Status): LucideIcon {
  return LOOK[status].icon;
}

export function statusLabel(status: Status): string {
  return t(LOOK[status].label);
}

/** The tone a status is drawn in: ok, warning, critical, info or neutral. */
export function statusTone(status: Status): string {
  return LOOK[status].tone;
}

export interface StatusDotProps {
  status: Status;
  /** Replaces the status word: "Low balance" instead of "Critical". */
  label?: string;
  /** Show the icon alone. The word is still there for a screen reader, so
   *  the state is never carried by colour only. */
  iconOnly?: boolean;
  size?: number;
  class?: string;
}

/** A state as an icon and a word. Colour reinforces it; it never stands alone. */
export function StatusDot({ status, label, iconOnly, size = 14, class: extra }: StatusDotProps) {
  const look = LOOK[status];
  const Icon = look.icon;
  const text = label ?? t(look.label);
  return (
    <span class={cx('status', `status--${look.tone}`, extra)} title={iconOnly ? text : undefined}>
      <Icon size={size} />
      <span class={iconOnly ? 'sr-only' : 'status__label'}>{text}</span>
    </span>
  );
}

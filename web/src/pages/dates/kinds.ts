// The six kinds of date. A kind only changes wording and the icon; every
// kind follows the same rules.
import { Cake, CalendarClock, CreditCard, Heart, Hourglass, PartyPopper, type LucideIcon } from 'lucide-preact';
import type { ReminderKind } from '../../api/types';
import { t } from '../../i18n';

/** In the server's display order (core.ReminderKinds). */
export const KINDS: readonly ReminderKind[] = ['renewal', 'birthday', 'anniversary', 'holiday', 'expiry', 'custom'];

/** The same icons "Coming up" on the overview uses. */
export const KIND_ICON: Record<ReminderKind, LucideIcon> = {
  renewal: CreditCard,
  birthday: Cake,
  anniversary: Heart,
  holiday: PartyPopper,
  expiry: Hourglass,
  custom: CalendarClock,
};

export function isKind(value: string | null | undefined): value is ReminderKind {
  return (KINDS as readonly string[]).includes(value ?? '');
}

/** "Birthday", "Renewal" … and "Other" for a date of no particular kind. */
export function kindLabel(kind: ReminderKind): string {
  return kind === 'custom' ? t('dates.kind.custom') : t(`common.reminder.${kind}`);
}

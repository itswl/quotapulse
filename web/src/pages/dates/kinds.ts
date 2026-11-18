// The six kinds of date. A kind only changes wording and the icon; every
// kind follows the same rules.
import { Cake, CalendarClock, CreditCard, Heart, Hourglass, PartyPopper, type LucideIcon } from 'lucide-preact';
import type { ReminderKind, ReminderView } from '../../api/types';
import { t } from '../../i18n';

/** In the server's display order (core.ReminderKinds). */
export const KINDS: readonly ReminderKind[] = ['renewal', 'birthday', 'anniversary', 'holiday', 'expiry', 'custom'];

/** One icon per kind, the same wherever a date is shown. */
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

/** "30th" — English only; Chinese puts the number in the sentence. */
function ordinal(n: number): string {
  const tail = n % 100;
  const suffix = tail >= 11 && tail <= 13 ? 'th' : (['th', 'st', 'nd', 'rd'][n % 10] ?? 'th');
  return `${n}${suffix}`;
}

/** What kind of date it is, with its count when it has one: "30th birthday". */
export function reminderKindLabel(reminder: ReminderView): string {
  if (reminder.kind === 'birthday' && reminder.ordinal > 0) {
    return t('common.reminder.birthdayNth', { n: reminder.ordinal, nth: ordinal(reminder.ordinal) });
  }
  if (reminder.kind === 'anniversary' && reminder.ordinal > 0) {
    return t('common.reminder.anniversaryNth', { n: reminder.ordinal, nth: ordinal(reminder.ordinal) });
  }
  return kindLabel(reminder.kind);
}

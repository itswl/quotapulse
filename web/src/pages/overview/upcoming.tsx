// "Coming up" on the overview: the next dates as a compact timeline.
import { Cake, CalendarClock, Check, CreditCard, Heart, Hourglass, PartyPopper, type LucideIcon } from 'lucide-preact';
import { useState } from 'preact/hooks';
import { completeReminder, reopenReminder } from '../../api/endpoints';
import type { ReminderKind, ReminderView } from '../../api/types';
import { Amount, Button, Card, Chip, cx, toast } from '../../components';
import { errorText } from '../../errors';
import { formatDay, formatUntil, monthName, parseDay, weekdayName, weekdayOf } from '../../format';
import { lang, t } from '../../i18n';

const KIND_ICON: Record<ReminderKind, LucideIcon> = {
  renewal: CreditCard,
  birthday: Cake,
  anniversary: Heart,
  holiday: PartyPopper,
  expiry: Hourglass,
  custom: CalendarClock,
};

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
  return t(`common.reminder.${reminder.kind}`);
}

export function Upcoming({ reminders }: { reminders: ReminderView[] }) {
  return (
    <Card flush>
      <ol class="ov-dates">
        {reminders.map((reminder) => (
          <UpcomingRow key={reminder.id} reminder={reminder} />
        ))}
      </ol>
    </Card>
  );
}

function UpcomingRow({ reminder }: { reminder: ReminderView }) {
  const [done, setDone] = useState(false);
  if (!reminder.next || reminder.days === null) return null;
  const date = parseDay(reminder.next);
  const Icon = KIND_ICON[reminder.kind] ?? CalendarClock;
  const near = reminder.state === 'soon' || reminder.state === 'today';
  const details = [reminderKindLabel(reminder)];
  if (reminder.lunar) details.push(t('common.date.lunar', { label: reminder.lunar.label }));

  const markDone = async () => {
    setDone(true);
    try {
      await completeReminder(reminder.id);
      toast(t('overview.upcoming.doneToast', { title: reminder.title }), {
        action: {
          label: t('common.action.undo'),
          run: () => {
            reopenReminder(reminder.id).catch((cause: unknown) => toast.error(errorText(cause)));
          },
        },
      });
    } catch (cause) {
      toast.error(errorText(cause));
    } finally {
      // The refreshed overview shows the reminder at its next occurrence.
      setDone(false);
    }
  };

  return (
    <li class={cx('ov-date', done && 'ov-date--done')}>
      <time class="ov-date__block" dateTime={reminder.next} title={formatDay(reminder.next, { weekday: true, year: -1 })}>
        <span class="ov-date__day num">{date.day}</span>
        <span class="ov-date__month">
          {monthName(date.month)}
          {lang.value === 'zh' ? ' ' : ' · '}
          {weekdayName(weekdayOf(reminder.next))}
        </span>
      </time>
      <div class="ov-date__text">
        <p class="ov-date__title">
          <Icon class="ov-date__icon" size={15} />
          <span class="truncate">{reminder.title}</span>
        </p>
        <p class="ov-date__detail truncate">{details.join(' · ')}</p>
      </div>
      {/* The cells below are always there, even when empty: the rows share
          one grid, and a missing cell would shift the ones after it. */}
      <span class="ov-date__amount">
        {reminder.amount !== null && (
          <>
            <span class="sr-only">{t('overview.upcoming.amount')} </span>
            <Amount value={reminder.amount} unit={reminder.currency} />
          </>
        )}
      </span>
      <Chip tone={near ? 'accent' : 'neutral'} class="ov-date__when">
        {formatUntil(reminder.days)}
      </Chip>
      <span class="ov-date__action">
        {near && (
          <Button size="sm" variant="ghost" icon={Check} disabled={done} aria-label={t('overview.upcoming.done', { title: reminder.title })} onClick={markDone}>
            {t('common.action.done')}
          </Button>
        )}
      </span>
    </li>
  );
}

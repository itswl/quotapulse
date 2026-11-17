// One date in the list: the day it lands on this time, what it is, and what
// can be done about it. The date block speaks the same visual language as
// "Coming up" on the overview.
import { AlarmClock, Bell, BellOff, BellRing, CalendarClock, Check, CirclePause, Pencil, Power, Trash2, Undo2 } from 'lucide-preact';
import { completeReminder, reopenReminder, unsnoozeReminder, updateReminder } from '../../api/endpoints';
import type { Day, ReminderView } from '../../api/types';
import { Amount, Button, Chip, cx, IconButton, Menu, toast, type MenuEntry } from '../../components';
import { errorText } from '../../errors';
import { DASH, formatDay, formatUntil, monthName, parseDay, weekdayName, weekdayOf } from '../../format';
import { useAction } from '../../hooks';
import { lang, t } from '../../i18n';
import { putReminder } from './changes';
import { KIND_ICON, kindLabel } from './kinds';
import { leadsText, ordinalText, resumeDay, ruleText } from './words';

export interface DateRowProps {
  reminder: ReminderView;
  /** Today, by the server's clock and in its zone. */
  today: Day;
  onEdit: (reminder: ReminderView) => void;
  onSnooze: (reminder: ReminderView) => void;
  onDelete: (reminder: ReminderView) => void;
}

export function DateRow({ reminder, today, onEdit, onSnooze, onDelete }: DateRowProps) {
  const { id, title } = reminder;
  const thisYear = parseDay(today).year;
  const short = (day: Day) => formatDay(day, { year: thisYear });

  // One action at a time; each reports its own failure as a toast. The row
  // may have moved to another month by the time an undo is asked for, so
  // undoing never touches the row's own state.
  const act = useAction(async (work: () => Promise<ReminderView>, done: (next: ReminderView) => string, undo?: () => Promise<ReminderView>) => {
    try {
      const next = await work();
      putReminder(next);
      const takeBack = () => {
        undo?.()
          .then(putReminder)
          .catch((cause: unknown) => toast.error(errorText(cause)));
      };
      toast(done(next), undo ? { action: { label: t('common.action.undo'), run: takeBack } } : {});
    } catch (cause) {
      toast.error(errorText(cause));
    }
  });

  const on = reminder.enabled;
  const Icon = KIND_ICON[reminder.kind] ?? CalendarClock;
  // A one-off with nothing ahead still has its day to show.
  const shown = reminder.next ?? reminder.rule.date ?? null;
  const near = reminder.state === 'soon' || reminder.state === 'today';
  const due = near || reminder.state === 'snoozed';
  const years = ordinalText(reminder.kind, reminder.ordinal);
  // A snooze is worth showing for as long as it holds anything back.
  const snoozedTo = on && reminder.next && reminder.snoozed_until && reminder.snoozed_until >= today ? resumeDay(reminder.snoozed_until) : null;
  // The occurrence that was marked done: one that is being skipped, or a
  // one-off, which has nothing after it.
  const handled = reminder.handled ?? (!reminder.next && reminder.done_through && reminder.done_through >= today ? reminder.done_through : null);

  const markDone = () => {
    const day = reminder.next;
    if (!day) return;
    void act.run(
      () => completeReminder(id),
      (next) => (next.next ? t('dates.toast.done', { title, day: short(day), next: short(next.next) }) : t('dates.toast.doneLast', { title })),
      () => reopenReminder(id),
    );
  };
  const undoDone = () =>
    void act.run(
      () => reopenReminder(id),
      () => t('dates.toast.undone', { title }),
    );
  const wake = () =>
    void act.run(
      () => unsnoozeReminder(id),
      () => t('dates.toast.woke', { title }),
    );
  const toggle = () =>
    void act.run(
      () => updateReminder(id, { enabled: !on }),
      (next) => t(next.enabled ? 'dates.toast.on' : 'dates.toast.off', { title }),
    );

  const entries: MenuEntry[] = [];
  if (on && reminder.next) entries.push({ label: t('dates.menu.done', { day: short(reminder.next) }), icon: Check, onSelect: markDone, disabled: act.busy });
  if (handled) entries.push({ label: t('dates.menu.undo', { day: short(handled) }), icon: Undo2, onSelect: undoDone, disabled: act.busy });
  // Snoozing means "remind me again on a later day, no later than the day
  // itself": there has to be a day in between.
  if (on && reminder.next && reminder.next > today) entries.push({ label: t('dates.menu.snooze'), icon: AlarmClock, onSelect: () => onSnooze(reminder) });
  if (snoozedTo) entries.push({ label: t('dates.menu.wake'), icon: BellRing, onSelect: wake, disabled: act.busy });
  if (entries.length > 0) entries.push({ separator: true });
  entries.push(
    { label: t('common.action.edit'), icon: Pencil, onSelect: () => onEdit(reminder) },
    { label: on ? t('dates.menu.turnOff') : t('dates.menu.turnOn'), icon: Power, onSelect: toggle, disabled: act.busy },
    { separator: true },
    { label: t('common.action.delete'), icon: Trash2, danger: true, onSelect: () => onDelete(reminder) },
  );

  return (
    <li class={cx('dates-row', !on && 'dates-row--off', !reminder.next && 'dates-row--over')}>
      {shown ? (
        <time class="dates-row__block" dateTime={shown} title={formatDay(shown, { weekday: true, year: -1 })}>
          <span class="dates-row__day num">{parseDay(shown).day}</span>
          <span class="dates-row__month">
            {monthName(parseDay(shown).month)}
            {lang.value === 'zh' ? ' ' : ' · '}
            {weekdayName(weekdayOf(shown))}
          </span>
          {/* For a lunar rule the block is the Gregorian day it lands on this
              time, and this line is the lunar day that put it there. */}
          {reminder.lunar && <span class="dates-row__lunar">{t('common.date.lunar', { label: reminder.lunar.label })}</span>}
          {!reminder.next && <span class="dates-row__lunar num">{parseDay(shown).year}</span>}
        </time>
      ) : (
        <span class="dates-row__block">
          <span class="dates-row__day num">{DASH}</span>
        </span>
      )}

      <div class="dates-row__text">
        <p class="dates-row__title">
          <Icon class="dates-row__icon" size={15} />
          <span class="sr-only">{kindLabel(reminder.kind)}</span>
          <span class="dates-row__name">{title}</span>
          {years && <span class="dates-row__years">{years}</span>}
        </p>
        <p class="dates-row__detail">
          <span>{ruleText(reminder.rule)}</span>
          <span class="dates-row__leads">
            <Bell size={12} />
            <span class="sr-only">{t('dates.lead.label')}</span>
            {leadsText(reminder.lead_days)}
          </span>
          {reminder.group && (
            <span>
              <span class="sr-only">{t('dates.row.group')} </span>
              {reminder.group}
            </span>
          )}
        </p>
        {reminder.notes && (
          <p class="dates-row__notes">
            <span class="sr-only">{t('dates.row.notes')} </span>
            {reminder.notes}
          </p>
        )}
        {(snoozedTo || !on || handled) && (
          <p class="dates-row__marks">
            {snoozedTo && (
              <span class="dates-mark">
                <BellOff size={12} />
                {t('dates.mark.snoozed', { day: short(snoozedTo) })}
              </span>
            )}
            {!on && (
              <span class="dates-mark">
                <CirclePause size={12} />
                {t('dates.mark.off')}
              </span>
            )}
            {handled && (
              <span class="dates-mark">
                <Check size={12} />
                {t('dates.mark.handled', { day: short(handled) })}
                <button type="button" class="dates-mark__undo" aria-label={t('dates.mark.undo', { title })} disabled={act.busy} onClick={undoDone}>
                  {t('common.action.undo')}
                </button>
              </span>
            )}
          </p>
        )}
      </div>

      {/* The cells below are always there, even when empty: the rows of the
          whole list share one grid, and a missing cell would shift the rest. */}
      <span class="dates-row__amount">
        {reminder.amount !== null && (
          <>
            <span class="sr-only">{t('dates.row.amount')} </span>
            <Amount value={reminder.amount} unit={reminder.currency} />
          </>
        )}
      </span>
      <span class="dates-row__when">{reminder.next && reminder.days !== null && <Chip tone={near ? 'accent' : 'neutral'}>{formatUntil(reminder.days)}</Chip>}</span>
      <span class="dates-row__done">
        {due && on && reminder.next && (
          <Button size="sm" variant="ghost" icon={Check} busy={act.busy} aria-label={t('dates.row.done', { title, day: short(reminder.next) })} onClick={markDone}>
            {t('common.action.done')}
          </Button>
        )}
      </span>
      <span class="dates-row__more">
        <IconButton class="dates-row__edit" icon={Pencil} label={t('dates.row.edit', { title })} onClick={() => onEdit(reminder)} />
        <Menu label={t('dates.menu.label', { title })} entries={entries} />
      </span>
    </li>
  );
}

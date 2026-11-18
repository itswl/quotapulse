// One date, two ways. In a list, a row: the day it lands on, what it is, and
// how far off. Opened, everything about it — how it repeats, when it
// reminds, the occurrences after this one — and, in plain sight, what can be
// done to it: mark this time done, snooze, edit, turn off, delete.
import { AlarmClock, BellRing, CalendarClock, Check, Pencil, Power, Trash2, Undo2, X } from 'lucide-preact';
import type { ComponentChildren } from 'preact';
import { completeReminder, reopenReminder, unsnoozeReminder, updateReminder } from '../../api/endpoints';
import type { Day, ReminderView } from '../../api/types';
import { Amount, Button, Chip, cx, DayBlock, IconButton, toast } from '../../components';
import { errorText } from '../../errors';
import { formatAmount, formatDay, formatUntil, parseDay } from '../../format';
import { useAction } from '../../hooks';
import { t } from '../../i18n';
import { putReminder } from './changes';
import { KIND_ICON, reminderKindLabel } from './kinds';
import { leadsText, ordinalText, resumeDay, ruleText } from './words';

export interface DateHandlers {
  onEdit: (reminder: ReminderView) => void;
  onSnooze: (reminder: ReminderView) => void;
  onDelete: (reminder: ReminderView) => void;
}

/** Where a date stands today: near, snoozed, done for this time, off. */
function standing(reminder: ReminderView, today: Day) {
  const on = reminder.enabled;
  const near = reminder.state === 'soon' || reminder.state === 'today';
  // A snooze is worth showing for as long as it holds anything back.
  const snoozedTo = on && reminder.next && reminder.snoozed_until && reminder.snoozed_until >= today ? resumeDay(reminder.snoozed_until) : null;
  // The occurrence that was marked done: one that is being skipped, or a
  // one-off, which has nothing after it.
  const handled = reminder.handled ?? (!reminder.next && reminder.done_through && reminder.done_through >= today ? reminder.done_through : null);
  return { on, near, snoozedTo, handled };
}

// ---------- a row ----------

export function DateItem({ reminder, today, selected, onOpen }: { reminder: ReminderView; today: Day; selected: boolean; onOpen: () => void }) {
  const { on, near, snoozedTo, handled } = standing(reminder, today);
  const short = (day: Day) => formatDay(day, { year: parseDay(today).year });
  const Icon = KIND_ICON[reminder.kind] ?? CalendarClock;
  // A one-off with nothing ahead still has its day to show.
  const shown = reminder.next ?? reminder.rule.date ?? null;
  const years = ordinalText(reminder.kind, reminder.ordinal);
  const detail = [ruleText(reminder.rule)];
  if (reminder.amount !== null) detail.push(formatAmount(reminder.amount, reminder.currency));
  const marks: string[] = [];
  if (snoozedTo) marks.push(t('dates.mark.snoozed', { day: short(snoozedTo) }));
  if (!on) marks.push(t('dates.mark.off'));
  if (handled) marks.push(t('dates.mark.handled', { day: short(handled) }));
  return (
    <li>
      <button type="button" class={cx('date-item', selected && 'is-selected', !on && 'is-off', !reminder.next && 'is-over')} aria-current={selected ? 'true' : undefined} onClick={onOpen}>
        {shown ? <DayBlock day={shown} /> : <span class="dayblock" />}
        <span class="date-item__text">
          <span class="date-item__title">
            <Icon class="date-item__icon" size={15} />
            <span class="date-item__name">{reminder.title}</span>
            {years && <span class="date-item__years">{years}</span>}
          </span>
          <span class="date-item__detail">{detail.join(' · ')}</span>
          {marks.length > 0 && <span class="date-item__marks">{marks.join(' · ')}</span>}
        </span>
        {reminder.next && reminder.days !== null && (
          <Chip tone={near ? 'accent' : 'neutral'} class="date-item__when">
            {formatUntil(reminder.days)}
          </Chip>
        )}
      </button>
    </li>
  );
}

// ---------- opened ----------

function Fact({ label, children }: { label: string; children: ComponentChildren }) {
  return (
    <div class="date-detail__fact">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

export interface DateDetailProps extends DateHandlers {
  reminder: ReminderView;
  today: Day;
  /** Show its own title. Off inside a sheet, which has one. */
  heading?: boolean;
  onClose?: () => void;
}

export function DateDetail({ reminder, today, heading = true, onClose, onEdit, onSnooze, onDelete }: DateDetailProps) {
  const { id, title } = reminder;
  const { on, near, snoozedTo, handled } = standing(reminder, today);
  const short = (day: Day) => formatDay(day, { year: parseDay(today).year });
  const Icon = KIND_ICON[reminder.kind] ?? CalendarClock;
  const years = ordinalText(reminder.kind, reminder.ordinal);

  // One action at a time; each says what it did, and most can be taken back.
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

  const markDone = () => {
    const day = reminder.next;
    if (!day) return;
    void act.run(
      () => completeReminder(id),
      (next) => (next.next ? t('dates.toast.done', { title, day: short(day), next: short(next.next) }) : t('dates.toast.doneLast', { title })),
      () => reopenReminder(id),
    );
  };

  const kindLine = [reminderKindLabel(reminder)];
  if (reminder.group) kindLine.push(reminder.group);

  return (
    <article class="date-detail">
      {(heading || onClose) && (
        <header class="date-detail__head">
          <span class="date-detail__icon" aria-hidden="true">
            <Icon size={20} />
          </span>
          <div class="date-detail__titles">
            {heading && (
              <h2 class="date-detail__title">
                {title}
                {years && <span class="date-detail__years">{years}</span>}
              </h2>
            )}
            <p class="date-detail__kind">{kindLine.join(' · ')}</p>
          </div>
          {onClose && <IconButton icon={X} label={t('common.action.close')} onClick={onClose} />}
        </header>
      )}

      <div class={cx('date-detail__when', near && 'is-near')}>
        {reminder.next ? (
          <>
            <DayBlock day={reminder.next} class="date-detail__day" />
            {/* The block says the day; beside it only what it leaves out:
                the lunar day that put it there, and a year that is not this one. */}
            <span class="date-detail__date">
              {reminder.lunar && <span class="date-detail__lunar">{t('common.date.lunar', { label: reminder.lunar.label })}</span>}
              {parseDay(reminder.next).year !== parseDay(today).year && <span class="date-detail__year num">{parseDay(reminder.next).year}</span>}
            </span>
            {reminder.days !== null && (
              <Chip tone={near ? 'accent' : 'neutral'} class="date-detail__until">
                {formatUntil(reminder.days)}
              </Chip>
            )}
          </>
        ) : (
          <span class="date-detail__over">{t('dates.detail.over')}</span>
        )}
      </div>

      {(snoozedTo || !on || handled) && (
        <p class="date-detail__marks dotted">
          {snoozedTo && <span>{t('dates.mark.snoozed', { day: short(snoozedTo) })}</span>}
          {!on && <span>{t('dates.mark.off')}</span>}
          {handled && <span>{t('dates.mark.handled', { day: short(handled) })}</span>}
        </p>
      )}

      <div class="date-detail__actions">
        {on && reminder.next && (
          <Button variant="primary" icon={Check} busy={act.busy} onClick={markDone}>
            {t('dates.menu.done', { day: short(reminder.next) })}
          </Button>
        )}
        {handled && (
          <Button icon={Undo2} disabled={act.busy} onClick={() => void act.run(() => reopenReminder(id), () => t('dates.toast.undone', { title }))}>
            {t('dates.menu.undo', { day: short(handled) })}
          </Button>
        )}
        {/* Snoozing means "remind me again on a later day, no later than the
            day itself": there has to be a day in between. */}
        {on && reminder.next && reminder.next > today && (
          <Button icon={AlarmClock} onClick={() => onSnooze(reminder)}>
            {t('dates.menu.snooze')}
          </Button>
        )}
        {snoozedTo && (
          <Button icon={BellRing} disabled={act.busy} onClick={() => void act.run(() => unsnoozeReminder(id), () => t('dates.toast.woke', { title }))}>
            {t('dates.menu.wake')}
          </Button>
        )}
      </div>

      <dl class="date-detail__facts">
        <Fact label={t('dates.detail.repeats')}>{ruleText(reminder.rule)}</Fact>
        <Fact label={t('dates.lead.label')}>{leadsText(reminder.lead_days)}</Fact>
        {reminder.amount !== null && (
          <Fact label={t('dates.row.amount')}>
            <Amount value={reminder.amount} unit={reminder.currency} />
            {reminder.monthly_amount !== null && reminder.monthly_amount !== reminder.amount && (
              <span class="date-detail__per"> · {t('dates.detail.perMonth', { amount: formatAmount(reminder.monthly_amount, reminder.currency) })}</span>
            )}
          </Fact>
        )}
        {reminder.following.length > 0 && <Fact label={t('dates.detail.following')}>{reminder.following.map(short).join(t('common.list.separator'))}</Fact>}
        {reminder.notes && <Fact label={t('dates.row.notes')}>{reminder.notes}</Fact>}
      </dl>

      <div class="date-detail__manage">
        <Button variant="ghost" icon={Pencil} onClick={() => onEdit(reminder)}>
          {t('common.action.edit')}
        </Button>
        <Button
          variant="ghost"
          icon={Power}
          disabled={act.busy}
          onClick={() => void act.run(() => updateReminder(id, { enabled: !on }), (next) => t(next.enabled ? 'dates.toast.on' : 'dates.toast.off', { title }))}
        >
          {on ? t('dates.menu.turnOff') : t('dates.menu.turnOn')}
        </Button>
        <Button variant="ghost" icon={Trash2} class="date-detail__delete" onClick={() => onDelete(reminder)}>
          {t('common.action.delete')}
        </Button>
      </div>
    </article>
  );
}

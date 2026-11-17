// Snoozing a date: no reminders about it until a chosen day.
//
// The dialog asks for the day reminders come back — "remind me again on
// Thursday" — and offers the usual answers as one tap each. The API wants the
// last quiet day instead (PUT …/snooze { until }), the day before; words.ts
// converts. The latest day on offer is the date itself: snoozing past it
// would only mean missing it.
import type { JSX } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { snoozeReminder } from '../../api/endpoints';
import type { Day, ReminderView } from '../../api/types';
import { Button, Dialog, toast } from '../../components';
import { errorText } from '../../errors';
import { addDays, formatDay, parseDay } from '../../format';
import { useAction } from '../../hooks';
import { t } from '../../i18n';
import { putReminder } from './changes';
import { DateInput, Labeled } from './controls';
import { snoozeUntil } from './words';

export interface SnoozeDialogProps {
  /** The date to snooze; null keeps the dialog closed. */
  reminder: ReminderView | null;
  /** Today, by the server's clock and in its zone. */
  today: Day;
  onClose: () => void;
}

export function SnoozeDialog({ reminder, today, onClose }: SnoozeDialogProps) {
  const [busy, setBusy] = useState(false);
  const open = reminder !== null && reminder.next !== null;
  useEffect(() => {
    if (!open) setBusy(false);
  }, [open]);
  return (
    <Dialog open={open} onClose={onClose} title={t('dates.snooze.title', { title: reminder?.title ?? '' })} description={t('dates.snooze.body')} size="sm" busy={busy}>
      {reminder?.next && <SnoozeForm reminder={reminder} last={reminder.next} today={today} onBusy={setBusy} onDone={onClose} />}
    </Dialog>
  );
}

interface FormProps {
  reminder: ReminderView;
  /** The date itself: the last day reminders can come back on. */
  last: Day;
  today: Day;
  onBusy: (busy: boolean) => void;
  onDone: () => void;
}

function SnoozeForm({ reminder, last, today, onBusy, onDone }: FormProps) {
  const [picked, setPicked] = useState('');
  const [refused, setRefused] = useState(false);
  const tomorrow = addDays(today, 1);
  const short = (day: Day) => formatDay(day, { year: parseDay(today).year });

  const snooze = useAction(async (resume: Day) => {
    onBusy(true);
    try {
      putReminder(await snoozeReminder(reminder.id, snoozeUntil(resume)));
      toast(t('dates.toast.snoozed', { title: reminder.title, day: short(resume) }));
      onDone();
    } finally {
      onBusy(false);
    }
  });

  // The usual answers, as far as they fit before the date itself.
  const quick: { day: Day; label: string }[] = [];
  const offer = (day: Day, label: string) => {
    if (day <= last && !quick.some((each) => each.day === day)) quick.push({ day, label });
  };
  offer(tomorrow, t('dates.snooze.tomorrow'));
  offer(addDays(today, 3), t('dates.snooze.in3'));
  offer(addDays(today, 7), t('dates.snooze.week'));
  offer(last, t('dates.snooze.onDay'));

  const submit = (event: JSX.TargetedEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (picked < tomorrow || picked > last) {
      setRefused(true);
      return;
    }
    void snooze.run(picked);
  };

  return (
    <div class="dates-snooze">
      <div class="dates-snooze__quick" role="group" aria-label={t('dates.snooze.quick')}>
        {quick.map((each) => (
          <Button key={each.day} disabled={snooze.busy} onClick={() => void snooze.run(each.day)}>
            {each.label}
            <span class="dates-snooze__day">{short(each.day)}</span>
          </Button>
        ))}
      </div>
      {/* With the date tomorrow there is no other day to pick. */}
      {tomorrow < last && (
        <form onSubmit={submit} noValidate>
          <Labeled
            label={t('dates.snooze.pick')}
            help={t('dates.snooze.pick.help', { day: short(last) })}
            error={refused ? t('dates.snooze.pick.invalid', { day: short(last) }) : undefined}
          >
            {(ids) => (
              <div class="dates-snooze__pick">
                <DateInput
                  id={ids.id}
                  describedBy={ids.describedBy}
                  invalid={ids.invalid}
                  value={picked}
                  min={tomorrow}
                  max={last}
                  onInput={(value) => {
                    setPicked(value);
                    setRefused(false);
                  }}
                />
                <Button type="submit" variant="primary" busy={snooze.busy}>
                  {t('dates.snooze.submit')}
                </Button>
              </div>
            )}
          </Labeled>
        </form>
      )}
      {snooze.error !== undefined && (
        <p class="form-error" role="alert">
          {errorText(snooze.error)}
        </p>
      )}
    </div>
  );
}

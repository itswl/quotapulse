// Adding a date, and editing one: the same form, reached two ways.
//
//   <DateDialog open onClose={…} kind="birthday" />      a new date
//   <DateDialog open onClose={…} reminder={reminder} />  an existing one
//
// The form asks only for what the chosen repeat needs, and shows — live —
// where the rule lands (preview.tsx). Whether a rule is valid is the
// server's call: what has not been filled in is simply left out of the rule,
// and the server's own sentence says what is missing.
import { ChevronRight, Plus } from 'lucide-preact';
import type { JSX } from 'preact';
import { useEffect, useId, useMemo, useRef, useState } from 'preact/hooks';
import { createReminder, updateReminder } from '../../api/endpoints';
import type { ReminderKind, ReminderView } from '../../api/types';
import { Button, cx, Dialog, Field, FilterChip, Input, Segmented, Select, toast, type SelectOption } from '../../components';
import { errorText } from '../../errors';
import { dayOf, formatDay, parseDay, weekdayName } from '../../format';
import { useAction } from '../../hooks';
import { lang, t } from '../../i18n';
import { now } from '../../state/clock';
import { overview } from '../../state/overview';
import { reminders } from '../../state/reminders';
import { readStored, writeStored } from '../../storage';
import { putReminder } from './changes';
import { Choices, DateInput, Labeled, TextArea } from './controls';
import {
  buildInput,
  buildRule,
  draftOf,
  everyOf,
  KIND_DEFAULTS,
  lastDay,
  LEAD_CHOICES,
  MAX_LEAD,
  MAX_LEADS,
  newDraft,
  parseAmount,
  usesLunar,
  type Calendar,
  type Draft,
  type Freq,
} from './draft';
import { KIND_ICON, kindLabel, KINDS } from './kinds';
import { PreviewPanel, useRulePreview } from './preview';
import { lunarDayName, lunarMonthName, monthLong, weekdayLong } from './words';

export interface DateDialogProps {
  open: boolean;
  onClose: () => void;
  /** The date to edit. Without it, the dialog adds one. */
  reminder?: ReminderView | undefined;
  /** The kind a new date starts as. */
  kind?: ReminderKind | undefined;
}

export function DateDialog({ open, onClose, reminder, kind }: DateDialogProps) {
  const [busy, setBusy] = useState(false);
  const formId = useId();
  useEffect(() => {
    if (!open) setBusy(false);
  }, [open]);
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={reminder ? t('dates.form.edit.title') : t('dates.form.add.title')}
      class="dates-dialog"
      busy={busy}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>
            {t('common.action.cancel')}
          </Button>
          <Button type="submit" form={formId} variant="primary" busy={busy}>
            {reminder ? t('common.action.save') : t('dates.form.submit')}
          </Button>
        </>
      }
    >
      <DateForm id={formId} reminder={reminder} kind={kind ?? 'birthday'} onBusy={setBusy} onDone={onClose} />
    </Dialog>
  );
}

// Family birthdays tend to be kept on one calendar, so a new birthday starts
// on the one the last birthday was saved with.
const CALENDAR_KEY = 'qp.dates.calendar';

function calendarFor(kind: ReminderKind): Calendar {
  return kind === 'birthday' && readStored(CALENDAR_KEY) === 'lunar' ? 'lunar' : 'solar';
}

const COMMON_CURRENCIES = ['CNY', 'USD', 'EUR', 'HKD', 'JPY', 'GBP'];
const upTo = (count: number) => Array.from({ length: count }, (_, index) => index + 1);
const digits = (typed: string, length: number) => typed.replace(/\D/g, '').slice(0, length);

/** Splits a phrase with a number in it — "{n} weeks", 「每 {n} 周」 — around
 *  the number, so an input can stand where the number goes. */
function around(phrase: string): { before: string | undefined; after: string | undefined } {
  const [before = '', after = ''] = phrase.split('{n}');
  return { before: before.trim() || undefined, after: after.trim() || undefined };
}

type Problems = Partial<Record<'title' | 'leads' | 'amount' | 'currency', string>>;

interface FormProps {
  id: string;
  reminder: ReminderView | undefined;
  kind: ReminderKind;
  onBusy: (busy: boolean) => void;
  onDone: () => void;
}

function DateForm({ id, reminder, kind, onBusy, onDone }: FormProps) {
  const initial = useMemo(() => (reminder ? draftOf(reminder) : newDraft(kind, calendarFor(kind))), []);
  const [draft, setDraft] = useState(initial);
  const [problems, setProblems] = useState<Problems>({});
  const [customLead, setCustomLead] = useState('');
  // Lead days beyond the usual ones — the date's own, and any typed in during
  // this visit — stay on offer after being unticked.
  const [typedLeads, setTypedLeads] = useState<number[]>(initial.leads);
  const [moreOpen, setMoreOpen] = useState(initial.every !== '' || initial.since !== '');
  const [amountOpen, setAmountOpen] = useState(initial.amount !== '');
  // The defaults the user has overruled. A change of kind leaves those alone.
  const touched = useRef(new Set<'freq' | 'calendar' | 'leads'>());
  const form = useRef<HTMLFormElement>(null);

  const preview = useRulePreview(buildRule(draft));
  const today = dayOf(now.value);
  const base = overview.data.value?.spend.base ?? '';
  const lunar = usesLunar(draft);
  const every = everyOf(draft);
  const repeats = draft.freq !== 'once';
  const counted = draft.freq === 'yearly' && (draft.kind === 'birthday' || draft.kind === 'anniversary' || draft.since !== '');
  const priced = draft.kind === 'renewal' || draft.kind === 'expiry' || amountOpen;

  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => setDraft((current) => ({ ...current, [key]: value }));
  const settle = (key: keyof Problems) => setProblems((current) => (current[key] ? { ...current, [key]: undefined } : current));
  /** Keeps the day only while the month still has it. */
  const fitDay = (next: Draft): Draft => (Number(next.day) > lastDay(next) ? { ...next, day: '' } : next);

  const pickKind = (next: ReminderKind) =>
    setDraft((current) => {
      const changed = { ...current, kind: next };
      // Defaults belong to a new date, and only where the user has not chosen.
      if (!reminder) {
        if (!touched.current.has('freq')) changed.freq = KIND_DEFAULTS[next].freq;
        if (!touched.current.has('leads')) changed.leads = [...KIND_DEFAULTS[next].leads];
        if (!touched.current.has('calendar')) changed.calendar = calendarFor(next);
      }
      return fitDay(changed);
    });
  const pickFreq = (freq: Freq) => {
    touched.current.add('freq');
    setDraft((current) => fitDay({ ...current, freq }));
  };
  const pickCalendar = (calendar: Calendar) => {
    touched.current.add('calendar');
    setDraft((current) => fitDay({ ...current, calendar }));
  };
  const typeEvery = (typed: string) => {
    const count = digits(typed, 3);
    setDraft((current) => {
      const changed = { ...current, every: count };
      // Counting needs a day to count from. Start at the next occurrence as
      // it stands, so that "every 3 months" does not move the first one.
      if (Number(count) > 1 && current.start === '') changed.start = preview.view?.next ?? today;
      return changed;
    });
  };

  const toggleLead = (lead: number) => {
    touched.current.add('leads');
    settle('leads');
    setDraft((current) => ({ ...current, leads: current.leads.includes(lead) ? current.leads.filter((each) => each !== lead) : [...current.leads, lead] }));
  };
  const addLead = () => {
    if (customLead === '') return;
    const lead = Number(customLead);
    if (!Number.isInteger(lead) || lead < 0 || lead > MAX_LEAD) {
      setProblems((current) => ({ ...current, leads: t('dates.form.leads.range') }));
      return;
    }
    touched.current.add('leads');
    settle('leads');
    setTypedLeads((current) => [...current, lead]);
    setDraft((current) => (current.leads.includes(lead) ? current : { ...current, leads: [...current.leads, lead] }));
    setCustomLead('');
  };

  const save = useAction(async () => {
    const found: Problems = {};
    const amount = parseAmount(draft.amount);
    const currency = draft.currency.trim();
    if (draft.title.trim() === '') found.title = t('dates.form.required', { field: t('dates.form.title') });
    if (draft.leads.length === 0) found.leads = t('dates.form.leads.none');
    else if (draft.leads.length > MAX_LEADS) found.leads = t('dates.form.leads.tooMany');
    if (amount === 'invalid') found.amount = t('dates.form.amount.invalid');
    else if (amount !== null && amount > 0 && currency !== '' && !/^[A-Za-z]{3}$/.test(currency)) found.currency = t('dates.form.currency.invalid');
    setProblems(found);
    if (Object.keys(found).length > 0) {
      // Bring the first thing that needs fixing into view, wherever the form
      // was scrolled to.
      requestAnimationFrame(() => {
        const field = form.current?.querySelector('.field__error')?.closest('.field');
        field?.scrollIntoView({ block: 'center' });
        field?.querySelector<HTMLElement>('input, button')?.focus({ preventScroll: true });
      });
      return;
    }

    const input = buildInput(draft, reminder ? initial : undefined);
    if (reminder && Object.keys(input).length === 0) {
      onDone();
      return;
    }
    onBusy(true);
    try {
      const saved = reminder ? await updateReminder(reminder.id, input) : await createReminder(input);
      putReminder(saved);
      if (saved.kind === 'birthday' && saved.rule.freq !== 'once') writeStored(CALENDAR_KEY, saved.rule.calendar === 'lunar' ? 'lunar' : null);
      if (reminder) toast(t('dates.toast.saved'));
      else if (saved.next) toast(t('dates.toast.added', { title: saved.title, day: formatDay(saved.next, { year: parseDay(today).year }) }));
      else toast(t('dates.toast.addedPast', { title: saved.title }));
      onDone();
    } finally {
      onBusy(false);
    }
  });

  // A refused save says why at the foot of the form: make sure that is seen.
  useEffect(() => {
    if (save.error !== undefined) form.current?.querySelector('.form-error')?.scrollIntoView({ block: 'nearest' });
  }, [save.error]);

  const submit = (event: JSX.TargetedEvent<HTMLFormElement>) => {
    event.preventDefault();
    void save.run();
  };

  const kinds = KINDS.map((each) => ({ value: each, label: kindLabel(each), icon: KIND_ICON[each] }));
  const freqs: SelectOption<Freq>[] = [
    { value: 'once', label: t('dates.form.freq.once') },
    { value: 'weekly', label: t('dates.form.freq.weekly') },
    { value: 'monthly', label: t('dates.form.freq.monthly') },
    { value: 'yearly', label: t('dates.form.freq.yearly') },
  ];
  const calendars: SelectOption<Calendar>[] = [
    { value: 'solar', label: t('dates.form.calendar.solar') },
    { value: 'lunar', label: t('dates.form.calendar.lunar') },
  ];
  const weekdays = upTo(7).map((day) => ({ value: String(day), label: weekdayName(day), name: weekdayLong(day) }));
  // On the lunar calendar the pickers speak its language: 正月 … 腊月, 初一 … 三十.
  const months: SelectOption<string>[] = [
    { value: '', label: t('dates.form.month') },
    ...upTo(12).map((month) => ({ value: String(month), label: lunar ? t('dates.form.lunarMonth', { month: lunarMonthName(month) }) : monthLong(month) })),
  ];
  const days: SelectOption<string>[] = [
    { value: '', label: t('dates.form.day') },
    ...upTo(lastDay(draft)).map((day) => ({ value: String(day), label: lunar ? lunarDayName(day) : lang.value === 'zh' ? `${day}日` : String(day) })),
  ];
  // Some months are too short for the day that was picked.
  const day = Number(draft.day);
  const short = lunar ? day === 30 : draft.freq === 'yearly' ? draft.month === '2' && day === 29 : day > 28;

  const leads = [...new Set([...LEAD_CHOICES, ...draft.leads, ...typedLeads])].sort((a, b) => a - b);
  const groups = [...new Set((reminders.data.value ?? []).map((each) => each.group).filter(Boolean))].sort();
  const currencies = [...new Set([base, ...COMMON_CURRENCIES, ...(reminders.data.value ?? []).map((each) => each.currency)].filter(Boolean))];
  const everyWords = around(t(`dates.form.every.${draft.freq === 'once' ? 'yearly' : draft.freq}`));
  const leadWords = around(t('dates.form.leads.custom.phrase'));
  const inside = [counted && t(draft.kind === 'birthday' ? 'dates.form.since.birth' : 'dates.form.since.start'), t(`dates.form.more.${draft.freq === 'once' ? 'yearly' : draft.freq}`)];

  return (
    <form class="dates-form" id={id} ref={form} onSubmit={submit} noValidate>
      <Field label={t('dates.form.title')} error={problems.title}>
        <Input
          value={draft.title}
          onInput={(value) => {
            set('title', value);
            settle('title');
          }}
          placeholder={t('dates.form.title.placeholder')}
          maxLength={80}
          autoComplete="off"
          autoFocus={!reminder}
        />
      </Field>

      <Labeled group label={t('dates.form.kind')}>
        {(ids) => <Choices class="dates-kinds" labelledBy={ids.labelledBy} value={draft.kind} onChange={pickKind} options={kinds} />}
      </Labeled>

      <Field label={t('dates.form.repeat')}>
        <Segmented value={draft.freq} onChange={pickFreq} options={freqs} />
      </Field>

      {draft.freq === 'once' && (
        <Labeled label={t('dates.form.date')}>{(ids) => <DateInput id={ids.id} value={draft.date} onInput={(value) => set('date', value)} />}</Labeled>
      )}

      {draft.freq === 'weekly' && (
        <Labeled group label={t('dates.form.weekday')}>
          {(ids) => <Choices class="dates-weekdays" labelledBy={ids.labelledBy} value={draft.weekday} onChange={(value) => set('weekday', value)} options={weekdays} />}
        </Labeled>
      )}

      {(draft.freq === 'monthly' || draft.freq === 'yearly') && (
        <div class="dates-form__when">
          <Field label={t('dates.form.calendar')}>
            <Segmented value={draft.calendar} onChange={pickCalendar} options={calendars} />
          </Field>
          <Labeled group label={draft.freq === 'yearly' ? t('dates.form.date') : t('dates.form.monthDay')} help={short ? t('dates.form.day.short') : undefined}>
            {() => (
              <div class={cx('dates-form__selects', draft.freq === 'monthly' && 'dates-form__selects--one')}>
                {draft.freq === 'yearly' && (
                  <Select aria-label={t('dates.form.month')} value={draft.month} onChange={(month) => setDraft((current) => fitDay({ ...current, month }))} options={months} />
                )}
                <Select aria-label={t('dates.form.day')} value={draft.day} onChange={(value) => set('day', value)} options={days} />
              </div>
            )}
          </Labeled>
        </div>
      )}

      {repeats && (
        <details class="dates-more" open={moreOpen} onToggle={(event) => setMoreOpen(event.currentTarget.open)}>
          <summary>
            <ChevronRight class="dates-more__chevron" size={14} />
            {t('dates.form.more')}
            <span class="dates-more__inside">{inside.filter(Boolean).join(t('common.list.separator'))}</span>
          </summary>
          <div class="dates-more__body">
            {counted && (
              <Field label={t(draft.kind === 'birthday' ? 'dates.form.since.birth' : 'dates.form.since.start')} optional help={t('dates.form.since.help')}>
                <Input class="dates-form__short" mono inputMode="decimal" value={draft.since} onInput={(value) => set('since', digits(value, 4))} placeholder="1990" autoComplete="off" />
              </Field>
            )}
            <Field label={t('dates.form.every')} help={lunar ? t('dates.form.every.lunar') : t('dates.form.every.help')}>
              <Input
                class="dates-form__short"
                mono
                inputMode="decimal"
                value={lunar ? '' : draft.every}
                onInput={typeEvery}
                placeholder="1"
                prefix={everyWords.before}
                suffix={everyWords.after}
                disabled={lunar}
                autoComplete="off"
              />
            </Field>
            {every > 1 && (
              <Labeled label={t('dates.form.start')} help={t('dates.form.start.help')}>
                {(ids) => <DateInput id={ids.id} describedBy={ids.describedBy} value={draft.start} onInput={(value) => set('start', value)} />}
              </Labeled>
            )}
          </div>
        </details>
      )}

      <PreviewPanel preview={preview} kind={draft.kind} />

      <Labeled group label={t('dates.form.leads')} error={problems.leads}>
        {() => (
          <div class="dates-leads">
            {leads.map((lead) => (
              <FilterChip key={lead} pressed={draft.leads.includes(lead)} onClick={() => toggleLead(lead)}>
                {lead === 0 ? t('dates.form.leads.onDay') : t('dates.form.leads.before', { n: lead })}
              </FilterChip>
            ))}
            <span class="dates-leads__custom">
              <Input
                class="dates-leads__input"
                mono
                inputMode="decimal"
                value={customLead}
                onInput={(value) => setCustomLead(digits(value, 3))}
                aria-label={t('dates.form.leads.custom')}
                prefix={leadWords.before}
                suffix={leadWords.after}
                autoComplete="off"
                onKeyDown={(event) => {
                  // Enter adds the number; it must not send the whole form.
                  if (event.key !== 'Enter') return;
                  event.preventDefault();
                  addLead();
                }}
              />
              <Button size="sm" onClick={addLead} disabled={customLead === ''}>
                {t('common.action.add')}
              </Button>
            </span>
          </div>
        )}
      </Labeled>

      {priced ? (
        <div class="dates-form__row">
          <Field label={t('dates.form.amount')} optional help={t('dates.form.amount.help')} error={problems.amount}>
            <Input
              mono
              inputMode="decimal"
              value={draft.amount}
              onInput={(value) => {
                set('amount', value);
                settle('amount');
              }}
              placeholder="0.00"
              autoComplete="off"
            />
          </Field>
          <Field label={t('dates.form.currency')} help={base ? t('dates.form.currency.help', { base }) : undefined} error={problems.currency}>
            <Input
              mono
              value={draft.currency}
              onInput={(value) => {
                set('currency', value.toUpperCase().slice(0, 3));
                settle('currency');
              }}
              placeholder={base}
              list="dates-currencies"
              maxLength={3}
              autoComplete="off"
              autoCapitalize="characters"
              spellcheck={false}
            />
          </Field>
          <datalist id="dates-currencies">
            {currencies.map((each) => (
              <option key={each} value={each} />
            ))}
          </datalist>
        </div>
      ) : (
        <Button class="dates-form__reveal" variant="ghost" size="sm" icon={Plus} onClick={() => setAmountOpen(true)}>
          {t('dates.form.amount.add')}
        </Button>
      )}

      <Field label={t('dates.form.group')} optional help={t('dates.form.group.help')}>
        <Input value={draft.group} onInput={(value) => set('group', value)} list="dates-groups" maxLength={80} autoComplete="off" />
      </Field>
      <datalist id="dates-groups">
        {groups.map((each) => (
          <option key={each} value={each} />
        ))}
      </datalist>

      <Labeled label={t('dates.form.notes')} optional help={t('dates.form.notes.help')}>
        {(ids) => <TextArea id={ids.id} describedBy={ids.describedBy} value={draft.notes} onInput={(value) => set('notes', value)} maxLength={2000} />}
      </Labeled>

      {save.error !== undefined && (
        <p class="form-error" role="alert">
          {errorText(save.error)}
        </p>
      )}
    </form>
  );
}

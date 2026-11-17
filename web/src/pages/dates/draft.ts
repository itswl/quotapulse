// The add/edit form's own shape — what is in its inputs, as text — and the
// way from there to the API and back. Pure, so the tests can pin it.
import type { RecurRule, ReminderInput, ReminderKind, ReminderView } from '../../api/types';
import { weekdayOf } from '../../format';

export type Freq = RecurRule['freq'];
export type Calendar = 'solar' | 'lunar';

export interface Draft {
  title: string;
  kind: ReminderKind;
  freq: Freq;
  calendar: Calendar;
  /** Once: the day itself. */
  date: string;
  /** Every N: the day to count from. */
  start: string;
  /** "1" Monday … "7" Sunday; "" until one is picked. */
  weekday: string;
  month: string;
  day: string;
  /** Every N weeks, months or years; "" and "1" both mean every one. */
  every: string;
  /** The year it began: a birth year. */
  since: string;
  leads: number[];
  amount: string;
  currency: string;
  group: string;
  notes: string;
}

/** The lead days offered as chips; any other number can be typed. */
export const LEAD_CHOICES: readonly number[] = [0, 1, 3, 7, 14, 30];
/** The server's limits (internal/engine/admin.go). */
export const MAX_LEADS = 12;
export const MAX_LEAD = 366;

/** What a new date of each kind starts as. Only ever applied to fields the
 *  user has not touched. */
export const KIND_DEFAULTS: Record<ReminderKind, { freq: Freq; leads: readonly number[] }> = {
  renewal: { freq: 'monthly', leads: [3, 0] },
  birthday: { freq: 'yearly', leads: [3, 0] },
  anniversary: { freq: 'yearly', leads: [7, 0] },
  holiday: { freq: 'yearly', leads: [7, 0] },
  expiry: { freq: 'once', leads: [30, 7, 1] },
  custom: { freq: 'once', leads: [0] },
};

/** An empty form for a new date of a kind. */
export function newDraft(kind: ReminderKind, calendar: Calendar = 'solar'): Draft {
  const defaults = KIND_DEFAULTS[kind];
  return {
    title: '',
    kind,
    freq: defaults.freq,
    calendar,
    date: '',
    start: '',
    weekday: '',
    month: '',
    day: '',
    every: '',
    since: '',
    leads: [...defaults.leads],
    amount: '',
    currency: '',
    group: '',
    notes: '',
  };
}

const text = (value: number | undefined) => (value ? String(value) : '');

/** The form for an existing date. */
export function draftOf(reminder: ReminderView): Draft {
  const { rule } = reminder;
  const once = rule.freq === 'once';
  return {
    title: reminder.title,
    kind: reminder.kind,
    freq: rule.freq,
    calendar: rule.calendar === 'lunar' ? 'lunar' : 'solar',
    date: once ? (rule.date ?? '') : '',
    start: once ? '' : (rule.date ?? ''),
    // Every N weeks may leave the weekday to its start date; show which it is.
    weekday: text(rule.weekday || (rule.freq === 'weekly' && rule.date ? weekdayOf(rule.date) : 0)),
    month: text(rule.month),
    day: text(rule.day),
    every: rule.interval !== undefined && rule.interval > 1 ? String(rule.interval) : '',
    since: text(rule.since),
    leads: [...reminder.lead_days],
    amount: reminder.amount === null ? '' : String(reminder.amount),
    currency: reminder.currency,
    group: reminder.group,
    notes: reminder.notes,
  };
}

/** True when the rule is on the lunar calendar: only a monthly or a yearly
 *  one can be. */
export function usesLunar(draft: Draft): boolean {
  return draft.calendar === 'lunar' && (draft.freq === 'monthly' || draft.freq === 'yearly');
}

/** How many weeks, months or years lie between two occurrences. */
export function everyOf(draft: Draft): number {
  const every = Number(draft.every);
  return draft.freq !== 'once' && !usesLunar(draft) && Number.isInteger(every) && every > 1 ? every : 1;
}

const MONTH_DAYS = [31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];

/** The last day the day picker offers: 30 on the lunar calendar, the length
 *  of the month for a yearly date (February 29 included), 31 otherwise. */
export function lastDay(draft: Draft): number {
  if (usesLunar(draft)) return 30;
  if (draft.freq === 'yearly') return MONTH_DAYS[Number(draft.month) - 1] ?? 31;
  return 31;
}

/**
 * The rule as the API takes it, with only the fields its frequency uses.
 * What has not been filled in yet is left out: the server then says what is
 * missing, in the preview and on saving.
 */
export function buildRule(draft: Draft): RecurRule {
  const rule: RecurRule = { freq: draft.freq };
  const every = everyOf(draft);
  if (usesLunar(draft)) rule.calendar = 'lunar';
  if (draft.freq === 'once') {
    if (draft.date) rule.date = draft.date;
    return rule;
  }
  if (every > 1 && draft.start) rule.date = draft.start;
  if (draft.freq === 'weekly' && draft.weekday) rule.weekday = Number(draft.weekday);
  if (draft.freq === 'yearly' && draft.month) rule.month = Number(draft.month);
  if (draft.freq !== 'weekly' && draft.day) rule.day = Number(draft.day);
  if (every > 1) rule.interval = every;
  const since = Number(draft.since);
  if (draft.freq === 'yearly' && Number.isInteger(since) && since > 0) rule.since = since;
  return rule;
}

/** Reads an amount someone typed: "", "1,200.5", " 68 ". */
export function parseAmount(input: string): number | null | 'invalid' {
  const cleaned = input.replace(/[,，\s]/g, '');
  if (cleaned === '') return null;
  const value = Number(cleaned);
  return Number.isFinite(value) && value >= 0 ? value : 'invalid';
}

/** Lead days as the server keeps them: each once, the widest first. */
export function cleanLeads(leads: readonly number[]): number[] {
  return [...new Set(leads)].sort((a, b) => b - a);
}

function wholeInput(draft: Draft): Required<Omit<ReminderInput, 'enabled'>> {
  const amount = parseAmount(draft.amount);
  return {
    title: draft.title.trim(),
    kind: draft.kind,
    rule: buildRule(draft),
    lead_days: cleanLeads(draft.leads),
    // 0 clears the amount.
    amount: typeof amount === 'number' ? amount : 0,
    currency: draft.currency.trim().toUpperCase(),
    group: draft.group.trim(),
    notes: draft.notes.trim(),
  };
}

/**
 * The request body for a save. A new date sends everything the form owns.
 * An edit sends only what changed since the form opened — so a rule that was
 * not touched is not sent back, and a "done" mark made against it survives —
 * and the rule, when sent, goes whole.
 */
export function buildInput(draft: Draft, original?: Draft): ReminderInput {
  const now = wholeInput(draft);
  if (!original) return now;
  const was = wholeInput(original);
  const changed: Record<string, unknown> = {};
  for (const key of Object.keys(now) as (keyof typeof now)[]) {
    if (JSON.stringify(now[key]) !== JSON.stringify(was[key])) changed[key] = now[key];
  }
  return changed as ReminderInput;
}

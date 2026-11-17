// The parts of the Dates page that take some composing: a rule read out in
// words ("every year on the 9th day of the 10th lunar month" /
// 「每年农历十月初九」), the days a date reminds on, and how the list is
// grouped and summed up. All of it is pure — arguments and the current
// language in, text or numbers out — so the tests can pin it.
import type { Day, RecurRule, ReminderKind, ReminderView } from '../../api/types';
import { addDays, formatDay, formatMonth, makeDay, parseDay, weekdayOf } from '../../format';
import { lang, t, type TKey } from '../../i18n';

// ---------- the lunar calendar's own names ----------

const DIGITS = ['一', '二', '三', '四', '五', '六', '七', '八', '九', '十'] as const;
const LUNAR_MONTHS = ['正月', '二月', '三月', '四月', '五月', '六月', '七月', '八月', '九月', '十月', '冬月', '腊月'] as const;

/** "1st", "22nd", "13th". */
export function ordinal(n: number): string {
  const tail = n % 100;
  const suffix = tail >= 11 && tail <= 13 ? 'th' : (['th', 'st', 'nd', 'rd'][n % 10] ?? 'th');
  return `${n}${suffix}`;
}

/** A lunar month by its traditional name — 正月, 冬月, 腊月 — and a plain
 *  ordinal in English: "10th". */
export function lunarMonthName(month: number): string {
  if (lang.value !== 'zh') return ordinal(month);
  return LUNAR_MONTHS[month - 1] ?? `${month}月`;
}

/** A day of a lunar month: 初一 … 初十, 十一 … 二十, 廿一 … 廿九, 三十 — and a
 *  plain ordinal in English: "9th". */
export function lunarDayName(day: number): string {
  if (lang.value !== 'zh') return ordinal(day);
  if (!Number.isInteger(day) || day < 1 || day > 30) return String(day);
  if (day <= 10) return '初' + DIGITS[day - 1]!;
  if (day < 20) return '十' + DIGITS[day - 11]!;
  if (day === 20) return '二十';
  if (day < 30) return '廿' + DIGITS[day - 21]!;
  return '三十';
}

// ---------- months and weekdays, written out ----------

const WEEKDAYS: readonly TKey[] = [
  'dates.weekday.1',
  'dates.weekday.2',
  'dates.weekday.3',
  'dates.weekday.4',
  'dates.weekday.5',
  'dates.weekday.6',
  'dates.weekday.7',
];

/** "Friday" / "周五" for an ISO weekday (1 Monday … 7 Sunday). */
export function weekdayLong(weekday: number): string {
  return t(WEEKDAYS[(((weekday - 1) % 7) + 7) % 7]!);
}

const longMonth = new Intl.DateTimeFormat('en-US', { month: 'long', timeZone: 'UTC' });
const longDay = new Intl.DateTimeFormat('en-US', { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric', timeZone: 'UTC' });

/** "November" / "11月" */
export function monthLong(month: number): string {
  if (lang.value === 'zh') return `${month}月`;
  return longMonth.format(Date.UTC(2000, month - 1, 1));
}

/** A day in full, for the one place that has room for it:
 *  "Thursday, December 24, 2026" / "2026年12月24日 周四". */
export function formatLongDay(day: Day): string {
  if (lang.value === 'zh') return formatDay(day, { weekday: true, year: -1 });
  const p = parseDay(day);
  return longDay.format(Date.UTC(p.year, p.month - 1, p.day));
}

// ---------- a rule, in words ----------

/**
 * Reads a rule out: 「每年农历十月初九」, 「每月 19 日」, 「每周五」,
 * 「每 3 个月的 15 日」, 「2031年5月1日（仅一次）」 — or "every year on the 9th
 * day of the 10th lunar month", "every month on the 19th", "every Friday".
 */
export function ruleText(rule: RecurRule): string {
  const every = rule.interval !== undefined && rule.interval > 1 ? rule.interval : 1;
  const lunar = rule.calendar === 'lunar';
  const day = rule.day ?? 0;
  switch (rule.freq) {
    case 'once':
      return t('dates.rule.once', { date: rule.date ? formatDay(rule.date, { year: -1 }) : '' });
    case 'weekly': {
      // Every N weeks may leave the weekday to its start date.
      const weekday = weekdayLong(rule.weekday || (rule.date ? weekdayOf(rule.date) : 1));
      return every > 1 ? t('dates.rule.weeklyN', { n: every, weekday }) : t('dates.rule.weekly', { weekday });
    }
    case 'monthly': {
      if (lunar) return t('dates.rule.monthlyLunar', { day: lunarDayName(day) });
      const nth = lang.value === 'zh' ? String(day) : ordinal(day);
      return every > 1 ? t('dates.rule.monthlyN', { n: every, day: nth }) : t('dates.rule.monthly', { day: nth });
    }
    case 'yearly': {
      const month = rule.month ?? 0;
      if (lunar) return t('dates.rule.yearlyLunar', { month: lunarMonthName(month), day: lunarDayName(day) });
      // Any leap year will do: only the month and the day are written.
      const date = formatDay(makeDay(2000, month, day));
      return every > 1 ? t('dates.rule.yearlyN', { n: every, date }) : t('dates.rule.yearly', { date });
    }
    default:
      return String(rule.freq);
  }
}

/** Which days a date reminds on: 「提前 3 天、当天」 / "3 days before, on the
 *  day". The widest lead comes first, as the server stores them. */
export function leadsText(leads: readonly number[]): string {
  const before = [...new Set(leads)].filter((lead) => lead > 0).sort((a, b) => b - a);
  const parts: string[] = [];
  if (before.length > 0) {
    const days = before.map((n) => t('dates.lead.days', { n }));
    const last = days.pop()!;
    const list = days.length > 0 ? days.join(t('common.list.separator')) + t('dates.lead.and') + last : last;
    parts.push(t('dates.lead.before', { list }));
  }
  if (leads.includes(0) || before.length === 0) parts.push(t('dates.lead.onDay'));
  return parts.join(t('common.list.separator'));
}

/** How many years it has been, for the kinds that count them: 「30 岁」 at a
 *  birthday, 「5 周年」 at an anniversary. Null when there is nothing to say. */
export function ordinalText(kind: ReminderKind, years: number): string | null {
  if (years <= 0) return null;
  if (kind === 'birthday') return t('dates.ordinal.birthday', { n: years, nth: ordinal(years) });
  if (kind === 'anniversary') return t('dates.ordinal.anniversary', { n: years, nth: ordinal(years) });
  return null;
}

// ---------- the list: groups, the summary, the search ----------

export interface MonthGroup {
  /** "YYYY-MM", or "" for the dates that have nothing ahead. */
  key: string;
  items: ReminderView[];
}

/** Groups dates by the month of their next occurrence, in calendar order,
 *  with the ones that have nothing ahead last. */
export function groupByMonth(list: readonly ReminderView[]): MonthGroup[] {
  const months = new Map<string, ReminderView[]>();
  const none: ReminderView[] = [];
  for (const item of list) {
    if (!item.next) {
      none.push(item);
      continue;
    }
    const key = item.next.slice(0, 7);
    const items = months.get(key);
    if (items) items.push(item);
    else months.set(key, [item]);
  }
  const groups = [...months].map(([key, items]) => ({ key, items })).sort((a, b) => (a.key < b.key ? -1 : 1));
  if (none.length > 0) groups.push({ key: '', items: none });
  return groups;
}

/** A group's heading: this year's months by name, later years with the
 *  year — "November", "January 2027". */
export function monthTitle(key: string, thisYear: number): string {
  if (key === '') return t('dates.group.none');
  const { year, month } = parseDay(`${key}-01`);
  return year === thisYear ? monthLong(month) : formatMonth(key);
}

export interface Summary {
  /** Dates whose next occurrence is at most 7 days away. */
  week: number;
  /** … and at most 30. */
  month: number;
  /** What the dates with an amount come to per month, one sum per currency:
   *  different currencies are never added together. */
  monthly: { currency: string; amount: number }[];
}

/** Sums the list up. Dates that are turned off count for nothing, as on the
 *  Spend page. */
export function summarise(list: readonly ReminderView[]): Summary {
  let week = 0;
  let month = 0;
  const sums = new Map<string, number>();
  for (const item of list) {
    if (!item.enabled) continue;
    if (item.days !== null && item.days <= 7) week += 1;
    if (item.days !== null && item.days <= 30) month += 1;
    if (item.monthly_amount !== null && item.currency !== '') {
      sums.set(item.currency, (sums.get(item.currency) ?? 0) + item.monthly_amount);
    }
  }
  const monthly = [...sums].map(([currency, amount]) => ({ currency, amount })).sort((a, b) => (a.currency < b.currency ? -1 : 1));
  return { week, month, monthly };
}

/** True when the title contains what was typed, whatever the case. */
export function matchesTitle(item: ReminderView, typed: string): boolean {
  const needle = typed.trim().toLowerCase();
  return needle === '' || item.title.toLowerCase().includes(needle);
}

/** The order the server lists dates in: soonest first, and those with
 *  nothing ahead last. Ties keep their order. */
export function soonestFirst(list: readonly ReminderView[]): ReminderView[] {
  const far = Number.MAX_SAFE_INTEGER;
  return list
    .map((item, index) => ({ item, index }))
    .sort((a, b) => (a.item.days ?? far) - (b.item.days ?? far) || a.index - b.index)
    .map((entry) => entry.item);
}

// ---------- snoozing ----------

// The API holds reminders back *through* `snoozed_until`: they come back the
// day after. People think of the day they come back — "remind me again on
// Thursday" — so that is the day the page shows and asks for.

/** The day reminders come back, for a stored `snoozed_until`. */
export function resumeDay(snoozedUntil: Day): Day {
  return addDays(snoozedUntil, 1);
}

/** The `until` to send so that reminders come back on a given day. */
export function snoozeUntil(resume: Day): Day {
  return addDays(resume, -1);
}

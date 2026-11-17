// How numbers, amounts, durations and dates are written — in the current
// language, and always in the user's configured time zone (state/clock), never
// the browser's.
//
// The rules mirror internal/i18n on the server, so a number in a card reads
// the same as the number in the incident sentence next to it:
//   money        1,234.56 CNY     two decimals, unit after
//   percent      38%  42.4%       at most one decimal
//   other units  923 calls        at most two decimals, trailing zeros dropped
//
// Functions that take `zone` or `nowMs` default to the live values; pass them
// only to pin a result, as the tests do.
import type { Day, Instant } from './api/types';
import { lang, t } from './i18n';
import { now, timezone } from './state/clock';

const DAY_MS = 86_400_000;
export const DASH = '—';

// ---------- numbers ----------

const numberFormats = new Map<string, Intl.NumberFormat>();

/** Formats with thousands separators and at most `decimals` decimals;
 *  `exact` keeps trailing zeros. */
export function formatNumber(value: number, decimals = 2, exact = false): string {
  if (!Number.isFinite(value)) return DASH;
  const key = `${decimals}:${exact}`;
  let format = numberFormats.get(key);
  if (!format) {
    format = new Intl.NumberFormat('en-US', {
      minimumFractionDigits: exact ? decimals : 0,
      maximumFractionDigits: decimals,
      signDisplay: 'negative',
    });
    numberFormats.set(key, format);
  }
  return format.format(value);
}

/** Reports whether a unit is a currency code: three capital letters. */
export function isCurrency(unit: string): boolean {
  return /^[A-Z]{3}$/.test(unit);
}

export interface AmountParts {
  number: string;
  /** Empty when there is nothing to show after the number. */
  unit: string;
  /** True when the unit hugs the number (38%), false when a space separates them. */
  tight: boolean;
}

/** Splits an amount into its number and its unit, for showing the unit
 *  smaller. formatAmount is the same thing as one string. */
export function amountParts(value: number, unit: string): AmountParts {
  if (unit === '%') return { number: formatNumber(value, 1), unit: '%', tight: true };
  if (isCurrency(unit)) return { number: formatNumber(value, 2, true), unit, tight: false };
  return { number: formatNumber(value, 2), unit, tight: false };
}

export function formatAmount(value: number, unit: string): string {
  const parts = amountParts(value, unit);
  if (parts.unit === '') return parts.number;
  return parts.number + (parts.tight ? '' : ' ') + parts.unit;
}

/** A share of 0-100 as "38%" or "42.4%". */
export function formatPercent(value: number): string {
  return formatNumber(value, 1) + '%';
}

/** A number short enough for an axis tick: written out below ten thousand,
 *  compact from there on (12.5K, 3M — or 1.25万, 3亿 in Chinese). */
export function compactNumber(value: number): string {
  if (!Number.isFinite(value)) return DASH;
  const size = Math.abs(value);
  if (size < 10_000) return formatNumber(value, size < 10 ? 2 : size < 100 ? 1 : 0);
  if (lang.value === 'zh') {
    return size < 100_000_000 ? formatNumber(value / 10_000, 2) + '万' : formatNumber(value / 100_000_000, 2) + '亿';
  }
  if (size < 1_000_000) return formatNumber(value / 1000, 1) + 'K';
  if (size < 1_000_000_000) return formatNumber(value / 1_000_000, 1) + 'M';
  return formatNumber(value / 1_000_000_000, 1) + 'B';
}

// ---------- durations ----------

/** How long a duration is, in the two largest units that matter:
 *  "45 min", "1 h 20 min", "3 days". */
export function formatSpan(ms: number): string {
  const minutes = Math.round(Math.abs(ms) / 60_000);
  if (minutes < 1) return t('common.span.underMinute');
  if (minutes < 60) return t('common.span.minutes', { n: minutes });
  if (minutes < 48 * 60) {
    const h = Math.floor(minutes / 60);
    const m = minutes % 60;
    return m === 0 || h >= 12 ? t('common.span.hours', { h }) : t('common.span.hoursMinutes', { h, m });
  }
  return formatDays(Math.abs(ms) / DAY_MS);
}

/** A number of days: one decimal under ten, whole days above, and "over a
 *  year" where precision would be pretence. */
export function formatDays(days: number): string {
  if (days >= 730) return t('common.days.over2y');
  if (days >= 365) return t('common.days.over1y');
  const n = days >= 10 ? Math.round(days) : Math.round(days * 10) / 10;
  return t('common.days.n', { n });
}

/** When a date is, relative to today: "today", "tomorrow", "in 3 days". */
export function formatUntil(days: number): string {
  if (days === 0) return t('common.until.today');
  if (days === 1) return t('common.until.tomorrow');
  if (days === 2) return t('common.until.in2');
  if (days > 0) return t('common.until.inN', { n: days });
  if (days === -1) return t('common.until.yesterday');
  return t('common.until.agoN', { n: -days });
}

/** How long ago an instant was: "just now", "5 min ago", "3 days ago"; past
 *  a month, the date. */
export function formatAgo(instant: Instant | number, nowMs: number = now.value, zone: string = timezone.value): string {
  const at = toMs(instant);
  const elapsed = nowMs - at;
  if (elapsed < 45_000) return t('common.ago.now');
  if (elapsed < 2 * DAY_MS) return t('common.ago.span', { span: formatSpan(elapsed) });
  if (elapsed < 31 * DAY_MS) return t('common.until.agoN', { n: Math.round(elapsed / DAY_MS) });
  return formatDay(dayOf(at, zone), { year: yearOf(nowMs, zone) });
}

/** How far ahead an instant is: "in 1 h 40 min". */
export function formatIn(instant: Instant | number, nowMs: number = now.value): string {
  return t('common.in.span', { span: formatSpan(toMs(instant) - nowMs) });
}

// ---------- the time zone ----------

export interface ZonedParts {
  year: number;
  month: number;
  day: number;
  hour: number;
  minute: number;
  second: number;
  /** ISO: 1 Monday … 7 Sunday */
  weekday: number;
}

const zoneFormats = new Map<string, Intl.DateTimeFormat>();
const WEEKDAYS: Record<string, number> = { Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6, Sun: 7 };

/** Breaks an instant into the calendar fields it has in a zone. */
export function zonedParts(ms: number, zone: string = timezone.value): ZonedParts {
  let format = zoneFormats.get(zone);
  if (!format) {
    format = new Intl.DateTimeFormat('en-US', {
      timeZone: zone,
      hourCycle: 'h23',
      year: 'numeric',
      month: 'numeric',
      day: 'numeric',
      hour: 'numeric',
      minute: 'numeric',
      second: 'numeric',
      weekday: 'short',
    });
    zoneFormats.set(zone, format);
  }
  const out: ZonedParts = { year: 1970, month: 1, day: 1, hour: 0, minute: 0, second: 0, weekday: 4 };
  for (const part of format.formatToParts(ms)) {
    switch (part.type) {
      case 'year':
        out.year = Number(part.value);
        break;
      case 'month':
        out.month = Number(part.value);
        break;
      case 'day':
        out.day = Number(part.value);
        break;
      case 'hour':
        out.hour = Number(part.value) % 24;
        break;
      case 'minute':
        out.minute = Number(part.value);
        break;
      case 'second':
        out.second = Number(part.value);
        break;
      case 'weekday':
        out.weekday = WEEKDAYS[part.value] ?? 1;
        break;
    }
  }
  return out;
}

/** The instant at which a zone's wall clock shows the given date and time. */
export function zonedTime(year: number, month: number, day: number, hour = 0, minute = 0, zone: string = timezone.value): number {
  const wall = Date.UTC(year, month - 1, day, hour, minute);
  // The zone's offset depends on the instant, which is what we are looking
  // for: guess, measure the offset there, and correct once more for a guess
  // that landed on the far side of a clock change.
  let at = wall - zoneOffset(wall, zone);
  at = wall - zoneOffset(at, zone);
  return at;
}

function zoneOffset(ms: number, zone: string): number {
  const p = zonedParts(ms, zone);
  return Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second) - Math.floor(ms / 1000) * 1000;
}

function yearOf(ms: number, zone: string): number {
  return zonedParts(ms, zone).year;
}

// ---------- calendar days ----------

/** The calendar day an instant falls on in a zone. */
export function dayOf(instant: Instant | number, zone: string = timezone.value): Day {
  const p = zonedParts(toMs(instant), zone);
  return makeDay(p.year, p.month, p.day);
}

export function makeDay(year: number, month: number, day: number): Day {
  return `${String(year).padStart(4, '0')}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
}

export function parseDay(day: Day): { year: number; month: number; day: number } {
  const [year, month, date] = day.split('-').map(Number);
  return { year: year ?? 1970, month: month ?? 1, day: date ?? 1 };
}

/** Midnight at the start of a calendar day in a zone. */
export function dayStart(day: Day, zone: string = timezone.value): number {
  const p = parseDay(day);
  return zonedTime(p.year, p.month, p.day, 0, 0, zone);
}

export function addDays(day: Day, count: number): Day {
  const p = parseDay(day);
  const moved = new Date(Date.UTC(p.year, p.month - 1, p.day + count));
  return makeDay(moved.getUTCFullYear(), moved.getUTCMonth() + 1, moved.getUTCDate());
}

/** ISO weekday of a calendar day: 1 Monday … 7 Sunday. */
export function weekdayOf(day: Day): number {
  const p = parseDay(day);
  return new Date(Date.UTC(p.year, p.month - 1, p.day)).getUTCDay() || 7;
}

const WEEKDAY_NAMES = {
  en: ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'],
  zh: ['周一', '周二', '周三', '周四', '周五', '周六', '周日'],
} as const;
const MONTH_NAMES = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'] as const;

/** "Mon" / "周一" for an ISO weekday. */
export function weekdayName(weekday: number): string {
  return WEEKDAY_NAMES[lang.value][(((weekday - 1) % 7) + 7) % 7]!;
}

/** "Nov" / "11月" */
export function monthName(month: number): string {
  return lang.value === 'zh' ? `${month}月` : MONTH_NAMES[(((month - 1) % 12) + 12) % 12]!;
}

export interface DayStyle {
  /** Add the weekday: "Thu, Dec 24" / "12月24日 周四". */
  weekday?: boolean;
  /** The year is left out when the day falls in this one. Omit to always
   *  leave it out. */
  year?: number;
}

/** Writes a calendar day: "Dec 24" / "12月24日". */
export function formatDay(day: Day, style: DayStyle = {}): string {
  const p = parseDay(day);
  const showYear = style.year !== undefined && style.year !== p.year;
  if (lang.value === 'zh') {
    let text = `${p.month}月${p.day}日`;
    if (showYear) text = `${p.year}年${text}`;
    return style.weekday ? `${text} ${weekdayName(weekdayOf(day))}` : text;
  }
  let text = `${monthName(p.month)} ${p.day}`;
  if (style.weekday) text = `${weekdayName(weekdayOf(day))}, ${text}`;
  return showYear ? `${text}, ${p.year}` : text;
}

/** "November 2026" / "2026年11月" for a "YYYY-MM". */
export function formatMonth(month: string): string {
  const [year, m] = month.split('-').map(Number);
  if (!year || !m) return month;
  if (lang.value === 'zh') return `${year}年${m}月`;
  return new Intl.DateTimeFormat('en-US', { month: 'long', year: 'numeric', timeZone: 'UTC' }).format(Date.UTC(year, m - 1, 1));
}

// ---------- instants ----------

/** "14:20" in the zone. */
export function formatTime(instant: Instant | number, zone: string = timezone.value): string {
  const p = zonedParts(toMs(instant), zone);
  return `${String(p.hour).padStart(2, '0')}:${String(p.minute).padStart(2, '0')}`;
}

/** A moment as a time of day, with the date when it is not today and the
 *  year when it is not this year: "14:20", "Nov 21 14:20". */
export function formatClock(instant: Instant | number, nowMs: number = now.value, zone: string = timezone.value): string {
  const at = toMs(instant);
  const day = dayOf(at, zone);
  const time = formatTime(at, zone);
  if (day === dayOf(nowMs, zone)) return time;
  return `${formatDay(day, { year: yearOf(nowMs, zone) })} ${time}`;
}

/** A moment with its date, always: "Nov 21 14:20", and the year when it is
 *  not this year. For chart readouts and logs, where "14:20" alone would be
 *  ambiguous. */
export function formatStamp(instant: Instant | number, nowMs: number = now.value, zone: string = timezone.value): string {
  const at = toMs(instant);
  return `${formatDay(dayOf(at, zone), { year: yearOf(nowMs, zone) })} ${formatTime(at, zone)}`;
}

/** A moment in full, for a tooltip or a detail row: "Nov 21, 2026 14:20". */
export function formatDateTime(instant: Instant | number, zone: string = timezone.value): string {
  const at = toMs(instant);
  return `${formatDay(dayOf(at, zone), { year: -1 })} ${formatTime(at, zone)}`;
}

/** The calendar day of an instant: "Nov 22", with the year when it is not
 *  this year. */
export function formatDayOf(instant: Instant | number, nowMs: number = now.value, zone: string = timezone.value): string {
  return formatDay(dayOf(instant, zone), { year: yearOf(nowMs, zone) });
}

export function toMs(instant: Instant | number): number {
  return typeof instant === 'number' ? instant : Date.parse(instant);
}

// The arithmetic under the charts: scales, tick positions, buckets. Pure
// functions of their arguments — no DOM, no signals — so they are tested
// directly (test/scale.test.ts).
import type { Day } from '../api/types';
import { addDays, dayOf, dayStart, parseDay, weekdayOf, zonedTime } from '../format';

const HOUR = 3_600_000;
const DAY = 86_400_000;

/** Maps a value from one interval onto another. */
export function linear(d0: number, d1: number, r0: number, r1: number): (value: number) => number {
  const span = d1 - d0;
  if (span === 0) return () => (r0 + r1) / 2;
  return (value) => r0 + ((value - d0) / span) * (r1 - r0);
}

/** A round step — 1, 2, 2.5 or 5 times a power of ten — near span/count. */
export function niceStep(span: number, count: number): number {
  if (!(span > 0) || count < 1) return 1;
  const raw = span / count;
  const power = 10 ** Math.floor(Math.log10(raw));
  const unit = raw / power;
  const nice = unit <= 1 ? 1 : unit <= 2 ? 2 : unit <= 2.5 ? 2.5 : unit <= 5 ? 5 : 10;
  return nice * power;
}

/** Tick values from zero up to the first round number at or above max. The
 *  last tick is the top of the axis. */
export function niceTicks(max: number, count = 4): number[] {
  if (!(max > 0)) return [0, 1];
  const step = niceStep(max, count);
  const ticks: number[] = [];
  for (let i = 0; ; i++) {
    // i * step keeps 0.1 + 0.2 out of the labels; the rounding removes what is left.
    const value = Math.round(i * step * 1e9) / 1e9;
    ticks.push(value);
    if (value >= max || i > 50) break;
  }
  return ticks;
}

/** The index of the value in a sorted list nearest to x; -1 for an empty list. */
export function nearest(sorted: readonly number[], x: number): number {
  if (sorted.length === 0) return -1;
  let low = 0;
  let high = sorted.length - 1;
  while (high - low > 1) {
    const mid = (low + high) >> 1;
    if (sorted[mid]! <= x) low = mid;
    else high = mid;
  }
  return Math.abs(sorted[low]! - x) <= Math.abs(sorted[high]! - x) ? low : high;
}

export interface TimeTick {
  t: number;
  unit: 'hour' | 'day' | 'month';
  /** The first of its larger unit: midnight among hours, the 1st among
   *  days, January among months. Labels say more at these. */
  major: boolean;
}

const HOUR_STEPS = [1, 2, 3, 6, 12];
const DAY_STEPS = [1, 2, 7, 14];
const MONTH_STEPS = [1, 2, 3, 6, 12];

/**
 * Tick positions for a time axis, at round moments of the zone's wall clock:
 * whole hours, midnights, firsts of months. The step is the finest one that
 * fits within maxTicks.
 */
export function timeTicks(from: number, to: number, maxTicks: number, zone: string): TimeTick[] {
  const span = to - from;
  if (!(span > 0)) return [];
  const limit = Math.max(1, maxTicks);
  const first = dayOf(from, zone);
  const last = dayOf(to, zone);
  const within = (t: number) => t >= from && t <= to;
  const ticks: TimeTick[] = [];

  for (const step of HOUR_STEPS) {
    if (span / (step * HOUR) > limit) continue;
    for (let day = first; day <= last; day = addDays(day, 1)) {
      const { year, month, day: date } = parseDay(day);
      for (let hour = 0; hour < 24; hour += step) {
        const t = zonedTime(year, month, date, hour, 0, zone);
        if (within(t)) ticks.push({ t, unit: 'hour', major: hour === 0 });
      }
    }
    return ticks;
  }

  for (const step of DAY_STEPS) {
    if (span / (step * DAY) > limit) continue;
    for (let day = first; day <= last; day = addDays(day, 1)) {
      const { year, month, day: date } = parseDay(day);
      const index = Math.round(Date.UTC(year, month - 1, date) / DAY);
      // Weekly steps land on Mondays (day 4 of the epoch was one); shorter
      // ones just count days, so the grid does not shift as time passes.
      const aligned = step >= 7 ? (index - 4) % step === 0 : index % step === 0;
      if (!aligned) continue;
      const t = dayStart(day, zone);
      if (within(t)) ticks.push({ t, unit: 'day', major: date === 1 });
    }
    return ticks;
  }

  const start = parseDay(first);
  const end = parseDay(last);
  let step = MONTH_STEPS[MONTH_STEPS.length - 1]!;
  for (const candidate of MONTH_STEPS) {
    if (span / (candidate * 30.44 * DAY) <= limit) {
      step = candidate;
      break;
    }
  }
  for (let index = start.year * 12 + start.month - 1; index <= end.year * 12 + end.month - 1; index++) {
    if (index % step !== 0) continue;
    const year = Math.floor(index / 12);
    const month = (index % 12) + 1;
    const t = zonedTime(year, month, 1, 0, 0, zone);
    if (within(t)) ticks.push({ t, unit: 'month', major: month === 1 });
  }
  return ticks;
}

export type BucketSize = 'day' | 'week' | 'month';

export interface Bucket {
  /** The part of the period the data covers, in epoch milliseconds. */
  start: number;
  end: number;
  firstDay: Day;
  lastDay: Day;
  value: number;
  /** The period is not fully covered: it began before the range, or it is
   *  still running. */
  partial: boolean;
  /** The period contains `to`: it is still running. */
  current: boolean;
}

/** The coarsest size at which a day gets at least minPx of width. */
export function chooseBucket(pxPerDay: number, minPx = 5): BucketSize {
  if (pxPerDay >= minPx) return 'day';
  if (pxPerDay * 7 >= minPx) return 'week';
  return 'month';
}

/**
 * Groups consecutive calendar days of spend into days, ISO weeks or months.
 * `from` and `to` are the instants the data covers: the first and the last
 * day are usually cut short by them.
 */
export function bucketDays(days: readonly { date: Day; spend: number }[], size: BucketSize, from: number, to: number, zone: string): Bucket[] {
  const groups = new Map<string, { first: Day; last: Day; value: number; count: number }>();
  for (const day of days) {
    const key = size === 'day' ? day.date : size === 'week' ? addDays(day.date, 1 - weekdayOf(day.date)) : day.date.slice(0, 7);
    const group = groups.get(key);
    if (group) {
      group.last = day.date;
      group.value += day.spend;
      group.count += 1;
    } else {
      groups.set(key, { first: day.date, last: day.date, value: day.spend, count: 1 });
    }
  }
  const buckets: Bucket[] = [];
  for (const group of groups.values()) {
    const fullStart = dayStart(group.first, zone);
    const fullEnd = dayStart(addDays(group.last, 1), zone);
    const whole =
      size === 'day' ? 1 : size === 'week' ? 7 : new Date(Date.UTC(parseDay(group.first).year, parseDay(group.first).month, 0)).getUTCDate();
    const start = Math.max(fullStart, from);
    const end = Math.min(fullEnd, to);
    if (end <= start) continue;
    buckets.push({
      start,
      end,
      firstDay: group.first,
      lastDay: group.last,
      value: group.value,
      partial: group.count < whole || start > fullStart || end < fullEnd,
      current: fullEnd > to,
    });
  }
  return buckets;
}

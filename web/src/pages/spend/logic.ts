// The arithmetic of the Spend page, kept apart from the drawing so that it
// can be tested (test/spend.test.ts). Pure functions: no signals, no DOM.
import type { Day, Spend } from '../../api/types';
import { dayStart, parseDay } from '../../format';

const DAY_MS = 86_400_000;

/** True for a month written the way the API takes it: "2026-11". */
export function isMonth(text: string | null | undefined): text is string {
  return typeof text === 'string' && /^\d{4}-(0[1-9]|1[0-2])$/.test(text);
}

/** The month a calendar day falls in: "2026-11-16" is in "2026-11". */
export function monthOf(day: Day): string {
  return day.slice(0, 7);
}

/** The month `by` months later — earlier when `by` is negative. */
export function shiftMonth(month: string, by: number): string {
  const [year = 1970, number = 1] = month.split('-').map(Number);
  const index = year * 12 + (number - 1) + by;
  return `${String(Math.floor(index / 12)).padStart(4, '0')}-${String((index % 12) + 1).padStart(2, '0')}`;
}

/** The instants a month begins and ends at in a zone: the edges of its chart. */
export function monthSpan(month: string, zone: string): { from: number; to: number } {
  return { from: dayStart(`${month}-01`, zone), to: dayStart(`${shiftMonth(month, 1)}-01`, zone) };
}

/** Whole days from one calendar day to another; negative when `to` is the earlier one. */
export function daysBetween(from: Day, to: Day): number {
  const a = parseDay(from);
  const b = parseDay(to);
  return Math.round((Date.UTC(b.year, b.month - 1, b.day) - Date.UTC(a.year, a.month - 1, a.day)) / DAY_MS);
}

/** Which day of the month it is, for "day 16 of 30": 15.8 days in is the 16th. */
export function dayNumber(spend: Pick<Spend, 'days_elapsed' | 'days_in_month'>): number {
  return Math.min(spend.days_in_month, Math.max(1, Math.floor(spend.days_elapsed) + 1));
}

/**
 * What the projection assumes each remaining day will cost, in the base
 * currency: the rest of the way from `total` to `projected`, spread over the
 * days that are left. Null when there is nothing of the kind to say — a month
 * that is over, its last hours, or a projection that does not rise.
 */
export function paceToProjection(spend: Pick<Spend, 'is_current' | 'total' | 'projected' | 'days_elapsed' | 'days_in_month'>): number | null {
  if (!spend.is_current) return null;
  const left = spend.days_in_month - spend.days_elapsed;
  const rest = spend.projected - spend.total;
  if (!(left >= 0.25) || !(rest > 0)) return null;
  return rest / left;
}

/** True when a month has nothing to show: no meter burned anything and no
 *  renewal falls in it. A meter with a budget is listed by the API even in a
 *  month it spent nothing in; that alone is not something to show. */
export function isEmpty(spend: Pick<Spend, 'rows' | 'renewals'>): boolean {
  return spend.renewals.length === 0 && !hasMetered(spend);
}

/** True when at least one meter spent, or is expected to spend, something. */
export function hasMetered(spend: Pick<Spend, 'rows'>): boolean {
  return spend.rows.some((row) => row.spend !== 0 || row.projected !== 0);
}

export interface HistoryBar {
  month: string;
  total: number;
  fixed: number;
  /** The bar's height as a share, 0 to 1, of the tallest thing drawn. */
  share: number;
  /** For the month still running: where it is heading, on the same scale.
   *  Zero when that adds nothing to the bar. */
  heading: number;
}

/**
 * Scales the months of the history to the tallest of them — counting, for
 * the month still running, where it is heading, so that its bar can be
 * compared with the finished ones.
 */
export function historyBars(history: Spend['history'], running: { month: string; projected: number } | null): HistoryBar[] {
  let top = 0;
  for (const each of history) top = Math.max(top, each.total);
  if (running) top = Math.max(top, running.projected);
  return history.map((each) => {
    const projected = running && running.month === each.month ? running.projected : 0;
    return {
      month: each.month,
      total: each.total,
      fixed: each.fixed,
      share: top > 0 ? Math.max(0, each.total) / top : 0,
      heading: top > 0 && projected > each.total ? projected / top : 0,
    };
  });
}

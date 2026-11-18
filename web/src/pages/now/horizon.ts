// What is ahead. Everything QuotaPulse watches comes down to a moment: a
// balance runs out, a quota is used up before it resets, a month passes its
// budget, a date comes due. This puts all of them on one line from now on.
//
//   now      open problems — the ones nobody has acknowledged first, worst
//            first. A thing that is here is not repeated further down.
//   groups   what happens next, soonest first: today, tomorrow, within a
//            week, within a month, later (up to the horizon)
//   steady   meters with nothing ahead inside the horizon
//
// Pure: it decides what goes where and when; the page decides the words.
import type { Day, IncidentView, MeterView, Overview, ReminderView, Severity } from '../../api/types';
import { dayOf, dayStart, parseDay, toMs } from '../../format';

const DAY = 86_400_000;

export type EventKind = 'runout' | 'exhaust' | 'budget' | 'date';

export interface HorizonEvent {
  key: string;
  kind: EventKind;
  /** When it happens, in epoch milliseconds. */
  at: number;
  /** The calendar day it happens on, in the zone. */
  day: Day;
  /** Whole days from today; 0 is today. */
  days: number;
  /** A forecast, rather than a date someone set. */
  estimated: boolean;
  meter?: MeterView;
  reminder?: ReminderView;
  /** An open incident about the same thing that does not ask for anyone:
   *  a date that is near. */
  incident?: IncidentView;
}

export type Bucket = 'today' | 'tomorrow' | 'week' | 'month' | 'later';

export interface Horizon {
  now: IncidentView[];
  groups: { bucket: Bucket; events: HorizonEvent[] }[];
  steady: MeterView[];
  /** The first thing ahead, for the line under the verdict. */
  next: HorizonEvent | null;
}

const RANK: Record<Severity, number> = { info: 1, warning: 2, critical: 3 };

/** Whole calendar days from one day to another. */
function daysFrom(from: Day, to: Day): number {
  const a = parseDay(from);
  const b = parseDay(to);
  return Math.round((Date.UTC(b.year, b.month - 1, b.day) - Date.UTC(a.year, a.month - 1, a.day)) / DAY);
}

export function bucketOf(days: number): Bucket {
  if (days <= 0) return 'today';
  if (days === 1) return 'tomorrow';
  if (days <= 7) return 'week';
  if (days <= 31) return 'month';
  return 'later';
}

/** Open incidents in the order they ask for a person: unacknowledged before
 *  acknowledged, then the worst first, then the oldest first. */
export function byUrgency(a: IncidentView, b: IncidentView): number {
  const acked = Number(a.acked_at !== null) - Number(b.acked_at !== null);
  if (acked !== 0) return acked;
  const rank = RANK[b.severity] - RANK[a.severity];
  if (rank !== 0) return rank;
  return toMs(a.opened_at) - toMs(b.opened_at);
}

/** The moment a month's spend crosses its budget at the pace it is going,
 *  or null when it will not, or already has. */
export function budgetCrossing(meter: MeterView, nowMs: number): number | null {
  const budget = meter.rules.budget;
  const month = meter.month;
  if (!budget || budget <= 0 || !month) return null;
  if (month.spend >= budget || month.projected <= budget) return null;
  const left = month.days_in_month - month.days_elapsed;
  if (!(left > 0)) return null;
  const perDay = (month.projected - month.spend) / left;
  if (!(perDay > 0)) return null;
  return nowMs + ((budget - month.spend) / perDay) * DAY;
}

/** What happens next to a meter, if anything does, and when. */
function meterEvent(meter: MeterView, nowMs: number): { kind: EventKind; at: number } | null {
  if (meter.kind === 'balance') {
    const forecast = meter.forecast;
    if (!forecast?.depletes_at || meter.value === null || meter.value <= 0) return null;
    return { kind: 'runout', at: toMs(forecast.depletes_at) };
  }
  if (meter.kind === 'quota') {
    const exhausts = meter.quota?.exhausts_at;
    return exhausts ? { kind: 'exhaust', at: toMs(exhausts) } : null;
  }
  const crossing = budgetCrossing(meter, nowMs);
  return crossing === null ? null : { kind: 'budget', at: crossing };
}

/** Whether a meter is watched at all right now: shown, and being read. */
export function isLive(meter: MeterView): boolean {
  return !meter.hidden && meter.status !== 'paused';
}

export function buildHorizon(data: Overview, nowMs: number, zone: string, horizonDays = 60): Horizon {
  const today = dayOf(nowMs, zone);

  // A date that is merely near is not a problem: it belongs on its day.
  const now = data.attention.filter((incident) => incident.state === 'open' && incident.kind !== 'due').sort(byUrgency);
  const taken = new Set(now.map((incident) => `${incident.subject_type}:${incident.subject_id}`));
  const accountsTaken = new Set(now.filter((incident) => incident.subject_type === 'account').map((incident) => incident.subject_id));

  const events: HorizonEvent[] = [];
  const steady: MeterView[] = [];

  for (const meter of data.meters) {
    if (!isLive(meter) || taken.has(`meter:${meter.id}`)) continue;
    // Its account cannot be read: that is said once, up top.
    if (meter.account_id && accountsTaken.has(meter.account_id)) continue;
    const next = meterEvent(meter, nowMs);
    if (next && next.at > nowMs) {
      const day = dayOf(next.at, zone);
      const days = daysFrom(today, day);
      if (days <= horizonDays) {
        events.push({ key: `meter:${meter.id}`, kind: next.kind, at: next.at, day, days, estimated: true, meter });
        continue;
      }
    }
    steady.push(meter);
  }

  for (const reminder of data.upcoming) {
    if (!reminder.next || reminder.days === null || reminder.state === 'disabled') continue;
    if (taken.has(`reminder:${reminder.id}`)) continue;
    const incident = data.attention.find((each) => each.state === 'open' && each.subject_type === 'reminder' && each.subject_id === reminder.id);
    events.push({
      key: `reminder:${reminder.id}`,
      kind: 'date',
      at: dayStart(reminder.next, zone),
      day: reminder.next,
      days: reminder.days,
      estimated: false,
      reminder,
      ...(incident ? { incident } : {}),
    });
  }

  // Soonest first; on the same day, a fixed date before a forecast.
  events.sort((a, b) => a.days - b.days || Number(a.estimated) - Number(b.estimated) || a.at - b.at || a.key.localeCompare(b.key));

  const groups: Horizon['groups'] = [];
  for (const event of events) {
    const bucket = bucketOf(event.days);
    const last = groups[groups.length - 1];
    if (last && last.bucket === bucket) last.events.push(event);
    else groups.push({ bucket, events: [event] });
  }

  // Steady meters: the ones that will last longest at the bottom.
  steady.sort((a, b) => (a.forecast?.runway_days ?? Infinity) - (b.forecast?.runway_days ?? Infinity) || a.display.localeCompare(b.display));

  return { now, groups, steady, next: events[0] ?? null };
}

export type Tone = 'ok' | 'neutral' | Severity;

/** The verdict at the top: how many problems ask for a person, how many are
 *  known and still open, and the tone of the whole. */
export function verdictOf(now: readonly IncidentView[]): { tone: Tone; fresh: number; known: number } {
  let fresh = 0;
  let known = 0;
  let worst: Severity | null = null;
  for (const incident of now) {
    if (incident.acked_at !== null) {
      known += 1;
      continue;
    }
    fresh += 1;
    if (worst === null || RANK[incident.severity] > RANK[worst]) worst = incident.severity;
  }
  if (worst !== null) return { tone: worst, fresh, known };
  return { tone: known > 0 ? 'neutral' : 'ok', fresh, known };
}

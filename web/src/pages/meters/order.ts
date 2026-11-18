// The order meters are listed in: the ones that are a problem first, then
// the ones that will be one soonest. Pure, so it can be tested.
import type { MeterView } from '../../api/types';
import { toMs } from '../../format';
import { budgetCrossing } from '../now/horizon';

const DAY = 86_400_000;

/** How much a meter's state asks for a person: lower is more. */
const RANK = { critical: 0, warning: 1, failing: 2, pending: 3, ok: 4, paused: 5 } as const;

/** A meter's state, counting one whose account cannot be read as failing. */
export function stateOf(meter: MeterView): keyof typeof RANK {
  if (meter.status !== 'paused' && meter.account?.status === 'failing') return 'failing';
  return meter.status;
}

/** Whether a meter belongs with the ones that need a look. */
export function needsLook(meter: MeterView): boolean {
  return RANK[stateOf(meter)] <= RANK.failing;
}

/** Days until a meter becomes a problem at the pace it is going: a balance
 *  runs out, a quota is used up before it resets, a month passes its budget.
 *  Infinity when nothing like that is ahead. */
export function daysToTrouble(meter: MeterView, nowMs: number): number {
  if (meter.kind === 'balance') {
    if (meter.value !== null && meter.value <= 0) return 0;
    const forecast = meter.forecast;
    if (!forecast || forecast.confidence === 'none' || forecast.runway_days === null) return Infinity;
    return forecast.runway_days;
  }
  if (meter.kind === 'quota') {
    const exhausts = meter.quota?.exhausts_at;
    return exhausts ? Math.max(0, (toMs(exhausts) - nowMs) / DAY) : Infinity;
  }
  const budget = meter.rules.budget;
  if (budget && meter.month && meter.month.spend >= budget) return 0;
  const crossing = budgetCrossing(meter, nowMs);
  return crossing === null ? Infinity : (crossing - nowMs) / DAY;
}

/** Problems first, worst first; then the soonest to become one; then by name. */
export function byUrgency(nowMs: number): (a: MeterView, b: MeterView) => number {
  return (a, b) =>
    RANK[stateOf(a)] - RANK[stateOf(b)] ||
    daysToTrouble(a, nowMs) - daysToTrouble(b, nowMs) ||
    a.display.localeCompare(b.display);
}

/** Whether a meter matches what was typed into the search: its name, where
 *  it comes from, or its group. */
export function matchesMeter(meter: MeterView, typed: string): boolean {
  const query = typed.trim().toLowerCase();
  if (!query) return true;
  const fields = [meter.display, meter.name, meter.account?.name, meter.account?.provider_name, meter.account?.group, meter.unit];
  return fields.some((field) => field?.toLowerCase().includes(query));
}

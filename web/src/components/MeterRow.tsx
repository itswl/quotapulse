// A meter as a row of a list: its name and the one figure that answers
// "how long until this is a problem", the facts behind that figure under it,
// and a bar that draws it. The helpers above the row are what every page
// that shows a meter shares.
import { BellOff } from 'lucide-preact';
import type { ComponentChildren } from 'preact';
import type { MeterView } from '../api/types';
import { BudgetBar, QuotaBar, RunwayBar, type BarTone } from '../charts';
import { amountParts, formatAmount, formatClock, formatDayOf, formatDays, formatIn, formatPercent, toMs } from '../format';
import { t } from '../i18n';
import { Link } from '../router';
import { now } from '../state/clock';
import { Amount } from './Amount';
import { cx } from './cx';
import { StatusDot } from './StatusDot';

/** True for a meter that asks for a person: something is wrong with it or
 *  with the account it comes from. */
export function needsAttention(meter: MeterView): boolean {
  return meter.status === 'warning' || meter.status === 'critical' || meter.status === 'failing';
}

/** True when the meter is pushed through the API rather than sampled. */
export function isPushed(meter: MeterView): boolean {
  return meter.account_id === '';
}

/** Where a meter comes from, for the line under its name: the platform and
 *  the group — leaving out whatever the name already says. When that leaves
 *  nothing, the kind of meter, which is always worth knowing. */
export function meterSource(meter: MeterView, withGroup = true): string {
  const account = meter.account;
  if (!account) return t('meters.card.pushed');
  const parts: string[] = [];
  if (account.provider_name && !meter.display.includes(account.provider_name)) parts.push(account.provider_name);
  if (withGroup && account.group) parts.push(account.group);
  if (parts.length === 0) parts.push(t(`meters.kind.${meter.kind}`));
  return parts.join(' · ');
}

/** How long a balance lasts, in words: "about 5.8 days left". */
export function runwayText(days: number): string {
  return t(days >= 365 ? 'meters.card.runwayLong' : 'meters.card.runway', { days: formatDays(days) });
}

/** The tone a quota's bar is drawn in. */
export function quotaTone(meter: MeterView): BarTone {
  if (meter.status === 'critical') return 'critical';
  if (meter.status === 'warning' || meter.quota?.exhausts_at) return 'warning';
  if (meter.status === 'failing' || meter.status === 'paused') return 'neutral';
  return 'accent';
}

/** A quota's bar, with the pace tick when the window is known. */
export function MeterQuotaBar({ meter }: { meter: MeterView }) {
  const quota = meter.quota;
  if (!quota) return null;
  const pace = quota.elapsed_pct === null ? null : 100 - quota.elapsed_pct;
  const left = formatPercent(quota.remaining_pct);
  const label = pace === null ? t('meters.bar.quota', { left }) : t('meters.bar.quotaPace', { left, pace: formatPercent(pace) });
  return <QuotaBar remaining={quota.remaining_pct} pace={pace} tone={quotaTone(meter)} label={label} />;
}

/** A meter's month against its budget. Nothing when it has no budget. */
export function MeterBudgetBar({ meter }: { meter: MeterView }) {
  const budget = meter.rules.budget;
  const month = meter.month;
  if (!budget || !month) return null;
  const amounts = { spent: formatAmount(month.spend, meter.unit), budget: formatAmount(budget, meter.unit), projected: formatAmount(month.projected, meter.unit) };
  return <BudgetBar spent={month.spend} projected={month.projected} budget={budget} label={t('meters.bar.budgetProjected', amounts)} />;
}

/** The tone a meter is drawn in: its state, and nothing when it is fine. */
export function meterTone(meter: MeterView): BarTone {
  if (meter.status === 'critical') return 'critical';
  if (meter.status === 'warning') return 'warning';
  if (meter.status !== 'ok' || meter.account?.status === 'failing') return 'neutral';
  return 'accent';
}

/** Days a balance lasts, when that is known well enough to say. */
export function runwayOf(meter: MeterView): number | null {
  const forecast = meter.forecast;
  if (meter.kind !== 'balance' || !forecast || forecast.confidence === 'none') return null;
  if (meter.value !== null && meter.value <= 0) return 0;
  return forecast.runway_days;
}

/** A number of days as a figure and its unit, for a figure set large:
 *  "13" "days". Past a year, words. */
export function daysFigure(days: number): { number: string; unit: string } | { words: string } {
  if (days >= 365) return { words: formatDays(days) };
  const n = days >= 10 ? Math.round(days) : Math.round(days * 10) / 10;
  return { number: String(n), unit: t('meters.row.days', { n }) };
}

export interface MeterRowProps {
  meter: MeterView;
  /** The days a runway bar's full width stands for. */
  horizon?: number;
  /** The meter open beside the list. */
  selected?: boolean;
}

/** One meter in a list. The whole row is a link to the meter. */
export function MeterRow({ meter, horizon = 60, selected }: MeterRowProps) {
  const unreadable = meter.account?.status === 'failing';
  const dim = unreadable || meter.status === 'paused' || meter.status === 'failing' || meter.status === 'pending';
  const runway = runwayOf(meter);
  const quiet = meter.status === 'ok' && !unreadable;
  return (
    <Link
      href={`/meters/${meter.id}`}
      class={cx('meter-row', selected && 'is-selected', meter.hidden && 'meter-row--hidden', dim && 'is-dim')}
      aria-current={selected ? 'page' : undefined}
    >
      <span class="meter-row__name">
        <span class="truncate">{meter.display}</span>
        {meter.muted && (
          <span class="meter-row__muted" title={meter.muted_until ? t('meters.card.muted', { time: formatClock(meter.muted_until) }) : t('common.status.muted')}>
            <BellOff size={13} />
            <span class="sr-only">{t('common.status.muted')}</span>
          </span>
        )}
      </span>
      <span class="meter-row__figure">
        <Figure meter={meter} runway={runway} unreadable={unreadable} />
      </span>
      <span class="meter-row__facts">
        {!quiet && <StatusDot status={unreadable ? 'failing' : meter.status} />}
        <span class="meter-row__line">
          <Facts meter={meter} runway={runway} unreadable={unreadable} />
        </span>
      </span>
      <Bar meter={meter} runway={runway} horizon={horizon} unreadable={unreadable} />
    </Link>
  );
}

function Figure({ meter, runway, unreadable }: { meter: MeterView; runway: number | null; unreadable: boolean }) {
  if (unreadable || meter.value === null) return <span class="num">—</span>;
  if (meter.kind === 'balance' && runway !== null) {
    const figure = daysFigure(runway);
    if ('words' in figure) return <span class="meter-row__words">{figure.words}</span>;
    return (
      <span class="num">
        {figure.number}
        <span class="unit unit--spaced">{figure.unit}</span>
      </span>
    );
  }
  if (meter.kind === 'quota' && meter.quota) return <Amount value={meter.quota.remaining_pct} unit="%" />;
  if (meter.kind === 'counter') return <Amount value={meter.month?.spend ?? meter.value} unit={meter.unit} />;
  return <Amount value={meter.value} unit={meter.unit} />;
}

function Facts({ meter, runway, unreadable }: { meter: MeterView; runway: number | null; unreadable: boolean }): ComponentChildren {
  if (meter.status === 'paused') return t('meters.card.paused');
  if (unreadable) {
    const error = meter.account?.last_error;
    return error ? `${t('meters.card.failing')}: ${error}` : t('meters.card.failing');
  }
  if (meter.value === null) return t('meters.card.pending');
  const parts: string[] = [];
  if (meter.kind === 'balance') {
    parts.push(formatAmount(meter.value, meter.unit));
    const forecast = meter.forecast;
    if (meter.value <= 0) parts.push(t('meters.card.empty'));
    else if (runway === null) parts.push(t('meters.card.collecting'));
    else if (forecast?.depletes_at && runway < 365) parts.push(t('meters.card.depletes', { date: formatDayOf(forecast.depletes_at) }));
    else if (forecast?.runway_days === null) parts.push(t('meters.card.barely'));
  } else if (meter.kind === 'quota') {
    if (meter.unit !== '%' && meter.limit) parts.push(`${amountParts(meter.value, meter.unit).number} / ${formatAmount(meter.limit, meter.unit)}`);
    const quota = meter.quota;
    if (quota?.exhausts_at && toMs(quota.exhausts_at) > now.value) parts.push(t('meters.card.exhausts', { time: formatClock(quota.exhausts_at) }));
    else if (meter.resets_at && toMs(meter.resets_at) > now.value) parts.push(t('meters.card.resets', { when: formatIn(meter.resets_at) }));
  } else {
    const month = meter.month;
    const budget = meter.rules.budget;
    if (!month) parts.push(t('meters.card.collecting'));
    else {
      parts.push(budget ? t('meters.row.ofBudget', { budget: formatAmount(budget, meter.unit) }) : t('meters.row.thisMonth'));
      if (month.projected > month.spend) parts.push(t('meters.card.heading', { amount: amountParts(month.projected, meter.unit).number }));
    }
  }
  return parts.join(' · ');
}

function Bar({ meter, runway, horizon, unreadable }: { meter: MeterView; runway: number | null; horizon: number; unreadable: boolean }) {
  if (unreadable || meter.value === null) return null;
  let bar: ComponentChildren = null;
  if (meter.kind === 'balance' && runway !== null) {
    bar = <RunwayBar days={runway} horizon={horizon} tone={meterTone(meter)} label={runwayText(runway)} />;
  } else if (meter.kind === 'quota') {
    bar = <MeterQuotaBar meter={meter} />;
  } else if (meter.kind === 'counter') {
    bar = <MeterBudgetBar meter={meter} />;
  }
  return bar ? <span class="meter-row__bar">{bar}</span> : null;
}

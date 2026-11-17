import { BellOff } from 'lucide-preact';
import type { ComponentChildren } from 'preact';
import { useMemo } from 'preact/hooks';
import type { MeterView } from '../api/types';
import { BudgetBar, QuotaBar, Sparkline, type BarTone } from '../charts';
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

export interface MeterCardProps {
  meter: MeterView;
  /** Show the account's group under the name. Off where the cards already
   *  sit under their group's heading. */
  showGroup?: boolean;
}

/**
 * One meter at a glance: its name, its value, its state, where it has been
 * going, and one line saying what the number means. The whole card is a
 * link to the meter's page.
 */
export function MeterCard({ meter, showGroup = true }: MeterCardProps) {
  const unreadable = meter.account?.status === 'failing';
  const dim = unreadable || meter.status === 'paused' || meter.status === 'failing';
  const number = meter.value === null ? '' : amountParts(meter.value, meter.unit).number;

  const spark = useMemo(() => meter.spark.map((point) => ({ t: toMs(point.t), v: point.v })), [meter.spark]);
  const first = meter.spark[0];
  const last = meter.spark[meter.spark.length - 1];
  const sparkTone = dim || meter.status === 'pending' ? 'neutral' : meter.status === 'critical' ? 'critical' : meter.status === 'warning' ? 'warning' : 'accent';

  return (
    <Link href={`/meters/${meter.id}`} class={cx('meter-card', meter.hidden && 'meter-card--hidden')}>
      <div class="meter-card__head">
        <div class="meter-card__titles">
          <h3 class="meter-card__name truncate" title={meter.display}>
            {meter.display}
          </h3>
          <p class="meter-card__source truncate">{meterSource(meter, showGroup)}</p>
        </div>
        <div class="meter-card__state">
          {meter.muted && (
            <span class="meter-card__muted" title={meter.muted_until ? t('meters.card.muted', { time: formatClock(meter.muted_until) }) : t('common.status.muted')}>
              <BellOff size={14} />
              <span class="sr-only">{t('common.status.muted')}</span>
            </span>
          )}
          <StatusDot status={meter.status} />
        </div>
      </div>
      <div class="meter-card__figure">
        <Amount class={cx('meter-card__value', dim && 'is-dim', number.length > 9 && 'is-long')} value={meter.value} unit={meter.unit} />
        {first && last && (
          <Sparkline
            class="meter-card__spark"
            points={spark}
            max={meter.kind === 'quota' && meter.limit ? meter.limit : undefined}
            tone={sparkTone}
            label={t('meters.card.spark', { first: formatAmount(first.v, meter.unit), last: formatAmount(last.v, meter.unit) })}
          />
        )}
      </div>
      <div class="meter-card__meaning">
        <Meaning meter={meter} unreadable={unreadable} />
      </div>
    </Link>
  );
}

function Line({ children, title }: { children: ComponentChildren; title?: string | undefined }) {
  return (
    <p class="meter-card__line" title={title}>
      {children}
    </p>
  );
}

/** The one line under the number that says what the number means. */
function Meaning({ meter, unreadable }: { meter: MeterView; unreadable: boolean }) {
  if (meter.status === 'paused') return <Line>{t('meters.card.paused')}</Line>;
  if (unreadable) {
    const error = meter.account?.last_error;
    const text = error ? `${t('meters.card.failing')}: ${error}` : t('meters.card.failing');
    return <Line title={text}>{text}</Line>;
  }
  if (meter.value === null) return <Line>{t('meters.card.pending')}</Line>;
  if (meter.kind === 'quota') return <QuotaMeaning meter={meter} />;
  if (meter.kind === 'counter') return <CounterMeaning meter={meter} />;
  return <Line>{balanceLine(meter)}</Line>;
}

function balanceLine(meter: MeterView): string {
  const forecast = meter.forecast;
  if (meter.value !== null && meter.value <= 0) return t('meters.card.empty');
  if (!forecast || forecast.confidence === 'none') return t('meters.card.collecting');
  if (forecast.runway_days === null) return t('meters.card.barely');
  const runway = runwayText(forecast.runway_days);
  if (forecast.runway_days >= 365 || !forecast.depletes_at) return runway;
  return `${runway} · ${t('meters.card.depletes', { date: formatDayOf(forecast.depletes_at) })}`;
}

function QuotaMeaning({ meter }: { meter: MeterView }) {
  const quota = meter.quota;
  const parts: string[] = [];
  if (meter.unit !== '%' && meter.limit) parts.push(t('meters.card.ofLimit', { amount: amountParts(meter.limit, meter.unit).number }));
  if (quota?.exhausts_at && toMs(quota.exhausts_at) > now.value) {
    parts.push(t('meters.card.exhausts', { time: formatClock(quota.exhausts_at) }));
  } else if (meter.resets_at && toMs(meter.resets_at) > now.value) {
    parts.push(t('meters.card.resets', { when: formatIn(meter.resets_at) }));
  } else if (quota && parts.length === 0) {
    parts.push(t('meters.card.left', { share: formatPercent(quota.remaining_pct) }));
  }
  return (
    <>
      <MeterQuotaBar meter={meter} />
      <Line>{parts.join(' · ')}</Line>
    </>
  );
}

function CounterMeaning({ meter }: { meter: MeterView }) {
  const month = meter.month;
  if (!month) return <Line>{t('meters.card.collecting')}</Line>;
  // The unit is on the big number above; the line can do without it.
  const number = (value: number) => amountParts(value, meter.unit).number;
  const heading = month.projected > month.spend ? t('meters.card.heading', { amount: number(month.projected) }) : '';
  const budget = meter.rules.budget;
  if (!budget) {
    const spent = t('meters.card.month', { amount: number(month.spend) });
    return <Line>{heading ? `${spent} · ${heading}` : spent}</Line>;
  }
  return (
    <>
      <MeterBudgetBar meter={meter} />
      <Line>
        <span>{t('meters.card.spentOf', { spent: number(month.spend), budget: number(budget) })}</span>
        {heading && <span>{heading}</span>}
      </Line>
    </>
  );
}

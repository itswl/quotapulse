// The pieces of Now: a problem with what can be done about it, a row of
// what is ahead, the line for everything that is fine, and the two cards a
// wide screen has room for at the side.
import { ArrowUpRight, BatteryLow, CalendarDays, Check, ChevronRight, CircleCheck, Gauge, TrendingUp, type LucideIcon } from 'lucide-preact';
import { useEffect, useState } from 'preact/hooks';
import { ackIncident, unackIncident } from '../../api/endpoints';
import type { IncidentView, MeterView, Overview, SpendSummary } from '../../api/types';
import { Amount, Button, Chip, cx, DayBlock, LinkButton, MeterQuotaBar, meterTone, runwayOf, runwayText, StatusDot, toast } from '../../components';
import { RunwayBar } from '../../charts';
import { errorText } from '../../errors';
import { formatAgo, formatAmount, formatDateTime, formatDays, formatIn, formatMonth, formatPercent, formatUntil } from '../../format';
import { pick, t } from '../../i18n';
import { Link } from '../../router';
import { providers } from '../../state/providers';
import { KIND_ICON, reminderKindLabel } from '../dates/kinds';
import { needsLook } from '../meters/order';
import { MonthBar } from '../spend/month-bar';
import { isLive, type HorizonEvent } from './horizon';

// ---------- words for an event ----------

const EVENT_ICON: Record<'runout' | 'exhaust' | 'budget', LucideIcon> = { runout: BatteryLow, exhaust: Gauge, budget: TrendingUp };

export function eventTitle(event: HorizonEvent): string {
  if (event.reminder) return event.reminder.title;
  const name = event.meter?.display ?? '';
  return t(`overview.event.${event.kind as 'runout' | 'exhaust' | 'budget'}`, { name });
}

export function eventDetail(event: HorizonEvent): string {
  const reminder = event.reminder;
  if (reminder) {
    const parts = [reminderKindLabel(reminder)];
    if (reminder.lunar) parts.push(t('common.date.lunar', { label: reminder.lunar.label }));
    if (reminder.amount !== null) parts.push(formatAmount(reminder.amount, reminder.currency));
    return parts.join(' · ');
  }
  const meter = event.meter;
  if (!meter) return '';
  if (event.kind === 'runout' && meter.value !== null && meter.forecast) {
    return t('overview.event.runout.detail', { amount: formatAmount(meter.value, meter.unit), burn: formatAmount(meter.forecast.burn_per_day, meter.unit) });
  }
  if (event.kind === 'exhaust' && meter.quota) {
    const left = formatPercent(meter.quota.remaining_pct);
    return meter.resets_at ? t('overview.event.exhaust.detail', { left, when: formatIn(meter.resets_at) }) : t('overview.event.exhaust.detail.noReset', { left });
  }
  if (event.kind === 'budget' && meter.month && meter.rules.budget) {
    return t('overview.event.budget.detail', {
      spent: formatAmount(meter.month.spend, meter.unit),
      budget: formatAmount(meter.rules.budget, meter.unit),
      projected: formatAmount(meter.month.projected, meter.unit),
    });
  }
  return '';
}

/** When it happens, from today: "tomorrow", "in 3 days"; a forecast says it
 *  is one, and today it says the hours. */
export function whenText(event: HorizonEvent): string {
  if (!event.estimated) return formatUntil(event.days);
  return t('overview.event.about', { when: event.days === 0 ? formatIn(event.at) : formatUntil(event.days) });
}

// ---------- a problem ----------

/** What can be done about a problem, beyond saying "I know": topping up at
 *  the platform it is about, looking at the account, fixing the channels. */
function remedy(incident: IncidentView, data: Overview): { href: string; label: string; external?: boolean } {
  if (incident.subject_type === 'channel') return { href: '/settings/channels', label: t('overview.action.channels') };
  if (incident.subject_type === 'account') return { href: incident.path, label: t('overview.action.account') };
  if (incident.subject_type === 'meter' && incident.kind === 'level') {
    const meter = data.meters.find((each) => each.id === incident.subject_id);
    const definition = meter?.account ? providers.data.value?.find((each) => each.id === meter.account?.provider) : undefined;
    if (definition?.homepage) return { href: definition.homepage, label: t('overview.action.open', { name: pick(definition.name, definition.name_zh) }), external: true };
  }
  return { href: incident.path, label: t('overview.attention.open') };
}

export function Problem({ incident, data }: { incident: IncidentView; data: Overview }) {
  // Acknowledging shows at once; the server's answer arrives with the next
  // refresh and replaces this guess.
  const [guess, setGuess] = useState<boolean | null>(null);
  useEffect(() => setGuess(null), [incident.acked_at]);
  const acked = guess ?? incident.acked_at !== null;

  const toggle = async () => {
    const next = !acked;
    setGuess(next);
    try {
      await (next ? ackIncident(incident.id) : unackIncident(incident.id));
    } catch (cause) {
      setGuess(null);
      toast.error(errorText(cause));
    }
  };

  const fix = remedy(incident, data);
  return (
    <li class={cx('problem', `problem--${incident.severity}`, acked && 'problem--acked')}>
      <StatusDot class="problem__mark" status={incident.severity} iconOnly size={18} />
      <div class="problem__body">
        <p class="problem__headline">
          <Link href={incident.path}>{incident.headline}</Link>
        </p>
        {incident.detail && <p class="problem__detail">{incident.detail}</p>}
        <div class="problem__foot">
          <time class="problem__since" dateTime={incident.opened_at} title={formatDateTime(incident.opened_at)}>
            {t('common.incident.opened', { ago: formatAgo(incident.opened_at) })}
          </time>
          <span class="problem__actions">
            {acked ? (
              <>
                <span class="problem__acked">
                  <Check size={14} />
                  {t('common.action.acked')}
                </span>
                <Button variant="ghost" size="sm" onClick={toggle}>
                  {t('common.action.undo')}
                </Button>
              </>
            ) : (
              <>
                {fix.external ? (
                  <a class="btn btn--secondary btn--sm" href={fix.href} target="_blank" rel="noopener noreferrer">
                    <span class="btn__label">{fix.label}</span>
                    <ArrowUpRight />
                  </a>
                ) : (
                  <LinkButton size="sm" href={fix.href}>
                    {fix.label}
                  </LinkButton>
                )}
                <Button size="sm" class="problem__ack" icon={Check} onClick={toggle}>
                  {t('common.action.ack')}
                </Button>
              </>
            )}
          </span>
        </div>
      </div>
    </li>
  );
}

// ---------- what is ahead ----------

export function AgendaRow({ event }: { event: HorizonEvent }) {
  const Icon = event.reminder ? KIND_ICON[event.reminder.kind] : EVENT_ICON[event.kind as 'runout' | 'exhaust' | 'budget'];
  const href = event.meter ? `/meters/${event.meter.id}` : `/dates?open=${encodeURIComponent(event.reminder?.id ?? '')}`;
  const near = event.days <= 3;
  return (
    <li>
      <Link class={cx('agenda-row', event.estimated && 'is-estimated', event.meter && `agenda-row--${meterTone(event.meter)}`)} href={href}>
        <DayBlock day={event.day} class="agenda-row__day" />
        <span class="agenda-row__icon" aria-hidden="true">
          <Icon size={16} />
        </span>
        <span class="agenda-row__text">
          <span class="agenda-row__title">{eventTitle(event)}</span>
          <span class="agenda-row__detail">{eventDetail(event)}</span>
        </span>
        <Chip tone={near ? 'accent' : 'neutral'} class="agenda-row__when">
          {whenText(event)}
        </Chip>
      </Link>
    </li>
  );
}

/** Everything with nothing ahead, in one line: proof that it is watched,
 *  without a row each. */
export function Steady({ meters }: { meters: MeterView[] }) {
  if (meters.length === 0) return null;
  const shown = meters.slice(0, 4).map((meter) => meter.display);
  const more = meters.length - shown.length;
  return (
    <Link class="steady" href="/meters">
      <CircleCheck class="steady__icon" size={18} />
      <span class="steady__text">
        <span class="steady__title">{t('overview.steady', { n: meters.length })}</span>
        <span class="steady__names">
          {shown.join(t('common.list.separator'))}
          {more > 0 && t('overview.steady.more', { n: more })}
        </span>
      </span>
      <ChevronRight class="steady__go" size={16} />
    </Link>
  );
}

// ---------- the way to everything else, on a phone ----------

/** Where everything that is watched can be looked through, with where it
 *  stands: on a phone, the way from home to the meters and the dates. */
export function Library({ data }: { data: Overview }) {
  const live = data.meters.filter(isLive);
  const look = live.filter(needsLook).length;
  const soon = data.upcoming.filter((reminder) => reminder.days !== null && reminder.days <= 7).length;
  const tiles = [
    { href: '/meters', icon: Gauge, title: t('common.nav.meters'), count: live.length, note: look > 0 ? t('overview.library.look', { n: look }) : t('overview.library.fine') },
    { href: '/dates', icon: CalendarDays, title: t('common.nav.dates'), count: data.counts.reminders, note: soon > 0 ? t('overview.library.soon', { n: soon }) : t('overview.library.quiet') },
  ];
  return (
    <nav class="library" aria-label={t('overview.library')}>
      {tiles.map((tile) => (
        <Link key={tile.href} class="library__tile" href={tile.href}>
          <span class="library__head">
            <tile.icon class="library__icon" size={17} />
            <span class="library__title">{tile.title}</span>
            <ChevronRight class="library__go" size={16} />
          </span>
          <span class="library__count num">{tile.count}</span>
          <span class="library__note">{tile.note}</span>
        </Link>
      ))}
    </nav>
  );
}

// ---------- at the side ----------

export function MonthCard({ spend }: { spend: SpendSummary }) {
  return (
    <Link class="side-card month-card" href="/spend">
      <span class="side-card__head">
        <span class="side-card__title">{t('overview.month.title')}</span>
        <span class="side-card__meta">{formatMonth(spend.month)}</span>
        <ChevronRight class="side-card__go" size={16} />
      </span>
      <Amount class="month-card__total" value={spend.projected + spend.fixed} unit={spend.base} />
      <span class="month-card__label">{t('overview.month.expected')}</span>
      <MonthBar spent={spend.total} projected={spend.projected} fixed={spend.fixed} base={spend.base} />
    </Link>
  );
}

/** Every balance as the time it lasts, on one scale: the one that runs out
 *  first is the shortest bar. Quotas under them, as what is left. */
export function RunwayCard({ meters, horizon }: { meters: MeterView[]; horizon: number }) {
  const live = meters.filter(isLive);
  const balances = live
    .filter((meter) => meter.kind === 'balance')
    .sort((a, b) => (runwayOf(a) ?? Infinity) - (runwayOf(b) ?? Infinity) || a.display.localeCompare(b.display));
  const quotas = live.filter((meter) => meter.kind === 'quota');
  if (balances.length + quotas.length === 0) return null;
  return (
    <section class="side-card runway-card" aria-labelledby="runway-title">
      <header class="side-card__head">
        <h2 class="side-card__title" id="runway-title">
          {t('overview.runway.title')}
        </h2>
        <span class="side-card__meta">{t('overview.runway.scale', { n: horizon })}</span>
      </header>
      <ul class="runway-list">
        {balances.map((meter) => {
          const days = runwayOf(meter);
          return (
            <li key={meter.id}>
              <Link class="runway-item" href={`/meters/${meter.id}`}>
                <span class="runway-item__name truncate">{meter.display}</span>
                <RunwayBar days={days} horizon={horizon} tone={meterTone(meter)} label={days === null ? t('overview.runway.collecting') : runwayText(days)} />
                <span class="runway-item__value num">{days === null ? t('overview.runway.collecting') : formatDays(days)}</span>
              </Link>
            </li>
          );
        })}
      </ul>
      {quotas.length > 0 && (
        <>
          <h3 class="runway-card__sub">{t('overview.quotas.title')}</h3>
          <ul class="runway-list">
            {quotas.map((meter) => (
              <li key={meter.id}>
                <Link class="runway-item" href={`/meters/${meter.id}`}>
                  <span class="runway-item__name truncate">{meter.display}</span>
                  <MeterQuotaBar meter={meter} />
                  <span class="runway-item__value num">{meter.quota ? formatPercent(meter.quota.remaining_pct) : '—'}</span>
                </Link>
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}

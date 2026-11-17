// What the month is made of: the meters, one row each, and the renewals.
//
// The API adds up only what is a currency with an exchange rate. A row it
// could not convert says so, and the total at the foot of the table is the
// headline's own number — so what is and is not in it can be seen.
import { ArrowRight } from 'lucide-preact';
import type { Spend, SpendRenewal, SpendRow } from '../../api/types';
import { BudgetBar, budgetTone } from '../../charts';
import { Amount, Card, Chip, Section } from '../../components';
import { dayOf, formatAmount, formatDay, formatMonth, formatUntil, isCurrency, monthName, parseDay, weekdayName, weekdayOf } from '../../format';
import { lang, t } from '../../i18n';
import { Link } from '../../router';
import { now } from '../../state/clock';
import { daysBetween } from './logic';
import { Fill, SLOT } from './parts';

/** The same amount in the base currency, under the amount in its own. */
function InBase({ value, base }: { value: number | null; base: string }) {
  return (
    <span class="sp-approx">
      <Fill text={t('spend.row.inBase', { amount: SLOT })}>
        <Amount value={value} unit={base} />
      </Fill>
    </span>
  );
}

/** Says why an amount is not in the totals: it is not money, or it is money
 *  with no exchange rate. */
function LeftOut({ unit, base }: { unit: string; base: string }) {
  const money = isCurrency(unit);
  return (
    <Chip title={money ? t('spend.row.noRate.hint', { unit, base }) : t('spend.row.notCounted.hint', { unit })}>
      {t(money ? 'spend.row.noRate' : 'spend.row.notCounted')}
    </Chip>
  );
}

export function MetersTable({ spend }: { spend: Spend }) {
  const { base } = spend;
  const current = spend.is_current;
  const budgets = spend.rows.some((row) => row.budget !== null && row.budget > 0);
  return (
    <Section title={t('spend.meters.title')} meta={<span class="num">{spend.rows.length}</span>}>
      <Card flush>
        {/* The roles restate what the elements are: on a narrow screen the
            rows are laid out as blocks, which would otherwise cost the table
            its meaning for a screen reader. */}
        <table class="sp-table" role="table">
          <caption class="sr-only">{t('spend.meters.caption', { month: formatMonth(spend.month) })}</caption>
          <thead role="rowgroup">
            <tr role="row">
              <th role="columnheader" scope="col">
                {t('spend.col.meter')}
              </th>
              <th role="columnheader" scope="col" class="sp-table__num">
                {t('spend.col.spent')}
              </th>
              {current && (
                <th role="columnheader" scope="col" class="sp-table__num">
                  {t('spend.col.projected')}
                </th>
              )}
              {budgets && (
                <th role="columnheader" scope="col" class="sp-table__budget">
                  {t('spend.col.budget')}
                </th>
              )}
            </tr>
          </thead>
          <tbody role="rowgroup">
            {spend.rows.map((row) => (
              <MeterRow key={row.meter_id} row={row} base={base} current={current} budgets={budgets} />
            ))}
          </tbody>
          <tfoot role="rowgroup">
            <tr role="row">
              <th role="rowheader" scope="row">
                {t('spend.total', { base })}
              </th>
              <td role="cell" class="sp-table__num" data-label={t('spend.col.spent')}>
                <Amount value={spend.total} unit={base} />
              </td>
              {current && (
                <td role="cell" class="sp-table__num" data-label={t('spend.col.projected')}>
                  <Amount value={spend.projected} unit={base} />
                </td>
              )}
              {budgets && <td role="cell" class="sp-table__budget" />}
            </tr>
          </tfoot>
        </table>
      </Card>
    </Section>
  );
}

function MeterRow({ row, base, current, budgets }: { row: SpendRow; base: string; current: boolean; budgets: boolean }) {
  const counted = row.base !== null;
  const foreign = counted && row.unit !== base;
  return (
    <tr role="row">
      <th role="rowheader" scope="row">
        <Link class="sp-table__name" href={`/meters/${row.meter_id}`} title={row.display}>
          {row.display}
        </Link>
      </th>
      <td role="cell" class="sp-table__num" data-label={t('spend.col.spent')}>
        <span class="sp-cell">
          <Amount value={row.spend} unit={row.unit} />
          {foreign && <InBase value={row.base} base={base} />}
          {!counted && <LeftOut unit={row.unit} base={base} />}
        </span>
      </td>
      {current && (
        <td role="cell" class="sp-table__num" data-label={t('spend.col.projected')}>
          <span class="sp-cell">
            <Amount value={row.projected} unit={row.unit} />
            {foreign && <InBase value={row.projected_base} base={base} />}
          </span>
        </td>
      )}
      {budgets && (
        <td role="cell" class="sp-table__budget" data-label={t('spend.col.budget')}>
          {row.budget !== null && row.budget > 0 && <Budget row={row} budget={row.budget} current={current} />}
        </td>
      )}
    </tr>
  );
}

/** A meter's month against its budget: the bar, the figure, and in words
 *  whether it is over or heading over — the bar's colour alone would not do. */
function Budget({ row, budget, current }: { row: SpendRow; budget: number; current: boolean }) {
  // A month that is over is not heading anywhere.
  const projected = current ? row.projected : null;
  const tone = budgetTone(row.spend, projected, budget);
  const amounts = {
    spent: formatAmount(row.spend, row.unit),
    budget: formatAmount(budget, row.unit),
    projected: formatAmount(row.projected, row.unit),
  };
  return (
    <div class="sp-budget">
      <BudgetBar spent={row.spend} projected={projected} budget={budget} label={t(current ? 'spend.bar.budgetProjected' : 'spend.bar.budget', amounts)} />
      <p class="sp-budget__line">
        <span>{t('spend.row.budget', { budget: amounts.budget })}</span>
        {(tone === 'critical' || tone === 'warning') && (
          <span class={`sp-budget__state sp-budget__state--${tone}`}>{t(tone === 'critical' ? 'spend.row.over' : 'spend.row.headingOver')}</span>
        )}
      </p>
    </div>
  );
}

export function Renewals({ spend }: { spend: Spend }) {
  const today = dayOf(now.value);
  return (
    <Section
      title={t('spend.renewals.title')}
      meta={t('spend.renewals.count', { n: spend.renewals.length })}
      action={
        <Link class="more-link" href="/dates">
          {t('spend.renewals.all')}
          <ArrowRight size={14} />
        </Link>
      }
    >
      <Card flush>
        <ol class="sp-renewals">
          {spend.renewals.map((renewal) => (
            <RenewalRow
              key={`${renewal.reminder_id}:${renewal.date}`}
              renewal={renewal}
              base={spend.base}
              // How far off it is only matters in the month still running.
              days={spend.is_current ? daysBetween(today, renewal.date) : null}
            />
          ))}
        </ol>
      </Card>
    </Section>
  );
}

function RenewalRow({ renewal, base, days }: { renewal: SpendRenewal; base: string; days: number | null }) {
  const date = parseDay(renewal.date);
  return (
    <li class="sp-renewal">
      <time class="sp-renewal__date" dateTime={renewal.date} title={formatDay(renewal.date, { weekday: true, year: -1 })}>
        <span class="sp-renewal__day num">{date.day}</span>
        <span class="sp-renewal__month">
          {monthName(date.month)}
          {lang.value === 'zh' ? ' ' : ' · '}
          {weekdayName(weekdayOf(renewal.date))}
        </span>
      </time>
      <span class="sp-renewal__title truncate" title={renewal.title}>
        {renewal.title}
      </span>
      <span class="sp-renewal__amount">
        <span class="sr-only">{t('spend.renewals.amount')} </span>
        <Amount value={renewal.amount} unit={renewal.currency} />
        {renewal.currency !== base && (renewal.base !== null ? <InBase value={renewal.base} base={base} /> : <LeftOut unit={renewal.currency} base={base} />)}
      </span>
      {/* Always there, even when empty: the rows share one grid. */}
      <span class="sp-renewal__when">
        {days !== null && (
          <>
            <span class="sr-only">{t('spend.renewals.when')} </span>
            {days >= 0 ? <Chip tone={days === 0 ? 'accent' : 'neutral'}>{formatUntil(days)}</Chip> : <span class="faint">{formatUntil(days)}</span>}
          </>
        )}
      </span>
    </li>
  );
}

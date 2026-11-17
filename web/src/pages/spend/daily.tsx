// The days of the month as bars: what all the meters together burned each
// day, in the base currency. The axis always spans the whole month, so the
// days still to come show as room on the right.
import { useMemo } from 'preact/hooks';
import type { Spend } from '../../api/types';
import { BarChart, ChartFrame, type Bar, type ChartTable, type LegendEntry } from '../../charts';
import { Card, EmptyState } from '../../components';
import { addDays, dayStart, formatAmount, formatDay, formatMonth, zonedParts } from '../../format';
import { lang, t } from '../../i18n';
import { now, timezone } from '../../state/clock';
import { monthSpan } from './logic';

export function DailyChart({ spend }: { spend: Spend }) {
  const zone = timezone.value;
  const language = lang.value;
  const year = zonedParts(now.value, zone).year;
  const { from, to } = monthSpan(spend.month, zone);
  const month = formatMonth(spend.month);

  const bars: Bar[] = useMemo(
    () =>
      spend.daily.map((day, index) => {
        // The API stops at today, so in the month still running the last day
        // is the one that is not over yet.
        const running = spend.is_current && index === spend.daily.length - 1;
        return {
          start: dayStart(day.date, zone),
          end: dayStart(addDays(day.date, 1), zone),
          value: day.amount,
          label: formatDay(day.date, { weekday: true, year }),
          partial: running,
          note: running ? t('common.chart.today') : undefined,
        };
      }),
    [spend.daily, spend.is_current, zone, year, language],
  );

  let total = 0;
  let peak: Bar | undefined;
  for (const bar of bars) {
    total += bar.value;
    if (!peak || bar.value > peak.value) peak = bar;
  }
  const none = t('spend.daily.none', { month });
  const summary =
    total > 0 && peak
      ? t('spend.daily.summary', { month, total: formatAmount(total, spend.base), peak: formatAmount(peak.value, spend.base), peakLabel: peak.label })
      : none;

  const table = (): ChartTable => ({
    columns: [t('common.chart.date'), t('spend.daily.col')],
    rows: [...spend.daily].reverse().map((day) => [formatDay(day.date, { weekday: true, year }), formatAmount(day.amount, spend.base)]),
  });
  const legend: LegendEntry[] = total > 0 && spend.is_current ? [{ key: 'bar-partial', label: t('common.chart.today') }] : [];

  return (
    <Card class="sp-daily">
      <ChartFrame title={t('spend.daily.title')} legend={legend} table={table}>
        {total > 0 ? <BarChart bars={bars} from={from} to={to} unit={spend.base} height={176} summary={summary} /> : <EmptyState compact title={none} />}
      </ChartFrame>
    </Card>
  );
}

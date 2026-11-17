// A meter's history: the value over time, and under it what was spent per
// day. Both charts share one time axis, so a day in one sits above the same
// day in the other.
import { useMemo } from 'preact/hooks';
import type { Series, SeriesRange } from '../../api/types';
import { BarChart, bucketDays, ChartFrame, chooseBucket, LineChart, useWidth, type Bar, type Bucket, type BucketSize, type ChartTable, type LegendEntry } from '../../charts';
import { PLOT } from '../../charts/axis';
import { Card, EmptyState, Tabs } from '../../components';
import { formatAmount, formatDay, formatDayOf, formatMonth, formatStamp, parseDay, toMs, zonedParts } from '../../format';
import { t, type TKey } from '../../i18n';
import { now, timezone } from '../../state/clock';

export const RANGES: readonly SeriesRange[] = ['24h', '7d', '30d', '90d', '1y'];

const DAY = 86_400_000;

export interface MeterChartsProps {
  series: Series;
  /** The range asked for; the series may still be the previous one's. */
  range: SeriesRange;
  onRange: (range: SeriesRange) => void;
  /** Newer data is loading: the charts dim instead of going blank. */
  refreshing: boolean;
}

export function MeterCharts({ series, range, onRange, refreshing }: MeterChartsProps) {
  const { meter } = series;
  const unit = meter.unit;
  const until = toMs(series.to);

  const points = useMemo(() => series.points.map((point) => ({ t: toMs(point.t), v: point.v })), [series.points]);
  const markers = useMemo(
    () => series.top_ups.map((event) => ({ t: toMs(event.at), label: t('meters.chart.topUp', { amount: formatAmount(event.amount, unit) }) })),
    [series.top_ups, unit],
  );

  // A meter younger than the range would leave most of the axis empty, so
  // the axis starts at its first reading instead — but never so late that a
  // single reading stretches minutes across the whole chart.
  const asked = toMs(series.from);
  const first = points[0]?.t ?? asked;
  const from = Math.max(asked, Math.min(first, until - (until - asked) / 10));

  // The axis runs on past now when there is somewhere the line is heading:
  // as far as the projection goes, up to half the width of the history.
  const projection = useMemo(() => {
    const line = series.projection.map((point) => ({ t: toMs(point.t), v: point.v }));
    const [start, end] = line;
    const horizon = until + (until - from) / 2;
    if (!start || !end || end.t <= horizon || end.t <= start.t) return line;
    const share = (horizon - start.t) / (end.t - start.t);
    return [start, { t: horizon, v: start.v + (end.v - start.v) * share }];
  }, [series.projection, until, from]);
  const projectionEnd = projection[projection.length - 1];
  const to = Math.max(until, projectionEnd?.t ?? until);

  // A threshold means nothing for a running total; the engine ignores it there.
  const threshold = meter.rules.low === null || meter.kind === 'counter' ? null : { value: meter.rules.low, label: t('meters.chart.threshold', { amount: formatAmount(meter.rules.low, unit) }) };

  // The title names the line; the legend explains what else is drawn.
  const legend: LegendEntry[] = [];
  if (projectionEnd) {
    legend.push({
      key: 'projection',
      label: projectionEnd.v <= 0 ? t('meters.chart.projection.depletes', { date: formatDayOf(projectionEnd.t) }) : t('meters.chart.projection'),
    });
  }
  if (threshold) legend.push({ key: 'threshold', label: threshold.label });
  if (markers.length > 0) legend.push({ key: 'marker', label: t('meters.chart.topUps') });

  let summary = t('meters.chart.summary.none');
  if (points.length > 0) {
    let low = Infinity;
    let high = -Infinity;
    for (const point of points) {
      low = Math.min(low, point.v);
      high = Math.max(high, point.v);
    }
    summary = t('meters.chart.summary', {
      count: points.length,
      from: formatStamp(points[0]!.t),
      to: formatStamp(points[points.length - 1]!.t),
      latest: formatAmount(points[points.length - 1]!.v, unit),
      low: formatAmount(low, unit),
      high: formatAmount(high, unit),
    });
  }

  const table = (): ChartTable => ({
    columns: [t('common.chart.time'), t(`meters.chart.${meter.kind}`)],
    rows: [...points].reverse().map((point) => [formatStamp(point.t), formatAmount(point.v, unit)]),
  });

  return (
    <section class="mt-charts" aria-label={t(`meters.chart.${meter.kind}`)}>
      <div class="mt-charts__range">
        <Tabs
          label={t('meters.range.label')}
          tabs={RANGES.map((each) => ({ id: each, label: t(`meters.range.${each}`), name: t(`meters.range.${each}.name`) }))}
          value={range}
          onChange={onRange}
        />
      </div>
      <Card>
        <ChartFrame title={t(`meters.chart.${meter.kind}`)} legend={legend} table={table} refreshing={refreshing}>
          {points.length === 0 ? (
            <EmptyState compact title={t('common.chart.empty')} />
          ) : (
            <>
              <LineChart
                points={points}
                from={from}
                to={to}
                projection={projection}
                threshold={threshold}
                markers={markers}
                unit={unit}
                ceiling={meter.kind === 'quota' ? meter.limit : null}
                summary={summary}
              />
              {points.length === 1 && <p class="mt-charts__note">{t('meters.chart.single')}</p>}
            </>
          )}
        </ChartFrame>
      </Card>
      {meter.kind !== 'quota' && series.daily.length > 0 && <SpendChart series={series} from={from} until={until} to={to} refreshing={refreshing} />}
    </section>
  );
}

const SPEND_TITLE: Record<BucketSize, TKey> = { day: 'meters.chart.spend.day', week: 'meters.chart.spend.week', month: 'meters.chart.spend.month' };
const SPEND_PARTIAL: Record<BucketSize, TKey> = { day: 'meters.chart.partial.day', week: 'meters.chart.partial.week', month: 'meters.chart.partial.month' };
const SPEND_RUNNING: Record<BucketSize, TKey> = { day: 'meters.chart.spend.today', week: 'meters.chart.spend.thisWeek', month: 'meters.chart.spend.thisMonth' };

/** Names a bucket: a day, a span of days, or a month. */
function bucketLabel(bucket: Bucket, size: BucketSize, year: number): string {
  if (size === 'day') return formatDay(bucket.firstDay, { weekday: true, year });
  if (size === 'month') {
    const { year: y, month } = parseDay(bucket.firstDay);
    return formatMonth(`${y}-${String(month).padStart(2, '0')}`);
  }
  if (bucket.firstDay === bucket.lastDay) return formatDay(bucket.firstDay, { year });
  return `${formatDay(bucket.firstDay, { year })} – ${formatDay(bucket.lastDay, { year })}`;
}

function SpendChart({ series, from, until, to, refreshing }: { series: Series; from: number; until: number; to: number; refreshing: boolean }) {
  const unit = series.meter.unit;
  const zone = timezone.value;
  // The bars are as fine as the width allows: days where a day is wide
  // enough to see, weeks or months where it is not.
  const [ref, width] = useWidth<HTMLDivElement>();
  const plot = Math.max(1, width - PLOT.left - PLOT.right);
  const size = chooseBucket(plot / Math.max(1, (to - from) / DAY));

  const buckets = useMemo(() => bucketDays(series.daily, size, from, until, zone), [series.daily, size, from, until, zone]);
  const year = zonedParts(now.value, zone).year;
  const bars: Bar[] = useMemo(
    () =>
      buckets.map((bucket) => ({
        start: bucket.start,
        end: bucket.end,
        value: bucket.value,
        label: bucketLabel(bucket, size, year),
        partial: bucket.partial,
        note: bucket.current ? t(SPEND_RUNNING[size]) : bucket.partial ? t(SPEND_PARTIAL[size]) : undefined,
      })),
    [buckets, size, year, t(SPEND_RUNNING[size])],
  );

  let total = 0;
  let peak: Bar | undefined;
  for (const bar of bars) {
    total += bar.value;
    if (!peak || bar.value > peak.value) peak = bar;
  }
  const first = series.daily[0];
  const last = series.daily[series.daily.length - 1];
  const summary =
    total > 0 && peak && first && last
      ? t('meters.chart.spend.summary', {
          total: formatAmount(total, unit),
          from: formatDay(first.date, { year }),
          to: formatDay(last.date, { year }),
          peak: formatAmount(peak.value, unit),
          peakLabel: peak.label,
        })
      : t('meters.chart.spend.none');

  const toppedUp = size === 'day' && series.daily.some((day) => day.top_up > 0);
  const table = (): ChartTable => {
    if (size === 'day') {
      return {
        columns: [t('common.chart.date'), t('meters.chart.col.spend'), ...(toppedUp ? [t('meters.chart.col.topUp')] : [])],
        rows: [...series.daily].reverse().map((day) => [
          formatDay(day.date, { weekday: true, year }),
          formatAmount(day.spend, unit),
          ...(toppedUp ? [day.top_up > 0 ? formatAmount(day.top_up, unit) : ''] : []),
        ]),
      };
    }
    return {
      columns: [t('meters.chart.col.period'), t('meters.chart.col.spend')],
      rows: [...bars].reverse().map((bar) => [bar.label, formatAmount(bar.value, unit)]),
    };
  };

  const legend: LegendEntry[] = total > 0 && bars.some((bar) => bar.partial) ? [{ key: 'bar-partial', label: t(SPEND_PARTIAL[size]) }] : [];

  return (
    <Card>
      <ChartFrame title={t(SPEND_TITLE[size])} legend={legend} table={table} refreshing={refreshing}>
        <div ref={ref}>
          {total > 0 ? <BarChart bars={bars} from={from} to={to} unit={unit} summary={summary} /> : <EmptyState compact title={t('meters.chart.spend.none')} />}
        </div>
      </ChartFrame>
    </Card>
  );
}

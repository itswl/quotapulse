import type { TargetedKeyboardEvent } from 'preact';
import { useMemo, useState } from 'preact/hooks';
import { cx } from '../components/cx';
import { formatAmount } from '../format';
import { t } from '../i18n';
import { axisTicks, PLOT, XAxis, YGrid } from './axis';
import { linear, niceTicks } from './scale';
import { useWidth } from './useSize';

export interface Bar {
  /** The period the bar covers, in epoch milliseconds; it is drawn there on
   *  the time axis. */
  start: number;
  end: number;
  value: number;
  /** Names the period: "Nov 16", "Nov 10 – 16". */
  label: string;
  /** The period is incomplete — today, so far. Drawn lighter. */
  partial?: boolean;
  /** A word about the bar for its readout: "today so far". */
  note?: string | undefined;
}

export interface BarChartProps {
  bars: readonly Bar[];
  /** The edges of the time axis. Give the chart above the same ones and
   *  the two line up day for day. */
  from: number;
  to: number;
  unit: string;
  height?: number;
  /** The chart in a sentence, for a screen reader. */
  summary: string;
}

const MAX_THICKNESS = 18;
const GAP = 2;
const ROUND = 4;

/**
 * Amounts per period as thin bars on a time axis. Each bar sits over the
 * time it covers, grows from the baseline, and is rounded at the data end
 * only. The whole column is the hover target, not just the bar.
 */
export function BarChart({ bars, from, to, unit, height = 150, summary }: BarChartProps) {
  const [ref, width] = useWidth<HTMLDivElement>();
  const [active, setActive] = useState<number | null>(null);

  const x0 = PLOT.left;
  const x1 = Math.max(x0 + 1, width - PLOT.right);
  const y0 = PLOT.top;
  const y1 = height - PLOT.bottom;

  const model = useMemo(() => {
    let top = 0;
    for (const bar of bars) top = Math.max(top, bar.value);
    const yTicks = niceTicks(top, 2);
    const x = linear(from, to, x0, x1);
    const y = linear(0, yTicks[yTicks.length - 1]!, y1, y0);
    const shapes = bars.map((bar) => {
      const left = Math.max(x(bar.start), x0);
      const right = Math.min(x(bar.end), x1);
      const thickness = Math.min(MAX_THICKNESS, Math.max(1, right - left - GAP));
      const centre = (left + right) / 2;
      // Something was spent: never let the bar round down to nothing.
      const topY = bar.value > 0 ? Math.min(y(bar.value), y1 - 1) : y1;
      return { left, right, centre, thickness, topY, path: barPath(centre - thickness / 2, topY, thickness, y1) };
    });
    return { x, y, yTicks, shapes, xTicks: axisTicks(from, to, x1 - x0) };
  }, [bars, from, to, x0, x1, y0, y1]);

  const pick = (clientX: number, element: HTMLElement) => {
    const px = clientX - element.getBoundingClientRect().left + x0;
    let best = -1;
    let distance = Infinity;
    model.shapes.forEach((shape, index) => {
      const away = px < shape.left ? shape.left - px : px > shape.right ? px - shape.right : 0;
      if (away < distance) {
        distance = away;
        best = index;
      }
    });
    setActive(best >= 0 && distance <= 12 ? best : null);
  };

  const onKeyDown = (event: TargetedKeyboardEvent<HTMLDivElement>) => {
    if (bars.length === 0) return;
    const index = active ?? bars.length - 1;
    const moves: Record<string, number> = { ArrowLeft: index - 1, ArrowRight: index + 1, Home: 0, End: bars.length - 1 };
    const next = moves[event.key];
    if (next === undefined) return;
    event.preventDefault();
    setActive(Math.min(bars.length - 1, Math.max(0, next)));
  };

  const bar = active === null ? undefined : bars[active];
  const shape = active === null ? undefined : model.shapes[active];
  const readout = bar ? `${bar.label}${t('common.list.separator')}${formatAmount(bar.value, unit)}` : '';

  return (
    <div class="chart" ref={ref} style={{ height: `${height}px` }}>
      {width > 0 && (
        <svg width={width} height={height} role="img" aria-label={summary}>
          <YGrid ticks={model.yTicks} y={model.y} x0={x0} x1={x1} />
          {shape && <rect class="chart__band" x={shape.left} y={y0} width={Math.max(1, shape.right - shape.left)} height={y1 - y0} />}
          <g aria-hidden="true">
            {model.shapes.map((each, index) =>
              each.path ? <path key={bars[index]!.start} class={cx('chart__bar', bars[index]!.partial && 'chart__bar--partial')} d={each.path} /> : null,
            )}
          </g>
          <XAxis ticks={model.xTicks} x={model.x} y={y1} x0={x0} x1={x1} />
        </svg>
      )}
      {width > 0 && bars.length > 0 && (
        <div
          class="chart__hit"
          style={{ left: `${x0}px`, top: `${y0}px`, width: `${x1 - x0}px`, height: `${y1 - y0}px` }}
          role="slider"
          tabIndex={0}
          aria-label={t('common.chart.inspect')}
          aria-valuemin={0}
          aria-valuemax={bars.length - 1}
          aria-valuenow={active ?? bars.length - 1}
          aria-valuetext={readout || undefined}
          onPointerMove={(event) => pick(event.clientX, event.currentTarget)}
          onPointerDown={(event) => pick(event.clientX, event.currentTarget)}
          onPointerLeave={(event) => {
            if (event.pointerType !== 'touch') setActive(null);
          }}
          onFocus={() => setActive((was) => was ?? bars.length - 1)}
          onBlur={() => setActive(null)}
          onKeyDown={onKeyDown}
        />
      )}
      {bar && shape && (
        <div
          // Always above the bar: below it would cover the bar itself.
          class={cx('chart-tip', shape.centre < 96 && 'chart-tip--start', shape.centre > width - 96 && 'chart-tip--end')}
          style={{ left: `${Math.round(shape.centre)}px`, top: `${Math.round(shape.topY)}px` }}
          aria-hidden="true"
        >
          <span class="chart-tip__value num">{formatAmount(bar.value, unit)}</span>
          <span class="chart-tip__when">{bar.label}</span>
          {bar.note && <span class="chart-tip__note">{bar.note}</span>}
        </div>
      )}
    </div>
  );
}

/** A bar from the baseline up: square at the bottom, rounded at the top. */
function barPath(left: number, top: number, width: number, bottom: number): string {
  const height = bottom - top;
  if (!(height > 0)) return '';
  const r = Math.min(ROUND, width / 2, height);
  const f = (value: number) => value.toFixed(1);
  return (
    `M${f(left)} ${f(bottom)}V${f(top + r)}Q${f(left)} ${f(top)} ${f(left + r)} ${f(top)}` +
    `H${f(left + width - r)}Q${f(left + width)} ${f(top)} ${f(left + width)} ${f(top + r)}V${f(bottom)}Z`
  );
}

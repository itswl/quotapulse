import type { JSX } from 'preact';
import { useMemo, useState } from 'preact/hooks';
import { cx } from '../components/cx';
import { formatAmount, formatStamp } from '../format';
import { t } from '../i18n';
import { axisTicks, PLOT, XAxis, YGrid } from './axis';
import { linear, nearest, niceTicks } from './scale';
import { useWidth } from './useSize';

export interface LinePoint {
  /** epoch milliseconds */
  t: number;
  v: number;
}

export interface LineMarker {
  t: number;
  /** What happened there: "Topped up 50.00 USD". */
  label: string;
}

export interface LineChartProps {
  /** The history, oldest first. */
  points: readonly LinePoint[];
  /** The left edge of the time axis. */
  from: number;
  /** The right edge. Later than the last point when there is a projection. */
  to: number;
  /** A continuation of the line into the future, drawn dashed. */
  projection?: readonly LinePoint[];
  /** A level worth marking, drawn as a labelled dashed line. */
  threshold?: { value: number; label: string } | null | undefined;
  /** Events marked on the line, such as top-ups. */
  markers?: readonly LineMarker[];
  unit: string;
  /** A known ceiling the axis should reach: a quota's limit. */
  ceiling?: number | null | undefined;
  height?: number;
  /** The chart in a sentence, for a screen reader. */
  summary: string;
}

type Active = { kind: 'point'; index: number } | { kind: 'projection'; t: number; v: number };

const NONE: readonly never[] = [];

/**
 * A value over time: a line with a soft area under it, on a value axis that
 * starts at zero. Hovering, touching or the arrow keys move a crosshair that
 * reads out the nearest sample; in the dashed part it reads the projection.
 */
export function LineChart({ points, from, to, projection = NONE, threshold, markers = NONE, unit, ceiling, height = 240, summary }: LineChartProps) {
  const [ref, width] = useWidth<HTMLDivElement>();
  const [active, setActive] = useState<Active | null>(null);

  const x0 = PLOT.left;
  const x1 = Math.max(x0 + 1, width - PLOT.right);
  const y0 = PLOT.top;
  const y1 = height - PLOT.bottom;

  const model = useMemo(() => {
    let top = Math.max(ceiling ?? 0, threshold?.value ?? 0);
    for (const point of points) top = Math.max(top, point.v);
    for (const point of projection) top = Math.max(top, point.v);
    const yTicks = niceTicks(top, height < 200 ? 3 : 4);
    const x = linear(from, to, x0, x1);
    const y = linear(0, yTicks[yTicks.length - 1]!, y1, y0);
    let line = '';
    for (const point of points) line += `${line ? 'L' : 'M'}${x(point.t).toFixed(1)} ${y(point.v).toFixed(1)}`;
    const first = points[0];
    const last = points[points.length - 1];
    const area = first && last && points.length > 1 ? `${line}L${x(last.t).toFixed(1)} ${y1}L${x(first.t).toFixed(1)} ${y1}Z` : '';
    const times = points.map((point) => point.t);
    const marks = markers
      .map((marker) => ({ marker, index: nearest(times, marker.t) }))
      .filter((mark) => mark.index >= 0);
    return { x, y, yTicks, line, area, times, marks, xTicks: axisTicks(from, to, x1 - x0) };
  }, [points, projection, markers, threshold?.value, ceiling, from, to, x0, x1, y0, y1, height]);

  const { x, y } = model;
  const last = points[points.length - 1];
  const projectionEnd = projection[projection.length - 1];
  const projectionStart = projection[0];

  const current: (LinePoint & { projected: boolean; note?: string | undefined }) | null = (() => {
    if (!active) return null;
    if (active.kind === 'projection') return { t: active.t, v: active.v, projected: true };
    const point = points[Math.min(active.index, points.length - 1)];
    if (!point) return null;
    const mark = model.marks.find((candidate) => candidate.index === active.index);
    return { ...point, projected: false, note: mark?.marker.label };
  })();

  const pick = (clientX: number, element: HTMLElement) => {
    if (points.length === 0) return;
    const box = element.getBoundingClientRect();
    const px = Math.min(Math.max(clientX - box.left, 0), box.width);
    const at = from + (px / Math.max(box.width, 1)) * (to - from);
    if (last && projectionStart && projectionEnd && at > last.t && projectionEnd.t > projectionStart.t) {
      const clamped = Math.min(at, projectionEnd.t);
      const share = (clamped - projectionStart.t) / (projectionEnd.t - projectionStart.t);
      setActive({ kind: 'projection', t: clamped, v: projectionStart.v + (projectionEnd.v - projectionStart.v) * share });
      return;
    }
    // A marker is a small target on a dense line: within reach, it wins.
    const sticky = model.marks.find((mark) => Math.abs(x(points[mark.index]!.t) - (x0 + px)) <= 10);
    setActive({ kind: 'point', index: sticky ? sticky.index : nearest(model.times, at) });
  };

  const onKeyDown = (event: JSX.TargetedKeyboardEvent<HTMLDivElement>) => {
    if (points.length === 0) return;
    const index = active?.kind === 'point' ? active.index : points.length - 1;
    const jump = Math.max(1, Math.round(points.length / 10));
    const moves: Record<string, number> = {
      ArrowLeft: index - 1,
      ArrowRight: index + 1,
      PageDown: index - jump,
      PageUp: index + jump,
      Home: 0,
      End: points.length - 1,
    };
    const next = moves[event.key];
    if (next === undefined) return;
    event.preventDefault();
    setActive({ kind: 'point', index: Math.min(points.length - 1, Math.max(0, next)) });
  };

  const readout = current ? `${formatStamp(current.t)}${t('common.list.separator')}${formatAmount(current.v, unit)}` : '';
  const tipX = current ? x(current.t) : 0;
  const tipY = current ? y(current.v) : 0;

  return (
    <div class="chart" ref={ref} style={{ height: `${height}px` }}>
      {width > 0 && (
        <svg width={width} height={height} role="img" aria-label={summary}>
          <YGrid ticks={model.yTicks} y={y} x0={x0} x1={x1} />
          <XAxis ticks={model.xTicks} x={x} y={y1} x0={x0} x1={x1} />
          {model.area && <path class="chart__area" d={model.area} />}
          {threshold && (
            <g aria-hidden="true">
              <line class="chart__threshold" x1={x0} x2={x1} y1={Math.round(y(threshold.value)) + 0.5} y2={Math.round(y(threshold.value)) + 0.5} />
              {/* On the left, where the past usually leaves room above the line. */}
              <text class="chart__note" x={x0 + 6} y={y(threshold.value) - 6}>
                {threshold.label}
              </text>
            </g>
          )}
          {projectionStart && projectionEnd && (
            <line
              class="chart__projection"
              aria-hidden="true"
              x1={x(projectionStart.t)}
              y1={y(projectionStart.v)}
              x2={x(projectionEnd.t)}
              y2={y(projectionEnd.v)}
            />
          )}
          {model.line && points.length > 1 && <path class="chart__line" d={model.line} />}
          {model.marks.map((mark) => {
            const point = points[mark.index]!;
            return <circle key={mark.marker.t} class="chart__marker" cx={x(point.t)} cy={y(point.v)} r="4" />;
          })}
          {last && <circle class="chart__end" cx={x(last.t)} cy={y(last.v)} r="3.5" />}
          {current && (
            <g aria-hidden="true">
              <line class="chart__crosshair" x1={Math.round(tipX) + 0.5} x2={Math.round(tipX) + 0.5} y1={y0} y2={y1} />
              <circle class={cx('chart__focus', current.projected && 'chart__focus--projected')} cx={tipX} cy={tipY} r="4.5" />
            </g>
          )}
        </svg>
      )}
      {width > 0 && points.length > 0 && (
        <div
          class="chart__hit"
          style={{ left: `${x0}px`, top: `${y0}px`, width: `${x1 - x0}px`, height: `${y1 - y0}px` }}
          role="slider"
          tabIndex={0}
          aria-label={t('common.chart.inspect')}
          aria-valuemin={0}
          aria-valuemax={points.length - 1}
          aria-valuenow={active?.kind === 'point' ? active.index : points.length - 1}
          aria-valuetext={readout || undefined}
          onPointerMove={(event) => pick(event.clientX, event.currentTarget)}
          onPointerDown={(event) => pick(event.clientX, event.currentTarget)}
          onPointerLeave={(event) => {
            // A finger lifting is not the pointer leaving: keep the readout
            // until the chart loses focus.
            if (event.pointerType !== 'touch') setActive(null);
          }}
          onFocus={() => setActive((was) => was ?? { kind: 'point', index: points.length - 1 })}
          onBlur={() => setActive(null)}
          onKeyDown={onKeyDown}
        />
      )}
      {current && (
        <div
          class={cx('chart-tip', tipX < 96 && 'chart-tip--start', tipX > width - 96 && 'chart-tip--end', tipY < 64 && 'chart-tip--below')}
          style={{ left: `${Math.round(tipX)}px`, top: `${Math.round(tipY)}px` }}
          aria-hidden="true"
        >
          <span class="chart-tip__value num">{formatAmount(current.v, unit)}</span>
          {current.projected && <span class="chart-tip__tag">{t('common.chart.projected')}</span>}
          <span class="chart-tip__when">{formatStamp(current.t)}</span>
          {current.note && <span class="chart-tip__note">{current.note}</span>}
        </div>
      )}
    </div>
  );
}

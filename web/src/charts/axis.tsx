// The furniture two charts share: where the plot sits, the value grid, and
// the time axis. LineChart and BarChart use the same margins and the same
// ticks, so stacked one above the other their days line up.
import { compactNumber, dayOf, formatDay, formatTime, monthName, parseDay } from '../format';
import { lang } from '../i18n';
import { timezone } from '../state/clock';
import { timeTicks, type TimeTick } from './scale';

/** Room around the plot: value labels on the left, time labels below. */
export const PLOT = { left: 44, right: 14, top: 14, bottom: 24 };

/** How much larger than 16px the root type is: a phone steps the whole
 *  scale up (styles/base.css), and the labels grow with it. */
function typeScale(): number {
  if (typeof document === 'undefined') return 1;
  return (parseFloat(getComputedStyle(document.documentElement).fontSize) || 16) / 16;
}

/** Roughly how wide a label is at the axis size, without measuring it. */
function textWidth(text: string, scale: number): number {
  let width = 0;
  for (const char of text) width += char.charCodeAt(0) > 0x2e7f ? 11 : 6.3;
  return width * scale;
}

export function timeLabel(tick: TimeTick, zone: string = timezone.value): string {
  const day = dayOf(tick.t, zone);
  if (tick.unit === 'hour') return tick.major ? formatDay(day) : formatTime(tick.t, zone);
  if (tick.unit === 'day') return formatDay(day);
  const { year, month } = parseDay(day);
  if (tick.major) return lang.value === 'zh' ? `${year}年` : String(year);
  return monthName(month);
}

/** The ticks of a time axis that fit a plot of the given width: one for
 *  every 64px or so. That is closer than the longest label is wide plus
 *  air, which is what lets a phone's plot have three or four of them;
 *  XAxis leaves out any label that would touch its neighbour. */
export function axisTicks(from: number, to: number, plotWidth: number): TimeTick[] {
  return timeTicks(from, to, Math.max(2, Math.floor(plotWidth / 64)), timezone.value);
}

export interface XAxisProps {
  ticks: readonly TimeTick[];
  x: (t: number) => number;
  /** The y of the baseline. */
  y: number;
  x0: number;
  x1: number;
}

/** The baseline and the time labels under it. A label that would run off
 *  the plot or into its neighbour is left out; its tick mark stays. */
export function XAxis({ ticks, x, y, x0, x1 }: XAxisProps) {
  let free = x0 - 20;
  const scale = typeScale();
  return (
    <g class="chart__axis" aria-hidden="true">
      <line class="chart__baseline" x1={x0} x2={x1} y1={y + 0.5} y2={y + 0.5} />
      {ticks.map((tick) => {
        const at = x(tick.t);
        const label = timeLabel(tick);
        const half = textWidth(label, scale) / 2;
        const fits = at - half >= free && at + half <= x1 + PLOT.right;
        if (fits) free = at + half + 10;
        return (
          <g key={tick.t}>
            <line class="chart__tick" x1={Math.round(at) + 0.5} x2={Math.round(at) + 0.5} y1={y + 1} y2={y + 4} />
            {fits && (
              <text class="chart__label" x={at} y={y + 17} text-anchor="middle">
                {label}
              </text>
            )}
          </g>
        );
      })}
    </g>
  );
}

export interface YGridProps {
  ticks: readonly number[];
  y: (value: number) => number;
  x0: number;
  x1: number;
}

/** Hairlines across the plot with their values on the left. Zero is left to
 *  the baseline. */
export function YGrid({ ticks, y, x0, x1 }: YGridProps) {
  return (
    <g class="chart__grid" aria-hidden="true">
      {ticks.map((value) => {
        const at = Math.round(y(value)) + 0.5;
        return (
          <g key={value}>
            {value !== 0 && <line class="chart__gridline" x1={x0} x2={x1} y1={at} y2={at} />}
            <text class="chart__label num" x={x0 - 8} y={at} text-anchor="end" dominant-baseline="middle">
              {compactNumber(value)}
            </text>
          </g>
        );
      })}
    </g>
  );
}

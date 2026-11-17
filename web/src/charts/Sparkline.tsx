import { useId, useMemo } from 'preact/hooks';
import { cx } from '../components/cx';

export interface SparkPoint {
  /** epoch milliseconds */
  t: number;
  v: number;
}

export interface SparklineProps {
  points: readonly SparkPoint[];
  /** The value at the top of the chart. Defaults to the largest point. The
   *  bottom is always zero: for something that runs out, how close the line
   *  is to the floor is the whole message. */
  max?: number;
  /** Colours the end dot: where the series stands now. */
  tone?: 'accent' | 'warning' | 'critical' | 'neutral';
  /** Says in words what the line shows; a sparkline has no axes to read. */
  label: string;
  class?: string;
}

// The line is drawn in a 100×100 box stretched to whatever size CSS gives
// the element, so it needs no measuring. The stroke does not stretch with
// it, and the end dot is an element of its own, so it stays round.
const TOP = 8;
const BOTTOM = 96;

/** A small line for "which way is this going". Needs at least two points. */
export function Sparkline({ points, max, tone = 'accent', label, class: extra }: SparklineProps) {
  const fade = useId();
  const shape = useMemo(() => {
    if (points.length < 2) return null;
    const first = points[0]!;
    const last = points[points.length - 1]!;
    const span = last.t - first.t;
    if (!(span > 0)) return null;
    let top = max ?? 0;
    if (max === undefined) for (const point of points) top = Math.max(top, point.v);
    if (!(top > 0)) top = 1;
    const x = (t: number) => ((t - first.t) / span) * 100;
    const y = (v: number) => BOTTOM - (Math.min(Math.max(v, 0), top) / top) * (BOTTOM - TOP);
    let line = '';
    for (const point of points) line += `${line ? 'L' : 'M'}${x(point.t).toFixed(2)} ${y(point.v).toFixed(2)}`;
    return { line, area: `${line}L100 100L0 100Z`, endY: y(last.v) };
  }, [points, max]);

  if (!shape) return null;
  return (
    <span class={cx('spark', `spark--${tone}`, extra)} role="img" aria-label={label}>
      <span class="spark__plot">
        <svg viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
          <defs>
            <linearGradient id={fade} x1="0" y1="0" x2="0" y2="1">
              <stop class="spark__fade-top" offset="0" />
              <stop class="spark__fade-bottom" offset="1" />
            </linearGradient>
          </defs>
          <path d={shape.area} fill={`url(#${fade})`} />
          <path class="spark__line" d={shape.line} vector-effect="non-scaling-stroke" />
        </svg>
        <span class="spark__dot" style={{ top: `${shape.endY}%` }} />
      </span>
    </span>
  );
}

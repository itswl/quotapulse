import { cx } from '../components/cx';

export type BarTone = 'accent' | 'warning' | 'critical' | 'neutral';

export interface QuotaBarProps {
  /** Share of the allowance that is left, 0-100. */
  remaining: number;
  /** Share that would be left at an even pace: 100 minus the share of the
   *  window that has passed. Drawn as a tick; omit when the window is unknown. */
  pace?: number | null;
  tone?: BarTone;
  /** The whole reading in words: "38% left; an even pace would leave 32%". */
  label: string;
  class?: string;
}

const clamp = (value: number) => Math.min(100, Math.max(0, value));

/**
 * What is left of an allowance, as a bar that empties to the left. The tick
 * marks where the bar would end if the allowance were being used evenly
 * across its window: a bar that stops short of the tick is being used up
 * faster than time is passing.
 */
export function QuotaBar({ remaining, pace, tone = 'accent', label, class: extra }: QuotaBarProps) {
  const fill = clamp(remaining);
  return (
    <svg class={cx('meterbar', `meterbar--${tone}`, extra)} role="img" aria-label={label} width="100%" height="16">
      <rect class="meterbar__track" x="0" y="4" width="100%" height="8" rx="4" />
      {fill > 0 && <rect class="meterbar__fill" x="0" y="4" width={`${fill}%`} height="8" rx="4" />}
      {pace != null && <Tick at={clamp(pace)} />}
    </svg>
  );
}

/** A reference mark across a bar, ringed in the surface colour so it reads
 *  over the fill and the track alike. */
export function Tick({ at }: { at: number }) {
  return (
    <>
      <line class="meterbar__tick-ring" x1={`${at}%`} x2={`${at}%`} y1="0" y2="16" />
      <line class="meterbar__tick" x1={`${at}%`} x2={`${at}%`} y1="1" y2="15" />
    </>
  );
}

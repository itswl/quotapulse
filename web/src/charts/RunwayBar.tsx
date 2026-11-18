import { cx } from '../components/cx';
import type { BarTone } from './QuotaBar';

export interface RunwayBarProps {
  /** Days the balance lasts at its expected burn; null when that is not
   *  known yet. */
  days: number | null;
  /** The days the full width stands for. */
  horizon: number;
  tone?: BarTone;
  /** The reading in words: "about 13 days left". */
  label: string;
  class?: string;
}

/**
 * How long a balance lasts, as a length of time: the bar runs from today to
 * the day it runs out, on a scale every balance shares, so the one that runs
 * out first is the shortest. A balance that lasts past the scale fills it and
 * fades out at the end, to say that it goes on.
 */
export function RunwayBar({ days, horizon, tone = 'accent', label, class: extra }: RunwayBarProps) {
  const known = days !== null && days >= 0;
  const share = known ? Math.min(1, days / horizon) : 0;
  return (
    <span class={cx('runway', `runway--${tone}`, known && days > horizon && 'runway--beyond', !known && 'runway--unknown', extra)} role="img" aria-label={label}>
      <span class="runway__fill" style={{ width: `${(share * 100).toFixed(2)}%` }} />
    </span>
  );
}

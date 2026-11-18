import { amountParts, DASH } from '../format';
import { cx } from './cx';

export interface AmountProps {
  /** null or undefined shows a dash: no reading yet. */
  value: number | null | undefined;
  unit: string;
  class?: string;
}

/** A number with its unit after it, smaller and quieter: 1,234.56 CNY ·
 *  38% · 923 calls. The digits are of equal width, so amounts line up in a
 *  column and do not shift as they change. */
export function Amount({ value, unit, class: extra }: AmountProps) {
  if (value === null || value === undefined) return <span class={cx('num', extra)}>{DASH}</span>;
  const parts = amountParts(value, unit);
  return (
    <span class={cx('num', extra)}>
      {parts.number}
      {parts.unit !== '' && <span class={cx('unit', parts.tight ? 'unit--tight' : 'unit--spaced')}>{parts.unit}</span>}
    </span>
  );
}

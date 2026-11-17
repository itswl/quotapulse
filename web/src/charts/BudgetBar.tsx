import { cx } from '../components/cx';
import { Tick, type BarTone } from './QuotaBar';

export interface BudgetBarProps {
  spent: number;
  /** Where the month is heading; drawn as a tick. */
  projected?: number | null;
  /** The full width of the bar. */
  budget: number;
  /** The whole reading in words: "69.49 of 100.00 USD spent; heading for 132.41". */
  label: string;
  class?: string;
}

/** The tone a budget is in: over it, heading over it, or within it. */
export function budgetTone(spent: number, projected: number | null | undefined, budget: number): BarTone {
  if (spent > budget) return 'critical';
  if (projected != null && projected > budget) return 'warning';
  return 'accent';
}

/**
 * This month's spend against a budget. The bar's full width is the budget,
 * the fill is what has been spent, and the tick is where the month is
 * heading. A projection past the budget cannot be drawn to scale: arrows
 * past the bar's end say that it runs on.
 */
export function BudgetBar({ spent, projected, budget, label, class: extra }: BudgetBarProps) {
  if (!(budget > 0)) return null;
  const fill = Math.min(100, Math.max(0, (spent / budget) * 100));
  const mark = projected == null ? null : Math.min(100, Math.max(0, (projected / budget) * 100));
  const beyond = projected != null && projected > budget;
  return (
    <svg
      class={cx('meterbar', `meterbar--${budgetTone(spent, projected, budget)}`, beyond && 'meterbar--beyond', extra)}
      role="img"
      aria-label={label}
      width="100%"
      height="16"
    >
      <rect class="meterbar__track" x="0" y="4" width="100%" height="8" rx="4" />
      {fill > 0 && <rect class="meterbar__fill" x="0" y="4" width={`${fill}%`} height="8" rx="4" />}
      {mark !== null && !beyond && <Tick at={mark} />}
      {beyond && (
        // Past the bar's end, in the room .meterbar--beyond keeps free.
        <svg x="100%" y="0" overflow="visible">
          <path class="meterbar__beyond" d="M4 4L8 8L4 12" />
          <path class="meterbar__beyond" d="M9.5 4L13.5 8L9.5 12" />
        </svg>
      )}
    </svg>
  );
}

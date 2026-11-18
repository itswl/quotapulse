// The month as one length, in three parts: what the meters have spent, what
// they are still expected to spend before the month is out, and the renewals
// that come on top. Now shows it under the month's total, Money at its head.
import { Amount, cx } from '../../components';
import { formatAmount } from '../../format';
import { t } from '../../i18n';

export interface MonthBarProps {
  /** Metered spend so far. */
  spent: number;
  /** Where metered spend ends the month; equal to `spent` once it is over. */
  projected: number;
  /** Renewals that fall in the month. */
  fixed: number;
  base: string;
  class?: string;
}

export function MonthBar({ spent, projected, fixed, base, class: extra }: MonthBarProps) {
  const coming = Math.max(0, projected - spent);
  const parts = [
    { key: 'spent', label: t('overview.month.part.spent'), value: spent },
    { key: 'coming', label: t('overview.month.part.coming'), value: coming },
    { key: 'renewals', label: t('overview.month.part.renewals'), value: fixed },
  ].filter((part) => part.value > 0);
  if (parts.length === 0) return null;
  const words = parts.map((part) => `${part.label} ${formatAmount(part.value, base)}`).join(t('common.list.separator'));
  return (
    <div class={cx('month-bar', extra)}>
      <span class="month-bar__track" role="img" aria-label={words}>
        {parts.map((part) => (
          <span key={part.key} class={`month-bar__part month-bar__part--${part.key}`} style={{ flexGrow: part.value }} />
        ))}
      </span>
      <ul class="month-bar__legend" aria-hidden="true">
        {parts.map((part) => (
          <li key={part.key} class="month-bar__item">
            <span class={`month-bar__key month-bar__part--${part.key}`} />
            <span class="month-bar__label">{part.label}</span>
            <Amount class="month-bar__value" value={part.value} unit={base} />
          </li>
        ))}
      </ul>
    </div>
  );
}

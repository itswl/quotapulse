// Six months of metered spend as small bars, for a sense of scale: is this
// month normal? Each bar is a link to its month.
//
// This is not built on BarChart: that one draws a time axis with a readout
// and has no way to mark a bar as selected or to follow one. What is here is
// a row of six links, each with a bar drawn in CSS.
import { useId } from 'preact/hooks';
import type { Spend } from '../../api/types';
import { Card } from '../../components';
import { compactNumber, formatAmount, formatMonth, monthName } from '../../format';
import { t } from '../../i18n';
import { Link } from '../../router';
import { historyBars } from './logic';

/** A share of the plot's height; never so little that something spent
 *  shows as nothing. */
const height = (share: number) => `max(2px, ${(Math.min(1, share) * 100).toFixed(2)}%)`;

export function History({ spend, thisMonth }: { spend: Spend; thisMonth: string }) {
  const titleId = useId();
  const bars = historyBars(spend.history, spend.is_current ? { month: spend.month, projected: spend.projected } : null);
  return (
    <Card class="sp-history">
      <div class="sp-history__head">
        <h3 class="sp-history__title" id={titleId}>
          {t('spend.history.title')}
        </h3>
        <span class="sp-history__meta">{t('spend.history.meta', { base: spend.base })}</span>
      </div>
      <ol class="sp-history__bars" aria-labelledby={titleId}>
        {bars.map((bar) => {
          const label = t('spend.history.month', {
            month: formatMonth(bar.month),
            metered: formatAmount(bar.total, spend.base),
            fixed: formatAmount(bar.fixed, spend.base),
          });
          return (
            <li key={bar.month}>
              <Link
                class="sp-month"
                href={bar.month >= thisMonth ? '/spend' : `/spend?month=${bar.month}`}
                replace
                aria-current={bar.month === spend.month ? 'true' : undefined}
                aria-label={label}
                title={label}
              >
                <span class="sp-month__amount num">{compactNumber(bar.total)}</span>
                <span class="sp-month__plot" aria-hidden="true">
                  {bar.heading > 0 && <span class="sp-month__bar sp-month__bar--heading" style={{ height: height(bar.heading) }} />}
                  {bar.total > 0 && <span class="sp-month__bar" style={{ height: height(bar.share) }} />}
                </span>
                <span class="sp-month__name">{monthName(Number(bar.month.slice(5)))}</span>
              </Link>
            </li>
          );
        })}
      </ol>
      {bars.some((bar) => bar.heading > 0) && (
        <p class="sp-history__note">
          <span class="chart-legend__key chart-legend__key--bar-partial" aria-hidden="true" />
          {t('spend.history.heading')}
        </p>
      )}
    </Card>
  );
}

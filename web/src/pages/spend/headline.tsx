// The month in one figure: what it comes to — metered spend where it is
// heading, and the renewals on top — then the same as one length in three
// parts, and a line on how the figure was reached. A month that is over has
// nothing still to come.
import type { Spend } from '../../api/types';
import { Amount, InfoTip } from '../../components';
import { dayOf, formatAmount } from '../../format';
import { t } from '../../i18n';
import { now } from '../../state/clock';
import { dayNumber, paceToProjection } from './logic';
import { MonthBar } from './month-bar';

export function Headline({ spend }: { spend: Spend }) {
  const { base } = spend;
  const current = spend.is_current;
  const pace = paceToProjection(spend);
  const today = dayOf(now.value);
  // Metered spend where the month ends: projected while it runs, spent once over.
  const end = current ? spend.projected : spend.total;

  const count = spend.renewals.length;
  const ahead = current ? spend.renewals.filter((each) => each.date >= today).length : 0;
  let renewals = t('spend.renewals.zero');
  if (count > 0) renewals = ahead > 0 ? t('spend.renewals.countLeft', { n: count, left: ahead }) : t('spend.renewals.count', { n: count });

  return (
    <section class="sp-hero" aria-labelledby="sp-hero-label">
      <p class="sp-hero__label" id="sp-hero-label">
        {t(current ? 'spend.hero.expected' : 'spend.hero.total')}
        <InfoTip label={t(current ? 'spend.hero.expected' : 'spend.hero.total')}>{t('spend.hero.hint')}</InfoTip>
      </p>
      <Amount class="sp-hero__total" value={end + spend.fixed} unit={base} />
      <MonthBar class="sp-hero__bar" spent={spend.total} projected={end} fixed={spend.fixed} base={base} />
      <p class="sp-hero__how dotted">
        {current && <span>{t('spend.spent.day', { day: dayNumber(spend), days: spend.days_in_month })}</span>}
        {pace !== null && <span>{t('spend.projected.pace', { amount: formatAmount(pace, base) })}</span>}
        <span>
          {t('spend.renewals')}: {renewals}
        </span>
        <span>{t('spend.base', { base })}</span>
      </p>
    </section>
  );
}

// The month in three numbers that read as one thought: what the meters have
// burned, where that is heading, and the renewals that come on top. The first
// two are "metered", the third is "fixed"; the line underneath adds them up.
import type { Spend } from '../../api/types';
import { Amount, Card, cx, InfoTip } from '../../components';
import { dayOf, formatAmount } from '../../format';
import { t } from '../../i18n';
import { now } from '../../state/clock';
import { dayNumber, paceToProjection } from './logic';
import { Fill, SLOT } from './parts';

export function Headline({ spend }: { spend: Spend }) {
  const { base } = spend;
  const current = spend.is_current;
  const pace = paceToProjection(spend);
  const today = dayOf(now.value);

  const count = spend.renewals.length;
  const ahead = current ? spend.renewals.filter((each) => each.date >= today).length : 0;
  let renewals = t('spend.renewals.zero');
  if (count > 0) renewals = ahead > 0 ? t('spend.renewals.countLeft', { n: count, left: ahead }) : t('spend.renewals.count', { n: count });

  // Adding the two parts up only says something when both are there.
  const end = current ? spend.projected : spend.total;
  const sum = spend.fixed > 0 && end > 0 ? t(current ? 'spend.sum.current' : 'spend.sum.past', { amount: SLOT }) : null;

  return (
    <Card class="sp-head">
      <div class={cx('sp-head__groups', !current && 'sp-head__groups--past')}>
        <div class="sp-group" role="group" aria-label={t('spend.metered')}>
          <p class="sp-group__label">
            {t('spend.metered')}
            <InfoTip label={t('spend.metered')}>{t('spend.metered.hint')}</InfoTip>
          </p>
          <dl class="sp-figures">
            <Figure
              label={t(current ? 'spend.spentSoFar' : 'spend.spent')}
              value={spend.total}
              unit={base}
              sub={current ? t('spend.spent.day', { day: dayNumber(spend), days: spend.days_in_month }) : undefined}
            />
            {current && (
              <Figure
                next
                label={t('spend.projected')}
                value={spend.projected}
                unit={base}
                sub={pace === null ? undefined : t('spend.projected.pace', { amount: formatAmount(pace, base) })}
              />
            )}
          </dl>
        </div>
        <div class="sp-group sp-group--fixed" role="group" aria-label={t('spend.fixed')}>
          <p class="sp-group__label">
            {t('spend.fixed')}
            <InfoTip label={t('spend.fixed')}>{t('spend.fixed.hint')}</InfoTip>
          </p>
          <dl class="sp-figures">
            <Figure label={t('spend.renewals')} value={spend.fixed} unit={base} sub={renewals} />
          </dl>
        </div>
      </div>
      <p class="sp-head__note">
        {sum !== null && (
          <span class="sp-head__sum">
            <Fill text={sum}>
              <Amount value={end + spend.fixed} unit={base} />
            </Fill>
          </span>
        )}
        <span>{t('spend.base', { base })}</span>
      </p>
    </Card>
  );
}

function Figure({ label, value, unit, sub, next }: { label: string; value: number; unit: string; sub?: string | undefined; next?: boolean }) {
  return (
    <div class={cx('sp-figure', next && 'sp-figure--next')}>
      <dt>{label}</dt>
      <dd>
        <Amount class="sp-figure__value" value={value} unit={unit} />
        {sub && <span class="sp-figure__sub">{sub}</span>}
      </dd>
    </div>
  );
}

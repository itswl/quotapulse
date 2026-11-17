// The numbers behind a meter: for a balance or a counter, how fast it burns
// and how long it lasts; for a quota, where it stands in its window.
import type { ComponentChildren } from 'preact';
import type { MeterView } from '../../api/types';
import { Amount, Banner, Card, InfoTip, MeterBudgetBar, MeterQuotaBar, runwayText } from '../../components';
import { DASH, formatClock, formatDays, formatIn, formatPercent, formatSpan, formatStamp, toMs } from '../../format';
import { t } from '../../i18n';
import { now } from '../../state/clock';

function Row({ label, hint, children }: { label: string; hint?: string; children: ComponentChildren }) {
  return (
    <div class="mt-numbers__row">
      <dt>
        {label}
        {hint && <InfoTip label={label}>{hint}</InfoTip>}
      </dt>
      <dd>{children}</dd>
    </div>
  );
}

export function MeterNumbers({ meter }: { meter: MeterView }) {
  return (
    <Card class="mt-numbers">
      <h2 class="mt-card-title">{t('meters.num.title')}</h2>
      {meter.kind === 'quota' ? <QuotaNumbers meter={meter} /> : <BurnNumbers meter={meter} />}
    </Card>
  );
}

function BurnNumbers({ meter }: { meter: MeterView }) {
  const { forecast, month, unit } = meter;
  if (!forecast) return <p class="mt-numbers__empty">{t('meters.card.pending')}</p>;

  const known = forecast.confidence !== 'none';
  const perDay = (value: number | null) =>
    value === null ? (
      <span class="num">{DASH}</span>
    ) : (
      <>
        <Amount value={value} unit={unit} /> <span class="mt-numbers__per">{t('meters.num.perDay')}</span>
      </>
    );
  const days = (value: number | null) => (value === null ? t('meters.num.never') : formatDays(value));

  return (
    <>
      {forecast.confidence === 'none' && <Banner class="mt-numbers__note">{t('meters.num.note.none')}</Banner>}
      {forecast.confidence === 'low' && <Banner class="mt-numbers__note">{t('meters.num.note.low')}</Banner>}
      <dl class="mt-numbers__list">
        <Row label={t('meters.num.burn')} hint={t('meters.num.burn.hint')}>
          {known ? perDay(forecast.burn_per_day) : t('meters.num.tooShort')}
        </Row>
        <Row label={t('meters.num.burn7')}>{perDay(forecast.burn_7d)}</Row>
        <Row label={t('meters.num.burn30')}>{perDay(forecast.burn_30d)}</Row>
        <Row label={t('meters.num.peak')} hint={t('meters.num.peak.hint')}>
          {known && forecast.peak_per_day > 0 ? <Amount value={forecast.peak_per_day} unit={unit} /> : <span class="num">{DASH}</span>}
        </Row>
        {meter.kind === 'balance' && known && (
          <>
            <Row label={t('meters.num.runway')} hint={t('meters.num.runway.hint')}>
              {forecast.runway_days === null ? t('meters.num.never') : runwayText(forecast.runway_days)}
            </Row>
            {forecast.depletes_at && <Row label={t('meters.num.depletes')}>{formatStamp(forecast.depletes_at)}</Row>}
            <Row label={t('meters.num.runwayPeak')}>{days(forecast.runway_days_at_peak)}</Row>
          </>
        )}
        {month && (
          <>
            <Row label={t('meters.num.month')}>
              <Amount value={month.spend} unit={unit} />
            </Row>
            <Row label={t('meters.num.monthProjected')}>
              <Amount value={month.projected} unit={unit} />
            </Row>
          </>
        )}
        {meter.rules.budget !== null && (
          <Row label={t('meters.num.budget')}>
            <Amount value={meter.rules.budget} unit={unit} />
          </Row>
        )}
        {forecast.last_top_up && (
          <Row label={t('meters.num.lastTopUp')}>
            <Amount value={forecast.last_top_up.amount} unit={unit} /> <span class="mt-numbers__per">· {formatStamp(forecast.last_top_up.at)}</span>
          </Row>
        )}
      </dl>
      {meter.rules.budget !== null && month && (
        <div class="mt-numbers__bar">
          <MeterBudgetBar meter={meter} />
        </div>
      )}
    </>
  );
}

function QuotaNumbers({ meter }: { meter: MeterView }) {
  const { quota, unit } = meter;
  if (!quota || meter.value === null) return <p class="mt-numbers__empty">{t('meters.card.pending')}</p>;
  const resets = meter.resets_at;
  const ahead = resets !== null && toMs(resets) > now.value;
  return (
    <>
      <div class="mt-numbers__bar">
        <MeterQuotaBar meter={meter} />
      </div>
      <dl class="mt-numbers__list">
        <Row label={t('meters.num.remaining')}>
          <Amount value={meter.value} unit={unit} />
          {unit !== '%' && <span class="mt-numbers__per"> {formatPercent(quota.remaining_pct)}</span>}
        </Row>
        {meter.limit !== null && unit !== '%' && (
          <Row label={t('meters.num.limit')}>
            <Amount value={meter.limit} unit={unit} />
          </Row>
        )}
        <Row label={t('meters.num.used')}>
          <span class="num">{formatPercent(quota.used_pct)}</span>
        </Row>
        <Row label={t('meters.num.resets')}>
          {resets ? (
            <>
              {formatClock(resets)}
              {ahead && <span class="mt-numbers__per"> {formatIn(resets)}</span>}
            </>
          ) : (
            t('meters.num.noReset')
          )}
        </Row>
        {meter.window_seconds !== null && meter.window_seconds > 0 && <Row label={t('meters.num.window')}>{formatSpan(meter.window_seconds * 1000)}</Row>}
        {quota.elapsed_pct !== null && (
          <Row label={t('meters.num.elapsed')}>
            <span class="num">{formatPercent(quota.elapsed_pct)}</span>
          </Row>
        )}
        {quota.projected_pct !== null && (
          <Row label={t('meters.num.projected')} hint={t('meters.num.projected.hint')}>
            <span class="num">{formatPercent(quota.projected_pct)}</span>
          </Row>
        )}
        {quota.exhausts_at && <Row label={t('meters.num.exhausts')}>{formatClock(quota.exhausts_at)}</Row>}
      </dl>
    </>
  );
}

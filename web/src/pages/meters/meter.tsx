// One meter: where it stands, where it has been, where it is heading, and
// the rules that decide when it is worth an alert.
import { Gauge } from 'lucide-preact';
import { getSeries } from '../../api/endpoints';
import type { MeterView, SeriesRange } from '../../api/types';
import { Card, EmptyState, ErrorState, IncidentRow, LinkButton, Section, Skeleton } from '../../components';
import { useQuery, useTitle } from '../../hooks';
import { t } from '../../i18n';
import { route, setQuery } from '../../router';
import type { PageProps } from '../../routes';
import { accounts } from '../../state/accounts';
import { useResource } from '../../state/resource';
import { MeterAccount } from './meter-account';
import { MeterCharts, RANGES } from './meter-charts';
import { MeterHeader } from './meter-header';
import { MeterNumbers } from './meter-numbers';
import { MeterRules } from './meter-rules';

const DEFAULT_RANGE: SeriesRange = '30d';
const TOPICS = ['meters', 'incidents', 'settings'] as const;

export default function MeterPage({ params }: PageProps) {
  const id = params.id ?? '';
  const requested = route.value.query.get('range') as SeriesRange | null;
  const range = requested && RANGES.includes(requested) ? requested : DEFAULT_RANGE;

  // The series carries the meter itself, so one request draws the whole page.
  const series = useQuery(`${id}:${range}`, (signal) => getSeries(id, range, signal), TOPICS);
  useResource(accounts);
  const data = series.data;
  useTitle(data?.meter.display ?? t('common.nav.meters'));

  if (series.error?.status === 404) {
    return (
      <EmptyState icon={Gauge} title={t('meters.missing.title')} action={<LinkButton href="/meters">{t('meters.missing.action')}</LinkButton>}>
        {t('meters.missing.body')}
      </EmptyState>
    );
  }
  if (!data) {
    if (series.error) return <ErrorState error={series.error} onRetry={series.reload} />;
    return (
      <div aria-busy="true">
        <span class="sr-only">{t('common.state.loading')}</span>
        <Skeleton width="240px" height="28px" />
        <Skeleton class="mt-loading" height="300px" />
      </div>
    );
  }

  const { meter } = data;
  const account = accounts.data.value?.find((each) => each.id === meter.account_id);
  const onChange = (next: MeterView) => series.set({ ...data, meter: next });

  return (
    <div class="mt-page">
      <MeterHeader meter={meter} account={account} onChange={onChange} />

      {meter.incidents.length > 0 && (
        <Section title={t('meters.incidents.title')} class="mt-page__incidents">
          <Card flush>
            <ul class="incidents">
              {meter.incidents.map((incident) => (
                <IncidentRow key={incident.id} incident={incident} link={false} facts />
              ))}
            </ul>
          </Card>
        </Section>
      )}

      <MeterCharts
        series={data}
        range={range}
        onRange={(next) => setQuery({ range: next === DEFAULT_RANGE ? null : next })}
        refreshing={!series.settled}
      />

      <div class="mt-columns">
        <MeterNumbers meter={meter} />
        <MeterRules meter={meter} onChange={onChange} />
      </div>
      <MeterAccount meter={meter} account={account} />
    </div>
  );
}

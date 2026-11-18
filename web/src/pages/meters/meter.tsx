// One meter: where it stands and how long that lasts, what is wrong with it,
// where it has been, the rules that decide when it is worth a word, what has
// happened to it, and the account it is read from. Watching shows it beside
// the list on a wide screen and in place of it on a narrow one.
import { Gauge } from 'lucide-preact';
import { getSeries } from '../../api/endpoints';
import type { MeterView, SeriesRange } from '../../api/types';
import { Card, EmptyState, ErrorState, IncidentRow, LinkButton, Section, Skeleton } from '../../components';
import { useQuery, useTitle } from '../../hooks';
import { t } from '../../i18n';
import { route, setQuery } from '../../router';
import { accounts } from '../../state/accounts';
import { useResource } from '../../state/resource';
import { MeterAccount } from './meter-account';
import { MeterCharts, RANGES } from './meter-charts';
import { MeterHeader } from './meter-header';
import { MeterHistory } from './meter-history';
import { MeterNumbers } from './meter-numbers';
import { MeterRules } from './meter-rules';

const DEFAULT_RANGE: SeriesRange = '30d';
const TOPICS = ['meters', 'incidents', 'settings'] as const;

export interface MeterDetailProps {
  id: string;
  /** The meter is the whole page, rather than open beside the list. */
  standalone?: boolean;
}

export function MeterDetail({ id, standalone = false }: MeterDetailProps) {
  const requested = route.value.query.get('range') as SeriesRange | null;
  const range = requested && RANGES.includes(requested) ? requested : DEFAULT_RANGE;

  // The series carries the meter itself, so one request draws the whole page.
  const series = useQuery(`${id}:${range}`, (signal) => getSeries(id, range, signal), TOPICS);
  useResource(accounts);
  const data = series.data;
  useTitle(data?.meter.display ?? t('common.nav.watching'));

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
      <div class="meter" aria-busy="true">
        <span class="sr-only">{t('common.state.loading')}</span>
        <Skeleton width="220px" height="30px" />
        <Skeleton class="meter__loading" height="120px" />
        <Skeleton class="meter__loading" height="280px" />
      </div>
    );
  }

  const { meter } = data;
  const account = accounts.data.value?.find((each) => each.id === meter.account_id);
  const onChange = (next: MeterView) => series.set({ ...data, meter: next });
  const open = meter.incidents.filter((incident) => incident.state === 'open');

  return (
    <article class="meter">
      <MeterHeader meter={meter} account={account} onChange={onChange} standalone={standalone} />

      {open.length > 0 && (
        <Section title={t('meters.incidents.title')} class="meter__incidents">
          <Card flush>
            <ul class="incidents">
              {open.map((incident) => (
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

      <div class="meter__columns">
        <MeterRules meter={meter} onChange={onChange} />
        <MeterNumbers meter={meter} />
      </div>
      <MeterHistory meter={meter} />
      <MeterAccount meter={meter} account={account} />
    </article>
  );
}

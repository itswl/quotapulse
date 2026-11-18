// What has happened to one meter: its incidents, open and over, newest
// first. The full story, with what was sent about each, is in History.
import { ArrowRight } from 'lucide-preact';
import { listIncidents } from '../../api/endpoints';
import type { MeterView } from '../../api/types';
import { Card, ErrorState, Skeleton, StatusDot } from '../../components';
import { formatDateTime, formatSpan, formatStamp, toMs } from '../../format';
import { useQuery } from '../../hooks';
import { t } from '../../i18n';
import { Link } from '../../router';
import { now } from '../../state/clock';

const TOPICS = ['incidents'] as const;

export function MeterHistory({ meter }: { meter: MeterView }) {
  const history = useQuery(`history:${meter.id}`, () => listIncidents({ subject_type: 'meter', subject_id: meter.id, limit: 8 }), TOPICS);
  const list = history.data;
  return (
    <Card class="meter-history">
      <div class="meter-history__head">
        <h2 class="mt-card-title">{t('meters.history.title')}</h2>
        <Link class="more-link" href="/activity">
          {t('meters.history.all')}
          <ArrowRight size={14} />
        </Link>
      </div>
      {!list ? (
        history.error ? (
          <ErrorState compact error={history.error} onRetry={history.reload} />
        ) : (
          <Skeleton height="64px" />
        )
      ) : list.length === 0 ? (
        <p class="meter-history__none">{t('meters.history.none')}</p>
      ) : (
        <ol class="meter-history__list">
          {list.map((incident) => {
            const opened = toMs(incident.opened_at);
            const over = incident.resolved_at === null ? null : toMs(incident.resolved_at);
            return (
              <li key={incident.id} class="meter-history__item">
                <StatusDot class="meter-history__mark" status={over === null ? incident.severity : 'ok'} iconOnly size={15} />
                <span class="meter-history__headline">{incident.headline}</span>
                <span class="meter-history__when dotted">
                  <time dateTime={incident.opened_at} title={formatDateTime(opened)}>
                    {formatStamp(opened)}
                  </time>
                  <span>{over === null ? t('meters.history.open', { span: formatSpan(now.value - opened) }) : t('meters.history.lasted', { span: formatSpan(over - opened) })}</span>
                </span>
              </li>
            );
          })}
        </ol>
      )}
    </Card>
  );
}

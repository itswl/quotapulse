// The record of what happened: what is open now, then history by day. A row
// opens to show the numbers behind the sentence and the notifications that
// were sent about it.
//
// The headline, the detail and the facts are the server's own sentences, in
// the language the dashboard asked for; nothing here rewrites them.
import { Activity, Check, ChevronDown, ChevronUp } from 'lucide-preact';
import { useEffect, useMemo, useState } from 'preact/hooks';
import { ackIncident, listDeliveries, listIncidents, unackIncident } from '../../api/endpoints';
import type { IncidentView } from '../../api/types';
import { Button, Card, cx, EmptyState, ErrorState, FilterChip, Section, Skeleton, StatusDot, toast } from '../../components';
import { errorText } from '../../errors';
import { formatClock, formatDateTime, formatSpan, formatTime, toMs } from '../../format';
import { useQuery } from '../../hooks';
import { t, type TKey } from '../../i18n';
import { Link, route, setQuery } from '../../router';
import { now, timezone } from '../../state/clock';
import { DeliveryItem } from './delivery';
import { groupByDay, incidentPager } from './logic';
import { usePaged } from './paged';
import { DayHeading, ListSkeleton, LoadMore } from './parts';

type Filter = 'all' | 'open' | 'resolved';

const FILTERS: { id: Filter; label: TKey }[] = [
  { id: 'all', label: 'activity.filter.all' },
  { id: 'open', label: 'activity.filter.open' },
  { id: 'resolved', label: 'activity.filter.resolved' },
];

const TOPICS = ['incidents', 'activity'] as const;
const SENT_TOPICS = ['activity'] as const;

export function Incidents() {
  const asked = route.value.query.get('state');
  const filter: Filter = asked === 'open' || asked === 'resolved' ? asked : 'all';

  // What is open is never much, so all of it is fetched at once; history
  // pages backwards.
  const open = useQuery('open', () => listIncidents({ state: 'open', limit: 500 }), TOPICS);
  const pager = useMemo(() => incidentPager(listIncidents, 'resolved'), []);
  const history = usePaged('resolved', pager, TOPICS);

  const [opened, setOpened] = useState<ReadonlySet<string>>(new Set());
  const toggle = (id: string) =>
    setOpened((was) => {
      const next = new Set(was);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  const row = (incident: IncidentView, dated: boolean) => (
    <IncidentItem key={incident.id} incident={incident} dated={dated} expanded={opened.has(incident.id)} onToggle={() => toggle(incident.id)} />
  );

  const current = filter === 'resolved' ? undefined : open.data;
  const past = filter === 'open' ? undefined : history.items;
  const waiting = (filter !== 'resolved' && !open.data) || (filter !== 'open' && !history.items);

  const filters = (
    <div class="filter-row ac-filters" role="group" aria-label={t('activity.filter.label')}>
      {FILTERS.map((each) => (
        <FilterChip
          key={each.id}
          pressed={filter === each.id}
          count={each.id === 'open' ? open.data?.length : undefined}
          onClick={() => setQuery({ state: each.id === 'all' ? null : each.id })}
        >
          {t(each.label)}
        </FilterChip>
      ))}
    </div>
  );

  if (waiting) {
    const error = (filter !== 'resolved' && !open.data ? open.error : undefined) ?? (filter !== 'open' && !history.items ? history.error : undefined);
    return (
      <>
        {filters}
        {error ? (
          <ErrorState
            error={error}
            onRetry={() => {
              open.reload();
              history.reload();
            }}
          />
        ) : (
          <ListSkeleton />
        )}
      </>
    );
  }

  if (filter === 'all' && current?.length === 0 && past?.length === 0) {
    return (
      <EmptyState icon={Activity} title={t('activity.none.title')}>
        {t('activity.none.body')}
      </EmptyState>
    );
  }

  return (
    <>
      {filters}
      {current && (
        <Section title={t('activity.open.title')} meta={<span class="num">{current.length}</span>}>
          {current.length === 0 ? (
            <EmptyState compact title={t('activity.open.none')} />
          ) : (
            <Card flush>
              <ul class="ac-list">{current.map((incident) => row(incident, false))}</ul>
            </Card>
          )}
        </Section>
      )}
      {past && (
        <Section title={t('activity.history.title')}>
          {past.length === 0 ? (
            <EmptyState compact title={t('activity.history.none')} />
          ) : (
            <>
              {groupByDay(past, (each) => each.opened_at, timezone.value).map((group) => (
                <div key={group.day} class="ac-daygroup">
                  <DayHeading day={group.day} level={3} />
                  <Card flush>
                    <ul class="ac-list">{group.items.map((incident) => row(incident, true))}</ul>
                  </Card>
                </div>
              ))}
              <LoadMore paged={history} />
            </>
          )}
        </Section>
      )}
    </>
  );
}

interface IncidentItemProps {
  incident: IncidentView;
  /** The row sits under its day's heading: the time alone says when. */
  dated: boolean;
  expanded: boolean;
  onToggle: () => void;
}

function IncidentItem({ incident, dated, expanded, onToggle }: IncidentItemProps) {
  const open = incident.state === 'open';
  // Acknowledging shows at once; the server's answer arrives with the next
  // refresh and replaces this guess.
  const [guess, setGuess] = useState<boolean | null>(null);
  useEffect(() => setGuess(null), [incident.acked_at]);
  const acked = open && (guess ?? incident.acked_at !== null);

  const toggleAck = async () => {
    const next = !acked;
    setGuess(next);
    try {
      await (next ? ackIncident(incident.id) : unackIncident(incident.id));
    } catch (cause) {
      setGuess(null);
      toast.error(errorText(cause));
    }
  };

  const since = toMs(incident.opened_at);
  const until = incident.resolved_at === null ? null : toMs(incident.resolved_at);
  const panel = `event-${incident.id}`;

  return (
    <li class={cx('incident', 'ac-incident', acked && 'incident--acked', !open && 'ac-incident--over')}>
      <StatusDot class="incident__mark" status={incident.severity} size={16} />
      <div class="incident__text">
        <p class="incident__headline">
          <Link href={incident.path}>{incident.headline}</Link>
        </p>
        {incident.detail && <p class="incident__detail">{incident.detail}</p>}
        <p class="incident__meta dotted">
          <time dateTime={incident.opened_at} title={formatDateTime(since)}>
            {t('activity.meta.opened', { time: dated ? formatTime(since) : formatClock(since) })}
          </time>
          {until === null ? (
            <span>{t('activity.meta.openFor', { span: formatSpan(now.value - since) })}</span>
          ) : (
            <time dateTime={incident.resolved_at ?? undefined} title={t('activity.meta.resolvedAt', { time: formatDateTime(until) })}>
              {t('activity.meta.lasted', { span: formatSpan(until - since) })}
            </time>
          )}
          {incident.notify_count > 0 && <span>{t('activity.meta.notified', { n: incident.notify_count })}</span>}
        </p>
      </div>
      <div class="incident__actions">
        {open &&
          (acked ? (
            <>
              <span class="incident__acked">
                <Check size={14} />
                {t('common.action.acked')}
              </span>
              <Button variant="ghost" size="sm" onClick={toggleAck}>
                {t('common.action.undo')}
              </Button>
            </>
          ) : (
            <Button size="sm" icon={Check} onClick={toggleAck}>
              {t('common.action.ack')}
            </Button>
          ))}
        <Button
          variant="ghost"
          size="sm"
          icon={expanded ? ChevronUp : ChevronDown}
          aria-expanded={expanded}
          aria-controls={expanded ? panel : undefined}
          aria-label={t('activity.details.of', { title: incident.headline })}
          onClick={onToggle}
        >
          {t('activity.details')}
        </Button>
      </div>
      {expanded && (
        <div class="ac-incident__more" id={panel}>
          <IncidentDetails incident={incident} />
        </div>
      )}
    </li>
  );
}

/** What a row shows when opened: the facts, and what was sent about it. */
function IncidentDetails({ incident }: { incident: IncidentView }) {
  const sent = useQuery(incident.id, () => listDeliveries({ incident: incident.id, limit: 200 }), SENT_TOPICS);
  const list = sent.data;
  return (
    <>
      {incident.facts.length > 0 && (
        <dl class="incident__facts">
          {incident.facts.map((fact) => (
            <div key={fact.label}>
              <dt>{fact.label}</dt>
              <dd class="num">{fact.value}</dd>
            </div>
          ))}
        </dl>
      )}
      <div>
        <p class="ac-sub">{t('activity.sent.title')}</p>
        {!list ? (
          sent.error ? (
            <ErrorState compact error={sent.error} onRetry={sent.reload} />
          ) : (
            <Skeleton width="min(100%, 320px)" height="20px" />
          )
        ) : list.length === 0 ? (
          <p class="ac-quiet">{t('activity.sent.none')}</p>
        ) : (
          // The API answers newest first; a story reads from its beginning.
          <ul class="ac-log ac-log--sent">
            {[...list].reverse().map((delivery) => (
              <DeliveryItem
                key={delivery.id}
                delivery={delivery}
                context="event"
                onChange={(next) => sent.set(list.map((each) => (each.id === next.id ? next : each)))}
              />
            ))}
          </ul>
        )}
      </div>
    </>
  );
}

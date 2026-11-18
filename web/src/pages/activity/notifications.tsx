// The notification log: every message that was sent, held back, dropped or
// that failed, newest first and by day. A row opens to show the message
// roughly as it was delivered; a failed one can be sent again.
import { Send } from 'lucide-preact';
import { useMemo } from 'preact/hooks';
import { listDeliveries } from '../../api/endpoints';
import { Card, EmptyState, ErrorState } from '../../components';
import { t } from '../../i18n';
import { timezone } from '../../state/clock';
import { DeliveryItem } from './delivery';
import { deliveryPager, groupByDay } from './logic';
import { usePaged } from './paged';
import { DayHeading, ListSkeleton, LoadMore } from './parts';

const TOPICS = ['activity'] as const;

export function Notifications() {
  const pager = useMemo(() => deliveryPager(listDeliveries), []);
  const log = usePaged('deliveries', pager, TOPICS);
  const items = log.items;

  if (!items) return log.error ? <ErrorState error={log.error} onRetry={log.reload} /> : <ListSkeleton />;
  if (items.length === 0) {
    return (
      <EmptyState icon={Send} title={t('activity.log.none.title')}>
        {t('activity.log.none.body')}
      </EmptyState>
    );
  }

  return (
    <>
      {groupByDay(items, (each) => each.created_at, timezone.value).map((group) => (
        <div key={group.day} class="ac-daygroup">
          <DayHeading day={group.day} level={2} />
          <Card flush>
            <ul class="ac-log">
              {group.items.map((delivery) => (
                <DeliveryItem key={delivery.id} delivery={delivery} context="log" onChange={(next) => log.replace((each) => each.id === next.id, next)} />
              ))}
            </ul>
          </Card>
        </div>
      ))}
      <LoadMore paged={log} />
    </>
  );
}

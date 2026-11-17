// Small pieces both views of the Activity page use.
import type { Day } from '../../api/types';
import { Button, Skeleton } from '../../components';
import { errorText } from '../../errors';
import { addDays, dayOf, formatDay, parseDay } from '../../format';
import { t } from '../../i18n';
import { now } from '../../state/clock';
import type { Paged } from './paged';

/** The heading of a day's worth of rows: "Today · Nov 16, Mon". */
export function DayHeading({ day, level }: { day: Day; level: 2 | 3 }) {
  const today = dayOf(now.value);
  const word = day === today ? t('activity.day.today') : day === addDays(today, -1) ? t('activity.day.yesterday') : null;
  const Tag = level === 2 ? 'h2' : 'h3';
  return (
    <Tag class="ac-day">
      {word && <span class="ac-day__word">{word}</span>}
      <span>{formatDay(day, { weekday: true, year: parseDay(today).year })}</span>
    </Tag>
  );
}

/** The end of a paged list: the button for the next page, and what went
 *  wrong with the last try, if anything. Nothing once everything is loaded. */
export function LoadMore<T>({ paged }: { paged: Paged<T> }) {
  if (!paged.more && !paged.moreError) return null;
  return (
    <div class="ac-more">
      {paged.moreError && (
        <p class="form-error" role="alert">
          {t('activity.more.error', { error: errorText(paged.moreError) })}
        </p>
      )}
      <Button busy={paged.loadingMore} onClick={paged.loadMore}>
        {t('activity.more')}
      </Button>
    </div>
  );
}

/** Stands in for a list on its first load. */
export function ListSkeleton() {
  return (
    <div class="ac-loading" aria-busy="true">
      <span class="sr-only">{t('common.state.loading')}</span>
      <Skeleton width="120px" height="18px" />
      <Skeleton height="72px" />
      <Skeleton height="72px" />
      <Skeleton height="72px" />
    </div>
  );
}

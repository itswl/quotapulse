// The Spend page: a month of money — what the meters burned, where that is
// heading, and which renewals fall in it. One request (GET /api/v1/spend)
// draws the whole page.
//
// The month is in the address (/spend?month=2026-10), so a month can be
// linked to and reloaded; no month means this one. Months yet to come are
// not shown: there is nothing in them but renewals.
import { ChevronLeft, ChevronRight, Wallet } from 'lucide-preact';
import { useEffect } from 'preact/hooks';
import { getSpend } from '../../api/endpoints';
import { Banner, Button, cx, EmptyState, ErrorState, IconButton, LinkButton, PageHeader, Skeleton } from '../../components';
import { dayOf, formatMonth } from '../../format';
import { useQuery, useTitle } from '../../hooks';
import { t } from '../../i18n';
import { route, setQuery } from '../../router';
import type { PageProps } from '../../routes';
import { now } from '../../state/clock';
import { overview } from '../../state/overview';
import { useResource } from '../../state/resource';
import { DailyChart } from './daily';
import { Headline } from './headline';
import { History } from './history';
import { hasMetered, isEmpty, isMonth, monthOf, shiftMonth } from './logic';
import { MetersTable, Renewals } from './tables';

const TOPICS = ['meters', 'reminders', 'settings'] as const;

export default function SpendPage(_props: PageProps) {
  const title = t('common.nav.money');
  useTitle(title);
  // Which month "this month" is, is the server's to say: its clock and its
  // zone, which the overview carries. Until that has answered there is no
  // telling which month to ask for.
  useResource(overview);
  if (!overview.data.value && !overview.error.value) {
    return (
      <>
        <PageHeader title={title} />
        <Loading />
      </>
    );
  }
  return <SpendMonth title={title} />;
}

function SpendMonth({ title }: { title: string }) {
  const thisMonth = monthOf(dayOf(now.value));
  const asked = route.value.query.get('month');
  const month = isMonth(asked) && asked < thisMonth ? asked : thisMonth;
  const current = month === thisMonth;

  // An address that names a month yet to come, or something that is no month
  // at all, becomes the plain one.
  useEffect(() => {
    if (asked !== null && asked !== month) setQuery({ month: null });
  }, [asked, month]);

  // The running month is asked for without a name: what "now" is, is for
  // the server to decide.
  const spend = useQuery(`spend:${month}`, (signal) => getSpend(current ? undefined : month, signal), TOPICS);
  const data = spend.data;
  const go = (to: string) => setQuery({ month: to >= thisMonth ? null : to });

  const header = (
    <PageHeader
      title={title}
      actions={
        <div class="sp-switch" role="group" aria-label={t('spend.month.label')}>
          {!current && (
            <Button variant="ghost" size="sm" onClick={() => go(thisMonth)}>
              {t('spend.month.current')}
            </Button>
          )}
          <IconButton outlined icon={ChevronLeft} label={t('spend.month.prev')} onClick={() => go(shiftMonth(month, -1))} />
          <span class="sp-switch__month" aria-live="polite">
            {formatMonth(month)}
          </span>
          <IconButton outlined icon={ChevronRight} label={t('spend.month.next')} disabled={current} onClick={() => go(shiftMonth(month, 1))} />
        </div>
      }
    />
  );

  if (!data) {
    return (
      <>
        {header}
        {spend.error ? <ErrorState error={spend.error} onRetry={spend.reload} /> : <Loading />}
      </>
    );
  }

  const empty = isEmpty(data);
  const history = data.history.some((each) => each.total > 0 || each.fixed > 0);

  return (
    <>
      {header}
      {/* While another month is on its way the last one stays, dimmed. */}
      <div class={cx('sp', !spend.settled && 'sp--refreshing')} aria-busy={!spend.settled}>
        {data.unconverted.length > 0 && (
          <Banner
            class="sp-note"
            action={
              <LinkButton size="sm" href="/settings/preferences">
                {t('spend.unconverted.action')}
              </LinkButton>
            }
          >
            {t('spend.unconverted', { list: data.unconverted.join(t('common.list.separator')) })}
          </Banner>
        )}

        {empty ? (
          <>
            <EmptyState
              icon={Wallet}
              title={t('spend.empty.title', { month: formatMonth(data.month) })}
              action={
                data.is_current ? (
                  <>
                    <LinkButton href="/meters">{t('spend.empty.meters')}</LinkButton>
                    <LinkButton href="/dates">{t('spend.empty.dates')}</LinkButton>
                  </>
                ) : (
                  <Button onClick={() => go(thisMonth)}>{t('spend.month.current')}</Button>
                )
              }
            >
              {t(data.is_current ? 'spend.empty.current' : 'spend.empty.past')}
            </EmptyState>
            {history && (
              <div class="sp-charts sp-charts--alone">
                <History spend={data} thisMonth={thisMonth} />
              </div>
            )}
          </>
        ) : (
          <>
            <Headline spend={data} />
            <div class="sp-charts">
              <DailyChart spend={data} />
              <History spend={data} thisMonth={thisMonth} />
            </div>
            {hasMetered(data) && <MetersTable spend={data} />}
            {data.renewals.length > 0 && <Renewals spend={data} />}
          </>
        )}
      </div>
    </>
  );
}

function Loading() {
  return (
    <div aria-busy="true">
      <span class="sr-only">{t('common.state.loading')}</span>
      <Skeleton height="132px" />
      <div class="sp-charts">
        <Skeleton height="246px" />
        <Skeleton height="246px" />
      </div>
      <Skeleton class="sp-loading" height="240px" />
    </div>
  );
}

// The dates side of Watching: every date — birthdays, renewals, expiries,
// on the solar or the lunar calendar — soonest first and grouped by month.
// A date opens beside the list on a wide screen, and as a sheet from the
// bottom on a narrow one; the way back closes it.
//
//   /dates                 the list
//   /dates?kind=birthday   one kind only
//   /dates?open=<id>       the list, with that date open
//   /dates?add=1           the list, with the form for a new date open
import { CalendarHeart, Plus } from 'lucide-preact';
import { useState } from 'preact/hooks';
import { deleteReminder } from '../../api/endpoints';
import type { ReminderKind, ReminderView } from '../../api/types';
import { Amount, Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, FilterChip, InfoTip, Skeleton, toast } from '../../components';
import { errorText } from '../../errors';
import { DASH, dayOf, parseDay } from '../../format';
import { useAction } from '../../hooks';
import { t } from '../../i18n';
import { navigate, route, setQuery } from '../../router';
import { now } from '../../state/clock';
import { reminders } from '../../state/reminders';
import { useResource } from '../../state/resource';
import { dropReminder } from './changes';
import { DateDetail, DateItem } from './detail';
import { DateDialog } from './form';
import { isKind, KIND_ICON, kindLabel, KINDS } from './kinds';
import { SnoozeDialog } from './snooze';
import { groupByMonth, matchesTitle, monthTitle, summarise } from './words';

export function DatesSegment({ search, wide }: { search: string; wide: boolean }) {
  useResource(reminders);

  const query = route.value.query;
  const requested = query.get('kind');
  const kind: ReminderKind | 'all' = isKind(requested) ? requested : 'all';
  const adding = query.has('add');
  const openId = query.get('open');
  // The dialogs work on the date as it was when they opened: a refresh of
  // the list behind them must not pull the form out from under the user.
  const [editing, setEditing] = useState<ReminderView | null>(null);
  const [snoozing, setSnoozing] = useState<ReminderView | null>(null);
  const [deleting, setDeleting] = useState<ReminderView | null>(null);

  const remove = useAction(async () => {
    if (!deleting) return;
    await deleteReminder(deleting.id);
    dropReminder(deleting.id);
    if (deleting.id === openId) setQuery({ open: null });
    setDeleting(null);
    toast(t('dates.toast.deleted', { title: deleting.title }));
  });

  // "Today" is the server's, in the zone set under Settings.
  const today = dayOf(now.value);
  const list = reminders.data.value;
  const open = (openId && list?.find((each) => each.id === openId)) || null;

  // On a narrow screen the open date is a sheet over the list, and a step in
  // the history, so the way back closes it.
  const openDate = (reminder: ReminderView) => {
    if (wide) setQuery({ open: reminder.id });
    else navigate(`/dates?${new URLSearchParams({ ...Object.fromEntries(query), open: reminder.id }).toString()}`, { keepScroll: true });
  };
  const closeDate = () => (wide ? setQuery({ open: null }) : history.back());
  const handlers = { onEdit: setEditing, onSnooze: setSnoozing, onDelete: setDeleting };

  const dialogs = (
    <>
      <DateDialog
        open={adding || editing !== null}
        reminder={editing ?? undefined}
        kind={kind === 'all' ? undefined : kind}
        onClose={() => {
          setEditing(null);
          if (adding) setQuery({ add: null });
        }}
      />
      <SnoozeDialog reminder={snoozing} today={today} onClose={() => setSnoozing(null)} />
      <ConfirmDialog
        open={deleting !== null}
        onClose={() => setDeleting(null)}
        title={t('dates.delete.title', { title: deleting?.title ?? '' })}
        confirmLabel={t('common.action.delete')}
        busy={remove.busy}
        error={remove.error === undefined ? undefined : errorText(remove.error)}
        onConfirm={() => void remove.run()}
      >
        <p>{t('dates.delete.body')}</p>
      </ConfirmDialog>
      {!wide && (
        <Dialog open={open !== null} onClose={closeDate} title={open?.title ?? ''} class="date-sheet">
          {open && <DateDetail reminder={open} today={today} heading={false} {...handlers} />}
        </Dialog>
      )}
    </>
  );

  if (!list) {
    return (
      <>
        <div class="watching__list">
          {reminders.error.value ? (
            <ErrorState error={reminders.error.value} onRetry={() => void reminders.load()} />
          ) : (
            <div class="w-skeleton" aria-busy="true">
              <span class="sr-only">{t('common.state.loading')}</span>
              {[0, 1, 2, 3, 4].map((index) => (
                <Skeleton key={index} height="64px" />
              ))}
            </div>
          )}
        </div>
        {dialogs}
      </>
    );
  }

  if (list.length === 0) {
    return (
      <>
        <div class="watching__list">
          <EmptyState
            icon={CalendarHeart}
            title={t('dates.empty.title')}
            action={
              <Button variant="primary" icon={Plus} onClick={() => setQuery({ add: '1' })}>
                {t('dates.add')}
              </Button>
            }
          >
            {t('dates.empty.body')}
          </EmptyState>
        </div>
        {dialogs}
      </>
    );
  }

  const found = list.filter((item) => matchesTitle(item, search));
  const shown = kind === 'all' ? found : found.filter((item) => item.kind === kind);
  const count = (each: ReminderKind) => found.filter((item) => item.kind === each).length;
  const groups = groupByMonth(shown);
  const thisYear = parseDay(today).year;

  return (
    <>
      <div class="watching__list">
        <div class="filter-row w-filters" role="group" aria-label={t('dates.filter.label')}>
          <FilterChip pressed={kind === 'all'} count={found.length} onClick={() => setQuery({ kind: null })}>
            {t('common.word.all')}
          </FilterChip>
          {/* Only the kinds in use, so the row stays short. */}
          {KINDS.filter((each) => each === kind || list.some((item) => item.kind === each)).map((each) => {
            const Icon = KIND_ICON[each];
            return (
              <FilterChip key={each} pressed={kind === each} count={count(each)} onClick={() => setQuery({ kind: kind === each ? null : each })}>
                <Icon size={14} />
                {kindLabel(each)}
              </FilterChip>
            );
          })}
        </div>

        {!wide && <SummaryCard list={list} />}

        {groups.length === 0 ? (
          <EmptyState
            compact
            title={t('dates.noMatch')}
            action={<Button onClick={() => setQuery({ kind: null })}>{t('dates.noMatch.clear')}</Button>}
          />
        ) : (
          groups.map((group) => (
            <section key={group.key} class="w-group" aria-labelledby={`w-month-${group.key}`}>
              <h2 class="w-group__title" id={`w-month-${group.key}`}>
                {monthTitle(group.key, thisYear)}
                <span class="w-group__count num">{group.items.length}</span>
              </h2>
              <ul class="w-list">
                {group.items.map((reminder) => (
                  <DateItem key={reminder.id} reminder={reminder} today={today} selected={wide && reminder.id === openId} onOpen={() => openDate(reminder)} />
                ))}
              </ul>
            </section>
          ))
        )}
      </div>
      {wide && (
        <div class="watching__detail">
          {open ? (
            <DateDetail key={open.id} reminder={open} today={today} onClose={closeDate} {...handlers} />
          ) : (
            <div class="watching__overview">
              <SummaryCard list={list} />
              <p class="watching__hint">{t('dates.pick')}</p>
            </div>
          )}
        </div>
      )}
      {dialogs}
    </>
  );
}

/** How much is coming, at a glance: the dates of the next week and month,
 *  and what the ones with a price come to per month. */
function SummaryCard({ list }: { list: ReminderView[] }) {
  const summary = summarise(list);
  const counts = [
    { label: t('dates.summary.week'), n: summary.week },
    { label: t('dates.summary.month'), n: summary.month },
  ];
  return (
    <Card class="dates-summary">
      <dl class="dates-summary__figures" aria-label={t('dates.summary.label')}>
        {counts.map((each) => (
          <div key={each.label} class="dates-summary__figure">
            <dt>{each.label}</dt>
            <dd class="dates-summary__value num">
              <span>
                {each.n}
                <span class="unit unit--spaced">{t('dates.summary.unit', { n: each.n })}</span>
              </span>
            </dd>
          </div>
        ))}
        <div class="dates-summary__figure">
          <dt>
            {t('dates.summary.monthly')}
            <InfoTip label={t('dates.summary.monthly')}>{t('dates.summary.monthly.hint')}</InfoTip>
          </dt>
          {/* One figure per currency: sums in different currencies stay apart. */}
          <dd class="dates-summary__value">
            {summary.monthly.length === 0 ? <span class="num">{DASH}</span> : summary.monthly.map((each) => <Amount key={each.currency} value={each.amount} unit={each.currency} />)}
          </dd>
        </div>
      </dl>
    </Card>
  );
}

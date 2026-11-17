// The Dates page: every date being watched — birthdays, renewals, expiries,
// on the solar or the lunar calendar — soonest first and grouped by month.
// It answers "what is coming, and when exactly", and it is where a date is
// added or put right.
//
//   /dates                 the list
//   /dates?kind=birthday   one kind only
//   /dates?add=1           the list, with the form for a new date open
import { CalendarHeart, Plus, Search } from 'lucide-preact';
import { useId, useState } from 'preact/hooks';
import { deleteReminder } from '../../api/endpoints';
import type { Day, ReminderKind, ReminderView } from '../../api/types';
import { Amount, Button, Card, ConfirmDialog, EmptyState, ErrorState, FilterChip, InfoTip, Input, PageHeader, Skeleton, toast } from '../../components';
import { errorText } from '../../errors';
import { dayOf, DASH, parseDay } from '../../format';
import { useAction, useTitle } from '../../hooks';
import { t } from '../../i18n';
import { route, setQuery } from '../../router';
import type { PageProps } from '../../routes';
import { now } from '../../state/clock';
import { reminders } from '../../state/reminders';
import { useResource } from '../../state/resource';
import { dropReminder } from './changes';
import { DateDialog } from './form';
import { isKind, KIND_ICON, kindLabel, KINDS } from './kinds';
import { DateRow } from './row';
import { SnoozeDialog } from './snooze';
import { groupByMonth, matchesTitle, monthTitle, summarise, type MonthGroup } from './words';

export default function DatesPage(_props: PageProps) {
  const title = t('common.nav.dates');
  useTitle(title);
  useResource(reminders);

  const query = route.value.query;
  const requested = query.get('kind');
  const kind: ReminderKind | 'all' = isKind(requested) ? requested : 'all';
  const adding = query.has('add');
  const [search, setSearch] = useState('');
  // The dialogs work on the date as it was when they opened: a refresh of
  // the list behind them must not pull the form out from under the user.
  const [editing, setEditing] = useState<ReminderView | null>(null);
  const [snoozing, setSnoozing] = useState<ReminderView | null>(null);
  const [deleting, setDeleting] = useState<ReminderView | null>(null);

  const remove = useAction(async () => {
    if (!deleting) return;
    await deleteReminder(deleting.id);
    dropReminder(deleting.id);
    setDeleting(null);
    toast(t('dates.toast.deleted', { title: deleting.title }));
  });

  // "Today" is the server's, in the zone set under Settings.
  const today = dayOf(now.value);
  const add = () => setQuery({ add: '1' });
  const list = reminders.data.value;

  const header = (
    <PageHeader
      title={title}
      actions={
        <Button variant="primary" icon={Plus} onClick={add}>
          {t('dates.add')}
        </Button>
      }
    />
  );
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
    </>
  );

  if (!list) {
    return (
      <>
        {header}
        {reminders.error.value ? (
          <ErrorState error={reminders.error.value} onRetry={() => void reminders.load()} />
        ) : (
          <div class="dates-loading" aria-busy="true">
            <span class="sr-only">{t('common.state.loading')}</span>
            <Skeleton height="78px" />
            {[0, 1, 2, 3, 4].map((index) => (
              <Skeleton key={index} height="62px" />
            ))}
          </div>
        )}
        {dialogs}
      </>
    );
  }

  if (list.length === 0) {
    return (
      <>
        {header}
        <EmptyState
          icon={CalendarHeart}
          title={t('dates.empty.title')}
          action={
            <Button variant="primary" icon={Plus} onClick={add}>
              {t('dates.add')}
            </Button>
          }
        >
          {t('dates.empty.body')}
        </EmptyState>
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
      {header}
      <SummaryCard list={list} />

      <div class="dates-tools">
        <div class="dates-filters" role="group" aria-label={t('dates.filter.label')}>
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
        <Input class="dates-search" type="search" value={search} onInput={setSearch} placeholder={t('dates.search')} aria-label={t('dates.search')} prefix={<Search size={14} />} />
      </div>

      {groups.length === 0 ? (
        <EmptyState
          compact
          title={t('dates.noMatch')}
          action={
            <Button
              onClick={() => {
                setSearch('');
                setQuery({ kind: null });
              }}
            >
              {t('dates.noMatch.clear')}
            </Button>
          }
        />
      ) : (
        <div class="dates-groups">
          {groups.map((group) => (
            <Group key={group.key} group={group} today={today} thisYear={thisYear} onEdit={setEditing} onSnooze={setSnoozing} onDelete={setDeleting} />
          ))}
        </div>
      )}
      {dialogs}
    </>
  );
}

interface GroupProps {
  group: MonthGroup;
  today: Day;
  thisYear: number;
  onEdit: (reminder: ReminderView) => void;
  onSnooze: (reminder: ReminderView) => void;
  onDelete: (reminder: ReminderView) => void;
}

/** One month of dates, under its name. */
function Group({ group, today, thisYear, onEdit, onSnooze, onDelete }: GroupProps) {
  const id = useId();
  return (
    <section class="dates-group" aria-labelledby={id}>
      <h2 class="dates-group__title" id={id}>
        {monthTitle(group.key, thisYear)}
        <span class="dates-group__count num">{group.items.length}</span>
      </h2>
      <ul class="dates-list">
        {group.items.map((reminder) => (
          <DateRow key={reminder.id} reminder={reminder} today={today} onEdit={onEdit} onSnooze={onSnooze} onDelete={onDelete} />
        ))}
      </ul>
    </section>
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

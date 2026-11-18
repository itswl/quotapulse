// The meters list: every meter as a card, grouped the way the accounts are,
// with the accounts that have no meter yet — new, or failing from the start —
// shown as rows so they can still be found and fixed.
import { Ellipsis, EyeOff, Gauge, Pencil, Plug, Plus, Power, RefreshCw, Trash2 } from 'lucide-preact';
import type { AccountView, MeterView } from '../../api/types';
import {
  Button,
  ErrorState,
  EmptyState,
  FilterChip,
  IconButton,
  InfoTip,
  isPushed,
  Menu,
  MeterCard,
  needsAttention,
  PageHeader,
  Skeleton,
  StatusDot,
  toast,
  type Status,
} from '../../components';
import { errorText } from '../../errors';
import { useTitle } from '../../hooks';
import { t, type TKey } from '../../i18n';
import { route, setQuery } from '../../router';
import type { PageProps } from '../../routes';
import { accounts } from '../../state/accounts';
import { meters } from '../../state/meters';
import { useResource } from '../../state/resource';
import { isSampling, sampleNow } from '../../state/sampling';
import { AccountDialog } from './account-dialog';
import { useAccountManager } from './account-manage';

type Filter = 'all' | 'attention' | 'balance' | 'quota' | 'reported';

const FILTERS: { id: Filter; label: TKey }[] = [
  { id: 'all', label: 'meters.list.filter.all' },
  { id: 'attention', label: 'meters.list.filter.attention' },
  { id: 'balance', label: 'meters.list.filter.balance' },
  { id: 'quota', label: 'meters.list.filter.quota' },
  { id: 'reported', label: 'meters.list.filter.reported' },
];

function meterMatches(meter: MeterView, filter: Filter): boolean {
  switch (filter) {
    case 'attention':
      return needsAttention(meter);
    case 'balance':
      return meter.kind === 'balance';
    case 'quota':
      return meter.kind === 'quota';
    case 'reported':
      // What a script reports: anything pushed, and every running total.
      return isPushed(meter) || meter.kind === 'counter';
    default:
      return true;
  }
}

/** An account with no meters has nothing to open, so it is listed itself —
 *  under "all", and under "needs attention" when it is failing. */
function accountMatches(account: AccountView, filter: Filter): boolean {
  if (filter === 'all') return true;
  return filter === 'attention' && account.enabled && account.status === 'failing';
}

interface Group {
  key: string;
  title: string;
  hint?: string;
  meters: MeterView[];
  accounts: AccountView[];
}

const PUSHED = '\u0000pushed';

function groupUp(list: MeterView[], bare: AccountView[]): Group[] {
  const groups = new Map<string, Group>();
  const at = (key: string): Group => {
    let group = groups.get(key);
    if (!group) {
      group = { key, title: key === '' ? t('common.word.ungrouped') : key, meters: [], accounts: [] };
      if (key === PUSHED) {
        group.title = t('meters.list.pushed');
        group.hint = t('meters.list.pushed.hint');
      }
      groups.set(key, group);
    }
    return group;
  };
  for (const meter of list) at(isPushed(meter) ? PUSHED : (meter.account?.group ?? '')).meters.push(meter);
  for (const account of bare) at(account.group).accounts.push(account);
  // Named groups in order, then the ungrouped, then what is pushed.
  const rank = (key: string) => (key === PUSHED ? 2 : key === '' ? 1 : 0);
  return [...groups.values()].sort((a, b) => rank(a.key) - rank(b.key) || a.title.localeCompare(b.title));
}

export default function MetersPage(_props: PageProps) {
  const title = t('common.nav.meters');
  useTitle(title);
  useResource(meters);
  useResource(accounts);

  const query = route.value.query;
  const requested = query.get('filter') as Filter | null;
  const filter: Filter = FILTERS.some((each) => each.id === requested) ? (requested as Filter) : 'all';
  const adding = query.has('add');
  const addProvider = query.get('add');

  const allMeters = meters.data.value;
  const allAccounts = accounts.data.value;

  const header = (
    <PageHeader
      title={title}
      actions={
        <Button variant="primary" icon={Plus} onClick={() => setQuery({ add: '1' })}>
          {t('meters.list.add')}
        </Button>
      }
    />
  );
  const dialog = <AccountDialog open={adding} onClose={() => setQuery({ add: null })} provider={addProvider && addProvider !== '1' ? addProvider : undefined} />;

  if (!allMeters || !allAccounts) {
    const error = meters.error.value ?? accounts.error.value;
    return (
      <>
        {header}
        {error ? (
          <ErrorState
            error={error}
            onRetry={() => {
              void meters.load();
              void accounts.load();
            }}
          />
        ) : (
          <div class="meter-grid" aria-busy="true">
            {[0, 1, 2, 3, 4, 5].map((index) => (
              <Skeleton key={index} height="164px" />
            ))}
          </div>
        )}
        {dialog}
      </>
    );
  }

  const visible = allMeters.filter((meter) => !meter.hidden);
  const hidden = allMeters.filter((meter) => meter.hidden);
  const bare = allAccounts.filter((account) => account.meter_ids.length === 0);
  const count = (id: Filter) => visible.filter((meter) => meterMatches(meter, id)).length + bare.filter((account) => accountMatches(account, id)).length;
  const groups = groupUp(
    visible.filter((meter) => meterMatches(meter, filter)),
    bare.filter((account) => accountMatches(account, filter)),
  );

  if (allMeters.length === 0 && allAccounts.length === 0) {
    return (
      <>
        {header}
        <EmptyState
          icon={Gauge}
          title={t('meters.list.empty.title')}
          action={
            <Button variant="primary" icon={Plus} onClick={() => setQuery({ add: '1' })}>
              {t('meters.list.add')}
            </Button>
          }
        >
          {t('meters.list.empty.body')}
        </EmptyState>
        {dialog}
      </>
    );
  }

  return (
    <>
      {header}
      <div class="filter-row mt-filters" role="group" aria-label={t('meters.list.filter')}>
        {FILTERS.map((each) => (
          <FilterChip key={each.id} pressed={filter === each.id} count={count(each.id)} onClick={() => setQuery({ filter: each.id === 'all' ? null : each.id })}>
            {t(each.label)}
          </FilterChip>
        ))}
      </div>

      {groups.length === 0 && <EmptyState compact title={t('meters.list.noMatch')} />}

      {groups.map((group) => (
        <section key={group.key} class="mt-group" aria-label={group.title}>
          <h2 class="mt-group__title">
            {group.title}
            {group.hint && <InfoTip label={group.title}>{group.hint}</InfoTip>}
            <span class="mt-group__count num">{group.meters.length + group.accounts.length}</span>
          </h2>
          {group.meters.length > 0 && (
            <div class="meter-grid">
              {group.meters.map((meter) => (
                <MeterCard key={meter.id} meter={meter} showGroup={false} />
              ))}
            </div>
          )}
          {group.accounts.length > 0 && (
            <ul class="mt-accounts">
              {group.accounts.map((account) => (
                <AccountRow key={account.id} account={account} />
              ))}
            </ul>
          )}
        </section>
      ))}

      {hidden.length > 0 && filter === 'all' && (
        <details class="mt-hidden">
          <summary>
            <EyeOff size={14} />
            {t('meters.list.hidden')}
            <span class="mt-group__count num">{hidden.length}</span>
          </summary>
          <div class="meter-grid">
            {hidden.map((meter) => (
              <MeterCard key={meter.id} meter={meter} />
            ))}
          </div>
        </details>
      )}
      {dialog}
    </>
  );
}

/** How an account with no meters stands, in the same words as a meter. */
function accountState(account: AccountView): { status: Status; note: string } {
  if (!account.enabled) return { status: 'paused', note: t('meters.list.account.off') };
  if (account.status === 'failing') return { status: 'failing', note: account.last_error || t('meters.list.account.failing') };
  if (account.status === 'pending') return { status: 'pending', note: t('meters.list.account.pending') };
  return { status: 'ok', note: t('meters.list.account.empty') };
}

function AccountRow({ account }: { account: AccountView }) {
  const manage = useAccountManager(account);
  const state = accountState(account);
  const reading = isSampling(account.id);
  const failures = account.enabled && account.consecutive_failures > 1 ? t('meters.account.failures', { n: account.consecutive_failures }) : '';
  return (
    <li class="mt-account" id={account.id}>
      <span class="mt-account__icon">
        <Plug size={18} />
      </span>
      <div class="mt-account__text">
        <p class="mt-account__name">
          <span class="truncate">{account.name}</span>
          <StatusDot status={state.status} />
        </p>
        <p class="mt-account__note dotted">
          {account.provider_name !== account.name && <span>{account.provider_name}</span>}
          <span class={state.status === 'failing' ? 'mt-account__error' : undefined}>{state.note}</span>
          {failures && <span>{failures}</span>}
        </p>
      </div>
      <div class="mt-account__actions">
        {account.enabled && (
          <IconButton
            icon={RefreshCw}
            label={reading ? t('common.action.reading') : t('common.action.readNow')}
            busy={reading}
            onClick={() => {
              sampleNow(account.id).catch((cause: unknown) => toast.error(errorText(cause)));
            }}
          />
        )}
        <Menu
          label={t('meters.account.actions')}
          icon={Ellipsis}
          entries={[
            { label: t('meters.account.edit'), icon: Pencil, onSelect: manage.edit },
            { label: account.enabled ? t('meters.account.turnOff') : t('meters.account.turnOn'), icon: Power, onSelect: manage.toggle },
            { separator: true },
            { label: t('meters.account.delete'), icon: Trash2, danger: true, onSelect: manage.remove },
          ]}
        />
      </div>
      {manage.dialogs}
    </li>
  );
}

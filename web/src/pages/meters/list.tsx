// The meters side of Watching: every meter as a row, the ones that need a
// look first and the soonest to become a problem after them; the accounts
// that have no meter yet, so they can still be found and fixed; and the
// hidden ones, folded away. On a wide screen the open meter stands beside
// the list, and with none open, how long every balance lasts.
//
//   /meters          the list
//   /meters/:id      the list, with that meter open
//   /meters?add=1    the list, with the form for a new account open
import { Ellipsis, EyeOff, Gauge, Pencil, Plug, Plus, Power, RefreshCw, Trash2 } from 'lucide-preact';
import type { AccountView, MeterView } from '../../api/types';
import { Button, EmptyState, ErrorState, IconButton, Menu, MeterRow, Skeleton, StatusDot, toast, type Status } from '../../components';
import { errorText } from '../../errors';
import { t } from '../../i18n';
import { route, setQuery } from '../../router';
import { accounts } from '../../state/accounts';
import { now } from '../../state/clock';
import { meters } from '../../state/meters';
import { useResource } from '../../state/resource';
import { isSampling, sampleNow } from '../../state/sampling';
import { settings } from '../../state/settings';
import { RunwayCard } from '../now/parts';
import { AccountDialog } from './account-dialog';
import { useAccountManager } from './account-manage';
import { MeterDetail } from './meter';
import { byUrgency, matchesMeter, needsLook } from './order';

export interface MetersSegmentProps {
  search: string;
  /** The meter open beside the list. */
  selected?: string | undefined;
  /** There is room for the open meter beside the list. */
  wide: boolean;
}

export function MetersSegment({ search, selected, wide }: MetersSegmentProps) {
  useResource(meters);
  useResource(accounts);
  useResource(settings);
  const query = route.value.query;
  const adding = query.has('add');
  const addProvider = query.get('add');
  const horizon = settings.data.value?.upcoming_days ?? 60;
  const dialog = <AccountDialog open={adding} onClose={() => setQuery({ add: null })} provider={addProvider && addProvider !== '1' ? addProvider : undefined} />;

  const allMeters = meters.data.value;
  const allAccounts = accounts.data.value;
  const detail = wide && (
    <div class="watching__detail">
      {selected ? (
        <MeterDetail key={selected} id={selected} />
      ) : allMeters ? (
        <div class="watching__overview">
          <RunwayCard meters={allMeters} horizon={horizon} />
          <p class="watching__hint">{t('meters.list.pick')}</p>
        </div>
      ) : null}
    </div>
  );

  if (!allMeters || !allAccounts) {
    const error = meters.error.value ?? accounts.error.value;
    return (
      <>
        <div class="watching__list">
          {error ? (
            <ErrorState
              error={error}
              onRetry={() => {
                void meters.load();
                void accounts.load();
              }}
            />
          ) : (
            <div class="w-skeleton" aria-busy="true">
              {[0, 1, 2, 3, 4].map((index) => (
                <Skeleton key={index} height="72px" />
              ))}
            </div>
          )}
        </div>
        {detail}
        {dialog}
      </>
    );
  }

  if (allMeters.length === 0 && allAccounts.length === 0) {
    return (
      <>
        <div class="watching__list">
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
        </div>
        {dialog}
      </>
    );
  }

  const order = byUrgency(now.value);
  const shown = allMeters.filter((meter) => !meter.hidden && matchesMeter(meter, search)).sort(order);
  const hidden = allMeters.filter((meter) => meter.hidden && matchesMeter(meter, search)).sort(order);
  // An account with no meters has nothing to open, so it is listed itself.
  const bare = allAccounts.filter((account) => account.meter_ids.length === 0 && matchesAccount(account, search));
  const look = shown.filter(needsLook);
  const fine = shown.filter((meter) => !needsLook(meter));
  const bareLook = bare.filter((account) => account.enabled && account.status === 'failing');
  const bareFine = bare.filter((account) => !(account.enabled && account.status === 'failing'));

  const row = (meter: MeterView) => (
    <li key={meter.id}>
      <MeterRow meter={meter} horizon={horizon} selected={meter.id === selected} />
    </li>
  );

  return (
    <>
      <div class="watching__list">
        {shown.length + hidden.length + bare.length === 0 && <EmptyState compact title={t('meters.list.noMatch')} />}
        {look.length + bareLook.length > 0 && (
          <section class="w-group" aria-labelledby="w-look">
            <h2 class="w-group__title" id="w-look">
              {t('meters.list.look')}
              <span class="w-group__count num">{look.length + bareLook.length}</span>
            </h2>
            <ul class="w-list">
              {look.map(row)}
              {bareLook.map((account) => (
                <AccountRow key={account.id} account={account} />
              ))}
            </ul>
          </section>
        )}
        {fine.length + bareFine.length > 0 && (
          <section class="w-group" aria-labelledby="w-fine">
            <h2 class="w-group__title" id="w-fine">
              {t('meters.list.fine')}
              <span class="w-group__count num">{fine.length + bareFine.length}</span>
            </h2>
            <ul class="w-list">
              {fine.map(row)}
              {bareFine.map((account) => (
                <AccountRow key={account.id} account={account} />
              ))}
            </ul>
          </section>
        )}
        {hidden.length > 0 && (
          <details class="w-group w-hidden" open={hidden.some((meter) => meter.id === selected)}>
            <summary class="w-group__title">
              <EyeOff size={14} />
              {t('meters.list.hidden')}
              <span class="w-group__count num">{hidden.length}</span>
            </summary>
            <ul class="w-list">{hidden.map(row)}</ul>
          </details>
        )}
      </div>
      {detail}
      {dialog}
    </>
  );
}

function matchesAccount(account: AccountView, typed: string): boolean {
  const query = typed.trim().toLowerCase();
  if (!query) return true;
  return [account.name, account.provider_name, account.group].some((field) => field.toLowerCase().includes(query));
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

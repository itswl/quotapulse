// What can be done to an account — edit, turn off or on, delete — with the
// dialogs that go with it. The meters list uses it for accounts that have no
// meter to open; a meter's page uses it for the meter's own account.
import type { VNode } from 'preact';
import { useState } from 'preact/hooks';
import { deleteAccount, updateAccount } from '../../api/endpoints';
import type { AccountView } from '../../api/types';
import { ConfirmDialog, toast } from '../../components';
import { errorText } from '../../errors';
import { useAction } from '../../hooks';
import { t } from '../../i18n';
import { AccountDialog } from './account-dialog';

export interface AccountManager {
  edit: () => void;
  /** Turns the account off, or back on. */
  toggle: () => void;
  toggling: boolean;
  /** Asks first. */
  remove: () => void;
  /** The dialogs; render this once, anywhere in the component. */
  dialogs: VNode;
}

export function useAccountManager(account: AccountView | undefined, onDeleted?: () => void): AccountManager {
  const [editing, setEditing] = useState(false);
  const [confirming, setConfirming] = useState(false);

  const toggle = useAction(async () => {
    if (!account) return;
    try {
      const next = await updateAccount(account.id, { enabled: !account.enabled });
      toast(t(next.enabled ? 'meters.account.turnedOn' : 'meters.account.turnedOff', { name: next.name }));
    } catch (cause) {
      toast.error(errorText(cause));
    }
  });

  const remove = useAction(async () => {
    if (!account) return;
    await deleteAccount(account.id);
    setConfirming(false);
    toast(t('meters.account.deleted', { name: account.name }));
    onDeleted?.();
  });

  const meterCount = account?.meter_ids.length ?? 0;
  return {
    edit: () => setEditing(true),
    toggle: () => void toggle.run(),
    toggling: toggle.busy,
    remove: () => {
      remove.clear();
      setConfirming(true);
    },
    dialogs: (
      <>
        <AccountDialog open={editing && account !== undefined} onClose={() => setEditing(false)} account={account} />
        <ConfirmDialog
          open={confirming && account !== undefined}
          onClose={() => setConfirming(false)}
          title={t('meters.account.delete.title', { name: account?.name ?? '' })}
          confirmLabel={t('meters.account.delete')}
          busy={remove.busy}
          error={remove.error === undefined ? undefined : errorText(remove.error)}
          onConfirm={() => void remove.run()}
        >
          <p>{meterCount > 0 ? t('meters.account.delete.body', { n: meterCount }) : t('meters.account.delete.bodyNone')}</p>
        </ConfirmDialog>
      </>
    ),
  };
}

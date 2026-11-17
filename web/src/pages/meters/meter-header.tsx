// The top of a meter's page: its name (which can be changed in place), its
// value and state, and the things that can be done to it.
import { BellOff, BellRing, ChevronLeft, Eye, EyeOff, Pencil, RefreshCw, Trash2 } from 'lucide-preact';
import type { JSX } from 'preact';
import { useState } from 'preact/hooks';
import { deleteMeter, muteMeter, unmuteMeter, updateMeter } from '../../api/endpoints';
import type { AccountView, MeterView } from '../../api/types';
import { Amount, Banner, Button, Chip, ConfirmDialog, IconButton, Input, isPushed, Menu, meterSource, StatusDot, toast, type MenuEntry } from '../../components';
import { errorText } from '../../errors';
import { amountParts, formatAgo, formatClock, formatDateTime } from '../../format';
import { useAction } from '../../hooks';
import { t, type TKey } from '../../i18n';
import { Link, navigate } from '../../router';
import { now } from '../../state/clock';
import { isSampling, sampleNow } from '../../state/sampling';

const HOUR = 3_600_000;
const MUTES: { label: TKey; ms: number }[] = [
  { label: 'meters.menu.mute.1h', ms: HOUR },
  { label: 'meters.menu.mute.8h', ms: 8 * HOUR },
  { label: 'meters.menu.mute.24h', ms: 24 * HOUR },
  { label: 'meters.menu.mute.7d', ms: 7 * 24 * HOUR },
];

export interface MeterHeaderProps {
  meter: MeterView;
  account: AccountView | undefined;
  /** Called with the server's answer after a change, so the page can show
   *  it without waiting for the next refresh. */
  onChange: (meter: MeterView) => void;
}

export function MeterHeader({ meter, account, onChange }: MeterHeaderProps) {
  const [renaming, setRenaming] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const pushed = isPushed(meter);

  // One action at a time; each reports its own failure as a toast.
  const act = useAction(async (work: () => Promise<MeterView>, done: (next: MeterView) => string) => {
    try {
      const next = await work();
      onChange(next);
      toast(done(next));
    } catch (cause) {
      toast.error(errorText(cause));
    }
  });

  const remove = useAction(async () => {
    await deleteMeter(meter.id);
    toast(t('meters.toast.deleted', { name: meter.display }));
    navigate('/meters', { replace: true });
  });

  const mute = (ms: number) =>
    // The server judges "in the future" by its own clock, so count from it.
    void act.run(
      () => muteMeter(meter.id, new Date(now.peek() + ms).toISOString()),
      (next) => t('meters.toast.muted', { time: next.muted_until ? formatClock(next.muted_until) : '' }),
    );

  const entries: MenuEntry[] = [{ heading: t('meters.menu.mute') }, ...MUTES.map((each) => ({ label: t(each.label), onSelect: () => mute(each.ms) }))];
  if (meter.muted) {
    entries.push({
      label: t('meters.menu.unmute'),
      icon: BellRing,
      onSelect: () => void act.run(() => unmuteMeter(meter.id), () => t('meters.toast.unmuted')),
    });
  }
  entries.push(
    { separator: true },
    {
      label: meter.hidden ? t('meters.menu.show') : t('meters.menu.hide'),
      icon: meter.hidden ? Eye : EyeOff,
      onSelect: () =>
        void act.run(
          () => updateMeter(meter.id, { hidden: !meter.hidden }),
          (next) => t(next.hidden ? 'meters.toast.hidden' : 'meters.toast.shown'),
        ),
    },
  );
  if (pushed) {
    entries.push({ separator: true }, { label: t('meters.menu.delete'), icon: Trash2, danger: true, onSelect: () => setConfirming(true) });
  }

  const reading = account ? isSampling(account.id) : false;
  const meta: string[] = [];
  // Until someone names it, an account's only meter goes by the account's
  // name; say what the platform calls it too.
  if (!meter.display.includes(meter.name)) meta.push(meter.name);
  meta.push(meterSource(meter));

  return (
    <>
      <header class="page-head">
        <div class="page-head__titles">
          <Link class="page-head__back" href="/meters">
            <ChevronLeft size={14} />
            {t('meters.page.all')}
          </Link>
          {renaming ? (
            <>
              <h1 class="sr-only">{meter.display}</h1>
              <RenameForm meter={meter} onDone={() => setRenaming(false)} onChange={onChange} />
            </>
          ) : (
            <div class="mt-head__name">
              <h1 class="page-head__title">{meter.display}</h1>
              <IconButton icon={Pencil} size="sm" label={t('meters.page.rename')} onClick={() => setRenaming(true)} />
            </div>
          )}
          <p class="page-head__subtitle mt-head__meta">
            {meta.map((part) => (
              <span key={part}>{part}</span>
            ))}
            {meter.sampled_at ? (
              <time dateTime={meter.sampled_at} title={formatDateTime(meter.sampled_at)}>
                {t(pushed ? 'meters.page.reported' : 'meters.page.read', { ago: formatAgo(meter.sampled_at) })}
              </time>
            ) : (
              <span>{t('meters.page.neverRead')}</span>
            )}
          </p>
        </div>
        <div class="page-head__actions">
          {account?.enabled && (
            <Button
              icon={RefreshCw}
              busy={reading}
              onClick={() => {
                sampleNow(account.id).catch((cause: unknown) => toast.error(errorText(cause)));
              }}
            >
              {reading ? t('common.action.reading') : t('common.action.readNow')}
            </Button>
          )}
          <Menu label={t('meters.menu.label')} entries={entries} outlined />
        </div>
      </header>

      <div class="mt-head__figure">
        <Amount class="mt-head__value" value={meter.value} unit={meter.unit} />
        {meter.kind === 'quota' && meter.unit !== '%' && meter.limit !== null && (
          <span class="mt-head__limit num">/ {amountParts(meter.limit, meter.unit).number}</span>
        )}
        <span class="mt-head__states">
          <StatusDot status={meter.status} size={16} />
          {meter.muted && meter.muted_until && (
            <Chip icon={BellOff} title={formatDateTime(meter.muted_until)}>
              {t('meters.page.mutedUntil', { time: formatClock(meter.muted_until) })}
            </Chip>
          )}
          {meter.hidden && <Chip icon={EyeOff}>{t('common.status.hidden')}</Chip>}
        </span>
      </div>

      {meter.hidden && (
        <Banner
          class="mt-head__hidden"
          icon={EyeOff}
          action={
            <Button size="sm" busy={act.busy} onClick={() => void act.run(() => updateMeter(meter.id, { hidden: false }), () => t('meters.toast.shown'))}>
              {t('meters.page.hidden.show')}
            </Button>
          }
        >
          {t('meters.page.hidden')}
        </Banner>
      )}

      <ConfirmDialog
        open={confirming}
        onClose={() => setConfirming(false)}
        title={t('meters.delete.title', { name: meter.display })}
        confirmLabel={t('meters.menu.delete')}
        busy={remove.busy}
        error={remove.error === undefined ? undefined : errorText(remove.error)}
        onConfirm={() => void remove.run()}
      >
        <p>{t('meters.delete.body')}</p>
      </ConfirmDialog>
    </>
  );
}

function RenameForm({ meter, onDone, onChange }: { meter: MeterView; onDone: () => void; onChange: (meter: MeterView) => void }) {
  // Start from the name on screen. Once saved, the meter goes by the new
  // name everywhere: lists, charts and notifications.
  const shown = meter.renamed ? meter.name : meter.display;
  const [name, setName] = useState(shown);
  const save = useAction(async () => {
    const next = name.trim();
    if (next === '' || next === shown) {
      onDone();
      return;
    }
    onChange(await updateMeter(meter.id, { name: next }));
    toast(t('meters.page.renamed'));
    onDone();
  });
  const submit = (event: JSX.TargetedEvent<HTMLFormElement>) => {
    event.preventDefault();
    void save.run();
  };
  return (
    <form
      class="mt-rename"
      onSubmit={submit}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          event.stopPropagation();
          onDone();
        }
      }}
    >
      <div class="mt-rename__row">
        <Input value={name} onInput={setName} aria-label={t('meters.page.rename.label')} maxLength={80} autoFocus autoComplete="off" />
        <Button type="submit" variant="primary" busy={save.busy}>
          {t('common.action.save')}
        </Button>
        <Button onClick={onDone} disabled={save.busy}>
          {t('common.action.cancel')}
        </Button>
      </div>
      {save.error !== undefined && (
        <p class="form-error" role="alert">
          {errorText(save.error)}
        </p>
      )}
    </form>
  );
}

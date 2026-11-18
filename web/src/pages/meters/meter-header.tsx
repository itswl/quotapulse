// The top of a meter: its name (which can be changed in place), the two
// figures that say where it stands, the bar that draws them, and the things
// that can be done to it — topping it up at its platform first.
import { ArrowUpRight, BellOff, BellRing, ChevronLeft, Eye, EyeOff, Pencil, RefreshCw, Trash2, X } from 'lucide-preact';
import type { ComponentChildren, TargetedEvent } from 'preact';
import { useState } from 'preact/hooks';
import { deleteMeter, muteMeter, unmuteMeter, updateMeter } from '../../api/endpoints';
import type { AccountView, MeterView } from '../../api/types';
import { RunwayBar } from '../../charts';
import {
  Amount,
  Banner,
  Button,
  Chip,
  ConfirmDialog,
  daysFigure,
  IconButton,
  Input,
  isPushed,
  Menu,
  MeterBudgetBar,
  MeterQuotaBar,
  meterSource,
  meterTone,
  runwayOf,
  runwayText,
  StatusDot,
  toast,
  type MenuEntry,
} from '../../components';
import { errorText } from '../../errors';
import { amountParts, formatAgo, formatAmount, formatClock, formatDateTime, formatDayOf, formatIn, toMs } from '../../format';
import { useAction } from '../../hooks';
import { pick, t, type TKey } from '../../i18n';
import { Link, navigate } from '../../router';
import { now } from '../../state/clock';
import { providers } from '../../state/providers';
import { useResource } from '../../state/resource';
import { isSampling, sampleNow } from '../../state/sampling';
import { settings } from '../../state/settings';

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
  /** The meter is the whole page: offer the way back to the list. Beside
   *  the list, offer to close it instead. */
  standalone: boolean;
}

export function MeterHeader({ meter, account, onChange, standalone }: MeterHeaderProps) {
  useResource(providers);
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
  const meta: string[] = [t(`meters.kind.${meter.kind}`)];
  // Until someone names it, an account's only meter goes by the account's
  // name; say what the platform calls it too.
  if (!meter.display.includes(meter.name)) meta.push(meter.name);
  meta.push(meterSource(meter));
  const definition = account ? providers.data.value?.find((each) => each.id === account.provider) : undefined;
  const unreadable = meter.account?.status === 'failing';

  return (
    <>
      <header class="meter-head">
        {standalone && (
          <Link class="meter-head__back" href="/meters">
            <ChevronLeft size={16} />
            {t('common.nav.watching')}
          </Link>
        )}
        <div class="meter-head__bar">
          <div class="meter-head__titles">
            {renaming ? (
              <>
                <h1 class="sr-only">{meter.display}</h1>
                <RenameForm meter={meter} onDone={() => setRenaming(false)} onChange={onChange} />
              </>
            ) : (
              <div class="meter-head__name">
                <h1 class="meter-head__title">{meter.display}</h1>
                <IconButton icon={Pencil} size="sm" label={t('meters.page.rename')} onClick={() => setRenaming(true)} />
              </div>
            )}
            <p class="meter-head__meta dotted">
              {[...new Set(meta)].map((part) => (
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
          <div class="meter-head__actions">
            {account?.enabled && (
              <Button
                icon={RefreshCw}
                compact
                busy={reading}
                onClick={() => {
                  sampleNow(account.id).catch((cause: unknown) => toast.error(errorText(cause)));
                }}
              >
                {reading ? t('common.action.reading') : t('common.action.readNow')}
              </Button>
            )}
            <Menu label={t('meters.menu.label')} entries={entries} outlined />
            {!standalone && <IconButton icon={X} label={t('common.action.close')} onClick={() => navigate('/meters')} />}
          </div>
        </div>

        <div class="meter-hero">
          <div class="meter-hero__states">
            <StatusDot status={unreadable ? 'failing' : meter.status} size={15} />
            {meter.muted && meter.muted_until && (
              <Chip icon={BellOff} title={formatDateTime(meter.muted_until)}>
                {t('meters.page.mutedUntil', { time: formatClock(meter.muted_until) })}
              </Chip>
            )}
            {meter.hidden && <Chip icon={EyeOff}>{t('common.status.hidden')}</Chip>}
          </div>
          <Hero meter={meter} />
          {definition?.homepage && !pushed && (
            <a class="btn btn--secondary meter-hero__remedy" href={definition.homepage} target="_blank" rel="noopener noreferrer">
              <span class="btn__label">{t('overview.action.open', { name: pick(definition.name, definition.name_zh) })}</span>
              <ArrowUpRight />
            </a>
          )}
        </div>
      </header>

      {meter.hidden && (
        <Banner
          class="meter-head__hidden"
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

function Stat({ label, children }: { label: string; children: ComponentChildren }) {
  return (
    <div class="meter-stat">
      <dt class="meter-stat__label">{label}</dt>
      <dd class="meter-stat__value">{children}</dd>
    </div>
  );
}

/** The two figures that say where a meter stands, what they come to, and
 *  the bar that draws it. Which two depends on the kind of meter. */
function Hero({ meter }: { meter: MeterView }) {
  useResource(settings);
  const horizon = settings.data.value?.upcoming_days ?? 60;
  const value = meter.value;

  if (meter.kind === 'quota') {
    const quota = meter.quota;
    return (
      <>
        <dl class="meter-hero__stats">
          <Stat label={t('meters.hero.left')}>{quota ? <Amount value={quota.remaining_pct} unit="%" /> : <Amount value={value} unit={meter.unit} />}</Stat>
          {meter.unit !== '%' && meter.limit !== null && (
            <Stat label={t('meters.hero.of')}>
              <span class="num">
                {value === null ? '—' : amountParts(value, meter.unit).number}
                <span class="unit unit--spaced">/ {formatAmount(meter.limit, meter.unit)}</span>
              </span>
            </Stat>
          )}
          {meter.resets_at && toMs(meter.resets_at) > now.value && <Stat label={t('meters.hero.resets')}>{formatIn(meter.resets_at)}</Stat>}
        </dl>
        {quota?.exhausts_at && toMs(quota.exhausts_at) > now.value && <p class="meter-hero__facts">{t('meters.card.exhausts', { time: formatClock(quota.exhausts_at) })}</p>}
        <MeterQuotaBar meter={meter} />
      </>
    );
  }

  if (meter.kind === 'counter') {
    const month = meter.month;
    const budget = meter.rules.budget;
    return (
      <>
        <dl class="meter-hero__stats">
          <Stat label={t('meters.hero.thisMonth')}>
            <Amount value={month?.spend ?? value} unit={meter.unit} />
          </Stat>
          {month && month.projected > month.spend && (
            <Stat label={t('meters.hero.heading')}>
              <Amount value={month.projected} unit={meter.unit} />
            </Stat>
          )}
          {budget !== null && budget > 0 && (
            <Stat label={t('meters.hero.budget')}>
              <Amount value={budget} unit={meter.unit} />
            </Stat>
          )}
        </dl>
        {month && budget ? <MeterBudgetBar meter={meter} /> : null}
      </>
    );
  }

  const runway = runwayOf(meter);
  const forecast = meter.forecast;
  const facts: string[] = [];
  if (forecast?.depletes_at && runway !== null && runway > 0 && runway < 365) facts.push(t('meters.card.depletes', { date: formatDayOf(forecast.depletes_at) }));
  if (forecast && forecast.confidence !== 'none' && forecast.burn_per_day > 0) facts.push(t('meters.hero.burn', { amount: formatAmount(forecast.burn_per_day, meter.unit) }));
  if (runway === null && value !== null) facts.push(t('meters.card.collecting'));
  const figure = runway === null ? null : daysFigure(runway);
  return (
    <>
      <dl class="meter-hero__stats">
        <Stat label={t('meters.hero.balance')}>
          <Amount value={value} unit={meter.unit} />
        </Stat>
        {figure && (
          <Stat label={t('meters.hero.lasts')}>
            {'words' in figure ? (
              figure.words
            ) : (
              <span class="num">
                {figure.number}
                <span class="unit unit--spaced">{figure.unit}</span>
              </span>
            )}
          </Stat>
        )}
      </dl>
      {facts.length > 0 && <p class="meter-hero__facts">{facts.join(' · ')}</p>}
      {runway !== null && <RunwayBar days={runway} horizon={horizon} tone={meterTone(meter)} label={runwayText(runway)} />}
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
  const submit = (event: TargetedEvent<HTMLFormElement>) => {
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

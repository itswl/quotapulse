// A meter's alert rules. The form sends the whole rules object back
// (PATCH /meters/{id} replaces it), so a rule this kind of meter does not
// show keeps whatever value it had.
import type { JSX } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { updateMeter } from '../../api/endpoints';
import type { MeterView, Rules } from '../../api/types';
import { Button, Card, Field, Input, isPushed, Segmented, Select, Switch, toast } from '../../components';
import { errorText } from '../../errors';
import { formatSpan, isCurrency } from '../../format';
import { useAction } from '../../hooks';
import { t } from '../../i18n';
import { useResource } from '../../state/resource';
import { settings } from '../../state/settings';

/** The form's own shape: what is in the inputs, as text. */
interface Draft {
  low: string;
  runway: string;
  anomaly: 'default' | 'on' | 'off';
  budget: string;
  pace: boolean;
  stale: string;
}

const text = (value: number | null) => (value === null ? '' : String(value));

function draftOf(rules: Rules): Draft {
  return {
    low: text(rules.low),
    runway: text(rules.runway_days),
    anomaly: rules.anomaly === null ? 'default' : rules.anomaly ? 'on' : 'off',
    budget: text(rules.budget),
    pace: rules.pace,
    stale: String(rules.stale_after_seconds ?? 0),
  };
}

const same = (a: Draft, b: Draft) => JSON.stringify(a) === JSON.stringify(b);

/** Reads a number someone typed: "", "1,200.5", " 7 ". */
function parse(input: string): number | null | 'invalid' {
  const cleaned = input.replace(/[,\s]/g, '');
  if (cleaned === '') return null;
  const value = Number(cleaned);
  return Number.isFinite(value) ? value : 'invalid';
}

/** Silence limits offered for a pushed meter, in seconds; 0 is off. */
const STALE = [0, 3600, 6 * 3600, 12 * 3600, 24 * 3600, 3 * 86400, 7 * 86400];

export function MeterRules({ meter, onChange }: { meter: MeterView; onChange: (meter: MeterView) => void }) {
  useResource(settings);
  const defaults = settings.data.value;
  const saved = draftOf(meter.rules);
  const [draft, setDraft] = useState(saved);
  const [errors, setErrors] = useState<Partial<Record<keyof Draft, string>>>({});

  // When the server's rules change — after a save, or from another window —
  // follow them, unless there are edits in progress.
  const base = useRef(saved);
  const snapshot = JSON.stringify(saved);
  useEffect(() => {
    setDraft((current) => (same(current, base.current) ? saved : current));
    base.current = saved;
  }, [snapshot]);

  const dirty = !same(draft, saved);
  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => {
    setDraft({ ...draft, [key]: value });
    setErrors({ ...errors, [key]: undefined });
  };

  // Which rules the engine applies to which kind of meter (internal/engine/
  // evaluate.go): a threshold means something for a balance and a quota, not
  // for a running total.
  const balance = meter.kind === 'balance';
  const quota = meter.kind === 'quota';
  const counter = meter.kind === 'counter';
  const pushed = isPushed(meter);

  const save = useAction(async () => {
    const problems: Partial<Record<keyof Draft, string>> = {};
    const low = parse(draft.low);
    const runway = parse(draft.runway);
    const budget = parse(draft.budget);
    if (low === 'invalid') problems.low = t('meters.rules.invalid.number');
    if (runway === 'invalid' || (runway !== null && (runway < 0 || runway > 365))) problems.runway = t('meters.rules.invalid.runway');
    if (budget === 'invalid') problems.budget = t('meters.rules.invalid.number');
    else if (budget !== null && budget < 0) problems.budget = t('meters.rules.invalid.negative');
    setErrors(problems);
    if (Object.keys(problems).length > 0) return;

    const rules: Rules = { ...meter.rules };
    if (!counter) rules.low = low as number | null;
    if (balance) {
      rules.runway_days = runway as number | null;
      rules.anomaly = draft.anomaly === 'default' ? null : draft.anomaly === 'on';
    }
    if (!quota) rules.budget = budget as number | null;
    if (quota) rules.pace = draft.pace;
    if (pushed) rules.stale_after_seconds = Number(draft.stale) || null;

    const next = await updateMeter(meter.id, { rules });
    base.current = draftOf(next.rules);
    setDraft(base.current);
    onChange(next);
    toast(t('meters.rules.saved'));
  });

  const submit = (event: JSX.TargetedEvent<HTMLFormElement>) => {
    event.preventDefault();
    void save.run();
  };

  const unit = meter.unit;
  const suffix = unit && !isCurrency(unit) && unit.length > 6 ? undefined : unit || undefined;
  // The placeholder shows what an empty field means: the default in force.
  const leadDefault = defaults?.runway_days ?? (meter.rules.runway_days === null ? meter.effective.runway_days : undefined);
  const burstDefault = defaults?.anomaly ?? (meter.rules.anomaly === null ? meter.effective.anomaly : undefined);
  const staleOptions = STALE.map((seconds) => ({ value: String(seconds), label: seconds === 0 ? t('common.word.off') : formatSpan(seconds * 1000) }));
  if (!STALE.includes(Number(draft.stale))) staleOptions.push({ value: draft.stale, label: formatSpan(Number(draft.stale) * 1000) });

  return (
    <Card class="mt-rules">
      <h2 class="mt-card-title">{t('meters.rules.title')}</h2>
      <form class="mt-rules__form" onSubmit={submit} noValidate>
        {!counter && (
          <Field label={t('meters.rules.low')} help={t('meters.rules.low.help')} error={errors.low}>
            <Input mono inputMode="decimal" value={draft.low} onInput={(value) => set('low', value)} placeholder={t('meters.rules.none')} suffix={suffix} autoComplete="off" />
          </Field>
        )}

        {balance && (
          <Field label={t('meters.rules.runway')} help={t('meters.rules.runway.help')} error={errors.runway}>
            <Input
              mono
              inputMode="decimal"
              value={draft.runway}
              onInput={(value) => set('runway', value)}
              placeholder={leadDefault === undefined ? '' : t('meters.rules.runway.placeholder', { n: leadDefault })}
              suffix={t('meters.rules.runway.unit')}
              autoComplete="off"
            />
          </Field>
        )}

        {!quota && (
          <Field label={t('meters.rules.budget')} help={t('meters.rules.budget.help')} error={errors.budget}>
            <Input mono inputMode="decimal" value={draft.budget} onInput={(value) => set('budget', value)} placeholder={t('meters.rules.none')} suffix={suffix} autoComplete="off" />
          </Field>
        )}

        {balance && (
          <Field label={t('meters.rules.anomaly')} help={t('meters.rules.anomaly.help')}>
            <Segmented
              value={draft.anomaly}
              onChange={(value) => set('anomaly', value)}
              options={[
                {
                  value: 'default',
                  label:
                    burstDefault === undefined
                      ? t('common.word.default')
                      : t('meters.rules.anomaly.default', { state: (burstDefault ? t('common.word.on') : t('common.word.off')).toLowerCase() }),
                },
                { value: 'on', label: t('common.word.on') },
                { value: 'off', label: t('common.word.off') },
              ]}
            />
          </Field>
        )}

        {quota && (
          <Field class="mt-rules__switch" label={t('meters.rules.pace')} help={t('meters.rules.pace.help')}>
            <Switch checked={draft.pace} onChange={(value) => set('pace', value)} />
          </Field>
        )}

        {pushed && (
          <Field label={t('meters.rules.stale')} help={t('meters.rules.stale.help')}>
            <Select value={draft.stale} onChange={(value) => set('stale', value)} options={staleOptions} />
          </Field>
        )}

        {save.error !== undefined && (
          <p class="form-error" role="alert">
            {errorText(save.error)}
          </p>
        )}

        <div class="mt-rules__actions">
          <Button type="submit" variant="primary" busy={save.busy} disabled={!dirty}>
            {t('common.action.save')}
          </Button>
          {dirty && (
            <Button
              variant="ghost"
              disabled={save.busy}
              onClick={() => {
                setDraft(saved);
                setErrors({});
                save.clear();
              }}
            >
              {t('meters.rules.discard')}
            </Button>
          )}
        </div>
      </form>
    </Card>
  );
}

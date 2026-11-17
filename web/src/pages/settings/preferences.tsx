// Preferences: the defaults everything else falls back on. Five groups, read
// top to bottom, each saved on its own — PATCH /settings takes a partial
// update (and `fx` whole).
//
// A group keeps what was typed until it is saved or discarded. What the
// server refuses comes back as "field: what is wrong; field: …", and each
// part is shown under the field it names.
import { Plus, Send, X } from 'lucide-preact';
import type { ComponentChildren, JSX } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { ApiError } from '../../api/client';
import { listChannels, sendDigest, updateSettings } from '../../api/endpoints';
import type { Settings } from '../../api/types';
import { Button, Card, ErrorState, Field, IconButton, Input, Segmented, Select, Skeleton, Switch, toast } from '../../components';
import { errorText } from '../../errors';
import { formatTime } from '../../format';
import { useAction } from '../../hooks';
import { t, type TKey } from '../../i18n';
import { now, setTimezone } from '../../state/clock';
import { useResource } from '../../state/resource';
import { settings } from '../../state/settings';
import { numberText, otherProblems, parseNumber, problemFor, rateRows, ratesOf, splitProblems, type RateRow } from './logic';
import { Block, Group, Labelled, TimeInput } from './parts';
import { ZoneSelect } from './zone-select';

/** What is wrong, by the server's name for the field. */
type Errors = Record<string, string>;

interface Spec<D> {
  /** This group's part of the settings, as its form holds it. */
  read: (current: Settings) => D;
  /** The update a draft amounts to, or what is wrong with it. */
  write: (draft: D, current: Settings) => { patch: Partial<Settings>; errors: Errors };
  /** What each part of the draft is called, for saying what changed. */
  names: Record<keyof D, TKey>;
  /** The server's names for the fields this group shows. */
  fields: readonly string[];
}

interface GroupState<D> {
  draft: D;
  /** Changes the draft, and clears the errors of the fields named. */
  set: (change: Partial<D>, ...fields: string[]) => void;
  errors: Errors;
  /** Problems the server reported that belong to no field shown here. */
  general: string[];
  /** The names of what differs from what is saved. */
  changed: string[];
  busy: boolean;
  failure: unknown;
  save: () => void;
  reset: () => void;
}

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

function useGroup<D extends object>(current: Settings, spec: Spec<D>): GroupState<D> {
  const saved = spec.read(current);
  const [draft, setDraft] = useState(saved);
  const [errors, setErrors] = useState<Errors>({});
  const [general, setGeneral] = useState<string[]>([]);

  // When the saved settings change — after a save, or from another window —
  // follow them, unless there are edits in progress.
  const base = useRef(saved);
  const snapshot = JSON.stringify(saved);
  useEffect(() => {
    setDraft((was) => (same(was, base.current) ? saved : was));
    base.current = saved;
  }, [snapshot]);

  const keys = (Object.keys(draft) as (keyof D)[]).filter((key) => !same(draft[key], saved[key]));
  const changed = [...new Set(keys.map((key) => t(spec.names[key])))];

  const save = useAction(async () => {
    const { patch, errors: found } = spec.write(draft, current);
    setErrors(found);
    setGeneral([]);
    if (Object.keys(found).length > 0) return;
    try {
      const next = await updateSettings(patch);
      settings.set(next);
      setTimezone(next.timezone);
      const after = spec.read(next);
      base.current = after;
      setDraft(after);
      toast(t('settings.prefs.saved', { list: changed.join(t('common.list.separator')) }));
    } catch (cause) {
      if (!(cause instanceof ApiError) || cause.code !== 'invalid') throw cause;
      const problems = splitProblems(cause.message);
      const mine: Errors = {};
      for (const field of spec.fields) {
        const text = problemFor(problems, field);
        if (text) mine[field] = text;
      }
      setErrors(mine);
      setGeneral(otherProblems(problems, spec.fields));
    }
  });

  return {
    draft,
    set: (change, ...fields) => {
      setDraft({ ...draft, ...change });
      if (fields.some((field) => errors[field])) setErrors(Object.fromEntries(Object.entries(errors).filter(([key]) => !fields.includes(key))));
    },
    errors,
    general,
    changed,
    busy: save.busy,
    failure: save.error,
    save: () => void save.run(),
    reset: () => {
      setDraft(saved);
      setErrors({});
      setGeneral([]);
      save.clear();
    },
  };
}

/** Reads a number field, noting what is wrong with it under `field`. */
function numberOf(text: string, field: string, errors: Errors, whole = false): number {
  const value = parseNumber(text);
  if (value === null || value === 'invalid') errors[field] = t('settings.prefs.invalid.number');
  else if (whole && !Number.isInteger(value)) errors[field] = t('settings.prefs.invalid.whole');
  else return value;
  return 0;
}

function GroupCard<D>({ title, about, group, children }: { title: string; about: string; group: GroupState<D>; children: ComponentChildren }) {
  const dirty = group.changed.length > 0;
  const submit = (event: JSX.TargetedEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (dirty) group.save();
  };
  return (
    <Block title={title} about={about}>
      <Card>
        <form class="st-prefs" onSubmit={submit} noValidate>
          {children}
          {group.general.length > 0 && (
            <ul class="form-error st-problems" role="alert">
              {group.general.map((text) => (
                <li key={text}>{text}</li>
              ))}
            </ul>
          )}
          {group.failure !== undefined && (
            <p class="form-error" role="alert">
              {errorText(group.failure)}
            </p>
          )}
          <div class="st-prefs__foot">
            <Button type="submit" variant="primary" busy={group.busy} disabled={!dirty}>
              {t('common.action.save')}
            </Button>
            {dirty && (
              <Button variant="ghost" disabled={group.busy} onClick={group.reset}>
                {t('settings.prefs.discard')}
              </Button>
            )}
            <p class="st-prefs__changed" aria-live="polite">
              {dirty ? t('settings.prefs.changed', { list: group.changed.join(t('common.list.separator')) }) : ''}
            </p>
          </div>
        </form>
      </Card>
    </Block>
  );
}

export function PreferencesSection() {
  useResource(settings);
  const current = settings.data.value;
  if (!current) {
    if (settings.error.value) return <ErrorState error={settings.error.value} onRetry={() => void settings.load()} />;
    return (
      <div class="st-blocks" aria-busy="true">
        <span class="sr-only">{t('common.state.loading')}</span>
        {[0, 1, 2].map((index) => (
          <Skeleton key={index} height="180px" />
        ))}
      </div>
    );
  }
  return (
    <div class="st-blocks">
      <TimeAndLanguage current={current} />
      <Money current={current} />
      <Watching current={current} />
      <Notifying current={current} />
      <Keeping current={current} />
    </div>
  );
}

// ---------- time and language ----------

interface TimeDraft {
  timezone: string;
  language: Settings['language'];
}

const TIME: Spec<TimeDraft> = {
  read: (s) => ({ timezone: s.timezone, language: s.language }),
  write: (draft) => ({ patch: { timezone: draft.timezone, language: draft.language }, errors: {} }),
  names: { timezone: 'settings.prefs.timezone', language: 'settings.prefs.language' },
  fields: ['timezone', 'language'],
};

function TimeAndLanguage({ current }: { current: Settings }) {
  const group = useGroup(current, TIME);
  const { draft } = group;
  let clock = '';
  try {
    clock = formatTime(now.value, draft.timezone);
  } catch {
    // a zone this browser does not know: say nothing about its time
  }
  return (
    <GroupCard title={t('settings.prefs.time.title')} about={t('settings.prefs.time.about')} group={group}>
      <Field
        label={t('settings.prefs.timezone')}
        help={
          <>
            {t('settings.prefs.timezone.help')} {clock && t('settings.prefs.timezone.now', { time: clock })}
          </>
        }
        error={group.errors.timezone}
      >
        <ZoneSelect value={draft.timezone} onChange={(timezone) => group.set({ timezone }, 'timezone')} />
      </Field>
      <Field label={t('settings.prefs.language')} help={t('settings.prefs.language.help')} error={group.errors.language}>
        <Segmented
          value={draft.language}
          onChange={(language) => group.set({ language }, 'language')}
          options={[
            { value: 'zh', label: t('settings.prefs.language.zh') },
            { value: 'en', label: t('settings.prefs.language.en') },
          ]}
        />
      </Field>
    </GroupCard>
  );
}

// ---------- money ----------

interface MoneyDraft {
  base: string;
  rates: RateRow[];
}

const MONEY: Spec<MoneyDraft> = {
  read: (s) => ({ base: s.base_currency, rates: rateRows(s.fx) }),
  write: (draft) => {
    const { fx, problems } = ratesOf(draft.rates);
    const errors: Errors = {};
    for (const [index, kind] of problems) errors[`fx.${index}`] = t(`settings.prefs.fx.problem.${kind}`);
    return { patch: { base_currency: draft.base.trim().toUpperCase(), fx }, errors };
  },
  names: { base: 'settings.prefs.base', rates: 'settings.prefs.fx' },
  fields: ['base_currency', 'fx'],
};

function Money({ current }: { current: Settings }) {
  const group = useGroup(current, MONEY);
  const { draft } = group;
  const base = draft.base.trim().toUpperCase() || '?';
  const setRow = (index: number, change: Partial<RateRow>) =>
    group.set({ rates: draft.rates.map((row, at) => (at === index ? { ...row, ...change } : row)) }, 'fx', `fx.${index}`);
  // Removing a row renumbers the ones after it, so their errors go too.
  const clearRows = draft.rates.map((_, index) => `fx.${index}`);

  return (
    <GroupCard title={t('settings.prefs.money.title')} about={t('settings.prefs.money.about')} group={group}>
      <Field label={t('settings.prefs.base')} help={t('settings.prefs.base.help')} error={group.errors.base_currency}>
        <Input mono class="st-short" value={draft.base} onInput={(value) => group.set({ base: value.toUpperCase() }, 'base_currency')} maxLength={3} placeholder="USD" autoComplete="off" autoCapitalize="characters" spellcheck={false} />
      </Field>

      <Group label={t('settings.prefs.fx')} help={t('settings.prefs.fx.help', { base })} error={group.errors.fx}>
        {draft.rates.length === 0 ? (
          <p class="st-note">{t('settings.prefs.fx.none')}</p>
        ) : (
          <ul class="st-rates">
            {draft.rates.map((row, index) => (
              <li key={index} class="st-rate">
                <span class="st-rate__sign num" aria-hidden="true">
                  1
                </span>
                <Input
                  mono
                  class="st-rate__code"
                  value={row.code}
                  onInput={(code) => setRow(index, { code: code.toUpperCase() })}
                  maxLength={3}
                  placeholder="USD"
                  aria-label={t('settings.prefs.fx.code', { n: index + 1 })}
                  aria-invalid={group.errors[`fx.${index}`] ? true : undefined}
                  autoComplete="off"
                  autoCapitalize="characters"
                  spellcheck={false}
                />
                <span class="st-rate__sign" aria-hidden="true">
                  =
                </span>
                <Input
                  mono
                  class="st-rate__value"
                  inputMode="decimal"
                  value={row.rate}
                  onInput={(rate) => setRow(index, { rate })}
                  suffix={base}
                  aria-label={t('settings.prefs.fx.rate', { code: row.code || '?', base })}
                  aria-invalid={group.errors[`fx.${index}`] ? true : undefined}
                  autoComplete="off"
                />
                <IconButton icon={X} label={t('settings.prefs.fx.remove', { code: row.code || String(index + 1) })} onClick={() => group.set({ rates: draft.rates.filter((_, at) => at !== index) }, 'fx', ...clearRows)} />
                {group.errors[`fx.${index}`] && (
                  <p class="field__error st-rate__error" role="alert">
                    {group.errors[`fx.${index}`]}
                  </p>
                )}
              </li>
            ))}
          </ul>
        )}
        <Button variant="ghost" size="sm" icon={Plus} class="st-add" onClick={() => group.set({ rates: [...draft.rates, { code: '', rate: '' }] }, 'fx')}>
          {t('settings.prefs.fx.add')}
        </Button>
      </Group>
    </GroupCard>
  );
}

// ---------- watching ----------

interface WatchDraft {
  minutes: string;
  runway: string;
  anomaly: boolean;
}

const WATCHING: Spec<WatchDraft> = {
  read: (s) => ({ minutes: numberText(s.sample_seconds / 60), runway: numberText(s.runway_days), anomaly: s.anomaly }),
  write: (draft) => {
    const errors: Errors = {};
    // The server counts in seconds, 60 to 86400; the form in minutes.
    const minutes = numberOf(draft.minutes, 'sample_seconds', errors);
    if (!errors.sample_seconds && (minutes < 1 || minutes > 1440)) errors.sample_seconds = t('settings.prefs.sample.range');
    const runway = numberOf(draft.runway, 'runway_days', errors);
    return { patch: { sample_seconds: Math.round(minutes * 60), runway_days: runway, anomaly: draft.anomaly }, errors };
  },
  names: { minutes: 'settings.prefs.sample', runway: 'settings.prefs.runway', anomaly: 'settings.prefs.anomaly' },
  fields: ['sample_seconds', 'runway_days'],
};

function Watching({ current }: { current: Settings }) {
  const group = useGroup(current, WATCHING);
  const { draft } = group;
  return (
    <GroupCard title={t('settings.prefs.watch.title')} about={t('settings.prefs.watch.about')} group={group}>
      <Field label={t('settings.prefs.sample')} help={t('settings.prefs.sample.help')} error={group.errors.sample_seconds}>
        <Input mono class="st-short" inputMode="decimal" value={draft.minutes} onInput={(minutes) => group.set({ minutes }, 'sample_seconds')} suffix={t('settings.prefs.unit.minutes')} autoComplete="off" />
      </Field>
      <Field label={t('settings.prefs.runway')} help={t('settings.prefs.runway.help')} error={group.errors.runway_days}>
        <Input mono class="st-short" inputMode="decimal" value={draft.runway} onInput={(runway) => group.set({ runway }, 'runway_days')} suffix={t('settings.prefs.unit.days')} autoComplete="off" />
      </Field>
      <Field class="st-switch" label={t('settings.prefs.anomaly')} help={t('settings.prefs.anomaly.help')}>
        <Switch checked={draft.anomaly} onChange={(anomaly) => group.set({ anomaly })} />
      </Field>
    </GroupCard>
  );
}

// ---------- notifying ----------

interface NotifyDraft {
  reminderTime: string;
  quietOn: boolean;
  quietStart: string;
  quietEnd: string;
  /** "off", "0" for every day, or "1" Monday … "7" Sunday. */
  digest: string;
  digestTime: string;
  renotify: string;
  resolved: boolean;
  publicUrl: string;
}

const NOTIFYING: Spec<NotifyDraft> = {
  read: (s) => ({
    reminderTime: s.reminder_time,
    quietOn: s.quiet.enabled,
    quietStart: s.quiet.start,
    quietEnd: s.quiet.end,
    digest: s.digest.enabled ? String(s.digest.weekday) : 'off',
    digestTime: s.digest.time,
    renotify: String(s.renotify_hours),
    resolved: s.notify_resolved,
    publicUrl: s.public_url,
  }),
  write: (draft, s) => {
    const errors: Errors = {};
    const renotify = numberOf(draft.renotify, 'renotify_hours', errors, true);
    return {
      patch: {
        reminder_time: draft.reminderTime,
        quiet: { enabled: draft.quietOn, start: draft.quietStart, end: draft.quietEnd },
        // Turned off, the digest keeps the day it was set to.
        digest: draft.digest === 'off' ? { enabled: false, weekday: s.digest.weekday, time: draft.digestTime } : { enabled: true, weekday: Number(draft.digest), time: draft.digestTime },
        renotify_hours: renotify,
        notify_resolved: draft.resolved,
        public_url: draft.publicUrl.trim(),
      },
      errors,
    };
  },
  names: {
    reminderTime: 'settings.prefs.reminderTime',
    quietOn: 'settings.prefs.quiet',
    quietStart: 'settings.prefs.quiet',
    quietEnd: 'settings.prefs.quiet',
    digest: 'settings.prefs.digest',
    digestTime: 'settings.prefs.digest',
    renotify: 'settings.prefs.renotify',
    resolved: 'settings.prefs.resolved',
    publicUrl: 'settings.prefs.publicUrl',
  },
  fields: ['reminder_time', 'quiet.start', 'quiet.end', 'digest.weekday', 'digest.time', 'renotify_hours', 'public_url'],
};

const DIGEST_DAYS: readonly TKey[] = [
  'settings.prefs.digest.daily',
  'settings.prefs.digest.w1',
  'settings.prefs.digest.w2',
  'settings.prefs.digest.w3',
  'settings.prefs.digest.w4',
  'settings.prefs.digest.w5',
  'settings.prefs.digest.w6',
  'settings.prefs.digest.w7',
];

function Notifying({ current }: { current: Settings }) {
  const group = useGroup(current, NOTIFYING);
  const { draft } = group;
  const origin = location.origin;

  // The digest goes to channels that take the topic and everything down to
  // "info" (core.Channel.Wants); with none, sending it would do nothing.
  const digestNow = useAction(async () => {
    try {
      const takers = (await listChannels()).filter((channel) => channel.enabled && channel.topics.includes('digest') && channel.min_severity === 'info');
      if (takers.length === 0) {
        toast(t('settings.prefs.digest.nobody'), { tone: 'info', duration: 8000 });
        return;
      }
      await sendDigest();
      toast(t('settings.prefs.digest.sent', { n: takers.length }));
    } catch (cause) {
      toast.error(errorText(cause));
    }
  });

  return (
    <GroupCard title={t('settings.prefs.notify.title')} about={t('settings.prefs.notify.about')} group={group}>
      <Labelled label={t('settings.prefs.reminderTime')} help={t('settings.prefs.reminderTime.help')} error={group.errors.reminder_time}>
        {(ids) => <TimeInput {...ids} value={draft.reminderTime} onInput={(reminderTime) => group.set({ reminderTime }, 'reminder_time')} />}
      </Labelled>

      <Field class="st-switch" label={t('settings.prefs.quiet')} help={t('settings.prefs.quiet.help')}>
        <Switch checked={draft.quietOn} onChange={(quietOn) => group.set({ quietOn })} />
      </Field>
      {draft.quietOn && (
        <div class="st-prefs__pair">
          <Labelled label={t('settings.prefs.quiet.start')} error={group.errors['quiet.start']}>
            {(ids) => <TimeInput {...ids} value={draft.quietStart} onInput={(quietStart) => group.set({ quietStart }, 'quiet.start')} />}
          </Labelled>
          <Labelled label={t('settings.prefs.quiet.end')} error={group.errors['quiet.end']}>
            {(ids) => <TimeInput {...ids} value={draft.quietEnd} onInput={(quietEnd) => group.set({ quietEnd }, 'quiet.end')} />}
          </Labelled>
        </div>
      )}

      <div class="st-prefs__pair">
        <Field label={t('settings.prefs.digest')} error={group.errors['digest.weekday']}>
          <Select
            value={draft.digest}
            onChange={(digest) => group.set({ digest }, 'digest.weekday')}
            options={[{ value: 'off', label: t('settings.prefs.digest.off') }, ...DIGEST_DAYS.map((key, weekday) => ({ value: String(weekday), label: t(key) }))]}
          />
        </Field>
        {draft.digest !== 'off' && (
          <Labelled label={t('settings.prefs.digest.time')} error={group.errors['digest.time']}>
            {(ids) => <TimeInput {...ids} value={draft.digestTime} onInput={(digestTime) => group.set({ digestTime }, 'digest.time')} />}
          </Labelled>
        )}
      </div>
      <div class="st-prefs__aside">
        <p class="field__help">{t('settings.prefs.digest.help')}</p>
        <Button size="sm" icon={Send} busy={digestNow.busy} onClick={() => void digestNow.run()}>
          {t('settings.prefs.digest.now')}
        </Button>
      </div>

      <Field label={t('settings.prefs.renotify')} help={t('settings.prefs.renotify.help')} error={group.errors.renotify_hours}>
        <Input mono class="st-short" inputMode="numeric" value={draft.renotify} onInput={(renotify) => group.set({ renotify }, 'renotify_hours')} suffix={t('settings.prefs.unit.hours')} autoComplete="off" />
      </Field>

      <Field class="st-switch" label={t('settings.prefs.resolved')} help={t('settings.prefs.resolved.help')}>
        <Switch checked={draft.resolved} onChange={(resolved) => group.set({ resolved })} />
      </Field>

      <Field
        label={t('settings.prefs.publicUrl')}
        help={
          <>
            {t('settings.prefs.publicUrl.help')}{' '}
            {draft.publicUrl.trim() !== origin && (
              <button type="button" class="st-inline" onClick={() => group.set({ publicUrl: origin }, 'public_url')}>
                {t('settings.prefs.publicUrl.use', { origin })}
              </button>
            )}
          </>
        }
        error={group.errors.public_url}
      >
        <Input mono type="url" inputMode="url" value={draft.publicUrl} onInput={(publicUrl) => group.set({ publicUrl }, 'public_url')} placeholder="https://pulse.example.com" autoComplete="off" spellcheck={false} autoCapitalize="off" />
      </Field>
    </GroupCard>
  );
}

// ---------- keeping ----------

interface KeepDraft {
  retention: string;
  upcoming: string;
}

const KEEPING: Spec<KeepDraft> = {
  read: (s) => ({ retention: String(s.retention_days), upcoming: String(s.upcoming_days) }),
  write: (draft) => {
    const errors: Errors = {};
    const retention = numberOf(draft.retention, 'retention_days', errors, true);
    const upcoming = numberOf(draft.upcoming, 'upcoming_days', errors, true);
    return { patch: { retention_days: retention, upcoming_days: upcoming }, errors };
  },
  names: { retention: 'settings.prefs.retention', upcoming: 'settings.prefs.upcoming' },
  fields: ['retention_days', 'upcoming_days'],
};

function Keeping({ current }: { current: Settings }) {
  const group = useGroup(current, KEEPING);
  const { draft } = group;
  return (
    <GroupCard title={t('settings.prefs.keep.title')} about={t('settings.prefs.keep.about')} group={group}>
      <Field label={t('settings.prefs.retention')} help={t('settings.prefs.retention.help')} error={group.errors.retention_days}>
        <Input mono class="st-short" inputMode="numeric" value={draft.retention} onInput={(retention) => group.set({ retention }, 'retention_days')} suffix={t('settings.prefs.unit.days')} autoComplete="off" />
      </Field>
      <Field label={t('settings.prefs.upcoming')} help={t('settings.prefs.upcoming.help')} error={group.errors.upcoming_days}>
        <Input mono class="st-short" inputMode="numeric" value={draft.upcoming} onInput={(upcoming) => group.set({ upcoming }, 'upcoming_days')} suffix={t('settings.prefs.unit.days')} autoComplete="off" />
      </Field>
    </GroupCard>
  );
}

// Adding a channel, and editing one: the same form, reached two ways.
//
//   <ChannelDialog open onClose={…} />                    pick a type, fill in the form, then try it
//   <ChannelDialog open onClose={…} channel={channel} />  the form, for an existing channel
//
// The form's fields come from the type (GET /channel-types), so a type added
// tomorrow needs no change here.
import { ArrowLeft, CircleCheck, Send } from 'lucide-preact';
import type { JSX } from 'preact';
import { useEffect, useMemo, useState } from 'preact/hooks';
import { ApiError } from '../../api/client';
import { createChannel, testChannel, updateChannel } from '../../api/endpoints';
import type { ChannelInput, ChannelType, ChannelView, Field as TypeField, Severity, Topic } from '../../api/types';
import { Button, Dialog, ErrorState, Field, Input, Segmented, Skeleton, toast } from '../../components';
import { errorText } from '../../errors';
import { useAction } from '../../hooks';
import { lang, pick, t, translate, type TKey } from '../../i18n';
import { CheckBox, Group } from './parts';
import { channels, channelTypes } from './state';

export const TOPICS: readonly Topic[] = ['meters', 'dates', 'system', 'digest'];
export const SEVERITIES: readonly Severity[] = ['info', 'warning', 'critical'];

/** A string the server sends in English only, in the current language when
 *  the dashboard knows it; undefined when it does not. */
function known(key: string): string | undefined {
  const text = translate(lang.value, key);
  return text === key ? undefined : text;
}

export function typeName(type: ChannelType): string {
  return pick(type.name, type.name_zh);
}

function fieldLabel(type: ChannelType, field: TypeField): string {
  return known(`settings.channels.field.${type.id}.${field.key}`) ?? known(`settings.channels.field.${field.key}`) ?? (field.label || field.key);
}

function fieldHelp(type: ChannelType, field: TypeField): string | undefined {
  return known(`settings.channels.help.${type.id}.${field.key}`) ?? field.help;
}

export interface ChannelDialogProps {
  open: boolean;
  onClose: () => void;
  /** The channel to edit. Without it, the dialog adds one. */
  channel?: ChannelView | undefined;
}

export function ChannelDialog({ open, onClose, channel }: ChannelDialogProps) {
  const [picked, setPicked] = useState<string | null>(null);
  const [saved, setSaved] = useState<ChannelView | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (open) return channelTypes.retain();
    setPicked(null);
    setSaved(null);
    setBusy(false);
    return undefined;
  }, [open]);

  const types = channelTypes.data.value;
  const type = types?.find((each) => each.id === (channel?.type ?? picked));
  const title = saved ? t('settings.channels.added.title') : channel ? t('settings.channels.edit') : t('settings.channels.add.title');

  return (
    <Dialog open={open} onClose={onClose} title={title} description={types && !type && !saved ? t('settings.channels.add.pick') : undefined} size={type || saved ? 'md' : 'lg'} busy={busy}>
      {saved ? (
        <Added channel={saved} onDone={onClose} />
      ) : !types ? (
        channelTypes.error.value ? (
          <ErrorState compact error={channelTypes.error.value} onRetry={() => void channelTypes.load()} />
        ) : (
          <div class="st-types" aria-busy="true">
            {[0, 1, 2, 3, 4, 5].map((index) => (
              <Skeleton key={index} height="64px" />
            ))}
          </div>
        )
      ) : !type ? (
        <ul class="st-types">
          {types
            .filter((each) => !each.managed)
            .map((each) => (
              <li key={each.id}>
                <button type="button" class="st-type" onClick={() => setPicked(each.id)}>
                  <span class="st-type__name">{typeName(each)}</span>
                  <span class="st-type__hint">{known(`settings.channels.type.${each.id}`) ?? ''}</span>
                </button>
              </li>
            ))}
        </ul>
      ) : (
        <ChannelForm
          key={type.id}
          type={type}
          channel={channel}
          onBack={channel ? undefined : () => setPicked(null)}
          onBusy={setBusy}
          onCancel={onClose}
          onSaved={(result) => {
            if (channel) onClose();
            else setSaved(result);
          }}
        />
      )}
    </Dialog>
  );
}

// ---------- the form ----------

interface FormProps {
  type: ChannelType;
  channel: ChannelView | undefined;
  onBack: (() => void) | undefined;
  onBusy: (busy: boolean) => void;
  onCancel: () => void;
  onSaved: (channel: ChannelView) => void;
}

const TOPIC_WORDS: Record<Topic, { label: TKey; help: TKey }> = {
  meters: { label: 'settings.topic.meters', help: 'settings.topic.meters.help' },
  dates: { label: 'settings.topic.dates', help: 'settings.topic.dates.help' },
  system: { label: 'settings.topic.system', help: 'settings.topic.system.help' },
  digest: { label: 'settings.topic.digest', help: 'settings.topic.digest.help' },
};

export function topicLabel(topic: Topic): string {
  return TOPIC_WORDS[topic] ? t(TOPIC_WORDS[topic].label) : topic;
}

const SEVERITY_WORDS: Record<Severity, TKey> = {
  info: 'settings.severity.info',
  warning: 'settings.severity.warning',
  critical: 'settings.severity.critical',
};

export function severityLabel(severity: Severity): string {
  return SEVERITY_WORDS[severity] ? t(SEVERITY_WORDS[severity]) : severity;
}

function ChannelForm({ type, channel, onBack, onBusy, onCancel, onSaved }: FormProps) {
  // A managed type's settings are the browser's business, not a form's.
  const fields = type.managed ? [] : type.fields;

  // Plain settings start from what is stored; secret ones start empty,
  // because the server never sends them back.
  const initial = useMemo(() => {
    const values: Record<string, string> = {};
    for (const field of fields) values[field.key] = field.secret ? '' : (channel?.config[field.key] ?? '');
    return values;
  }, [type.id, channel?.id]);

  const [name, setName] = useState(() => channel?.name ?? freeName(typeName(type)));
  const [values, setValues] = useState(initial);
  const [topics, setTopics] = useState<readonly Topic[]>(channel?.topics ?? TOPICS);
  const [severity, setSeverity] = useState<Severity>(channel?.min_severity ?? 'info');
  const [problems, setProblems] = useState<Record<string, string>>({});

  const configChanged = fields.some((field) => (values[field.key] ?? '') !== (initial[field.key] ?? ''));
  const clear = (key: string) => {
    if (problems[key]) setProblems({ ...problems, [key]: '' });
  };

  const save = useAction(async () => {
    // What is required can be checked here; whether it works, only sending
    // a message can say — that is what the test is for.
    const found: Record<string, string> = {};
    if (name.trim() === '') found.name = t('settings.form.required', { field: t('settings.channels.form.name') });
    if (!channel) {
      for (const field of fields) {
        if (!field.optional && !field.default && (values[field.key] ?? '').trim() === '') found[field.key] = t('settings.form.required', { field: fieldLabel(type, field) });
      }
    }
    if (topics.length === 0) found.topics = t('settings.channels.form.topics.none');
    setProblems(found);
    if (Object.keys(found).length > 0) return;

    const input: ChannelInput = { name: name.trim(), topics: TOPICS.filter((topic) => topics.includes(topic)), min_severity: severity };
    if (!channel) input.type = type.id;
    if (!channel || configChanged) {
      input.config = {};
      for (const field of fields) input.config[field.key] = (values[field.key] ?? '').trim();
    }

    onBusy(true);
    try {
      const result = channel ? await updateChannel(channel.id, input) : await createChannel(input);
      channels.set((list) => (list ? (channel ? list.map((each) => (each.id === result.id ? result : each)) : [...list, result]) : list));
      void channels.load();
      if (channel) toast(t('settings.channels.saved'));
      onSaved(result);
    } catch (cause) {
      // The one refusal that belongs to a field rather than to the form.
      if (cause instanceof ApiError && cause.code === 'invalid' && /^a channel named /.test(cause.message)) {
        setProblems({ name: t('settings.channels.form.name.taken') });
        return;
      }
      throw cause;
    } finally {
      onBusy(false);
    }
  });

  const submit = (event: JSX.TargetedEvent<HTMLFormElement>) => {
    event.preventDefault();
    void save.run();
  };

  return (
    <form class="st-form" onSubmit={submit} noValidate>
      <div class="st-form__kind">
        <strong>{typeName(type)}</strong>
        {onBack && (
          <Button variant="ghost" size="sm" icon={ArrowLeft} onClick={onBack}>
            {t('settings.channels.add.change')}
          </Button>
        )}
      </div>
      {type.managed && <p class="st-note">{t('settings.channels.form.managed')}</p>}

      {fields.map((field, index) => {
        const stored = channel && field.secret ? channel.config[field.key] : undefined;
        const help = [fieldHelp(type, field), stored ? t('settings.channels.form.secret.keep') : undefined].filter(Boolean).join(' ');
        return (
          <Field key={field.key} label={fieldLabel(type, field)} optional={Boolean(field.optional)} help={help || undefined} error={problems[field.key] || undefined}>
            <Input
              mono
              type={field.secret ? 'password' : 'text'}
              value={values[field.key] ?? ''}
              onInput={(value) => {
                setValues({ ...values, [field.key]: value });
                clear(field.key);
              }}
              placeholder={stored ? t('settings.channels.form.secret.stored', { mask: stored }) : (field.placeholder ?? field.default ?? '')}
              autoComplete="off"
              spellcheck={false}
              autoCapitalize="off"
              autoFocus={index === 0 && !channel}
              data-1p-ignore
            />
          </Field>
        );
      })}

      <Field label={t('settings.channels.form.name')} help={t('settings.channels.form.name.help')} error={problems.name || undefined}>
        <Input
          value={name}
          onInput={(value) => {
            setName(value);
            clear('name');
          }}
          maxLength={80}
          autoComplete="off"
        />
      </Field>

      <Group label={t('settings.channels.form.topics')} error={problems.topics || undefined}>
        <div class="st-checks">
          {TOPICS.map((topic) => (
            <CheckBox
              key={topic}
              checked={topics.includes(topic)}
              label={t(TOPIC_WORDS[topic].label)}
              help={t(TOPIC_WORDS[topic].help)}
              onChange={(checked) => {
                setTopics(checked ? [...topics, topic] : topics.filter((each) => each !== topic));
                clear('topics');
              }}
            />
          ))}
        </div>
      </Group>

      <Field
        label={t('settings.channels.form.severity')}
        help={topics.includes('digest') && severity !== 'info' ? t('settings.channels.form.severity.digest') : t('settings.channels.form.severity.help')}
      >
        <Segmented value={severity} onChange={setSeverity} options={SEVERITIES.map((each) => ({ value: each, label: t(SEVERITY_WORDS[each]) }))} />
      </Field>

      {save.error !== undefined && (
        <p class="form-error" role="alert">
          {errorText(save.error)}
        </p>
      )}

      <div class="st-form__actions">
        <Button onClick={onCancel} disabled={save.busy}>
          {t('common.action.cancel')}
        </Button>
        <Button type="submit" variant="primary" busy={save.busy}>
          {channel ? t('common.action.save') : t('settings.channels.form.add')}
        </Button>
      </div>
    </form>
  );
}

/** The type's name, numbered if a channel is already called that. */
function freeName(base: string): string {
  const taken = new Set((channels.data.value ?? []).map((each) => each.name.toLowerCase()));
  if (!taken.has(base.toLowerCase())) return base;
  for (let n = 2; ; n++) {
    if (!taken.has(`${base} ${n}`.toLowerCase())) return `${base} ${n}`;
  }
}

// ---------- after adding: try it ----------

export interface TestOutcome {
  ok: boolean;
  error?: string;
}

/** Sends a test message through a channel and keeps what came of it. */
export function useChannelTest(channelId: string | undefined) {
  const [outcome, setOutcome] = useState<TestOutcome | undefined>(undefined);
  const action = useAction(async () => {
    if (!channelId) return;
    setOutcome(undefined);
    try {
      setOutcome(await testChannel(channelId));
    } catch (cause) {
      setOutcome({ ok: false, error: errorText(cause) });
    }
  });
  return { run: () => void action.run(), busy: action.busy, outcome, clear: () => setOutcome(undefined) };
}

/** What a test came to, in a line. */
export function TestResult({ outcome }: { outcome: TestOutcome | undefined }) {
  if (!outcome) return null;
  return outcome.ok ? (
    <p class="st-test st-test--ok" role="status">
      {t('settings.channels.test.ok')}
    </p>
  ) : (
    <p class="st-test st-test--failed" role="status">
      <span>{t('settings.channels.test.failed')}</span> <span class="st-test__error">{outcome.error || t('common.error.unknown')}</span>
    </p>
  );
}

function Added({ channel, onDone }: { channel: ChannelView; onDone: () => void }) {
  const test = useChannelTest(channel.id);
  return (
    <div class="st-added">
      <p class="st-added__title">
        <CircleCheck size={18} />
        {t('settings.channels.added', { name: channel.name })}
      </p>
      <p class="st-note">{t('settings.channels.added.body')}</p>
      <TestResult outcome={test.outcome} />
      <div class="st-form__actions">
        <Button icon={Send} busy={test.busy} variant={test.outcome?.ok ? 'secondary' : 'primary'} onClick={test.run}>
          {test.outcome ? t('settings.channels.test.again') : t('settings.channels.test')}
        </Button>
        <Button variant={test.outcome?.ok ? 'primary' : 'secondary'} onClick={onDone}>
          {t('common.action.done')}
        </Button>
      </div>
    </div>
  );
}

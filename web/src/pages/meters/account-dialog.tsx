// Adding an account, and editing one: the same form, reached two ways.
//
//   <AccountDialog open onClose={…} />                    pick a platform, then fill in the form
//   <AccountDialog open onClose={…} account={account} />  the form, for an existing account
//
// The form's fields come from the platform's definition (GET /providers),
// so a platform added tomorrow needs no change here.
import { ArrowLeft, ExternalLink, FlaskConical, Search } from 'lucide-preact';
import type { JSX } from 'preact';
import { useEffect, useMemo, useState } from 'preact/hooks';
import { createAccount, testAccount, updateAccount } from '../../api/endpoints';
import type { AccountInput, AccountView, Checked, Definition, Sample, TestResult } from '../../api/types';
import { Amount, Button, Chip, cx, Dialog, EmptyState, ErrorState, Field, Input, Select, Skeleton, toast, Tooltip, type Tone } from '../../components';
import { errorText } from '../../errors';
import { amountParts, formatSpan } from '../../format';
import { useAction } from '../../hooks';
import { pick, t, type TKey } from '../../i18n';
import { Link } from '../../router';
import { accounts } from '../../state/accounts';
import { providers } from '../../state/providers';
import { settings } from '../../state/settings';

const CHECKED: Record<Checked, { label: TKey; hint: TKey; tone: Tone }> = {
  live: { label: 'meters.checked.live', hint: 'meters.checked.live.hint', tone: 'ok' },
  recorded: { label: 'meters.checked.recorded', hint: 'meters.checked.recorded.hint', tone: 'info' },
  docs: { label: 'meters.checked.docs', hint: 'meters.checked.docs.hint', tone: 'neutral' },
};

/** Intervals offered for "read every", in seconds; 0 is the default. */
const INTERVALS = [0, 300, 900, 1800, 3600, 6 * 3600, 24 * 3600];

export interface AccountDialogProps {
  open: boolean;
  onClose: () => void;
  /** The account to edit. Without it, the dialog adds one. */
  account?: AccountView | undefined;
  /** Opens the add flow straight on a platform's form. */
  provider?: string | undefined;
}

export function AccountDialog({ open, onClose, account, provider }: AccountDialogProps) {
  // undefined: nothing chosen yet, so the account's or the requested platform
  // applies. null: the user went back to the list.
  const [picked, setPicked] = useState<string | null | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!open) {
      setPicked(undefined);
      setBusy(false);
      return;
    }
    const release = [providers.retain(), accounts.retain(), settings.retain()];
    return () => release.forEach((stop) => stop());
  }, [open]);

  const all = providers.data.value;
  const wanted = picked === undefined ? (account?.provider ?? provider) : picked;
  const definition = all?.find((each) => each.id === wanted);
  const title = account ? t('meters.account.edit') : t('meters.add.title');

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={title}
      description={all && !definition ? t('meters.add.pick') : undefined}
      size={definition ? 'md' : 'lg'}
      busy={busy}
    >
      {!all ? (
        providers.error.value ? (
          <ErrorState compact error={providers.error.value} onRetry={() => void providers.load()} />
        ) : (
          <div class="mt-providers" aria-busy="true">
            {[0, 1, 2, 3, 4, 5].map((index) => (
              <Skeleton key={index} height="64px" />
            ))}
          </div>
        )
      ) : !definition ? (
        <ProviderPicker definitions={all} onPick={setPicked} />
      ) : (
        <AccountForm
          key={definition.id}
          definition={definition}
          account={account}
          onBack={account ? undefined : () => setPicked(null)}
          onBusy={setBusy}
          onDone={onClose}
        />
      )}
    </Dialog>
  );
}

// ---------- step 1: which platform ----------

function ProviderPicker({ definitions, onPick }: { definitions: Definition[]; onPick: (id: string) => void }) {
  const [query, setQuery] = useState('');
  const needle = query.trim().toLowerCase();
  const shown = useMemo(() => {
    const sorted = [...definitions].sort((a, b) => pick(a.name, a.name_zh).localeCompare(pick(b.name, b.name_zh)));
    if (!needle) return sorted;
    return sorted.filter((each) => [each.id, each.name, each.name_zh ?? ''].some((text) => text.toLowerCase().includes(needle)));
  }, [definitions, needle]);

  return (
    <div class="mt-picker">
      <Input
        type="search"
        value={query}
        onInput={setQuery}
        placeholder={t('meters.add.search')}
        aria-label={t('meters.add.search')}
        prefix={<Search size={14} />}
        autoFocus
      />
      {shown.length === 0 ? (
        <EmptyState compact title={t('meters.add.noMatch', { query: query.trim() })} />
      ) : (
        <ul class="mt-providers">
          {shown.map((definition) => {
            const checked = definition.builtin && definition.checked ? CHECKED[definition.checked] : undefined;
            const tile = (
              <button type="button" class="mt-provider" onClick={() => onPick(definition.id)}>
                <span class="mt-provider__name">{pick(definition.name, definition.name_zh)}</span>
                {checked ? <Chip tone={checked.tone}>{t(checked.label)}</Chip> : !definition.builtin ? <Chip>{t('meters.add.custom')}</Chip> : null}
              </button>
            );
            return <li key={definition.id}>{checked ? <Tooltip content={t(checked.hint)}>{tile}</Tooltip> : tile}</li>;
          })}
        </ul>
      )}
      <p class="mt-picker__more">
        {t('meters.add.missing')} <Link href="/settings/providers">{t('meters.add.missing.link')}</Link>
      </p>
    </div>
  );
}

// ---------- step 2: the form ----------

interface FormProps {
  definition: Definition;
  account: AccountView | undefined;
  onBack: (() => void) | undefined;
  onBusy: (busy: boolean) => void;
  onDone: () => void;
}

function AccountForm({ definition, account, onBack, onBusy, onDone }: FormProps) {
  const providerName = pick(definition.name, definition.name_zh);
  const fields = definition.fields ?? [];

  // Plain fields start from what is stored; secret ones start empty, because
  // the server never sends them back.
  const initial = useMemo(() => {
    const values: Record<string, string> = {};
    for (const field of fields) values[field.key] = field.secret ? '' : (account?.credential[field.key] ?? '');
    return values;
  }, [definition.id, account?.id]);

  const [name, setName] = useState(account?.name ?? providerName);
  const [group, setGroup] = useState(account?.group ?? '');
  const [interval, setIntervalSeconds] = useState(String(account?.interval_seconds ?? 0));
  const [values, setValues] = useState(initial);
  const [missing, setMissing] = useState<ReadonlySet<string>>(new Set());
  const [result, setResult] = useState<TestResult | undefined>(undefined);

  const groups = useMemo(() => [...new Set((accounts.data.value ?? []).map((each) => each.group).filter(Boolean))].sort(), [accounts.data.value]);
  const credentialChanged = fields.some((field) => (values[field.key] ?? '') !== (initial[field.key] ?? ''));

  const credential = () => {
    const out: Record<string, string> = {};
    for (const field of fields) out[field.key] = (values[field.key] ?? '').trim();
    return out;
  };

  const test = useAction(async () => {
    setResult(undefined);
    setResult(await testAccount({ provider: definition.id, credential: credential(), ...(account ? { account_id: account.id } : {}) }));
  });

  const save = useAction(async () => {
    // What is required can be checked here; whether it is right, only the
    // platform can say — that is what Test is for.
    const empty = new Set<string>();
    if (name.trim() === '') empty.add('name');
    if (!account) {
      for (const field of fields) {
        if (!field.optional && !field.default && (values[field.key] ?? '').trim() === '') empty.add(field.key);
      }
    }
    setMissing(empty);
    if (empty.size > 0) return;

    onBusy(true);
    try {
      if (account) {
        const input: AccountInput = { name: name.trim(), group: group.trim(), interval_seconds: Number(interval) };
        if (credentialChanged || account.credential_unreadable) input.credential = credential();
        await updateAccount(account.id, input);
        toast(t('meters.form.saved'));
      } else {
        const created = await createAccount({ name: name.trim(), provider: definition.id, group: group.trim(), credential: credential() });
        toast(t('meters.form.added', { name: created.name }));
      }
      onDone();
    } finally {
      onBusy(false);
    }
  });

  const defaultSpan = formatSpan((settings.data.value?.sample_seconds ?? 1800) * 1000);
  const intervals = INTERVALS.map((seconds) => ({
    value: String(seconds),
    label: seconds === 0 ? t('meters.form.interval.default', { span: defaultSpan }) : formatSpan(seconds * 1000),
  }));
  if (account && !INTERVALS.includes(account.interval_seconds)) {
    intervals.push({ value: String(account.interval_seconds), label: formatSpan(account.interval_seconds * 1000) });
  }

  const submit = (event: JSX.TargetedEvent<HTMLFormElement>) => {
    event.preventDefault();
    void save.run();
  };
  const notes = pick(definition.notes ?? '', definition.notes_zh);

  return (
    <form class="mt-form" onSubmit={submit} noValidate>
      <div class="mt-form__platform">
        <div class="mt-form__platform-name">
          <strong>{providerName}</strong>
          {definition.homepage && (
            <a class="mt-form__site" href={definition.homepage} target="_blank" rel="noopener noreferrer">
              {t('meters.add.homepage')}
              <ExternalLink size={12} />
            </a>
          )}
        </div>
        {onBack && (
          <Button variant="ghost" size="sm" icon={ArrowLeft} onClick={onBack}>
            {t('meters.add.change')}
          </Button>
        )}
      </div>
      {notes && <p class="mt-form__notes">{notes}</p>}

      {fields.map((field, index) => {
        const stored = account && field.secret ? account.credential[field.key] : undefined;
        const help = [field.help, stored ? t('meters.form.secret.keep') : undefined].filter(Boolean).join(' ');
        return (
          <Field
            key={field.key}
            label={field.label || field.key}
            optional={Boolean(field.optional)}
            help={help || undefined}
            error={missing.has(field.key) ? t('meters.form.required', { field: field.label || field.key }) : undefined}
          >
            <Input
              mono
              type={field.secret ? 'password' : 'text'}
              value={values[field.key] ?? ''}
              onInput={(value) => {
                setValues({ ...values, [field.key]: value });
                setResult(undefined);
                if (missing.has(field.key)) setMissing(new Set([...missing].filter((key) => key !== field.key)));
              }}
              placeholder={stored ? t('meters.form.secret.stored', { mask: stored }) : (field.placeholder ?? field.default ?? '')}
              autoComplete="off"
              spellcheck={false}
              autoCapitalize="off"
              autoFocus={index === 0 && !account}
              data-1p-ignore
            />
          </Field>
        );
      })}

      <div class="mt-form__test">
        <Button icon={FlaskConical} busy={test.busy} onClick={() => void test.run()}>
          {t('meters.form.test')}
        </Button>
        {test.error !== undefined && <p class="form-error">{errorText(test.error)}</p>}
        {result && <TestOutcome result={result} />}
      </div>

      <div class="mt-form__row">
        <Field label={t('meters.form.name')} help={t('meters.form.name.help')} error={missing.has('name') ? t('meters.form.required', { field: t('meters.form.name') }) : undefined}>
          <Input
            value={name}
            onInput={(value) => {
              setName(value);
              if (missing.has('name')) setMissing(new Set([...missing].filter((key) => key !== 'name')));
            }}
            maxLength={80}
            autoComplete="off"
          />
        </Field>
        <Field label={t('meters.form.group')} optional help={t('meters.form.group.help')}>
          <Input value={group} onInput={setGroup} list="mt-groups" autoComplete="off" />
        </Field>
        <datalist id="mt-groups">
          {groups.map((each) => (
            <option key={each} value={each} />
          ))}
        </datalist>
      </div>

      {account && (
        <Field label={t('meters.form.interval')}>
          <Select value={interval} onChange={setIntervalSeconds} options={intervals} />
        </Field>
      )}

      {save.error !== undefined && (
        <p class="form-error" role="alert">
          {errorText(save.error)}
        </p>
      )}

      <div class="mt-form__actions">
        <Button onClick={onDone} disabled={save.busy}>
          {t('common.action.cancel')}
        </Button>
        <Button type="submit" variant="primary" busy={save.busy}>
          {account ? t('common.action.save') : t('meters.form.add')}
        </Button>
      </div>
    </form>
  );
}

/** What a test fetch found: the meters, or the platform's own error. */
function TestOutcome({ result }: { result: TestResult }) {
  // A demo answers in no time at all; "0 ms" would only raise questions.
  const took = result.latency_ms > 0 ? t('meters.form.test.took', { ms: result.latency_ms }) : '';
  if (!result.ok) {
    return (
      <div class="mt-test mt-test--failed" role="status">
        <p class="mt-test__title">{t('meters.form.test.failed')}</p>
        <p class="mt-test__error">{result.error || `HTTP ${result.status}`}</p>
      </div>
    );
  }
  if (result.samples.length === 0) {
    return (
      <div class="mt-test mt-test--failed" role="status">
        <p class="mt-test__title">{t('meters.form.test.none')}</p>
      </div>
    );
  }
  return (
    <div class={cx('mt-test', 'mt-test--ok')} role="status">
      <p class="mt-test__title">
        {t('meters.form.test.ok', { n: result.samples.length })} {took && <span class="mt-test__took">{took}</span>}
      </p>
      <ul class="mt-test__meters">
        {result.samples.map((sample) => (
          <li key={sample.key}>
            <span class="mt-test__name">{sampleName(sample)}</span>
            <Chip>{t(`meters.kind.${sample.kind}`)}</Chip>
            <span class="mt-test__value">
              <Amount value={sample.value} unit={sample.unit} />
              {sample.limit !== undefined && sample.unit !== '%' && (
                <span class="mt-test__limit"> {t('meters.card.ofLimit', { amount: amountParts(sample.limit, sample.unit).number })}</span>
              )}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function sampleName(sample: Sample): string {
  return pick(sample.name ?? sample.key, sample.name_zh) || sample.key;
}

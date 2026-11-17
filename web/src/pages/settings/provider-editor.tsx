// One provider definition, being written: the form on one side, a test bench
// on the other. The bench runs the definition as it stands — unsaved — so the
// jq program can be written against what the platform really answers.
//
// What the server refuses comes back as "request.url: …; extract: …", and each
// part is shown at the field it names.
import { ArrowDown, ArrowLeft, ArrowUp, Compass, Play, Plus, Trash2, X } from 'lucide-preact';
import type { ComponentChildren, JSX } from 'preact';
import { useState } from 'preact/hooks';
import { ApiError } from '../../api/client';
import { deleteProvider, saveProvider, testAccount } from '../../api/endpoints';
import type { AuthType, Definition, TestResult } from '../../api/types';
import { Button, Card, ConfirmDialog, EmptyState, ErrorState, Field, IconButton, Input, LinkButton, Segmented, Select, Skeleton, toast } from '../../components';
import { errorText } from '../../errors';
import { formatSpan, formatStamp } from '../../format';
import { useAction } from '../../hooks';
import { pick, t, translate, type TKey } from '../../i18n';
import { Link, navigate } from '../../router';
import { providers } from '../../state/providers';
import { authNeeds, blankDraft, copyId, definitionOf, draftOf, ID_PATTERN, moved, problemFor, splitProblems, type DefinitionDraft, type FieldDraft, type Problem } from './logic';
import { CheckBox, CopyButton, Labelled, PairRows, TextArea } from './parts';

const CATALOG = '/settings/providers';

export function ProviderEditor({ id, from }: { id?: string; from?: string }) {
  const all = providers.data.value;
  if (!all) {
    if (providers.error.value) return <ErrorState error={providers.error.value} onRetry={() => void providers.load()} />;
    return (
      <div aria-busy="true">
        <span class="sr-only">{t('common.state.loading')}</span>
        <Skeleton height="420px" />
      </div>
    );
  }

  const source = all.find((each) => each.id === (id ?? from));
  const back = <LinkButton href={CATALOG}>{t('settings.providers.all')}</LinkButton>;
  if ((id !== undefined || from) && !source) {
    return (
      <EmptyState icon={Compass} title={t('settings.providers.editor.missing.title')} action={back}>
        {t('settings.providers.editor.missing.body')}
      </EmptyState>
    );
  }
  if (id !== undefined && source?.builtin) {
    return (
      <EmptyState
        icon={Compass}
        title={t('settings.providers.editor.builtin.title', { name: pick(source.name, source.name_zh) })}
        action={
          <>
            {back}
            <LinkButton variant="primary" href={`${CATALOG}/new?from=${encodeURIComponent(source.id)}`}>
              {t('settings.providers.duplicate')}
            </LinkButton>
          </>
        }
      >
        {t('settings.providers.editor.builtin.body')}
      </EmptyState>
    );
  }
  return <Editor source={source} editing={id !== undefined} all={all} />;
}

/** The server's names for the parts of a definition the form shows an error at. */
const PLACED = ['id', 'name', 'fields', 'request.method', 'request.url', 'request.auth', 'request.auth.type', 'request.auth.name', 'request.auth.value', 'extract'];

const AUTH_TYPES: readonly AuthType[] = ['bearer', 'header', 'query', 'basic', 'none', 'aliyun-rpc', 'volc-v4'];
const AUTH_WORDS: Record<AuthType, { label: TKey; help: TKey }> = {
  bearer: { label: 'settings.providers.auth.bearer', help: 'settings.providers.auth.bearer.help' },
  header: { label: 'settings.providers.auth.header', help: 'settings.providers.auth.header.help' },
  query: { label: 'settings.providers.auth.query', help: 'settings.providers.auth.query.help' },
  basic: { label: 'settings.providers.auth.basic', help: 'settings.providers.auth.basic.help' },
  none: { label: 'settings.providers.auth.none', help: 'settings.providers.auth.none.help' },
  'aliyun-rpc': { label: 'settings.providers.auth.aliyun', help: 'settings.providers.auth.aliyun.help' },
  'volc-v4': { label: 'settings.providers.auth.volc', help: 'settings.providers.auth.volc.help' },
};

function Editor({ source, editing, all }: { source: Definition | undefined; editing: boolean; all: Definition[] }) {
  const [draft, setDraft] = useState<DefinitionDraft>(() => {
    if (!source) return blankDraft();
    const start = draftOf(source);
    if (editing) return start;
    // A copy needs an id of its own, and a name that tells it from the original.
    return {
      ...start,
      id: copyId(source.id, new Set(all.map((each) => each.id))),
      name: translate('en', 'settings.providers.copyName', { name: start.name }),
      nameZh: start.nameZh ? translate('zh', 'settings.providers.copyName', { name: start.nameZh }) : '',
    };
  });
  const [problems, setProblems] = useState<Problem[]>([]);
  const [local, setLocal] = useState<Record<string, string>>({});
  const [credential, setCredential] = useState<Record<string, string>>({});
  const [result, setResult] = useState<TestResult | undefined>(undefined);
  const [confirming, setConfirming] = useState(false);

  /** Changes the draft and forgets what was said about the fields named. */
  const set = (change: Partial<DefinitionDraft>, ...fields: string[]) => {
    setDraft({ ...draft, ...change });
    if (fields.length === 0) return;
    if (problems.some((problem) => problem.field !== null && fields.includes(problem.field))) setProblems(problems.filter((problem) => problem.field === null || !fields.includes(problem.field)));
    if (fields.some((field) => local[field])) setLocal(Object.fromEntries(Object.entries(local).filter(([key]) => !fields.includes(key))));
  };
  const error = (...fields: string[]) => fields.map((field) => local[field]).find(Boolean) ?? problemFor(problems, ...fields);
  const unplaced = problems.filter((problem) => problem.field === null || !PLACED.includes(problem.field)).map((problem) => (problem.field ? `${problem.field}: ${problem.text}` : problem.text));

  const refused = (cause: unknown): boolean => {
    if (!(cause instanceof ApiError) || cause.code !== 'invalid') return false;
    setProblems(splitProblems(cause.message));
    return true;
  };

  const save = useAction(async () => {
    const definition = definitionOf(draft);
    const found: Record<string, string> = {};
    if (!ID_PATTERN.test(definition.id)) found.id = t('settings.providers.editor.id.invalid');
    else if (!editing && all.some((each) => each.id === definition.id)) found.id = t(all.find((each) => each.id === definition.id)?.builtin ? 'settings.providers.editor.id.builtin' : 'settings.providers.editor.id.taken');
    setLocal(found);
    setProblems([]);
    if (Object.keys(found).length > 0) return;
    try {
      const saved = await saveProvider(definition);
      providers.set((list) => (list ? [...list.filter((each) => each.id !== saved.id), saved] : list));
      void providers.load();
      toast(t('settings.providers.editor.saved', { name: saved.name }));
      navigate(CATALOG);
    } catch (cause) {
      if (!refused(cause)) throw cause;
    }
  });

  const run = useAction(async () => {
    setResult(undefined);
    setProblems([]);
    // A definition can be tried before it has been given an id or a name.
    const definition = definitionOf(draft);
    if (definition.id === '') definition.id = 'untitled';
    if (definition.name === '') definition.name = 'untitled';
    const values: Record<string, string> = {};
    for (const field of definition.fields) values[field.key] = (credential[field.key] ?? '').trim();
    try {
      setResult(await testAccount({ definition, credential: values }));
    } catch (cause) {
      if (!refused(cause)) throw cause;
    }
  });

  const remove = useAction(async () => {
    await deleteProvider(draft.id);
    providers.set((list) => list?.filter((each) => each.id !== draft.id));
    toast(t('settings.providers.deleted', { name: draft.name }));
    navigate(CATALOG, { replace: true });
  });

  const submit = (event: JSX.TargetedEvent<HTMLFormElement>) => {
    event.preventDefault();
    void save.run();
  };

  const setField = (index: number, change: Partial<FieldDraft>) => set({ fields: draft.fields.map((field, at) => (at === index ? { ...field, ...change } : field)) }, 'fields', 'request.auth.value');
  const needs = authNeeds(draft.auth);
  const title = editing ? t('settings.providers.editor.edit', { name: source ? pick(source.name, source.name_zh) : draft.id }) : source ? t('settings.providers.editor.copy', { name: pick(source.name, source.name_zh) }) : t('settings.providers.editor.new');

  return (
    <div class="st-editor">
      <header class="st-editor__head">
        <Link class="page-head__back" href={CATALOG}>
          <ArrowLeft size={14} />
          {t('settings.providers.all')}
        </Link>
        <h2 class="st-editor__title">{title}</h2>
      </header>

      <form class="st-editor__form" onSubmit={submit} noValidate>
        <Card class="st-part">
          <h3 class="st-card-title">{t('settings.providers.editor.what')}</h3>
          <div class="st-form__row">
            <Field label={t('settings.providers.editor.id')} help={editing ? t('settings.providers.editor.id.fixed') : t('settings.providers.editor.id.help')} error={error('id')}>
              <Input mono value={draft.id} onInput={(value) => set({ id: value.toLowerCase() }, 'id')} disabled={editing} maxLength={40} placeholder="my-relay" autoComplete="off" spellcheck={false} autoCapitalize="off" />
            </Field>
            <div class="st-stack">
              <Field label={t('settings.providers.editor.name')} error={error('name')}>
                <Input value={draft.name} onInput={(name) => set({ name }, 'name')} maxLength={80} placeholder="My relay" autoComplete="off" />
              </Field>
              <Field label={t('settings.providers.editor.nameZh')} optional>
                <Input value={draft.nameZh} onInput={(nameZh) => set({ nameZh })} maxLength={80} autoComplete="off" />
              </Field>
            </div>
          </div>
        </Card>

        <Card class="st-part">
          <h3 class="st-card-title">{t('settings.providers.editor.fields')}</h3>
          <p class="st-note">{t('settings.providers.editor.fields.about')}</p>
          {draft.fields.length === 0 && <p class="st-note">{t('settings.providers.editor.fields.none')}</p>}
          {draft.fields.map((field, index) => (
            <fieldset key={index} class="st-fielddef">
              <legend class="sr-only">{t('settings.providers.editor.field', { n: index + 1 })}</legend>
              <div class="st-fielddef__grid">
                <Field label={t('settings.providers.editor.field.key')}>
                  <Input mono value={field.key} onInput={(key) => setField(index, { key })} placeholder="api_key" maxLength={40} autoComplete="off" spellcheck={false} autoCapitalize="off" />
                </Field>
                <Field label={t('settings.providers.editor.field.label')}>
                  <Input value={field.label} onInput={(label) => setField(index, { label })} placeholder="API Key" autoComplete="off" />
                </Field>
                <Field label={t('settings.providers.editor.field.placeholder')} optional>
                  <Input value={field.placeholder} onInput={(placeholder) => setField(index, { placeholder })} autoComplete="off" />
                </Field>
                <Field label={t('settings.providers.editor.field.default')} optional>
                  <Input mono value={field.default} onInput={(value) => setField(index, { default: value })} autoComplete="off" spellcheck={false} autoCapitalize="off" />
                </Field>
              </div>
              <div class="st-fielddef__foot">
                <div class="st-fielddef__flags">
                  <CheckBox checked={field.secret} onChange={(secret) => setField(index, { secret })} label={t('settings.providers.editor.field.secret')} />
                  <CheckBox checked={field.optional} onChange={(optional) => setField(index, { optional })} label={t('settings.providers.editor.field.optional')} />
                </div>
                <div class="st-fielddef__tools">
                  <IconButton icon={ArrowUp} label={t('settings.providers.editor.field.up', { n: index + 1 })} disabled={index === 0} onClick={() => set({ fields: moved(draft.fields, index, -1) })} />
                  <IconButton icon={ArrowDown} label={t('settings.providers.editor.field.down', { n: index + 1 })} disabled={index === draft.fields.length - 1} onClick={() => set({ fields: moved(draft.fields, index, 1) })} />
                  <IconButton icon={X} label={t('settings.providers.editor.field.remove', { n: index + 1 })} onClick={() => set({ fields: draft.fields.filter((_, at) => at !== index) }, 'fields', 'request.auth.value')} />
                </div>
              </div>
            </fieldset>
          ))}
          {error('fields') && (
            <p class="form-error" role="alert">
              {error('fields')}
            </p>
          )}
          <Button variant="ghost" size="sm" icon={Plus} class="st-add" onClick={() => set({ fields: [...draft.fields, { key: '', label: '', secret: false, optional: false, default: '', placeholder: '', help: '' }] }, 'fields')}>
            {t('settings.providers.editor.field.add')}
          </Button>
        </Card>

        <Card class="st-part">
          <h3 class="st-card-title">{t('settings.providers.editor.request')}</h3>
          <div class="st-request">
            <Field label={t('settings.providers.editor.method')} error={error('request.method')}>
              <Segmented
                value={draft.method}
                onChange={(method) => set({ method }, 'request.method')}
                options={[
                  { value: 'GET', label: 'GET' },
                  { value: 'POST', label: 'POST' },
                ]}
              />
            </Field>
            <Field label={t('settings.providers.editor.url')} help={t('settings.providers.editor.url.help')} error={error('request.url')}>
              <Input mono type="url" inputMode="url" value={draft.url} onInput={(url) => set({ url }, 'request.url')} placeholder="https://api.example.com/v1/balance" autoComplete="off" spellcheck={false} autoCapitalize="off" />
            </Field>
          </div>

          <Field label={t('settings.providers.editor.auth')} help={t(AUTH_WORDS[draft.auth]?.help ?? 'settings.providers.auth.none.help')} error={error('request.auth', 'request.auth.type')}>
            <Select class="st-auth" value={draft.auth} onChange={(auth) => set({ auth }, 'request.auth', 'request.auth.type', 'request.auth.name', 'request.auth.value', 'fields')} options={AUTH_TYPES.map((type) => ({ value: type, label: t(AUTH_WORDS[type].label) }))} />
          </Field>
          {(needs.name || needs.value || needs.scope) && (
            <div class="st-form__row">
              {needs.name && (
                <Field label={t(draft.auth === 'query' ? 'settings.providers.auth.name.query' : 'settings.providers.auth.name.header')} error={error('request.auth.name')}>
                  <Input mono value={draft.authName} onInput={(authName) => set({ authName }, 'request.auth.name')} placeholder={draft.auth === 'query' ? 'key' : 'X-API-Key'} autoComplete="off" spellcheck={false} autoCapitalize="off" />
                </Field>
              )}
              {needs.value && (
                <Field label={t('settings.providers.auth.value')} optional help={t('settings.providers.auth.value.help')} error={error('request.auth.value')}>
                  <Input mono value={draft.authValue} onInput={(authValue) => set({ authValue }, 'request.auth.value')} placeholder="{{api_key}}" autoComplete="off" spellcheck={false} autoCapitalize="off" />
                </Field>
              )}
              {needs.scope && (
                <>
                  <Field label={t('settings.providers.auth.region')}>
                    <Input mono value={draft.region} onInput={(region) => set({ region }, 'request.auth')} placeholder="cn-shanghai" autoComplete="off" spellcheck={false} autoCapitalize="off" />
                  </Field>
                  <Field label={t('settings.providers.auth.service')}>
                    <Input mono value={draft.service} onInput={(service) => set({ service }, 'request.auth')} placeholder="billing" autoComplete="off" spellcheck={false} autoCapitalize="off" />
                  </Field>
                </>
              )}
            </div>
          )}

          <div class="st-labelled">
            <p class="field__label">{t('settings.providers.editor.headers')}</p>
            <PairRows label={t('settings.providers.editor.headers')} rows={draft.headers} onChange={(headers) => set({ headers })} keyPlaceholder={t('settings.providers.editor.pair.name')} valuePlaceholder={t('settings.providers.editor.pair.value')} addLabel={t('settings.providers.editor.headers.add')} />
          </div>
          <div class="st-labelled">
            <p class="field__label">{t('settings.providers.editor.query')}</p>
            <PairRows label={t('settings.providers.editor.query')} rows={draft.query} onChange={(query) => set({ query })} keyPlaceholder={t('settings.providers.editor.pair.name')} valuePlaceholder={t('settings.providers.editor.pair.value')} addLabel={t('settings.providers.editor.query.add')} />
          </div>
          {draft.method === 'POST' && (
            <Labelled label={t('settings.providers.editor.body')} optional help={t('settings.providers.editor.body.help')}>
              {(ids) => <TextArea {...ids} rows={4} value={draft.body} onInput={(body) => set({ body })} placeholder={'{"since": "{{month_start_date}}"}'} />}
            </Labelled>
          )}

          <Reference title={t('settings.providers.editor.templates')} about={t('settings.providers.editor.templates.about')}>
            <Term name="{{now}}">{t('settings.providers.editor.templates.now')}</Term>
            <Term name="{{today}}">{t('settings.providers.editor.templates.today')}</Term>
            <Term name="{{month_start}}">{t('settings.providers.editor.templates.monthStart')}</Term>
            <Term name="{{month_start_date}}">{t('settings.providers.editor.templates.monthStartDate')}</Term>
          </Reference>
        </Card>

        <Card class="st-part">
          <h3 class="st-card-title">{t('settings.providers.editor.extract')}</h3>
          <Labelled label={t('settings.providers.editor.extract.label')} error={error('extract')}>
            {(ids) => <TextArea {...ids} class="st-textarea--tall" rows={12} value={draft.extract} onInput={(extract) => set({ extract }, 'extract')} />}
          </Labelled>
          <Reference title={t('settings.providers.editor.out')} about={t('settings.providers.editor.out.about')}>
            <Term name="value">{t('settings.providers.editor.out.value')}</Term>
            <Term name="key">{t('settings.providers.editor.out.key')}</Term>
            <Term name="kind">{t('settings.providers.editor.out.kind')}</Term>
            <Term name="unit">{t('settings.providers.editor.out.unit')}</Term>
            <Term name="name, name_zh">{t('settings.providers.editor.out.name')}</Term>
            <Term name="limit">{t('settings.providers.editor.out.limit')}</Term>
            <Term name="resets_at">{t('settings.providers.editor.out.resets')}</Term>
            <Term name="window">{t('settings.providers.editor.out.window')}</Term>
          </Reference>
          <Reference title={t('settings.providers.editor.helpers')} about={t('settings.providers.editor.fetch')}>
            <Term name="num">{t('settings.providers.editor.helper.num')}</Term>
            <Term name={'error("…")'}>{t('settings.providers.editor.helper.error')}</Term>
            <Term name="$now, $fields">{t('settings.providers.editor.helper.vars')}</Term>
          </Reference>
        </Card>

        {(unplaced.length > 0 || (problems.length > 0 && !run.busy)) && (
          <div class="st-refused" role="alert">
            <p class="st-refused__title">{t('settings.providers.editor.invalid')}</p>
            <ul>
              {problems.map((problem, index) => (
                <li key={index}>{problem.field ? `${problem.field}: ${problem.text}` : problem.text}</li>
              ))}
            </ul>
          </div>
        )}
        {save.error !== undefined && (
          <p class="form-error" role="alert">
            {errorText(save.error)}
          </p>
        )}

        <div class="st-editor__actions">
          {editing && (
            <Button
              variant="ghost"
              icon={Trash2}
              class="st-editor__delete"
              disabled={save.busy}
              onClick={() => {
                remove.clear();
                setConfirming(true);
              }}
            >
              {t('settings.providers.delete')}
            </Button>
          )}
          <LinkButton href={CATALOG}>{t('common.action.cancel')}</LinkButton>
          <Button type="submit" variant="primary" busy={save.busy}>
            {t('settings.providers.editor.save')}
          </Button>
        </div>
      </form>

      <aside class="st-editor__bench" aria-label={t('settings.providers.bench.title')}>
        <Card class="st-part">
          <h3 class="st-card-title">{t('settings.providers.bench.title')}</h3>
          <p class="st-note">{t('settings.providers.bench.about')}</p>
          {draft.fields.length === 0 && <p class="st-note">{t('settings.providers.bench.noFields')}</p>}
          {draft.fields.map((field, index) => (
            <Field key={index} label={field.label.trim() || field.key.trim() || t('settings.providers.editor.field', { n: index + 1 })} optional={field.optional}>
              <Input
                mono
                type={field.secret ? 'password' : 'text'}
                value={credential[field.key] ?? ''}
                onInput={(value) => setCredential({ ...credential, [field.key]: value })}
                placeholder={field.placeholder || field.default}
                autoComplete="off"
                spellcheck={false}
                autoCapitalize="off"
                data-1p-ignore
              />
            </Field>
          ))}
          <Button icon={Play} busy={run.busy} class="st-add" onClick={() => void run.run()}>
            {t('settings.providers.bench.run')}
          </Button>
          {run.error !== undefined && (
            <p class="form-error" role="alert">
              {errorText(run.error)}
            </p>
          )}
          {problems.length > 0 && !result && !run.busy && (
            <p class="st-note" role="status">
              {t('settings.providers.bench.invalid')}
            </p>
          )}
          {result && <Outcome result={result} />}
        </Card>
      </aside>

      <ConfirmDialog
        open={confirming}
        onClose={() => setConfirming(false)}
        title={t('settings.providers.delete.title', { name: draft.name })}
        confirmLabel={t('settings.providers.delete')}
        busy={remove.busy}
        error={remove.error === undefined ? undefined : errorText(remove.error)}
        onConfirm={() => void remove.run()}
      >
        <p>{t('settings.providers.delete.body')}</p>
      </ConfirmDialog>
    </div>
  );
}

/** A short list of names and what each means, kept beside what it explains. */
function Reference({ title, about, children }: { title: string; about: string; children: ComponentChildren }) {
  return (
    <div class="st-ref">
      <p class="st-ref__title">{title}</p>
      <p>{about}</p>
      <dl class="st-ref__list">{children}</dl>
    </div>
  );
}

function Term({ name, children }: { name: string; children: ComponentChildren }) {
  return (
    <div class="st-ref__row">
      <dt>
        <code>{name}</code>
      </dt>
      <dd>{children}</dd>
    </div>
  );
}

/** What a run came to: the meters, or the error — and the platform's own
 *  answer underneath, to write the program against. */
function Outcome({ result }: { result: TestResult }) {
  const facts = [result.status > 0 ? `HTTP ${result.status}` : '', result.latency_ms > 0 ? t('settings.providers.bench.took', { ms: result.latency_ms }) : ''].filter(Boolean).join(' · ');
  const raw = result.body === undefined || result.body === null ? undefined : typeof result.body === 'string' ? result.body : JSON.stringify(result.body, null, 2);
  const good = result.ok && result.samples.length > 0;
  return (
    <div class="st-outcome" role="status">
      <div class={good ? 'st-outcome__verdict st-outcome__verdict--ok' : 'st-outcome__verdict st-outcome__verdict--failed'}>
        <p class="st-outcome__title">
          {!result.ok ? t('settings.providers.bench.failed') : good ? t('settings.providers.bench.ok', { n: result.samples.length }) : t('settings.providers.bench.none')}
          {facts && <span class="st-outcome__facts num"> {facts}</span>}
        </p>
        {!result.ok && <p class="st-outcome__error">{result.error || `HTTP ${result.status}`}</p>}
      </div>

      {good && (
        <div class="st-table-wrap" tabIndex={0}>
          <table class="st-table">
            <thead>
              <tr>
                {['key', 'kind', 'unit', 'value', 'limit', 'resets_at'].map((column) => (
                  <th key={column} scope="col">
                    <code>{column}</code>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {result.samples.map((sample) => (
                <tr key={sample.key}>
                  <th scope="row">
                    <span class="st-table__key">{sample.key}</span>
                    {(sample.name || sample.name_zh) && <span class="st-table__name">{pick(sample.name ?? '', sample.name_zh) || sample.name_zh}</span>}
                  </th>
                  <td>{sample.kind}</td>
                  <td>{sample.unit || '—'}</td>
                  <td class="num">{sample.value}</td>
                  <td class="num">{sample.limit ?? '—'}</td>
                  <td>
                    {sample.resets_at ? formatStamp(sample.resets_at) : '—'}
                    {sample.window_seconds ? <span class="st-table__name">{t('settings.providers.bench.window', { span: formatSpan(sample.window_seconds * 1000) })}</span> : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {raw !== undefined ? (
        <details class="st-raw" open={!good}>
          <summary>{t('settings.providers.bench.raw')}</summary>
          <div class="st-raw__body">
            <CopyButton text={raw} />
            <pre class="st-code" tabIndex={0}>
              {raw}
            </pre>
          </div>
        </details>
      ) : (
        <p class="st-note">{t('settings.providers.bench.raw.none')}</p>
      )}
    </div>
  );
}

// Tokens: access for scripts and agents. A token is shown exactly once, right
// after it is made — with what to paste where — and never again.
import { KeyRound, Plus, Trash2 } from 'lucide-preact';
import type { JSX } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { ApiError } from '../../api/client';
import { createToken, deleteToken } from '../../api/endpoints';
import type { NewToken, Scope, Token, TokenInput } from '../../api/types';
import { Banner, Button, Card, Chip, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, Input, Section, Select, Skeleton, toast } from '../../components';
import { errorText } from '../../errors';
import { addDays, dayOf, dayStart, formatAgo, formatDateTime, formatDayOf, toMs } from '../../format';
import { useAction } from '../../hooks';
import { t, type TKey } from '../../i18n';
import { now } from '../../state/clock';
import { useResource } from '../../state/resource';
import { allows, curlIngest, curlOverview, mcpConfig } from './logic';
import { CheckBox, CopyButton, DateInput, Group, Labelled, Snippet } from './parts';
import { tokens } from './state';

const SCOPES: readonly Scope[] = ['read', 'act', 'ingest', 'admin'];
const SCOPE_WORDS: Record<Scope, { label: TKey; help: TKey }> = {
  read: { label: 'settings.scope.read', help: 'settings.scope.read.help' },
  act: { label: 'settings.scope.act', help: 'settings.scope.act.help' },
  ingest: { label: 'settings.scope.ingest', help: 'settings.scope.ingest.help' },
  admin: { label: 'settings.scope.admin', help: 'settings.scope.admin.help' },
};

const scopeLabel = (scope: Scope): string => (SCOPE_WORDS[scope] ? t(SCOPE_WORDS[scope].label) : scope);

export function TokensSection() {
  useResource(tokens);
  const [creating, setCreating] = useState(false);
  const [fresh, setFresh] = useState<NewToken | null>(null);
  const list = tokens.data.value;

  const create = (
    <Button variant="primary" size="sm" icon={Plus} onClick={() => setCreating(true)}>
      {t('settings.tokens.new')}
    </Button>
  );

  return (
    <>
      <p class="st-intro">{t('settings.tokens.intro')}</p>
      {fresh && <FreshToken token={fresh} onDone={() => setFresh(null)} />}

      <Section title={t('settings.tokens.title')} meta={list && list.length > 0 ? <span class="num">{list.length}</span> : undefined} action={list && list.length > 0 ? create : undefined}>
        {!list ? (
          tokens.error.value ? (
            <ErrorState compact error={tokens.error.value} onRetry={() => void tokens.load()} />
          ) : (
            <div class="st-list" aria-busy="true">
              {[0, 1].map((index) => (
                <Skeleton key={index} height="76px" />
              ))}
            </div>
          )
        ) : list.length === 0 ? (
          <Card>
            <EmptyState compact icon={KeyRound} title={t('settings.tokens.empty.title')} action={create}>
              {t('settings.tokens.empty.body')}
            </EmptyState>
          </Card>
        ) : (
          <ul class="st-list">
            {list.map((token) => (
              <TokenRow key={token.id} token={token} fresh={token.id === fresh?.id} />
            ))}
          </ul>
        )}
      </Section>

      <CreateDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={(token) => {
          setCreating(false);
          setFresh(token);
          void tokens.load();
        }}
      />
    </>
  );
}

// ---------- the list ----------

function TokenRow({ token, fresh }: { token: Token; fresh: boolean }) {
  const [confirming, setConfirming] = useState(false);
  const revoke = useAction(async () => {
    await deleteToken(token.id);
    tokens.set((list) => list?.filter((each) => each.id !== token.id));
    setConfirming(false);
    toast(t('settings.tokens.revoked', { name: token.name }));
  });
  const expired = token.expires_at !== null && toMs(token.expires_at) <= now.value;

  return (
    <li class={expired ? 'st-item is-off' : 'st-item'}>
      <span class="st-item__icon">
        <KeyRound size={18} />
      </span>
      <div class="st-item__text">
        <p class="st-item__name">
          <span class="st-item__title">{token.name}</span>
          {fresh && <Chip tone="accent">{t('settings.tokens.row.new')}</Chip>}
          {expired && <Chip tone="critical">{t('settings.tokens.row.expired')}</Chip>}
        </p>
        <p class="st-item__chips">
          <code class="st-prefix" title={t('settings.tokens.row.prefix')}>
            {token.prefix}
          </code>
          {token.scopes.map((scope) => (
            <Chip key={scope} title={SCOPE_WORDS[scope] ? t(SCOPE_WORDS[scope].help) : undefined}>
              {scopeLabel(scope)}
            </Chip>
          ))}
        </p>
        <p class="st-item__meta">
          <time dateTime={token.created_at} title={formatDateTime(token.created_at)}>
            {t('settings.tokens.row.created', { date: formatDayOf(token.created_at) })}
          </time>
          {token.last_used_at ? (
            <time dateTime={token.last_used_at} title={formatDateTime(token.last_used_at)}>
              {t('settings.tokens.row.used', { ago: formatAgo(token.last_used_at) })}
            </time>
          ) : (
            <span>{t('settings.tokens.row.unused')}</span>
          )}
          {token.expires_at === null ? (
            <span>{t('settings.tokens.row.noExpiry')}</span>
          ) : (
            <time dateTime={token.expires_at} title={formatDateTime(token.expires_at)}>
              {t(expired ? 'settings.tokens.row.expiredOn' : 'settings.tokens.row.expires', { date: formatDayOf(token.expires_at) })}
            </time>
          )}
        </p>
      </div>
      <div class="st-item__actions">
        <Button
          size="sm"
          icon={Trash2}
          aria-label={t('settings.tokens.revoke.label', { name: token.name })}
          onClick={() => {
            revoke.clear();
            setConfirming(true);
          }}
        >
          {t('settings.tokens.revoke')}
        </Button>
      </div>
      <ConfirmDialog
        open={confirming}
        onClose={() => setConfirming(false)}
        title={t('settings.tokens.revoke.title', { name: token.name })}
        confirmLabel={t('settings.tokens.revoke')}
        busy={revoke.busy}
        error={revoke.error === undefined ? undefined : errorText(revoke.error)}
        onConfirm={() => void revoke.run()}
      >
        <p>{t('settings.tokens.revoke.body')}</p>
      </ConfirmDialog>
    </li>
  );
}

// ---------- making one ----------

type Expiry = 'never' | '7' | '30' | '90' | '365' | 'date';
const EXPIRIES: readonly Expiry[] = ['never', '7', '30', '90', '365', 'date'];

function CreateDialog({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (token: NewToken) => void }) {
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!open) setBusy(false);
  }, [open]);
  return (
    <Dialog open={open} onClose={onClose} title={t('settings.tokens.create.title')} description={t('settings.tokens.create.body')} busy={busy}>
      <CreateForm onBusy={setBusy} onCancel={onClose} onCreated={onCreated} />
    </Dialog>
  );
}

function CreateForm({ onBusy, onCancel, onCreated }: { onBusy: (busy: boolean) => void; onCancel: () => void; onCreated: (token: NewToken) => void }) {
  const [name, setName] = useState('');
  const [scopes, setScopes] = useState<readonly Scope[]>(['read']);
  const [expiry, setExpiry] = useState<Expiry>('never');
  const [day, setDay] = useState('');
  const [problems, setProblems] = useState<Record<string, string>>({});
  const today = dayOf(now.value);

  const clear = (key: string) => {
    if (problems[key]) setProblems({ ...problems, [key]: '' });
  };

  const create = useAction(async () => {
    const found: Record<string, string> = {};
    if (name.trim() === '') found.name = t('settings.form.required', { field: t('settings.tokens.form.name') });
    if (scopes.length === 0) found.scopes = t('settings.tokens.form.scopes.none');
    const input: TokenInput = { name: name.trim(), scopes: SCOPES.filter((scope) => scopes.includes(scope)) };
    if (expiry === 'date') {
      // Good through the chosen day: until a second before the midnight
      // after it, in the configured time zone.
      if (day === '') found.day = t('settings.tokens.form.expiry.pick');
      else if (dayStart(addDays(day, 1)) <= now.value) found.day = t('settings.tokens.form.expiry.past');
      else input.expires_at = new Date(dayStart(addDays(day, 1)) - 1000).toISOString();
    } else if (expiry !== 'never') {
      input.expires_at = new Date(now.value + Number(expiry) * 86_400_000).toISOString();
    }
    setProblems(found);
    if (Object.keys(found).length > 0) return;

    onBusy(true);
    try {
      onCreated(await createToken(input));
    } catch (cause) {
      if (cause instanceof ApiError && cause.code === 'invalid' && /^a token named /.test(cause.message)) {
        setProblems({ name: t('settings.tokens.form.name.taken') });
        return;
      }
      throw cause;
    } finally {
      onBusy(false);
    }
  });

  const submit = (event: JSX.TargetedEvent<HTMLFormElement>) => {
    event.preventDefault();
    void create.run();
  };

  return (
    <form class="st-form" onSubmit={submit} noValidate>
      <Field label={t('settings.tokens.form.name')} help={t('settings.tokens.form.name.help')} error={problems.name || undefined}>
        <Input
          value={name}
          onInput={(value) => {
            setName(value);
            clear('name');
          }}
          maxLength={80}
          autoComplete="off"
          autoFocus
          data-1p-ignore
        />
      </Field>

      <Group label={t('settings.tokens.form.scopes')} error={problems.scopes || undefined}>
        <div class="st-checks">
          {SCOPES.map((scope) => (
            <CheckBox
              key={scope}
              checked={scopes.includes(scope)}
              label={
                <>
                  {t(SCOPE_WORDS[scope].label)} <code>{scope}</code>
                </>
              }
              help={t(SCOPE_WORDS[scope].help)}
              onChange={(checked) => {
                setScopes(checked ? [...scopes, scope] : scopes.filter((each) => each !== scope));
                clear('scopes');
              }}
            />
          ))}
        </div>
      </Group>

      <div class="st-form__row">
        <Field label={t('settings.tokens.form.expiry')} help={expiry === 'never' ? t('settings.tokens.form.expiry.neverHelp') : t('settings.tokens.form.expiry.help')}>
          <Select value={expiry} onChange={setExpiry} options={EXPIRIES.map((each) => ({ value: each, label: t(`settings.tokens.form.expiry.${each}`) }))} />
        </Field>
        {expiry === 'date' && (
          <Labelled label={t('settings.tokens.form.expiry.day')} error={problems.day || undefined}>
            {(ids) => (
              <DateInput
                {...ids}
                value={day}
                min={today}
                onInput={(value) => {
                  setDay(value);
                  clear('day');
                }}
              />
            )}
          </Labelled>
        )}
      </div>

      {create.error !== undefined && (
        <p class="form-error" role="alert">
          {errorText(create.error)}
        </p>
      )}

      <div class="st-form__actions">
        <Button onClick={onCancel} disabled={create.busy}>
          {t('common.action.cancel')}
        </Button>
        <Button type="submit" variant="primary" busy={create.busy}>
          {t('settings.tokens.form.create')}
        </Button>
      </div>
    </form>
  );
}

// ---------- the one time it is shown ----------

function FreshToken({ token, onDone }: { token: NewToken; onDone: () => void }) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.focus({ preventScroll: true });
    heading.current?.scrollIntoView({ block: 'start', behavior: 'smooth' });
  }, [token.id]);
  const origin = location.origin;

  return (
    <Card class="st-fresh">
      <h2 class="st-card-title" tabIndex={-1} ref={heading}>
        {t('settings.tokens.fresh.title', { name: token.name })}
      </h2>
      <Banner tone="warning">{t('settings.tokens.fresh.once')}</Banner>

      <div class="st-secret">
        <code class="st-secret__value">{token.token}</code>
        <CopyButton text={token.token} label={t('settings.tokens.fresh.copy')} variant="primary" />
      </div>

      <h3 class="st-fresh__sub">{t('settings.tokens.fresh.use')}</h3>
      <Snippet title={t('settings.tokens.fresh.mcp')} note={t('settings.tokens.fresh.mcp.note')} text={mcpConfig(origin, token.token)} />
      {allows(token.scopes, 'read') && <Snippet title={t('settings.tokens.fresh.curl')} text={curlOverview(origin, token.token)} />}
      {allows(token.scopes, 'ingest') && <Snippet title={t('settings.tokens.fresh.ingest')} note={t('settings.tokens.fresh.ingest.note')} text={curlIngest(origin, token.token)} />}

      <div class="st-fresh__actions">
        <Button variant="primary" onClick={onDone}>
          {t('settings.tokens.fresh.done')}
        </Button>
      </div>
    </Card>
  );
}

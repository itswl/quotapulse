// Providers: how platforms are read. The catalog lists the built-in
// definitions, which cannot be changed, and the user's own; the editor for
// one definition lives beneath it:
//
//   /settings/providers               the catalog
//   /settings/providers/new           a new definition; ?from=<id> starts from a copy
//   /settings/providers/edit/<id>     one of the user's own
import { Compass, CopyPlus, Pencil, Plug, Plus, Trash2 } from 'lucide-preact';
import { useState } from 'preact/hooks';
import { deleteProvider } from '../../api/endpoints';
import type { Checked, Definition } from '../../api/types';
import { Button, Card, Chip, ConfirmDialog, EmptyState, ErrorState, LinkButton, Menu, Section, Skeleton, toast, type Tone } from '../../components';
import { errorText } from '../../errors';
import { useAction } from '../../hooks';
import { pick, t, type TKey } from '../../i18n';
import { navigate, route } from '../../router';
import { accounts } from '../../state/accounts';
import { providers } from '../../state/providers';
import { useResource } from '../../state/resource';
import { ProviderEditor } from './provider-editor';

export const CHECKED: Record<Checked, { label: TKey; about: TKey; tone: Tone }> = {
  live: { label: 'settings.providers.checked.live', about: 'settings.providers.checked.live.about', tone: 'ok' },
  recorded: { label: 'settings.providers.checked.recorded', about: 'settings.providers.checked.recorded.about', tone: 'info' },
  docs: { label: 'settings.providers.checked.docs', about: 'settings.providers.checked.docs.about', tone: 'neutral' },
};

const duplicate = (id: string) => navigate(`/settings/providers/new?from=${encodeURIComponent(id)}`);

export function ProvidersSection({ rest }: { rest: string }) {
  useResource(providers);
  useResource(accounts);

  if (rest === 'new') {
    const from = route.value.query.get('from') ?? '';
    return <ProviderEditor key={`new:${from}`} from={from} />;
  }
  if (rest.startsWith('edit/')) {
    const id = rest.slice('edit/'.length);
    return <ProviderEditor key={`edit:${id}`} id={id} />;
  }
  if (rest !== '') {
    return (
      <EmptyState icon={Compass} title={t('common.state.notFound.title')} action={<LinkButton href="/settings/providers">{t('settings.providers.all')}</LinkButton>}>
        {t('settings.missing.body')}
      </EmptyState>
    );
  }
  return <Catalog />;
}

function Catalog() {
  const all = providers.data.value;
  const create = (
    <LinkButton variant="primary" size="sm" icon={Plus} href="/settings/providers/new">
      {t('settings.providers.new')}
    </LinkButton>
  );

  if (!all) {
    return (
      <>
        <p class="st-intro">{t('settings.providers.intro')}</p>
        {providers.error.value ? (
          <ErrorState compact error={providers.error.value} onRetry={() => void providers.load()} />
        ) : (
          <div class="st-list" aria-busy="true">
            {[0, 1, 2, 3].map((index) => (
              <Skeleton key={index} height="60px" />
            ))}
          </div>
        )}
      </>
    );
  }

  const byName = (a: Definition, b: Definition) => pick(a.name, a.name_zh).localeCompare(pick(b.name, b.name_zh));
  const own = all.filter((each) => !each.builtin).sort(byName);
  const builtin = all.filter((each) => each.builtin).sort(byName);
  // How many accounts read through each definition: why one cannot be deleted.
  const used = new Map<string, number>();
  for (const account of accounts.data.value ?? []) used.set(account.provider, (used.get(account.provider) ?? 0) + 1);

  return (
    <>
      <p class="st-intro">{t('settings.providers.intro')}</p>

      <Section title={t('settings.providers.own')} meta={own.length > 0 ? <span class="num">{own.length}</span> : undefined} action={own.length > 0 ? create : undefined}>
        {own.length === 0 ? (
          <Card>
            <EmptyState compact icon={Plug} title={t('settings.providers.own.empty.title')} action={create}>
              {t('settings.providers.own.empty.body')}
            </EmptyState>
          </Card>
        ) : (
          <ul class="st-list">
            {own.map((definition) => (
              <DefinitionRow key={definition.id} definition={definition} accounts={used.get(definition.id) ?? 0} />
            ))}
          </ul>
        )}
      </Section>

      <Section title={t('settings.providers.builtin')} meta={<span class="num">{builtin.length}</span>}>
        <p class="st-note st-legend__lead">{t('settings.providers.builtin.about')}</p>
        <dl class="st-legend">
          {(Object.keys(CHECKED) as Checked[]).map((level) => (
            <div key={level} class="st-legend__row">
              <dt>
                <Chip tone={CHECKED[level].tone}>{t(CHECKED[level].label)}</Chip>
              </dt>
              <dd>{t(CHECKED[level].about)}</dd>
            </div>
          ))}
        </dl>
        <ul class="st-list">
          {builtin.map((definition) => (
            <DefinitionRow key={definition.id} definition={definition} accounts={used.get(definition.id) ?? 0} />
          ))}
        </ul>
      </Section>
    </>
  );
}

function DefinitionRow({ definition, accounts: count }: { definition: Definition; accounts: number }) {
  const [confirming, setConfirming] = useState(false);
  const name = pick(definition.name, definition.name_zh);
  const checked = definition.builtin && definition.checked ? CHECKED[definition.checked] : undefined;

  const remove = useAction(async () => {
    await deleteProvider(definition.id);
    providers.set((list) => list?.filter((each) => each.id !== definition.id));
    setConfirming(false);
    toast(t('settings.providers.deleted', { name }));
  });

  return (
    <li class="st-item st-item--plain">
      <span class="st-item__icon">
        <Plug size={18} />
      </span>
      <div class="st-item__text">
        <p class="st-item__name">
          <span class="st-item__title">{name}</span>
          {checked && <Chip tone={checked.tone}>{t(checked.label)}</Chip>}
        </p>
        <p class="st-item__meta">
          <code class="st-prefix">{definition.id}</code>
          <span>{t('settings.providers.row.fields', { n: definition.fields.length })}</span>
          <span>{count > 0 ? t('settings.providers.row.accounts', { n: count }) : t('settings.providers.row.unused')}</span>
        </p>
      </div>
      <div class="st-item__actions">
        {definition.builtin ? (
          <Button size="sm" icon={CopyPlus} aria-label={t('settings.providers.duplicate.label', { name })} onClick={() => duplicate(definition.id)}>
            {t('settings.providers.duplicate')}
          </Button>
        ) : (
          <>
            <LinkButton size="sm" icon={Pencil} href={`/settings/providers/edit/${encodeURIComponent(definition.id)}`} aria-label={t('settings.providers.edit.label', { name })}>
              {t('common.action.edit')}
            </LinkButton>
            <Menu
              label={t('settings.providers.row.actions', { name })}
              entries={[
                { label: t('settings.providers.duplicate'), icon: CopyPlus, onSelect: () => duplicate(definition.id) },
                { separator: true },
                {
                  label: t('common.action.delete'),
                  icon: Trash2,
                  danger: true,
                  onSelect: () => {
                    remove.clear();
                    setConfirming(true);
                  },
                },
              ]}
            />
          </>
        )}
      </div>
      {!definition.builtin && (
        <ConfirmDialog
          open={confirming}
          onClose={() => setConfirming(false)}
          title={t('settings.providers.delete.title', { name })}
          confirmLabel={t('settings.providers.delete')}
          busy={remove.busy}
          error={remove.error === undefined ? undefined : errorText(remove.error)}
          onConfirm={() => void remove.run()}
        >
          <p>{count > 0 ? t('settings.providers.delete.inUse', { n: count }) : t('settings.providers.delete.body')}</p>
        </ConfirmDialog>
      )}
    </li>
  );
}

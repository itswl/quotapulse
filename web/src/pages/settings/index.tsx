// The Settings pages: one area, five sections, each at its own address.
//
//   /settings/channels      where notifications go (also what /settings shows)
//   /settings/providers     how platforms are read; the editor lives beneath it
//   /settings/tokens        access for scripts and agents
//   /settings/preferences   the defaults
//   /settings/system        about, backup, export, import
//
// `params.section` names the section and `params.rest` anything deeper.
// Other pages link to /settings/channels, /settings/providers and
// /settings/preferences.
import { Compass } from 'lucide-preact';
import { useEffect, useRef } from 'preact/hooks';
import { cx, EmptyState, LinkButton, PageHeader } from '../../components';
import { useTitle } from '../../hooks';
import { t, type TKey } from '../../i18n';
import { Link, navigate } from '../../router';
import type { PageProps } from '../../routes';
import { ChannelsSection } from './channels';
import { PreferencesSection } from './preferences';
import { ProvidersSection } from './providers';
import { SystemSection } from './system';
import { TokensSection } from './tokens';

type SectionId = 'channels' | 'providers' | 'tokens' | 'preferences' | 'system';

const SECTIONS: readonly { id: SectionId; label: TKey }[] = [
  { id: 'channels', label: 'settings.nav.channels' },
  { id: 'providers', label: 'settings.nav.providers' },
  { id: 'tokens', label: 'settings.nav.tokens' },
  { id: 'preferences', label: 'settings.nav.preferences' },
  { id: 'system', label: 'settings.nav.system' },
];

export default function SettingsPage({ params }: PageProps) {
  const section = SECTIONS.find((each) => each.id === (params.section ?? 'channels'));
  const title = t('common.nav.settings');
  useTitle(section ? `${t(section.label)} · ${title}` : title);

  // /settings is the channels: say so in the address, so that a reload or a
  // copied link lands on the same section.
  useEffect(() => {
    if (params.section === undefined) navigate('/settings/channels', { replace: true });
  }, [params.section]);

  return (
    <div class="st">
      <PageHeader title={title} />
      <SectionNav current={section?.id} />
      {!section ? (
        <EmptyState icon={Compass} title={t('common.state.notFound.title')} action={<LinkButton href="/settings/channels">{t('settings.nav.channels')}</LinkButton>}>
          {t('settings.missing.body')}
        </EmptyState>
      ) : section.id === 'channels' ? (
        <ChannelsSection />
      ) : section.id === 'providers' ? (
        <ProvidersSection rest={params.rest ?? ''} />
      ) : section.id === 'tokens' ? (
        <TokensSection />
      ) : section.id === 'preferences' ? (
        <PreferencesSection />
      ) : (
        <SystemSection />
      )}
    </div>
  );
}

/** The sections as links. On a phone the row scrolls sideways, and brings
 *  the current section into view. */
function SectionNav({ current }: { current: SectionId | undefined }) {
  const nav = useRef<HTMLElement>(null);
  useEffect(() => {
    const row = nav.current;
    const active = row?.querySelector<HTMLElement>('[aria-current="page"]');
    if (row && active) row.scrollLeft = active.offsetLeft - (row.clientWidth - active.offsetWidth) / 2;
  }, [current]);
  return (
    <nav class="st-nav" aria-label={t('settings.nav.label')} ref={nav}>
      <div class="st-nav__row">
        {SECTIONS.map((each) => (
          <Link key={each.id} href={`/settings/${each.id}`} class={cx('st-nav__item', each.id === current && 'is-active')} aria-current={each.id === current ? 'page' : undefined}>
            {t(each.label)}
          </Link>
        ))}
      </div>
    </nav>
  );
}

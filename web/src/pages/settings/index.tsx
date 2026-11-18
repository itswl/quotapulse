// The Settings pages: a list of the sections, and each section at its own
// address beneath it.
//
//   /settings               the list
//   /settings/channels      where notifications go
//   /settings/providers     how platforms are read; the editor lives beneath it
//   /settings/tokens        access for scripts and agents
//   /settings/preferences   the defaults
//   /settings/system        about, backup, export, import
//
// `params.section` names the section and `params.rest` anything deeper.
// Other pages link to /settings/channels, /settings/providers and
// /settings/preferences.
import { Bell, ChevronRight, Compass, History, KeyRound, LogOut, Plug, Server, SlidersHorizontal, type LucideIcon } from 'lucide-preact';
import { useEffect, useRef } from 'preact/hooks';
import { Button, cx, EmptyState, LinkButton, PageHeader } from '../../components';
import { useTitle } from '../../hooks';
import { t, type TKey } from '../../i18n';
import { Link } from '../../router';
import type { PageProps } from '../../routes';
import { sessionInfo, signOut } from '../../state/session';
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
  const section = SECTIONS.find((each) => each.id === params.section);
  const title = t('common.nav.settings');
  useTitle(section ? `${t(section.label)} · ${title}` : title);

  if (params.section === undefined) return <SettingsHome />;

  return (
    <div class="st">
      <PageHeader title={section ? t(section.label) : title} back={{ href: '/settings', label: title }} />
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

const HOME: { title: TKey; items: { href: string; label: TKey; hint: TKey; icon: LucideIcon }[] }[] = [
  {
    title: 'settings.home.notify',
    items: [
      { href: '/settings/channels', label: 'settings.nav.channels', hint: 'settings.home.channels', icon: Bell },
      { href: '/activity/notifications', label: 'settings.home.log.title', hint: 'settings.home.log', icon: History },
    ],
  },
  {
    title: 'settings.home.connect',
    items: [
      { href: '/settings/providers', label: 'settings.nav.providers', hint: 'settings.home.providers', icon: Plug },
      { href: '/settings/tokens', label: 'settings.nav.tokens', hint: 'settings.home.tokens', icon: KeyRound },
    ],
  },
  {
    title: 'settings.home.this',
    items: [
      { href: '/settings/preferences', label: 'settings.nav.preferences', hint: 'settings.home.preferences', icon: SlidersHorizontal },
      { href: '/settings/system', label: 'settings.nav.system', hint: 'settings.home.system', icon: Server },
    ],
  },
];

/** The sections as a list, the way a phone's own settings are: grouped,
 *  each with a line saying what is in it. */
function SettingsHome() {
  const info = sessionInfo.value;
  return (
    <div class="st st-home">
      <PageHeader title={t('common.nav.settings')} />
      {HOME.map((group) => (
        <section key={group.title} class="st-home__group" aria-label={t(group.title)}>
          <h2 class="st-home__title">{t(group.title)}</h2>
          <ul class="st-home__list">
            {group.items.map((item) => (
              <li key={item.href}>
                <Link class="st-home__item" href={item.href}>
                  <span class="st-home__icon" aria-hidden="true">
                    <item.icon size={18} />
                  </span>
                  <span class="st-home__text">
                    <span class="st-home__label">{t(item.label)}</span>
                    <span class="st-home__hint">{t(item.hint)}</span>
                  </span>
                  <ChevronRight class="st-home__go" size={16} />
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ))}
      <div class="st-home__foot">
        <Button icon={LogOut} onClick={() => void signOut().catch(() => undefined)}>
          {t('common.shell.signOut')}
        </Button>
        {info?.version && <p class="st-home__version num">{t('common.shell.version', { v: info.version })}</p>}
      </div>
    </div>
  );
}

/** The sections as links, for moving between them without going back to
 *  the list. On a narrow screen the row scrolls sideways, and brings the
 *  current section into view. */
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

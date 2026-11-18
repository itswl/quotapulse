// The shell: the bars around the page, the standing banners, toasts, and the
// outlet where the page renders. Signed out, the sign-in screen stands in for
// all of it.
//
// Now is home. Watching, Money, History and the settings are a step down
// from it, and everything opened from them a step further. A phone has one
// slim bar at the top: at home the mark, adding and the settings; a step
// down, the way back. Anything wider has room for the three places to go in
// the bar as well, and keeps them there.
import { BellOff, CalendarPlus, ChevronLeft, Compass, LucideProvider, Plug, Plus, Settings, SquareTerminal, WifiOff, type LucideIcon } from 'lucide-preact';
import { useEffect, useLayoutEffect, useState } from 'preact/hooks';
import { Banner, Button, Chip, cx, Dialog, EmptyState, LinkButton, Logo, Toaster } from './components';
import { useTitle } from './hooks';
import { lang, t, type TKey } from './i18n';
import LoginPage from './pages/login';
import { verdictOf } from './pages/now/horizon';
import { goBack, Link, navigate, openedFrom, route } from './router';
import { resolve, type Area } from './routes';
import { online } from './state/live';
import { overview } from './state/overview';
import { useResource } from './state/resource';
import { muted, sessionInfo, sessionStatus } from './state/session';

export function App() {
  return (
    <LucideProvider size={16} strokeWidth={1.75}>
      {sessionStatus.value === 'signedIn' ? <Shell /> : <LoginPage />}
    </LucideProvider>
  );
}

const NAV: { area: Area; href: string; label: TKey }[] = [
  { area: 'now', href: '/', label: 'common.nav.now' },
  { area: 'watching', href: '/meters', label: 'common.nav.watching' },
  { area: 'money', href: '/spend', label: 'common.nav.money' },
];

/** Going where you already are takes you back to the top of it. */
function toTop(): void {
  scrollTo({ top: 0, behavior: 'smooth' });
}

const AREA_LABEL: Record<Area, TKey> = {
  now: 'common.nav.now',
  watching: 'common.nav.watching',
  money: 'common.nav.money',
  history: 'common.nav.history',
  settings: 'common.nav.settings',
};

/** The page a step up from this one: a meter's list, a settings section's
 *  settings, and home for the rest. */
function parentOf(path: string): string {
  if (/^\/meters\/[^/]+/.test(path)) return '/meters';
  if (/^\/settings\/[^/]+/.test(path)) return '/settings';
  return '/';
}

/** Where the way back goes, and what it is called: the page this one was
 *  opened from, or the one a step up when it was opened from a link. */
function backTo(path: string): { href: string; label: string } {
  const href = openedFrom() ?? parentOf(path);
  const area = resolve(new URL(href, location.origin).pathname)?.area ?? 'now';
  return { href, label: t(AREA_LABEL[area]) };
}

/** Open problems that ask for a person: what the badge on Now counts, and
 *  the number on the app's icon. */
function openIssues(): { count: number; tone: string } {
  const open = (overview.data.value?.attention ?? []).filter((incident) => incident.state === 'open' && incident.kind !== 'due');
  const verdict = verdictOf(open);
  return { count: verdict.fresh, tone: verdict.tone };
}

function Shell() {
  // The overview is the source of the server's clock and time zone, which
  // every page needs; the bars also count its open issues.
  useResource(overview);
  const current = route.value;
  const match = resolve(current.path);
  const Page = match?.page;
  const area = match?.area;
  const home = area === 'now';
  // Adding belongs where the things added show up.
  const adds = home || (area === 'watching' && !match?.params.id);
  const back = home ? null : backTo(current.path);
  const bar = useTopBar(current.path);
  const issues = openIssues();
  const [adding, setAdding] = useState(false);

  // An installed app shows the count on its icon, as a native one would.
  useEffect(() => {
    const nav = navigator as Navigator & { setAppBadge?: (count?: number) => Promise<void>; clearAppBadge?: () => Promise<void> };
    if (!nav.setAppBadge) return;
    (issues.count > 0 ? nav.setAppBadge(issues.count) : nav.clearAppBadge?.())?.catch(() => undefined);
  }, [issues.count]);

  const settings = t('common.nav.settings');
  return (
    <div class="shell">
      <a class="skip-link" href="#main">
        {t('common.nav.skip')}
      </a>
      <header class={cx('topbar', home && 'is-home', adds && 'can-add', bar.scrolled && 'is-scrolled', bar.title !== null && 'is-carrying')}>
        {back && (
          <a
            class="topbar__back"
            href={back.href}
            onClick={(event) => {
              if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
              event.preventDefault();
              goBack(back.href);
            }}
          >
            <ChevronLeft size={22} />
            <span class="truncate">{back.label}</span>
          </a>
        )}
        <Brand path={current.path} />
        <nav class="topnav" aria-label={t('common.nav.label')}>
          {NAV.map((item) => (
            <NavLink key={item.area} item={item} active={area === item.area} path={current.path} issues={issues} class="topnav__item" />
          ))}
        </nav>
        {/* Decoration: the heading it repeats is still in the page. */}
        <p class="topbar__title truncate" aria-hidden="true">
          {bar.title}
        </p>
        <div class="topbar__actions">
          <Button class="topbar__add" icon={Plus} compact onClick={() => setAdding(true)}>
            {t('common.add.label')}
          </Button>
          <Link
            href="/settings"
            class={cx('icon-btn topbar__settings', area === 'settings' && 'is-active')}
            aria-label={settings}
            aria-current={area === 'settings' ? 'page' : undefined}
            title={settings}
          >
            <Settings size={18} />
          </Link>
        </div>
      </header>
      {muted.value && (
        <Banner slim tone="warning" icon={BellOff} class="shell__banner">
          {t('common.shell.muted')}
        </Banner>
      )}
      {!online.value && (
        <Banner slim tone="warning" icon={WifiOff} class="shell__banner">
          {t('common.shell.offline')}
        </Banner>
      )}
      <main class={cx('page', area && `page--${area}`)} id="main" tabIndex={-1}>
        {/* A new area mounts afresh, so no state leaks from one to the next.
            Within an area the page stays mounted and only its params change:
            Watching keeps its list while one meter after another opens. */}
        <div class="page__view" key={area ?? 'missing'}>
          {Page && match ? <Page params={match.params} /> : <NotFound />}
        </div>
      </main>
      <AddChooser open={adding} onClose={() => setAdding(false)} />
      <Toaster />
    </div>
  );
}

function NavLink({ item, active, path, issues, class: extra }: { item: (typeof NAV)[number]; active: boolean; path: string; issues: { count: number; tone: string }; class: string }) {
  const label = t(item.label);
  const badge = item.area === 'now' && issues.count > 0;
  return (
    <Link href={item.href} class={cx(extra, active && 'is-active')} aria-current={active ? 'page' : undefined} onClick={path === item.href ? toTop : undefined}>
      <span class="nav-label">{label}</span>
      {badge && (
        <>
          <span class={cx('nav-badge num', `nav-badge--${issues.tone}`)} aria-hidden="true">
            {issues.count}
          </span>
          <span class="sr-only">{t('common.nav.attention', { n: issues.count })}</span>
        </>
      )}
    </Link>
  );
}

/** How the phone's top bar stands, as a native title bar would. At the top
 *  of a page it is clear glass over whatever the page begins with. Once
 *  something has scrolled under it, it is a bar; and once the page's own
 *  heading has gone under, it carries the heading.
 *
 *  A new page is read as it renders, before it is painted, so that it never
 *  shows the last page's bar. Where a step through history then restores
 *  the scroll position, that scroll is read again before the frame is
 *  painted. */
function useTopBar(path: string): { scrolled: boolean; title: string | null } {
  const [scrolled, setScrolled] = useState(false);
  const [title, setTitle] = useState<string | null>(null);
  const language = lang.value;
  useLayoutEffect(() => {
    let frame = 0;
    const read = () => {
      frame = 0;
      setScrolled(scrollY > 4);
      const slot = document.querySelector('.topbar__title');
      const heading = document.querySelector('#main h1');
      // The title slot only has a box on a phone.
      if (!slot || !heading || getComputedStyle(slot).display === 'none') return setTitle(null);
      const top = document.querySelector('.topbar')?.getBoundingClientRect().bottom ?? 0;
      setTitle(heading.getBoundingClientRect().bottom < top ? heading.textContent : null);
    };
    const later = () => {
      if (frame === 0) frame = requestAnimationFrame(read);
    };
    addEventListener('scroll', later, { passive: true });
    addEventListener('resize', later, { passive: true });
    read();
    return () => {
      removeEventListener('scroll', later);
      removeEventListener('resize', later);
      cancelAnimationFrame(frame);
    };
  }, [path, language]);
  return { scrolled, title };
}

function Brand({ path }: { path: string }) {
  const info = sessionInfo.value;
  return (
    <div class="brand">
      <Link href="/" class="brand__link" aria-label={t('common.app.name')} onClick={path === '/' ? toTop : undefined}>
        <Logo size={24} />
        <span class="brand__name">{t('common.app.name')}</span>
      </Link>
      {info?.demo && (
        <Chip tone="info" title={t('common.shell.demo.hint')} class="brand__demo">
          {t('common.shell.demo')}
        </Chip>
      )}
    </div>
  );
}

/** One way in for everything that can be watched: a platform's account, a
 *  date, or a number a script reports. */
function AddChooser({ open, onClose }: { open: boolean; onClose: () => void }) {
  const choices: { icon: LucideIcon; title: TKey; hint: TKey; href: string }[] = [
    { icon: Plug, title: 'common.add.account', hint: 'common.add.account.hint', href: '/meters?add=1' },
    { icon: CalendarPlus, title: 'common.add.date', hint: 'common.add.date.hint', href: '/dates?add=1' },
    { icon: SquareTerminal, title: 'common.add.script', hint: 'common.add.script.hint', href: '/settings/tokens' },
  ];
  return (
    <Dialog open={open} onClose={onClose} title={t('common.add.title')} size="sm" class="add-chooser">
      <ul class="add-chooser__list">
        {choices.map((choice) => (
          <li key={choice.href}>
            <button
              type="button"
              class="add-chooser__choice"
              onClick={() => {
                onClose();
                navigate(choice.href);
              }}
            >
              <span class="add-chooser__icon">
                <choice.icon size={20} />
              </span>
              <span class="add-chooser__text">
                <span class="add-chooser__title">{t(choice.title)}</span>
                <span class="add-chooser__hint">{t(choice.hint)}</span>
              </span>
            </button>
          </li>
        ))}
      </ul>
    </Dialog>
  );
}

function NotFound() {
  const title = t('common.state.notFound.title');
  useTitle(title);
  return (
    <EmptyState icon={Compass} title={title} action={<LinkButton href="/">{t('common.state.notFound.action')}</LinkButton>}>
      {t('common.state.notFound.body')}
    </EmptyState>
  );
}

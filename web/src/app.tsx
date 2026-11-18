// The shell: navigation, the standing banners, toasts, and the outlet where
// the current page renders. Signed out, the sign-in screen stands in for all
// of it.
import {
  Activity,
  BellOff,
  CalendarDays,
  Compass,
  Gauge,
  LayoutDashboard,
  LucideProvider,
  Settings,
  Wallet,
  WifiOff,
  type LucideIcon,
} from 'lucide-preact';
import { useEffect, useState } from 'preact/hooks';
import type { Severity } from './api/types';
import { Banner, Chip, cx, EmptyState, LinkButton, Logo, Toaster } from './components';
import { useTitle } from './hooks';
import { lang, t, type TKey } from './i18n';
import LoginPage from './pages/login';
import { isActive, Link, route } from './router';
import { resolve, type Area } from './routes';
import { ShellTools } from './shell-tools';
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

const NAV: { area: Area; href: string; label: TKey; icon: LucideIcon; aside?: boolean }[] = [
  { area: 'overview', href: '/', label: 'common.nav.overview', icon: LayoutDashboard },
  { area: 'meters', href: '/meters', label: 'common.nav.meters', icon: Gauge },
  { area: 'dates', href: '/dates', label: 'common.nav.dates', icon: CalendarDays },
  { area: 'spend', href: '/spend', label: 'common.nav.spend', icon: Wallet },
  { area: 'activity', href: '/activity', label: 'common.nav.activity', icon: Activity },
  // Set apart in the rail. A phone's tab bar holds five, so there it is the
  // gear in the top bar instead.
  { area: 'settings', href: '/settings', label: 'common.nav.settings', icon: Settings, aside: true },
];

/** Going where you already are takes you back to the top of it. */
function toTop(): void {
  scrollTo({ top: 0, behavior: 'smooth' });
}

function Shell() {
  // The overview is the source of the server's clock and time zone, which
  // every page needs; the navigation also counts its open issues.
  useResource(overview);
  const current = route.value;
  const match = resolve(current.path);
  const Page = match?.page;
  const bar = useTopBar(current.path);
  const settings = t('common.nav.settings');
  return (
    <div class="shell">
      <a class="skip-link" href="#main">
        {t('common.nav.skip')}
      </a>
      <Navigation path={current.path} />
      <div class="shell__body">
        <header class={cx('topbar', bar.scrolled && 'is-scrolled', bar.title !== null && 'is-carrying')}>
          <Brand path={current.path} />
          {/* Decoration: the heading it repeats is still in the page. */}
          <p class="topbar__title truncate" aria-hidden="true">
            {bar.title}
          </p>
          <Link
            href="/settings"
            class={cx('icon-btn topbar__settings', isActive('/settings', current.path) && 'is-active')}
            aria-label={settings}
            aria-current={isActive('/settings', current.path) ? 'page' : undefined}
            title={settings}
          >
            <Settings size={20} />
          </Link>
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
        <main class="page" id="main" tabIndex={-1}>
          {/* The key remounts the page when the address moves to another
              meter, so no state leaks from one to the next — and the new
              page fades in. Within an area that does its own routing
              (settings sections), the page stays mounted and only its
              params change. */}
          <div class="page__view" key={match ? `${match.area}/${match.params.id ?? ''}` : 'missing'}>
            {Page && match ? <Page params={match.params} /> : <NotFound />}
          </div>
        </main>
      </div>
      <Toaster />
    </div>
  );
}

/** How the phone's top bar stands, as a native title bar would. At the top
 *  of a page it is clear glass over whatever the page begins with. Once
 *  something has scrolled under it, it is a bar; and once the page's own
 *  heading has gone under, it carries the heading. */
function useTopBar(path: string): { scrolled: boolean; title: string | null } {
  const [scrolled, setScrolled] = useState(false);
  const [title, setTitle] = useState<string | null>(null);
  const language = lang.value;
  useEffect(() => {
    let frame = 0;
    const read = () => {
      frame = 0;
      setScrolled(scrollY > 4);
      const bar = document.querySelector('.topbar');
      const heading = document.querySelector('#main h1');
      // The bar is display: none on wider screens, and then has no box.
      if (!bar || !heading || bar.getClientRects().length === 0) return setTitle(null);
      const gone = heading.getBoundingClientRect().bottom < bar.getBoundingClientRect().bottom;
      setTitle(gone ? heading.textContent : null);
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

function Navigation({ path }: { path: string }) {
  const info = sessionInfo.value;
  const open = openIssues();
  return (
    <aside class="nav">
      <Brand path={path} />
      <nav class="nav__items" aria-label={t('common.nav.label')}>
        {NAV.map((item) => {
          const active = isActive(item.href, path);
          const label = t(item.label);
          const Icon = item.icon;
          return (
            <Link
              key={item.area}
              href={item.href}
              class={cx('nav__item', active && 'is-active', item.aside && 'nav__item--aside')}
              aria-current={active ? 'page' : undefined}
              title={label}
              onClick={path === item.href ? toTop : undefined}
            >
              <span class="nav__icon">
                <Icon size={18} />
              </span>
              <span class="nav__label">{label}</span>
              {item.area === 'overview' && open.count > 0 && (
                <>
                  <span class={cx('nav__badge num', `nav__badge--${open.worst}`)} aria-hidden="true">
                    {open.count}
                  </span>
                  <span class="sr-only">{t('common.nav.attention', { n: open.count })}</span>
                </>
              )}
            </Link>
          );
        })}
      </nav>
      <div class="nav__foot">
        <ShellTools />
        {info?.version && <p class="nav__version num">{t('common.shell.version', { v: info.version })}</p>}
      </div>
    </aside>
  );
}

/** Open, unacknowledged issues: what the badge on Overview counts. Dates
 *  that are merely near are not issues. */
function openIssues(): { count: number; worst: Severity } {
  let count = 0;
  let worst: Severity = 'info';
  const rank: Record<Severity, number> = { info: 1, warning: 2, critical: 3 };
  for (const incident of overview.data.value?.attention ?? []) {
    if (incident.kind === 'due' || incident.acked_at) continue;
    count += 1;
    if (rank[incident.severity] > rank[worst]) worst = incident.severity;
  }
  return { count, worst };
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

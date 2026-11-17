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
import type { Severity } from './api/types';
import { Banner, Chip, cx, EmptyState, LinkButton, Logo, Toaster } from './components';
import { useTitle } from './hooks';
import { t, type TKey } from './i18n';
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

const NAV: { area: Area; href: string; label: TKey; icon: LucideIcon }[] = [
  { area: 'overview', href: '/', label: 'common.nav.overview', icon: LayoutDashboard },
  { area: 'meters', href: '/meters', label: 'common.nav.meters', icon: Gauge },
  { area: 'dates', href: '/dates', label: 'common.nav.dates', icon: CalendarDays },
  { area: 'spend', href: '/spend', label: 'common.nav.spend', icon: Wallet },
  { area: 'activity', href: '/activity', label: 'common.nav.activity', icon: Activity },
  { area: 'settings', href: '/settings', label: 'common.nav.settings', icon: Settings },
];

function Shell() {
  // The overview is the source of the server's clock and time zone, which
  // every page needs; the navigation also counts its open issues.
  useResource(overview);
  const current = route.value;
  const match = resolve(current.path);
  const Page = match?.page;
  return (
    <div class="shell">
      <a class="skip-link" href="#main">
        {t('common.nav.skip')}
      </a>
      <Navigation path={current.path} />
      <div class="shell__body">
        <header class="topbar">
          <Brand />
          <ShellTools />
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
              meter, so no state leaks from one to the next. Within an area
              that does its own routing (settings sections), the page stays
              mounted and only its params change. */}
          {Page && match ? <Page key={`${match.area}/${match.params.id ?? ''}`} params={match.params} /> : <NotFound />}
        </main>
      </div>
      <Toaster />
    </div>
  );
}

function Brand() {
  const info = sessionInfo.value;
  return (
    <div class="brand">
      <Link href="/" class="brand__link" aria-label={t('common.app.name')}>
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
      <Brand />
      <nav class="nav__items" aria-label={t('common.nav.label')}>
        {NAV.map((item) => {
          const active = isActive(item.href, path);
          const label = t(item.label);
          const Icon = item.icon;
          return (
            <Link key={item.area} href={item.href} class={cx('nav__item', active && 'is-active')} aria-current={active ? 'page' : undefined} title={label}>
              <Icon size={18} />
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

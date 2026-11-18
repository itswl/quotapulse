// History: the record of what happened, and of what was said about it. Two
// views, each with an address of its own:
//
//   /activity                  events, by day, newest first
//   /activity/notifications    the notification log (Settings links here)
import { Compass } from 'lucide-preact';
import { cx, EmptyState, LinkButton, PageHeader } from '../../components';
import { useTitle } from '../../hooks';
import { t } from '../../i18n';
import { Link } from '../../router';
import type { PageProps } from '../../routes';
import { Incidents } from './incidents';
import { Notifications } from './notifications';

type View = 'incidents' | 'notifications';

const VIEWS: { id: View; href: string; label: 'activity.tab.incidents' | 'activity.tab.notifications' }[] = [
  { id: 'incidents', href: '/activity', label: 'activity.tab.incidents' },
  { id: 'notifications', href: '/activity/notifications', label: 'activity.tab.notifications' },
];

export default function ActivityPage({ params }: PageProps) {
  const title = t('common.nav.history');
  const rest = params.rest ?? '';
  const view: View | null = rest === '' ? 'incidents' : rest === 'notifications' ? 'notifications' : null;
  useTitle(view === 'notifications' ? `${t('activity.tab.notifications')} · ${title}` : title);

  if (!view) {
    return (
      <EmptyState icon={Compass} title={t('common.state.notFound.title')} action={<LinkButton href="/activity">{title}</LinkButton>}>
        {t('common.state.notFound.body')}
      </EmptyState>
    );
  }

  return (
    <>
      <PageHeader title={title} />
      {/* A view is not a step: switching replaces the address rather than
          piling up behind the way back. */}
      <nav class="seg-links ac-views" aria-label={t('activity.tabs.label')}>
        {VIEWS.map((each) => (
          <Link key={each.id} href={each.href} replace class={cx('seg-links__item', view === each.id && 'is-active')} aria-current={view === each.id ? 'page' : undefined}>
            {t(each.label)}
          </Link>
        ))}
      </nav>
      <section aria-label={t(view === 'incidents' ? 'activity.tab.incidents' : 'activity.tab.notifications')}>{view === 'incidents' ? <Incidents /> : <Notifications />}</section>
    </>
  );
}

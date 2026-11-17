// The Activity page: the record of what happened, and of what was said about
// it. Two views, each with an address of its own:
//
//   /activity                  events: what is open, then history by day
//   /activity/notifications    the notification log
import { Compass } from 'lucide-preact';
import { EmptyState, LinkButton, PageHeader, Tabs } from '../../components';
import { useTitle } from '../../hooks';
import { t } from '../../i18n';
import { navigate } from '../../router';
import type { PageProps } from '../../routes';
import { Incidents } from './incidents';
import { Notifications } from './notifications';

type View = 'incidents' | 'notifications';

const PATHS: Record<View, string> = { incidents: '/activity', notifications: '/activity/notifications' };
const PANEL = 'activity-view';

export default function ActivityPage({ params }: PageProps) {
  const title = t('common.nav.activity');
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
      <div class="ac-tabs">
        <Tabs
          label={t('activity.tabs.label')}
          tabs={[
            { id: 'incidents', label: t('activity.tab.incidents') },
            { id: 'notifications', label: t('activity.tab.notifications') },
          ]}
          value={view}
          // A tab is not a step: it replaces the address rather than piling
          // up behind the back button.
          onChange={(next) => navigate(PATHS[next], { replace: true })}
          controls={PANEL}
        />
      </div>
      <div id={PANEL} role="tabpanel" aria-label={t(view === 'incidents' ? 'activity.tab.incidents' : 'activity.tab.notifications')}>
        {view === 'incidents' ? <Incidents /> : <Notifications />}
      </div>
    </>
  );
}

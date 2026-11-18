// The overview: is everything fine, and if not, what needs a person. One
// request (GET /api/v1/overview) feeds the whole page.
import { ArrowRight, BellPlus, CalendarPlus, Check, Plug, RefreshCw, type LucideIcon } from 'lucide-preact';
import { useEffect } from 'preact/hooks';
import type { IncidentView, Overview, Severity } from '../../api/types';
import { Banner, Button, Card, cx, ErrorState, IncidentRow, InfoTip, LinkButton, MeterCard, Section, Skeleton, statusIcon, Amount } from '../../components';
import { errorText } from '../../errors';
import { dayOf, formatAgo, formatDateTime, formatDay, formatMonth, toMs } from '../../format';
import { useTitle } from '../../hooks';
import { t } from '../../i18n';
import { Link } from '../../router';
import type { PageProps } from '../../routes';
import { now } from '../../state/clock';
import { overview } from '../../state/overview';
import { useResource } from '../../state/resource';
import { isSampling, sampleNow } from '../../state/sampling';
import { toast } from '../../components';
import { Upcoming } from './upcoming';

const METERS_SHOWN = 8;

export default function OverviewPage(_props: PageProps) {
  useTitle(t('common.nav.overview'));
  useResource(overview);
  const data = overview.data.value;

  if (!data) {
    if (overview.error.value) return <ErrorState error={overview.error.value} onRetry={() => void overview.load()} />;
    return <Loading />;
  }

  const { counts } = data;
  if (counts.accounts + counts.meters + counts.reminders + counts.channels === 0) return <FirstSteps />;

  const issues = data.attention.filter((incident) => incident.kind !== 'due');
  const meters = data.meters.filter((meter) => !meter.hidden);

  return (
    <div class="ov">
      <Verdict data={data} issues={issues} />

      {counts.channels === 0 && (
        <Banner
          tone="warning"
          class="ov-hint"
          action={
            <LinkButton size="sm" href="/settings/channels">
              {t('overview.noChannel.action')}
            </LinkButton>
          }
        >
          {t('overview.noChannel')}
        </Banner>
      )}

      {issues.length > 0 && (
        <Section title={t('overview.attention.title')}>
          <Card flush>
            <ul class="incidents">
              {issues.map((incident) => (
                <IncidentRow key={incident.id} incident={incident} />
              ))}
            </ul>
          </Card>
        </Section>
      )}

      {meters.length > 0 && (
        <Section
          title={t('overview.meters.title')}
          action={
            <Link class="more-link" href="/meters">
              {t('overview.meters.all', { n: meters.length })}
              <ArrowRight size={14} />
            </Link>
          }
        >
          <div class="meter-grid">
            {meters.slice(0, METERS_SHOWN).map((meter) => (
              <MeterCard key={meter.id} meter={meter} />
            ))}
          </div>
        </Section>
      )}

      {data.upcoming.length > 0 && (
        <Section
          title={t('overview.upcoming.title')}
          action={
            <Link class="more-link" href="/dates">
              {t('overview.upcoming.all')}
              <ArrowRight size={14} />
            </Link>
          }
        >
          <Upcoming reminders={data.upcoming} />
        </Section>
      )}

      {(counts.meters > 0 || data.spend.fixed > 0) && <Month data={data} />}
    </div>
  );
}

const RANK: Record<Severity, number> = { info: 1, warning: 2, critical: 3 };

function worst(incidents: IncidentView[]): Severity {
  let top: Severity = 'info';
  for (const incident of incidents) if (RANK[incident.severity] > RANK[top]) top = incident.severity;
  return top;
}

/** The sentence at the top: fine or not, and how fresh that answer is. */
function Verdict({ data, issues }: { data: Overview; issues: IncidentView[] }) {
  const fresh = issues.filter((incident) => incident.acked_at === null);
  const known = issues.length - fresh.length;

  let tone: 'ok' | 'neutral' | Severity = 'ok';
  let title = t('overview.verdict.clear');
  // The mark is a disc already: a bare tick in it, not the ringed one.
  let Icon: LucideIcon = Check;
  let note: string | null = null;
  if (fresh.length > 0) {
    tone = worst(fresh);
    title = t('overview.verdict.attention', { n: fresh.length });
    Icon = statusIcon(tone);
    if (known > 0) note = t('overview.verdict.plusKnown', { n: known });
  } else if (known > 0) {
    tone = 'neutral';
    title = t('overview.verdict.known');
    note = t('overview.verdict.known.note', { n: known });
  }

  // "Last read" is the most recent time anything was sampled or reported.
  let lastRead = 0;
  for (const account of data.accounts) if (account.sampled_at) lastRead = Math.max(lastRead, toMs(account.sampled_at));
  for (const meter of data.meters) if (meter.sampled_at) lastRead = Math.max(lastRead, toMs(meter.sampled_at));

  const watching = t('overview.watching', {
    meters: t('overview.watching.meters', { n: data.counts.meters }),
    dates: t('overview.watching.dates', { n: data.counts.reminders }),
  });
  const reading = isSampling();
  const read = () => {
    sampleNow().catch((cause: unknown) => toast.error(errorText(cause)));
  };

  // While this page is up, the top of the window takes the verdict's
  // colour (styles/pages/overview.css): the answer is there before a word
  // of it has been read.
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.verdict = tone;
    return () => {
      delete root.dataset.verdict;
    };
  }, [tone]);

  const today = dayOf(now.value);

  return (
    <header class={cx('ov-verdict', `ov-verdict--${tone}`)}>
      <p class="ov-verdict__date">
        <time dateTime={today}>{formatDay(today, { weekday: true })}</time>
      </p>
      <span class="ov-verdict__mark">
        <Icon size={22} strokeWidth={2} />
      </span>
      <h1 class="ov-verdict__title">{title}</h1>
      <p class="ov-verdict__sub dotted">
        {note && <span class="ov-verdict__note">{note}</span>}
        <span>{watching}</span>
        {lastRead > 0 ? (
          <time dateTime={new Date(lastRead).toISOString()} title={formatDateTime(lastRead)}>
            {t('overview.lastRead', { ago: formatAgo(lastRead) })}
          </time>
        ) : (
          <span>{t('overview.neverRead')}</span>
        )}
      </p>
      {data.counts.accounts > 0 && (
        <Button class="ov-verdict__read" icon={RefreshCw} compact busy={reading} onClick={read}>
          {reading ? t('common.action.reading') : t('common.action.readNow')}
        </Button>
      )}
    </header>
  );
}

/** The month in one line: spent, heading for, and what is due. */
function Month({ data }: { data: Overview }) {
  const { spend } = data;
  const figures: { label: string; hint: string; value: number }[] = [
    { label: t('overview.month.spent'), hint: t('overview.month.spent.hint'), value: spend.total },
    { label: t('overview.month.projected'), hint: t('overview.month.projected.hint'), value: spend.projected },
    { label: t('overview.month.fixed'), hint: t('overview.month.fixed.hint'), value: spend.fixed },
  ];
  return (
    <Section
      title={t('overview.month.title')}
      meta={formatMonth(spend.month)}
      action={
        <Link class="more-link" href="/spend">
          {t('overview.month.all')}
          <ArrowRight size={14} />
        </Link>
      }
    >
      <Card class="ov-month">
        <dl class="ov-month__figures">
          {figures.map((figure) => (
            <div key={figure.label} class="ov-month__figure">
              <dt>
                {figure.label}
                <InfoTip label={figure.label}>{figure.hint}</InfoTip>
              </dt>
              <dd>
                <Amount class="ov-month__value" value={figure.value} unit={spend.base} />
              </dd>
            </div>
          ))}
        </dl>
        <p class="ov-month__note">
          {t('overview.month.base', { base: spend.base })}
          {spend.unconverted.length > 0 && <> {t('overview.month.unconverted', { list: spend.unconverted.join(t('common.list.separator')) })}</>}
        </p>
      </Card>
    </Section>
  );
}

/** An install with nothing in it: say what to do first. */
function FirstSteps() {
  const steps: { icon: LucideIcon; title: string; body: string; href: string }[] = [
    { icon: Plug, title: t('overview.start.account.title'), body: t('overview.start.account.body'), href: '/meters?add=1' },
    { icon: BellPlus, title: t('overview.start.channel.title'), body: t('overview.start.channel.body'), href: '/settings/channels' },
    { icon: CalendarPlus, title: t('overview.start.date.title'), body: t('overview.start.date.body'), href: '/dates' },
  ];
  return (
    <div class="ov ov-start">
      <header class="ov-start__head">
        <h1 class="ov-verdict__title">{t('overview.start.title')}</h1>
        <p class="ov-start__body">{t('overview.start.body')}</p>
      </header>
      <ol class="ov-start__steps">
        {steps.map((step, index) => (
          <li key={step.href}>
            <Link class="ov-step" href={step.href}>
              <span class="ov-step__number num">{index + 1}</span>
              <step.icon class="ov-step__icon" size={20} />
              <span class="ov-step__title">{step.title}</span>
              <span class="ov-step__body">{step.body}</span>
              <ArrowRight class="ov-step__arrow" size={16} />
            </Link>
          </li>
        ))}
      </ol>
    </div>
  );
}

function Loading() {
  return (
    <div class="ov" aria-busy="true">
      <span class="sr-only">{t('common.state.loading')}</span>
      <Skeleton width="320px" height="44px" />
      <Skeleton class="ov-loading__gap" width="260px" height="16px" />
      <div class="meter-grid ov-loading__grid">
        {[0, 1, 2, 3].map((index) => (
          <Skeleton key={index} height="148px" />
        ))}
      </div>
    </div>
  );
}

// Now: is anything asking for you, and what is ahead. One request (GET
// /api/v1/overview) feeds the whole page; ./horizon.ts decides what goes
// where, ./parts.tsx draws it.
//
//   the verdict      one sentence, and the next thing that happens
//   needs you        open problems, each with what can be done about it
//   ahead            today, tomorrow, within a week, a month, later
//   steady           everything with nothing ahead, in one line
//   at the side      the month, and how long every balance lasts
import { ArrowRight, BellPlus, CalendarPlus, Check, History, Plug, RefreshCw, type LucideIcon } from 'lucide-preact';
import { useEffect } from 'preact/hooks';
import type { Overview } from '../../api/types';
import { Banner, Button, cx, ErrorState, LinkButton, Skeleton, toast } from '../../components';
import { errorText } from '../../errors';
import { dayOf, formatAgo, formatDateTime, formatDay, toMs } from '../../format';
import { useTitle } from '../../hooks';
import { t } from '../../i18n';
import { Link } from '../../router';
import type { PageProps } from '../../routes';
import { now, timezone } from '../../state/clock';
import { overview } from '../../state/overview';
import { providers } from '../../state/providers';
import { useResource } from '../../state/resource';
import { isSampling, sampleNow } from '../../state/sampling';
import { settings } from '../../state/settings';
import { buildHorizon, verdictOf, type Horizon } from './horizon';
import { AgendaRow, eventTitle, Library, MonthCard, Problem, RunwayCard, Steady, whenText } from './parts';

export default function NowPage(_props: PageProps) {
  useTitle(t('common.nav.now'));
  useResource(overview);
  useResource(settings);
  useResource(providers);
  const data = overview.data.value;

  if (!data) {
    if (overview.error.value) return <ErrorState error={overview.error.value} onRetry={() => void overview.load()} />;
    return <Loading />;
  }

  const { counts } = data;
  // Until something is watched, the first steps stand in for the page; a
  // channel added before anything to watch is ticked off among them.
  if (counts.accounts + counts.meters + counts.reminders === 0) return <FirstSteps channels={counts.channels} />;

  const horizonDays = settings.data.value?.upcoming_days ?? 60;
  const horizon = buildHorizon(data, now.value, timezone.value, horizonDays);

  return (
    <div class="now">
      <div class="now__main">
        <Verdict data={data} horizon={horizon} horizonDays={horizonDays} />

        {counts.channels === 0 && (
          <Banner
            tone="warning"
            class="now__hint"
            action={
              <LinkButton size="sm" href="/settings/channels">
                {t('overview.noChannel.action')}
              </LinkButton>
            }
          >
            {t('overview.noChannel')}
          </Banner>
        )}

        {horizon.now.length > 0 && (
          <section class="now-group" aria-labelledby="now-problems">
            <h2 class="now-group__title" id="now-problems">
              {t('overview.attention.title')}
            </h2>
            <ul class="problems">
              {horizon.now.map((incident) => (
                <Problem key={incident.id} incident={incident} data={data} />
              ))}
            </ul>
          </section>
        )}

        {horizon.groups.map((group) => (
          <section key={group.bucket} class="now-group" aria-labelledby={`now-${group.bucket}`}>
            <h2 class="now-group__title" id={`now-${group.bucket}`}>
              {t(`overview.bucket.${group.bucket}`)}
            </h2>
            <ul class="agenda">
              {group.events.map((event) => (
                <AgendaRow key={event.key} event={event} />
              ))}
            </ul>
          </section>
        ))}

        <Steady meters={horizon.steady} />

        {/* A phone has no bar of places to go: home is where they are
            reached from. A wider screen has them in its top bar, and the
            month at the side. */}
        <div class="now__library">
          <Library data={data} />
          {(counts.meters > 0 || data.spend.fixed > 0) && <MonthCard spend={data.spend} />}
        </div>

        <Link class="now__history" href="/activity">
          <History size={16} />
          <span>
            <span class="now__history-title">{t('overview.history')}</span>
            <span class="now__history-hint">{t('overview.history.hint')}</span>
          </span>
          <ArrowRight size={16} />
        </Link>
      </div>

      <aside class="now__aside" aria-label={t('overview.month.title')}>
        {(counts.meters > 0 || data.spend.fixed > 0) && <MonthCard spend={data.spend} />}
        <RunwayCard meters={data.meters} horizon={horizonDays} />
      </aside>
    </div>
  );
}

/** The sentence at the top: fine or not, what happens next, and how fresh
 *  that answer is. */
function Verdict({ data, horizon, horizonDays }: { data: Overview; horizon: Horizon; horizonDays: number }) {
  const verdict = verdictOf(horizon.now);

  let title = t('overview.verdict.clear');
  let note: string | null = null;
  if (verdict.fresh > 0) {
    title = t('overview.verdict.attention', { n: verdict.fresh });
    if (verdict.known > 0) note = t('overview.verdict.plusKnown', { n: verdict.known });
  } else if (verdict.known > 0) {
    title = t('overview.verdict.known');
    note = t('overview.verdict.known.note', { n: verdict.known });
  }
  const next = horizon.next ? t('overview.next', { what: eventTitle(horizon.next), when: whenText(horizon.next) }) : t('overview.next.none', { n: horizonDays });

  // While this page is up, the top of the window takes the verdict's colour
  // (styles/pages/now.css): the answer is there before a word of it is read.
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.verdict = verdict.tone;
    return () => {
      delete root.dataset.verdict;
    };
  }, [verdict.tone]);

  // "Last read" is the most recent time anything was sampled or reported.
  let lastRead = 0;
  for (const account of data.accounts) if (account.sampled_at) lastRead = Math.max(lastRead, toMs(account.sampled_at));
  for (const meter of data.meters) if (meter.sampled_at) lastRead = Math.max(lastRead, toMs(meter.sampled_at));
  const watching = t('overview.watching', {
    meters: t('overview.watching.meters', { n: data.counts.meters }),
    dates: t('overview.watching.dates', { n: data.counts.reminders }),
  });
  const reading = isSampling();
  const today = dayOf(now.value);

  return (
    <header class={cx('verdict', `verdict--${verdict.tone}`)}>
      <p class="verdict__date">
        <time dateTime={today}>{formatDay(today, { weekday: true })}</time>
      </p>
      <h1 class="verdict__title">
        <span class="verdict__pulse" aria-hidden="true" />
        {title}
      </h1>
      <p class="verdict__next">
        {note && <span class="verdict__note">{note} · </span>}
        {next}
      </p>
      <p class="verdict__meta dotted">
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
        <Button
          class="verdict__read"
          icon={RefreshCw}
          compact
          busy={reading}
          onClick={() => {
            sampleNow().catch((cause: unknown) => toast.error(errorText(cause)));
          }}
        >
          {reading ? t('common.action.reading') : t('common.action.readNow')}
        </Button>
      )}
    </header>
  );
}

/** An install with nothing watched yet: say what to do first, and tick off
 *  what is done. */
function FirstSteps({ channels }: { channels: number }) {
  const steps: { icon: LucideIcon; title: string; body: string; href: string; done: boolean }[] = [
    { icon: Plug, title: t('overview.start.account.title'), body: t('overview.start.account.body'), href: '/meters?add=1', done: false },
    { icon: BellPlus, title: t('overview.start.channel.title'), body: t('overview.start.channel.body'), href: '/settings/channels', done: channels > 0 },
    { icon: CalendarPlus, title: t('overview.start.date.title'), body: t('overview.start.date.body'), href: '/dates?add=1', done: false },
  ];
  return (
    <div class="now now--start">
      <header class="now-start__head">
        <h1 class="now-start__title">{t('overview.start.title')}</h1>
        <p class="now-start__body">{t('overview.start.body')}</p>
      </header>
      <ol class="now-start__steps">
        {steps.map((step, index) => (
          <li key={step.href}>
            <Link class={cx('now-step', step.done && 'is-done')} href={step.href}>
              {step.done ? (
                <span class="now-step__number now-step__done">
                  <Check size={14} />
                  {t('overview.start.done')}
                </span>
              ) : (
                <span class="now-step__number num">{index + 1}</span>
              )}
              <step.icon class="now-step__icon" size={20} />
              <span class="now-step__title">{step.title}</span>
              <span class="now-step__body">{step.body}</span>
              <ArrowRight class="now-step__arrow" size={16} />
            </Link>
          </li>
        ))}
      </ol>
    </div>
  );
}

function Loading() {
  return (
    <div class="now" aria-busy="true">
      <span class="sr-only">{t('common.state.loading')}</span>
      <div class="now__main">
        <Skeleton width="140px" height="16px" />
        <Skeleton class="now-loading__title" width="min(100%, 360px)" height="40px" />
        <Skeleton width="min(100%, 280px)" height="18px" />
        <Skeleton class="now-loading__list" height="220px" />
      </div>
    </div>
  );
}

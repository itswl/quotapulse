// Where a rule lands, asked of the server while the form is being filled in
// (POST /reminders/preview, which saves nothing). This is what tells someone
// they entered the day they meant — the Gregorian day a lunar birthday falls
// on this time, and the ones after it — so it is the loudest thing in the
// form. A rule that is plainly unfinished — no day chosen yet — is not sent:
// the panel says what is still missing, in the reader's language. One the
// server refuses for another reason shows the server's own reason.
import { CalendarCheck, CalendarClock, CalendarX2 } from 'lucide-preact';
import { useEffect, useState } from 'preact/hooks';
import { ApiError } from '../../api/client';
import { previewReminder } from '../../api/endpoints';
import type { RecurRule, ReminderKind, ReminderView } from '../../api/types';
import { cx, Skeleton } from '../../components';
import { errorText } from '../../errors';
import { dayOf, formatDay, formatUntil, parseDay } from '../../format';
import { useQuery, type Query } from '../../hooks';
import { lang, t } from '../../i18n';
import { now } from '../../state/clock';
import type { LiveTopic } from '../../state/live';
import { formatLongDay, lunarMonthName, ordinalText } from './words';

/** "Today" is the server's, in the zone set under Settings. */
const TOPICS: readonly LiveTopic[] = ['settings'];
/** How long the rule has to stand still before the server is asked. */
const PAUSE = 250;

export interface RulePreview {
  query: Query<ReminderView>;
  /** What is on screen answers an earlier version of the rule; the answer
   *  for this one is on its way. */
  stale: boolean;
  /** Where the rule lands as it stands now, once the server has said. */
  view: ReminderView | undefined;
}

/** The code of an answer that never went to the server: the rule is unfinished. */
const UNFINISHED = 'unfinished';

/** What a rule still needs before it can land anywhere, or undefined when
 *  nothing obvious is missing. The server remains the judge of the rest. */
export function missingOf(rule: RecurRule): string | undefined {
  if (rule.freq === 'once') return rule.date ? undefined : t('dates.preview.missing.date');
  if ((rule.interval ?? 1) > 1 && !rule.date) return t('dates.preview.missing.start');
  if (rule.freq === 'weekly') return rule.weekday ? undefined : t('dates.preview.missing.weekday');
  if (rule.freq === 'monthly') return rule.day ? undefined : t('dates.preview.missing.day');
  if (rule.freq === 'yearly') return rule.month && rule.day ? undefined : t('dates.preview.missing.monthDay');
  return undefined;
}

/** Asks where a rule lands, again each time it changes and then pauses. */
export function useRulePreview(rule: RecurRule): RulePreview {
  const wanted = JSON.stringify(rule);
  const [key, setKey] = useState(wanted);
  useEffect(() => {
    if (key === wanted) return;
    const timer = setTimeout(() => setKey(wanted), PAUSE);
    return () => clearTimeout(timer);
  }, [wanted, key]);
  const query = useQuery(
    key,
    (signal) => {
      const asked = JSON.parse(key) as RecurRule;
      const missing = missingOf(asked);
      // Nothing to ask yet, and no refused request left in the console.
      if (missing !== undefined) return Promise.reject(new ApiError(400, UNFINISHED, missing));
      return previewReminder(asked, signal);
    },
    TOPICS,
  );
  const stale = key !== wanted || !query.settled;
  return { query, stale, view: stale || query.error ? undefined : query.data };
}

/** What is worth saying about a landing that is not the day that was asked
 *  for: a month too short for it, and 腊月三十, which is New Year's Eve even
 *  in a year whose 腊月 has 29 days. */
function landingNotes(view: ReminderView): string[] {
  const { rule, next, lunar } = view;
  const notes: string[] = [];
  if (!next || !rule.day || rule.freq === 'once' || rule.freq === 'weekly') return notes;
  if (rule.calendar === 'lunar') {
    if (lunar && lunar.day < rule.day) {
      const month = (lunar.leap && lang.value === 'zh' ? '闰' : '') + lunarMonthName(lunar.month);
      notes.push(t('dates.preview.clampedLunar', { month, n: lunar.day }));
    }
    if (rule.freq === 'yearly' && rule.month === 12 && rule.day === 30) notes.push(t('dates.preview.eve'));
  } else if (parseDay(next).day < rule.day) {
    notes.push(t('dates.preview.clamped'));
  }
  return notes;
}

export function PreviewPanel({ preview, kind }: { preview: RulePreview; kind: ReminderKind }) {
  const { query, stale } = preview;
  const view = query.data;
  const busy = stale || undefined;

  // The server's verdict on the rule comes first: its sentence names exactly
  // what is missing.
  if (query.error) {
    const unfinished = query.error.code === UNFINISHED;
    const invalid = unfinished || query.error.code === 'invalid';
    return (
      <div class={cx('dates-preview', 'dates-preview--pending', stale && 'is-stale')} role="status" aria-busy={busy}>
        <CalendarClock class="dates-preview__icon" size={18} />
        <div class="dates-preview__body">
          <p class="dates-preview__label">{invalid ? t('dates.preview.invalid') : t('dates.preview.failed')}</p>
          <p class="dates-preview__problem">{unfinished ? query.error.message : errorText(query.error)}</p>
        </div>
      </div>
    );
  }

  if (!view) {
    return (
      <div class="dates-preview dates-preview--pending" role="status" aria-busy="true">
        <CalendarClock class="dates-preview__icon" size={18} />
        <div class="dates-preview__body">
          <span class="sr-only">{t('common.state.loading')}</span>
          <Skeleton width="56px" height="12px" />
          <Skeleton class="dates-preview__wait" width="220px" height="18px" />
          <Skeleton class="dates-preview__wait" width="160px" height="12px" />
        </div>
      </div>
    );
  }

  if (!view.next || view.days === null) {
    return (
      <div class={cx('dates-preview', 'dates-preview--warning', stale && 'is-stale')} role="status" aria-busy={busy}>
        <CalendarX2 class="dates-preview__icon" size={18} />
        <div class="dates-preview__body">
          <p class="dates-preview__label">{t('dates.preview.label')}</p>
          <p class="dates-preview__problem">{t('dates.preview.past')}</p>
        </div>
      </div>
    );
  }

  const thisYear = parseDay(dayOf(now.value)).year;
  const facts: string[] = [];
  if (view.lunar) facts.push(t('common.date.lunar', { label: view.lunar.label }));
  facts.push(formatUntil(view.days));
  const years = ordinalText(kind, view.ordinal);
  if (years) facts.push(years);
  const notes = landingNotes(view);

  return (
    <div class={cx('dates-preview', stale && 'is-stale')} role="status" aria-busy={busy}>
      <CalendarCheck class="dates-preview__icon" size={18} />
      <div class="dates-preview__body">
        <p class="dates-preview__label">{t('dates.preview.label')}</p>
        <p class="dates-preview__date">
          <time dateTime={view.next}>{formatLongDay(view.next)}</time>
        </p>
        <p class="dates-preview__facts">
          {facts.map((fact) => (
            <span key={fact}>{fact}</span>
          ))}
        </p>
        {view.following.length > 0 ? (
          <p class="dates-preview__then">
            <span class="dates-preview__then-label">{t('dates.preview.then')}</span>
            {view.following.map((day) => (
              <time key={day} dateTime={day}>
                {formatDay(day, { year: thisYear })}
              </time>
            ))}
          </p>
        ) : (
          view.rule.freq === 'once' && <p class="dates-preview__then">{t('dates.preview.once')}</p>
        )}
        {notes.length > 0 && <p class="dates-preview__note">{notes.join(lang.value === 'zh' ? '' : ' ')}</p>}
      </div>
    </div>
  );
}

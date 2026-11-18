import type { Day } from '../api/types';
import { formatDay, monthName, parseDay, weekdayName, weekdayOf } from '../format';
import { lang } from '../i18n';
import { cx } from './cx';

/** A calendar day as a block at the start of a row: the date large, the
 *  month and the weekday small under it. Every list of dates leads with one,
 *  so their days line up wherever they are. */
export function DayBlock({ day, class: extra }: { day: Day; class?: string }) {
  const parts = parseDay(day);
  return (
    <time class={cx('dayblock', extra)} dateTime={day} title={formatDay(day, { weekday: true, year: -1 })}>
      <span class="dayblock__day num">{parts.day}</span>
      <span class="dayblock__sub">
        {monthName(parts.month)}
        {lang.value === 'zh' ? ' ' : ' · '}
        {weekdayName(weekdayOf(day))}
      </span>
    </time>
  );
}

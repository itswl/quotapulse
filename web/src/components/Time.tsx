import type { Instant } from '../api/types';
import { formatAgo, formatDateTime } from '../format';

/** A moment as "5 min ago", with the exact time on hover and for machines. */
export function Ago({ at, class: extra }: { at: Instant; class?: string }) {
  return (
    <time class={extra} dateTime={at} title={formatDateTime(at)}>
      {formatAgo(at)}
    </time>
  );
}

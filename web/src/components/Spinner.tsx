import { LoaderCircle } from 'lucide-preact';
import { cx } from './cx';

/** A turning circle for "this is in progress". Decorative: say what is
 *  happening in words next to it, or with aria-busy on the control. */
export function Spinner({ size = 16, class: extra }: { size?: number; class?: string }) {
  return <LoaderCircle size={size} class={cx('spinner', extra)} />;
}

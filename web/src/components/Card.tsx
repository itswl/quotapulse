import type { ComponentChildren, HTMLAttributes } from 'preact';
import { cx } from './cx';

export interface CardProps extends Omit<HTMLAttributes<HTMLDivElement>, 'class'> {
  /** Leave out the padding, for a card whose content runs edge to edge: a
   *  list with dividers, a table. */
  flush?: boolean;
  class?: string;
  children?: ComponentChildren;
}

/** A surface: the card's white on the page's paper, a hairline border, and
 *  the faintest lift (--shadow-card). Real shadows are for things that
 *  float over the page. */
export function Card({ flush, class: extra, children, ...rest }: CardProps) {
  return (
    <div {...rest} class={cx('card', flush && 'card--flush', extra)}>
      {children}
    </div>
  );
}

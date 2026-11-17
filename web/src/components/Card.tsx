import type { ComponentChildren, JSX } from 'preact';
import { cx } from './cx';

export interface CardProps extends Omit<JSX.HTMLAttributes<HTMLDivElement>, 'class'> {
  /** Leave out the padding, for a card whose content runs edge to edge: a
   *  list with dividers, a table. */
  flush?: boolean;
  class?: string;
  children?: ComponentChildren;
}

/** A surface with a hairline border. Borders do the separating here; a card
 *  has no shadow. */
export function Card({ flush, class: extra, children, ...rest }: CardProps) {
  return (
    <div {...rest} class={cx('card', flush && 'card--flush', extra)}>
      {children}
    </div>
  );
}

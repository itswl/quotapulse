import type { ComponentChildren } from 'preact';
import { useId } from 'preact/hooks';
import { cx } from './cx';

export interface SectionProps {
  title: string;
  /** A quiet note after the title: a count, a period. */
  meta?: ComponentChildren;
  /** Sits at the right of the heading: usually a link to the full page. */
  action?: ComponentChildren;
  class?: string;
  children: ComponentChildren;
}

/** A titled part of a page. */
export function Section({ title, meta, action, class: extra, children }: SectionProps) {
  const id = useId();
  return (
    <section class={cx('section', extra)} aria-labelledby={id}>
      <header class="section__head">
        <h2 class="section__title" id={id}>
          {title}
        </h2>
        {meta != null && <span class="section__meta">{meta}</span>}
        {action != null && <span class="section__action">{action}</span>}
      </header>
      {children}
    </section>
  );
}

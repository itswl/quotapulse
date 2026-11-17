import { ChevronLeft } from 'lucide-preact';
import type { ComponentChildren } from 'preact';
import { Link } from '../router';

export interface PageHeaderProps {
  title: ComponentChildren;
  /** A quiet line under the title. */
  subtitle?: ComponentChildren;
  /** Where "up" is, for a page beneath another: a link back to the list. */
  back?: { href: string; label: string };
  /** Buttons at the right; the main action goes last. */
  actions?: ComponentChildren;
}

/** The top of a page: its name, and the things one can do on it. Every page
 *  has exactly one, and it holds the page's only <h1>. */
export function PageHeader({ title, subtitle, back, actions }: PageHeaderProps) {
  return (
    <header class="page-head">
      <div class="page-head__titles">
        {back && (
          <Link class="page-head__back" href={back.href}>
            <ChevronLeft size={14} />
            {back.label}
          </Link>
        )}
        <h1 class="page-head__title">{title}</h1>
        {subtitle != null && <p class="page-head__subtitle">{subtitle}</p>}
      </div>
      {actions != null && <div class="page-head__actions">{actions}</div>}
    </header>
  );
}

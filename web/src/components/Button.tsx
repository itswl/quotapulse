import type { LucideIcon } from 'lucide-preact';
import type { ButtonHTMLAttributes, ComponentChildren } from 'preact';
import { Link } from '../router';
import { cx } from './cx';
import { Spinner } from './Spinner';

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger';
export type ButtonSize = 'sm' | 'md';

interface Look {
  /** primary: the one main action of a view. secondary: everything else.
   *  ghost: quiet, for actions inside rows. danger: destroys something. */
  variant?: ButtonVariant;
  size?: ButtonSize;
  icon?: LucideIcon;
  /** Stretch to the container's width. */
  block?: boolean;
  /** On a phone, show the icon alone. The label is still the button's name,
   *  so this is only for an icon that says it all: a refresh, a plus. */
  compact?: boolean;
  class?: string;
  children?: ComponentChildren;
}

function classes({ variant = 'secondary', size = 'md', block, compact, class: extra }: Look): string {
  return cx('btn', `btn--${variant}`, size === 'sm' && 'btn--sm', block && 'btn--block', compact && 'btn--compact', extra);
}

export interface ButtonProps extends Look, Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'size' | 'icon' | 'class' | 'type' | 'disabled' | 'form'> {
  /** A request is in flight: shows a spinner and refuses clicks. */
  busy?: boolean;
  type?: 'button' | 'submit' | 'reset';
  disabled?: boolean;
  form?: string;
}

export function Button({ variant, size, icon: Icon, block, compact, class: extra, busy, disabled, type = 'button', children, ...rest }: ButtonProps) {
  return (
    <button
      {...rest}
      type={type}
      class={classes({ variant, size, block, compact, class: extra })}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
    >
      {busy ? <Spinner /> : Icon ? <Icon /> : null}
      {children != null && <span class="btn__label">{children}</span>}
    </button>
  );
}

export interface LinkButtonProps extends Look {
  href: string;
  /** Opens another site in a new tab. */
  external?: boolean;
  'aria-label'?: string;
}

/** A link that looks like a button, for actions that are really navigation. */
export function LinkButton({ variant, size, icon: Icon, block, compact, class: extra, href, external, children, ...rest }: LinkButtonProps) {
  const body = (
    <>
      {Icon ? <Icon /> : null}
      {children != null && <span class="btn__label">{children}</span>}
    </>
  );
  const className = classes({ variant, size, block, compact, class: extra });
  if (external) {
    return (
      <a {...rest} class={className} href={href} target="_blank" rel="noopener noreferrer">
        {body}
      </a>
    );
  }
  return (
    <Link {...rest} class={className} href={href}>
      {body}
    </Link>
  );
}

export interface IconButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'size' | 'icon' | 'label' | 'type' | 'disabled'> {
  icon: LucideIcon;
  /** What the button does. It is the accessible name and the hover hint. */
  label: string;
  size?: ButtonSize;
  busy?: boolean;
  disabled?: boolean;
  type?: 'button' | 'submit';
  /** Draws a border, for a button that stands alone rather than in a row. */
  outlined?: boolean;
}

/** A button that is only an icon. The label is required: an icon is not a name. */
export function IconButton({ icon: Icon, label, size = 'md', busy, disabled, outlined, class: extra, type = 'button', ...rest }: IconButtonProps) {
  return (
    <button
      title={label}
      {...rest}
      type={type}
      class={cx('icon-btn', size === 'sm' && 'icon-btn--sm', outlined && 'icon-btn--outlined', typeof extra === 'string' && extra)}
      aria-label={label}
      aria-busy={busy || undefined}
      disabled={disabled || busy}
    >
      {busy ? <Spinner /> : <Icon />}
    </button>
  );
}

import { X } from 'lucide-preact';
import type { ComponentChildren } from 'preact';
import { useEffect, useId, useRef } from 'preact/hooks';
import { t } from '../i18n';
import { Button, IconButton, type ButtonVariant } from './Button';
import { cx } from './cx';

export interface DialogProps {
  open: boolean;
  /** Asked to close: the close button, Escape, or a click on the backdrop. */
  onClose: () => void;
  title: string;
  /** A sentence under the title. */
  description?: ComponentChildren;
  size?: 'sm' | 'md' | 'lg';
  /** While true the dialog cannot be dismissed: a request is in flight. */
  busy?: boolean;
  /** Buttons at the bottom, right-aligned; the main action goes last. */
  footer?: ComponentChildren;
  class?: string;
  children?: ComponentChildren;
}

/**
 * A modal dialog on the native <dialog>, which brings the hard parts with
 * it: focus is trapped inside, Escape closes, the page behind is inert, and
 * focus returns to where it was. The content is only mounted while open, so
 * a form inside starts fresh each time.
 */
export function Dialog({ open, onClose, title, description, size = 'md', busy, footer, class: extra, children }: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const descriptionId = useId();

  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    else if (!open && dialog.open) dialog.close();
  }, [open]);

  return (
    <dialog
      ref={ref}
      class={cx('dialog', `dialog--${size}`, extra)}
      aria-labelledby={titleId}
      aria-describedby={description != null ? descriptionId : undefined}
      onCancel={(event) => {
        // Escape. The dialog closes when the owner says so, not by itself.
        event.preventDefault();
        if (!busy) onClose();
      }}
      onClose={() => {
        if (open) onClose();
      }}
      onMouseDown={(event) => {
        // The backdrop is the dialog element itself; its content is inside .dialog__box.
        if (event.target === ref.current && !busy) onClose();
      }}
    >
      {open && (
        <div class="dialog__box">
          <header class="dialog__head">
            <div class="dialog__titles">
              <h2 class="dialog__title" id={titleId}>
                {title}
              </h2>
              {description != null && (
                <p class="dialog__description" id={descriptionId}>
                  {description}
                </p>
              )}
            </div>
            <IconButton icon={X} label={t('common.action.close')} onClick={onClose} disabled={busy} />
          </header>
          <div class="dialog__body">{children}</div>
          {footer != null && <footer class="dialog__foot">{footer}</footer>}
        </div>
      )}
    </dialog>
  );
}

export interface ConfirmDialogProps {
  open: boolean;
  onClose: () => void;
  title: string;
  /** What will happen, in a sentence or two. */
  children?: ComponentChildren;
  confirmLabel: string;
  /** Runs the action. The dialog shows progress and closes when the promise
   *  resolves; if it rejects, the dialog stays and shows `error`. */
  onConfirm: () => void | Promise<void>;
  busy?: boolean;
  error?: string | undefined;
  /** destructive actions get the danger button */
  variant?: ButtonVariant;
}

/** Asks before something that cannot be taken back. */
export function ConfirmDialog({ open, onClose, title, children, confirmLabel, onConfirm, busy, error, variant = 'danger' }: ConfirmDialogProps) {
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={title}
      size="sm"
      busy={busy ?? false}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>
            {t('common.action.cancel')}
          </Button>
          <Button variant={variant} busy={busy} onClick={() => void onConfirm()}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      <div class="confirm">
        {children}
        {error && (
          <p class="form-error" role="alert">
            {error}
          </p>
        )}
      </div>
    </Dialog>
  );
}

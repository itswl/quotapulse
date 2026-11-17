import { signal } from '@preact/signals';
import { CircleCheck, CircleX, Info, X } from 'lucide-preact';
import { t } from '../i18n';
import { cx } from './cx';

export interface ToastOptions {
  tone?: 'ok' | 'critical' | 'info';
  /** One thing the user can do about it, usually Undo. */
  action?: { label: string; run: () => void };
  /** How long it stays, in milliseconds. */
  duration?: number;
}

interface ToastItem extends ToastOptions {
  id: number;
  message: string;
}

const items = signal<ToastItem[]>([]);
let nextId = 1;

function dismiss(id: number): void {
  items.value = items.value.filter((item) => item.id !== id);
}

/**
 * Says briefly that something happened: `toast('Saved')`. For something that
 * went wrong, `toast.error(message)`. A toast confirms; it must not be the
 * only way to learn something that matters.
 */
export function toast(message: string, options: ToastOptions = {}): void {
  const id = nextId++;
  const tone = options.tone ?? 'ok';
  items.value = [...items.value.slice(-2), { ...options, tone, id, message }];
  const duration = options.duration ?? (tone === 'critical' || options.action ? 8000 : 4000);
  setTimeout(() => dismiss(id), duration);
}

toast.error = (message: string, options: ToastOptions = {}): void => toast(message, { ...options, tone: 'critical' });

const ICONS = { ok: CircleCheck, critical: CircleX, info: Info };

/** Where toasts appear. Mounted once, by the shell. */
export function Toaster() {
  return (
    <div class="toaster">
      {items.value.map((item) => {
        const Icon = ICONS[item.tone ?? 'ok'];
        return (
          <div key={item.id} class={cx('toast', `toast--${item.tone}`)} role={item.tone === 'critical' ? 'alert' : 'status'}>
            <Icon class="toast__icon" />
            <span class="toast__message">{item.message}</span>
            {item.action && (
              <button
                type="button"
                class="toast__action"
                onClick={() => {
                  item.action?.run();
                  dismiss(item.id);
                }}
              >
                {item.action.label}
              </button>
            )}
            <button type="button" class="toast__close" aria-label={t('common.action.dismiss')} onClick={() => dismiss(item.id)}>
              <X size={14} />
            </button>
          </div>
        );
      })}
    </div>
  );
}

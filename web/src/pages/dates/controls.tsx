// Form controls the shared set does not have — a row of choices with icons,
// a date input, a text area — and the label that goes with them. They wear
// the shared controls' classes (field, input, segmented), so they look and
// space themselves like the rest of every form.
import type { LucideIcon } from 'lucide-preact';
import type { ComponentChildren, TargetedKeyboardEvent } from 'preact';
import { useId } from 'preact/hooks';
import { cx } from '../../components';
import { t } from '../../i18n';

/** What a control needs to be tied to its label, help and error. */
export interface ControlIds {
  /** For a single control: the label's `for` points here. */
  id: string;
  /** For a set of controls: names the set after the label. */
  labelledBy: string;
  describedBy: string | undefined;
  invalid: boolean;
}

export interface LabeledProps {
  label: string;
  /** One plain sentence under the control. */
  help?: ComponentChildren;
  /** Shown instead of the help, in the critical colour, and announced. */
  error?: string | undefined;
  /** Marks the label "optional". */
  optional?: boolean;
  /** The label names a set of controls rather than one input. */
  group?: boolean;
  class?: string;
  children: (ids: ControlIds) => ComponentChildren;
}

/** What Field is to the shared controls, for the ones on this page: the same
 *  label, help and error, with the ids handed to whatever is inside. */
export function Labeled({ label, help, error, optional, group, class: extra, children }: LabeledProps) {
  const id = useId();
  const labelId = useId();
  const noteId = useId();
  const describedBy = Boolean(error) || help != null ? noteId : undefined;
  const words = (
    <>
      {label}
      {optional && <span class="field__optional">{t('common.word.optional')}</span>}
    </>
  );
  return (
    <div class={cx('field', extra)} role={group ? 'group' : undefined} aria-labelledby={group ? labelId : undefined} aria-describedby={group ? describedBy : undefined}>
      <div class="field__label-row">
        {group ? (
          <span class="field__label" id={labelId}>
            {words}
          </span>
        ) : (
          <label class="field__label" id={labelId} for={id}>
            {words}
          </label>
        )}
      </div>
      {children({ id, labelledBy: labelId, describedBy, invalid: Boolean(error) })}
      {error ? (
        <p class="field__error" id={noteId} role="alert">
          {error}
        </p>
      ) : help != null ? (
        <p class="field__help" id={noteId}>
          {help}
        </p>
      ) : null}
    </div>
  );
}

export interface Choice<V extends string> {
  value: V;
  label: string;
  icon?: LucideIcon;
  /** A fuller name for assistive technology when the label is terse ("Mon"). */
  name?: string;
}

export interface ChoicesProps<V extends string> {
  /** "" while nothing is chosen yet. */
  value: V | '';
  onChange: (value: V) => void;
  options: readonly Choice<V>[];
  /** The id of the label that names the set. */
  labelledBy: string;
  describedBy?: string | undefined;
  class?: string;
}

/** A few mutually exclusive choices, all visible at once: Segmented, with
 *  room for an icon in each, and for none being chosen yet. Arrow keys move
 *  the choice, as in any radio group. */
export function Choices<V extends string>({ value, onChange, options, labelledBy, describedBy, class: extra }: ChoicesProps<V>) {
  const chosen = options.findIndex((option) => option.value === value);
  const move = (event: TargetedKeyboardEvent<HTMLDivElement>) => {
    const step = event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 0;
    if (step === 0) return;
    event.preventDefault();
    const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="radio"]'));
    const from = chosen >= 0 ? chosen : buttons.indexOf(document.activeElement as HTMLButtonElement);
    const index = (Math.max(from, 0) + step + options.length) % options.length;
    const next = options[index];
    if (!next) return;
    onChange(next.value);
    buttons[index]?.focus();
  };
  return (
    <div class={cx('segmented', extra)} role="radiogroup" aria-labelledby={labelledBy} aria-describedby={describedBy} onKeyDown={move}>
      {options.map((option, index) => {
        const Icon = option.icon;
        const checked = index === chosen;
        return (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={checked}
            aria-label={option.name}
            tabIndex={checked || (chosen < 0 && index === 0) ? 0 : -1}
            class="segmented__option"
            onClick={() => onChange(option.value)}
          >
            {Icon ? <Icon size={18} /> : null}
            <span>{option.label}</span>
          </button>
        );
      })}
    </div>
  );
}

export interface DateInputProps {
  /** "YYYY-MM-DD", or "" while no day is picked. */
  value: string;
  onInput: (value: string) => void;
  id?: string;
  describedBy?: string | undefined;
  invalid?: boolean;
  /** The first and the last day that can be picked. */
  min?: string;
  max?: string;
}

/** A calendar day, picked with the platform's own date picker. A day has no
 *  time zone in it, so the browser's zone cannot move it. */
export function DateInput({ value, onInput, id, describedBy, invalid, min, max }: DateInputProps) {
  return (
    <span class={cx('input', 'dates-date', invalid && 'input--invalid')}>
      <input
        id={id}
        type="date"
        value={value}
        min={min}
        max={max}
        aria-describedby={describedBy}
        aria-invalid={invalid || undefined}
        onInput={(event) => onInput(event.currentTarget.value)}
      />
    </span>
  );
}

export interface TextAreaProps {
  value: string;
  onInput: (value: string) => void;
  id?: string;
  describedBy?: string | undefined;
  rows?: number;
  maxLength?: number;
}

/** A few lines of free text. */
export function TextArea({ value, onInput, id, describedBy, rows = 2, maxLength }: TextAreaProps) {
  return (
    <textarea
      class="dates-textarea"
      id={id}
      rows={rows}
      maxLength={maxLength}
      aria-describedby={describedBy}
      value={value}
      onInput={(event) => onInput(event.currentTarget.value)}
    />
  );
}

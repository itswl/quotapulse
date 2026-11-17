import { createContext, type ComponentChildren, type HTMLAttributes, type TargetedKeyboardEvent } from 'preact';
import { useContext, useId } from 'preact/hooks';
import { t } from '../i18n';
import { cx } from './cx';

interface FieldIds {
  id: string;
  labelId: string;
  describedBy: string | undefined;
  invalid: boolean;
}

const FieldContext = createContext<FieldIds | null>(null);

export interface FieldProps {
  label: string;
  /** One plain sentence under the control saying what it does. */
  help?: ComponentChildren;
  /** Shown instead of the help, in the critical colour, and announced. */
  error?: string | undefined;
  /** Marks the label "optional". */
  optional?: boolean;
  /** Something small beside the label, such as an InfoTip. */
  aside?: ComponentChildren;
  class?: string;
  /** The control: an Input, Select, Segmented or Switch. It picks up its id
   *  and description from here. */
  children: ComponentChildren;
}

/** A labelled control with its help and its error. */
export function Field({ label, help, error, optional, aside, class: extra, children }: FieldProps) {
  const id = useId();
  const labelId = useId();
  const noteId = useId();
  const hasNote = Boolean(error) || help != null;
  return (
    <div class={cx('field', extra)}>
      <div class="field__label-row">
        <label class="field__label" id={labelId} for={id}>
          {label}
          {optional && <span class="field__optional">{t('common.word.optional')}</span>}
        </label>
        {aside}
      </div>
      <FieldContext.Provider value={{ id, labelId, describedBy: hasNote ? noteId : undefined, invalid: Boolean(error) }}>
        {children}
      </FieldContext.Provider>
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

function useField(): Partial<FieldIds> {
  return useContext(FieldContext) ?? {};
}

export interface InputProps extends Omit<HTMLAttributes<HTMLInputElement>, 'onInput' | 'value' | 'class' | 'size' | 'prefix' | 'role' | 'list'> {
  value: string;
  onInput: (value: string) => void;
  type?: 'text' | 'password' | 'search' | 'email' | 'url';
  /** "decimal" brings up the number keyboard without the quirks of type=number. */
  inputMode?: 'text' | 'decimal' | 'numeric' | 'search' | 'url' | 'email';
  placeholder?: string;
  /** A unit or other short text shown inside the control, after the value. */
  suffix?: ComponentChildren;
  /** An icon or short text shown inside the control, before the value. */
  prefix?: ComponentChildren;
  disabled?: boolean;
  required?: boolean;
  autoComplete?: string;
  name?: string;
  /** The id of a <datalist> of suggestions. */
  list?: string;
  maxLength?: number;
  /** Numbers and codes read better in the mono face. */
  mono?: boolean;
  class?: string;
  /** What a text field may also be: the box of a combobox, or a search box. */
  role?: 'textbox' | 'combobox' | 'searchbox';
}

export function Input({ value, onInput, type = 'text', suffix, prefix, mono, class: extra, list, ...rest }: InputProps) {
  const field = useField();
  // Preact types a field with suggestions and one without as two different
  // elements. Which this is is only known when it runs, so the attribute is
  // handed over in the shape of the plain one.
  const suggestions = (list ? { list } : {}) as { list?: undefined };
  return (
    <span class={cx('input', mono && 'input--mono', field.invalid && 'input--invalid', rest.disabled && 'input--disabled', extra)}>
      {prefix != null && <span class="input__affix">{prefix}</span>}
      <input
        id={field.id}
        aria-describedby={field.describedBy}
        aria-invalid={field.invalid || undefined}
        {...rest}
        {...suggestions}
        // Preact types <input> as a union over `type`, each member with the
        // roles that type allows. The five text-like types this component
        // offers all take what a text field takes, so it is checked as one.
        type={type as 'text'}
        value={value}
        onInput={(event) => onInput(event.currentTarget.value)}
      />
      {suffix != null && <span class="input__affix">{suffix}</span>}
    </span>
  );
}

export interface SelectOption<V extends string> {
  value: V;
  label: string;
}

export interface SelectProps<V extends string> {
  value: V;
  onChange: (value: V) => void;
  options: readonly SelectOption<V>[];
  disabled?: boolean;
  'aria-label'?: string;
  class?: string;
}

/** The platform's own select: it is the most accessible one there is. */
export function Select<V extends string>({ value, onChange, options, class: extra, ...rest }: SelectProps<V>) {
  const field = useField();
  return (
    <span class={cx('select', extra)}>
      <select
        id={field.id}
        aria-describedby={field.describedBy}
        {...rest}
        value={value}
        onChange={(event) => onChange(event.currentTarget.value as V)}
      >
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </span>
  );
}

export interface SwitchProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  /** Needed when the switch is not inside a Field. */
  'aria-label'?: string;
}

/** On or off, effective as soon as the surrounding form is saved. */
export function Switch({ checked, onChange, disabled, ...rest }: SwitchProps) {
  const field = useField();
  return (
    <button
      type="button"
      role="switch"
      id={field.id}
      aria-describedby={field.describedBy}
      aria-checked={checked}
      class="switch"
      disabled={disabled}
      onClick={() => onChange(!checked)}
      {...rest}
    >
      <span class="switch__thumb" />
    </button>
  );
}

export interface SegmentedProps<V extends string> {
  value: V;
  onChange: (value: V) => void;
  options: readonly SelectOption<V>[];
  /** Needed when the control is not inside a Field. */
  'aria-label'?: string;
  disabled?: boolean;
}

/** A few mutually exclusive choices, all visible at once. */
export function Segmented<V extends string>({ value, onChange, options, disabled, ...rest }: SegmentedProps<V>) {
  const field = useField();
  const move = (event: TargetedKeyboardEvent<HTMLDivElement>) => {
    const step = event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 0;
    if (step === 0) return;
    event.preventDefault();
    const index = options.findIndex((option) => option.value === value);
    const next = options[(index + step + options.length) % options.length];
    if (!next) return;
    onChange(next.value);
    const buttons = event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="radio"]');
    buttons[(index + step + options.length) % options.length]?.focus();
  };
  return (
    <div
      class="segmented"
      role="radiogroup"
      id={field.id}
      aria-labelledby={field.labelId}
      aria-describedby={field.describedBy}
      onKeyDown={move}
      {...rest}
    >
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          role="radio"
          aria-checked={option.value === value}
          tabIndex={option.value === value ? 0 : -1}
          class="segmented__option"
          disabled={disabled}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

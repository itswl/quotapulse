// Small pieces the Settings pages share with each other and that the shared
// components do not have: a labelled wrapper for controls of our own, a
// checkbox, a text area, a time input, a copy button, rows of facts.
import { Check, Copy, Plus, X } from 'lucide-preact';
import type { ComponentChildren, TargetedEvent } from 'preact';
import { useEffect, useId, useRef, useState } from 'preact/hooks';
import { Button, cx, IconButton } from '../../components';
import { t } from '../../i18n';
import type { Pair } from './logic';

// ---------- a label, a control of our own, its help and its error ----------

export interface ControlIds {
  id: string;
  describedBy: string | undefined;
  invalid: boolean;
}

export interface LabelledProps {
  label: string;
  help?: ComponentChildren;
  error?: string | undefined;
  optional?: boolean;
  class?: string;
  /** Renders the control; give it the id and the description it is handed. */
  children: (ids: ControlIds) => ComponentChildren;
}

/** The shared Field, for a control it does not know: it looks the same and
 *  ties label, help and error to the control the same way. */
export function Labelled({ label, help, error, optional, class: extra, children }: LabelledProps) {
  const id = useId();
  const noteId = useId();
  const hasNote = Boolean(error) || help != null;
  return (
    <div class={cx('field', extra)}>
      <div class="field__label-row">
        <label class="field__label" for={id}>
          {label}
          {optional && <span class="field__optional">{t('common.word.optional')}</span>}
        </label>
      </div>
      {children({ id, describedBy: hasNote ? noteId : undefined, invalid: Boolean(error) })}
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

/** A set of related controls under one caption: the checkboxes of a choice. */
export function Group({ label, help, error, class: extra, children }: { label: string; help?: ComponentChildren; error?: string | undefined; class?: string; children: ComponentChildren }) {
  const noteId = useId();
  return (
    <fieldset class={cx('st-group', extra)} aria-describedby={error || help != null ? noteId : undefined}>
      <legend class="field__label">{label}</legend>
      {children}
      {error ? (
        <p class="field__error" id={noteId} role="alert">
          {error}
        </p>
      ) : help != null ? (
        <p class="field__help" id={noteId}>
          {help}
        </p>
      ) : null}
    </fieldset>
  );
}

export interface CheckProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: ComponentChildren;
  /** A few words under the label saying what ticking it means. */
  help?: ComponentChildren;
  disabled?: boolean;
}

/** A checkbox with its label, and what it means underneath. */
export function CheckBox({ checked, onChange, label, help, disabled }: CheckProps) {
  return (
    <label class={cx('st-check', disabled && 'is-disabled')}>
      <input type="checkbox" checked={checked} disabled={disabled} onChange={(event) => onChange(event.currentTarget.checked)} />
      <span class="st-check__text">
        <span class="st-check__label">{label}</span>
        {help != null && <span class="st-check__help">{help}</span>}
      </span>
    </label>
  );
}

export interface TextAreaProps extends Partial<ControlIds> {
  value: string;
  onInput: (value: string) => void;
  rows?: number;
  placeholder?: string;
  'aria-label'?: string;
  class?: string;
}

/** Several lines of code or text, in the mono face. */
export function TextArea({ value, onInput, rows = 6, id, describedBy, invalid, class: extra, ...rest }: TextAreaProps) {
  return (
    <textarea
      {...rest}
      id={id}
      class={cx('st-textarea', invalid && 'is-invalid', extra)}
      rows={rows}
      value={value}
      aria-describedby={describedBy}
      aria-invalid={invalid || undefined}
      spellcheck={false}
      autoCapitalize="off"
      autoComplete="off"
      onInput={(event) => onInput(event.currentTarget.value)}
    />
  );
}

export interface PickerProps extends Partial<ControlIds> {
  value: string;
  onInput: (value: string) => void;
  disabled?: boolean;
  /** The earliest value the picker offers. */
  min?: string;
  'aria-label'?: string;
}

/** A time of day, HH:MM, with the platform's own picker. */
export function TimeInput(props: PickerProps) {
  return <Picker type="time" {...props} />;
}

/** A calendar day, YYYY-MM-DD, with the platform's own picker. */
export function DateInput(props: PickerProps) {
  return <Picker type="date" {...props} />;
}

function Picker({ type, value, onInput, id, describedBy, invalid, disabled, ...rest }: PickerProps & { type: 'time' | 'date' }) {
  return (
    <span class={cx('input', 'st-picker', invalid && 'input--invalid', disabled && 'input--disabled')}>
      <input
        {...rest}
        id={id}
        type={type}
        value={value}
        disabled={disabled}
        aria-describedby={describedBy}
        aria-invalid={invalid || undefined}
        onInput={(event) => onInput(event.currentTarget.value)}
      />
    </span>
  );
}

// ---------- copying ----------

async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // No clipboard API on plain http; the old way still works there.
  }
  try {
    const area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.opacity = '0';
    // Inside an open dialog only the dialog's own content can take focus.
    (document.querySelector('dialog[open]') ?? document.body).appendChild(area);
    area.select();
    const done = document.execCommand('copy');
    area.remove();
    return done;
  } catch {
    return false;
  }
}

/** Copies a text, and says so for a moment. */
export function CopyButton({ text, label, variant = 'secondary' }: { text: string; label?: string; variant?: 'secondary' | 'primary' | 'ghost' }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const copy = async () => {
    const done = await copyText(text);
    setState(done ? 'copied' : 'failed');
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setState('idle'), 2400);
  };
  return (
    <Button variant={variant} size="sm" icon={state === 'copied' ? Check : Copy} onClick={() => void copy()}>
      <span aria-live="polite">{state === 'copied' ? t('settings.copy.done') : state === 'failed' ? t('settings.copy.failed') : (label ?? t('settings.copy'))}</span>
    </Button>
  );
}

/** Something to paste somewhere else: a title, the text, a copy button. */
export function Snippet({ title, text, note }: { title: string; text: string; note?: ComponentChildren }) {
  return (
    <figure class="st-snippet">
      <figcaption class="st-snippet__head">
        <span class="st-snippet__title">{title}</span>
        <CopyButton text={text} />
      </figcaption>
      {note != null && <p class="st-snippet__note">{note}</p>}
      <pre class="st-code" tabIndex={0}>
        {text}
      </pre>
    </figure>
  );
}

// ---------- a titled block: what it is on one side, the thing itself on the other ----------

/** A part of a settings page: its name and a sentence about it beside the
 *  card that holds it — above it, where there is no room for beside. */
export function Block({ title, about, children }: { title: string; about?: ComponentChildren; children: ComponentChildren }) {
  const id = useId();
  return (
    <section class="st-block" aria-labelledby={id}>
      <header class="st-block__head">
        <h2 class="st-block__title" id={id}>
          {title}
        </h2>
        {about != null && <p class="st-block__about">{about}</p>}
      </header>
      <div class="st-block__body">{children}</div>
    </section>
  );
}

// ---------- facts in rows ----------

/** A list of facts, label on the left and value on the right. */
export function Facts({ children, class: extra }: { children: ComponentChildren; class?: string }) {
  return <dl class={cx('st-facts', extra)}>{children}</dl>;
}

export function Fact({ label, children }: { label: ComponentChildren; children: ComponentChildren }) {
  return (
    <div class="st-facts__row">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

// ---------- key and value rows ----------

export interface PairRowsProps {
  /** Names the set for assistive technology: "Headers". */
  label: string;
  rows: readonly Pair[];
  onChange: (rows: Pair[]) => void;
  keyPlaceholder: string;
  valuePlaceholder: string;
  addLabel: string;
}

/** Headers, query parameters: any number of name and value pairs. */
export function PairRows({ label, rows, onChange, keyPlaceholder, valuePlaceholder, addLabel }: PairRowsProps) {
  const set = (index: number, part: keyof Pair) => (event: TargetedEvent<HTMLInputElement>) => {
    const value = event.currentTarget.value;
    onChange(rows.map((row, at) => (at === index ? { ...row, [part]: value } : row)));
  };
  return (
    <div class="st-pairs" role="group" aria-label={label}>
      {rows.map((row, index) => (
        <div key={index} class="st-pairs__row">
          <span class="input input--mono">
            <input value={row.key} placeholder={keyPlaceholder} aria-label={`${label} ${index + 1}: ${keyPlaceholder}`} spellcheck={false} autoCapitalize="off" autoComplete="off" onInput={set(index, 'key')} />
          </span>
          <span class="input input--mono">
            <input value={row.value} placeholder={valuePlaceholder} aria-label={`${label} ${index + 1}: ${valuePlaceholder}`} spellcheck={false} autoCapitalize="off" autoComplete="off" onInput={set(index, 'value')} />
          </span>
          <IconButton icon={X} label={t('settings.row.remove')} onClick={() => onChange(rows.filter((_, at) => at !== index))} />
        </div>
      ))}
      <Button variant="ghost" size="sm" icon={Plus} class="st-pairs__add" onClick={() => onChange([...rows, { key: '', value: '' }])}>
        {addLabel}
      </Button>
    </div>
  );
}

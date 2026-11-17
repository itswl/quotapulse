// Picking a time zone out of four hundred: type a few letters, pick from
// what is left. Used inside a Field, whose label and help it picks up
// through the Input it is built on.
import { ChevronsUpDown } from 'lucide-preact';
import type { TargetedKeyboardEvent } from 'preact';
import { useEffect, useId, useMemo, useRef, useState } from 'preact/hooks';
import { cx, Input } from '../../components';
import { zonedParts } from '../../format';
import { t } from '../../i18n';
import { now } from '../../state/clock';

const SHOWN = 80;

function supported(): string[] {
  try {
    return Intl.supportedValuesOf('timeZone');
  } catch {
    return [];
  }
}

function browserZone(): string {
  try {
    return new Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

/** "UTC+8", "UTC−3:30": where a zone stands right now. */
export function zoneOffset(zone: string, at: number): string {
  try {
    const p = zonedParts(at, zone);
    const minutes = Math.round((Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second) - Math.floor(at / 1000) * 1000) / 60_000);
    const size = Math.abs(minutes);
    const rest = size % 60;
    return `UTC${minutes < 0 ? '−' : '+'}${Math.floor(size / 60)}${rest ? ':' + String(rest).padStart(2, '0') : ''}`;
  } catch {
    return '';
  }
}

export function ZoneSelect({ value, onChange }: { value: string; onChange: (zone: string) => void }) {
  const listId = useId();
  const root = useRef<HTMLDivElement>(null);
  // null while the field shows its value; a string once the user types.
  const [query, setQuery] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);

  // The zone in force, this browser's and UTC first: the likeliest picks.
  const all = useMemo(() => [...new Set([value, browserZone(), 'UTC', ...supported()].filter(Boolean))], [value]);
  const needle = (query ?? '').trim().toLowerCase().replace(/\s+/g, '_');
  const matches = useMemo(() => (needle ? all.filter((zone) => zone.toLowerCase().includes(needle)) : all), [all, needle]);
  const shown = matches.slice(0, SHOWN);

  useEffect(() => {
    if (open) root.current?.querySelector('.is-active')?.scrollIntoView({ block: 'nearest' });
  }, [open, active]);

  const close = () => {
    setOpen(false);
    setQuery(null);
  };
  const choose = (zone: string) => {
    onChange(zone);
    close();
  };

  const onKeyDown = (event: TargetedKeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (!open) {
        setOpen(true);
        return;
      }
      const step = event.key === 'ArrowDown' ? 1 : -1;
      setActive((index) => (shown.length === 0 ? 0 : (index + step + shown.length) % shown.length));
    } else if (event.key === 'Enter' && open) {
      // Enter picks; it must not also submit the form around it.
      event.preventDefault();
      const zone = shown[active];
      if (zone) choose(zone);
    } else if (event.key === 'Escape' && open) {
      event.preventDefault();
      event.stopPropagation();
      close();
    }
  };

  const at = now.value;
  return (
    <div class="st-combo" ref={root}>
      <Input
        mono
        role="combobox"
        aria-expanded={open}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={open && shown[active] ? `${listId}-${active}` : undefined}
        value={query ?? value}
        onInput={(text) => {
          setQuery(text);
          setOpen(true);
          setActive(0);
        }}
        onFocus={(event) => event.currentTarget.select()}
        onClick={() => setOpen(true)}
        onBlur={close}
        onKeyDown={onKeyDown}
        suffix={<ChevronsUpDown size={14} />}
        placeholder={t('settings.prefs.timezone.search')}
        autoComplete="off"
        spellcheck={false}
        autoCapitalize="off"
      />
      {open && (
        <ul class="st-combo__list" role="listbox" id={listId} aria-label={t('settings.prefs.timezone')}>
          {shown.map((zone, index) => (
            <li
              key={zone}
              id={`${listId}-${index}`}
              role="option"
              aria-selected={zone === value}
              class={cx('st-combo__option', index === active && 'is-active', zone === value && 'is-current')}
              // Keeps the focus in the field, so the click lands before the blur closes the list.
              onMouseDown={(event) => event.preventDefault()}
              onMouseEnter={() => setActive(index)}
              onClick={() => choose(zone)}
            >
              <span class="st-combo__name">{zone}</span>
              <span class="st-combo__offset num">{zoneOffset(zone, at)}</span>
            </li>
          ))}
          {shown.length === 0 && <li class="st-combo__note">{t('settings.prefs.timezone.none', { query: (query ?? '').trim() })}</li>}
          {matches.length > shown.length && <li class="st-combo__note">{t('settings.prefs.timezone.more', { n: matches.length - shown.length })}</li>}
        </ul>
      )}
    </div>
  );
}

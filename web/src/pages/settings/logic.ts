// The parts of the Settings pages that are plain logic: no DOM, no network,
// no words. test/settings.test.ts covers them.
import type { AuthType, Definition, Field, Scope } from '../../api/types';

// ---------- what the server said was wrong ----------

export interface Problem {
  /** The field the server named — "quiet.start", "request.url" — or null
   *  when the problem is about the whole thing. */
  field: string | null;
  text: string;
}

/**
 * Splits the message of a 400 into its problems. Settings and provider
 * definitions report everything wrong at once, as
 * "timezone: … is not a known IANA time zone; fx: the rate for USD must be positive".
 */
export function splitProblems(message: string): Problem[] {
  const out: Problem[] = [];
  for (const raw of message.split('; ')) {
    const part = raw.trim();
    if (part === '') continue;
    const match = /^([a-z][a-z0-9_.]*): (.+)$/s.exec(part);
    out.push(match ? { field: match[1]!, text: sentence(match[2]!) } : { field: null, text: sentence(part) });
  }
  return out;
}

/** The problems about one field, as one string; undefined when there are none. */
export function problemFor(problems: readonly Problem[], ...fields: string[]): string | undefined {
  const texts = problems.filter((problem) => problem.field !== null && fields.includes(problem.field)).map((problem) => problem.text);
  return texts.length > 0 ? texts.join(' ') : undefined;
}

/** The problems that belong to none of the given fields. */
export function otherProblems(problems: readonly Problem[], fields: readonly string[]): string[] {
  return problems.filter((problem) => problem.field === null || !fields.includes(problem.field)).map((problem) => (problem.field ? `${problem.field}: ${problem.text}` : problem.text));
}

function sentence(text: string): string {
  const capital = text.charAt(0).toUpperCase() + text.slice(1);
  return /[.!?。]$/.test(capital) ? capital : capital + '.';
}

// ---------- numbers typed into a form ----------

/** Reads a number someone typed: "", "1,200.5", " 7 ". */
export function parseNumber(input: string): number | null | 'invalid' {
  const cleaned = input.replace(/[,\s]/g, '');
  if (cleaned === '') return null;
  const value = Number(cleaned);
  return Number.isFinite(value) ? value : 'invalid';
}

/** Writes a number for an input: no thousands separators, no float noise. */
export function numberText(value: number): string {
  return String(Math.round(value * 1e6) / 1e6);
}

/** "428 KB", "1.2 MB": a size on disk. */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const text = unit === 0 || value >= 100 ? String(Math.round(value)) : value.toFixed(1).replace(/\.0$/, '');
  return `${text} ${units[unit]}`;
}

// ---------- exchange rates ----------

export interface RateRow {
  code: string;
  rate: string;
}

export function rateRows(fx: Record<string, number>): RateRow[] {
  return Object.keys(fx)
    .sort()
    .map((code) => ({ code, rate: numberText(fx[code]!) }));
}

export type RateProblem = 'code' | 'rate' | 'twice';

/**
 * Turns the rows of the rates table into the `fx` map. A row left entirely
 * empty is dropped; what a row is missing is reported by its index. Whether
 * a code is a real currency and a rate is positive is the server's call.
 */
export function ratesOf(rows: readonly RateRow[]): { fx: Record<string, number>; problems: Map<number, RateProblem> } {
  const fx: Record<string, number> = {};
  const problems = new Map<number, RateProblem>();
  rows.forEach((row, index) => {
    const code = row.code.trim().toUpperCase();
    const rate = parseNumber(row.rate);
    if (code === '' && rate === null) return;
    if (code === '') problems.set(index, 'code');
    else if (rate === null || rate === 'invalid') problems.set(index, 'rate');
    else if (code in fx) problems.set(index, 'twice');
    else fx[code] = rate;
  });
  return { fx, problems };
}

// ---------- browser push ----------

/**
 * The fingerprint of a push subscription: the first 32 hex digits of the
 * SHA-256 of its endpoint — the same thing engine.DeviceOf computes, so a
 * browser can find its own channel in the list.
 */
export async function deviceFingerprint(endpoint: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(endpoint));
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0'))
    .join('')
    .slice(0, 32);
}

/** Decodes the server's base64url key into the bytes pushManager wants. */
export function keyBytes(base64url: string): Uint8Array<ArrayBuffer> {
  const base64 = base64url.trim().replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(base64.padEnd(Math.ceil(base64.length / 4) * 4, '='));
  const out = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

export function sameBytes(a: ArrayBuffer | ArrayBufferView | null | undefined, b: Uint8Array): boolean {
  if (!a) return false;
  const left = a instanceof ArrayBuffer ? new Uint8Array(a) : new Uint8Array(a.buffer, a.byteOffset, a.byteLength);
  if (left.length !== b.length) return false;
  for (let i = 0; i < left.length; i++) if (left[i] !== b[i]) return false;
  return true;
}

/**
 * Names a device from its user agent: "Chrome · macOS". A guess, kept
 * simple; the name can be changed afterwards like any channel's. Empty when
 * nothing is recognised, which leaves the naming to the server.
 */
export function deviceName(userAgent: string, touch = false): string {
  const ua = userAgent;
  const browser = /Edg(?:e|A|iOS)?\//.test(ua)
    ? 'Edge'
    : /OPR\/|Opera/.test(ua)
      ? 'Opera'
      : /Firefox\/|FxiOS\//.test(ua)
        ? 'Firefox'
        : /Chrome\/|CriOS\//.test(ua)
          ? 'Chrome'
          : /Safari\//.test(ua)
            ? 'Safari'
            : '';
  const os = /iPhone|iPod/.test(ua)
    ? 'iPhone'
    : /iPad/.test(ua)
      ? 'iPad'
      : /Android/.test(ua)
        ? 'Android'
        : /CrOS/.test(ua)
          ? 'ChromeOS'
          : /Windows/.test(ua)
            ? 'Windows'
            : /Macintosh|Mac OS X/.test(ua)
              ? // An iPad asking for the desktop site says it is a Mac; a Mac has no touch screen.
                touch
                ? 'iPad'
                : 'macOS'
              : /Linux/.test(ua)
                ? 'Linux'
                : '';
  return [browser, os].filter(Boolean).join(' · ');
}

// ---------- tokens ----------

/** Whether a token with these scopes may do what `scope` covers: admin
 *  carries the other three (core.Token.Allows). */
export function allows(scopes: readonly Scope[], scope: Scope): boolean {
  return scopes.includes(scope) || scopes.includes('admin');
}

/** An MCP client's configuration for this server, ready to paste. */
export function mcpConfig(origin: string, token: string): string {
  return JSON.stringify(
    { mcpServers: { quotapulse: { type: 'http', url: `${origin}/mcp`, headers: { Authorization: `Bearer ${token}` } } } },
    null,
    2,
  );
}

export function curlOverview(origin: string, token: string): string {
  return `curl -H "Authorization: Bearer ${token}" ${origin}/api/v1/overview`;
}

export function curlIngest(origin: string, token: string): string {
  return [
    `curl -X POST ${origin}/api/v1/ingest \\`,
    `  -H "Authorization: Bearer ${token}" -H "Content-Type: application/json" \\`,
    `  -d '{"meter": "my-meter", "name": "My meter", "kind": "counter", "unit": "USD", "value": 12.5}'`,
  ].join('\n');
}

// ---------- provider definitions ----------

export interface Pair {
  key: string;
  value: string;
}

export interface FieldDraft {
  key: string;
  label: string;
  secret: boolean;
  optional: boolean;
  default: string;
  placeholder: string;
  /** Carried through from the definition it was copied from; not edited. */
  help: string;
}

/** A definition as the editor holds it: every input as text. */
export interface DefinitionDraft {
  id: string;
  name: string;
  nameZh: string;
  /** homepage, notes and notesZh are carried through, not edited. */
  homepage: string;
  notes: string;
  notesZh: string;
  fields: FieldDraft[];
  method: 'GET' | 'POST';
  url: string;
  auth: AuthType;
  authName: string;
  authValue: string;
  region: string;
  service: string;
  headers: Pair[];
  query: Pair[];
  body: string;
  extract: string;
}

/** What a server-side id must look like (provider.idPattern). */
export const ID_PATTERN = /^[a-z0-9][a-z0-9-]{1,39}$/;

const pairs = (map: Record<string, string> | undefined): Pair[] => Object.entries(map ?? {}).map(([key, value]) => ({ key, value }));

export function draftOf(definition: Definition): DefinitionDraft {
  const { request } = definition;
  return {
    id: definition.id,
    name: definition.name,
    nameZh: definition.name_zh ?? '',
    homepage: definition.homepage ?? '',
    notes: definition.notes ?? '',
    notesZh: definition.notes_zh ?? '',
    fields: (definition.fields ?? []).map((field) => ({
      key: field.key,
      label: field.label ?? '',
      secret: Boolean(field.secret),
      optional: Boolean(field.optional),
      default: field.default ?? '',
      placeholder: field.placeholder ?? '',
      help: field.help ?? '',
    })),
    method: (request.method ?? '').toUpperCase() === 'POST' ? 'POST' : 'GET',
    url: request.url,
    auth: request.auth.type,
    authName: request.auth.name ?? '',
    authValue: request.auth.value ?? '',
    region: request.auth.region ?? '',
    service: request.auth.service ?? '',
    headers: pairs(request.headers),
    query: pairs(request.query),
    body: request.body ?? '',
    // The catalog's programs start on a new line; an editor should not.
    extract: definition.extract.replace(/^\s*\n/, ''),
  };
}

/** What a new definition starts from: one API key, sent as a bearer token. */
export function blankDraft(): DefinitionDraft {
  return {
    id: '',
    name: '',
    nameZh: '',
    homepage: '',
    notes: '',
    notesZh: '',
    fields: [{ key: 'api_key', label: 'API Key', secret: true, optional: false, default: '', placeholder: '', help: '' }],
    method: 'GET',
    url: '',
    auth: 'bearer',
    authName: '',
    authValue: '',
    region: '',
    service: '',
    headers: [],
    query: [],
    body: '',
    extract: '{\n  key: "balance", kind: "balance", unit: "USD",\n  value: (.balance | num)\n}',
  };
}

const record = (rows: readonly Pair[]): Record<string, string> | undefined => {
  const out: Record<string, string> = {};
  for (const row of rows) if (row.key.trim() !== '') out[row.key.trim()] = row.value;
  return Object.keys(out).length > 0 ? out : undefined;
};

/** Which of name, value, region and service an auth type reads. */
export function authNeeds(type: AuthType): { name: boolean; value: boolean; scope: boolean } {
  return {
    name: type === 'header' || type === 'query',
    value: type === 'bearer' || type === 'header' || type === 'query' || type === 'basic',
    scope: type === 'volc-v4',
  };
}

/** The definition a draft describes, with everything empty left out. */
export function definitionOf(draft: DefinitionDraft): Definition {
  const needs = authNeeds(draft.auth);
  const auth: Definition['request']['auth'] = { type: draft.auth };
  if (needs.name && draft.authName.trim() !== '') auth.name = draft.authName.trim();
  if (needs.value && draft.authValue.trim() !== '') auth.value = draft.authValue.trim();
  if (needs.scope) {
    if (draft.region.trim() !== '') auth.region = draft.region.trim();
    if (draft.service.trim() !== '') auth.service = draft.service.trim();
  }

  const request: Definition['request'] = { url: draft.url.trim(), auth };
  if (draft.method === 'POST') request.method = 'POST';
  const headers = record(draft.headers);
  const query = record(draft.query);
  if (headers) request.headers = headers;
  if (query) request.query = query;
  if (draft.method === 'POST' && draft.body.trim() !== '') request.body = draft.body;

  const definition: Definition = {
    id: draft.id.trim(),
    name: draft.name.trim(),
    fields: draft.fields.map((each) => {
      const field: Field = { key: each.key.trim(), label: each.label.trim() };
      if (each.help !== '') field.help = each.help;
      if (each.placeholder.trim() !== '') field.placeholder = each.placeholder.trim();
      if (each.secret) field.secret = true;
      if (each.optional) field.optional = true;
      if (each.default.trim() !== '') field.default = each.default.trim();
      return field;
    }),
    request,
    extract: draft.extract,
    builtin: false,
  };
  if (draft.nameZh.trim() !== '') definition.name_zh = draft.nameZh.trim();
  if (draft.homepage !== '') definition.homepage = draft.homepage;
  if (draft.notes !== '') definition.notes = draft.notes;
  if (draft.notesZh !== '') definition.notes_zh = draft.notesZh;
  return definition;
}

/** An id for a copy that nothing uses yet: "my-deepseek", "my-deepseek-2". */
export function copyId(id: string, taken: ReadonlySet<string>): string {
  const base = (id.startsWith('my-') ? id : `my-${id}`).slice(0, 36);
  if (!taken.has(base)) return base;
  for (let n = 2; ; n++) {
    const candidate = `${base}-${n}`;
    if (!taken.has(candidate)) return candidate;
  }
}

/** Moves an item of a list one place up (-1) or down (+1). */
export function moved<T>(list: readonly T[], index: number, step: -1 | 1): T[] {
  const target = index + step;
  if (target < 0 || target >= list.length) return [...list];
  const out = [...list];
  [out[index], out[target]] = [out[target]!, out[index]!];
  return out;
}

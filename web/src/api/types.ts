// The API's JSON, exactly as the Go server writes it. Each type names the Go
// type it mirrors; when the server changes, change this file to match and let
// the compiler find what breaks.
//
// Conventions of the wire format:
//   - an instant is an RFC 3339 string in UTC          (Instant)
//   - a calendar day is "YYYY-MM-DD", with no zone      (Day)
//   - a nil pointer is null; a field marked omitempty may be missing (`?`)

export type Instant = string;
export type Day = string;

// ---------- envelopes (internal/api) ----------

/** Every list endpoint answers with this. */
export interface List<T> {
  items: T[];
}

/** The body of every failed request: { "error": { code, message } }. */
export interface ErrorBody {
  error: { code: string; message: string };
}

/** GET /api/v1/session */
export interface SessionInfo {
  signed_in: boolean;
  /** False until someone chooses the password: then `setup` says how. */
  password_set: boolean;
  /** While there is no password: "open" to whoever opens a new install
   *  within the hour, "code" with the one-time code from the server's log. */
  setup?: 'open' | 'code';
  /** Nothing is configured yet, so the setup asks the time zone and language
   *  as well. */
  fresh?: boolean;
  /** Signed in: the password is QUOTAPULSE_PASSWORD, which the dashboard
   *  cannot change. */
  password_from_env?: boolean;
  version: string;
  demo: boolean;
  muted: boolean;
  /** The server's clock and zone; only when signed in. */
  now?: Instant;
  timezone?: string;
}

export interface SetupInput {
  password: string;
  /** The one-time code, when the setup takes one. */
  code?: string;
  /** For a new install: its time zone and language. */
  timezone?: string;
  language?: 'en' | 'zh';
}

// ---------- core (internal/core/core.go) ----------

export type Kind = 'balance' | 'quota' | 'counter';
export type Severity = 'info' | 'warning' | 'critical';
export type AccountStatus = 'pending' | 'ok' | 'failing';

/** core.Account, without the credential, which never leaves the server. */
export interface Account {
  id: string;
  name: string;
  provider: string;
  credential_unreadable: boolean;
  group: string;
  /** 0 means the default interval. */
  interval_seconds: number;
  enabled: boolean;
  status: AccountStatus;
  last_error: string;
  consecutive_failures: number;
  sampled_at: Instant | null;
  succeeded_at: Instant | null;
  next_sample_at: Instant | null;
  latency_ms: number | null;
  created_at: Instant;
  updated_at: Instant;
}

/** core.Rules. null means "use the default from Settings"; 0 turns a rule off. */
export interface Rules {
  low: number | null;
  runway_days: number | null;
  anomaly: boolean | null;
  budget: number | null;
  pace: boolean;
  stale_after_seconds: number | null;
}

/** core.Meter */
export interface Meter {
  id: string;
  /** Empty for a pushed meter. */
  account_id: string;
  key: string;
  name: string;
  /** True once a person has named it; it is then shown as `name` everywhere. */
  renamed: boolean;
  kind: Kind;
  /** A currency code (CNY, USD), "%", or any label (credits, calls). */
  unit: string;
  value: number | null;
  limit: number | null;
  resets_at: Instant | null;
  window_seconds: number | null;
  sampled_at: Instant | null;
  rules: Rules;
  muted_until: Instant | null;
  hidden: boolean;
  sort: number;
  created_at: Instant;
  updated_at: Instant;
}

export type ReminderKind = 'renewal' | 'birthday' | 'anniversary' | 'holiday' | 'expiry' | 'custom';

/** recur.Rule (internal/recur/rule.go) */
export interface RecurRule {
  freq: 'once' | 'weekly' | 'monthly' | 'yearly';
  /** solar when missing */
  calendar?: 'solar' | 'lunar';
  date?: Day;
  /** 1 Monday … 7 Sunday */
  weekday?: number;
  month?: number;
  day?: number;
  interval?: number;
  since?: number;
}

/** recur.Lunar */
export interface Lunar {
  year: number;
  month: number;
  day: number;
  leap: boolean;
  /** The traditional spelling, e.g. 冬月十六. */
  label: string;
}

/** core.Reminder */
export interface Reminder {
  id: string;
  title: string;
  kind: ReminderKind;
  rule: RecurRule;
  lead_days: number[];
  amount: number | null;
  currency: string;
  group: string;
  notes: string;
  done_through: Day | null;
  snoozed_until: Day | null;
  enabled: boolean;
  created_at: Instant;
  updated_at: Instant;
}

export type SubjectType = 'meter' | 'account' | 'reminder' | 'channel';
export type IncidentKind = 'level' | 'budget' | 'stale' | 'failing' | 'due' | 'delivery';
export type IncidentState = 'open' | 'resolved';

/** core.Incident */
export interface Incident {
  id: string;
  subject_type: SubjectType;
  subject_id: string;
  kind: IncidentKind;
  dedup: string;
  severity: Severity;
  state: IncidentState;
  reason: string;
  title: string;
  data: Record<string, unknown> | null;
  opened_at: Instant;
  updated_at: Instant;
  resolved_at: Instant | null;
  acked_at: Instant | null;
  notified_at: Instant | null;
  notify_count: number;
}

export type Topic = 'meters' | 'dates' | 'system' | 'digest';

/** core.Channel, without its config. */
export interface Channel {
  id: string;
  name: string;
  type: string;
  topics: Topic[];
  min_severity: Severity;
  enabled: boolean;
  created_at: Instant;
  updated_at: Instant;
}

export type DeliveryEvent = 'opened' | 'escalated' | 'reminder' | 'resolved' | 'digest' | 'test';
export type DeliveryStatus = 'pending' | 'sent' | 'failed' | 'dropped';

/** notify.Message (internal/notify/message.go): a delivery's rendered text. */
export interface Message {
  event: string;
  severity: Severity;
  topic: string;
  title: string;
  body?: string;
  facts?: Fact[];
  sections?: { title: string; lines: string[] }[];
  link?: string;
  link_label?: string;
  /** The same place as link, without the host. */
  path?: string;
  footer?: string;
  at: Instant;
  subject?: { incident_id?: string; kind?: string; reason?: string; type?: string; id?: string; name?: string };
}

/** core.Delivery */
export interface Delivery {
  id: number;
  incident_id: string;
  event: DeliveryEvent;
  channel_id: string;
  channel: string;
  message: Message;
  status: DeliveryStatus;
  attempts: number;
  not_before: Instant;
  last_error: string;
  created_at: Instant;
  sent_at: Instant | null;
}

export type Scope = 'read' | 'act' | 'ingest' | 'admin';

/** core.Token */
export interface Token {
  id: string;
  name: string;
  prefix: string;
  scopes: Scope[];
  expires_at: Instant | null;
  last_used_at: Instant | null;
  created_at: Instant;
}

// ---------- settings (internal/core/settings.go) ----------

export interface Settings {
  /** An IANA zone. It decides what "today" is, here and on the server. */
  timezone: string;
  /** The language notifications are written in. */
  language: 'zh' | 'en';
  base_currency: string;
  /** One unit of the key is worth this much base currency. */
  fx: Record<string, number>;
  sample_seconds: number;
  runway_days: number;
  anomaly: boolean;
  /** HH:MM */
  reminder_time: string;
  quiet: { enabled: boolean; start: string; end: string };
  /** weekday: 1 Monday … 7 Sunday, 0 every day. */
  digest: { enabled: boolean; weekday: number; time: string };
  renotify_hours: number;
  notify_resolved: boolean;
  retention_days: number;
  upcoming_days: number;
  public_url: string;
}

// ---------- forecasts (internal/forecast) ----------

export type Confidence = 'none' | 'low' | 'medium' | 'high';

/** forecast.Event */
export interface FlowEvent {
  at: Instant;
  amount: number;
}

/** forecast.Burst */
export interface Burst {
  window_hours: number;
  spend: number;
  rate_per_day: number;
  runway_days: number;
  baseline_per_day: number;
  ratio: number | null;
}

/** forecast.Balance: the forecast for a balance or a counter. */
export interface BalanceForecast {
  as_of: Instant;
  coverage_days: number;
  confidence: Confidence;
  /** Expected spend per day: the last 30 days, recent days weighing more. */
  burn_per_day: number;
  burn_7d: number | null;
  burn_30d: number | null;
  peak_per_day: number;
  spend_24h: number;
  runway_days: number | null;
  depletes_at: Instant | null;
  runway_days_at_peak: number | null;
  bursts?: Burst[];
  last_top_up: FlowEvent | null;
}

/** forecast.Quota */
export interface QuotaForecast {
  remaining_pct: number;
  used_pct: number;
  elapsed_pct: number | null;
  /** Above 100 means it runs out before the reset. */
  projected_pct: number | null;
  exhausts_at: Instant | null;
}

/** forecast.Month */
export interface MonthForecast {
  /** YYYY-MM */
  month: string;
  spend: number;
  projected: number;
  days_elapsed: number;
  days_in_month: number;
}

/** forecast.Day */
export interface DaySpend {
  date: Day;
  spend: number;
  top_up: number;
}

// ---------- views (internal/engine/views.go) ----------

/** notify.Fact */
export interface Fact {
  label: string;
  value: string;
}

/** engine.Point */
export interface Point {
  t: Instant;
  v: number;
}

/** engine.AccountRef */
export interface AccountRef {
  id: string;
  name: string;
  provider: string;
  provider_name: string;
  group: string;
  status: AccountStatus;
  last_error: string;
}

export type MeterStatus = 'ok' | 'warning' | 'critical' | 'failing' | 'pending' | 'paused';

/** engine.MeterView */
export interface MeterView extends Meter {
  /** The name to show: the account's when that says it all. */
  display: string;
  status: MeterStatus;
  muted: boolean;
  account: AccountRef | null;
  forecast: BalanceForecast | null;
  quota: QuotaForecast | null;
  month: MonthForecast | null;
  effective: { runway_days: number; anomaly: boolean };
  incidents: IncidentView[];
  /** The last two weeks, thinned for a small chart. */
  spark: Point[];
}

/** engine.AccountView */
export interface AccountView extends Account {
  provider_name: string;
  /** Plain fields as they are, secret fields masked. */
  credential: Record<string, string>;
  effective_interval_seconds: number;
  meter_ids: string[];
  incident: IncidentView | null;
}

/** engine.IncidentView. headline, detail and facts arrive already written in
 *  the request's language: show them as given. */
export interface IncidentView extends Incident {
  headline: string;
  detail: string;
  facts: Fact[];
  /** Where on the dashboard the incident is best looked at. */
  path: string;
}

export type ReminderState = 'upcoming' | 'soon' | 'today' | 'snoozed' | 'past' | 'disabled';

/** engine.ReminderView */
export interface ReminderView extends Reminder {
  next: Day | null;
  /** Days until next; 0 is today. */
  days: number | null;
  lunar: Lunar | null;
  ordinal: number;
  state: ReminderState;
  handled: Day | null;
  following: Day[];
  monthly_amount: number | null;
}

/** engine.SpendSummary */
export interface SpendSummary {
  month: string;
  base: string;
  total: number;
  projected: number;
  fixed: number;
  unconverted: string[];
}

/** engine.Counts */
export interface Counts {
  accounts: number;
  meters: number;
  reminders: number;
  channels: number;
}

/** engine.Overview — GET /api/v1/overview */
export interface Overview {
  now: Instant;
  timezone: string;
  status: 'ok' | 'warning' | 'critical';
  attention: IncidentView[];
  meters: MeterView[];
  accounts: AccountView[];
  upcoming: ReminderView[];
  spend: SpendSummary;
  counts: Counts;
  muted: boolean;
}

export type SeriesRange = '24h' | '7d' | '30d' | '90d' | '1y';

/** engine.Series — GET /api/v1/meters/{id}/series */
export interface Series {
  meter: MeterView;
  range: SeriesRange;
  from: Instant;
  to: Instant;
  points: Point[];
  daily: DaySpend[];
  top_ups: FlowEvent[];
  /** Two points continuing the line at the expected burn, or none. */
  projection: Point[];
}

// ---------- spend (internal/engine/spend.go) ----------

export interface SpendRow {
  meter_id: string;
  display: string;
  unit: string;
  spend: number;
  projected: number;
  budget: number | null;
  base: number | null;
  projected_base: number | null;
}

export interface SpendRenewal {
  reminder_id: string;
  title: string;
  date: Day;
  amount: number;
  currency: string;
  base: number | null;
}

/** engine.Spend — GET /api/v1/spend?month=YYYY-MM */
export interface Spend {
  month: string;
  base: string;
  is_current: boolean;
  total: number;
  projected: number;
  fixed: number;
  days_elapsed: number;
  days_in_month: number;
  rows: SpendRow[];
  renewals: SpendRenewal[];
  daily: { date: Day; amount: number }[];
  unconverted: string[];
  history: { month: string; total: number; fixed: number }[];
}

// ---------- providers (internal/provider) ----------

/** provider.Field and notify.Field: something to fill in on a form. */
export interface Field {
  key: string;
  label: string;
  help?: string;
  placeholder?: string;
  /** Write-only: the API never returns the value. */
  secret?: boolean;
  optional?: boolean;
  default?: string;
}

export type AuthType = 'bearer' | 'header' | 'query' | 'basic' | 'none' | 'aliyun-rpc' | 'volc-v4';
export type Checked = 'live' | 'recorded' | 'docs';

/** provider.Definition */
export interface Definition {
  id: string;
  name: string;
  name_zh?: string;
  homepage?: string;
  notes?: string;
  notes_zh?: string;
  fields: Field[];
  request: {
    method?: string;
    url: string;
    headers?: Record<string, string>;
    query?: Record<string, string>;
    body?: string;
    auth: { type: AuthType; name?: string; value?: string; region?: string; service?: string };
  };
  /** A jq program that turns the response into meters. */
  extract: string;
  builtin: boolean;
  /** How a built-in was verified. */
  checked?: Checked;
}

/** provider.Sample: a meter as a provider reports it. */
export interface Sample {
  key: string;
  name?: string;
  name_zh?: string;
  kind: Kind;
  unit: string;
  value: number;
  limit?: number;
  resets_at?: Instant;
  window_seconds?: number;
}

// ---------- request and response bodies (internal/engine/admin.go) ----------

/** engine.AccountInput. A missing field is left unchanged. */
export interface AccountInput {
  name?: string;
  provider?: string;
  /** On an edit, a secret field left empty keeps its stored value. */
  credential?: Record<string, string>;
  group?: string;
  interval_seconds?: number;
  enabled?: boolean;
}

/** engine.TestInput */
export interface TestInput {
  provider?: string;
  credential?: Record<string, string>;
  /** Lets an edit form test with the stored secrets it cannot see. */
  account_id?: string;
  definition?: Definition;
}

/** engine.TestResult */
export interface TestResult {
  ok: boolean;
  error?: string;
  status: number;
  latency_ms: number;
  samples: Sample[];
  body?: unknown;
}

/** engine.MeterInput. rules, when given, replaces the meter's rules as a whole. */
export interface MeterInput {
  name?: string;
  rules?: Rules;
  hidden?: boolean;
  sort?: number;
  unit?: string;
}

/** engine.ReminderInput */
export interface ReminderInput {
  title?: string;
  kind?: ReminderKind;
  rule?: RecurRule;
  lead_days?: number[];
  /** 0 clears the amount. */
  amount?: number;
  currency?: string;
  group?: string;
  notes?: string;
  enabled?: boolean;
}

/** engine.ChannelInput */
export interface ChannelInput {
  name?: string;
  type?: string;
  config?: Record<string, string>;
  topics?: Topic[];
  min_severity?: Severity;
  enabled?: boolean;
}

/** notify.Type — GET /api/v1/channel-types */
export interface ChannelType {
  id: string;
  name: string;
  name_zh?: string;
  fields: Field[];
}

/** store.ChannelHealth */
export interface ChannelHealth {
  last_status: string;
  last_error: string;
  last_at: Instant | null;
  failed_in_a_row: number;
}

/** engine.ChannelView */
export interface ChannelView extends Channel {
  type_name: string;
  /** Secrets masked. */
  config: Record<string, string>;
  health: ChannelHealth;
}

/** engine.TokenInput */
export interface TokenInput {
  name: string;
  scopes: Scope[];
  expires_at?: Instant;
}

/** engine.NewToken: `token` is the plaintext, shown this once. */
export interface NewToken extends Token {
  token: string;
}

/** engine.Push — POST /api/v1/ingest */
export interface Push {
  meter: string;
  name?: string;
  kind?: Kind;
  unit?: string;
  value: number;
  limit?: number;
  resets_at?: Instant;
  window_seconds?: number;
  at?: Instant;
}

// ---------- system (internal/api/handlers.go, internal/runtime, internal/store, internal/bundle) ----------

/** runtime.Job */
export interface Job {
  name: string;
  last_run: Instant | null;
  last_error: string;
  runs: number;
  every_seconds: number;
}

/** GET /api/v1/system */
export interface SystemInfo {
  version: string;
  demo: boolean;
  muted: boolean;
  now: Instant;
  stats: {
    bytes: number;
    accounts: number;
    meters: number;
    readings: number;
    reminders: number;
    incidents: number;
    deliveries: number;
  };
  jobs: Job[];
}

/** bundle.Report — POST /api/v1/import */
export interface ImportReport {
  created: Record<string, number>;
  skipped: Record<string, number>;
  notes: string[];
}

// ---------- browser push (internal/engine/admin.go, internal/notify/drivers.go) ----------
// Two fields the types above were missing, added by declaration merging.

/** engine.ChannelView.Device: on a webpush channel, the first 32 hex digits
 *  of the SHA-256 of its subscription's endpoint. A browser hashes its own
 *  endpoint the same way to find itself in the list. */
export interface ChannelView {
  device?: string;
}

/** notify.Type.Managed: channels of this type are not created from a form;
 *  a browser's push subscription makes them. */
export interface ChannelType {
  managed?: boolean;
}

/**
/**
 * Type definitions for the HTTP contract.
 *
 * Fields cross-checked one by one against docs/API.md and
 * internal/model/model.go — the backend serializes "no data" as null rather
 * than 0, so `| null` is used consistently instead of optional properties:
 * a missed null check would show a failed project as "balance 0, healthy".
 */

// ---------- Scheduled jobs ----------

/** Balance type, mirrors model.TypeBalance / TypeCredits / TypeQuota */
export type BalanceType = 'balance' | 'credits' | 'quota';

/** Runway confidence, mirrors model.Confidence*; none empties the other estimate fields, low never feeds alerting */
export type Confidence = 'none' | 'low' | 'medium' | 'high';

/** Runway confidence, mirrors model.Confidence*; none empties the other estimate fields, low never feeds alerting */
export type CycleType = 'weekly' | 'monthly' | 'yearly' | 'lunar_yearly';

// ---------- Scheduled jobs ----------

/** Error response; validation failures also carry errors */
export interface ErrorResponse {
  status: 'error';
  message: string;
  errors?: string[];
}

/** Write success response */
export interface MutationResponse {
  status: 'success';
  message?: string;
}

/** data is null on parse failure or non-JSON responses; callers must handle that */
export type ApiPayload<T> = T | ErrorResponse | null;

// ==================== /api/features ====================

/** Optional capability switches; add fields here as the backend grows — unknown fields are ignored without error */
export interface Features {
  subscriptions: boolean;
  dynamic_config: boolean;
  history: boolean;
  email_scan: boolean;
  /* Balance history is kept, so runways can be estimated. Absent from older servers. */
  database?: boolean;
}

export interface FeaturesResponse {
  status: 'success';
  features: Features;
}

// ==================== /api/credits ====================

/** Spend grouped by local date in runway analysis, mirrors model.DailySpend */
export interface DailySpend {
  date: string; // YYYY-MM-DD
  consumed: number;
}

/** One account's spend profile, mirrors model.Runway */
export interface Runway {
  project_id: string;
  project_name: string;
  provider: string;
  balance_type: BalanceType;
  current_balance: number | null;
  window_days: number;
  data_points: number;
  span_hours: number;
  consumed: number;
  topped_up: number;
  burn_per_day: number | null;
  runway_days: number | null;
  depletion_date: string | null; // YYYY-MM-DD
  confidence: Confidence;
  daily: DailySpend[];
  today_consumed: number | null;
  baseline_consumed: number | null;
  spike_ratio: number | null;
  monthly_projection?: number | null;
}

/**
/**
 * One balance check's result, mirrors model.CheckResult.
 * runway is omitempty: without a database or enough history the whole field
 * is absent.
 */
export interface CheckResult {
  project: string;
  owner_project: string | null;
  provider: string;
  type: BalanceType;
  success: boolean;
  credits: number | null;
  threshold: number | null;
  need_alarm: boolean;
  alarm_sent: boolean;
  error: string | null;
  cached: boolean;
  runway?: Runway | null;
}

/** Dashboard header counts, mirrors model.BalanceSummary */
export interface BalanceSummary {
  total: number;
  success: number;
  failed: number;
  need_alarm: number;
}

/** Right after process start nothing has been checked yet: projects is empty and summary is {}, hence Partial */
export interface CreditsResponse {
  last_update: string | null; // ISO timestamp ending in Z.
  projects: CheckResult[];
  summary: Partial<BalanceSummary>;
}

// ==================== /api/refresh ====================

export interface RefreshResponse {
  status: 'success';
  message: string;
  refreshed_count: number;
  execution_time_seconds: number;
  dry_run: boolean;
}

// ==================== /api/subscriptions ====================

/** One subscription's check result, mirrors model.SubscriptionResult */
export interface SubscriptionResult {
  name: string;
  owner_project: string | null;
  renewal_day: number;
  cycle_type: CycleType;
  days_until_renewal: number;
  next_renewal_date: string; // YYYY-MM-DD
  need_alert: boolean;
  alert_sent: boolean;
  amount: number;
  already_renewed: boolean;
  last_renewed_date: string | null;
  /** Notification outcome from the last check; absent on older responses. */
  alert_state?: 'sent' | 'cooldown_skipped' | 'failed' | 'dry_run' | 'snoozed' | string;
  snoozed_until?: string | null;
  next_eligible_at?: string | null;
  last_error?: string | null;
}

export interface SubscriptionsResponse {
  last_update: string | null;
  subscriptions: SubscriptionResult[];
  summary: Partial<{ total: number; need_alert: number }>;
}

// ==================== /api/config/subscriptions ====================

/** Subscription config (including fields absent from runtime status like alert_days_before), mirrors model.Subscription */
export interface SubscriptionConfig {
  name: string;
  owner_project: string | null;
  cycle_type: CycleType;
  renewal_day: number; // Weekly 1-7，Monthly 1-31，Yearly/lunar_yearly MMDD
  alert_days_before: number;
  amount: number;
  enabled: boolean;
  last_renewed_date: string | null;
  timezone?: string;
  snoozed_until?: string | null;
}

export interface SubscriptionsConfigResponse {
  status: 'success';
  subscriptions: SubscriptionConfig[];
}

/** POST /api/config/subscription payload: name locates, new_name renames, the rest optional */
export interface SubscriptionPayload {
  name: string;
  new_name?: string;
  owner_project: string | null;
  amount: number;
  cycle_type: CycleType;
  renewal_day: number | string;
  alert_days_before: number;
  enabled: boolean;
  last_renewed_date?: string;
}

// ==================== /api/providers ====================

export interface ProviderOption {
  value: string;
  label: string;
  default_type: BalanceType;
  currency?: string;
}

export interface ProvidersResponse {
  status: 'success';
  providers: ProviderOption[];
}

// ==================== /api/config/projects ====================

/** Project config, mirrors model.Project; api_key is masked and from_env projects cannot be deleted */
export interface ProjectConfig {
  name: string;
  provider: string;
  api_key: string;
  threshold: number;
  type: BalanceType;
  currency?: string;
  monthly_budget?: number;
  owner_project: string | null;
  enabled: boolean;
  from_env?: boolean;
}

export interface ProjectsConfigResponse {
  status: 'success';
  projects: ProjectConfig[];
}

/** POST /api/config/project payload: creating needs provider + api_key; empty key on update keeps the old one */
export interface ProjectPayload {
  name: string;
  provider: string;
  type: BalanceType | null;
  owner_project: string | null;
  enabled: boolean;
  threshold?: number;
  api_key?: string;
  currency?: string;
  monthly_budget?: number;
}

// ==================== /api/config/emails ====================

/** Mailbox config, mirrors model.Mailbox; password is masked to '***' or '' */
export interface MailboxConfig {
  name: string;
  host: string;
  port: number;
  username: string;
  password: string;
  use_ssl: boolean;
  enabled: boolean;
  from_env?: boolean;
}

export interface EmailsConfigResponse {
  status: 'success';
  emails: MailboxConfig[];
}

/** POST /api/config/email payload: creating needs host / username / password; empty password on update keeps the old one */
export interface MailboxPayload {
  name: string;
  host: string;
  port: number;
  username: string;
  use_ssl: boolean;
  enabled: boolean;
  password?: string;
}

// ==================== /api/email/scan ====================

/** One mailbox's connection and stats for a scan, mirrors model.MailboxResult */
export interface MailboxResult {
  name: string;
  host: string;
  port: number;
  username: string;
  total_emails: number;
  alert_count: number;
  success: boolean;
  error: string | null;
}

/** One keyword-hit mail, mirrors model.EmailAlert */
export interface EmailAlert {
  mailbox: string;
  subject: string;
  sender: string;
  date: string;
  keywords: string[];
  service_name: string | null;
  amount: number | null;
  alert_sent: boolean;
  /**
/**
 * Scan results live in process memory and clear on restart: last_update is
 * then null, and days / dry_run are null too — the page uses this to tell
 * "never scanned" from "scanned, nothing hit".
 */
  duplicate?: boolean;
}

export interface EmailScanSummary {
  total_mailboxes: number;
  failed_mailboxes: number;
  total_emails: number;
  total_alerts: number;
  alerts_sent: number;
}

/**
/**
 * Scan results live in process memory and clear on restart: last_update is
 * then null, and days / dry_run are null too — the page uses this to tell
 * "never scanned" from "scanned, nothing hit".
 */
export interface EmailScanState {
  last_update: string | null;
  days: number | null;
  dry_run: boolean | null;
  mailboxes: MailboxResult[];
  alerts: EmailAlert[];
  summary: Partial<EmailScanSummary>;
}

// ==================== /api/history/* ====================

export interface HistoryListResponse<T> {
  status: 'success';
  count: number;
  data: T[];
}

/** One balance snapshot row from the database */
export interface BalanceHistoryRecord {
  id: number;
  project_id: string;
  project_name: string;
  provider: string;
  balance: number;
  threshold: number | null;
  balance_type: BalanceType;
  need_alarm: boolean;
  timestamp: string;
}

/* A mailbox+sender pair muted as a false positive: matched emails stay in history but don't notify. */
export interface EmailSuppression {
  mailbox: string;
  sender: string;
}

/** Historical alert mail; the stored column is matched_keywords, a different name from the live scan's keywords */
export interface EmailAlertRecord {
  id: number;
  mailbox: string;
  sender: string;
  subject: string;
  date: string;
  service_name: string | null;
  amount: number | null;
  matched_keywords: string[];
  alert_sent: boolean;
  timestamp: string;
}

/** One data point in the trend dialog */
export interface TrendPoint {
  timestamp: string;
  balance: number;
  need_alarm: boolean;
}

/** GET /api/history/trend/<project_id>; fewer than two points means no change / change_percent */
export interface TrendData {
  project_id: string;
  project_name: string;
  days: number;
  data_points: number;
  current_balance: number;
  min_balance: number;
  max_balance: number;
  avg_balance: number;
  threshold: number;
  first_timestamp: string;
  last_timestamp: string;
  history: TrendPoint[];
  change?: number;
  change_percent?: number;
}

export interface TrendResponse {
  status: 'success';
  data: TrendData;
}

// ==================== /health ====================

export interface HealthResponse {
  status: 'healthy' | 'degraded';
  has_data: boolean;
  is_stale: boolean;
  jobs_healthy: boolean;
  failed_jobs: string[];
  last_update: string | null;
  uptime_seconds: number;
  version: string;
}

// ==================== /api/jobs ====================

export interface JobState {
  name: string;
  description: string;
  schedule: string;
  enabled: boolean;
  next_run: string | null;
  last_run: string | null;
  last_success: string | null;
  last_error: string | null;
  last_duration_seconds: number | null;
  last_detail: Record<string, unknown> | null;
  runs: number;
  failures: number;
}

export interface JobsResponse {
  healthy: boolean;
  jobs: JobState[];
}

// ==================== Custom providers ====================

/** A page-defined platform: one GET, a JSON path to the balance, and how the key travels. */
export interface CustomProviderConfig {
  name: string;
  display_name: string;
  url: string;
  auth_mode: 'bearer' | 'query' | 'header' | 'none';
  auth_param?: string;
  default_type: BalanceType;
  currency?: string;
  balance_path: string;
  balance_scale?: number;
  success_path?: string;
  success_values?: string;
  error_path?: string;
  subtract_path?: string;
  extract_mode?: '' | 'path' | 'min_window';
  window_types?: string;
  window_reset_hours?: number;
  extra_params?: string;
  sign_region?: string;
  sign_service?: string;
  from_db?: boolean;
}

export interface CustomProvidersResponse {
  status: 'success';
  providers: CustomProviderConfig[];
}

/** POST /api/config/provider payload: name identifies; fields are optional on update. */
export interface CustomProviderPayload {
  name: string;
  display_name?: string;
  url?: string;
  auth_mode?: string;
  auth_param?: string;
  default_type?: BalanceType;
  currency?: string;
  balance_path?: string;
  balance_scale?: number;
  success_path?: string;
  success_values?: string;
  error_path?: string;
  subtract_path?: string;
  extract_mode?: string;
  window_types?: string;
  window_reset_hours?: number;
  extra_params?: string;
  sign_region?: string;
  sign_service?: string;
}

export interface CustomProviderTestPayload extends CustomProviderPayload {
  api_key?: string;
}

export interface CustomProviderTestResponse {
  status: 'success';
  message: string;
  value: number;
  type: BalanceType;
}

// ==================== Costs ====================

export interface ProjectSpend {
  project: string;
  provider: string;
  currency: string | null;
  spend: number;
  spend_base: number;
  budget?: number;
  projected: number;
  over_budget: boolean;
}

export interface CostSummary {
  month: string;
  base_currency: string;
  days_elapsed: number;
  days_in_month: number;
  projects: ProjectSpend[];
  total_base: number;
  unknown_fx?: string[];
}

export interface CostsSummaryResponse {
  status: 'success';
  summary: CostSummary;
}

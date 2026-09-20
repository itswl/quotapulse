/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 * Implementation note.
 */

// Implementation note.

/* Implementation note. */
export type BalanceType = 'balance' | 'credits' | 'quota';

/* Implementation note. */
export type Confidence = 'none' | 'low' | 'medium' | 'high';

/* Implementation note. */
export type CycleType = 'weekly' | 'monthly' | 'yearly' | 'lunar_yearly';

// Implementation note.

/* Implementation note. */
export interface ErrorResponse {
  status: 'error';
  message: string;
  errors?: string[];
}

/* Implementation note. */
export interface MutationResponse {
  status: 'success';
  message?: string;
}

/* Implementation note. */
export type ApiPayload<T> = T | ErrorResponse | null;

// ==================== /api/features ====================

/* Implementation note. */
export interface Features {
  subscriptions: boolean;
  dynamic_config: boolean;
  history: boolean;
  email_scan: boolean;
}

export interface FeaturesResponse {
  status: 'success';
  features: Features;
}

// ==================== /api/credits ====================

/* Implementation note. */
export interface DailySpend {
  date: string; // YYYY-MM-DD
  consumed: number;
}

/* Implementation note. */
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
}

/**
 * Implementation note.
 * Implementation note.
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

/* Implementation note. */
export interface BalanceSummary {
  total: number;
  success: number;
  failed: number;
  need_alarm: number;
}

/* Implementation note. */
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

/* Implementation note. */
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
}

export interface SubscriptionsResponse {
  last_update: string | null;
  subscriptions: SubscriptionResult[];
  summary: Partial<{ total: number; need_alert: number }>;
}

// ==================== /api/config/subscriptions ====================

/* Implementation note. */
export interface SubscriptionConfig {
  name: string;
  owner_project: string | null;
  cycle_type: CycleType;
  renewal_day: number; // Weekly 1-7，Monthly 1-31，Yearly/lunar_yearly MMDD
  alert_days_before: number;
  amount: number;
  enabled: boolean;
  last_renewed_date: string | null;
}

export interface SubscriptionsConfigResponse {
  status: 'success';
  subscriptions: SubscriptionConfig[];
}

/* Implementation note. */
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
}

export interface ProvidersResponse {
  status: 'success';
  providers: ProviderOption[];
}

// ==================== /api/config/projects ====================

/* Implementation note. */
export interface ProjectConfig {
  name: string;
  provider: string;
  api_key: string;
  threshold: number;
  type: BalanceType;
  owner_project: string | null;
  enabled: boolean;
  from_env?: boolean;
}

export interface ProjectsConfigResponse {
  status: 'success';
  projects: ProjectConfig[];
}

/* Implementation note. */
export interface ProjectPayload {
  name: string;
  provider: string;
  type: BalanceType | null;
  owner_project: string | null;
  enabled: boolean;
  threshold?: number;
  api_key?: string;
}

// ==================== /api/config/emails ====================

/* Implementation note. */
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

/* Implementation note. */
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

/* Implementation note. */
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

/* Implementation note. */
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
   * Implementation note.
   * Implementation note.
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
 * Implementation note.
 * Implementation note.
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

/* Implementation note. */
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

/* Implementation note. */
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

/* Implementation note. */
export interface TrendPoint {
  timestamp: string;
  balance: number;
  need_alarm: boolean;
}

/* Implementation note. */
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

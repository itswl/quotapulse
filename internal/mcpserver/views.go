package mcpserver

// Output shapes of the MCP tools. Every result carries Meta; projects are identified the
// same way everywhere (project_id, project_name, provider); failures are ErrorInfo.
//
// This file only describes shapes and pure helpers and must not import internal
// packages; converting from the domain types happens in server.go.

import (
	"strings"
)

// Meta says how fresh a result is and where it came from.
type Meta struct {
	AsOf           string  `json:"as_of" jsonschema:"when this result was produced (RFC 3339, UTC)"`
	Timezone       string  `json:"timezone" jsonschema:"server timezone that dates, schedules and renewal days use"`
	DataUpdatedAt  *string `json:"data_updated_at" jsonschema:"when the underlying data was last refreshed; null if unknown"`
	DataAgeSeconds *int    `json:"data_age_seconds" jsonschema:"seconds since data_updated_at; null if unknown"`
	IsStale        bool    `json:"is_stale" jsonschema:"true when the latest checks are older than three refresh intervals"`
	Source         string  `json:"source" jsonschema:"live_state (latest checks), persisted_history (database), configuration or catalog"`
	Truncated      bool    `json:"truncated,omitempty" jsonschema:"true when more rows matched than one read returns; narrow the time range"`
}

// ErrorInfo describes a failure: a tool call that failed, or one project's failed check.
type ErrorInfo struct {
	Category          string `json:"category" jsonschema:"auth, rate_limited, network, provider, invalid_response, config, invalid_argument, not_found, unavailable or internal"`
	Message           string `json:"message"`
	Retryable         bool   `json:"retryable"`
	RetryAfterSeconds *int   `json:"retry_after_seconds,omitempty" jsonschema:"for check failures, seconds until the next scheduled check"`
	Provider          string `json:"provider,omitempty"`
	UsedCachedData    bool   `json:"used_cached_data,omitempty"`
}

// ErrorOutput is the structuredContent of a failed tool call (isError: true).
type ErrorOutput struct {
	Error ErrorInfo `json:"error"`
}

// ==================== Balances ====================

// Units by balance type. Currencies are the provider account's own and never converted,
// so amounts of different providers are not comparable.
const (
	unitBalance = "account currency, as the provider reports it (not converted)"
	unitCredits = "provider credits"
	unitQuota   = "percent of the plan remaining, 0-100"
)

func unitOf(balanceType string) string {
	switch balanceType {
	case "credits":
		return unitCredits
	case "quota":
		return unitQuota
	default:
		return unitBalance
	}
}

type BalanceCounts struct {
	Total          int `json:"total"`
	Healthy        int `json:"healthy"`
	BelowThreshold int `json:"below_threshold"`
	CheckFailed    int `json:"check_failed"`
	Disabled       int `json:"disabled"`
}

type RunwayView struct {
	Days              *float64 `json:"days" jsonschema:"estimated days until the balance runs out"`
	DepletionDate     *string  `json:"depletion_date" jsonschema:"YYYY-MM-DD in the server timezone"`
	BurnPerDay        *float64 `json:"burn_per_day"`
	MonthlyProjection *float64 `json:"monthly_projection"`
	Confidence        string   `json:"confidence" jsonschema:"none, low, medium or high, by how much history backs the estimate"`
	WindowDays        int      `json:"window_days"`
	ToppedUp          float64  `json:"topped_up" jsonschema:"top-ups inside the window"`
	SpikeRatio        *float64 `json:"spike_ratio" jsonschema:"today's spend over the window's median day"`
}

type CheckView struct {
	Status    string     `json:"status" jsonschema:"ok, failed or disabled"`
	CheckedAt *string    `json:"checked_at"`
	LatencyMs *int64     `json:"latency_ms" jsonschema:"provider call duration; null when served from the response cache"`
	Cached    bool       `json:"cached"`
	Error     *ErrorInfo `json:"error"`
}

type ProjectStatus struct {
	ProjectID    string      `json:"project_id" jsonschema:"stable ID; prefer it for filtering other tools"`
	ProjectName  string      `json:"project_name"`
	Provider     string      `json:"provider"`
	OwnerProject *string     `json:"owner_project"`
	BalanceType  string      `json:"balance_type" jsonschema:"balance, credits or quota"`
	Unit         string      `json:"unit"`
	Status       string      `json:"status" jsonschema:"healthy, below_threshold, check_failed or disabled"`
	Balance      *float64    `json:"balance"`
	Threshold    *float64    `json:"threshold"`
	AlertSent    bool        `json:"alert_sent"`
	Runway       *RunwayView `json:"runway" jsonschema:"null when there is no estimate; runway_note says why"`
	RunwayNote   string      `json:"runway_note,omitempty"`
	Check        CheckView   `json:"check"`
}

type BalanceStatusOutput struct {
	Meta     Meta            `json:"meta"`
	Counts   BalanceCounts   `json:"counts"`
	Projects []ProjectStatus `json:"projects"`
}

type ProviderErrorView struct {
	ProjectID     string    `json:"project_id"`
	ProjectName   string    `json:"project_name"`
	Error         ErrorInfo `json:"error"`
	LastSuccessAt *string   `json:"last_success_at" jsonschema:"the project's newest snapshot in the history; null without one"`
}

type ProviderStatusView struct {
	Provider      string              `json:"provider"`
	Name          string              `json:"name"`
	Status        string              `json:"status" jsonschema:"ok, degraded (some checks fail) or down (every check fails)"`
	Projects      int                 `json:"projects"`
	Failing       int                 `json:"failing"`
	LastCheckedAt *string             `json:"last_checked_at"`
	LastSuccessAt *string             `json:"last_success_at" jsonschema:"the provider's newest snapshot in the history; null without the database"`
	AvgLatencyMs  *int64              `json:"avg_latency_ms" jsonschema:"over the checks that called the provider, not the cached ones"`
	MaxLatencyMs  *int64              `json:"max_latency_ms"`
	Cached        int                 `json:"cached" jsonschema:"checks answered from the response cache"`
	Errors        []ProviderErrorView `json:"errors" jsonschema:"one per failing project"`
}

type ProviderStatusOutput struct {
	Meta      Meta                 `json:"meta"`
	Count     int                  `json:"count"`
	Providers []ProviderStatusView `json:"providers" jsonschema:"providers with monitored projects, failing first"`
}

// ==================== Spending ====================

type DailySpendView struct {
	Date     string  `json:"date" jsonschema:"YYYY-MM-DD in the server timezone"`
	Consumed float64 `json:"consumed"`
	ToppedUp float64 `json:"topped_up"`
}

type SpendProjectView struct {
	ProjectID   string           `json:"project_id"`
	ProjectName string           `json:"project_name"`
	Provider    string           `json:"provider"`
	BalanceType string           `json:"balance_type"`
	Unit        string           `json:"unit"`
	Samples     int              `json:"samples"`
	SpanDays    float64          `json:"span_days" jsonschema:"how many days the snapshots cover"`
	Balance     float64          `json:"balance" jsonschema:"the newest snapshot"`
	Consumed    float64          `json:"consumed" jsonschema:"spending: the sum of the decreases between consecutive snapshots"`
	ToppedUp    float64          `json:"topped_up" jsonschema:"the sum of the increases"`
	TopUps      int              `json:"top_ups"`
	PerDay      *float64         `json:"per_day" jsonschema:"consumed per day of span; null with a single snapshot"`
	Today       *float64         `json:"today" jsonschema:"spending so far today; null without a snapshot today"`
	SpikeRatio  *float64         `json:"spike_ratio" jsonschema:"today's spending over the median earlier day; null with fewer than three earlier days"`
	Daily       []DailySpendView `json:"daily" jsonschema:"oldest first"`
}

type SpendProviderView struct {
	Provider    string  `json:"provider"`
	BalanceType string  `json:"balance_type"`
	Unit        string  `json:"unit"`
	Projects    int     `json:"projects"`
	Consumed    float64 `json:"consumed"`
	ToppedUp    float64 `json:"topped_up"`
}

type SpendSummaryOutput struct {
	Meta     Meta                `json:"meta"`
	Days     int                 `json:"days"`
	Totals   []SpendProviderView `json:"totals" jsonschema:"per provider and balance type; there is no grand total, because providers bill in different units and currencies"`
	Projects []SpendProjectView  `json:"projects" jsonschema:"most spending first"`
	Excluded []string            `json:"excluded" jsonschema:"quota projects, left out because their percentages reset rather than being spent"`
}

// ==================== Subscriptions ====================

type ReminderView struct {
	Due            bool    `json:"due" jsonschema:"inside the reminder window and not paid"`
	State          string  `json:"state" jsonschema:"not_due, pending, sent, cooldown_skipped, failed, dry_run or snoozed"`
	NextEligibleAt *string `json:"next_eligible_at"`
	SnoozedUntil   *string `json:"snoozed_until"`
	LastError      *string `json:"last_error"`
}

type SubscriptionView struct {
	SubscriptionID   string       `json:"subscription_id" jsonschema:"stable ID; subscription reminders in the alert history carry it as project_id"`
	Name             string       `json:"name"`
	OwnerProject     *string      `json:"owner_project"`
	CycleType        string       `json:"cycle_type" jsonschema:"weekly, monthly, yearly or lunar_yearly"`
	RenewalDay       int          `json:"renewal_day" jsonschema:"weekly 1-7 (Mon-Sun), monthly 1-31, yearly MMDD, lunar_yearly a lunar MMDD; next_renewal_date is always Gregorian"`
	NextRenewalDate  string       `json:"next_renewal_date" jsonschema:"YYYY-MM-DD in the server timezone"`
	DaysUntilRenewal int          `json:"days_until_renewal"`
	Amount           float64      `json:"amount"`
	AlreadyRenewed   bool         `json:"already_renewed" jsonschema:"a renewal mark pays for the upcoming renewal"`
	LastRenewedDate  *string      `json:"last_renewed_date"`
	Reminder         ReminderView `json:"reminder"`
}

type SubscriptionCounts struct {
	Total   int `json:"total"`
	Due     int `json:"due"`
	Renewed int `json:"renewed"`
}

type SubscriptionStatusOutput struct {
	Meta          Meta               `json:"meta"`
	Counts        SubscriptionCounts `json:"counts"`
	Subscriptions []SubscriptionView `json:"subscriptions"`
}

type UpcomingRenewalsOutput struct {
	Meta     Meta               `json:"meta"`
	Days     int                `json:"days"`
	Count    int                `json:"count"`
	Renewals []SubscriptionView `json:"renewals"`
}

// ==================== Email ====================

type MailboxScanView struct {
	Name        string  `json:"name"`
	Host        string  `json:"host"`
	Port        int     `json:"port"`
	Username    string  `json:"username" jsonschema:"masked"`
	Success     bool    `json:"success"`
	Error       *string `json:"error"`
	TotalEmails int     `json:"total_emails"`
	AlertCount  int     `json:"alert_count"`
}

type EmailAlertView struct {
	Mailbox     string   `json:"mailbox"`
	Subject     string   `json:"subject"`
	Sender      string   `json:"sender"`
	Date        string   `json:"date" jsonschema:"the email's own Date header"`
	Keywords    []string `json:"keywords"`
	ServiceName *string  `json:"service_name"`
	Amount      *float64 `json:"amount"`
	AlertSent   bool     `json:"alert_sent"`
	Duplicate   bool     `json:"duplicate" jsonschema:"already notified in an earlier scan, so skipped"`
}

type EmailSummaryView struct {
	TotalMailboxes  int `json:"total_mailboxes"`
	FailedMailboxes int `json:"failed_mailboxes"`
	TotalEmails     int `json:"total_emails"`
	TotalAlerts     int `json:"total_alerts"`
	AlertsSent      int `json:"alerts_sent"`
}

type EmailScanOutput struct {
	Meta       Meta              `json:"meta"`
	Status     string            `json:"status" jsonschema:"active, configured_but_inactive or not_scanned_yet"`
	Reason     string            `json:"reason,omitempty"`
	Days       *int              `json:"days" jsonschema:"how many recent days the last scan covered"`
	DryRun     *bool             `json:"dry_run"`
	Mailboxes  []MailboxScanView `json:"mailboxes"`
	Alerts     []EmailAlertView  `json:"alerts"`
	Summary    EmailSummaryView  `json:"summary"`
	LastScanAt *string           `json:"last_scan_at"`
}

// ==================== Jobs and health ====================

type JobView struct {
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	Schedule            string   `json:"schedule"`
	Enabled             bool     `json:"enabled"`
	Status              string   `json:"status" jsonschema:"ok, failing, disabled, scheduled_not_yet_run or configured_but_inactive"`
	Reason              string   `json:"reason,omitempty"`
	NextRun             *string  `json:"next_run"`
	LastRun             *string  `json:"last_run"`
	LastSuccess         *string  `json:"last_success"`
	LastError           *string  `json:"last_error"`
	LastDurationSeconds *float64 `json:"last_duration_seconds"`
	Runs                int      `json:"runs"`
	Failures            int      `json:"failures"`
}

type JobStatusOutput struct {
	Meta    Meta      `json:"meta"`
	Healthy bool      `json:"healthy"`
	Jobs    []JobView `json:"jobs"`
}

type HealthOutput struct {
	Meta          Meta      `json:"meta"`
	Status        string    `json:"status" jsonschema:"healthy or degraded"`
	Version       string    `json:"version"`
	HasData       bool      `json:"has_data"`
	IsStale       bool      `json:"is_stale"`
	LastUpdate    *string   `json:"last_update"`
	JobsHealthy   bool      `json:"jobs_healthy"`
	FailedJobs    []string  `json:"failed_jobs"`
	Jobs          []JobView `json:"jobs"`
	UptimeSeconds float64   `json:"uptime_seconds"`
}

// ==================== History ====================

type BalanceSnapshotView struct {
	ID             int64    `json:"id"`
	ProjectID      string   `json:"project_id"`
	ProjectName    string   `json:"project_name"`
	Provider       string   `json:"provider"`
	BalanceType    string   `json:"balance_type"`
	Unit           string   `json:"unit"`
	Balance        float64  `json:"balance"`
	Threshold      *float64 `json:"threshold"`
	BelowThreshold bool     `json:"below_threshold"`
	Timestamp      string   `json:"timestamp"`
}

type BalanceHistoryOutput struct {
	Meta       Meta                  `json:"meta"`
	Count      int                   `json:"count"`
	Snapshots  []BalanceSnapshotView `json:"snapshots" jsonschema:"newest first"`
	NextCursor *string               `json:"next_cursor" jsonschema:"pass as cursor for the next page; null on the last page"`
}

type TrendPointView struct {
	Timestamp      string  `json:"timestamp" jsonschema:"the snapshot's time, or the start of the hour, day or week (server timezone)"`
	Balance        float64 `json:"balance" jsonschema:"the snapshot, or the interval's last snapshot"`
	Min            float64 `json:"min"`
	Max            float64 `json:"max"`
	Samples        int     `json:"samples" jsonschema:"snapshots in the interval"`
	BelowThreshold bool    `json:"below_threshold" jsonschema:"the (last) snapshot was below the threshold"`
}

type TrendSummary struct {
	Samples  int     `json:"samples"`
	First    float64 `json:"first"`
	Last     float64 `json:"last"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Average  float64 `json:"average"`
	Change   float64 `json:"change" jsonschema:"last minus first"`
	Consumed float64 `json:"consumed" jsonschema:"sum of the decreases between consecutive snapshots"`
	ToppedUp float64 `json:"topped_up" jsonschema:"sum of the increases: top-ups, refunds, quota resets"`
	TopUps   int     `json:"top_ups" jsonschema:"how many increases there were"`
	FirstAt  string  `json:"first_at"`
	LastAt   string  `json:"last_at"`
}

type TrendOutput struct {
	Meta        Meta             `json:"meta"`
	ProjectID   string           `json:"project_id"`
	ProjectName string           `json:"project_name"`
	Provider    *string          `json:"provider" jsonschema:"null when the project is no longer monitored"`
	Unit        *string          `json:"unit"`
	Days        int              `json:"days"`
	Interval    string           `json:"interval"`
	Threshold   *float64         `json:"threshold"`
	Summary     TrendSummary     `json:"summary" jsonschema:"over every snapshot in the window, whatever the interval and page"`
	Points      []TrendPointView `json:"points" jsonschema:"oldest first"`
	NextCursor  *string          `json:"next_cursor" jsonschema:"pass as cursor for the next page; null on the last page"`
}

type AlertView struct {
	ID          int64    `json:"id"`
	Type        string   `json:"type"`
	Status      string   `json:"status" jsonschema:"sent, or failed when the notification could not be delivered"`
	ProjectID   string   `json:"project_id" jsonschema:"the project's ID; for subscription reminders the subscription_id"`
	ProjectName string   `json:"project_name" jsonschema:"the project or subscription name"`
	Provider    *string  `json:"provider" jsonschema:"null for subscriptions and projects no longer monitored"`
	Message     string   `json:"message"`
	Value       *float64 `json:"value" jsonschema:"the balance for balance alerts, the amount for subscription reminders"`
	Threshold   *float64 `json:"threshold" jsonschema:"the alert threshold, or a reminder's alert_days_before"`
	Timestamp   string   `json:"timestamp"`
}

type AlertsOutput struct {
	Meta       Meta        `json:"meta"`
	Count      int         `json:"count"`
	Alerts     []AlertView `json:"alerts" jsonschema:"newest first"`
	NextCursor *string     `json:"next_cursor" jsonschema:"pass as cursor for the next page; null on the last page"`
}

type EmailAlertRecordView struct {
	ID          int64    `json:"id"`
	Mailbox     string   `json:"mailbox"`
	Sender      string   `json:"sender"`
	Subject     string   `json:"subject"`
	Date        string   `json:"date" jsonschema:"the email's own Date header"`
	ServiceName *string  `json:"service_name"`
	Amount      *float64 `json:"amount"`
	Keywords    []string `json:"keywords"`
	AlertSent   bool     `json:"alert_sent"`
	Timestamp   string   `json:"timestamp" jsonschema:"when the scan recorded it"`
}

type EmailAlertsOutput struct {
	Meta        Meta                   `json:"meta"`
	Count       int                    `json:"count"`
	EmailAlerts []EmailAlertRecordView `json:"email_alerts" jsonschema:"newest first"`
	NextCursor  *string                `json:"next_cursor" jsonschema:"pass as cursor for the next page; null on the last page"`
}

type EventView struct {
	ID        string  `json:"id" jsonschema:"alert:<id> or email:<id>, matching the id in recent_alerts or recent_email_alerts"`
	Type      string  `json:"type"`
	Status    string  `json:"status"`
	ProjectID *string `json:"project_id" jsonschema:"null for email alerts"`
	Provider  *string `json:"provider" jsonschema:"null unless a monitored project"`
	Source    string  `json:"source" jsonschema:"the project, subscription or mailbox the event is about"`
	Message   string  `json:"message"`
	Timestamp string  `json:"timestamp"`
}

type EventsOutput struct {
	Meta       Meta        `json:"meta"`
	Count      int         `json:"count"`
	Events     []EventView `json:"events" jsonschema:"newest first"`
	NextCursor *string     `json:"next_cursor" jsonschema:"pass as cursor for the next page; null on the last page"`
}

type TopProjectView struct {
	ProjectName string `json:"project_name"`
	Alerts      int    `json:"alerts"`
}

type AlertStatsOutput struct {
	Meta        Meta             `json:"meta"`
	Days        int              `json:"days"`
	TotalAlerts int              `json:"total_alerts"`
	ByType      map[string]int   `json:"by_type"`
	TopProjects []TopProjectView `json:"top_projects"`
}

// ==================== Dashboard ====================

type AttentionItem struct {
	Severity  string  `json:"severity" jsonschema:"critical or warning"`
	Kind      string  `json:"kind" jsonschema:"check_failed, below_threshold, runway_short, renewal_due, mailbox_failed, email_alerts, job_failing, stale_data or no_data"`
	Subject   string  `json:"subject" jsonschema:"the project, subscription, mailbox or job concerned"`
	ProjectID *string `json:"project_id"`
	Message   string  `json:"message"`
}

type RunwayBrief struct {
	ProjectID     string  `json:"project_id"`
	ProjectName   string  `json:"project_name"`
	Provider      string  `json:"provider"`
	Days          float64 `json:"days"`
	DepletionDate *string `json:"depletion_date"`
}

type BalanceOverview struct {
	Counts         BalanceCounts `json:"counts"`
	ShortestRunway *RunwayBrief  `json:"shortest_runway" jsonschema:"null when no project has an estimate"`
}

type RenewalBrief struct {
	SubscriptionID   string  `json:"subscription_id"`
	Name             string  `json:"name"`
	NextRenewalDate  string  `json:"next_renewal_date"`
	DaysUntilRenewal int     `json:"days_until_renewal"`
	Amount           float64 `json:"amount"`
}

type SubscriptionOverview struct {
	Total        int           `json:"total"`
	Due          int           `json:"due" jsonschema:"inside their reminder window and not paid"`
	Within7Days  int           `json:"within_7_days" jsonschema:"unpaid renewals in the next 7 days"`
	Within30Days int           `json:"within_30_days" jsonschema:"unpaid renewals in the next 30 days"`
	Next         *RenewalBrief `json:"next" jsonschema:"the next unpaid renewal; null if there is none"`
}

type EmailOverview struct {
	Status          string  `json:"status" jsonschema:"as in email_scan_status"`
	LastScanAt      *string `json:"last_scan_at"`
	AlertEmails     int     `json:"alert_emails" jsonschema:"alert emails the last scan found, repeats excluded"`
	FailedMailboxes int     `json:"failed_mailboxes"`
}

type AlertOverview struct {
	Days   int            `json:"days"`
	Total  int            `json:"total"`
	ByType map[string]int `json:"by_type"`
	Latest []AlertView    `json:"latest" jsonschema:"up to five, newest first"`
}

type ServiceOverview struct {
	Status      string   `json:"status" jsonschema:"healthy or degraded"`
	Version     string   `json:"version"`
	IsStale     bool     `json:"is_stale"`
	FailingJobs []string `json:"failing_jobs"`
}

type DashboardSummaryOutput struct {
	Meta          Meta                  `json:"meta"`
	Status        string                `json:"status" jsonschema:"ok, attention (something needs action) or degraded (the service itself has a problem)"`
	Attention     []AttentionItem       `json:"attention" jsonschema:"what needs action, critical first"`
	Balances      *BalanceOverview      `json:"balances" jsonschema:"null without the balance scope"`
	Subscriptions *SubscriptionOverview `json:"subscriptions" jsonschema:"null without the subscriptions scope"`
	Email         *EmailOverview        `json:"email" jsonschema:"null without the email scope"`
	Alerts        *AlertOverview        `json:"alerts" jsonschema:"the last seven days; null without the alerts scope or the database"`
	Service       ServiceOverview       `json:"service"`
}

// ==================== Catalog and configuration ====================

type Conventions struct {
	Units       map[string]string `json:"units" jsonschema:"what balance measures, by balance_type"`
	Timezone    string            `json:"timezone"`
	RenewalDays string            `json:"renewal_days"`
	ProjectIDs  string            `json:"project_ids"`
}

type CapabilitiesOutput struct {
	Meta         Meta            `json:"meta"`
	Version      string          `json:"version"`
	Features     map[string]bool `json:"features"`
	ConfigCounts map[string]int  `json:"config_counts"`
	Caller       string          `json:"caller" jsonschema:"name of the key this session uses"`
	Scopes       []string        `json:"scopes" jsonschema:"data this key may read"`
	Conventions  Conventions     `json:"conventions"`
}

type ProviderInfo struct {
	Provider    string `json:"provider" jsonschema:"identifier used in filters and project results"`
	Name        string `json:"name"`
	DefaultType string `json:"default_type"`
	Unit        string `json:"unit"`
}

type ProvidersOutput struct {
	Meta      Meta           `json:"meta"`
	Count     int            `json:"count"`
	Providers []ProviderInfo `json:"providers"`
}

type ProjectConfigView struct {
	ProjectID        string  `json:"project_id"`
	ProjectName      string  `json:"project_name"`
	Provider         string  `json:"provider"`
	BalanceType      string  `json:"balance_type"`
	Threshold        float64 `json:"threshold"`
	OwnerProject     *string `json:"owner_project"`
	Enabled          bool    `json:"enabled"`
	APIKeyConfigured bool    `json:"api_key_configured"`
	FromEnv          bool    `json:"from_env" jsonschema:"auto-discovered from environment variables"`
}

type ProjectConfigOutput struct {
	Meta     Meta                `json:"meta"`
	Count    int                 `json:"count"`
	Projects []ProjectConfigView `json:"projects"`
}

type MailboxConfigView struct {
	Name               string `json:"name"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username" jsonschema:"masked"`
	UseSSL             bool   `json:"use_ssl"`
	Enabled            bool   `json:"enabled"`
	PasswordConfigured bool   `json:"password_configured"`
	FromEnv            bool   `json:"from_env"`
}

type MailboxConfigOutput struct {
	Meta      Meta                `json:"meta"`
	Count     int                 `json:"count"`
	Mailboxes []MailboxConfigView `json:"mailboxes"`
}

type SubscriptionConfigView struct {
	Name            string  `json:"name"`
	OwnerProject    *string `json:"owner_project"`
	CycleType       string  `json:"cycle_type"`
	RenewalDay      int     `json:"renewal_day"`
	AlertDaysBefore int     `json:"alert_days_before"`
	Amount          float64 `json:"amount"`
	Enabled         bool    `json:"enabled"`
	LastRenewedDate *string `json:"last_renewed_date"`
	Timezone        string  `json:"timezone,omitempty"`
	SnoozedUntil    *string `json:"snoozed_until"`
	WebhookOverride bool    `json:"webhook_override" jsonschema:"reminders go to their own webhook; the URL itself is never returned"`
}

type SubscriptionConfigOutput struct {
	Meta          Meta                     `json:"meta"`
	Count         int                      `json:"count"`
	Subscriptions []SubscriptionConfigView `json:"subscriptions"`
}

// ==================== Masking and classification ====================

// maskEmail keeps a mailbox address recognisable to its owner without handing the full
// address to every agent: the first character of the local part and the domain.
func maskEmail(address string) string {
	local, domain, found := strings.Cut(address, "@")
	if !found {
		if len(address) <= 2 {
			return "***"
		}
		return address[:1] + "***"
	}
	if local == "" {
		return "***@" + domain
	}
	return local[:1] + "***@" + domain
}

// classifyCheckError sorts a (redacted) check error into a category and whether trying
// again later can help. Auth and configuration problems won't fix themselves.
func classifyCheckError(message string) (category string, retryable bool) {
	lower := strings.ToLower(message)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(lower, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("unknown provider", "invalid api key format", "api key format"):
		return "config", false
	case has("http 401", "http 403", "unauthorized", "forbidden", "invalid key", "invalid api key", "invalid token", "access key", "accesskey", "authentication", "invalid ak", "signature"):
		return "auth", false
	case has("http 429", "too many requests", "rate limit", "throttl"):
		return "rate_limited", true
	case has("timeout", "timed out", "deadline exceeded", "connection refused", "connection reset", "no such host", "tls", "eof", "network"):
		return "network", true
	case has("http 5", "service unavailable", "bad gateway", "internal server error"):
		return "provider", true
	case has("not valid json", "could not parse", "parse", "missing", "unexpected"):
		return "invalid_response", false
	default:
		return "provider", true
	}
}

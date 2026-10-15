// Package model defines the domain types shared across modules.
//
// These types double as HTTP API response structures; their JSON tags must
// match the existing frontend contract: values that may be absent use
// pointers and serialize as null rather than 0, so the frontend never reads
// "no data" as "zero balance".
package model

import (
	"crypto/md5"
	"encoding/hex"
	"strings"
)

// Balance types: display units differ, so cross-project comparison should
// look at ratios, not absolute values.
const (
	TypeBalance = "balance" // monetary balance
	TypeCredits = "credits" // platform credits
	TypeQuota   = "quota"   // plan quota as remaining percent
)

// Project is one monitored account.
type Project struct {
	Name          string  `json:"name"`
	Provider      string  `json:"provider"`
	APIKey        string  `json:"api_key"`
	Threshold     float64 `json:"threshold"`
	Type          string  `json:"type"`
	Currency      string  `json:"currency,omitempty"`       // empty = the provider's default; quota types carry none
	MonthlyBudget float64 `json:"monthly_budget,omitempty"` // in the project's currency; 0 = no budget
	OwnerProject  *string `json:"owner_project"`
	Enabled       bool    `json:"enabled"`
	FromEnv       bool    `json:"from_env,omitempty"` // auto-discovered from environment variables; read-only on the dashboard
}

// ID is the project's stable identity across history tables and alert cooldowns.
// Stored history uses exactly this algorithm; changing it treats all existing
// data as a different set of projects: balance curves break, runway restarts
// from zero, cooldown state is lost.
func (p Project) ID() string { return ProjectID(p.Provider, p.Name) }

// ProjectID = md5("provider:name")。
func ProjectID(provider, name string) string {
	// Identities predate the custom: namespace: a project that moved from the
	// built-in adapter to the seeded custom definition keeps its history,
	// runway and cooldowns because the hash ignores the prefix.
	provider = strings.TrimPrefix(provider, "custom:")
	sum := md5.Sum([]byte(provider + ":" + name))
	return hex.EncodeToString(sum[:])
}

// SubscriptionID = md5("subscription:name"), likewise a stable identity of existing data; do not change.
func SubscriptionID(name string) string {
	sum := md5.Sum([]byte("subscription:" + name))
	return hex.EncodeToString(sum[:])
}

// Subscription is one renewal reminder.
type Subscription struct {
	Name            string  `json:"name"`
	OwnerProject    *string `json:"owner_project"`
	CycleType       string  `json:"cycle_type"`  // weekly / monthly / yearly / lunar_yearly
	RenewalDay      int     `json:"renewal_day"` // weekly 1-7, monthly 1-31, yearly MMDD
	AlertDaysBefore int     `json:"alert_days_before"`
	Amount          float64 `json:"amount"`
	Enabled         bool    `json:"enabled"`
	LastRenewedDate *string `json:"last_renewed_date"` // YYYY-MM-DD
	SnoozedUntil    *string `json:"snoozed_until,omitempty"`
	Timezone        string  `json:"timezone,omitempty"`
	WebhookURL      string  `json:"-"`
}

// Balance types: display units differ, so cross-project comparison should
// look at ratios, not absolute values.
const (
	CycleWeekly  = "weekly"
	CycleMonthly = "monthly"
	CycleYearly  = "yearly"
	// CycleLunarYearly is an annual reminder whose renewal_day is a lunar MMDD.
	CycleLunarYearly = "lunar_yearly"
)

// Mailbox is one scanned IMAP inbox.
type Mailbox struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	UseSSL   bool   `json:"use_ssl"`
	Enabled  bool   `json:"enabled"`
	FromEnv  bool   `json:"from_env,omitempty"`
}

// Config is the three business inventories, merged from environment auto-discovery and database dynamic config.
type Config struct {
	Projects      []Project      `json:"projects"`
	Subscriptions []Subscription `json:"subscriptions"`
	Mailboxes     []Mailbox      `json:"email"`
}

// Filters like EnabledProjects keep only enabled entries; disabled entries stay in the inventory for the page to show.
func (c Config) EnabledProjects() []Project {
	out := make([]Project, 0, len(c.Projects))
	for _, p := range c.Projects {
		if p.Enabled {
			out = append(out, p)
		}
	}
	return out
}

func (c Config) EnabledSubscriptions() []Subscription {
	out := make([]Subscription, 0, len(c.Subscriptions))
	for _, s := range c.Subscriptions {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out
}

func (c Config) EnabledMailboxes() []Mailbox {
	out := make([]Mailbox, 0, len(c.Mailboxes))
	for _, m := range c.Mailboxes {
		if m.Enabled {
			out = append(out, m)
		}
	}
	return out
}

// CheckResult is one balance check's outcome and an element of projects[] in
// /api/credits.
//
// On failure Credits / Threshold / NeedAlarm / Cached are nil and serialize
// as null.
type CheckResult struct {
	Project      string   `json:"project"`
	OwnerProject *string  `json:"owner_project"`
	Provider     string   `json:"provider"`
	Type         string   `json:"type"`
	Success      bool     `json:"success"`
	Credits      *float64 `json:"credits"`
	Threshold    *float64 `json:"threshold"`
	NeedAlarm    bool     `json:"need_alarm"`
	AlarmSent    bool     `json:"alarm_sent"`
	Error        *string  `json:"error"`
	Cached       bool     `json:"cached"`
	Runway       *Runway  `json:"runway,omitempty"`
	// CheckedAt is when this check ran (RFC 3339, UTC); LatencyMs is how long the provider
	// call took, absent when the result came from the response cache.
	CheckedAt *string `json:"checked_at,omitempty"`
	LatencyMs *int64  `json:"latency_ms,omitempty"`
}

// BalanceSummary is the dashboard header count.
type BalanceSummary struct {
	Total     int `json:"total"`
	Success   int `json:"success"`
	Failed    int `json:"failed"`
	NeedAlarm int `json:"need_alarm"`
}

// SummarizeBalance aggregates a batch of check results.
func SummarizeBalance(results []CheckResult) BalanceSummary {
	s := BalanceSummary{Total: len(results)}
	for _, r := range results {
		if r.Success {
			s.Success++
		} else {
			s.Failed++
		}
		if r.NeedAlarm {
			s.NeedAlarm++
		}
	}
	return s
}

// DailySpend is spend grouped by local date for runway analysis.
type DailySpend struct {
	Date     string  `json:"date"` // YYYY-MM-DD
	Consumed float64 `json:"consumed"`
}

// Balance types: display units differ, so cross-project comparison should
// look at ratios, not absolute values.
const (
	ConfidenceNone   = "none"
	ConfidenceLow    = "low"
	ConfidenceMedium = "medium"
	ConfidenceHigh   = "high"
)

// Runway is one account's spend profile. Field names double as API JSON keys; the frontend and dashboard read these names.
type Runway struct {
	ProjectID         string       `json:"project_id"`
	ProjectName       string       `json:"project_name"`
	Provider          string       `json:"provider"`
	BalanceType       string       `json:"balance_type"`
	CurrentBalance    *float64     `json:"current_balance"`
	WindowDays        int          `json:"window_days"`
	DataPoints        int          `json:"data_points"`
	SpanHours         float64      `json:"span_hours"`
	Consumed          float64      `json:"consumed"`
	ToppedUp          float64      `json:"topped_up"`
	BurnPerDay        *float64     `json:"burn_per_day"`
	RunwayDays        *float64     `json:"runway_days"`
	DepletionDate     *string      `json:"depletion_date"`
	Confidence        string       `json:"confidence"`
	Daily             []DailySpend `json:"daily"`
	TodayConsumed     *float64     `json:"today_consumed"`
	BaselineSpend     *float64     `json:"baseline_consumed"`
	SpikeRatio        *float64     `json:"spike_ratio"`
	MonthlyProjection *float64     `json:"monthly_projection,omitempty"`
}

// HasEstimate reports whether the profile's daily average spend is usable.
func (r *Runway) HasEstimate() bool {
	return r != nil && r.BurnPerDay != nil && r.Confidence != ConfidenceNone
}

// Alertable reports whether confidence suffices for alerting (spans under a day do not count).
func (r *Runway) Alertable() bool {
	return r != nil && (r.Confidence == ConfidenceMedium || r.Confidence == ConfidenceHigh)
}

// SubscriptionResult is one subscription's check result and an element of /api/subscriptions.
type SubscriptionResult struct {
	Name             string  `json:"name"`
	OwnerProject     *string `json:"owner_project"`
	RenewalDay       int     `json:"renewal_day"`
	CycleType        string  `json:"cycle_type"`
	DaysUntilRenewal int     `json:"days_until_renewal"`
	NextRenewalDate  string  `json:"next_renewal_date"` // YYYY-MM-DD
	NeedAlert        bool    `json:"need_alert"`
	AlertSent        bool    `json:"alert_sent"`
	Amount           float64 `json:"amount"`
	AlreadyRenewed   bool    `json:"already_renewed"`
	LastRenewedDate  *string `json:"last_renewed_date"`
	AlertState       string  `json:"alert_state,omitempty"`
	NextEligibleAt   *string `json:"next_eligible_at,omitempty"`
	LastError        string  `json:"last_error,omitempty"`
	SnoozedUntil     *string `json:"snoozed_until,omitempty"`
}

// MailboxResult is one mailbox's connection and stats for a scan.
type MailboxResult struct {
	Name        string  `json:"name"`
	Host        string  `json:"host"`
	Port        int     `json:"port"`
	Username    string  `json:"username"`
	TotalEmails int     `json:"total_emails"`
	AlertCount  int     `json:"alert_count"`
	Success     bool    `json:"success"`
	Error       *string `json:"error"`
}

// EmailAlert is one mail that hit a keyword.
type EmailAlert struct {
	Mailbox     string   `json:"mailbox"`
	Subject     string   `json:"subject"`
	Sender      string   `json:"sender"`
	Date        string   `json:"date"`
	Keywords    []string `json:"keywords"`
	ServiceName *string  `json:"service_name"`
	Amount      *float64 `json:"amount"`
	AlertSent   bool     `json:"alert_sent"`
	// Duplicate means this mail already notified recently and is skipped. The
	// dashboard shows an "already notified" badge so nobody thinks it was missed.
	Duplicate bool `json:"duplicate,omitempty"`
}

// ScanResult is the complete result of one mailbox scan.
type ScanResult struct {
	Days      int             `json:"days"`
	DryRun    bool            `json:"dry_run"`
	Mailboxes []MailboxResult `json:"mailboxes"`
	Alerts    []EmailAlert    `json:"alerts"`
}

// BalancePoint is one snapshot in balance history, the runway analysis input.
type BalancePoint struct {
	ProjectID   string
	ProjectName string
	Provider    string
	BalanceType string
	Balance     float64
	Threshold   *float64
	NeedAlarm   bool
	Timestamp   int64 // Unix seconds, UTC
}

// NormalizeProject fills omitted fields: provider lowercased, name defaults to the provider, type derived from the provider.
func NormalizeProject(p *Project) {
	p.Provider = strings.ToLower(strings.TrimSpace(p.Provider))
	if p.Name == "" {
		p.Name = p.Provider
		if p.Name == "" {
			p.Name = "unknown"
		}
	}
	if p.Type == "" {
		p.Type = DefaultBalanceType(p.Provider)
	}
}

// DefaultBalanceType derives the display balance type from the provider.
func DefaultBalanceType(provider string) string {
	switch provider {
	case "openrouter", "uniapi", "wxrank":
		return TypeCredits
	case "glm":
		return TypeQuota
	default:
		return TypeBalance
	}
}

// Ptr takes the address of an optional field without temporary variables everywhere.
func Ptr[T any](v T) *T { return &v }

// OwnerProjectOf normalizes empty strings (whitespace-only included) to nil; "no owning project" has exactly one representation.
func OwnerProjectOf(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// EmailSuppression marks a mailbox+sender pair whose billing emails should not trigger
// notifications (false positives).
type EmailSuppression struct {
	Mailbox string `json:"mailbox"`
	Sender  string `json:"sender"`
}

// PushSubscription is one browser's Web Push registration.
type PushSubscription struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

// CustomProvider is a zero-code platform integration defined on the page: one
// GET, a JSON path to the balance, and how the API key travels.
type CustomProvider struct {
	Name          string  `json:"name"`
	DisplayName   string  `json:"display_name"`
	URL           string  `json:"url"`
	AuthMode      string  `json:"auth_mode"`
	AuthParam     string  `json:"auth_param,omitempty"`
	DefaultType   string  `json:"default_type"`
	Currency      string  `json:"currency,omitempty"`
	BalancePath   string  `json:"balance_path"`
	BalanceScale  float64 `json:"balance_scale,omitempty"`
	SuccessPath   string  `json:"success_path,omitempty"`
	SuccessValues string  `json:"success_values,omitempty"`
	ErrorPath     string  `json:"error_path,omitempty"`
	// SubtractPath (optional): the value is balance_path − subtract_path —
	// platforms that report cumulative top-ups and spend (OpenRouter style).
	SubtractPath string `json:"subtract_path,omitempty"`
	// ExtractMode selects the pipeline: "path" (default) or "min_window".
	// min_window picks the quota windows resetting soonest and reports the
	// lowest remaining percentage (GLM Coding Plan style).
	ExtractMode string `json:"extract_mode,omitempty"`
	// WindowTypes limits min_window to these entry types (CSV); empty means
	// TOKENS_LIMIT/CREDIT_LIMIT. WindowResetHours is how far out a reset may
	// sit and still count as rolling (default 6).
	WindowTypes      string  `json:"window_types,omitempty"`
	WindowResetHours float64 `json:"window_reset_hours,omitempty"`
	// ExtraParams adds fixed query parameters, "k=v,k2=v2" — API-mandated
	// units, actions and versions live here instead of a fat URL.
	ExtraParams string `json:"extra_params,omitempty"`
	// SignRegion / SignService parameterize the volc_v4 signing preset.
	SignRegion  string `json:"sign_region,omitempty"`
	SignService string `json:"sign_service,omitempty"`
	FromDB      bool   `json:"from_db"`
}

// CustomProviderKey is the registry key of a custom provider.
func CustomProviderKey(name string) string { return "custom:" + name }

// AlertBudget is the budget-pace alert's cooldown key.
const AlertBudget = "budget"

// UsageEntry is one aggregated day/model cell reported by a local tool (the
// Claude Code reporter, a cron script). Re-reporting the same cell overwrites
// it, so reporters can run as often as they like.
type UsageEntry struct {
	Source       string  `json:"source"`   // reporter label, e.g. claude-code
	Project      string  `json:"project"`  // groups into the cost view
	Date         string  `json:"date"`     // YYYY-MM-DD, local to the reporter
	Model        string  `json:"model"`    // model or "all"
	Cost         float64 `json:"cost"`     // native currency
	Currency     string  `json:"currency"` // ISO code
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	Requests     int64   `json:"requests"`
}

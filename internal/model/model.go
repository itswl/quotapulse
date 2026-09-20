// Package model provides the package implementation.
//
// Implementation note.
// Implementation note.
package model

import (
	"crypto/md5"
	"encoding/hex"
	"strings"
)

// Implementation note.
const (
	TypeBalance = "balance" // operation
	TypeCredits = "credits" // operation
	TypeQuota   = "quota"   // operation
)

// Implementation note.
type Project struct {
	Name         string  `json:"name"`
	Provider     string  `json:"provider"`
	APIKey       string  `json:"api_key"`
	Threshold    float64 `json:"threshold"`
	Type         string  `json:"type"`
	OwnerProject *string `json:"owner_project"`
	Enabled      bool    `json:"enabled"`
	FromEnv      bool    `json:"from_env,omitempty"` // environment variableauto-discovered,operation
}

// Implementation note.
// Implementation note.
// Implementation note.
func (p Project) ID() string { return ProjectID(p.Provider, p.Name) }

// ProjectID = md5("provider:name")。
func ProjectID(provider, name string) string {
	sum := md5.Sum([]byte(provider + ":" + name))
	return hex.EncodeToString(sum[:])
}

// Implementation note.
func SubscriptionID(name string) string {
	sum := md5.Sum([]byte("subscription:" + name))
	return hex.EncodeToString(sum[:])
}

// Implementation note.
type Subscription struct {
	Name            string  `json:"name"`
	OwnerProject    *string `json:"owner_project"`
	CycleType       string  `json:"cycle_type"`  // weekly / monthly / yearly / lunar_yearly
	RenewalDay      int     `json:"renewal_day"` // operation 1-7,operation 1-31,operation MMDD
	AlertDaysBefore int     `json:"alert_days_before"`
	Amount          float64 `json:"amount"`
	Enabled         bool    `json:"enabled"`
	LastRenewedDate *string `json:"last_renewed_date"` // YYYY-MM-DD
}

// Implementation note.
const (
	CycleWeekly  = "weekly"
	CycleMonthly = "monthly"
	CycleYearly  = "yearly"
	// CycleLunarYearly is an annual reminder whose renewal_day is a lunar MMDD.
	CycleLunarYearly = "lunar_yearly"
)

// Implementation note.
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

// Implementation note.
type Config struct {
	Projects      []Project      `json:"projects"`
	Subscriptions []Subscription `json:"subscriptions"`
	Mailboxes     []Mailbox      `json:"email"`
}

// Implementation note.
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

// Implementation note.
//
// Implementation note.
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
}

// Implementation note.
type BalanceSummary struct {
	Total     int `json:"total"`
	Success   int `json:"success"`
	Failed    int `json:"failed"`
	NeedAlarm int `json:"need_alarm"`
}

// Implementation note.
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

// Implementation note.
type DailySpend struct {
	Date     string  `json:"date"` // YYYY-MM-DD
	Consumed float64 `json:"consumed"`
}

// Implementation note.
const (
	ConfidenceNone   = "none"
	ConfidenceLow    = "low"
	ConfidenceMedium = "medium"
	ConfidenceHigh   = "high"
)

// Implementation note.
type Runway struct {
	ProjectID      string       `json:"project_id"`
	ProjectName    string       `json:"project_name"`
	Provider       string       `json:"provider"`
	BalanceType    string       `json:"balance_type"`
	CurrentBalance *float64     `json:"current_balance"`
	WindowDays     int          `json:"window_days"`
	DataPoints     int          `json:"data_points"`
	SpanHours      float64      `json:"span_hours"`
	Consumed       float64      `json:"consumed"`
	ToppedUp       float64      `json:"topped_up"`
	BurnPerDay     *float64     `json:"burn_per_day"`
	RunwayDays     *float64     `json:"runway_days"`
	DepletionDate  *string      `json:"depletion_date"`
	Confidence     string       `json:"confidence"`
	Daily          []DailySpend `json:"daily"`
	TodayConsumed  *float64     `json:"today_consumed"`
	BaselineSpend  *float64     `json:"baseline_consumed"`
	SpikeRatio     *float64     `json:"spike_ratio"`
}

// Implementation note.
func (r *Runway) HasEstimate() bool {
	return r != nil && r.BurnPerDay != nil && r.Confidence != ConfidenceNone
}

// Implementation note.
func (r *Runway) Alertable() bool {
	return r != nil && (r.Confidence == ConfidenceMedium || r.Confidence == ConfidenceHigh)
}

// Implementation note.
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
}

// Implementation note.
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

// Implementation note.
type EmailAlert struct {
	Mailbox     string   `json:"mailbox"`
	Subject     string   `json:"subject"`
	Sender      string   `json:"sender"`
	Date        string   `json:"date"`
	Keywords    []string `json:"keywords"`
	ServiceName *string  `json:"service_name"`
	Amount      *float64 `json:"amount"`
	AlertSent   bool     `json:"alert_sent"`
	// Implementation note.
	// Implementation note.
	Duplicate bool `json:"duplicate,omitempty"`
}

// Implementation note.
type ScanResult struct {
	Days      int             `json:"days"`
	DryRun    bool            `json:"dry_run"`
	Mailboxes []MailboxResult `json:"mailboxes"`
	Alerts    []EmailAlert    `json:"alerts"`
}

// Implementation note.
type BalancePoint struct {
	ProjectID   string
	ProjectName string
	Provider    string
	BalanceType string
	Balance     float64
	Threshold   *float64
	NeedAlarm   bool
	Timestamp   int64 // Unix operation,UTC
}

// Implementation note.
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

// Implementation note.
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

// Implementation note.
func Ptr[T any](v T) *T { return &v }

// Implementation note.
func OwnerProjectOf(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

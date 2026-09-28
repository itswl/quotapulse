// Package store provides the package implementation.
//
// Implementation note.
// Implementation note.
package store

import (
	"context"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

// Implementation note.
//
// Implementation note.
// Implementation note.
type Store interface {
	// Implementation note.

	ListProjects(ctx context.Context) ([]model.Project, error)
	UpsertProject(ctx context.Context, p model.Project) error
	DeleteProject(ctx context.Context, name string) error

	ListSubscriptions(ctx context.Context) ([]model.Subscription, error)
	UpsertSubscription(ctx context.Context, s model.Subscription) error
	DeleteSubscription(ctx context.Context, name string) error

	ListMailboxes(ctx context.Context) ([]model.Mailbox, error)
	UpsertMailbox(ctx context.Context, m model.Mailbox) error
	DeleteMailbox(ctx context.Context, name string) error

	// Implementation note.

	SaveBalance(ctx context.Context, rec BalanceRecord) error
	// Implementation note.
	BalanceSeries(ctx context.Context, days int) ([]model.BalancePoint, error)
	BalanceHistory(ctx context.Context, q BalanceQuery) ([]BalanceRow, error)
	// Implementation note.
	BalanceTrend(ctx context.Context, projectID string, days int) (*Trend, error)

	// Implementation note.

	SaveAlert(ctx context.Context, rec AlertRecord) error
	// Implementation note.
	HasRecentAlert(ctx context.Context, alertID, alertType string, within time.Duration) (bool, error)
	LastSentAlert(ctx context.Context, alertID, alertType string, within time.Duration) (*time.Time, error)
	Backup(ctx context.Context, dir string, keep int) (string, error)
	SetSubscriptionSnooze(ctx context.Context, name string, snoozedUntil string) error
	UpdateSubscriptionTimezone(ctx context.Context, name string, timezone string) error
	UpdateSubscriptionWebhook(ctx context.Context, name string, webhookURL string) error
	ListEmailSuppressions(ctx context.Context) ([]model.EmailSuppression, error)
	AddEmailSuppression(ctx context.Context, mailbox, sender string) error
	DeleteEmailSuppression(ctx context.Context, mailbox, sender string) error
	ListPushSubscriptions(ctx context.Context) ([]model.PushSubscription, error)
	UpsertPushSubscription(ctx context.Context, sub model.PushSubscription) error
	DeletePushSubscription(ctx context.Context, endpoint string) error
	GetAppSetting(ctx context.Context, key string) (string, error)
	SetAppSetting(ctx context.Context, key, value string) error
	RecentAlerts(ctx context.Context, q AlertQuery) ([]AlertRow, error)
	AlertStats(ctx context.Context, days int) (*Stats, error)

	// Implementation note.

	SaveEmailAlert(ctx context.Context, rec EmailAlertRecord) error
	HasRecentEmailAlert(ctx context.Context, mailbox, sender, subject, date string, days int) (bool, error)
	EmailAlerts(ctx context.Context, q EmailAlertQuery) ([]EmailAlertRow, error)

	// Implementation note.
	Enabled() bool
	Close() error
}

// Implementation note.

// Implementation note.
type BalanceRecord struct {
	ProjectID   string
	ProjectName string
	Provider    string
	Balance     float64
	Threshold   *float64
	BalanceType string
	NeedAlarm   bool
}

// Implementation note.
type AlertRecord struct {
	AlertID   string // operation ProjectID,operation SubscriptionID
	Name      string
	AlertType string // low_balance / low_runway / spend_spike / subscription_renewal / email_alert
	Message   string
	Value     *float64
	Threshold *float64
	Status    string // operation sent operation
}

// Implementation note.
type EmailAlertRecord struct {
	Mailbox     string
	Sender      string
	Subject     string
	Date        string
	ServiceName *string
	Amount      *float64
	Keywords    []string
	AlertSent   bool
}

// Implementation note.

// Implementation note.
type BalanceQuery struct {
	ProjectID string
	Provider  string
	Days      int
	Limit     int
}

// Implementation note.
type AlertQuery struct {
	ProjectID string
	AlertType string
	Days      int
	Limit     int
}

// Implementation note.
type EmailAlertQuery struct {
	Mailbox string
	Days    int
	Limit   int
}

// Implementation note.
type BalanceRow struct {
	ID          int64    `json:"id"`
	ProjectID   string   `json:"project_id"`
	ProjectName string   `json:"project_name"`
	Provider    string   `json:"provider"`
	Balance     float64  `json:"balance"`
	Threshold   *float64 `json:"threshold"`
	BalanceType string   `json:"balance_type"`
	NeedAlarm   bool     `json:"need_alarm"`
	Timestamp   string   `json:"timestamp"`
}

// Implementation note.
type AlertRow struct {
	ID             int64    `json:"id"`
	ProjectID      string   `json:"project_id"`
	ProjectName    string   `json:"project_name"`
	AlertType      string   `json:"alert_type"`
	Status         string   `json:"status"`
	Message        string   `json:"message"`
	BalanceValue   *float64 `json:"balance_value"`
	ThresholdValue *float64 `json:"threshold_value"`
	Timestamp      string   `json:"timestamp"`
}

// Implementation note.
type EmailAlertRow struct {
	ID              int64    `json:"id"`
	Mailbox         string   `json:"mailbox"`
	Sender          string   `json:"sender"`
	Subject         string   `json:"subject"`
	Date            string   `json:"date"`
	ServiceName     *string  `json:"service_name"`
	Amount          *float64 `json:"amount"`
	MatchedKeywords *string  `json:"matched_keywords"` // JSON operation
	AlertSent       bool     `json:"alert_sent"`
	Timestamp       string   `json:"timestamp"`
}

// Implementation note.
type TrendPoint struct {
	Timestamp string  `json:"timestamp"`
	Balance   float64 `json:"balance"`
	NeedAlarm bool    `json:"need_alarm"`
}

// Implementation note.
type Trend struct {
	ProjectID      string       `json:"project_id"`
	ProjectName    string       `json:"project_name"`
	Days           int          `json:"days"`
	DataPoints     int          `json:"data_points"`
	CurrentBalance float64      `json:"current_balance"`
	MinBalance     float64      `json:"min_balance"`
	MaxBalance     float64      `json:"max_balance"`
	AvgBalance     float64      `json:"avg_balance"`
	Threshold      float64      `json:"threshold"`
	FirstTimestamp string       `json:"first_timestamp"`
	LastTimestamp  string       `json:"last_timestamp"`
	History        []TrendPoint `json:"history"`
	Change         *float64     `json:"change,omitempty"`
	ChangePercent  *float64     `json:"change_percent,omitempty"`
}

// Implementation note.
type TopProject struct {
	Project string `json:"project"`
	Count   int    `json:"count"`
}

// Implementation note.
type Stats struct {
	Days        int            `json:"days"`
	TotalAlerts int            `json:"total_alerts"`
	ByType      map[string]int `json:"by_type"`
	TopProjects []TopProject   `json:"top_projects"`
}

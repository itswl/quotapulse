package store

import (
	"context"
	"database/sql"
	"time"
)

// Implementation note.
//
// Implementation note.
// Implementation note.
// Implementation note.
//
// Implementation note.
// Implementation note.

type projectConfigRow struct {
	ID           int64
	Name         string
	OwnerProject sql.NullString
	Provider     string
	ApiKey       string
	Threshold    sql.NullFloat64
	Type         sql.NullString
	Enabled      sql.NullBool
	CreatedAt    sql.NullTime
	UpdatedAt    sql.NullTime
}

type subscriptionConfigRow struct {
	ID              int64
	Name            string
	OwnerProject    sql.NullString
	CycleType       sql.NullString
	RenewalDay      sql.NullInt64
	AlertDaysBefore sql.NullInt64
	Amount          sql.NullFloat64
	Enabled         sql.NullBool
	LastRenewedDate sql.NullString
	SnoozedUntil    sql.NullString
	Timezone        sql.NullString
	WebhookUrl      sql.NullString
	CreatedAt       sql.NullTime
	UpdatedAt       sql.NullTime
}

type emailSuppressionRow struct {
	Mailbox string
	Sender  string
}

type setSubscriptionSnoozeParams struct {
	SnoozedUntil string
	Name         string
}

type updateSubscriptionTimezoneParams struct {
	Timezone string
	Name     string
}

type updateSubscriptionWebhookParams struct {
	WebhookUrl string
	Name       string
}

type addEmailSuppressionParams struct {
	Mailbox   string
	Sender    string
	CreatedAt time.Time
}

type deleteEmailSuppressionParams struct {
	Mailbox string
	Sender  string
}

type pushSubscriptionRow struct {
	Endpoint string
	P256dh   string
	Auth     string
}

type upsertPushSubscriptionParams struct {
	Endpoint  string
	P256dh    string
	Auth      string
	CreatedAt sql.NullTime
}

type deletePushSubscriptionParams struct {
	Endpoint string
}

type setAppSettingParams struct {
	SettingKey   string
	SettingValue string
}

type emailConfigRow struct {
	ID        int64
	Name      string
	Host      string
	Port      sql.NullInt64
	Username  string
	Password  string
	UseSsl    sql.NullBool
	Enabled   sql.NullBool
	CreatedAt sql.NullTime
	UpdatedAt sql.NullTime
}

type balanceHistoryRow struct {
	ID          int64
	ProjectID   string
	ProjectName string
	Provider    string
	Balance     float64
	Threshold   sql.NullFloat64
	BalanceType sql.NullString
	NeedAlarm   sql.NullBool
	Timestamp   sql.NullTime
}

type alertHistoryRow struct {
	ID             int64
	ProjectID      string
	ProjectName    string
	AlertType      string
	Status         sql.NullString
	Message        sql.NullString
	BalanceValue   sql.NullFloat64
	ThresholdValue sql.NullFloat64
	Timestamp      sql.NullTime
}

type emailAlertHistoryRow struct {
	ID              int64
	Mailbox         string
	Sender          string
	Subject         string
	Date            string
	ServiceName     sql.NullString
	Amount          sql.NullFloat64
	MatchedKeywords sql.NullString
	AlertSent       sql.NullBool
	Timestamp       sql.NullTime
}

type typeCountRow struct {
	AlertType string
	Count     int64
}

type projectCountRow struct {
	ProjectName string
	Count       int64
}

// Implementation note.

type upsertProjectParams struct {
	Name         string
	OwnerProject sql.NullString
	Provider     string
	ApiKey       string
	Threshold    sql.NullFloat64
	Type         sql.NullString
	Enabled      sql.NullBool
	CreatedAt    sql.NullTime
	UpdatedAt    sql.NullTime
}

type upsertSubscriptionParams struct {
	Name            string
	OwnerProject    sql.NullString
	CycleType       sql.NullString
	RenewalDay      sql.NullInt64
	AlertDaysBefore sql.NullInt64
	Amount          sql.NullFloat64
	Enabled         sql.NullBool
	LastRenewedDate sql.NullString
	CreatedAt       sql.NullTime
	UpdatedAt       sql.NullTime
}

type upsertEmailParams struct {
	Name      string
	Host      string
	Port      sql.NullInt64
	Username  string
	Password  string
	UseSsl    sql.NullBool
	Enabled   sql.NullBool
	CreatedAt sql.NullTime
	UpdatedAt sql.NullTime
}

type insertBalanceParams struct {
	ProjectID   string
	ProjectName string
	Provider    string
	Balance     float64
	Threshold   sql.NullFloat64
	BalanceType sql.NullString
	NeedAlarm   sql.NullBool
	Timestamp   sql.NullTime
}

type insertAlertParams struct {
	ProjectID      string
	ProjectName    string
	AlertType      string
	Status         sql.NullString
	Message        sql.NullString
	BalanceValue   sql.NullFloat64
	ThresholdValue sql.NullFloat64
	Timestamp      sql.NullTime
}

type insertEmailAlertParams struct {
	Mailbox         string
	Sender          string
	Subject         string
	Date            string
	ServiceName     sql.NullString
	Amount          sql.NullFloat64
	MatchedKeywords sql.NullString
	AlertSent       sql.NullBool
	Timestamp       sql.NullTime
}

// Implementation note.
//
// Implementation note.
// Implementation note.
type querier interface {
	listProjectConfigs(ctx context.Context) ([]projectConfigRow, error)
	upsertProjectConfig(ctx context.Context, arg upsertProjectParams) error
	deleteProjectConfig(ctx context.Context, name string) error
	updateProjectAPIKey(ctx context.Context, name, apiKey string) error

	listSubscriptionConfigs(ctx context.Context) ([]subscriptionConfigRow, error)
	upsertSubscriptionConfig(ctx context.Context, arg upsertSubscriptionParams) error
	deleteSubscriptionConfig(ctx context.Context, name string) error

	listEmailConfigs(ctx context.Context) ([]emailConfigRow, error)
	upsertEmailConfig(ctx context.Context, arg upsertEmailParams) error
	deleteEmailConfig(ctx context.Context, name string) error
	updateEmailPassword(ctx context.Context, name, password string) error

	insertBalance(ctx context.Context, arg insertBalanceParams) error
	listBalanceSeries(ctx context.Context, since time.Time) ([]balanceHistoryRow, error)
	listBalanceTrend(ctx context.Context, projectID string, since time.Time) ([]balanceHistoryRow, error)
	listBalanceHistory(ctx context.Context, since time.Time, projectID, provider string, limit int64) ([]balanceHistoryRow, error)

	insertAlert(ctx context.Context, arg insertAlertParams) error
	countRecentAlerts(ctx context.Context, alertID, alertType, status string, since time.Time) (int64, error)
	listAlertHistory(ctx context.Context, since time.Time, projectID, alertType string, limit int64) ([]alertHistoryRow, error)
	countAlerts(ctx context.Context, since time.Time) (int64, error)
	countAlertsByType(ctx context.Context, since time.Time) ([]typeCountRow, error)
	countAlertsByProject(ctx context.Context, since time.Time) ([]projectCountRow, error)

	insertEmailAlert(ctx context.Context, arg insertEmailAlertParams) error
	countRecentEmailAlerts(ctx context.Context, mailbox, sender, subject, date string, since time.Time) (int64, error)
	setSubscriptionSnooze(ctx context.Context, arg setSubscriptionSnoozeParams) error
	updateSubscriptionTimezone(ctx context.Context, arg updateSubscriptionTimezoneParams) error
	updateSubscriptionWebhook(ctx context.Context, arg updateSubscriptionWebhookParams) error
	addEmailSuppression(ctx context.Context, arg addEmailSuppressionParams) error
	deleteEmailSuppression(ctx context.Context, arg deleteEmailSuppressionParams) error
	listEmailSuppressions(ctx context.Context) ([]emailSuppressionRow, error)
	listPushSubscriptions(ctx context.Context) ([]pushSubscriptionRow, error)
	upsertPushSubscription(ctx context.Context, arg upsertPushSubscriptionParams) error
	deletePushSubscription(ctx context.Context, arg deletePushSubscriptionParams) error
	getAppSetting(ctx context.Context, key string) (string, error)
	setAppSetting(ctx context.Context, arg setAppSettingParams) error
	listEmailAlertHistory(ctx context.Context, since time.Time, mailbox string, limit int64) ([]emailAlertHistoryRow, error)
}

// Implementation note.
func mapRows[S, D any](src []S, conv func(S) D) []D {
	out := make([]D, len(src))
	for i := range src {
		out[i] = conv(src[i])
	}
	return out
}

// Implementation note.
func nullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t.UTC(), Valid: true}
}

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
func Null() Store { return nullStore{} }

type nullStore struct{}

func (nullStore) ListProjects(context.Context) ([]model.Project, error)           { return nil, nil }
func (nullStore) UpsertProject(context.Context, model.Project) error              { return ErrDisabled }
func (nullStore) DeleteProject(context.Context, string) error                     { return ErrDisabled }
func (nullStore) ListSubscriptions(context.Context) ([]model.Subscription, error) { return nil, nil }
func (nullStore) UpsertSubscription(context.Context, model.Subscription) error    { return ErrDisabled }
func (nullStore) DeleteSubscription(context.Context, string) error                { return ErrDisabled }
func (nullStore) ListMailboxes(context.Context) ([]model.Mailbox, error)          { return nil, nil }
func (nullStore) UpsertMailbox(context.Context, model.Mailbox) error              { return ErrDisabled }
func (nullStore) DeleteMailbox(context.Context, string) error                     { return ErrDisabled }

func (nullStore) SaveBalance(context.Context, BalanceRecord) error { return nil }

func (nullStore) BalanceSeries(context.Context, int) ([]model.BalancePoint, error) { return nil, nil }

func (nullStore) BalanceHistory(context.Context, BalanceQuery) ([]BalanceRow, error) {
	return nil, nil
}

func (nullStore) BalanceTrend(context.Context, string, int) (*Trend, error) { return nil, nil }

func (nullStore) SaveAlert(context.Context, AlertRecord) error { return nil }

func (nullStore) HasRecentAlert(context.Context, string, string, time.Duration) (bool, error) {
	return false, nil
}

func (nullStore) LastSentAlert(context.Context, string, string, time.Duration) (*time.Time, error) {
	return nil, nil
}

func (nullStore) Backup(context.Context, string, int) (string, error) { return "", ErrDisabled }

func (nullStore) SetSubscriptionSnooze(context.Context, string, string) error { return nil }

func (nullStore) UpdateSubscriptionTimezone(context.Context, string, string) error { return nil }

func (nullStore) UpdateSubscriptionWebhook(context.Context, string, string) error { return nil }

func (nullStore) ListEmailSuppressions(context.Context) ([]model.EmailSuppression, error) {
	return nil, nil
}

func (nullStore) AddEmailSuppression(context.Context, string, string) error { return nil }

func (nullStore) DeleteEmailSuppression(context.Context, string, string) error { return nil }

func (nullStore) ListPushSubscriptions(context.Context) ([]model.PushSubscription, error) {
	return nil, nil
}

func (nullStore) UpsertPushSubscription(context.Context, model.PushSubscription) error { return nil }

func (nullStore) DeletePushSubscription(context.Context, string) error { return nil }

func (nullStore) GetAppSetting(context.Context, string) (string, error) { return "", nil }

func (nullStore) SetAppSetting(context.Context, string, string) error { return nil }

func (nullStore) RecentAlerts(context.Context, AlertQuery) ([]AlertRow, error) { return nil, nil }

func (nullStore) AlertStats(context.Context, int) (*Stats, error) { return nil, nil }

func (nullStore) SaveEmailAlert(context.Context, EmailAlertRecord) error { return nil }

func (nullStore) HasRecentEmailAlert(context.Context, string, string, string, string, int) (bool, error) {
	return false, nil
}

func (nullStore) EmailAlerts(context.Context, EmailAlertQuery) ([]EmailAlertRow, error) {
	return nil, nil
}

func (nullStore) Enabled() bool { return false }
func (nullStore) Close() error  { return nil }

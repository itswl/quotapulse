package store

import (
	"context"
	"database/sql"
	"time"

	gen "github.com/itswl/quotapulse/internal/store/sqlc/mysql"
)

// Implementation note.
// Implementation note.
type mysqlQuerier struct{ q *gen.Queries }

func newMySQLQuerier(db *sql.DB) querier { return mysqlQuerier{q: gen.New(db)} }

func (e mysqlQuerier) listProjectConfigs(ctx context.Context) ([]projectConfigRow, error) {
	rows, err := e.q.ListProjectConfigs(ctx)
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(r gen.ProjectConfig) projectConfigRow { return projectConfigRow(r) }), nil
}

func (e mysqlQuerier) upsertProjectConfig(ctx context.Context, arg upsertProjectParams) error {
	return e.q.UpsertProjectConfig(ctx, gen.UpsertProjectConfigParams(arg))
}

func (e mysqlQuerier) deleteProjectConfig(ctx context.Context, name string) error {
	return e.q.DeleteProjectConfig(ctx, name)
}

func (e mysqlQuerier) updateProjectAPIKey(ctx context.Context, name, apiKey string) error {
	return e.q.UpdateProjectAPIKey(ctx, gen.UpdateProjectAPIKeyParams{Name: name, ApiKey: apiKey})
}

func (e mysqlQuerier) listSubscriptionConfigs(ctx context.Context) ([]subscriptionConfigRow, error) {
	rows, err := e.q.ListSubscriptionConfigs(ctx)
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(r gen.SubscriptionConfig) subscriptionConfigRow { return subscriptionConfigRow(r) }), nil
}

func (e mysqlQuerier) upsertSubscriptionConfig(ctx context.Context, arg upsertSubscriptionParams) error {
	return e.q.UpsertSubscriptionConfig(ctx, gen.UpsertSubscriptionConfigParams(arg))
}

func (e mysqlQuerier) deleteSubscriptionConfig(ctx context.Context, name string) error {
	return e.q.DeleteSubscriptionConfig(ctx, name)
}

func (e mysqlQuerier) listEmailConfigs(ctx context.Context) ([]emailConfigRow, error) {
	rows, err := e.q.ListEmailConfigs(ctx)
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(r gen.EmailConfig) emailConfigRow { return emailConfigRow(r) }), nil
}

func (e mysqlQuerier) upsertEmailConfig(ctx context.Context, arg upsertEmailParams) error {
	return e.q.UpsertEmailConfig(ctx, gen.UpsertEmailConfigParams(arg))
}

func (e mysqlQuerier) deleteEmailConfig(ctx context.Context, name string) error {
	return e.q.DeleteEmailConfig(ctx, name)
}

func (e mysqlQuerier) updateEmailPassword(ctx context.Context, name, password string) error {
	return e.q.UpdateEmailPassword(ctx, gen.UpdateEmailPasswordParams{Name: name, Password: password})
}

func (e mysqlQuerier) insertBalance(ctx context.Context, arg insertBalanceParams) error {
	return e.q.InsertBalanceHistory(ctx, gen.InsertBalanceHistoryParams(arg))
}

func (e mysqlQuerier) listBalanceSeries(ctx context.Context, since time.Time) ([]balanceHistoryRow, error) {
	rows, err := e.q.ListBalanceSeries(ctx, nullTime(since))
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(r gen.BalanceHistory) balanceHistoryRow { return balanceHistoryRow(r) }), nil
}

func (e mysqlQuerier) listBalanceTrend(ctx context.Context, projectID string, since time.Time) ([]balanceHistoryRow, error) {
	rows, err := e.q.ListBalanceTrend(ctx, gen.ListBalanceTrendParams{ProjectID: projectID, Since: nullTime(since)})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(r gen.BalanceHistory) balanceHistoryRow { return balanceHistoryRow(r) }), nil
}

func (e mysqlQuerier) listBalanceHistory(ctx context.Context, since time.Time, projectID, provider string, limit int64) ([]balanceHistoryRow, error) {
	rows, err := e.q.ListBalanceHistory(ctx, gen.ListBalanceHistoryParams{
		Since:     nullTime(since),
		ProjectID: projectID,
		Provider:  provider,
		Limit:     rowLimit32(limit),
	})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(r gen.BalanceHistory) balanceHistoryRow { return balanceHistoryRow(r) }), nil
}

func (e mysqlQuerier) insertAlert(ctx context.Context, arg insertAlertParams) error {
	return e.q.InsertAlertHistory(ctx, gen.InsertAlertHistoryParams(arg))
}

func (e mysqlQuerier) countRecentAlerts(ctx context.Context, alertID, alertType, status string, since time.Time) (int64, error) {
	return e.q.CountRecentAlerts(ctx, gen.CountRecentAlertsParams{
		ProjectID: alertID,
		AlertType: alertType,
		Status:    sql.NullString{String: status, Valid: true},
		Since:     nullTime(since),
	})
}

func (e mysqlQuerier) listAlertHistory(ctx context.Context, since time.Time, projectID, alertType string, limit int64) ([]alertHistoryRow, error) {
	rows, err := e.q.ListAlertHistory(ctx, gen.ListAlertHistoryParams{
		Since:     nullTime(since),
		ProjectID: projectID,
		AlertType: alertType,
		Limit:     rowLimit32(limit),
	})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(r gen.AlertHistory) alertHistoryRow { return alertHistoryRow(r) }), nil
}

func (e mysqlQuerier) countAlerts(ctx context.Context, since time.Time) (int64, error) {
	return e.q.CountAlerts(ctx, nullTime(since))
}

func (e mysqlQuerier) countAlertsByType(ctx context.Context, since time.Time) ([]typeCountRow, error) {
	rows, err := e.q.CountAlertsByType(ctx, nullTime(since))
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(r gen.CountAlertsByTypeRow) typeCountRow { return typeCountRow(r) }), nil
}

func (e mysqlQuerier) countAlertsByProject(ctx context.Context, since time.Time) ([]projectCountRow, error) {
	rows, err := e.q.CountAlertsByProject(ctx, nullTime(since))
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(r gen.CountAlertsByProjectRow) projectCountRow { return projectCountRow(r) }), nil
}

func (e mysqlQuerier) insertEmailAlert(ctx context.Context, arg insertEmailAlertParams) error {
	return e.q.InsertEmailAlertHistory(ctx, gen.InsertEmailAlertHistoryParams(arg))
}

func (e mysqlQuerier) countRecentEmailAlerts(ctx context.Context, mailbox, sender, subject, date string, since time.Time) (int64, error) {
	return e.q.CountRecentEmailAlerts(ctx, gen.CountRecentEmailAlertsParams{
		Mailbox: mailbox,
		Sender:  sender,
		Subject: subject,
		Date:    date,
		Since:   nullTime(since),
	})
}

func (e mysqlQuerier) listEmailAlertHistory(ctx context.Context, since time.Time, mailbox string, limit int64) ([]emailAlertHistoryRow, error) {
	rows, err := e.q.ListEmailAlertHistory(ctx, gen.ListEmailAlertHistoryParams{
		Since:   nullTime(since),
		Mailbox: mailbox,
		Limit:   rowLimit32(limit),
	})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(r gen.EmailAlertHistory) emailAlertHistoryRow { return emailAlertHistoryRow(r) }), nil
}

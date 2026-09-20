package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/itswl/quotapulse/internal/model"

	_ "github.com/go-sql-driver/mysql" // mysql
	_ "github.com/jackc/pgx/v5/stdlib" // pgx
	_ "modernc.org/sqlite"             // sqlite
)

// Implementation note.
//
// Implementation note.
// Implementation note.
type Options struct {
	DatabaseURL string // operation URL operation,operation ParseURL operation DSN
	// Implementation note.
	EncryptionKey string
	// Implementation note.
	// Implementation note.
	AutoEncryptOnRead bool
}

// Implementation note.
// Implementation note.
const (
	defaultBalanceDays    = 7
	defaultBalanceLimit   = 100
	defaultAlertDays      = 7
	defaultAlertLimit     = 50
	defaultTrendDays      = 30
	defaultStatsDays      = 30
	defaultEmailDays      = 30
	defaultEmailLimit     = 100
	defaultTopProjects    = 10
	defaultBalanceKindTag = model.TypeCredits // balance_type operation
)

type sqlStore struct {
	db          *sql.DB
	q           querier
	engine      Engine
	cipher      *cipher
	autoEncrypt bool
}

// Implementation note.
func Open(ctx context.Context, opts Options) (Store, error) {
	target, err := ParseURL(opts.DatabaseURL)
	if err != nil {
		return nil, err
	}

	// Implementation note.
	if target.FilePath != "" {
		if dir := filepath.Dir(target.FilePath); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("operation %s operation:%w", dir, err)
			}
		}
	}

	db, err := sql.Open(target.DriverName, target.DSN)
	if err != nil {
		return nil, fmt.Errorf("Failed to open database:%w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("Failed to connect to database:%w", err)
	}
	if err := createTables(ctx, db, target.Engine); err != nil {
		db.Close()
		return nil, err
	}

	var q querier
	switch target.Engine {
	case EngineSQLite:
		q = newSQLiteQuerier(db)
	case EnginePostgres:
		q = newPostgresQuerier(db)
	case EngineMySQL:
		q = newMySQLQuerier(db)
	default:
		db.Close()
		return nil, fmt.Errorf("operation %s operation", target.Engine)
	}

	return &sqlStore{
		db:          db,
		q:           q,
		engine:      target.Engine,
		cipher:      newCipher(opts.EncryptionKey),
		autoEncrypt: opts.AutoEncryptOnRead,
	}, nil
}

func (s *sqlStore) Enabled() bool { return true }
func (s *sqlStore) Close() error  { return s.db.Close() }

// Implementation note.

func (s *sqlStore) ListProjects(ctx context.Context) ([]model.Project, error) {
	rows, err := s.q.listProjectConfigs(ctx)
	if err != nil {
		return nil, err
	}
	// Implementation note.
	for _, row := range rows {
		if s.shouldEncryptOnRead(row.ApiKey) {
			if err := s.q.updateProjectAPIKey(ctx, row.Name, s.cipher.encrypt(row.ApiKey)); err != nil {
				return nil, err
			}
		}
	}

	out := make([]model.Project, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.Project{
			Name:         row.Name,
			Provider:     row.Provider,
			APIKey:       s.cipher.decrypt(row.ApiKey),
			Threshold:    row.Threshold.Float64,
			Type:         row.Type.String,
			OwnerProject: model.OwnerProjectOf(row.OwnerProject.String),
			Enabled:      row.Enabled.Bool,
		})
	}
	return out, nil
}

func (s *sqlStore) UpsertProject(ctx context.Context, p model.Project) error {
	now := time.Now().UTC()
	return s.q.upsertProjectConfig(ctx, upsertProjectParams{
		Name:         p.Name,
		OwnerProject: nullStringOf(p.OwnerProject),
		Provider:     p.Provider,
		ApiKey:       s.cipher.encrypt(p.APIKey),
		Threshold:    sql.NullFloat64{Float64: p.Threshold, Valid: true},
		Type:         sql.NullString{String: p.Type, Valid: true},
		Enabled:      sql.NullBool{Bool: p.Enabled, Valid: true},
		CreatedAt:    nullTime(now),
		UpdatedAt:    nullTime(now),
	})
}

func (s *sqlStore) DeleteProject(ctx context.Context, name string) error {
	return s.q.deleteProjectConfig(ctx, name)
}

// Implementation note.

func (s *sqlStore) ListSubscriptions(ctx context.Context) ([]model.Subscription, error) {
	rows, err := s.q.listSubscriptionConfigs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.Subscription, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.Subscription{
			Name:            row.Name,
			OwnerProject:    model.OwnerProjectOf(row.OwnerProject.String),
			CycleType:       row.CycleType.String,
			RenewalDay:      int(row.RenewalDay.Int64),
			AlertDaysBefore: int(row.AlertDaysBefore.Int64),
			Amount:          row.Amount.Float64,
			Enabled:         row.Enabled.Bool,
			LastRenewedDate: stringPtrOf(row.LastRenewedDate),
		})
	}
	return out, nil
}

func (s *sqlStore) UpsertSubscription(ctx context.Context, sub model.Subscription) error {
	now := time.Now().UTC()
	return s.q.upsertSubscriptionConfig(ctx, upsertSubscriptionParams{
		Name:            sub.Name,
		OwnerProject:    nullStringOf(sub.OwnerProject),
		CycleType:       sql.NullString{String: sub.CycleType, Valid: true},
		RenewalDay:      sql.NullInt64{Int64: int64(sub.RenewalDay), Valid: true},
		AlertDaysBefore: sql.NullInt64{Int64: int64(sub.AlertDaysBefore), Valid: true},
		Amount:          sql.NullFloat64{Float64: sub.Amount, Valid: true},
		Enabled:         sql.NullBool{Bool: sub.Enabled, Valid: true},
		LastRenewedDate: nullStringOf(sub.LastRenewedDate),
		CreatedAt:       nullTime(now),
		UpdatedAt:       nullTime(now),
	})
}

func (s *sqlStore) DeleteSubscription(ctx context.Context, name string) error {
	return s.q.deleteSubscriptionConfig(ctx, name)
}

// Implementation note.

func (s *sqlStore) ListMailboxes(ctx context.Context) ([]model.Mailbox, error) {
	rows, err := s.q.listEmailConfigs(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if s.shouldEncryptOnRead(row.Password) {
			if err := s.q.updateEmailPassword(ctx, row.Name, s.cipher.encrypt(row.Password)); err != nil {
				return nil, err
			}
		}
	}

	out := make([]model.Mailbox, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.Mailbox{
			Name:     row.Name,
			Host:     row.Host,
			Port:     int(row.Port.Int64),
			Username: row.Username,
			Password: s.cipher.decrypt(row.Password),
			UseSSL:   row.UseSsl.Bool,
			Enabled:  row.Enabled.Bool,
		})
	}
	return out, nil
}

func (s *sqlStore) UpsertMailbox(ctx context.Context, m model.Mailbox) error {
	now := time.Now().UTC()
	return s.q.upsertEmailConfig(ctx, upsertEmailParams{
		Name:      m.Name,
		Host:      m.Host,
		Port:      sql.NullInt64{Int64: int64(m.Port), Valid: true},
		Username:  m.Username,
		Password:  s.cipher.encrypt(m.Password),
		UseSsl:    sql.NullBool{Bool: m.UseSSL, Valid: true},
		Enabled:   sql.NullBool{Bool: m.Enabled, Valid: true},
		CreatedAt: nullTime(now),
		UpdatedAt: nullTime(now),
	})
}

func (s *sqlStore) DeleteMailbox(ctx context.Context, name string) error {
	return s.q.deleteEmailConfig(ctx, name)
}

// Implementation note.
func (s *sqlStore) shouldEncryptOnRead(value string) bool {
	return s.autoEncrypt && s.cipher.enabled() && value != "" && !isEncrypted(value)
}

// Implementation note.

func (s *sqlStore) SaveBalance(ctx context.Context, rec BalanceRecord) error {
	balanceType := rec.BalanceType
	if balanceType == "" {
		balanceType = defaultBalanceKindTag // operation balance_type operation
	}
	return s.q.insertBalance(ctx, insertBalanceParams{
		ProjectID:   rec.ProjectID,
		ProjectName: rec.ProjectName,
		Provider:    rec.Provider,
		Balance:     rec.Balance,
		Threshold:   nullFloatOf(rec.Threshold),
		BalanceType: sql.NullString{String: balanceType, Valid: true},
		NeedAlarm:   sql.NullBool{Bool: rec.NeedAlarm, Valid: true},
		Timestamp:   nullTime(time.Now()),
	})
}

func (s *sqlStore) BalanceSeries(ctx context.Context, days int) ([]model.BalancePoint, error) {
	rows, err := s.q.listBalanceSeries(ctx, since(days, defaultBalanceDays))
	if err != nil {
		return nil, err
	}
	out := make([]model.BalancePoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.BalancePoint{
			ProjectID:   row.ProjectID,
			ProjectName: row.ProjectName,
			Provider:    row.Provider,
			BalanceType: row.BalanceType.String,
			Balance:     row.Balance,
			Threshold:   floatPtrOf(row.Threshold),
			NeedAlarm:   row.NeedAlarm.Bool,
			Timestamp:   row.Timestamp.Time.UTC().Unix(),
		})
	}
	return out, nil
}

func (s *sqlStore) BalanceHistory(ctx context.Context, q BalanceQuery) ([]BalanceRow, error) {
	rows, err := s.q.listBalanceHistory(ctx,
		since(q.Days, defaultBalanceDays), q.ProjectID, q.Provider, limitOf(q.Limit, defaultBalanceLimit))
	if err != nil {
		return nil, err
	}
	out := make([]BalanceRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, BalanceRow{
			ID:          row.ID,
			ProjectID:   row.ProjectID,
			ProjectName: row.ProjectName,
			Provider:    row.Provider,
			Balance:     row.Balance,
			Threshold:   floatPtrOf(row.Threshold),
			BalanceType: row.BalanceType.String,
			NeedAlarm:   row.NeedAlarm.Bool,
			Timestamp:   isoUTC(row.Timestamp),
		})
	}
	return out, nil
}

func (s *sqlStore) BalanceTrend(ctx context.Context, projectID string, days int) (*Trend, error) {
	window := daysOf(days, defaultTrendDays)
	rows, err := s.q.listBalanceTrend(ctx, projectID, sinceDays(window))
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil // operation:operation nil,operation 404
	}

	first, last := rows[0], rows[len(rows)-1]
	minBalance, maxBalance, sum := first.Balance, first.Balance, 0.0
	history := make([]TrendPoint, 0, len(rows))
	for _, row := range rows {
		minBalance = min(minBalance, row.Balance)
		maxBalance = max(maxBalance, row.Balance)
		sum += row.Balance
		history = append(history, TrendPoint{
			Timestamp: isoUTC(row.Timestamp),
			Balance:   row.Balance,
			NeedAlarm: row.NeedAlarm.Bool,
		})
	}

	trend := &Trend{
		ProjectID:      projectID,
		ProjectName:    first.ProjectName,
		Days:           window,
		DataPoints:     len(rows),
		CurrentBalance: last.Balance,
		MinBalance:     minBalance,
		MaxBalance:     maxBalance,
		AvgBalance:     sum / float64(len(rows)),
		Threshold:      last.Threshold.Float64, // operation 0 operation,operation
		FirstTimestamp: isoUTC(first.Timestamp),
		LastTimestamp:  isoUTC(last.Timestamp),
		History:        history,
	}
	if len(rows) >= 2 {
		change := last.Balance - first.Balance
		// Implementation note.
		percent := 0.0
		if first.Balance != 0 {
			percent = change / first.Balance * 100
		}
		trend.Change = &change
		trend.ChangePercent = &percent
	}
	return trend, nil
}

// Implementation note.

func (s *sqlStore) SaveAlert(ctx context.Context, rec AlertRecord) error {
	status := rec.Status
	if status == "" {
		status = "sent"
	}
	return s.q.insertAlert(ctx, insertAlertParams{
		ProjectID:      rec.AlertID,
		ProjectName:    rec.Name,
		AlertType:      rec.AlertType,
		Status:         sql.NullString{String: status, Valid: true},
		Message:        sql.NullString{String: rec.Message, Valid: true},
		BalanceValue:   nullFloatOf(rec.Value),
		ThresholdValue: nullFloatOf(rec.Threshold),
		Timestamp:      nullTime(time.Now()),
	})
}

func (s *sqlStore) HasRecentAlert(ctx context.Context, alertID, alertType string, within time.Duration) (bool, error) {
	// Implementation note.
	if within <= 0 {
		return false, nil
	}
	// Implementation note.
	n, err := s.q.countRecentAlerts(ctx, alertID, alertType, "sent", time.Now().UTC().Add(-within))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *sqlStore) RecentAlerts(ctx context.Context, q AlertQuery) ([]AlertRow, error) {
	rows, err := s.q.listAlertHistory(ctx,
		since(q.Days, defaultAlertDays), q.ProjectID, q.AlertType, limitOf(q.Limit, defaultAlertLimit))
	if err != nil {
		return nil, err
	}
	out := make([]AlertRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, AlertRow{
			ID:             row.ID,
			ProjectID:      row.ProjectID,
			ProjectName:    row.ProjectName,
			AlertType:      row.AlertType,
			Status:         row.Status.String,
			Message:        row.Message.String,
			BalanceValue:   floatPtrOf(row.BalanceValue),
			ThresholdValue: floatPtrOf(row.ThresholdValue),
			Timestamp:      isoUTC(row.Timestamp),
		})
	}
	return out, nil
}

func (s *sqlStore) AlertStats(ctx context.Context, days int) (*Stats, error) {
	window := daysOf(days, defaultStatsDays)
	from := sinceDays(window)

	total, err := s.q.countAlerts(ctx, from)
	if err != nil {
		return nil, err
	}
	byType, err := s.q.countAlertsByType(ctx, from)
	if err != nil {
		return nil, err
	}
	topProjects, err := s.q.countAlertsByProject(ctx, from)
	if err != nil {
		return nil, err
	}

	stats := &Stats{
		Days:        window,
		TotalAlerts: int(total),
		ByType:      make(map[string]int, len(byType)),
		TopProjects: make([]TopProject, 0, min(len(topProjects), defaultTopProjects)),
	}
	for _, row := range byType {
		stats.ByType[row.AlertType] = int(row.Count)
	}
	for _, row := range topProjects {
		stats.TopProjects = append(stats.TopProjects, TopProject{Project: row.ProjectName, Count: int(row.Count)})
	}
	return stats, nil
}

// Implementation note.

func (s *sqlStore) SaveEmailAlert(ctx context.Context, rec EmailAlertRecord) error {
	keywords, err := encodeKeywords(rec.Keywords)
	if err != nil {
		return err
	}
	return s.q.insertEmailAlert(ctx, insertEmailAlertParams{
		Mailbox:         rec.Mailbox,
		Sender:          rec.Sender,
		Subject:         rec.Subject,
		Date:            rec.Date,
		ServiceName:     nullStringOf(rec.ServiceName),
		Amount:          nullFloatOf(rec.Amount),
		MatchedKeywords: sql.NullString{String: keywords, Valid: true},
		AlertSent:       sql.NullBool{Bool: rec.AlertSent, Valid: true},
		Timestamp:       nullTime(time.Now()),
	})
}

func (s *sqlStore) HasRecentEmailAlert(ctx context.Context, mailbox, sender, subject, date string, days int) (bool, error) {
	n, err := s.q.countRecentEmailAlerts(ctx, mailbox, sender, subject, date, since(days, defaultEmailDays))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *sqlStore) EmailAlerts(ctx context.Context, q EmailAlertQuery) ([]EmailAlertRow, error) {
	rows, err := s.q.listEmailAlertHistory(ctx,
		since(q.Days, defaultEmailDays), q.Mailbox, limitOf(q.Limit, defaultEmailLimit))
	if err != nil {
		return nil, err
	}
	out := make([]EmailAlertRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, EmailAlertRow{
			ID:              row.ID,
			Mailbox:         row.Mailbox,
			Sender:          row.Sender,
			Subject:         row.Subject,
			Date:            row.Date,
			ServiceName:     stringPtrOf(row.ServiceName),
			Amount:          floatPtrOf(row.Amount),
			MatchedKeywords: stringPtrOf(row.MatchedKeywords),
			AlertSent:       row.AlertSent.Bool,
			Timestamp:       isoUTC(row.Timestamp),
		})
	}
	return out, nil
}

// Implementation note.
//
// Implementation note.
// Implementation note.
func encodeKeywords(keywords []string) (string, error) {
	if keywords == nil {
		keywords = []string{} // nil operation null,operation JSON operation
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(keywords); err != nil {
		return "", err
	}
	return string(bytes.TrimRight(buf.Bytes(), "\n")), nil
}

// Implementation note.

// Implementation note.
func daysOf(days, fallback int) int {
	if days <= 0 {
		return fallback
	}
	return days
}

// maxRowLimit caps every history query. The HTTP layer already rejects limit > 1000 and the
// MCP tools cap at 100, so this only protects future callers.
const maxRowLimit = 1000

func limitOf(limit, fallback int) int64 {
	if limit <= 0 {
		return int64(fallback)
	}
	if limit > maxRowLimit {
		return maxRowLimit
	}
	return int64(limit)
}

// rowLimit32 narrows a row limit for the MySQL and PostgreSQL drivers, which take the LIMIT
// parameter as int32. Out-of-range values are clamped, not wrapped: a wrapped limit reaches
// the database as a negative LIMIT and fails the query.
func rowLimit32(limit int64) int32 {
	if limit < 0 {
		return 0
	}
	if limit > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(limit)
}

func since(days, fallback int) time.Time { return sinceDays(daysOf(days, fallback)) }

func sinceDays(days int) time.Time {
	return time.Now().UTC().AddDate(0, 0, -days)
}

// Implementation note.
func isoUTC(v sql.NullTime) string {
	if !v.Valid {
		return ""
	}
	return v.Time.UTC().Format(time.RFC3339Nano)
}

func nullStringOf(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}

func stringPtrOf(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func nullFloatOf(v *float64) sql.NullFloat64 {
	if v == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *v, Valid: true}
}

func floatPtrOf(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}

// Package config provides the package implementation.
//
// Implementation note.
//
// Implementation note.
// Implementation note.
// Implementation note.
//
// Implementation note.
// Implementation note.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/itswl/quotapulse/internal/timeutil"
)

// Implementation note.
//
// Implementation note.
// Implementation note.
type Settings struct {
	// Implementation note.
	EnableDatabase      bool
	EnableDynamicConfig bool
	EnableHistoryAPI    bool
	EnableSubscriptions bool
	EnablePrometheus    bool
	EnableWebAlarm      bool
	EnableMCP           bool

	// Implementation note.
	BalanceRefreshIntervalSeconds   *int
	MaxConcurrentChecks             *int
	AlertCooldownSeconds            *int
	SubscriptionAlertCooldownSecond *int
	JobFailureAlertThreshold        *int
	JobTimeoutSeconds               *int
	BackupSchedule                  string
	BackupTimes                     []timeutil.ClockTime
	BackupDir                       string
	BackupKeep                      *int

	// Implementation note.
	WebhookURL    string
	WebhookType   string
	WebhookSource string

	// Implementation note.
	AlertSchedule          string
	EmailScanSchedule      string
	EmailScanDays          int
	WeeklyReportSchedule   string
	EmailAlertKeywords     string
	EmailExtraAlertKeyword string

	// Implementation note.
	BurnRateWindowDays  int
	RunwayAlertDays     float64
	SpendSpikeRatio     float64
	SpendSpikeMinAmount float64

	// Implementation note.
	RequestTimeout   int
	MaxEmailsToScan  int
	ResponseCacheTTL int

	// Implementation note.
	LogLevel  string
	LogFormat string
	LogFile   string

	// Implementation note.
	DatabaseURL          string
	StrictDatabaseErrors bool
	AutoEncryptOnRead    bool
	ConfigEncryptionKey  string

	// Implementation note.
	WebPort       int
	MetricsPort   int
	AppVersion    string
	WebEnableCORS bool
	CORSOrigins   string
	WebAPIKey     string
	// Implementation note.
	// Implementation note.
	ShutdownDelaySeconds int

	// Implementation note.
	AlertTimes           []timeutil.ClockTime
	EmailScanTimes       []timeutil.ClockTime
	WeeklyReportTimes    []timeutil.ClockTime
	WeeklyReportWeekdays map[int]bool
}

// Implementation note.
//
//	go build -ldflags "-X github.com/itswl/quotapulse/internal/config.Version=v1.2.3"
//
// Implementation note.
// Implementation note.
var Version = "dev"

// Implementation note.
const (
	DefaultRefreshInterval = 3600
	DefaultMaxConcurrent   = 20
	MaxConcurrentUpper     = 50
	DefaultCooldownSeconds = 86400

	DefaultJobFailureAlertThreshold = 3
	DefaultJobTimeoutSeconds        = 600
	DefaultBackupKeep               = 7
)

// Implementation note.
func Load() (*Settings, error) {
	e := &envReader{}
	s := &Settings{
		EnableDatabase:      e.boolean("ENABLE_DATABASE", false),
		EnableDynamicConfig: e.boolean("ENABLE_DYNAMIC_CONFIG", false),
		EnableHistoryAPI:    e.boolean("ENABLE_HISTORY_API", false),
		EnableSubscriptions: e.boolean("ENABLE_SUBSCRIPTIONS", false),
		EnablePrometheus:    e.boolean("ENABLE_PROMETHEUS", false),
		EnableWebAlarm:      e.boolean("ENABLE_WEB_ALARM", false),
		EnableMCP:           e.boolean("ENABLE_MCP", false),

		BalanceRefreshIntervalSeconds:   e.optionalInt("BALANCE_REFRESH_INTERVAL_SECONDS"),
		MaxConcurrentChecks:             e.optionalInt("MAX_CONCURRENT_CHECKS"),
		AlertCooldownSeconds:            e.optionalInt("ALERT_COOLDOWN_SECONDS"),
		SubscriptionAlertCooldownSecond: e.optionalInt("SUBSCRIPTION_ALERT_COOLDOWN_SECONDS"),

		WebhookURL:    e.text("WEBHOOK_URL", ""),
		WebhookType:   e.text("WEBHOOK_TYPE", ""),
		WebhookSource: e.text("WEBHOOK_SOURCE", ""),

		AlertSchedule:          e.text("ALERT_SCHEDULE", "09:00,15:00"),
		EmailScanSchedule:      e.text("EMAIL_SCAN_SCHEDULE", "10:00"),
		EmailScanDays:          e.integer("EMAIL_SCAN_DAYS", 1),
		WeeklyReportSchedule:   e.text("WEEKLY_REPORT_SCHEDULE", "Mon 09:00"),
		EmailAlertKeywords:     e.text("EMAIL_ALERT_KEYWORDS", ""),
		EmailExtraAlertKeyword: e.text("EMAIL_EXTRA_ALERT_KEYWORDS", ""),

		BurnRateWindowDays:  e.integer("BURN_RATE_WINDOW_DAYS", 7),
		RunwayAlertDays:     e.number("RUNWAY_ALERT_DAYS", 7),
		SpendSpikeRatio:     e.number("SPEND_SPIKE_RATIO", 3),
		SpendSpikeMinAmount: e.number("SPEND_SPIKE_MIN_AMOUNT", 1),

		RequestTimeout:   e.integer("REQUEST_TIMEOUT", 10),
		MaxEmailsToScan:  e.integer("MAX_EMAILS_TO_SCAN", 1000),
		ResponseCacheTTL: e.integer("RESPONSE_CACHE_TTL", 300),

		LogLevel:  e.text("LOG_LEVEL", "INFO"),
		LogFormat: e.text("LOG_FORMAT", "text"),
		LogFile:   e.text("LOG_FILE", ""),

		DatabaseURL:          e.text("DATABASE_URL", "sqlite:///./data/quotapulse.db"),
		StrictDatabaseErrors: e.boolean("STRICT_DATABASE_ERRORS", false),
		AutoEncryptOnRead:    e.boolean("AUTO_ENCRYPT_ON_READ", true),
		ConfigEncryptionKey:  e.text("CONFIG_ENCRYPTION_KEY", ""),

		WebPort:                  e.integer("WEB_PORT", 8080),
		MetricsPort:              e.integer("METRICS_PORT", 9100),
		AppVersion:               e.text("APP_VERSION", Version),
		WebEnableCORS:            e.boolean("WEB_ENABLE_CORS", false),
		CORSOrigins:              e.text("CORS_ORIGINS", ""),
		WebAPIKey:                strings.TrimSpace(e.text("WEB_API_KEY", "")),
		JobFailureAlertThreshold: e.optionalInt("JOB_FAILURE_ALERT_THRESHOLD"),
		JobTimeoutSeconds:        e.optionalInt("JOB_TIMEOUT_SECONDS"),
		BackupSchedule:           strings.TrimSpace(e.text("DB_BACKUP_SCHEDULE", "")),
		BackupDir:                e.text("DB_BACKUP_DIR", "backups"),
		BackupKeep:               e.optionalInt("DB_BACKUP_KEEP"),

		ShutdownDelaySeconds: e.integer("SHUTDOWN_DELAY_SECONDS", 0),
	}
	if err := e.err(); err != nil {
		return nil, err
	}
	if err := s.parseSchedules(); err != nil {
		return nil, err
	}
	if s.EmailScanDays < 1 || s.EmailScanDays > 30 {
		return nil, fmt.Errorf("EMAIL_SCAN_DAYS must be between 1 and 30; got %d", s.EmailScanDays)
	}
	if s.BurnRateWindowDays < 1 || s.BurnRateWindowDays > 90 {
		return nil, fmt.Errorf("BURN_RATE_WINDOW_DAYS must be between 1 and 90; got %d", s.BurnRateWindowDays)
	}
	return s, nil
}

func (s *Settings) parseSchedules() error {
	var err error
	if s.AlertTimes, err = timeutil.ParseDailyTimes(s.AlertSchedule); err != nil {
		return fmt.Errorf("ALERT_SCHEDULE: %w", err)
	}
	if s.EmailScanTimes, err = timeutil.ParseDailyTimes(s.EmailScanSchedule); err != nil {
		return fmt.Errorf("EMAIL_SCAN_SCHEDULE: %w", err)
	}
	if s.BackupSchedule != "" {
		if s.BackupTimes, err = timeutil.ParseDailyTimes(s.BackupSchedule); err != nil {
			return err
		}
	}
	if s.WeeklyReportWeekdays, s.WeeklyReportTimes, err = timeutil.ParseWeeklySchedule(s.WeeklyReportSchedule); err != nil {
		return fmt.Errorf("WEEKLY_REPORT_SCHEDULE: %w", err)
	}
	return nil
}

// Implementation note.
func (s *Settings) RefreshInterval() int {
	if s.BalanceRefreshIntervalSeconds == nil || *s.BalanceRefreshIntervalSeconds <= 0 {
		return DefaultRefreshInterval
	}
	return *s.BalanceRefreshIntervalSeconds
}

// Implementation note.
func (s *Settings) Concurrency() int {
	n := DefaultMaxConcurrent
	if s.MaxConcurrentChecks != nil {
		n = *s.MaxConcurrentChecks
	}
	return max(1, min(n, MaxConcurrentUpper))
}

// Implementation note.
// Implementation note.
func (s *Settings) CooldownSeconds(kind string) int {
	var value *int
	if kind == "subscription" {
		value = s.SubscriptionAlertCooldownSecond
	}
	if value == nil {
		value = s.AlertCooldownSeconds
	}
	if value == nil {
		return DefaultCooldownSeconds
	}
	return max(0, *value)
}

// Implementation note.
func (s *Settings) AlertKeywordOverride() []string { return splitList(s.EmailAlertKeywords) }

// Implementation note.
func (s *Settings) AlertKeywordExtras() []string { return splitList(s.EmailExtraAlertKeyword) }

// Implementation note.
func (s *Settings) CORSOriginList() []string { return splitList(s.CORSOrigins) }

// JobFailureAlertThresholdValue is how many consecutive job failures trigger one
// escalation webhook; zero disables escalation entirely.
func (s *Settings) JobFailureAlertThresholdValue() int {
	if s.JobFailureAlertThreshold == nil {
		return DefaultJobFailureAlertThreshold
	}
	return max(0, *s.JobFailureAlertThreshold)
}

// JobTimeoutSecondsValue caps a single scheduled run; zero disables the cap.
func (s *Settings) JobTimeoutSecondsValue() int {
	if s.JobTimeoutSeconds == nil {
		return DefaultJobTimeoutSeconds
	}
	return max(0, *s.JobTimeoutSeconds)
}

func (s *Settings) BackupKeepValue() int {
	if s.BackupKeep == nil {
		return DefaultBackupKeep
	}
	return max(1, *s.BackupKeep)
}

// APIKeys splits WEB_API_KEY on commas: add a replacement key before retiring the old
// one and both stay valid until the old entry is dropped.
func (s *Settings) APIKeys() []string { return splitList(s.WebAPIKey) }

func splitList(text string) []string {
	var out []string
	for _, item := range strings.Split(text, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// Implementation note.

// Implementation note.
type envReader struct{ problems []string }

func (e *envReader) err() error {
	if len(e.problems) == 0 {
		return nil
	}
	return fmt.Errorf("Invalid environment variables:\n  %s", strings.Join(e.problems, "\n  "))
}

// Implementation note.
func raw(key string) (string, bool) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	if value = strings.TrimSpace(value); value == "" {
		return "", false
	}
	return value, true
}

func (e *envReader) text(key, fallback string) string {
	if value, ok := raw(key); ok {
		return value
	}
	return fallback
}

var truthy = map[string]bool{"1": true, "true": true, "yes": true, "on": true, "y": true, "t": true}
var falsy = map[string]bool{"0": true, "false": true, "no": true, "off": true, "n": true, "f": true}

func (e *envReader) boolean(key string, fallback bool) bool {
	value, ok := raw(key)
	if !ok {
		return fallback
	}
	lowered := strings.ToLower(value)
	if truthy[lowered] {
		return true
	}
	if falsy[lowered] {
		return false
	}
	e.problems = append(e.problems, fmt.Sprintf("%s=%q is not a boolean; use true or false", key, value))
	return fallback
}

func (e *envReader) integer(key string, fallback int) int {
	value, ok := raw(key)
	if !ok {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		e.problems = append(e.problems, fmt.Sprintf("%s=%q is not an integer", key, value))
		return fallback
	}
	return n
}

func (e *envReader) optionalInt(key string) *int {
	value, ok := raw(key)
	if !ok {
		return nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		e.problems = append(e.problems, fmt.Sprintf("%s=%q is not an integer", key, value))
		return nil
	}
	return &n
}

func (e *envReader) number(key string, fallback float64) float64 {
	value, ok := raw(key)
	if !ok {
		return fallback
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		e.problems = append(e.problems, fmt.Sprintf("%s=%q is not a number", key, value))
		return fallback
	}
	return f
}

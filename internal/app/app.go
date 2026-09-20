// Package app provides the package implementation.
//
// Implementation note.
package app

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/httpapi"
	"github.com/itswl/quotapulse/internal/mailscan"
	"github.com/itswl/quotapulse/internal/mcpserver"
	"github.com/itswl/quotapulse/internal/metrics"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/monitor"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/report"
	"github.com/itswl/quotapulse/internal/runway"
	"github.com/itswl/quotapulse/internal/scheduler"
	"github.com/itswl/quotapulse/internal/state"
	"github.com/itswl/quotapulse/internal/store"
	"github.com/itswl/quotapulse/internal/subscription"
)

// Implementation note.
type App struct {
	Settings *config.Settings
	Log      *slog.Logger
	Store    store.Store
	Resolver *config.Resolver
	Notifier notify.Notifier
	Metrics  *metrics.Collector
	State    *state.Manager
	Monitor  *monitor.Monitor
	Subs     *subscription.Checker
	Scanner  *mailscan.Scanner
	Server   *httpapi.Server

	scheduler *scheduler.Scheduler
}

// Implementation note.
func New(settings *config.Settings, log *slog.Logger, assets fs.FS) (*App, error) {
	st, err := openStore(settings, log)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: time.Duration(settings.RequestTimeout) * time.Second}
	notifier, err := notify.New(settings.WebhookURL, settings.WebhookType, settings.WebhookSource, httpClient)
	if err != nil {
		return nil, fmt.Errorf("Invalid Webhook configuration: %w", err)
	}
	if notifier == nil {
		log.Warn("WEBHOOK_URL is not set,Low balanceoperation")
	}

	app := &App{
		Settings: settings,
		Log:      log,
		Store:    st,
		Resolver: config.NewResolver(settings, st, log),
		Notifier: notifier,
		Metrics:  metrics.New(nil),
		State:    state.New(),
	}

	app.Monitor = &monitor.Monitor{
		Settings: settings,
		Resolver: app.Resolver,
		Store:    st,
		Notifier: notifier,
		Client:   provider.NewClient(time.Duration(settings.RequestTimeout) * time.Second),
		Log:      log,
		OnNotify: app.Metrics.RecordNotification,
		Alerter: &runway.Alerter{
			Store: st, Notifier: notifier, Log: log,
			RunwayAlertDays: settings.RunwayAlertDays,
			SpikeRatio:      settings.SpendSpikeRatio,
			SpikeMinAmount:  settings.SpendSpikeMinAmount,
			Cooldown:        time.Duration(settings.CooldownSeconds("balance")) * time.Second,
		},
	}
	app.Subs = &subscription.Checker{
		Store: st, Notifier: notifier, Log: log,
		Cooldown: time.Duration(settings.CooldownSeconds("subscription")) * time.Second,
		OnNotify: app.Metrics.RecordNotification,
	}
	app.Scanner = &mailscan.Scanner{
		Store: st, Notifier: notifier, Log: log,
		Keywords:  keywords(settings),
		MaxEmails: settings.MaxEmailsToScan,
		Timeout:   time.Duration(settings.RequestTimeout) * time.Second,
		OnNotify:  app.Metrics.RecordNotification,
	}
	app.Server = &httpapi.Server{
		Settings: settings, Resolver: app.Resolver, Store: st, State: app.State,
		Monitor: app.Monitor, Subs: app.Subs, Scanner: app.Scanner,
		Log: log, Assets: assets,
		OnBalanceUpdated:      app.Metrics.UpdateBalance,
		OnSubscriptionUpdated: app.Metrics.UpdateSubscriptions,
		OnEmailScanned:        app.Metrics.UpdateEmailScan,
	}
	if settings.EnableMCP {
		app.Server.MCP = mcpserver.NewHandler(settings, app.State, st, app.Resolver, log)
	}
	return app, nil
}

// Implementation note.
func keywords(settings *config.Settings) []string {
	base := settings.AlertKeywordOverride()
	if len(base) == 0 {
		base = mailscan.DefaultAlertKeywords
	}
	return append(append([]string(nil), base...), settings.AlertKeywordExtras()...)
}

// Implementation note.
func openStore(settings *config.Settings, log *slog.Logger) (store.Store, error) {
	if !settings.EnableDatabase {
		return store.Null(), nil
	}
	st, err := store.Open(context.Background(), store.Options{
		DatabaseURL:       settings.DatabaseURL,
		EncryptionKey:     settings.ConfigEncryptionKey,
		AutoEncryptOnRead: settings.AutoEncryptOnRead,
	})
	if err != nil {
		// Implementation note.
		if settings.StrictDatabaseErrors {
			return nil, fmt.Errorf("Database initialization failed: %w", err)
		}
		log.Warn("Database initialization failed,operation", "error", err)
		return store.Null(), nil
	}
	log.Info("Database initialized")
	return st, nil
}

// Implementation note.
func (a *App) Close() error { return a.Store.Close() }

// Implementation note.

// Implementation note.
func (a *App) BuildTasks() []*scheduler.Task {
	settings := a.Settings

	webAlarmNote := "Check only; no alerts"
	if settings.EnableWebAlarm {
		webAlarmNote = "Send real alerts"
	}

	return []*scheduler.Task{
		{
			Name:        "dashboard_refresh",
			Description: fmt.Sprintf("operation(%s)", webAlarmNote),
			Interval:    time.Duration(settings.RefreshInterval()) * time.Second,
			RunAtStart:  true,
			Run: func(ctx context.Context) (any, error) {
				return a.refreshAll(ctx, !settings.EnableWebAlarm)
			},
		},
		{
			Name:        "alert_check",
			Description: "operation,operation",
			DailyTimes:  settings.AlertTimes,
			Run: func(ctx context.Context) (any, error) {
				return a.refreshAll(ctx, false)
			},
		},
		{
			Name:        "email_scan",
			Description: fmt.Sprintf("operation %d operation / operation,operation", settings.EmailScanDays),
			DailyTimes:  settings.EmailScanTimes,
			Run: func(ctx context.Context) (any, error) {
				return a.ScanMailboxes(ctx, settings.EmailScanDays, false)
			},
		},
		{
			Name:        "weekly_report",
			Description: "operation, operation",
			DailyTimes:  settings.WeeklyReportTimes,
			Weekdays:    settings.WeeklyReportWeekdays,
			Run:         a.SendWeeklyReport,
		},
	}
}

// Implementation note.
func (a *App) refreshAll(ctx context.Context, dryRun bool) (any, error) {
	outcome, err := a.Monitor.Run(ctx, "", dryRun)
	if err != nil {
		return nil, err
	}
	a.State.SetBalance(outcome.Results)
	a.Metrics.UpdateBalance(outcome.Results)
	summary := model.SummarizeBalance(outcome.Results)

	detail := map[string]any{
		"projects": summary.Total, "failed": summary.Failed,
		"need_alarm": summary.NeedAlarm, "dry_run": dryRun,
	}

	subs := a.refreshSubscriptions(ctx, dryRun)
	detail["subscriptions"] = len(subs)
	needAlert := 0
	for _, s := range subs {
		if s.NeedAlert {
			needAlert++
		}
	}
	detail["need_alert"] = needAlert
	return detail, nil
}

// Implementation note.
func (a *App) refreshSubscriptions(ctx context.Context, dryRun bool) []model.SubscriptionResult {
	if !a.Settings.EnableSubscriptions {
		return nil
	}
	cfg := a.Resolver.Load(ctx)
	results := a.Subs.Check(ctx, cfg.EnabledSubscriptions(), dryRun)
	a.State.SetSubscriptions(results)
	a.Metrics.UpdateSubscriptions(results)
	return results
}

// Implementation note.
func (a *App) ScanMailboxes(ctx context.Context, days int, dryRun bool) (any, error) {
	cfg := a.Resolver.Load(ctx)
	mailboxes := cfg.EnabledMailboxes()
	if len(mailboxes) == 0 {
		return map[string]any{"mailboxes": 0, "skipped": "operation"}, nil
	}

	result := a.Scanner.Scan(ctx, mailboxes, days, dryRun)
	a.State.SetEmailScan(result)
	a.Metrics.UpdateEmailScan(result)

	summary := a.State.EmailScan().Summary
	return map[string]any{
		"mailboxes": summary.TotalMailboxes, "failed_mailboxes": summary.FailedMailboxes,
		"emails": summary.TotalEmails, "alerts": summary.TotalAlerts,
		"alerts_sent": summary.AlertsSent, "dry_run": dryRun,
	}, nil
}

// Implementation note.
func (a *App) SendWeeklyReport(ctx context.Context) (any, error) {
	balance := a.State.Balance()
	subs := a.State.Subscriptions()
	email := a.State.EmailScan()

	series, err := a.Store.BalanceSeries(ctx, report.WindowDays)
	if err != nil {
		a.Log.Warn("operation,operation", "error", err)
	}
	runways := runway.ComputeAll(series, report.WindowDays, time.Now())

	summary := report.Build(balance.Projects, subs.Subscriptions,
		email.Mailboxes, email.Summary.TotalAlerts, runways, time.Now())

	sent := false
	if a.Notifier == nil {
		a.Log.Error("Webhook URL is not configured,operation")
	} else {
		msg := notify.Custom("operation", []string{report.Render(summary)}, "weekly_report")
		sendErr := a.Notifier.Send(ctx, msg)
		a.Metrics.RecordNotification(msg.Kind, sendErr == nil)
		if sendErr != nil {
			a.Log.Error("operation", "error", sendErr)
		} else {
			sent = true
		}
	}

	return map[string]any{
		"accounts": summary.Accounts.Total, "consumed": summary.TotalConsumed,
		"upcoming_amount": summary.UpcomingAmount, "sent": sent,
	}, nil
}

// Implementation note.
func (a *App) StartScheduler(ctx context.Context) {
	tasks := a.BuildTasks()
	a.scheduler = scheduler.New(tasks, a.onJobResult, a.Log)

	for _, task := range tasks {
		a.State.RegisterJob(task.Name, task.Description, task.ScheduleText(), task.Enabled(), task.NextRun())
		a.Log.Info("operation", "name", task.Name, "schedule", task.ScheduleText(), "description", task.Description)
	}
	a.scheduler.Start(ctx)
}

// Implementation note.
func (a *App) onJobResult(result scheduler.Result) {
	a.State.RecordJobRun(result.Name, result.Success, result.StartedAt,
		result.Duration, result.Err, result.Detail, result.NextRun)
	a.Metrics.RecordJobRun(result.Name, result.Success, result.StartedAt, result.Duration)
}

// Implementation note.
func (a *App) StopScheduler() {
	if a.scheduler != nil {
		a.scheduler.Stop(5 * time.Second)
	}
}

// Implementation note.
func (a *App) ServeMetrics(ctx context.Context) {
	if !a.Settings.EnablePrometheus {
		return
	}
	addr := fmt.Sprintf(":%d", a.Settings.MetricsPort)
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", a.Metrics.Handler())
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		a.Log.Info("Prometheus operation", "addr", addr+"/metrics")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.Log.Error("operation", "error", err)
		}
	}()
}

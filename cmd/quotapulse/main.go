// Implementation note.
//
// Implementation note.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	// Implementation note.
	// Implementation note.
	_ "time/tzdata"

	"github.com/itswl/quotapulse/internal/app"
	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/selfcheck"
	"github.com/itswl/quotapulse/ui"
)

func main() {
	opts := parseFlags()

	// Implementation note.
	if err := config.LoadEnvFile(".env"); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to load .env:", err)
		os.Exit(1)
	}
	settings, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Implementation note.
	// Implementation note.
	if opts.healthcheck {
		os.Exit(probe(settings.WebPort))
	}

	log, closeLog, err := newLogger(settings)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to initialize logger:", err)
		os.Exit(1)
	}
	defer closeLog()

	instance, err := app.New(settings, log, assets(log))
	if err != nil {
		log.Error("Startup failed", "error", err)
		os.Exit(1)
	}
	defer instance.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	os.Exit(run(ctx, instance, opts))
}

type options struct {
	showConfig        bool
	healthcheck       bool
	checkBalance      bool
	checkSubscription bool
	checkEmail        bool
	emailDays         int
	project           string
	importConfig      string
	dryRun            bool
}

func parseFlags() options {
	var opts options
	flag.BoolVar(&opts.showConfig, "show-config", false, "Check configuration sources and missing values; exit nonzero on errors")
	flag.BoolVar(&opts.healthcheck, "healthcheck", false, "Probe local /live for the container health check")
	flag.BoolVar(&opts.checkBalance, "check", false, "Run one balance check and exit")
	flag.BoolVar(&opts.checkSubscription, "check-subscriptions", false, "Run one subscription check and exit")
	flag.BoolVar(&opts.checkEmail, "check-email", false, "Run one mailbox scan and exit")
	flag.IntVar(&opts.emailDays, "email-days", 1, "Number of recent days to scan")
	flag.StringVar(&opts.project, "project", "", "Check only the selected project")
	flag.StringVar(&opts.importConfig, "import-config", "", "Import a legacy config.json into the database dynamic configuration once; delete the file afterwards")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "Dry run: check only; no alerts")
	flag.Parse()
	return opts
}

// Implementation note.
func run(ctx context.Context, instance *app.App, opts options) int {
	switch {
	case opts.showConfig:
		problems := selfcheck.Run(ctx, instance, os.Stdout)
		if problems > 0 {
			return 1
		}
		return 0

	case opts.importConfig != "":
		if _, err := instance.ImportLegacyConfig(ctx, opts.importConfig, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0

	case opts.checkBalance || opts.project != "":
		if _, err := instance.Monitor.Run(ctx, opts.project, opts.dryRun); err != nil {
			instance.Log.Error("Balance check failed", "error", err)
			return 1
		}
		return 0

	case opts.checkSubscription:
		cfg := instance.Resolver.Load(ctx)
		instance.Subs.Check(ctx, cfg.EnabledSubscriptions(), opts.dryRun)
		return 0

	case opts.checkEmail:
		if _, err := instance.ScanMailboxes(ctx, opts.emailDays, opts.dryRun); err != nil {
			instance.Log.Error("Mailbox scan failed", "error", err)
			return 1
		}
		return 0
	}

	return serve(ctx, instance)
}

func serve(ctx context.Context, instance *app.App) int {
	settings := instance.Settings

	instance.StartScheduler(ctx)
	defer instance.StopScheduler()
	instance.ServeMetrics(ctx)

	if settings.EnableWebAlarm {
		instance.Log.Warn("Dashboard refresh sends real alerts (ENABLE_WEB_ALARM=true)")
	} else {
		instance.Log.Info("Dashboard refresh checks only; scheduled alert_check / email_scan jobs send real alerts")
	}

	addr := fmt.Sprintf(":%d", settings.WebPort)
	if err := instance.Server.ListenAndServe(ctx, addr); err != nil {
		instance.Log.Error("Web service exited unexpectedly", "error", err)
		return 1
	}
	instance.Log.Info("Service stopped")
	return 0
}

// Implementation note.
func probe(port int) int {
	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/live", port)

	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Health check failed:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Health check failed: HTTP %d\n", resp.StatusCode)
		return 1
	}
	return 0
}

// Implementation note.
func assets(log *slog.Logger) fs.FS {
	dist, err := ui.Assets()
	if err != nil {
		log.Warn("Frontend assets are not embedded; the dashboard is unavailable", "error", err)
		return nil
	}
	return dist
}

// Implementation note.
// Implementation note.
func newLogger(settings *config.Settings) (*slog.Logger, func(), error) {
	level := slog.LevelInfo
	switch strings.ToUpper(settings.LogLevel) {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARNING", "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	}

	closeLog := func() {}
	var output io.Writer = os.Stdout
	if settings.LogFile != "" {
		file, err := os.OpenFile(settings.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, err
		}
		closeLog = func() { _ = file.Close() }
		output = io.MultiWriter(os.Stdout, file)
	}

	var handler slog.Handler
	if strings.EqualFold(settings.LogFormat, "json") {
		handler = slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(output, &slog.HandlerOptions{Level: level})
	}
	log := slog.New(handler)
	slog.SetDefault(log)
	return log, closeLog, nil
}

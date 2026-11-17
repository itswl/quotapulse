// Command quotapulse tells you before something you depend on runs out or
// comes due.
//
//	quotapulse                 run the server
//	quotapulse demo            run on generated data, touching nothing real
//	quotapulse export          write the configuration and history as a bundle
//	quotapulse import FILE     merge a bundle into the database
//	quotapulse healthcheck     exit 0 if the local server answers
//	quotapulse version
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // the runtime image is empty: bring the time zones along

	"github.com/itswl/quotapulse/internal/api"
	"github.com/itswl/quotapulse/internal/buildinfo"
	"github.com/itswl/quotapulse/internal/bundle"
	"github.com/itswl/quotapulse/internal/demo"
	"github.com/itswl/quotapulse/internal/engine"
	"github.com/itswl/quotapulse/internal/mcp"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/runtime"
	"github.com/itswl/quotapulse/internal/secret"
	"github.com/itswl/quotapulse/internal/store"
	"github.com/itswl/quotapulse/web"
)

// config is everything read from the environment. It is deliberately short:
// what to watch, where to notify and how to behave are set in the dashboard
// and kept in the database. The environment only says how to start.
type config struct {
	Listen    string // QUOTAPULSE_LISTEN
	DataDir   string // QUOTAPULSE_DATA
	Password  string // QUOTAPULSE_PASSWORD
	Secret    string // QUOTAPULSE_SECRET
	Bootstrap string // QUOTAPULSE_BOOTSTRAP
	Mute      bool   // QUOTAPULSE_MUTE
	LogFormat string // QUOTAPULSE_LOG
	LogLevel  string // QUOTAPULSE_LOG_LEVEL
	Language  string // QUOTAPULSE_LANG, for a new install
	Timezone  string // TZ, for a new install
}

func loadConfig() config {
	get := func(key, fallback string) string {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
		return fallback
	}
	mute := strings.ToLower(get("QUOTAPULSE_MUTE", ""))
	return config{
		Listen:    get("QUOTAPULSE_LISTEN", ":8080"),
		DataDir:   get("QUOTAPULSE_DATA", "data"),
		Password:  os.Getenv("QUOTAPULSE_PASSWORD"),
		Secret:    os.Getenv("QUOTAPULSE_SECRET"),
		Bootstrap: get("QUOTAPULSE_BOOTSTRAP", ""),
		Mute:      mute == "1" || mute == "true" || mute == "yes",
		LogFormat: get("QUOTAPULSE_LOG", "text"),
		LogLevel:  get("QUOTAPULSE_LOG_LEVEL", "info"),
		Language:  get("QUOTAPULSE_LANG", ""),
		Timezone:  get("TZ", ""),
	}
}

// assets is the dashboard to serve: the one built into the binary, or — for
// working on the dashboard without rebuilding the binary — a directory named
// by QUOTAPULSE_WEB_DIR.
func assets() fs.FS {
	if dir := strings.TrimSpace(os.Getenv("QUOTAPULSE_WEB_DIR")); dir != "" {
		return os.DirFS(dir)
	}
	return web.Assets()
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "quotapulse:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	cfg := loadConfig()
	switch command {
	case "serve":
		return serve(cfg)
	case "demo":
		return runDemo(cfg, args)
	case "export":
		return runExport(cfg, args)
	case "import":
		return runImport(cfg, args)
	case "healthcheck":
		return healthcheck(cfg)
	case "version", "-v", "--version":
		fmt.Println("quotapulse", buildinfo.Version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q\n\n%s", command, usage)
}

const usage = `Usage:
  quotapulse                 run the server
  quotapulse demo            run on generated data, touching nothing real
  quotapulse export          write the configuration and history as a bundle
  quotapulse import FILE     merge a bundle into the database
  quotapulse healthcheck     exit 0 if the local server answers
  quotapulse version

Environment:
  QUOTAPULSE_PASSWORD   signs you in to the dashboard (required to use it)
  QUOTAPULSE_DATA       where the database lives (default: ./data)
  QUOTAPULSE_LISTEN     address to listen on (default: :8080)
  QUOTAPULSE_SECRET     encrypts stored credentials; a key file in the data
                        directory is used when this is not set
  QUOTAPULSE_BOOTSTRAP  a bundle to import when the database is empty
  QUOTAPULSE_MUTE       1 records notifications without sending them
  QUOTAPULSE_LOG        text or json (default: text)
  QUOTAPULSE_LOG_LEVEL  debug, info, warn or error (default: info)
  TZ, QUOTAPULSE_LANG   time zone and language (en, zh) of a new install;
                        both can be changed later in the dashboard
`

func newLogger(cfg config) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		level = slog.LevelInfo
	}
	options := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, options))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, options))
}

// writable checks that the data directory can be written to, and says what
// to do when it cannot. SQLite's own complaint about a read-only directory
// ("unable to open database file") sends people looking in the wrong place.
func writable(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("the data directory %s cannot be created: %w", dir, err)
	}
	probe, err := os.CreateTemp(dir, ".write-test-")
	if err != nil {
		return fmt.Errorf("the data directory %s is not writable by this process (uid %d). "+
			"Give that user the directory (chown %d %s), or run as the directory's owner — "+
			"with Docker Compose, QUOTAPULSE_USER=\"$(id -u):$(id -g)\" in .env",
			dir, os.Getuid(), os.Getuid(), dir)
	}
	probe.Close()
	return os.Remove(probe.Name())
}

// open opens the database in the data directory and builds an engine on it.
func open(cfg config, log *slog.Logger) (*engine.Engine, *secret.Keyring, error) {
	if err := writable(cfg.DataDir); err != nil {
		return nil, nil, err
	}
	keys, err := secret.Load(cfg.Secret, cfg.DataDir)
	if err != nil {
		return nil, nil, err
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "quotapulse.db"), keys)
	if err != nil {
		return nil, nil, err
	}
	fetcher := provider.NewClient()
	fetcher.UserAgent = buildinfo.UserAgent()
	e := engine.New(engine.Options{Store: st, Fetcher: fetcher, Sender: notify.NewSender(), Log: log, Mute: cfg.Mute})
	return e, keys, nil
}

// firstRun gives a brand-new database its starting settings and, if one was
// named, its bootstrap bundle.
func firstRun(ctx context.Context, e *engine.Engine, cfg config, log *slog.Logger) error {
	st := e.Store()
	if _, exists, err := st.State(ctx, "settings"); err != nil || exists {
		return err
	}
	settings, err := st.Settings(ctx)
	if err != nil {
		return err
	}
	if cfg.Timezone != "" {
		if _, err := time.LoadLocation(cfg.Timezone); err == nil {
			settings.Timezone = cfg.Timezone
		} else {
			log.Warn("TZ is not a known time zone; starting in UTC", "tz", cfg.Timezone)
		}
	}
	if cfg.Language == "zh" {
		settings.Language, settings.BaseCurrency = "zh", "CNY"
	}
	if err := st.SaveSettings(ctx, settings); err != nil {
		return err
	}
	if cfg.Bootstrap == "" {
		return nil
	}
	raw, err := os.ReadFile(cfg.Bootstrap)
	if err != nil {
		return fmt.Errorf("read the bootstrap bundle: %w", err)
	}
	parsed, err := bundle.Parse(raw)
	if err != nil {
		return fmt.Errorf("bootstrap bundle: %w", err)
	}
	report, err := bundle.Import(ctx, e, parsed, bundle.ImportOptions{Settings: true})
	if err != nil {
		return fmt.Errorf("bootstrap bundle: %w", err)
	}
	log.Info("bootstrap bundle imported", "created", report.Summary())
	for _, note := range report.Notes {
		log.Warn("bootstrap: " + note)
	}
	return nil
}

func serve(cfg config) error {
	log := newLogger(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	e, keys, err := open(cfg, log)
	if err != nil {
		return err
	}
	defer e.Store().Close()
	if err := firstRun(ctx, e, cfg, log); err != nil {
		return err
	}
	if cfg.Password == "" {
		log.Warn("QUOTAPULSE_PASSWORD is not set: the dashboard cannot be signed in to. API tokens still work.")
	}
	if cfg.Mute {
		log.Warn("QUOTAPULSE_MUTE is on: notifications are recorded, not sent")
	}
	runner := runtime.New(e, log, cfg.DataDir)
	return listen(ctx, cfg, log, &api.Server{
		Engine: e, Keys: keys, Runner: runner, Log: log, Password: cfg.Password,
		Assets: assets(), MCP: mcp.Handler(e, log), Version: buildinfo.Version,
	}, runner)
}

// listen runs the HTTP server and the background loops until ctx ends.
func listen(ctx context.Context, cfg config, log *slog.Logger, server *api.Server, runner *runtime.Runner) error {
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Bounds a request body; the largest is an imported bundle. There is
		// no write timeout on purpose: the event stream stays open.
		ReadTimeout: 2 * time.Minute,
		IdleTimeout: 2 * time.Minute,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()
	failed := make(chan error, 1)
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			failed <- err
		}
	}()
	log.Info("quotapulse is running", "version", buildinfo.Version, "listen", listener.Addr().String(), "data", cfg.DataDir)

	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdown)
	<-done
	return nil
}

// runDemo serves a throwaway instance on generated data. Every request it
// would make to the outside world is answered locally.
func runDemo(cfg config, args []string) error {
	flags := flag.NewFlagSet("demo", flag.ContinueOnError)
	listenAddr := flags.String("listen", cfg.Listen, "address to listen on")
	lang := flags.String("lang", "en", "language of the sample data: en or zh")
	at := flags.String("at", "", "pin the clock to this RFC 3339 time, for reproducible screenshots")
	if err := flags.Parse(args); err != nil {
		return err
	}
	now := time.Now
	if *at != "" {
		pinned, err := time.Parse(time.RFC3339, *at)
		if err != nil {
			return fmt.Errorf("--at must be an RFC 3339 time: %w", err)
		}
		now = func() time.Time { return pinned }
	}
	log := newLogger(cfg)
	dir, err := os.MkdirTemp("", "quotapulse-demo-")
	if err != nil {
		// The runtime image has no /tmp; the data directory is the one
		// place there that can be written to.
		if dir, err = os.MkdirTemp(cfg.DataDir, "demo-"); err != nil {
			return fmt.Errorf("the demo needs a directory to work in: %w", err)
		}
	}
	defer os.RemoveAll(dir)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	world := demo.New(*lang, now)
	keys := secret.FromPassphrase("demo")
	st, err := store.Open(filepath.Join(dir, "quotapulse.db"), keys)
	if err != nil {
		return err
	}
	defer st.Close()
	fetcher := provider.NewClient()
	fetcher.HTTP = world.HTTPClient()
	sender := notify.NewSender()
	sender.HTTP = world.HTTPClient()
	e := engine.New(engine.Options{Store: st, Fetcher: fetcher, Sender: sender, Log: log, Now: now})
	if err := world.Seed(ctx, e); err != nil {
		return fmt.Errorf("seed the demo: %w", err)
	}
	cfg.Listen, cfg.DataDir = *listenAddr, ""
	log.Info("demo data is ready; sign in with the password \"" + demo.Password + "\"")
	runner := runtime.New(e, log, "")
	return listen(ctx, cfg, log, &api.Server{
		Engine: e, Keys: keys, Runner: runner, Log: log, Password: demo.Password,
		Assets: assets(), MCP: mcp.Handler(e, log), Version: buildinfo.Version, Demo: true,
	}, runner)
}

func runExport(cfg config, args []string) error {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	secrets := flags.Bool("secrets", false, "include credentials, channel settings and token hashes in the clear")
	noHistory := flags.Bool("no-history", false, "leave out readings and closed incidents")
	output := flags.String("o", "", "write to this file instead of standard output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	e, _, err := open(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		return err
	}
	defer e.Store().Close()
	exported, err := bundle.Export(context.Background(), e, bundle.ExportOptions{
		Secrets: *secrets, History: !*noHistory, Source: "quotapulse " + buildinfo.Version,
	})
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(exported, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if *output == "" {
		_, err = os.Stdout.Write(raw)
		return err
	}
	// A bundle with secrets is as sensitive as the database.
	return os.WriteFile(*output, raw, 0o600)
}

func runImport(cfg config, args []string) error {
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	withSettings := flags.Bool("settings", false, "also apply the bundle's settings")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: quotapulse import [--settings] FILE")
	}
	raw, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		return err
	}
	parsed, err := bundle.Parse(raw)
	if err != nil {
		return err
	}
	log := newLogger(cfg)
	e, _, err := open(cfg, log)
	if err != nil {
		return err
	}
	defer e.Store().Close()
	ctx := context.Background()
	if err := firstRun(ctx, e, config{Timezone: cfg.Timezone, Language: cfg.Language}, log); err != nil {
		return err
	}
	report, err := bundle.Import(ctx, e, parsed, bundle.ImportOptions{Settings: *withSettings})
	if err != nil {
		return err
	}
	fmt.Println("imported:", report.Summary())
	for _, note := range report.Notes {
		fmt.Println("  note:", note)
	}
	return nil
}

// healthcheck asks the local server whether it is alive. The runtime image
// has no shell and no curl, so the binary checks itself.
func healthcheck(cfg config) error {
	_, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/readyz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("not ready: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

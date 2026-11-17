// Package runtime keeps QuotaPulse moving when nobody is looking: it samples
// accounts as they come due, drains the outbox, watches the calendar and
// tidies up once a day.
//
// Everything runs inside the one process. There is no cron and no queue to
// operate; what each loop last did is kept so the dashboard can show it, and
// so a loop that has stopped is noticed.
package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/itswl/quotapulse/internal/engine"
)

const (
	// pumpEvery is how often the outbox and the sampling schedule are
	// checked when nothing wakes the loop sooner.
	pumpEvery = 3 * time.Second
	// clockEvery is how often the calendar is looked at. Reminders go out
	// at a time of day with minute precision; half a minute keeps them on it.
	clockEvery = 30 * time.Second
	// housekeepingEvery is how often the daily chores are considered.
	housekeepingEvery = 15 * time.Minute

	backupsKept        = 7
	housekeepingMarker = "housekeeping.last"
)

// Job is what one loop last did.
type Job struct {
	Name      string     `json:"name"`
	LastRun   *time.Time `json:"last_run"`
	LastError string     `json:"last_error"`
	Runs      int        `json:"runs"`
	// Every is how often the job is expected to run, in seconds.
	Every int `json:"every_seconds"`
}

// Runner owns the background loops.
type Runner struct {
	engine  *engine.Engine
	log     *slog.Logger
	dataDir string

	mu       sync.Mutex
	jobs     map[string]*Job
	sampling bool
}

// New prepares a Runner. dataDir is where daily backups are written; empty
// turns them off.
func New(e *engine.Engine, log *slog.Logger, dataDir string) *Runner {
	if log == nil {
		log = slog.Default()
	}
	r := &Runner{engine: e, log: log, dataDir: dataDir, jobs: map[string]*Job{}}
	for name, every := range map[string]time.Duration{
		"sample": pumpEvery, "deliver": pumpEvery, "calendar": clockEvery, "housekeeping": housekeepingEvery,
	} {
		r.jobs[name] = &Job{Name: name, Every: int(every.Seconds())}
	}
	return r
}

// Run blocks until ctx is done.
func (r *Runner) Run(ctx context.Context) {
	var wg sync.WaitGroup
	loops := []func(context.Context){r.pump, r.clock, r.housekeeping}
	for _, loop := range loops {
		wg.Add(1)
		go func() {
			defer wg.Done()
			loop(ctx)
		}()
	}
	wg.Wait()
}

// Jobs reports what each loop last did, by name.
func (r *Runner) Jobs() []Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Job, 0, len(r.jobs))
	for _, job := range r.jobs {
		out = append(out, *job)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Healthy reports whether every loop has run recently. A loop that has
// stopped means nothing is being watched, however healthy the HTTP server
// looks; the readiness probe says so.
func (r *Runner) Healthy(now time.Time) (bool, string) {
	for _, job := range r.Jobs() {
		if job.LastRun == nil {
			continue // not started yet
		}
		allowed := 10*time.Duration(job.Every)*time.Second + 2*time.Minute
		if now.Sub(*job.LastRun) > allowed {
			return false, fmt.Sprintf("the %s loop has not run since %s", job.Name, job.LastRun.Format(time.RFC3339))
		}
	}
	return true, ""
}

func (r *Runner) record(name string, err error) {
	now := time.Now().UTC().Truncate(time.Second)
	r.mu.Lock()
	defer r.mu.Unlock()
	job := r.jobs[name]
	job.LastRun, job.Runs = &now, job.Runs+1
	job.LastError = ""
	if err != nil {
		job.LastError = err.Error()
		r.log.Error("background job failed", "job", name, "error", err)
	}
}

// guard runs fn and turns a panic into an error: one bad reading must not
// take the loops down with it.
func guard(fn func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	return fn()
}

// pump delivers what is due and starts sampling what is due. It wakes on a
// timer, and at once when the engine says something has just become due.
func (r *Runner) pump(ctx context.Context) {
	ticker := time.NewTicker(pumpEvery)
	defer ticker.Stop()
	for {
		r.record("deliver", guard(func() error {
			r.engine.Dispatch(ctx)
			return nil
		}))
		r.startSampling(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-r.engine.Wake():
		}
	}
}

// startSampling samples due accounts in the background, unless a round is
// still running. A slow platform must not hold up deliveries.
func (r *Runner) startSampling(ctx context.Context) {
	r.mu.Lock()
	if r.sampling {
		r.mu.Unlock()
		return
	}
	r.sampling = true
	r.mu.Unlock()
	go func() {
		err := guard(func() error {
			r.engine.SampleDue(ctx)
			return nil
		})
		r.mu.Lock()
		r.sampling = false
		r.mu.Unlock()
		r.record("sample", err)
	}()
}

// clock looks at the calendar: date reminders, silent meters, the digest.
func (r *Runner) clock(ctx context.Context) {
	ticker := time.NewTicker(clockEvery)
	defer ticker.Stop()
	for {
		r.record("calendar", guard(func() error {
			if err := r.engine.EvaluateReminders(ctx); err != nil {
				return err
			}
			if err := r.engine.EvaluateStale(ctx); err != nil {
				return err
			}
			_, err := r.engine.MaybeDigest(ctx)
			return err
		}))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// housekeeping prunes old history and writes a backup, once a day.
func (r *Runner) housekeeping(ctx context.Context) {
	ticker := time.NewTicker(housekeepingEvery)
	defer ticker.Stop()
	for {
		r.record("housekeeping", guard(func() error { return r.dailyChores(ctx) }))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runner) dailyChores(ctx context.Context) error {
	st := r.engine.Store()
	settings, err := st.Settings(ctx)
	if err != nil {
		return err
	}
	now := r.engine.Now()
	today := now.In(settings.Location()).Format("2006-01-02")
	if last, _, err := st.State(ctx, housekeepingMarker); err != nil || last == today {
		return err
	}
	removed, err := st.Prune(ctx, now.AddDate(0, 0, -settings.RetentionDays))
	if err != nil {
		return fmt.Errorf("prune: %w", err)
	}
	if removed > 0 {
		r.log.Info("pruned old readings", "rows", removed, "older_than_days", settings.RetentionDays)
	}
	if _, err := r.Backup(ctx); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return st.SetState(ctx, housekeepingMarker, today)
}

// Backup writes a snapshot of the database into the backups directory and
// removes all but the newest few. It returns the file it wrote.
func (r *Runner) Backup(ctx context.Context) (string, error) {
	if r.dataDir == "" {
		return "", nil
	}
	dir := filepath.Join(r.dataDir, "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "quotapulse-"+r.engine.Now().UTC().Format("20060102")+".db")
	if err := r.engine.Store().Backup(ctx, path); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return path, err
	}
	var backups []string
	for _, entry := range entries {
		if name := entry.Name(); strings.HasPrefix(name, "quotapulse-") && strings.HasSuffix(name, ".db") {
			backups = append(backups, name)
		}
	}
	sort.Strings(backups) // the date in the name sorts chronologically
	for len(backups) > backupsKept {
		_ = os.Remove(filepath.Join(dir, backups[0]))
		backups = backups[1:]
	}
	r.log.Info("backup written", "path", path)
	return path, nil
}

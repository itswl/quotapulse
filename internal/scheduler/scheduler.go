// Package scheduler is the in-process job scheduler, replacing cron in the
// container.
//
// Why not cron: metrics accumulate in-process. Jobs running in another
// process would leave Prometheus forever scraping an idle one.
//
// Two trigger kinds: fixed intervals (dashboard refresh) and fixed daily
// times (alert checks, mailbox scans, the weekly report). All jobs run
// sequentially in one goroutine, never concurrently; one failing task only
// records a failure and never affects the others.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/itswl/quotapulse/internal/timeutil"
)

// Task is one scheduled job. Interval or DailyTimes, never both; neither means disabled.
type Task struct {
	Name        string
	Description string
	// Run's detail is passed along the run record to OnResult, displayed by /api/jobs.
	Run        func(ctx context.Context) (any, error)
	Interval   time.Duration
	DailyTimes []timeutil.ClockTime
	Weekdays   map[int]bool // ISO weekdays: 1=Monday through 7=Sunday.
	RunAtStart bool
	// Timeout caps a single run; the run context is cancelled at the deadline and the
	// result records a timeout error. Zero disables the cap.
	Timeout time.Duration

	nextRun time.Time
}

// Enabled reports whether the task actually fires.
func (t *Task) Enabled() bool { return t.Interval > 0 || len(t.DailyTimes) > 0 }

// ScheduleText is the human-readable trigger description.
func (t *Task) ScheduleText() string {
	if t.Interval > 0 {
		return fmt.Sprintf("Every %d seconds", int(t.Interval.Seconds()))
	}
	return timeutil.Describe(t.DailyTimes, t.Weekdays)
}

// NextRun is the next fire time; the zero value means it never fires.
func (t *Task) NextRun() time.Time { return t.nextRun }

func (t *Task) initialNextRun(now time.Time) time.Time {
	if t.Interval > 0 {
		if t.RunAtStart {
			return now
		}
		return now.Add(t.Interval)
	}
	next, _ := timeutil.NextOccurrence(now, t.DailyTimes, t.Weekdays)
	return next
}

func (t *Task) followingRun(now time.Time) time.Time {
	if t.Interval > 0 {
		return now.Add(t.Interval)
	}
	next, _ := timeutil.NextOccurrence(now, t.DailyTimes, t.Weekdays)
	return next
}

// Result is one task run's outcome.
type Result struct {
	Name      string
	Success   bool
	StartedAt time.Time
	Duration  time.Duration
	Err       error
	Detail    any
	NextRun   time.Time
}

// Scheduler runs due tasks sequentially.
type Scheduler struct {
	tasks    []*Task
	onResult func(Result)
	log      *slog.Logger
	maxWait  time.Duration

	runMu sync.Mutex // manual triggers never race the background loop
	done  chan struct{}
	stop  chan struct{}
	once  sync.Once
}

// New creates the scheduler. onResult is optional; it hands run records to the
// state manager and metrics — the scheduler itself depends on neither.
func New(tasks []*Task, onResult func(Result), log *slog.Logger) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	now := time.Now()
	for _, t := range tasks {
		if t.Enabled() {
			t.nextRun = t.initialNextRun(now)
		}
	}
	return &Scheduler{
		tasks:    tasks,
		onResult: onResult,
		log:      log,
		maxWait:  time.Minute, // one-minute cap: clock jumps get picked up promptly
		done:     make(chan struct{}),
		stop:     make(chan struct{}),
	}
}

// Tasks returns all tasks for startup-time registration.
func (s *Scheduler) Tasks() []*Task { return s.tasks }

// Start runs the scheduling loop in a background goroutine.
func (s *Scheduler) Start(ctx context.Context) {
	descriptions := make([]string, 0, len(s.tasks))
	for _, t := range s.tasks {
		descriptions = append(descriptions, t.Name+"="+t.ScheduleText())
	}
	s.log.Info("Scheduler started", "tasks", strings.Join(descriptions, ", "))

	go func() {
		defer close(s.done)
		for {
			s.RunPending(ctx, time.Now())
			select {
			case <-ctx.Done():
				s.log.Info("Scheduler stopped")
				return
			case <-s.stop:
				s.log.Info("Scheduler stopped")
				return
			case <-time.After(s.sleepFor(time.Now())):
			}
		}
	}()
}

// Stop halts the loop, waiting at most timeout.
func (s *Scheduler) Stop(timeout time.Duration) {
	s.once.Do(func() { close(s.stop) })
	select {
	case <-s.done:
	case <-time.After(timeout):
	}
}

// sleepFor is the time until the next task, clamped to [0.1s, maxWait].
func (s *Scheduler) sleepFor(now time.Time) time.Duration {
	wait := s.maxWait
	for _, t := range s.tasks {
		if !t.Enabled() || t.nextRun.IsZero() {
			continue
		}
		if d := t.nextRun.Sub(now); d < wait {
			wait = d
		}
	}
	return max(100*time.Millisecond, min(s.maxWait, wait))
}

// RunPending executes every due task and returns this round's results.
func (s *Scheduler) RunPending(ctx context.Context, now time.Time) []Result {
	var results []Result
	for _, t := range s.tasks {
		if !t.Enabled() || t.nextRun.IsZero() || t.nextRun.After(now) {
			continue
		}
		results = append(results, s.RunTask(ctx, t))
	}
	return results
}

// RunTask executes one task immediately and advances its next time. Manual triggers go through here too, so they never race the loop.
func (s *Scheduler) RunTask(ctx context.Context, t *Task) Result {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	startedAt := time.Now()
	s.log.Info("Task started", "task", t.Name)

	runCtx, cancel := context.WithCancel(ctx)
	if t.Timeout > 0 {
		var withDeadline context.CancelFunc
		runCtx, withDeadline = context.WithTimeout(ctx, t.Timeout)
		defer withDeadline()
	}
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()

	detail, err := t.Run(runCtx)
	if err != nil && ctx.Err() == nil && t.Timeout > 0 && errors.Is(err, context.DeadlineExceeded) {
		err = fmt.Errorf("timed out after %s: %w", t.Timeout, err)
	}
	result := Result{
		Name:      t.Name,
		Success:   err == nil,
		StartedAt: startedAt,
		Duration:  time.Since(startedAt),
		Err:       err,
		Detail:    detail,
	}
	if err == nil {
		s.log.Info("Task completed", "task", t.Name, "seconds", result.Duration.Seconds())
	} else {
		s.log.Error("Task failed", "task", t.Name, "error", err)
	}

	if t.Enabled() {
		t.nextRun = t.followingRun(time.Now())
	}
	result.NextRun = t.nextRun

	if s.onResult != nil {
		s.onResult(result)
	}
	return result
}

// Lookup finds a task by name; the HTTP layer uses it for manual triggers.
func (s *Scheduler) Lookup(name string) *Task {
	for _, t := range s.tasks {
		if t.Name == name {
			return t
		}
	}
	return nil
}

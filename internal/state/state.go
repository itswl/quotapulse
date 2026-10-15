// Package state holds the runtime state shown on the dashboard.
//
// The background scheduler thread writes; HTTP handlers read — true
// concurrency. Every access happens under the lock, and reads return copies
// the caller may mutate freely without touching internal state.
//
// This state lives only in process memory and clears on restart — it is
// "the result of the last check", not a ledger. Anything that must survive
// lives in store.
package state

import (
	"sync"
	"time"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/timeutil"
)

// BalanceState is the /api/credits response body.
type BalanceState struct {
	LastUpdate *string              `json:"last_update"`
	Projects   []model.CheckResult  `json:"projects"`
	Summary    model.BalanceSummary `json:"summary"`
}

// SubscriptionState is the /api/subscriptions response body.
type SubscriptionState struct {
	LastUpdate    *string                    `json:"last_update"`
	Subscriptions []model.SubscriptionResult `json:"subscriptions"`
	Summary       SubscriptionSummary        `json:"summary"`
}

// SubscriptionSummary is the subscription view's header count.
type SubscriptionSummary struct {
	Total     int `json:"total"`
	NeedAlert int `json:"need_alert"`
}

// EmailState is the /api/email/scan response body.
type EmailState struct {
	LastUpdate *string               `json:"last_update"`
	Days       *int                  `json:"days"`
	DryRun     *bool                 `json:"dry_run"`
	Mailboxes  []model.MailboxResult `json:"mailboxes"`
	Alerts     []model.EmailAlert    `json:"alerts"`
	Summary    EmailSummary          `json:"summary"`
}

// EmailSummary is the mailbox view's header count.
type EmailSummary struct {
	TotalMailboxes  int `json:"total_mailboxes"`
	FailedMailboxes int `json:"failed_mailboxes"`
	TotalEmails     int `json:"total_emails"`
	TotalAlerts     int `json:"total_alerts"`
	AlertsSent      int `json:"alerts_sent"`
}

// Job is one scheduled task's runtime state, an element of /api/jobs.
type Job struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Schedule     string   `json:"schedule"`
	Enabled      bool     `json:"enabled"`
	NextRun      *string  `json:"next_run"`
	LastRun      *string  `json:"last_run"`
	LastSuccess  *string  `json:"last_success"`
	LastError    *string  `json:"last_error"`
	LastDuration *float64 `json:"last_duration_seconds"`
	LastDetail   any      `json:"last_detail"`
	Runs         int      `json:"runs"`
	Failures     int      `json:"failures"`
}

// JobState is the /api/jobs response body. healthy = every enabled task's last run succeeded.
type JobState struct {
	Healthy bool  `json:"healthy"`
	Jobs    []Job `json:"jobs"`
}

// Manager is the thread-safe state container.
type Manager struct {
	mu        sync.RWMutex
	startTime time.Time
	balance   BalanceState
	// balanceChecked is set once any balance check has completed.
	balanceChecked bool
	subs           SubscriptionState
	email          EmailState
	jobs           map[string]*Job
	jobOrder       []string // registration order, keeping /api/jobs output stable
}

// New creates the state container.
func New() *Manager {
	return &Manager{
		startTime: time.Now(),
		jobs:      make(map[string]*Job),
	}
}

// UptimeSeconds is how long the process has been up.
func (m *Manager) UptimeSeconds() float64 { return time.Since(m.startTime).Seconds() }

// SetBalance replaces the balance state wholesale.
func (m *Manager) SetBalance(results []model.CheckResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.balanceChecked = true
	m.setBalanceLocked(results, len(results) == 0 || anySucceeded(results))
}

// MergeBalance merges a partial refresh by project name, for single-project refreshes.
func (m *Manager) MergeBalance(results []model.CheckResult) {
	m.mu.Lock()
	defer m.mu.Unlock()

	merged := make([]model.CheckResult, len(m.balance.Projects))
	copy(merged, m.balance.Projects)
	index := make(map[string]int, len(merged))
	for i, p := range merged {
		index[p.Project] = i
	}
	for _, r := range results {
		if i, ok := index[r.Project]; ok {
			merged[i] = r
		} else {
			index[r.Project] = len(merged)
			merged = append(merged, r)
		}
	}
	m.balanceChecked = true
	m.setBalanceLocked(merged, anySucceeded(results))
}

// RemoveBalanceProject drops a project from the dashboard right after deletion, without waiting for the next check.
func (m *Manager) RemoveBalanceProject(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	kept := make([]model.CheckResult, 0, len(m.balance.Projects))
	for _, p := range m.balance.Projects {
		if p.Project != name {
			kept = append(kept, p)
		}
	}
	if len(kept) != len(m.balance.Projects) {
		m.setBalanceLocked(kept, false)
	}
}

// setBalanceLocked stores results. last_update is the time balances were last actually
// read, so it only advances when fresh reads succeeded: a total provider outage must
// age the dashboard instead of stamping "updated just now" on a page of errors.
func (m *Manager) setBalanceLocked(results []model.CheckResult, fresh bool) {
	lastUpdate := m.balance.LastUpdate
	if fresh {
		now := timeutil.NowISO()
		lastUpdate = &now
	}
	m.balance = BalanceState{
		LastUpdate: lastUpdate,
		Projects:   results,
		Summary:    model.SummarizeBalance(results),
	}
}

// BalanceChecked reports whether any balance check has completed since startup. Before
// that there is nothing to show; after it an empty project list is a real answer.
func (m *Manager) BalanceChecked() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.balanceChecked
}

func anySucceeded(results []model.CheckResult) bool {
	for _, r := range results {
		if r.Success {
			return true
		}
	}
	return false
}

// Balance returns a copy of the balance state.
func (m *Manager) Balance() BalanceState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := m.balance
	// Never nil: an empty list must encode as [] for API clients, not null.
	out.Projects = append([]model.CheckResult{}, m.balance.Projects...)
	return out
}

// SetSubscriptions replaces the subscription state wholesale.
func (m *Manager) SetSubscriptions(results []model.SubscriptionResult) {
	m.mu.Lock()
	defer m.mu.Unlock()

	summary := SubscriptionSummary{Total: len(results)}
	for _, r := range results {
		if r.NeedAlert {
			summary.NeedAlert++
		}
	}
	now := timeutil.NowISO()
	m.subs = SubscriptionState{LastUpdate: &now, Subscriptions: results, Summary: summary}
}

// Subscriptions returns a copy of the subscription state.
func (m *Manager) Subscriptions() SubscriptionState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := m.subs
	out.Subscriptions = append([]model.SubscriptionResult(nil), m.subs.Subscriptions...)
	return out
}

// SetEmailScan replaces the mailbox scan state.
func (m *Manager) SetEmailScan(result model.ScanResult) {
	m.mu.Lock()
	defer m.mu.Unlock()

	summary := EmailSummary{
		TotalMailboxes: len(result.Mailboxes),
		TotalAlerts:    len(result.Alerts),
	}
	for _, mb := range result.Mailboxes {
		if mb.Error != nil {
			summary.FailedMailboxes++
		}
		summary.TotalEmails += mb.TotalEmails
	}
	for _, a := range result.Alerts {
		if a.AlertSent {
			summary.AlertsSent++
		}
	}
	now := timeutil.NowISO()
	days, dryRun := result.Days, result.DryRun
	m.email = EmailState{
		LastUpdate: &now, Days: &days, DryRun: &dryRun,
		Mailboxes: result.Mailboxes, Alerts: result.Alerts, Summary: summary,
	}
}

// EmailScan returns a copy of the mailbox scan state.
func (m *Manager) EmailScan() EmailState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := m.email
	out.Mailboxes = append([]model.MailboxResult(nil), m.email.Mailboxes...)
	out.Alerts = append([]model.EmailAlert(nil), m.email.Alerts...)
	return out
}

// ---------- Scheduled jobs ----------

// RegisterJob registers a task's static info; run records arrive via RecordJobRun.
func (m *Manager) RegisterJob(name, description, schedule string, enabled bool, nextRun time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[name]
	if !ok {
		job = &Job{Name: name}
		m.jobs[name] = job
		m.jobOrder = append(m.jobOrder, name)
	}
	job.Description = description
	job.Schedule = schedule
	job.Enabled = enabled
	if enabled {
		job.NextRun = timeutil.UTCISO(nextRun)
	} else {
		job.NextRun = nil
	}
}

// RecordJobRun records one task run.
func (m *Manager) RecordJobRun(name string, success bool, startedAt time.Time,
	duration time.Duration, runErr error, detail any, nextRun time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[name]
	if !ok {
		job = &Job{Name: name, Enabled: true}
		m.jobs[name] = job
		m.jobOrder = append(m.jobOrder, name)
	}
	job.LastRun = timeutil.UTCISO(startedAt)
	seconds := duration.Seconds()
	job.LastDuration = &seconds
	if job.Enabled {
		job.NextRun = timeutil.UTCISO(nextRun)
	} else {
		job.NextRun = nil
	}
	job.Runs++
	if success {
		job.LastSuccess = job.LastRun
		job.LastError = nil
		job.LastDetail = detail
		return
	}
	job.Failures++
	message := "Unknown error"
	if runErr != nil {
		message = runErr.Error()
	}
	job.LastError = &message
}

// Jobs returns a copy of the job state.
func (m *Manager) Jobs() JobState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := JobState{Healthy: true, Jobs: make([]Job, 0, len(m.jobOrder))}
	for _, name := range m.jobOrder {
		job := m.jobs[name]
		if job.Enabled && job.LastError != nil {
			out.Healthy = false
		}
		out.Jobs = append(out.Jobs, *job)
	}
	return out
}

// FailedJobs returns the enabled tasks whose last run failed; /health cites them when unhealthy.
func (m *Manager) FailedJobs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var failed []string
	for _, name := range m.jobOrder {
		if job := m.jobs[name]; job.Enabled && job.LastError != nil {
			failed = append(failed, name)
		}
	}
	return failed
}

// Package monitor provides the package implementation.
//
// Implementation note.
// Implementation note.
package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/runway"
	"github.com/itswl/quotapulse/internal/store"
)

// Implementation note.
type Monitor struct {
	Settings *config.Settings
	Resolver *config.Resolver
	Store    store.Store
	Notifier notify.Notifier
	Client   *provider.Client
	Alerter  *runway.Alerter
	Log      *slog.Logger

	// Implementation note.
	OnNotify func(kind string, ok bool)

	cache responseCache
}

// Implementation note.
type Outcome struct {
	Results []model.CheckResult
	Runways map[string]model.Runway
	Sent    runway.Sent
}

// Implementation note.
// Implementation note.
func (m *Monitor) Run(ctx context.Context, projectName string, dryRun bool) (Outcome, error) {
	started := time.Now()
	cfg := m.Resolver.Load(ctx)

	projects := m.selectProjects(cfg, projectName)
	if len(projects) == 0 {
		if projectName != "" {
			return Outcome{}, fmt.Errorf("operation: %s", projectName)
		}
		m.log().Warn("No projects to monitor; check {PROVIDER}_API_KEY or database dynamic configuration")
		return Outcome{}, nil
	}

	m.log().Info("Starting monitoring", "projects", len(projects), "dry_run", dryRun)
	results := m.checkAll(ctx, projects, dryRun)

	outcome := Outcome{Results: results}
	outcome.Runways, outcome.Sent = m.analyze(ctx, results, dryRun)

	m.logSummary(results, time.Since(started))
	return outcome, nil
}

// Implementation note.
// Implementation note.
func (m *Monitor) selectProjects(cfg model.Config, projectName string) []model.Project {
	if projectName == "" {
		return cfg.EnabledProjects()
	}
	for _, p := range cfg.Projects {
		if p.Name == projectName {
			return []model.Project{p}
		}
	}
	return nil
}

// Implementation note.
func (m *Monitor) checkAll(ctx context.Context, projects []model.Project, dryRun bool) []model.CheckResult {
	workers := min(m.Settings.Concurrency(), len(projects))
	m.log().Info("Concurrent checks", "workers", workers, "projects", len(projects))

	results := make([]model.CheckResult, len(projects))
	jobs := make(chan int)
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = m.CheckProject(ctx, projects[i], dryRun)
			}
		}()
	}
	for i := range projects {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}

// Implementation note.
func (m *Monitor) CheckProject(ctx context.Context, p model.Project, dryRun bool) model.CheckResult {
	m.log().Info("Checking project", "project", p.Name, "provider", p.Provider, "threshold", p.Threshold)

	adapter, err := provider.New(p.Provider, p.APIKey, m.Client)
	if err != nil {
		return m.failure(p, err)
	}

	cacheKey := cacheKeyFor(p.Provider, p.APIKey)
	ttl := time.Duration(m.Settings.ResponseCacheTTL) * time.Second
	credits, cached := m.cache.get(cacheKey, ttl)
	if cached {
		m.log().Info("Using cached result", "project", p.Name, "ttl_seconds", m.Settings.ResponseCacheTTL)
	} else {
		if credits, err = adapter.Fetch(ctx); err != nil {
			m.log().Error("Failed to fetch balance", "project", p.Name, "error", err)
			return m.failure(p, err)
		}
		if ttl > 0 {
			m.cache.set(cacheKey, credits)
		}
	}
	m.log().Info("Current balance", "project", p.Name, "credits", credits)

	result := model.CheckResult{
		Project:      p.Name,
		OwnerProject: p.OwnerProject,
		Provider:     p.Provider,
		Type:         p.Type,
		Success:      true,
		Credits:      &credits,
		Threshold:    model.Ptr(p.Threshold),
		NeedAlarm:    credits < p.Threshold,
		Cached:       cached,
	}

	projectID := p.ID()
	if err := m.Store.SaveBalance(ctx, store.BalanceRecord{
		ProjectID: projectID, ProjectName: p.Name, Provider: p.Provider,
		Balance: credits, Threshold: model.Ptr(p.Threshold),
		BalanceType: p.Type, NeedAlarm: result.NeedAlarm,
	}); err != nil {
		m.log().Warn("Failed to record balance history", "project", p.Name, "error", err)
	}

	if !result.NeedAlarm {
		m.log().Info("Balance is sufficient", "project", p.Name, "credits", credits, "threshold", p.Threshold)
		return result
	}

	m.log().Warn("Low balance", "project", p.Name, "credits", credits, "threshold", p.Threshold)
	if dryRun {
		return result
	}
	result.AlarmSent = m.sendBalanceAlert(ctx, p, projectID, credits)
	return result
}

// Implementation note.
func (m *Monitor) sendBalanceAlert(ctx context.Context, p model.Project, projectID string, credits float64) bool {
	cooldown := time.Duration(m.Settings.CooldownSeconds("balance")) * time.Second
	cooling, err := m.Store.HasRecentAlert(ctx, projectID, "low_balance", cooldown)
	if err != nil {
		m.log().Warn("Failed to query alert cooldown; treating it as not cooling down", "project", p.Name, "error", err)
	}
	if cooling {
		m.log().Info("Alert is cooling down; skipping duplicate notification", "project", p.Name, "cooldown", cooldown)
		return false
	}
	if m.Notifier == nil {
		m.log().Error("Webhook URL is not configured")
		return false
	}

	msg := notify.BalanceAlert(p.Name, p.OwnerProject, provider.DisplayName(p.Provider), credits, p.Threshold)
	sendErr := m.Notifier.Send(ctx, msg)
	if m.OnNotify != nil {
		m.OnNotify(msg.Kind, sendErr == nil)
	}
	if sendErr != nil {
		m.log().Error("Failed to send balance alert", "project", p.Name, "error", sendErr)
		return false
	}

	if err := m.Store.SaveAlert(ctx, store.AlertRecord{
		AlertID: projectID, Name: p.Name, AlertType: "low_balance",
		Message:   fmt.Sprintf("Low balance: %v < %v", credits, p.Threshold),
		Value:     &credits,
		Threshold: model.Ptr(p.Threshold),
	}); err != nil {
		m.log().Warn("Failed to record alert", "project", p.Name, "error", err)
	}
	return true
}

// Implementation note.
// Implementation note.
func (m *Monitor) analyze(ctx context.Context, results []model.CheckResult, dryRun bool) (map[string]model.Runway, runway.Sent) {
	series, err := m.Store.BalanceSeries(ctx, m.Settings.BurnRateWindowDays)
	if err != nil {
		m.log().Warn("Failed to read balance history; skipping trend analysis", "error", err)
		return nil, runway.Sent{}
	}
	runways := runway.ComputeAll(series, m.Settings.BurnRateWindowDays, time.Now())
	if len(runways) == 0 {
		return nil, runway.Sent{}
	}
	runway.Attach(results, runways)

	if m.Alerter == nil {
		return runways, runway.Sent{}
	}
	return runways, m.Alerter.Check(ctx, results, dryRun)
}

func (m *Monitor) failure(p model.Project, err error) model.CheckResult {
	message := err.Error()
	return model.CheckResult{
		Project: p.Name, OwnerProject: p.OwnerProject, Provider: p.Provider,
		Type: p.Type, Success: false, Error: &message,
	}
}

func (m *Monitor) logSummary(results []model.CheckResult, elapsed time.Duration) {
	summary := model.SummarizeBalance(results)
	m.log().Info("Check summary",
		"total", summary.Total, "success", summary.Success,
		"failed", summary.Failed, "need_alarm", summary.NeedAlarm,
		"seconds", elapsed.Seconds())

	for _, r := range results {
		if !r.Success {
			m.log().Error("Check failed", "project", r.Project, "error", deref(r.Error))
			continue
		}
		if r.NeedAlarm {
			m.log().Warn("Alert required", "project", r.Project, "credits", deref(r.Credits), "threshold", deref(r.Threshold), "sent", r.AlarmSent)
		}
	}
}

func (m *Monitor) log() *slog.Logger {
	if m.Log != nil {
		return m.Log
	}
	return slog.Default()
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// cacheKeyFor identifies one credential inside the in-memory response cache. The key never
// leaves this process, so the hash only has to keep keys apart without storing the key
// itself — SHA-256 rather than MD5 so the digest is not the weak link if it ever escapes.
func cacheKeyFor(providerKey, apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return providerKey + ":" + hex.EncodeToString(sum[:])
}

// Implementation note.
type responseCache struct {
	mu   sync.Mutex
	data map[string]cacheEntry
}

type cacheEntry struct {
	at      time.Time
	credits float64
}

func (c *responseCache) get(key string, ttl time.Duration) (float64, bool) {
	if ttl <= 0 {
		return 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.data[key]
	if !ok || time.Since(entry.at) >= ttl {
		delete(c.data, key)
		return 0, false
	}
	return entry.credits, true
}

func (c *responseCache) set(key string, credits float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data == nil {
		c.data = make(map[string]cacheEntry)
	}
	c.data[key] = cacheEntry{at: time.Now(), credits: credits}
}

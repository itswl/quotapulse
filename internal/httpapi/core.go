package httpapi

import (
	"math"
	"net/http"
	"time"
)

// Implementation note.
// Implementation note.
const stalenessMultiplier = 3

type liveBody struct {
	Status        string `json:"status"`
	UptimeSeconds int    `json:"uptime_seconds"`
	Version       string `json:"version"`
}

func (s *Server) handleLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, liveBody{
		Status:        "alive",
		UptimeSeconds: int(s.State.UptimeSeconds()),
		Version:       s.Settings.AppVersion,
	})
}

type healthBody struct {
	Status        string   `json:"status"`
	HasData       bool     `json:"has_data"`
	IsStale       bool     `json:"is_stale"`
	JobsHealthy   bool     `json:"jobs_healthy"`
	FailedJobs    []string `json:"failed_jobs"`
	LastUpdate    *string  `json:"last_update"`
	UptimeSeconds int      `json:"uptime_seconds"`
	Version       string   `json:"version"`
}

// Implementation note.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	balance := s.State.Balance()
	jobs := s.State.Jobs()
	failed := s.State.FailedJobs()
	if failed == nil {
		failed = []string{}
	}

	hasData := len(balance.Projects) > 0
	isStale := s.isStale(balance.LastUpdate)
	healthy := hasData && !isStale && jobs.Healthy

	status := http.StatusOK
	label := "healthy"
	if !healthy {
		status, label = http.StatusServiceUnavailable, "degraded"
	}
	writeJSON(w, status, healthBody{
		Status: label, HasData: hasData, IsStale: isStale,
		JobsHealthy: jobs.Healthy, FailedJobs: failed,
		LastUpdate:    balance.LastUpdate,
		UptimeSeconds: int(s.State.UptimeSeconds()),
		Version:       s.Settings.AppVersion,
	})
}

func (s *Server) isStale(lastUpdate *string) bool {
	if lastUpdate == nil {
		return false
	}
	updatedAt, err := time.Parse("2006-01-02T15:04:05.999999Z", *lastUpdate)
	if err != nil {
		return false
	}
	limit := time.Duration(s.Settings.RefreshInterval()*stalenessMultiplier) * time.Second
	return time.Since(updatedAt) > limit
}

type featuresBody struct {
	Status   string         `json:"status"`
	Features featureToggles `json:"features"`
}

type featureToggles struct {
	Subscriptions bool `json:"subscriptions"`
	DynamicConfig bool `json:"dynamic_config"`
	History       bool `json:"history"`
	EmailScan     bool `json:"email_scan"`
}

// Implementation note.
func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	cfg := s.Resolver.Load(r.Context())
	writeJSON(w, http.StatusOK, featuresBody{
		Status: "success",
		Features: featureToggles{
			Subscriptions: s.Settings.EnableSubscriptions,
			DynamicConfig: s.Settings.EnableDynamicConfig,
			History:       s.Settings.EnableHistoryAPI,
			EmailScan:     len(s.Settings.EmailScanTimes) > 0 && len(cfg.EnabledMailboxes()) > 0,
		},
	})
}

func (s *Server) handleCredits(w http.ResponseWriter, r *http.Request) {
	balance := s.State.Balance()
	if len(balance.Projects) == 0 {
		fail(w, http.StatusServiceUnavailable, "Balance data is not initialized; please try again")
		return
	}
	etagJSON(w, r, balance)
}

func (s *Server) handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	etagJSON(w, r, s.State.Subscriptions())
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	etagJSON(w, r, s.State.Jobs())
}

type refreshRequest struct {
	ProjectName *string `json:"project_name"`
}

// Implementation note.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	projectName := ""
	if r.Method == http.MethodPost && r.ContentLength > 0 {
		var body refreshRequest
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.ProjectName != nil {
			projectName = trimSpace(*body.ProjectName)
		}
	}

	if busy := s.refreshGuard.acquire(); busy != "" {
		fail(w, http.StatusTooManyRequests, "Refresh "+busy)
		return
	}
	defer s.refreshGuard.release()

	started := time.Now()
	// Implementation note.
	dryRun := !s.Settings.EnableWebAlarm

	outcome, err := s.Monitor.Run(r.Context(), projectName, dryRun)
	if err != nil {
		fail(w, http.StatusInternalServerError, "Refresh failed: "+err.Error())
		return
	}

	if projectName != "" {
		s.State.MergeBalance(outcome.Results)
	} else {
		s.State.SetBalance(outcome.Results)
	}
	if s.OnBalanceUpdated != nil {
		s.OnBalanceUpdated(s.State.Balance().Projects)
	}

	message := "Refresh complete"
	if projectName != "" {
		message += " (project: " + projectName + ")"
	}
	ok(w, map[string]any{
		"message":                message,
		"refreshed_count":        len(outcome.Results),
		"execution_time_seconds": round2(time.Since(started).Seconds()),
		"dry_run":                dryRun,
	})
}

// Implementation note.
func round2(value float64) float64 {
	return math.RoundToEven(value*100) / 100
}

package httpapi

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/itswl/quotapulse/internal/model"
)

// usageIngestRequest is what a local reporter posts: one batch of aggregated
// day/model cells. Re-reporting a cell overwrites it, so reporters can run on
// any schedule they like.
type usageIngestRequest struct {
	Source  string             `json:"source"`
	Project string             `json:"project"`
	Entries []model.UsageEntry `json:"entries"`
}

var (
	usageSourcePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	usageDatePattern    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	usageProjectPattern = regexp.MustCompile(`^[^|]{1,120}$`)
)

// handleUsageIngest accepts reported usage from local tools (the Claude Code
// reporter, cron scripts). Each entry keys on source+project+date+model, so
// the same report posted twice is a refresh, not a duplicate.
func (s *Server) handleUsageIngest(w http.ResponseWriter, r *http.Request) {
	if s.Store == nil || !s.Store.Enabled() {
		fail(w, http.StatusServiceUnavailable, "Usage ingestion needs ENABLE_DATABASE=true")
		return
	}
	var body usageIngestRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Source = strings.ToLower(strings.TrimSpace(body.Source))
	if !usageSourcePattern.MatchString(body.Source) {
		failValidation(w, []string{"source: lowercase letters, digits, dash or underscore, 2-32 characters"})
		return
	}
	body.Project = strings.TrimSpace(body.Project)
	if body.Project == "" {
		body.Project = body.Source
	}
	if !usageProjectPattern.MatchString(body.Project) {
		failValidation(w, []string{"project: 1-120 characters, no pipes"})
		return
	}

	var problems []string
	entries := make([]model.UsageEntry, 0, len(body.Entries))
	for i, entry := range body.Entries {
		entry.Source = body.Source
		entry.Project = body.Project
		entry.Date = strings.TrimSpace(entry.Date)
		entry.Model = strings.TrimSpace(entry.Model)
		entry.Currency = strings.ToUpper(strings.TrimSpace(entry.Currency))
		if entry.Model == "" {
			entry.Model = "all"
		}
		if entry.Currency == "" {
			entry.Currency = "USD"
		}
		switch {
		case !usageDatePattern.MatchString(entry.Date):
			problems = append(problems, "entries: dates look like 2026-10-15")
		case entry.Cost < 0:
			problems = append(problems, "entries: cost cannot be negative")
		}
		_ = i
		entries = append(entries, entry)
		if len(problems) > 0 {
			break
		}
	}
	if len(entries) == 0 {
		failValidation(w, []string{"entries: at least one is required"})
		return
	}
	if len(problems) > 0 {
		failValidation(w, problems)
		return
	}

	for _, entry := range entries {
		if err := s.Store.UpsertUsageEvent(r.Context(), entry); err != nil {
			s.log().Error("Failed to store a usage event", "project", entry.Project, "error", err)
			fail(w, http.StatusInternalServerError, "Failed to store the report")
			return
		}
	}
	ok(w, map[string]any{"message": "Report accepted", "accepted": len(entries)})
}

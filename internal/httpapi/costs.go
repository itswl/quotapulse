package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/itswl/quotapulse/internal/cost"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/store"
)

// handleCostsSummary renders the unified monthly cost view: per-project
// month-to-date spend in native and base currency, budget pace, and totals.
// It needs balance history; without a database it 503s like the history API.
func (s *Server) handleCostsSummary(w http.ResponseWriter, r *http.Request) {
	if s.Store == nil || !s.Store.Enabled() {
		fail(w, http.StatusServiceUnavailable, "Cost summary needs ENABLE_DATABASE=true balance history")
		return
	}
	now := time.Now()
	if month := trimSpace(r.URL.Query().Get("month")); month != "" {
		parsed, err := time.ParseInLocation("2006-01", month, time.Local)
		if err != nil {
			fail(w, http.StatusBadRequest, "month must look like 2026-10")
			return
		}
		if !parsed.After(now) {
			now = parsed.AddDate(0, 1, 0).Add(-time.Second)
		}
	}
	_, daysInMonth := cost.MonthWindow(now)

	points, err := s.Store.BalanceSeries(r.Context(), daysInMonth+1)
	if err != nil {
		fail(w, http.StatusInternalServerError, "Failed to read balance history")
		return
	}
	cfg := s.Resolver.Load(r.Context())

	fx := s.Settings.FXTable()
	base := s.Settings.BaseCurrencyValue()
	summary := cost.Build(points, cfg.Projects, projectCurrency, fx, base, now)
	mergeReportedUsage(&summary, s.Store, r, fx, now)
	etagJSON(w, r, map[string]any{"status": "success", "summary": summary})
}

// mergeReportedUsage folds local-tool reports (the Claude Code reporter)
// into the summary as their own entries. A project name that already carries
// balance-derived spend keeps that number: reports on the same name would
// double-count it.
func mergeReportedUsage(summary *cost.Summary, storage interface {
	ListUsageSpend(ctx context.Context, sinceDate string) ([]store.UsageSpend, error)
}, r *http.Request, fx map[string]float64, now time.Time) {
	since := now.Format("2006-01-02")
	spend, err := storage.ListUsageSpend(r.Context(), since[:8]+"01")
	if err != nil {
		return
	}
	known := map[string]bool{}
	for _, p := range summary.Projects {
		known[p.Project] = true
	}
	_, daysInMonth := cost.MonthWindow(now)
	for _, entry := range spend {
		if known[entry.Project] || entry.Cost == 0 {
			continue
		}
		sp := cost.ProjectSpend{
			Project: entry.Project, Provider: "usage:" + entry.Source,
			Currency: entry.Currency, Spend: entry.Cost,
			Projected: cost.Projected(entry.Cost, now.Day(), daysInMonth),
		}
		if rate, ok := fx[entry.Currency]; ok {
			sp.SpendBase = round2f(entry.Cost * rate)
			summary.TotalBase += sp.SpendBase
		} else if entry.Currency != "" {
			summary.UnknownFX = append(summary.UnknownFX, entry.Currency)
		}
		summary.Projects = append(summary.Projects, sp)
	}
	summary.TotalBase = round2f(summary.TotalBase)
}

func round2f(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// projectCurrency resolves a project's native currency: an explicit override
// wins, then the provider's default; quota percents carry none.
func projectCurrency(p model.Project) string {
	if p.Type == model.TypeQuota {
		return ""
	}
	if p.Currency != "" {
		return p.Currency
	}
	if info, ok := provider.Lookup(p.Provider); ok {
		return info.Currency
	}
	return ""
}

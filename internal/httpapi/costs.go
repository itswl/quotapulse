package httpapi

import (
	"net/http"
	"time"

	"github.com/itswl/quotapulse/internal/cost"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/provider"
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
	etagJSON(w, r, map[string]any{"status": "success", "summary": summary})
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

package httpapi

import (
	"net/http"

	"github.com/itswl/quotapulse/internal/store"
)

// handleEvents merges alert history and email alerts into one queryable timeline,
// newest first. Read-model only: both feeds already exist in the store.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	days, err := intParam(r, "days", 30, 1, 365)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, err := intParam(r, "limit", 100, 1, 500)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	eventType := r.URL.Query().Get("type")

	alerts, err := s.Store.RecentAlerts(r.Context(), store.AlertQuery{Days: days, Limit: 500})
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	emails, err := s.Store.EmailAlerts(r.Context(), store.EmailAlertQuery{Days: days, Limit: 500})
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	events := store.MergeEvents(alerts, emails, limit)
	if eventType != "" {
		filtered := make([]store.EventRow, 0, len(events))
		for _, event := range events {
			if event.Type == eventType {
				filtered = append(filtered, event)
			}
		}
		events = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "count": len(events), "data": events})
}

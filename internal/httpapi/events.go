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

	// The type decides which feeds to read: alert types live only in
	// alert_history, "email_alert" only in email_alert_history. Filtering has to
	// happen in the query — after the merge the requested limit is already spent
	// on the newer feed, and older matches of the asked-for type disappear.
	// Each feed is read up to the limit itself: the merged top-limit cannot
	// contain more than limit rows of either feed.
	var alerts []store.AlertRow
	var emails []store.EmailAlertRow
	if eventType != "email_alert" {
		alerts, err = s.Store.RecentAlerts(r.Context(), store.AlertQuery{AlertType: eventType, Days: days, Limit: limit})
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if eventType == "" || eventType == "email_alert" {
		emails, err = s.Store.EmailAlerts(r.Context(), store.EmailAlertQuery{Days: days, Limit: limit})
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	events := store.MergeEvents(alerts, emails, limit)
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "count": len(events), "data": events})
}

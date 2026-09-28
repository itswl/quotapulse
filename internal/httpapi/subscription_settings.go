package httpapi

import (
	"net/http"
	"net/url"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

// handleSubscriptionSnooze postpones renewal reminders for one subscription without
// marking it renewed. Empty days resets the snooze.
func (s *Server) handleSubscriptionSnooze(w http.ResponseWriter, r *http.Request) {
	if !s.requireSubscriptions(w) || !s.requireDynamicConfig(w, "snooze subscription") {
		return
	}
	var body struct {
		Name string `json:"name"`
		Days *int   `json:"days,omitempty"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Name == "" {
		fail(w, http.StatusBadRequest, "snooze requires a subscription name")
		return
	}
	days := 7
	if body.Days != nil {
		days = *body.Days
	}
	if days < 1 || days > 365 {
		fail(w, http.StatusBadRequest, "days must be between 1 and 365")
		return
	}
	until := time.Now().AddDate(0, 0, days).Format("2006-01-02")
	if err := s.Store.SetSubscriptionSnooze(r.Context(), body.Name, until); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.refreshSubscriptions(r)
	ok(w, map[string]any{"status": "success", "message": "Reminders snoozed until " + until})
}

// handleSubscriptionTimezone evaluates one subscription in its own timezone.
func (s *Server) handleSubscriptionTimezone(w http.ResponseWriter, r *http.Request) {
	if !s.requireSubscriptions(w) || !s.requireDynamicConfig(w, "set subscription timezone") {
		return
	}
	var body struct {
		Name     string `json:"name"`
		Timezone string `json:"timezone"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Name == "" {
		fail(w, http.StatusBadRequest, "timezone requires a subscription name")
		return
	}
	if body.Timezone != "" {
		if _, err := time.LoadLocation(body.Timezone); err != nil {
			fail(w, http.StatusBadRequest, "unknown timezone: "+body.Timezone)
			return
		}
	}
	if err := s.Store.UpdateSubscriptionTimezone(r.Context(), body.Name, body.Timezone); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.refreshSubscriptions(r)
	ok(w, map[string]any{"status": "success"})
}

// handleSubscriptionWebhook points one subscription at its own webhook destination.
func (s *Server) handleSubscriptionWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.requireSubscriptions(w) || !s.requireDynamicConfig(w, "set subscription webhook") {
		return
	}
	var body struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Name == "" {
		fail(w, http.StatusBadRequest, "webhook requires a subscription name")
		return
	}
	if body.URL != "" {
		if len(body.URL) > 2000 {
			fail(w, http.StatusBadRequest, "webhook URL is too long")
			return
		}
		if parsed, parseErr := url.Parse(body.URL); parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			fail(w, http.StatusBadRequest, "webhook URL must be an http(s) URL")
			return
		}
	}
	if err := s.Store.UpdateSubscriptionWebhook(r.Context(), body.Name, body.URL); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.refreshSubscriptions(r)
	ok(w, map[string]any{"status": "success"})
}

// handleListEmailSuppressions returns the mailbox+sender pairs muted as false positives.
func (s *Server) handleListEmailSuppressions(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListEmailSuppressions(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []model.EmailSuppression{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "count": len(list), "data": list})
}

// handleAddEmailSuppression mutes a mailbox+sender pair.
func (s *Server) handleAddEmailSuppression(w http.ResponseWriter, r *http.Request) {
	var body model.EmailSuppression
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Mailbox == "" || body.Sender == "" {
		fail(w, http.StatusBadRequest, "mailbox and sender are required")
		return
	}
	if err := s.Store.AddEmailSuppression(r.Context(), body.Mailbox, body.Sender); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

// handleDeleteEmailSuppression removes a suppression so the sender notifies again.
func (s *Server) handleDeleteEmailSuppression(w http.ResponseWriter, r *http.Request) {
	var body model.EmailSuppression
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Mailbox == "" || body.Sender == "" {
		fail(w, http.StatusBadRequest, "mailbox and sender are required")
		return
	}
	if err := s.Store.DeleteEmailSuppression(r.Context(), body.Mailbox, body.Sender); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

package httpapi

import (
	"net/http"

	"github.com/itswl/quotapulse/internal/notify"
)

// handleNotifyTest sends a canary message through the real webhook so the operator can
// verify the channel without waiting for a genuine alert.
func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	notifier := s.notify()
	if notifier == nil {
		fail(w, http.StatusBadRequest, "Webhook URL is not configured; set WEBHOOK_URL first")
		return
	}
	msg := notify.Custom("QuotaPulse test notification", []string{
		"This is a test alert sent from the dashboard.",
		"If you can read this, the webhook channel works.",
	}, notify.KindTest)
	if err := notifier.Send(r.Context(), msg); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	ok(w, map[string]any{"status": "success", "message": "Test notification sent"})
}

// notify returns the shared webhook sender; the monitor and the subscription checker
// are wired to the same instance.
func (s *Server) notify() notify.Notifier {
	if s.Subs != nil && s.Subs.Notifier != nil {
		return s.Subs.Notifier
	}
	if s.Monitor != nil {
		return s.Monitor.Notifier
	}
	return nil
}

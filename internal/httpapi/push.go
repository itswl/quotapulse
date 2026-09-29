package httpapi

import (
	"net/http"

	"github.com/itswl/quotapulse/internal/model"
)

// handlePushConfig returns the VAPID public key browsers subscribe with.
func (s *Server) handlePushConfig(w http.ResponseWriter, r *http.Request) {
	if s.Push == nil {
		fail(w, http.StatusServiceUnavailable, "push is not available")
		return
	}
	publicKey, err := s.Push.PublicKey(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "publicKey": publicKey})
}

// handlePushSubscribe registers or refreshes one browser's push subscription.
func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.Push == nil {
		fail(w, http.StatusServiceUnavailable, "push is not available")
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Endpoint == "" || body.Keys.P256dh == "" || body.Keys.Auth == "" {
		fail(w, http.StatusBadRequest, "endpoint and keys are required")
		return
	}
	sub := model.PushSubscription{
		Endpoint: body.Endpoint, P256dh: body.Keys.P256dh, Auth: body.Keys.Auth,
	}
	if err := s.Push.Subscribe(r.Context(), sub); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Send a confirmation push immediately so the operator learns the channel works —
	// or gets the real delivery error — instead of waiting for a genuine alert.
	if err := s.Push.SendTo(r.Context(), sub, "QuotaPulse push enabled", "Browser push is live. You will receive alerts here."); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	ok(w, map[string]any{"status": "success"})
}

// handlePushUnsubscribe removes one browser's push registration.
func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if s.Push == nil {
		fail(w, http.StatusServiceUnavailable, "push is not available")
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Endpoint == "" {
		fail(w, http.StatusBadRequest, "endpoint is required")
		return
	}
	if err := s.Push.Unsubscribe(r.Context(), body.Endpoint); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"status": "success"})
}

// handlePushTest delivers a test notification to every registered browser and reports
// the per-browser outcome, so delivery problems surface immediately instead of at the
// first real alert.
func (s *Server) handlePushTest(w http.ResponseWriter, r *http.Request) {
	if s.Push == nil {
		fail(w, http.StatusServiceUnavailable, "push is not available")
		return
	}
	subs, err := s.Store.ListPushSubscriptions(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(subs) == 0 {
		fail(w, http.StatusBadRequest, "no browser is subscribed yet; enable browser push first")
		return
	}
	sent, failed := 0, 0
	var failures []string
	for _, sub := range subs {
		if err := s.Push.SendTo(r.Context(), sub, "QuotaPulse test notification", "If you can read this, browser push works."); err != nil {
			failed++
			failures = append(failures, shortEndpointOf(sub.Endpoint)+": "+err.Error())
			continue
		}
		sent++
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success", "sent": sent, "failed": failed, "failures": failures,
	})
}

func shortEndpointOf(endpoint string) string {
	if len(endpoint) > 48 {
		return endpoint[:48] + "…"
	}
	return endpoint
}

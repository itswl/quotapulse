package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/subscription"
)

// Implementation note.
func (s *Server) requireSubscriptions(w http.ResponseWriter) bool {
	if s.Settings.EnableSubscriptions {
		return true
	}
	fail(w, http.StatusServiceUnavailable, "operationDisabled,operation ENABLE_SUBSCRIPTIONS=true")
	return false
}

func (s *Server) handleListSubscriptions(w http.ResponseWriter, r *http.Request) {
	if !s.requireSubscriptions(w) {
		return
	}
	cfg := s.Resolver.Load(r.Context())
	subs := cfg.Subscriptions
	if subs == nil {
		subs = []model.Subscription{}
	}
	etagJSON(w, r, map[string]any{"status": "success", "subscriptions": subs})
}

// Implementation note.
// Implementation note.
type subscriptionRequest struct {
	Name            string          `json:"name"`
	NewName         *string         `json:"new_name"`
	OwnerProject    *string         `json:"owner_project"`
	CycleType       *string         `json:"cycle_type"`
	RenewalDay      json.RawMessage `json:"renewal_day"`
	AlertDaysBefore *int            `json:"alert_days_before"`
	Amount          *float64        `json:"amount"`
	Enabled         *bool           `json:"enabled"`
	LastRenewedDate *string         `json:"last_renewed_date"`
}

var validCycles = map[string]bool{
	model.CycleWeekly: true, model.CycleMonthly: true, model.CycleYearly: true,
	model.CycleLunarYearly: true,
}

func (s *Server) handleAddSubscription(w http.ResponseWriter, r *http.Request) {
	if !s.requireSubscriptions(w) || !s.requireDynamicConfig(w, "operation") {
		return
	}
	var body subscriptionRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Name = trimSpace(body.Name)
	if body.Name == "" {
		failValidation(w, []string{"name: cannot be empty"})
		return
	}

	cfg := s.Resolver.Load(r.Context())
	if findSubscription(cfg.Subscriptions, body.Name) != nil {
		fail(w, http.StatusBadRequest, "operation ["+body.Name+"] operation")
		return
	}

	sub := model.Subscription{
		Name: body.Name, CycleType: model.CycleMonthly,
		RenewalDay: 1, AlertDaysBefore: 3, Enabled: true,
	}
	if problems := applySubscriptionPatch(&sub, body); len(problems) > 0 {
		failValidation(w, problems)
		return
	}

	if err := s.Store.UpsertSubscription(r.Context(), sub); err != nil {
		s.log().Error("operation", "subscription", body.Name, "error", err)
		fail(w, storeWriteStatus(err), "operation")
		return
	}
	s.log().Info("[AUDIT] operation", "subscription", body.Name, "cycle", sub.CycleType, "amount", sub.Amount)
	s.refreshSubscriptions(r)
	ok(w, map[string]any{"message": "operation [" + body.Name + "] operation"})
}

// Implementation note.
func (s *Server) handleUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	if !s.requireSubscriptions(w) || !s.requireDynamicConfig(w, "operation") {
		return
	}
	var body subscriptionRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	cfg := s.Resolver.Load(r.Context())
	current := findSubscription(cfg.Subscriptions, body.Name)
	if current == nil {
		fail(w, http.StatusNotFound, "operation: "+body.Name)
		return
	}

	updated := *current
	if problems := applySubscriptionPatch(&updated, body); len(problems) > 0 {
		failValidation(w, problems)
		return
	}
	if body.NewName != nil && trimSpace(*body.NewName) != "" {
		updated.Name = trimSpace(*body.NewName)
		if err := s.Store.DeleteSubscription(r.Context(), body.Name); err != nil {
			s.log().Warn("operation", "subscription", body.Name, "error", err)
		}
	}

	if err := s.Store.UpsertSubscription(r.Context(), updated); err != nil {
		s.log().Error("operation", "subscription", body.Name, "error", err)
		fail(w, storeWriteStatus(err), "operation")
		return
	}
	s.log().Info("[AUDIT] operation", "subscription", body.Name)
	s.refreshSubscriptions(r)
	ok(w, map[string]any{"message": "operation [" + body.Name + "] operation"})
}

// Implementation note.
func applySubscriptionPatch(target *model.Subscription, body subscriptionRequest) []string {
	var problems []string

	if body.CycleType != nil {
		if !validCycles[*body.CycleType] {
			problems = append(problems, "cycle_type: operation weekly / monthly / yearly")
		} else {
			target.CycleType = *body.CycleType
		}
	}
	if len(body.RenewalDay) > 0 && string(body.RenewalDay) != "null" {
		day, ok := parseRenewalDay(body.RenewalDay, target.CycleType)
		if !ok {
			problems = append(problems, `renewal_day: use MMDD for yearly/lunar_yearly, 1-31 for monthly, or 1-7 for weekly`)
		} else {
			target.RenewalDay = day
		}
	}
	if body.AlertDaysBefore != nil {
		if *body.AlertDaysBefore < 0 {
			problems = append(problems, "alert_days_before: cannot be negative")
		} else {
			target.AlertDaysBefore = *body.AlertDaysBefore
		}
	}
	if body.Amount != nil {
		target.Amount = *body.Amount
	}
	if body.Enabled != nil {
		target.Enabled = *body.Enabled
	}
	if body.OwnerProject != nil {
		target.OwnerProject = model.OwnerProjectOf(*body.OwnerProject)
	}
	if body.LastRenewedDate != nil {
		if *body.LastRenewedDate == "" {
			target.LastRenewedDate = nil
		} else if _, err := time.Parse("2006-01-02", *body.LastRenewedDate); err != nil {
			problems = append(problems, "last_renewed_date: operation YYYY-MM-DD")
		} else {
			target.LastRenewedDate = body.LastRenewedDate
		}
	}
	return problems
}

// Implementation note.
func parseRenewalDay(raw json.RawMessage, cycleType string) (int, bool) {
	var asNumber int
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		return asNumber, true
	}
	var asText string
	if err := json.Unmarshal(raw, &asText); err != nil {
		return 0, false
	}
	return subscription.CoerceRenewalDay(asText, cycleType)
}

func (s *Server) handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	if !s.requireSubscriptions(w) || !s.requireDynamicConfig(w, "operation") {
		return
	}
	var body nameRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	cfg := s.Resolver.Load(r.Context())
	if findSubscription(cfg.Subscriptions, body.Name) == nil {
		fail(w, http.StatusNotFound, "operation: "+body.Name)
		return
	}
	if err := s.Store.DeleteSubscription(r.Context(), body.Name); err != nil {
		fail(w, storeWriteStatus(err), "operation")
		return
	}
	s.log().Info("[AUDIT] operation", "subscription", body.Name)
	s.refreshSubscriptions(r)
	ok(w, map[string]any{"message": "operation [" + body.Name + "] operation"})
}

type renewedRequest struct {
	Name        string  `json:"name"`
	RenewedDate *string `json:"renewed_date"`
}

// Implementation note.
func (s *Server) handleMarkRenewed(w http.ResponseWriter, r *http.Request) {
	if !s.requireSubscriptions(w) || !s.requireDynamicConfig(w, "operation") {
		return
	}
	var body renewedRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Name == "" {
		fail(w, http.StatusBadRequest, "operation: name")
		return
	}

	renewedDate := time.Now().Format("2006-01-02")
	if body.RenewedDate != nil && *body.RenewedDate != "" {
		if _, err := time.Parse("2006-01-02", *body.RenewedDate); err != nil {
			fail(w, http.StatusBadRequest, "renewed_date operation YYYY-MM-DD")
			return
		}
		renewedDate = *body.RenewedDate
	}

	cfg := s.Resolver.Load(r.Context())
	current := findSubscription(cfg.Subscriptions, body.Name)
	if current == nil {
		fail(w, http.StatusNotFound, "operation")
		return
	}
	updated := *current
	updated.LastRenewedDate = &renewedDate
	if err := s.Store.UpsertSubscription(r.Context(), updated); err != nil {
		fail(w, storeWriteStatus(err), "operation")
		return
	}
	s.log().Info("[AUDIT] operation", "subscription", body.Name, "date", renewedDate)
	s.refreshSubscriptions(r)

	renewedAt, _ := time.ParseInLocation("2006-01-02", renewedDate, time.Local)
	next, err := subscription.NextRenewalFrom(updated.CycleType, updated.RenewalDay, renewedAt)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, map[string]any{
		"message":           "operation [" + body.Name + "] operation",
		"next_renewal_date": next.Format("2006-01-02T15:04:05"),
	})
}

func (s *Server) handleClearRenewed(w http.ResponseWriter, r *http.Request) {
	if !s.requireSubscriptions(w) || !s.requireDynamicConfig(w, "operation") {
		return
	}
	var body nameRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	cfg := s.Resolver.Load(r.Context())
	current := findSubscription(cfg.Subscriptions, body.Name)
	if current == nil {
		fail(w, http.StatusNotFound, "operation")
		return
	}
	updated := *current
	updated.LastRenewedDate = nil
	if err := s.Store.UpsertSubscription(r.Context(), updated); err != nil {
		fail(w, storeWriteStatus(err), "operation")
		return
	}
	s.log().Info("[AUDIT] operation", "subscription", body.Name)
	s.refreshSubscriptions(r)
	ok(w, map[string]any{"message": "operation [" + body.Name + "] operation"})
}

// Implementation note.
func (s *Server) refreshSubscriptions(r *http.Request) {
	cfg := s.Resolver.Load(r.Context())
	results := s.Subs.Check(r.Context(), cfg.EnabledSubscriptions(), !s.Settings.EnableWebAlarm)
	s.State.SetSubscriptions(results)
	if s.OnSubscriptionUpdated != nil {
		s.OnSubscriptionUpdated(results)
	}
}

func findSubscription(subs []model.Subscription, name string) *model.Subscription {
	for i := range subs {
		if subs[i].Name == name {
			return &subs[i]
		}
	}
	return nil
}

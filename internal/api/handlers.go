package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"quotapulse/internal/bundle"
	"quotapulse/internal/core"
	"quotapulse/internal/engine"
	"quotapulse/internal/notify"
	"quotapulse/internal/provider"
	"quotapulse/internal/recur"
	"quotapulse/internal/store"
)

// list wraps a slice, so every list endpoint answers {"items": [...]} and
// can grow fields — a cursor, a total — without breaking anyone.
func list[T any](items []T) map[string]any {
	if items == nil {
		items = []T{}
	}
	return map[string]any{"items": items}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": s.Version})
}

// handleReady says whether QuotaPulse is doing its job, not merely running:
// the database answers and the background loops are turning.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if _, err := s.Engine.Store().Stats(r.Context()); err != nil {
		fail(w, http.StatusServiceUnavailable, "database", "the database is not answering")
		return
	}
	if s.Runner != nil {
		if ok, why := s.Runner.Healthy(time.Now()); !ok {
			fail(w, http.StatusServiceUnavailable, "stalled", why)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "version": s.Version})
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := s.Engine.Overview(r.Context(), language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

// ---------- accounts ----------

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.Engine.Accounts(r.Context(), language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(accounts))
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	account, err := s.Engine.AccountView(r.Context(), r.PathValue("id"), language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var in engine.AccountInput
	if !decode(w, r, &in) {
		return
	}
	account, err := s.Engine.CreateAccount(r.Context(), in, language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, account)
}

func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	var in engine.AccountInput
	if !decode(w, r, &in) {
		return
	}
	account, err := s.Engine.UpdateAccount(r.Context(), r.PathValue("id"), in, language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if err := s.Engine.DeleteAccount(r.Context(), r.PathValue("id")); err != nil {
		s.failWith(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestAccount(w http.ResponseWriter, r *http.Request) {
	var in engine.TestInput
	if !decode(w, r, &in) {
		return
	}
	result, err := s.Engine.TestAccount(r.Context(), in)
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleSample asks for a fresh sample of one account, or of all of them.
// It answers at once; the result arrives on the event stream.
func (s *Server) handleSample(w http.ResponseWriter, r *http.Request) {
	if err := s.Engine.RequestSample(r.Context(), r.PathValue("id")); err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"requested": true})
}

// ---------- meters ----------

func (s *Server) handleMeters(w http.ResponseWriter, r *http.Request) {
	meters, err := s.Engine.Meters(r.Context(), language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(meters))
}

func (s *Server) handleMeter(w http.ResponseWriter, r *http.Request) {
	meter, err := s.Engine.MeterView(r.Context(), r.PathValue("id"), language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, meter)
}

func (s *Server) handleUpdateMeter(w http.ResponseWriter, r *http.Request) {
	var in engine.MeterInput
	if !decode(w, r, &in) {
		return
	}
	meter, err := s.Engine.UpdateMeter(r.Context(), r.PathValue("id"), in, language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, meter)
}

func (s *Server) handleDeleteMeter(w http.ResponseWriter, r *http.Request) {
	if err := s.Engine.DeleteMeter(r.Context(), r.PathValue("id")); err != nil {
		s.failWith(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	rangeName := r.URL.Query().Get("range")
	if rangeName == "" {
		rangeName = "30d"
	}
	series, err := s.Engine.Series(r.Context(), r.PathValue("id"), rangeName, language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, series)
}

func (s *Server) handleMute(w http.ResponseWriter, r *http.Request) {
	var until *time.Time
	if r.Method == http.MethodPut {
		var body struct {
			Until *time.Time `json:"until"`
		}
		if !decode(w, r, &body) {
			return
		}
		if body.Until == nil {
			fail(w, http.StatusBadRequest, "invalid", "until is required")
			return
		}
		until = body.Until
	}
	meter, err := s.Engine.MuteMeter(r.Context(), r.PathValue("id"), until, language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, meter)
}

// handleIngest takes readings pushed from outside: one object, or
// {"readings": [...]} for several.
func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		engine.Push
		Readings []engine.Push `json:"readings"`
	}
	if !decode(w, r, &body) {
		return
	}
	pushes := body.Readings
	if len(pushes) == 0 && body.Meter != "" {
		pushes = []engine.Push{body.Push}
	}
	meters, err := s.Engine.Ingest(r.Context(), pushes)
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accepted": len(pushes), "meters": meters})
}

// ---------- reminders ----------

func (s *Server) handleReminders(w http.ResponseWriter, r *http.Request) {
	reminders, err := s.Engine.Reminders(r.Context(), language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(reminders))
}

func (s *Server) handleReminder(w http.ResponseWriter, r *http.Request) {
	reminder, err := s.Engine.ReminderView(r.Context(), r.PathValue("id"), language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reminder)
}

func (s *Server) handleCreateReminder(w http.ResponseWriter, r *http.Request) {
	var in engine.ReminderInput
	if !decode(w, r, &in) {
		return
	}
	reminder, err := s.Engine.CreateReminder(r.Context(), in, language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, reminder)
}

func (s *Server) handleUpdateReminder(w http.ResponseWriter, r *http.Request) {
	var in engine.ReminderInput
	if !decode(w, r, &in) {
		return
	}
	reminder, err := s.Engine.UpdateReminder(r.Context(), r.PathValue("id"), in, language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reminder)
}

func (s *Server) handleDeleteReminder(w http.ResponseWriter, r *http.Request) {
	if err := s.Engine.DeleteReminder(r.Context(), r.PathValue("id")); err != nil {
		s.failWith(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePreviewReminder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rule recur.Rule `json:"rule"`
	}
	if !decode(w, r, &body) {
		return
	}
	preview, err := s.Engine.PreviewRule(r.Context(), body.Rule, language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleReminderDone(w http.ResponseWriter, r *http.Request) {
	change := s.Engine.CompleteReminder
	if r.Method == http.MethodDelete {
		change = s.Engine.ReopenReminder
	}
	reminder, err := change(r.Context(), r.PathValue("id"), language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reminder)
}

func (s *Server) handleReminderSnooze(w http.ResponseWriter, r *http.Request) {
	var until *recur.Date
	if r.Method == http.MethodPut {
		var body struct {
			Until *recur.Date `json:"until"`
		}
		if !decode(w, r, &body) {
			return
		}
		if body.Until == nil || body.Until.IsZero() {
			fail(w, http.StatusBadRequest, "invalid", "until is required, as YYYY-MM-DD")
			return
		}
		until = body.Until
	}
	reminder, err := s.Engine.SnoozeReminder(r.Context(), r.PathValue("id"), until, language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reminder)
}

// ---------- incidents, deliveries, spend ----------

func queryLimit(r *http.Request) int {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return limit
}

func (s *Server) handleIncidents(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := store.IncidentFilter{
		State: query.Get("state"), SubjectType: query.Get("subject_type"), SubjectID: query.Get("subject_id"),
		Limit: queryLimit(r),
	}
	if filter.State != "" && filter.State != core.StateOpen && filter.State != core.StateResolved {
		fail(w, http.StatusBadRequest, "invalid", "state must be open or resolved")
		return
	}
	if before := query.Get("before"); before != "" {
		at, err := time.Parse(time.RFC3339, before)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid", "before must be an RFC 3339 time")
			return
		}
		filter.Before = at
	}
	incidents, err := s.Engine.Incidents(r.Context(), filter, language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(incidents))
}

func (s *Server) handleAck(w http.ResponseWriter, r *http.Request) {
	incident, err := s.Engine.Ack(r.Context(), r.PathValue("id"), r.Method == http.MethodPost)
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	// Answer with the incident as every list shows it, story included.
	view, err := s.Engine.IncidentView(r.Context(), incident, language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleDeliveries(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := store.DeliveryFilter{IncidentID: query.Get("incident"), ChannelID: query.Get("channel"), Limit: queryLimit(r)}
	if before := query.Get("before"); before != "" {
		id, err := strconv.ParseInt(before, 10, 64)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid", "before must be a delivery id")
			return
		}
		filter.BeforeID = id
	}
	deliveries, err := s.Engine.Deliveries(r.Context(), filter)
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(deliveries))
}

func (s *Server) handleRetryDelivery(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusNotFound, "not_found", "there is no such thing")
		return
	}
	delivery, err := s.Engine.RetryDelivery(r.Context(), id)
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, delivery)
}

func (s *Server) handleSpend(w http.ResponseWriter, r *http.Request) {
	spend, err := s.Engine.Spend(r.Context(), r.URL.Query().Get("month"), language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, spend)
}

// ---------- providers ----------

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	definitions, err := s.Engine.Definitions(r.Context())
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(definitions))
}

func (s *Server) handleSaveProvider(w http.ResponseWriter, r *http.Request) {
	var def provider.Definition
	if !decode(w, r, &def) {
		return
	}
	def.ID = r.PathValue("id")
	saved, err := s.Engine.SaveProvider(r.Context(), def)
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.Engine.DeleteProvider(r.Context(), r.PathValue("id")); err != nil {
		s.failWith(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- channels ----------

func (s *Server) handleChannelTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, list(notify.Types()))
}

func (s *Server) handleChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.Engine.Channels(r.Context(), language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(channels))
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	var in engine.ChannelInput
	if !decode(w, r, &in) {
		return
	}
	channel, err := s.Engine.CreateChannel(r.Context(), in, language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, channel)
}

func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	var in engine.ChannelInput
	if !decode(w, r, &in) {
		return
	}
	channel, err := s.Engine.UpdateChannel(r.Context(), r.PathValue("id"), in, language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, channel)
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	if err := s.Engine.DeleteChannel(r.Context(), r.PathValue("id")); err != nil {
		s.failWith(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTestChannel sends a test message and answers with what the platform
// said. A refusal is the answer, not a server error: 200 with ok false.
func (s *Server) handleTestChannel(w http.ResponseWriter, r *http.Request) {
	err := s.Engine.TestChannel(r.Context(), r.PathValue("id"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case engine.IsInvalid(err):
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
	default:
		s.failWith(w, r, err)
	}
}

// handlePushKey returns what a browser needs in order to subscribe.
func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	key, err := s.Engine.PushKey(r.Context())
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"public_key": key})
}

// handlePushSubscribe turns a browser's push subscription into a channel.
func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string                  `json:"name"`
		Subscription engine.PushSubscription `json:"subscription"`
	}
	if !decode(w, r, &body) {
		return
	}
	channel, err := s.Engine.SubscribePush(r.Context(), body.Name, body.Subscription, language(r))
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, channel)
}

// ---------- tokens ----------

func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.Engine.Tokens(r.Context())
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(tokens))
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var in engine.TokenInput
	if !decode(w, r, &in) {
		return
	}
	token, err := s.Engine.CreateToken(r.Context(), in)
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, token)
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	if err := s.Engine.DeleteToken(r.Context(), r.PathValue("id")); err != nil {
		s.failWith(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- settings and system ----------

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.Engine.Settings(r.Context())
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// handleUpdateSettings applies a partial update: the body is laid over the
// current settings, so a form can send only what it changed.
func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.Engine.Settings(r.Context())
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	// FX is replaced as a whole when sent: laying a map over a map could
	// never remove a rate.
	var probe struct {
		FX map[string]float64 `json:"fx"`
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	if json.Unmarshal(raw, &probe) == nil && probe.FX != nil {
		settings.FX = nil
	}
	if !decodeBytes(w, raw, &settings) {
		return
	}
	saved, err := s.Engine.UpdateSettings(r.Context(), settings)
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleDigest(w http.ResponseWriter, r *http.Request) {
	if err := s.Engine.SendDigest(r.Context()); err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true})
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	stats, err := s.Engine.Store().Stats(r.Context())
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	out := map[string]any{
		"version": s.Version, "demo": s.Demo, "muted": s.Engine.Muted(),
		"now": s.Engine.Now().UTC(), "stats": stats, "jobs": []any{},
	}
	if s.Runner != nil {
		out["jobs"] = s.Runner.Jobs()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if s.Runner == nil {
		fail(w, http.StatusBadRequest, "invalid", "backups are not available here")
		return
	}
	path, err := s.Runner.Backup(r.Context())
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path})
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	exported, err := bundle.Export(r.Context(), s.Engine, bundle.ExportOptions{
		Secrets: query.Get("secrets") == "1", History: query.Get("history") != "0",
		Source: "quotapulse " + s.Version,
	})
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	name := "quotapulse-" + s.Engine.Now().UTC().Format("20060102") + ".json"
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	writeJSON(w, http.StatusOK, exported)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	raw, ok := readBodyLimited(w, r, maxImportBody)
	if !ok {
		return
	}
	parsed, err := bundle.Parse(raw)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	report, err := bundle.Import(r.Context(), s.Engine, parsed, bundle.ImportOptions{Settings: r.URL.Query().Get("settings") == "1"})
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

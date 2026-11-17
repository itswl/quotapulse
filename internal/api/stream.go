package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/core"
)

// readBody reads a request body within the usual size limit.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	return readBodyLimited(w, r, maxBody)
}

func readBodyLimited(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "too_large", "the request body is too large")
		return nil, false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	return raw, true
}

func decodeBytes(w http.ResponseWriter, raw []byte, v any) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "invalid", "the request body is not valid: "+jsonProblem(err))
		return false
	}
	return true
}

// heartbeat keeps proxies from closing a quiet stream.
const heartbeat = 25 * time.Second

// handleStream is a server-sent event stream of "something changed". It
// carries only topic names; the dashboard refetches what it is showing. That
// keeps the stream trivially correct: missing an event costs nothing.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, "internal", "streaming is not supported here")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // ask buffering proxies to pass events through
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 5000\n\n")
	flusher.Flush()

	changes, stop := s.Engine.Changes().Subscribe()
	defer stop()
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case topic := <-changes:
			// Several changes usually arrive together — a sample touches
			// meters and incidents. Send each topic once.
			topics := map[string]bool{topic: true}
			settle := time.After(150 * time.Millisecond)
		drain:
			for {
				select {
				case more := <-changes:
					topics[more] = true
				case <-settle:
					break drain
				case <-r.Context().Done():
					return
				}
			}
			names := make([]string, 0, len(topics))
			for name := range topics {
				names = append(names, name)
			}
			sort.Strings(names)
			fmt.Fprintf(w, "event: change\ndata: %s\n\n", strings.Join(names, ","))
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}

// ---------- Prometheus ----------

// handleMetrics exposes the current state in Prometheus' text format, for
// anyone who already has dashboards and alert rules elsewhere. It is written
// by hand: a handful of gauges does not need a client library.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	overview, err := s.Engine.Overview(r.Context(), "en")
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	var b strings.Builder
	metric := func(name, help string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
	}
	sample := func(name string, labels map[string]string, value float64) {
		b.WriteString(name)
		if len(labels) > 0 {
			keys := make([]string, 0, len(labels))
			for key := range labels {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			b.WriteByte('{')
			for i, key := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				fmt.Fprintf(&b, `%s="%s"`, key, escapeLabel(labels[key]))
			}
			b.WriteByte('}')
		}
		b.WriteByte(' ')
		b.WriteString(strconv.FormatFloat(value, 'g', -1, 64))
		b.WriteByte('\n')
	}
	labelsOf := func(display string, kind core.Kind, unit string) map[string]string {
		return map[string]string{"meter": display, "kind": string(kind), "unit": unit}
	}

	metric("quotapulse_build_info", "Always 1; the version is in the label.")
	sample("quotapulse_build_info", map[string]string{"version": s.Version}, 1)

	metric("quotapulse_meter_value", "Current value of a meter, in its own unit.")
	for _, m := range overview.Meters {
		if m.Value != nil {
			sample("quotapulse_meter_value", labelsOf(m.Display, m.Kind, m.Unit), *m.Value)
		}
	}
	metric("quotapulse_meter_runway_days", "Days a balance lasts at its expected burn. Absent when nothing is being spent.")
	for _, m := range overview.Meters {
		if m.Forecast != nil && m.Forecast.RunwayDays != nil {
			sample("quotapulse_meter_runway_days", labelsOf(m.Display, m.Kind, m.Unit), *m.Forecast.RunwayDays)
		}
	}
	metric("quotapulse_meter_burn_per_day", "Expected spend per day, in the meter's unit.")
	for _, m := range overview.Meters {
		if m.Forecast != nil && m.Forecast.Confidence != "none" {
			sample("quotapulse_meter_burn_per_day", labelsOf(m.Display, m.Kind, m.Unit), m.Forecast.Burn)
		}
	}
	metric("quotapulse_meter_remaining_ratio", "Share of a quota that is left, from 0 to 1.")
	for _, m := range overview.Meters {
		if m.Quota != nil {
			sample("quotapulse_meter_remaining_ratio", labelsOf(m.Display, m.Kind, m.Unit), m.Quota.RemainingPct/100)
		}
	}
	metric("quotapulse_meter_sampled_timestamp_seconds", "When a meter was last read.")
	for _, m := range overview.Meters {
		if m.SampledAt != nil {
			sample("quotapulse_meter_sampled_timestamp_seconds", labelsOf(m.Display, m.Kind, m.Unit), float64(m.SampledAt.Unix()))
		}
	}
	metric("quotapulse_account_up", "1 when an account's last sample succeeded, 0 when it failed.")
	for _, a := range overview.Accounts {
		if !a.Enabled || a.Status == core.StatusPending {
			continue
		}
		up := 0.0
		if a.Status == core.StatusOK {
			up = 1
		}
		sample("quotapulse_account_up", map[string]string{"account": a.Name, "provider": a.Provider}, up)
	}
	metric("quotapulse_incidents_open", "Open incidents by severity.")
	open := map[core.Severity]int{core.Info: 0, core.Warning: 0, core.Critical: 0}
	for _, inc := range overview.Attention {
		open[inc.Severity]++
	}
	for _, severity := range []core.Severity{core.Info, core.Warning, core.Critical} {
		sample("quotapulse_incidents_open", map[string]string{"severity": string(severity)}, float64(open[severity]))
	}
	metric("quotapulse_spend_month", "Metered spend this month, in the base currency.")
	sample("quotapulse_spend_month", map[string]string{"currency": overview.Spend.Base}, overview.Spend.Total)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, b.String())
}

func escapeLabel(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(value)
}

// ---------- the dashboard's files ----------

// assets serves the built dashboard. It is a single-page app: any path that
// is not a file gets index.html, and the page works out what to show.
func (s *Server) assets() http.Handler {
	if s.Assets == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "The dashboard was not built into this binary. Run `npm --prefix web run build`, then build again.", http.StatusNotFound)
		})
	}
	files := http.FileServerFS(s.Assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if info, err := fs.Stat(s.Assets, name); err != nil || info.IsDir() {
			name = "index.html"
		}
		switch {
		case name == "index.html" || name == "sw.js" || strings.HasSuffix(name, ".webmanifest"):
			// These name the hashed files; a stale copy pins an old build.
			w.Header().Set("Cache-Control", "no-cache")
		case strings.HasPrefix(name, "assets/"):
			// Built files carry a content hash in their name.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		default:
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		r = r.Clone(r.Context())
		r.URL.Path = "/" + name
		if name == "index.html" {
			// FileServer redirects /index.html to /; serve the bytes directly.
			raw, err := fs.ReadFile(s.Assets, "index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(raw)
			return
		}
		files.ServeHTTP(w, r)
	})
}

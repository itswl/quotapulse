package metrics

import (
	"net/http"
	"strconv"
	"strings"
)

// HTTP labels. route is Go's ServeMux pattern minus the method ("/api/credits"),
// so one series per registered route instead of one per URL — path parameters
// such as trend IDs or project names never reach the label.
var (
	httpRequestLabels  = []string{"route", "method", "status"}
	httpDurationLabels = []string{"route", "method"}
)

// Requests that never matched a pattern (404s, probes hit by scanners) collapse
// into one series; their raw paths would be unbounded.
const routeUnmatched = "unmatched"

// Middleware records one count and one latency observation per request. Wire it
// around the router: Go's ServeMux fills r.Pattern while serving, so the route
// is only known after next returns.
func (c *Collector) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := c.now()
		recorder := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK // handler wrote nothing, net/http sends 200
		}
		route := routeOf(r)
		c.httpRequests.WithLabelValues(route, r.Method, strconv.Itoa(status)).Inc()
		c.httpDuration.WithLabelValues(route, r.Method).Observe(c.now().Sub(start).Seconds())
	})
}

// routeOf turns "GET /api/credits" into "/api/credits": the method already has
// its own label, and repeating it would double every series.
func routeOf(r *http.Request) string {
	pattern := r.Pattern
	if pattern == "" {
		return routeUnmatched
	}
	if i := strings.IndexByte(pattern, ' '); i >= 0 {
		pattern = pattern[i+1:]
	}
	return pattern
}

// statusWriter captures what the handler sent. Unwrap keeps
// http.ResponseController working (flushing, hijacking) through the wrapper.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

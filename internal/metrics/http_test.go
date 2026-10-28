package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHTTPMiddlewareRecordsRouteNotPath pins the cardinality contract: the route
// label comes from the ServeMux pattern, so project names and trend IDs never
// become series.
func TestHTTPMiddlewareRecordsRouteNotPath(t *testing.T) {
	c, reg := newTestCollector(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/credits", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{}"))
	})
	mux.HandleFunc("GET /api/history/trend/{key}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("POST /api/refresh", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	handler := c.Middleware(mux)

	got := func(method, path string) int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec.Code
	}

	got("GET", "/api/credits")
	got("GET", "/api/credits")
	got("GET", "/api/history/trend/deepseek%3Adeepseek")
	got("POST", "/api/refresh")
	got("GET", "/nope/missing") // unmatched: one series, whatever the path

	text := dump(t, reg)
	mustContain(t, text,
		`quotapulse_http_requests_total{method="GET",route="/api/credits",status="200"} 2`,
		`quotapulse_http_requests_total{method="GET",route="/api/history/trend/{key}",status="404"} 1`,
		`quotapulse_http_requests_total{method="POST",route="/api/refresh",status="429"} 1`,
		`quotapulse_http_requests_total{method="GET",route="unmatched",status="404"} 1`,
	)
	// The concrete project key must never leak into a label.
	mustNotContain(t, text, "deepseek")
	// Every request also lands in the latency histogram.
	mustContain(t, text,
		`quotapulse_http_request_duration_seconds_count{method="GET",route="/api/credits"} 2`,
	)
}

// TestHTTPMiddlewareDefaultsTo200 covers handlers that write a body without an
// explicit WriteHeader: net/http would answer 200, so the metric must too.
func TestHTTPMiddlewareDefaultsTo200(t *testing.T) {
	c, reg := newTestCollector(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	rec := httptest.NewRecorder()
	c.Middleware(mux).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	mustContain(t, dump(t, reg), `quotapulse_http_requests_total{method="GET",route="/",status="200"} 1`)
}

// TestHTTPMiddlewareObservesLatency uses a stepped clock so the histogram gets a
// real value instead of the fixed clock's zero.
func TestHTTPMiddlewareObservesLatency(t *testing.T) {
	c, reg := newTestCollector(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	calls := 0
	c.now = func() time.Time {
		calls++
		if calls == 1 {
			return testNow
		}
		return testNow.Add(120 * time.Millisecond)
	}

	c.Middleware(mux).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/slow", nil))

	text := dump(t, reg)
	mustContain(t, text,
		`quotapulse_http_request_duration_seconds_bucket{method="GET",route="/slow",le="0.1"} 0`,
		`quotapulse_http_request_duration_seconds_bucket{method="GET",route="/slow",le="0.25"} 1`,
	)
	if strings.Contains(text, `quotapulse_http_requests_total{method="GET",route="unmatched"`) {
		t.Error("matched route must not also count as unmatched")
	}
}

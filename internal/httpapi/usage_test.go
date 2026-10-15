package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// A report batch lands, a re-report overwrites the same cells, and the cost
// summary carries the reported projects as their own entries.
func TestUsageIngestRoundTrip(t *testing.T) {
	_, handler, st := newServerWithDB(t, nil)

	post := func(body string) *responseRecorderWrap {
		return wrapRequest(t, handler, "POST", "/api/usage/ingest", body)
	}

	// validation: bad source and empty entries are rejected field by field
	got := post(`{"source":"Bad Source","entries":[{"date":"2026-10-15"}]}`)
	if got.code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", got.code, got.body)
	}

	got = post(`{"source":"claude-code","entries":[]}`)
	if got.code != http.StatusBadRequest || !contains(got.body, "at least one") {
		t.Fatalf("empty batch should be rejected: %d %s", got.code, got.body)
	}

	// a valid batch
	got = post(`{"source":"claude-code","project":"claude-code","entries":[
		{"date":"2026-10-15","model":"claude-sonnet-4-5","cost":3.5,"currency":"USD","input_tokens":1000,"output_tokens":2000,"requests":10},
		{"date":"2026-10-15","model":"claude-opus-4-6","cost":1.25,"input_tokens":500,"output_tokens":100,"requests":2}
	]}`)
	if got.code != http.StatusOK {
		t.Fatalf("ingest failed: %d %s", got.code, got.body)
	}

	// re-reporting the same cell overwrites instead of accumulating
	got = post(`{"source":"claude-code","entries":[
		{"date":"2026-10-15","model":"claude-sonnet-4-5","cost":4.5,"currency":"USD","requests":12}
	]}`)
	if got.code != http.StatusOK {
		t.Fatalf("refresh failed: %d %s", got.code, got.body)
	}

	spend, err := st.ListUsageSpend(context.Background(), time.Now().Format("2006-01")+"-01")
	if err != nil {
		t.Fatal(err)
	}
	byProject := map[string]float64{}
	for _, entry := range spend {
		byProject[entry.Project] += entry.Cost
	}
	if byProject["claude-code"] != 5.75 { // 4.5 (overwritten) + 1.25
		t.Errorf("reported spend = %v, want 5.75", byProject)
	}

	// the costs summary lists the reported project with its own entry
	got2 := wrapRequest(t, handler, "GET", "/api/costs/summary", "")
	if got2.code != http.StatusOK {
		t.Fatalf("costs failed: %d %s", got2.code, got2.body)
	}
	if !contains(got2.body, "claude-code") || !contains(got2.body, "usage:claude-code") {
		t.Errorf("summary should carry the reported project: %s", got2.body)
	}
	// default project falls back to the source name
	got = post(`{"source":"cron-job","entries":[{"date":"2026-10-15","model":"all","cost":0,"currency":"CNY"}]}`)
	if got.code != http.StatusOK {
		t.Fatalf("zero-cost entry rejected: %d %s", got.code, got.body)
	}
}

// ---------- tiny helpers ----------

type responseRecorderWrap struct {
	code int
	body string
}

func wrapRequest(t *testing.T, handler http.Handler, method, path, body string) *responseRecorderWrap {
	t.Helper()
	rec := request(t, handler, method, path, body, true)
	return &responseRecorderWrap{code: rec.Code, body: rec.Body.String()}
}

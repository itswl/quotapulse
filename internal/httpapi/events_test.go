package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/state"
	"github.com/itswl/quotapulse/internal/store"
)

// TestEventsTypeFilterLooksPastNewerFeed pins the fix for filtering after the
// merge: five email alerts newer than every balance alert must not hide balance
// events from ?type=low_balance — the filter has to run in the query, not on
// the already-trimmed merge result.
func TestEventsTypeFilterLooksPastNewerFeed(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	settings := &config.Settings{WebAPIKey: testAPIKey, AppVersion: "1", EnableHistoryAPI: true}
	st, storeErr := store.Open(context.Background(), store.Options{DatabaseURL: "sqlite://" + filepath.Join(t.TempDir(), "events.db")})
	if storeErr != nil {
		t.Fatalf("open test store: %v", storeErr)
	}
	defer st.Close()
	ctx := context.Background()

	for i := range 3 {
		if err := st.SaveAlert(ctx, store.AlertRecord{
			AlertID: "p1", Name: "project-one", AlertType: "low_balance",
			Message: "balance dropped", Status: "sent",
		}); err != nil {
			t.Fatalf("seed alert %d: %v", i, err)
		}
	}
	if err := st.SaveAlert(ctx, store.AlertRecord{
		AlertID: "p2", Name: "project-two", AlertType: "low_runway",
		Message: "runway short", Status: "sent",
	}); err != nil {
		t.Fatalf("seed runway alert: %v", err)
	}
	// A sleep keeps the email rows strictly newer than every alert, which is the
	// ordering the old post-merge filter got wrong.
	time.Sleep(20 * time.Millisecond)
	for i := range 5 {
		if err := st.SaveEmailAlert(ctx, store.EmailAlertRecord{
			Mailbox: "ops@example.com", Sender: "billing@loud.com",
			Subject: "invoice " + string(rune('A'+i)), Date: "2026-10-31",
			Keywords: []string{"invoice"}, AlertSent: true,
		}); err != nil {
			t.Fatalf("seed email alert %d: %v", i, err)
		}
	}

	s := &Server{Settings: settings, Resolver: config.NewResolver(settings, st, log), Store: st, State: state.New(), Log: log}
	handler := s.Handler()
	get := func(path string) eventsBody {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
		}
		var body eventsBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if body.Count != len(body.Data) {
			t.Fatalf("%s: count %d but %d rows", path, body.Count, len(body.Data))
		}
		return body
	}

	balanced := get("/api/events?type=low_balance&limit=3")
	if balanced.Count != 3 {
		t.Fatalf("type=low_balance 应返回 3 条，得到 %d — 5 条更新的邮件告警把限额度吃掉了", balanced.Count)
	}
	for _, event := range balanced.Data {
		if event.Type != "low_balance" {
			t.Fatalf("type 过滤后混入了 %q", event.Type)
		}
	}

	emails := get("/api/events?type=email_alert&limit=2")
	if emails.Count != 2 {
		t.Fatalf("type=email_alert&limit=2 应返回 2 条，得到 %d", emails.Count)
	}
	for _, event := range emails.Data {
		if event.Type != "email_alert" {
			t.Fatalf("email 过滤后混入了 %q", event.Type)
		}
	}

	runway := get("/api/events?type=low_runway")
	if runway.Count != 1 {
		t.Fatalf("type=low_runway 应返回 1 条，得到 %d", runway.Count)
	}

	if none := get("/api/events?type=does_not_exist"); none.Count != 0 {
		t.Fatalf("未知 type 应返回 0 条，得到 %d", none.Count)
	}

	// Unfiltered still merges both feeds, newest first, trimmed to the limit.
	merged := get("/api/events?limit=4")
	if merged.Count != 4 {
		t.Fatalf("无过滤 limit=4 应返回 4 条，得到 %d", merged.Count)
	}
	for _, event := range merged.Data {
		if event.Type != "email_alert" && event.Type != "low_balance" && event.Type != "low_runway" {
			t.Fatalf("合并结果混入未知类型 %q", event.Type)
		}
	}
	if !strings.Contains(strings.Join(eventTypes(merged.Data), ","), "email_alert") {
		t.Fatal("合并结果应包含邮件告警")
	}
}

type eventsBody struct {
	Count int              `json:"count"`
	Data  []store.EventRow `json:"data"`
}

func eventTypes(events []store.EventRow) []string {
	out := make([]string, len(events))
	for i, event := range events {
		out[i] = event.Type
	}
	return out
}

package mcp_test

import (
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/core"
)

func TestWhatTheReadingToolsTellAnAgent(t *testing.T) {
	w := newWorld(t)
	session := w.connect(core.ScopeRead, core.ScopeAct, core.ScopeIngest)

	// Several readings at once. A quota says how long its window is, and a
	// reading may say when it was taken.
	batch, result := call[struct {
		Accepted int
		Meters   []meterOut
		Timezone string
	}](t, session, "record_readings", map[string]any{"readings": []map[string]any{
		{"meter": "deepseek", "name": "DeepSeek", "kind": "balance", "unit": "CNY", "value": 547.67},
		{"meter": "plan", "name": "GLM window", "kind": "quota", "unit": "%", "value": 6, "limit": 100, "window_seconds": 21600},
		{"meter": "gone", "name": "Empty wallet", "kind": "balance", "unit": "USD", "value": 0},
	}})
	if result.IsError || batch.Accepted != 3 || len(batch.Meters) != 3 || batch.Timezone != "Asia/Shanghai" {
		t.Fatalf("record_readings: %+v %s", batch, errorText(result))
	}
	type oneMeter struct {
		Meter struct {
			Key           string
			WindowSeconds *int64     `json:"window_seconds"`
			SampledAt     *time.Time `json:"sampled_at"`
		} `json:"meter"`
	}
	quota, result := call[oneMeter](t, session, "get_meter", map[string]any{"meter": "GLM window"})
	if result.IsError || quota.Meter.Key != "plan" || quota.Meter.WindowSeconds == nil || *quota.Meter.WindowSeconds != 21600 {
		t.Fatalf("the quota's window: %+v %s", quota, errorText(result))
	}
	taken := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	late, result := call[oneMeter](t, session, "record_reading", map[string]any{"meter": "disk", "kind": "counter", "unit": "GB", "value": 12, "at": taken.Format(time.RFC3339)})
	if result.IsError || late.Meter.SampledAt == nil || !late.Meter.SampledAt.Equal(taken) {
		t.Fatalf("a backdated reading: %+v %s", late, errorText(result))
	}
	for name, args := range map[string]map[string]any{
		"a time that is not a time": {"meter": "disk", "value": 13, "at": "yesterday"},
		"a time in the future":      {"meter": "disk", "value": 13, "at": "2027-01-01T00:00:00Z"},
	} {
		if _, result := call[oneMeter](t, session, "record_reading", args); !result.IsError {
			t.Errorf("record_reading with %s succeeded", name)
		}
	}
	if _, result := call[struct{}](t, session, "record_readings", map[string]any{"readings": []map[string]any{}}); !result.IsError {
		t.Error("recorded no readings at all")
	}

	// The overview says whether notifications are muted, and how much is
	// configured.
	overview, result := call[struct {
		NotificationsMuted bool `json:"notifications_muted"`
		Counts             struct{ Accounts, Meters, Dates, Channels int }
	}](t, session, "attention", nil)
	if result.IsError || overview.NotificationsMuted || overview.Counts.Meters != 4 || overview.Counts.Accounts != 0 {
		t.Fatalf("attention: %+v %s", overview, errorText(result))
	}

	// Spend, day by day, with the months before.
	spend, result := call[struct {
		IsCurrent   bool `json:"is_current"`
		DaysInMonth int  `json:"days_in_month"`
		Daily       []struct{ Date string }
		History     []struct{ Month string }
	}](t, session, "get_spend", nil)
	if result.IsError || !spend.IsCurrent || spend.DaysInMonth != 30 || spend.Daily == nil || spend.History == nil {
		t.Fatalf("get_spend: %+v %s", spend, errorText(result))
	}

	// Incidents: about one thing, a page at a time.
	type activity struct {
		Incidents []struct {
			ID           string
			SubjectID    string `json:"subject_id"`
			SubjectType  string `json:"subject_type"`
			Acknowledged bool
		}
	}
	all, result := call[activity](t, session, "list_activity", nil)
	if result.IsError || len(all.Incidents) == 0 {
		t.Fatalf("list_activity: %+v %s", all, errorText(result))
	}
	if aboutMeters, result := call[activity](t, session, "list_activity", map[string]any{"subject_type": "meter"}); result.IsError || len(aboutMeters.Incidents) != len(all.Incidents) {
		t.Errorf("about meters: %+v %s", aboutMeters, errorText(result))
	}
	if page, result := call[activity](t, session, "list_activity", map[string]any{"limit": 1}); result.IsError || len(page.Incidents) != 1 {
		t.Errorf("a page of one: %+v %s", page, errorText(result))
	}
	if resolved, result := call[activity](t, session, "list_activity", map[string]any{"state": "resolved"}); result.IsError || len(resolved.Incidents) != 0 {
		t.Errorf("resolved incidents: %+v %s", resolved, errorText(result))
	}
	for name, args := range map[string]map[string]any{
		"a kind of thing that does not exist": {"subject_type": "planet"},
		"a before that is not a time":         {"before": "yesterday"},
	} {
		if _, result := call[activity](t, session, "list_activity", args); !result.IsError {
			t.Errorf("list_activity with %s succeeded", name)
		}
	}

	// Acknowledge, then take it back.
	first := all.Incidents[0]
	if acked, result := call[struct{ Acknowledged bool }](t, session, "acknowledge", map[string]any{"incident_id": first.ID}); result.IsError || !acked.Acknowledged {
		t.Fatalf("acknowledge: %s", errorText(result))
	}
	if shown, _ := call[activity](t, session, "list_activity", map[string]any{"subject_id": first.SubjectID}); len(shown.Incidents) != 1 || shown.Incidents[0].ID != first.ID || !shown.Incidents[0].Acknowledged {
		t.Fatalf("the acknowledgement does not show: %+v", shown.Incidents)
	}
	if taken, result := call[struct{ Acknowledged bool }](t, session, "unacknowledge", map[string]any{"incident_id": first.ID}); result.IsError || taken.Acknowledged {
		t.Fatalf("unacknowledge: %+v %s", taken, errorText(result))
	}
	if shown, _ := call[activity](t, session, "list_activity", map[string]any{"subject_id": first.SubjectID}); shown.Incidents[0].Acknowledged {
		t.Error("it is still acknowledged")
	}
	if _, result := call[struct{}](t, session, "unacknowledge", map[string]any{"incident_id": "inc_missing"}); !result.IsError {
		t.Error("unacknowledged nothing")
	}
}

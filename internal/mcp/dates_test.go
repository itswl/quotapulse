package mcp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/engine"
)

type dateView struct {
	ID, Title, Kind, Next, State, Rule, Lunar string
	Days                                      int
	LeadDays                                  []int `json:"lead_days"`
	Schedule                                  struct {
		Freq, Calendar string
		Month, Day     int
	} `json:"schedule"`
	Enabled   bool
	Following []string
	HeldUntil string `json:"notifications_held_until"`
}

type dateEnvelope struct {
	Date dateView `json:"date"`
}

func TestAnAgentPreviewsAndChangesDates(t *testing.T) {
	w := newWorld(t)
	session := w.connect(core.ScopeRead, core.ScopeAct)

	// Where a rule lands, before anything is saved.
	preview, result := call[dateEnvelope](t, session, "preview_date", map[string]any{
		"freq": "yearly", "calendar": "lunar", "month": 12, "day": 30, "lead_days": []int{0, 3},
	})
	if result.IsError {
		t.Fatalf("preview_date: %s", errorText(result))
	}
	if p := preview.Date; p.Title != "(preview)" || p.Kind != "custom" || p.Next != "2027-02-05" || p.Lunar != "腊月廿九" ||
		p.Days != 81 || p.State != "upcoming" || len(p.Following) != 3 || p.Following[0] != "2028-01-25" {
		t.Fatalf("preview = %+v", p)
	}
	if leads := preview.Date.LeadDays; len(leads) != 2 || leads[0] != 3 || leads[1] != 0 {
		t.Errorf("lead days are not soonest last: %v", leads)
	}
	if _, result := call[dateEnvelope](t, session, "preview_date", map[string]any{"freq": "monthly", "day": 40}); !result.IsError || !strings.Contains(errorText(result), "day must be between 1 and 31") {
		t.Errorf("an impossible rule: %s", errorText(result))
	}

	// Add one, then change it piece by piece.
	added, result := call[dateEnvelope](t, session, "add_date", map[string]any{
		"title": "Domain", "kind": "renewal", "freq": "yearly", "month": 12, "day": 1, "amount": 10,
	})
	if result.IsError || added.Date.Next != "2026-12-01" {
		t.Fatalf("add_date: %+v %s", added, errorText(result))
	}
	id := added.Date.ID

	renamed, result := call[dateEnvelope](t, session, "update_date", map[string]any{"date": "domain", "title": "Domain name", "lead_days": []int{7, 0}})
	if result.IsError || renamed.Date.Title != "Domain name" || renamed.Date.Next != "2026-12-01" || renamed.Date.Kind != "renewal" {
		t.Fatalf("update_date: %+v %s", renamed, errorText(result))
	}
	recurred, result := call[dateEnvelope](t, session, "update_date", map[string]any{"date": id, "rule": map[string]any{"freq": "monthly", "day": 20}})
	if result.IsError || recurred.Date.Next != "2026-11-20" || recurred.Date.Rule != "every month on day 20" || recurred.Date.State != "soon" {
		t.Fatalf("a new rule: %+v %s", recurred, errorText(result))
	}

	// The list gives the rule as fields and the reminder days, so an agent
	// can hand them straight back.
	listed, result := call[struct{ Dates []dateView }](t, session, "list_dates", map[string]any{"query": "domain name"})
	if result.IsError || len(listed.Dates) != 1 {
		t.Fatalf("list_dates: %+v %s", listed, errorText(result))
	}
	if d := listed.Dates[0]; d.Schedule.Freq != "monthly" || d.Schedule.Day != 20 || len(d.LeadDays) != 2 || !d.Enabled {
		t.Errorf("listed = %+v", d)
	}

	// Held back, then woken.
	snoozed, result := call[dateEnvelope](t, session, "snooze_date", map[string]any{"date": "Domain name", "until": "2026-12-31"})
	if result.IsError || snoozed.Date.State != "snoozed" || snoozed.Date.HeldUntil != "2026-12-31" || snoozed.Date.Next != "2026-11-20" {
		t.Fatalf("snooze_date: %+v %s", snoozed, errorText(result))
	}
	woken, result := call[dateEnvelope](t, session, "unsnooze_date", map[string]any{"date": id})
	if result.IsError || woken.Date.State != "soon" || woken.Date.HeldUntil != "" {
		t.Fatalf("unsnooze_date: %+v %s", woken, errorText(result))
	}

	// Switched off: out of the default list, still there when asked.
	off, result := call[dateEnvelope](t, session, "update_date", map[string]any{"date": id, "enabled": false})
	if result.IsError || off.Date.Enabled || off.Date.State != "disabled" {
		t.Fatalf("disabled: %+v %s", off, errorText(result))
	}
	if hidden, _ := call[struct{ Dates []dateView }](t, session, "list_dates", map[string]any{"query": "domain name"}); len(hidden.Dates) != 0 {
		t.Errorf("a disabled date is still listed: %+v", hidden.Dates)
	}
	if every, _ := call[struct{ Dates []dateView }](t, session, "list_dates", map[string]any{"query": "domain name", "include_inactive": true}); len(every.Dates) != 1 {
		t.Errorf("include_inactive does not list it: %+v", every.Dates)
	}
	if on, result := call[dateEnvelope](t, session, "update_date", map[string]any{"date": id, "enabled": true}); result.IsError || on.Date.State != "soon" {
		t.Fatalf("enabled again: %+v %s", on, errorText(result))
	}

	// Dealt with, then taken back.
	done, result := call[dateEnvelope](t, session, "complete_date", map[string]any{"date": id})
	if result.IsError || done.Date.Next != "2026-12-20" {
		t.Fatalf("complete_date: %+v %s", done, errorText(result))
	}
	reopened, result := call[dateEnvelope](t, session, "reopen_date", map[string]any{"date": id})
	if result.IsError || reopened.Date.Next != "2026-11-20" {
		t.Fatalf("reopen_date: %+v %s", reopened, errorText(result))
	}

	// What does not work says why.
	for name, args := range map[string]map[string]any{
		"nothing to change": {"date": id},
		"no such date":      {"date": "no such date"},
	} {
		if _, result := call[dateEnvelope](t, session, "update_date", args); !result.IsError {
			t.Errorf("update_date with %s succeeded", name)
		}
	}
	if _, result := call[dateEnvelope](t, session, "snooze_date", map[string]any{"date": id, "until": "2020-01-01"}); !result.IsError {
		t.Error("snoozed until a day in the past")
	}
	if _, result := call[dateEnvelope](t, session, "snooze_date", map[string]any{"date": id, "until": "December"}); !result.IsError {
		t.Error("snoozed until a word")
	}
	if _, result := call[dateEnvelope](t, session, "preview_date", map[string]any{"freq": "weekly", "weekday": 9}); !result.IsError {
		t.Error("previewed a weekday that does not exist")
	}

	// Gone for good.
	deleted, result := call[struct{ Deleted bool }](t, session, "delete_date", map[string]any{"date": id})
	if result.IsError || !deleted.Deleted {
		t.Fatalf("delete_date: %s", errorText(result))
	}
	if left, _ := call[struct{ Dates []dateView }](t, session, "list_dates", map[string]any{"include_inactive": true}); len(left.Dates) != 0 {
		t.Errorf("still listed: %+v", left.Dates)
	}
	if _, result := call[struct{ Deleted bool }](t, session, "delete_date", map[string]any{"date": id}); !result.IsError {
		t.Error("deleted it twice")
	}
	// A read-only token can look, not touch.
	if !refused(t, w.connect(core.ScopeRead), "update_date", map[string]any{"date": "x", "title": "y"}) {
		t.Error("a read token changed a date")
	}
}

type meterEdit struct {
	ID         string     `json:"id"`
	Muted      bool       `json:"muted"`
	MutedUntil *time.Time `json:"muted_until"`
	Name       string
	Key        string
	Unit       string
	Hidden     bool
}

func TestAnAgentSilencesAndEditsAMeter(t *testing.T) {
	w := newWorld(t)
	_, err := w.engine.Ingest(ctx, []engine.Push{{Meter: "wallet", Name: "Wallet", Kind: core.Balance, Unit: "USD", Value: ptr(100.0)}})
	if err != nil {
		t.Fatal(err)
	}
	type envelope struct {
		Meter meterEdit `json:"meter"`
		Rules struct {
			Low    *float64 `json:"low"`
			Budget *float64 `json:"budget"`
		} `json:"rules"`
		Daily []struct{ Date string }
	}
	session := w.connect(core.ScopeRead, core.ScopeAct, core.ScopeAdmin)

	muted, result := call[envelope](t, session, "mute_meter", map[string]any{"meter": "wallet", "until": "2026-12-01T00:00:00Z"})
	if result.IsError || !muted.Meter.Muted || muted.Meter.MutedUntil == nil {
		t.Fatalf("mute_meter: %+v %s", muted, errorText(result))
	}
	if seen, _ := call[envelope](t, session, "get_meter", map[string]any{"meter": "wallet"}); !seen.Meter.Muted {
		t.Errorf("get_meter does not show the mute: %+v", seen.Meter)
	}
	if _, result := call[envelope](t, session, "mute_meter", map[string]any{"meter": "wallet", "until": "2020-01-01T00:00:00Z"}); !result.IsError {
		t.Error("muted until a moment in the past")
	}
	if _, result := call[envelope](t, session, "mute_meter", map[string]any{"meter": "wallet", "until": "soon"}); !result.IsError {
		t.Error("muted until a word")
	}
	if woken, result := call[envelope](t, session, "unmute_meter", map[string]any{"meter": "wallet"}); result.IsError || woken.Meter.Muted || woken.Meter.MutedUntil != nil {
		t.Fatalf("unmute_meter: %+v %s", woken, errorText(result))
	}

	edited, result := call[envelope](t, session, "update_meter", map[string]any{
		"meter": "wallet", "name": "My wallet", "unit": "EUR", "rules": map[string]any{"low": 50, "budget": 0},
	})
	if result.IsError || edited.Meter.Name != "My wallet" || edited.Meter.Unit != "EUR" || edited.Meter.Key != "wallet" {
		t.Fatalf("update_meter: %+v %s", edited, errorText(result))
	}
	if edited.Rules.Low == nil || *edited.Rules.Low != 50 || edited.Rules.Budget != nil {
		t.Errorf("rules = %+v", edited.Rules)
	}
	if listed, _ := call[struct{ Meters []meterOut }](t, session, "list_meters", map[string]any{"query": "my wallet"}); len(listed.Meters) != 1 {
		t.Errorf("the new name is not listed: %+v", listed.Meters)
	}

	// Hidden: out of the lists and not judged, still there by id.
	id := edited.Meter.ID
	hidden, result := call[envelope](t, session, "update_meter", map[string]any{"meter": "My wallet", "hidden": true})
	if result.IsError || !hidden.Meter.Hidden {
		t.Fatalf("hidden: %+v %s", hidden, errorText(result))
	}
	if listed, _ := call[struct{ Meters []meterOut }](t, session, "list_meters", nil); len(listed.Meters) != 0 {
		t.Errorf("a hidden meter is still listed: %+v", listed.Meters)
	}
	if shown, result := call[envelope](t, session, "get_meter", map[string]any{"meter": id, "range": "7d"}); result.IsError || !shown.Meter.Hidden || len(shown.Daily) > 7 {
		t.Errorf("get_meter of a hidden meter: %+v %s", shown, errorText(result))
	}
	if _, result := call[envelope](t, session, "get_meter", map[string]any{"meter": id, "range": "2w"}); !result.IsError || !strings.Contains(errorText(result), "range must be") {
		t.Errorf("an unknown range: %s", errorText(result))
	}
	if _, result := call[envelope](t, session, "update_meter", map[string]any{"meter": id}); !result.IsError || !strings.Contains(errorText(result), "nothing to change") {
		t.Errorf("update_meter with nothing to change: %s", errorText(result))
	}

	deleted, result := call[struct{ Deleted bool }](t, session, "delete_meter", map[string]any{"meter": "My wallet"})
	if result.IsError || !deleted.Deleted {
		t.Fatalf("delete_meter: %s", errorText(result))
	}
	if _, result := call[envelope](t, session, "get_meter", map[string]any{"meter": id}); !result.IsError {
		t.Error("the deleted meter is still there")
	}
	// Changing a meter is for an admin token; silencing one is not.
	acting := w.connect(core.ScopeAct)
	if !refused(t, acting, "update_meter", map[string]any{"meter": "x", "hidden": true}) {
		t.Error("an act token changed a meter")
	}
	if !refused(t, acting, "delete_meter", map[string]any{"meter": "x"}) {
		t.Error("an act token deleted a meter")
	}
}

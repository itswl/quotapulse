package mcp

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/engine"
	"github.com/itswl/quotapulse/internal/recur"
)

// Everything here is about dates, which the engine calls reminders: the
// rest of a date's tools — add_date and complete_date — sit with the other
// tools that change something.

// findDate resolves an id, or a title that matches exactly one date.
func (t *tools) findDate(ctx context.Context, ref string) (engine.ReminderView, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return engine.ReminderView{}, &engine.InvalidError{Reason: "date is required: an id or a title from list_dates"}
	}
	all, err := t.e.Reminders(ctx, t.lang)
	if err != nil {
		return engine.ReminderView{}, err
	}
	var matches []engine.ReminderView
	for _, r := range all {
		if r.ID == ref {
			return r, nil
		}
		if strings.EqualFold(r.Title, ref) {
			matches = append(matches, r)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return engine.ReminderView{}, &engine.InvalidError{Reason: fmt.Sprintf("no date is called %q; see list_dates", ref)}
	}
	return engine.ReminderView{}, &engine.InvalidError{Reason: fmt.Sprintf("%d dates are called %q; use the id", len(matches), ref)}
}

// ---------- preview ----------

type previewDateIn struct {
	Title    string `json:"title,omitempty" jsonschema:"a label for the answer; \"(preview)\" when omitted"`
	Kind     string `json:"kind,omitempty" jsonschema:"renewal, birthday, anniversary, holiday, expiry or custom (the default)"`
	Freq     string `json:"freq" jsonschema:"once, weekly, monthly or yearly"`
	Calendar string `json:"calendar,omitempty" jsonschema:"solar (the default) or lunar; lunar works with monthly and yearly"`
	Date     string `json:"date,omitempty" jsonschema:"for once: the day, as YYYY-MM-DD. With interval above 1: the day to count from"`
	Weekday  int    `json:"weekday,omitempty" jsonschema:"for weekly: 1 is Monday, 7 is Sunday"`
	Month    int    `json:"month,omitempty" jsonschema:"for yearly: the month, 1-12 (a lunar month when calendar is lunar)"`
	Day      int    `json:"day,omitempty" jsonschema:"for monthly and yearly: the day of the month"`
	Interval int    `json:"interval,omitempty" jsonschema:"every N weeks, months or years; needs date to count from"`
	Since    int    `json:"since,omitempty" jsonschema:"the year it started — a birth year — so the answer can say which one it is"`
	LeadDays []int  `json:"lead_days,omitempty" jsonschema:"the days before the date on which to remind; 0 is the day itself. Only decides whether the answer says soon"`
}

func (t *tools) previewDate(ctx context.Context, in previewDateIn) (dateOut, error) {
	rule, err := ruleOf(in.Freq, in.Calendar, in.Date, in.Weekday, in.Month, in.Day, in.Interval, in.Since)
	if err != nil {
		return dateOut{}, err
	}
	kind := in.Kind
	if kind == "" {
		kind = core.ReminderCustom
	}
	if !slices.Contains(core.ReminderKinds, kind) {
		return dateOut{}, &engine.InvalidError{Reason: "kind must be one of " + strings.Join(core.ReminderKinds, ", ")}
	}
	leads, err := checkLeadDays(in.LeadDays)
	if err != nil {
		return dateOut{}, err
	}
	view, err := t.e.PreviewRule(ctx, rule, t.lang)
	if err != nil {
		return dateOut{}, err
	}
	out := t.dateOf(view)
	out.Kind = kind
	if title := strings.TrimSpace(in.Title); title != "" {
		out.Title = title
	} else {
		out.Title = "(preview)"
	}
	if leads != nil {
		out.LeadDays = leads
		if view.Days != nil {
			switch {
			case *view.Days == 0:
				out.State = "today"
			case slices.Contains(leads, *view.Days):
				out.State = "soon"
			default:
				out.State = "upcoming"
			}
		}
	}
	return t.dated(ctx, out)
}

// checkLeadDays tidies reminder days the way the engine stores them: most
// days first, each once. A nil slice is left alone.
func checkLeadDays(leads []int) ([]int, error) {
	if leads == nil {
		return nil, nil
	}
	if len(leads) > 12 {
		return nil, &engine.InvalidError{Reason: "at most 12 lead days"}
	}
	out := append([]int(nil), leads...)
	slices.SortFunc(out, func(a, b int) int { return b - a })
	out = slices.Compact(out)
	for _, lead := range out {
		if lead < 0 || lead > 366 {
			return nil, &engine.InvalidError{Reason: "lead_days must be between 0 and 366"}
		}
	}
	if len(out) == 0 {
		out = []int{0}
	}
	return out, nil
}

// ---------- change ----------

// updateDateIn changes a date. A field left out keeps its value; a string
// sent as "" clears it where the date has a use for empty.
type updateDateIn struct {
	Date     string    `json:"date" jsonschema:"the date's id, or its title as shown in list_dates"`
	Title    *string   `json:"title,omitempty"`
	Kind     *string   `json:"kind,omitempty" jsonschema:"renewal, birthday, anniversary, holiday, expiry or custom"`
	Rule     *schedule `json:"rule,omitempty" jsonschema:"a new rule, in the shape of a date's schedule field; the rule replaces the old one as a whole"`
	LeadDays *[]int    `json:"lead_days,omitempty" jsonschema:"the days before the date on which to remind; 0 is the day itself. Replaces the old ones"`
	Amount   *float64  `json:"amount,omitempty" jsonschema:"what a renewal costs; 0 clears it"`
	Currency *string   `json:"currency,omitempty" jsonschema:"three-letter code for amount; the base currency when empty"`
	Group    *string   `json:"group,omitempty"`
	Notes    *string   `json:"notes,omitempty"`
	Enabled  *bool     `json:"enabled,omitempty" jsonschema:"false switches the date off without deleting it; true switches it back on"`
	_        struct{}  `json:"-"`
}

func (t *tools) updateDate(ctx context.Context, in updateDateIn) (dateOut, error) {
	view, err := t.findDate(ctx, in.Date)
	if err != nil {
		return dateOut{}, err
	}
	input := engine.ReminderInput{
		Title: in.Title, Kind: in.Kind, LeadDays: in.LeadDays, Amount: in.Amount, Currency: in.Currency,
		Group: in.Group, Notes: in.Notes, Enabled: in.Enabled,
	}
	if in.Rule != nil {
		rule, err := in.Rule.rule()
		if err != nil {
			return dateOut{}, err
		}
		input.Rule = &rule
	}
	if input == (engine.ReminderInput{}) {
		return dateOut{}, &engine.InvalidError{Reason: "nothing to change: give at least one field besides date"}
	}
	updated, err := t.e.UpdateReminder(ctx, view.ID, input, t.lang)
	if err != nil {
		return dateOut{}, err
	}
	return t.dateResult(ctx, updated)
}

type dateRefIn struct {
	Date string `json:"date" jsonschema:"the date's id, or its title as shown in list_dates"`
}

// deletedOut is the answer of a tool that removes something for good.
type deletedOut struct {
	Deleted bool `json:"deleted"`
}

func (t *tools) deleteDate(ctx context.Context, in dateRefIn) (deletedOut, error) {
	view, err := t.findDate(ctx, in.Date)
	if err != nil {
		return deletedOut{}, err
	}
	if err := t.e.DeleteReminder(ctx, view.ID); err != nil {
		return deletedOut{}, err
	}
	return deletedOut{Deleted: true}, nil
}

func (t *tools) reopenDate(ctx context.Context, in dateRefIn) (dateOut, error) {
	view, err := t.findDate(ctx, in.Date)
	if err != nil {
		return dateOut{}, err
	}
	reopened, err := t.e.ReopenReminder(ctx, view.ID, t.lang)
	if err != nil {
		return dateOut{}, err
	}
	return t.dateResult(ctx, reopened)
}

type snoozeDateIn struct {
	Date  string `json:"date" jsonschema:"the date's id, or its title as shown in list_dates"`
	Until string `json:"until" jsonschema:"no reminders until after this day, YYYY-MM-DD; today or later"`
}

func (t *tools) snoozeDate(ctx context.Context, in snoozeDateIn) (dateOut, error) {
	view, err := t.findDate(ctx, in.Date)
	if err != nil {
		return dateOut{}, err
	}
	until, err := recur.ParseDate(strings.TrimSpace(in.Until))
	if err != nil {
		return dateOut{}, &engine.InvalidError{Reason: err.Error()}
	}
	snoozed, err := t.e.SnoozeReminder(ctx, view.ID, &until, t.lang)
	if err != nil {
		return dateOut{}, err
	}
	return t.dateResult(ctx, snoozed)
}

func (t *tools) unsnoozeDate(ctx context.Context, in dateRefIn) (dateOut, error) {
	view, err := t.findDate(ctx, in.Date)
	if err != nil {
		return dateOut{}, err
	}
	woken, err := t.e.SnoozeReminder(ctx, view.ID, nil, t.lang)
	if err != nil {
		return dateOut{}, err
	}
	return t.dateResult(ctx, woken)
}

package mcp

import (
	"context"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/engine"
	"github.com/itswl/quotapulse/internal/i18n"
)

// Meters beyond reading them: silencing one, and — with admin — changing
// its rules or removing it.

type meterRefIn struct {
	Meter string `json:"meter" jsonschema:"the meter's id, or its name as shown in list_meters"`
}

type muteMeterIn struct {
	Meter string `json:"meter" jsonschema:"the meter's id, or its name as shown in list_meters"`
	Until string `json:"until" jsonschema:"hold notifications back until this moment, RFC 3339; it must be in the future"`
}

type meterOut struct {
	Context
	Meter Meter `json:"meter"`
}

func (t *tools) muteMeter(ctx context.Context, in muteMeterIn) (meterOut, error) {
	view, err := t.findMeter(ctx, in.Meter)
	if err != nil {
		return meterOut{}, err
	}
	until, err := time.Parse(time.RFC3339, strings.TrimSpace(in.Until))
	if err != nil {
		return meterOut{}, &engine.InvalidError{Reason: "until must be an RFC 3339 time in the future"}
	}
	view, err = t.e.MuteMeter(ctx, view.ID, &until, t.lang)
	if err != nil {
		return meterOut{}, err
	}
	return t.meterResult(ctx, view)
}

func (t *tools) unmuteMeter(ctx context.Context, in meterRefIn) (meterOut, error) {
	view, err := t.findMeter(ctx, in.Meter)
	if err != nil {
		return meterOut{}, err
	}
	view, err = t.e.MuteMeter(ctx, view.ID, nil, t.lang)
	if err != nil {
		return meterOut{}, err
	}
	return t.meterResult(ctx, view)
}

func (t *tools) meterResult(ctx context.Context, view engine.MeterView) (meterOut, error) {
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return meterOut{}, err
	}
	return meterOut{Context: t.contextOf(settings), Meter: meterOf(view, i18n.Of(t.lang), t.e.Now())}, nil
}

// ---------- admin ----------

// rulesIn is a meter's rules as an agent sends them. The stored rules mark
// nothing optional; here every rule is, so one can be sent alone — although
// rules always replace the old ones as a whole, and a rule left out goes
// back to the default.
type rulesIn struct {
	Low        *float64 `json:"low,omitempty" jsonschema:"the value at or below which an incident opens; for a balance or a quota"`
	RunwayDays *float64 `json:"runway_days,omitempty" jsonschema:"how many days of runway open a warning; for a balance"`
	Anomaly    *bool    `json:"anomaly,omitempty" jsonschema:"whether a burst of spend opens an incident; for a balance"`
	Budget     *float64 `json:"budget,omitempty" jsonschema:"a monthly ceiling on spend, in the meter's unit; 0 turns it off"`
	Pace       bool     `json:"pace,omitempty" jsonschema:"whether a quota that will run out before it resets opens an incident"`
	StaleAfter *int64   `json:"stale_after_seconds,omitempty" jsonschema:"for a meter reported from outside: how long its silence opens an incident, in seconds"`
}

func (r rulesIn) rules() core.Rules {
	return core.Rules{Low: r.Low, RunwayDays: r.RunwayDays, Anomaly: r.Anomaly, Budget: r.Budget, Pace: r.Pace, StaleAfter: r.StaleAfter}
}

type updateMeterIn struct {
	Meter  string   `json:"meter" jsonschema:"the meter's id, or its name as shown in list_meters"`
	Name   *string  `json:"name,omitempty" jsonschema:"a name of the meter's own; it is shown under it from then on"`
	Hidden *bool    `json:"hidden,omitempty" jsonschema:"a hidden meter is not judged: nothing opens on it"`
	Sort   *int     `json:"sort,omitempty" jsonschema:"where it sorts among its account's meters"`
	Unit   *string  `json:"unit,omitempty" jsonschema:"a pushed meter's unit; a sampled one takes its unit from its provider"`
	Rules  *rulesIn `json:"rules,omitempty" jsonschema:"the meter's rules as a whole: a rule left out goes back to the default, and 0 turns one off"`
}

func (t *tools) updateMeter(ctx context.Context, in updateMeterIn) (getMeterOut, error) {
	view, err := t.findMeter(ctx, in.Meter)
	if err != nil {
		return getMeterOut{}, err
	}
	if in.Name == nil && in.Hidden == nil && in.Sort == nil && in.Unit == nil && in.Rules == nil {
		return getMeterOut{}, &engine.InvalidError{Reason: "nothing to change: give at least one field besides meter"}
	}
	input := engine.MeterInput{Name: in.Name, Hidden: in.Hidden, Sort: in.Sort, Unit: in.Unit}
	if in.Rules != nil {
		rules := in.Rules.rules()
		input.Rules = &rules
	}
	updated, err := t.e.UpdateMeter(ctx, view.ID, input, t.lang)
	if err != nil {
		return getMeterOut{}, err
	}
	return t.meterDetail(ctx, updated, getMeterIn{})
}

func (t *tools) deleteMeter(ctx context.Context, in meterRefIn) (deletedOut, error) {
	view, err := t.findMeter(ctx, in.Meter)
	if err != nil {
		return deletedOut{}, err
	}
	if err := t.e.DeleteMeter(ctx, view.ID); err != nil {
		return deletedOut{}, err
	}
	return deletedOut{Deleted: true}, nil
}

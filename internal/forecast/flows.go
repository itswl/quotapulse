// Package forecast turns a meter's history into "how long will it last".
//
// A meter is only ever sampled, so everything here is reconstructed from the
// difference between neighbouring readings: a balance that dropped was spent,
// one that rose was topped up. From that come a burn rate, a runway, and —
// for quotas — whether the allowance will last until it resets.
//
// The package is pure. It never reads a clock: forecasts are "as of" the last
// reading, so a meter that stopped reporting does not quietly grow a longer
// runway.
package forecast

import (
	"time"

	"quotapulse/internal/core"
	"quotapulse/internal/recur"
)

// Flow is what moved between two neighbouring readings.
type Flow struct {
	From, To time.Time
	Spend    float64 // consumed in the interval
	TopUp    float64 // added in the interval
}

// Flows reads a time-ordered series as movement. What a rise or a drop means
// depends on the kind of meter.
func Flows(readings []core.Reading, kind core.Kind) []Flow {
	if len(readings) < 2 {
		return nil
	}
	flows := make([]Flow, 0, len(readings)-1)
	for i := 1; i < len(readings); i++ {
		prev, cur := readings[i-1], readings[i]
		if !cur.At.After(prev.At) {
			continue
		}
		flow := Flow{From: prev.At, To: cur.At}
		switch delta := cur.Value - prev.Value; {
		case kind == core.Counter && delta > 0:
			flow.Spend = delta
		case kind == core.Counter && delta < 0:
			// The counter started over. Whatever it reads now was spent
			// since; what was spent before the restart is unknowable.
			flow.Spend = max(cur.Value, 0)
		case kind != core.Counter && delta < 0:
			flow.Spend = -delta
		case kind != core.Counter && delta > 0:
			// A balance was topped up; a quota was reset.
			flow.TopUp = delta
		}
		if flow.Spend != 0 || flow.TopUp != 0 {
			flows = append(flows, flow)
		}
	}
	return flows
}

// SpendBetween adds up the spend that falls inside [from, to). A flow that
// straddles a boundary is split in proportion to time: with hourly samples the
// meter says nothing about when within the hour the money went.
func SpendBetween(flows []Flow, from, to time.Time) float64 {
	if !to.After(from) {
		return 0
	}
	var total float64
	for _, f := range flows {
		if f.Spend == 0 || !f.To.After(from) || !f.From.Before(to) {
			continue
		}
		start, end := f.From, f.To
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		total += f.Spend * float64(end.Sub(start)) / float64(f.To.Sub(f.From))
	}
	return total
}

// Event is a discrete movement at a point in time.
type Event struct {
	At     time.Time `json:"at"`
	Amount float64   `json:"amount"`
}

// Tidy returns the event rounded for a reader; see Balance.Tidy.
func (e Event) Tidy() Event {
	e.At, e.Amount = e.At.Truncate(time.Second), round(e.Amount, 6)
	return e
}

// TopUps lists the rises in a series. Unlike spend, a top-up is an event: it
// happened at one moment, and the best guess for that moment is the reading
// that first showed it.
func TopUps(flows []Flow) []Event {
	var events []Event
	for _, f := range flows {
		if f.TopUp > 0 {
			events = append(events, Event{At: f.To, Amount: f.TopUp})
		}
	}
	return events
}

// Day is one local calendar day of movement.
type Day struct {
	Date  recur.Date `json:"date"`
	Spend float64    `json:"spend"`
	TopUp float64    `json:"top_up"`
}

// Tidy returns the day rounded for a reader; see Balance.Tidy.
func (d Day) Tidy() Day {
	d.Spend, d.TopUp = round(d.Spend, 6), round(d.TopUp, 6)
	return d
}

// Daily buckets flows into local calendar days covering [from, to]. Days with
// no movement are present with zeros: a chart of spend needs its quiet days.
func Daily(flows []Flow, from, to time.Time, loc *time.Location) []Day {
	if to.Before(from) {
		return nil
	}
	first, last := recur.DateOf(from.In(loc)), recur.DateOf(to.In(loc))
	days := make([]Day, 0, last.Sub(first)+1)
	index := map[recur.Date]int{}
	for day := first; !day.After(last); day = day.AddDays(1) {
		index[day] = len(days)
		start, end := day.In(loc), day.AddDays(1).In(loc)
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		days = append(days, Day{Date: day, Spend: SpendBetween(flows, start, end)})
	}
	for _, event := range TopUps(flows) {
		if event.At.Before(from) || event.At.After(to) {
			continue
		}
		if i, ok := index[recur.DateOf(event.At.In(loc))]; ok {
			days[i].TopUp += event.Amount
		}
	}
	return days
}

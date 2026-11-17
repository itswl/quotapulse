package forecast

import (
	"math"
	"time"

	"quotapulse/internal/core"
)

// How much history a forecast uses, and how it is weighed.
const (
	// Lookback is how far back spend informs the burn rate.
	Lookback = 30 * 24 * time.Hour
	// HalfLife is how fast an old day stops mattering: a day a week ago
	// weighs half as much as the last one. A one-off batch job fades out
	// instead of dragging the average for a month.
	HalfLife = 7 * 24 * time.Hour

	day = 24 * time.Hour

	// Below minCoverage there is not enough history to say anything.
	minCoverage = 6 * time.Hour
	// Bursts are judged against a baseline, which needs a few days to exist.
	minBaseline = 3 * day
)

// Burst windows: a short one catches a leaked key within hours, a long one
// catches a job left running.
var burstWindows = []time.Duration{3 * time.Hour, day}

// Confidence says how much history stands behind a forecast.
const (
	ConfidenceNone   = "none"   // too little history to estimate
	ConfidenceLow    = "low"    // under a day: shown, never alerted on
	ConfidenceMedium = "medium" // one to three days
	ConfidenceHigh   = "high"   // three days or more
)

// Balance is the forecast for a stock that is spent down.
type Balance struct {
	// AsOf is the last reading. Everything below describes that moment.
	AsOf         time.Time `json:"as_of"`
	CoverageDays float64   `json:"coverage_days"`
	Confidence   string    `json:"confidence"`

	// Burn is the expected spend per day: an average of the last 30 days in
	// which recent days count for more.
	Burn float64 `json:"burn_per_day"`
	// Burn7 and Burn30 are plain averages, shown next to Burn so the
	// estimate can be checked by eye.
	Burn7  *float64 `json:"burn_7d"`
	Burn30 *float64 `json:"burn_30d"`
	// Peak is the heaviest 24 hours in the lookback.
	Peak     float64 `json:"peak_per_day"`
	Spend24h float64 `json:"spend_24h"`

	// RunwayDays is how long the balance lasts at Burn; nil when nothing is
	// being spent or there is no estimate.
	RunwayDays *float64   `json:"runway_days"`
	DepletesAt *time.Time `json:"depletes_at"`
	// RunwayAtPeak is how long it lasts if every day were like the heaviest one.
	RunwayAtPeak *float64 `json:"runway_days_at_peak"`

	// Bursts are recent windows in which money left faster than the baseline.
	Bursts    []Burst `json:"bursts,omitempty"`
	LastTopUp *Event  `json:"last_top_up"`
}

// Burst is a recent window compared with what came before it.
type Burst struct {
	WindowHours float64 `json:"window_hours"`
	Spend       float64 `json:"spend"`
	// RatePerDay is the window's spend scaled to a day.
	RatePerDay float64 `json:"rate_per_day"`
	// RunwayDays is how long the balance lasts at this rate.
	RunwayDays float64 `json:"runway_days"`
	// BaselinePerDay is the spend per day before the window. A burst
	// inflates every average it is part of, so "the usual pace" has to be
	// measured without it.
	BaselinePerDay float64 `json:"baseline_per_day"`
	// Ratio compares the rate with the baseline; nil when nothing was spent
	// before, so there is nothing to compare with.
	Ratio *float64 `json:"ratio"`
}

// Alertable reports whether the forecast rests on enough history to act on.
func (b Balance) Alertable() bool {
	return b.Confidence == ConfidenceMedium || b.Confidence == ConfidenceHigh
}

// Tidy returns the forecast rounded for a reader: amounts and rates to six
// decimals, days and ratios to two, times to the second. Decisions are made
// on the unrounded forecast; this is for whoever is shown the result — a
// person, a chart, or an agent paying for every digit of 1.6007670387311534.
func (b Balance) Tidy() Balance {
	b.AsOf = b.AsOf.Truncate(time.Second)
	b.Burn, b.Peak, b.Spend24h = round(b.Burn, 6), round(b.Peak, 6), round(b.Spend24h, 6)
	b.Burn7, b.Burn30 = roundPtr(b.Burn7, 6), roundPtr(b.Burn30, 6)
	b.RunwayDays, b.RunwayAtPeak = roundPtr(b.RunwayDays, 2), roundPtr(b.RunwayAtPeak, 2)
	if b.DepletesAt != nil {
		b.DepletesAt = core.Ptr(b.DepletesAt.Truncate(time.Second))
	}
	if b.Bursts != nil {
		bursts := make([]Burst, len(b.Bursts))
		for i, burst := range b.Bursts {
			burst.Spend, burst.RatePerDay = round(burst.Spend, 6), round(burst.RatePerDay, 6)
			burst.BaselinePerDay = round(burst.BaselinePerDay, 6)
			burst.RunwayDays, burst.Ratio = round(burst.RunwayDays, 2), roundPtr(burst.Ratio, 2)
			bursts[i] = burst
		}
		b.Bursts = bursts
	}
	if b.LastTopUp != nil {
		b.LastTopUp = core.Ptr(b.LastTopUp.Tidy())
	}
	return b
}

// ForBalance forecasts a balance or counter from its readings, oldest first.
func ForBalance(readings []core.Reading, kind core.Kind) Balance {
	out := Balance{Confidence: ConfidenceNone}
	if len(readings) == 0 {
		return out
	}
	last := readings[len(readings)-1]
	out.AsOf = last.At
	start := readings[0].At
	if floor := last.At.Add(-Lookback); start.Before(floor) {
		start = floor
	}
	coverage := last.At.Sub(start)
	out.CoverageDays = round(coverage.Hours()/24, 2)

	flows := Flows(readings, kind)
	if tops := TopUps(flows); len(tops) > 0 && kind == core.Balance {
		out.LastTopUp = &tops[len(tops)-1]
	}
	out.Spend24h = SpendBetween(flows, last.At.Add(-day), last.At)
	if coverage < minCoverage {
		return out
	}
	out.Confidence = confidence(coverage)

	// Walk back one day at a time from the last reading. Each day gets a
	// weight that halves every HalfLife; a day only partly covered by data
	// counts for the part that is covered.
	var weightedSpend, weightedDays float64
	for i := 0; ; i++ {
		to := last.At.Add(-time.Duration(i) * day)
		from := to.Add(-day)
		if !to.After(start) {
			break
		}
		if from.Before(start) {
			from = start
		}
		spend := SpendBetween(flows, from, to)
		covered := to.Sub(from).Hours() / 24
		weight := math.Pow(0.5, float64(i)*float64(day)/float64(HalfLife))
		weightedSpend += weight * spend
		weightedDays += weight * covered
		// only a full day can be the heaviest day
		if covered > 0.999 && spend > out.Peak {
			out.Peak = spend
		}
	}
	if weightedDays > 0 {
		out.Burn = weightedSpend / weightedDays
	}
	out.Burn7 = plainAverage(flows, start, last.At, 7*day)
	out.Burn30 = plainAverage(flows, start, last.At, Lookback)

	if kind == core.Balance {
		balance := math.Max(last.Value, 0)
		if out.Burn > 0 {
			runway := balance / out.Burn
			out.RunwayDays = &runway
			if runway < 3650 {
				at := last.At.Add(time.Duration(runway * float64(day)))
				out.DepletesAt = &at
			}
		}
		if out.Peak > 0 {
			out.RunwayAtPeak = core.Ptr(balance / out.Peak)
		}
		out.Bursts = bursts(flows, start, last.At, balance)
	}
	return out
}

// plainAverage is spend per day over the last window, or nil when history
// does not reach that far back.
func plainAverage(flows []Flow, start, end time.Time, window time.Duration) *float64 {
	from := end.Add(-window)
	if from.Before(start) {
		// Not a full window. Within a day of it is close enough to report.
		if start.Sub(from) > day {
			return nil
		}
		from = start
	}
	days := end.Sub(from).Hours() / 24
	if days <= 0 {
		return nil
	}
	return core.Ptr(SpendBetween(flows, from, end) / days)
}

// bursts compares each recent window with the spend before it.
func bursts(flows []Flow, start, end time.Time, balance float64) []Burst {
	var out []Burst
	for _, window := range burstWindows {
		windowStart := end.Add(-window)
		baselineSpan := windowStart.Sub(start)
		if baselineSpan < minBaseline {
			continue
		}
		spend := SpendBetween(flows, windowStart, end)
		if spend <= 0 {
			continue
		}
		rate := spend / (window.Hours() / 24)
		burst := Burst{
			WindowHours: window.Hours(),
			Spend:       spend,
			RatePerDay:  rate,
			RunwayDays:  balance / rate,
		}
		baseline := SpendBetween(flows, start, windowStart) / (baselineSpan.Hours() / 24)
		if baseline > 0 {
			burst.BaselinePerDay = baseline
			burst.Ratio = core.Ptr(rate / baseline)
		}
		out = append(out, burst)
	}
	return out
}

func confidence(coverage time.Duration) string {
	switch {
	case coverage < minCoverage:
		return ConfidenceNone
	case coverage < day:
		return ConfidenceLow
	case coverage < 3*day:
		return ConfidenceMedium
	default:
		return ConfidenceHigh
	}
}

// Quota is the forecast for an allowance that resets.
type Quota struct {
	// RemainingPct and UsedPct are shares of the limit, 0-100.
	RemainingPct float64 `json:"remaining_pct"`
	UsedPct      float64 `json:"used_pct"`
	// ElapsedPct is how far through the window we are; nil when the window's
	// length or reset time is unknown.
	ElapsedPct *float64 `json:"elapsed_pct"`
	// ProjectedPct is the share that will have been used by the reset if the
	// window continues as it has gone so far. Above 100 means it runs out.
	ProjectedPct *float64 `json:"projected_pct"`
	// ExhaustsAt is when it runs out, if that is before the reset.
	ExhaustsAt *time.Time `json:"exhausts_at"`
}

// Early in a window the pace is noise: one request in the first minute
// projects to a thousand percent. Wait until a slice of the window has passed.
const minElapsedForPace = 0.1

// ForQuota forecasts an allowance from its latest reading.
func ForQuota(value float64, limit *float64, resetsAt *time.Time, windowSeconds *int64, now time.Time) Quota {
	total := 100.0
	if limit != nil && *limit > 0 {
		total = *limit
	}
	remaining := clamp(value/total*100, 0, 100)
	out := Quota{RemainingPct: round(remaining, 2), UsedPct: round(100-remaining, 2)}

	if resetsAt == nil || windowSeconds == nil || *windowSeconds <= 0 || !resetsAt.After(now) {
		return out
	}
	window := time.Duration(*windowSeconds) * time.Second
	elapsed := now.Sub(resetsAt.Add(-window))
	if elapsed <= 0 || elapsed > window {
		return out
	}
	share := float64(elapsed) / float64(window)
	out.ElapsedPct = core.Ptr(round(share*100, 2))
	if share < minElapsedForPace || out.UsedPct <= 0 {
		return out
	}
	out.ProjectedPct = core.Ptr(round(out.UsedPct/share, 2))
	if remaining <= 0 {
		out.ExhaustsAt = &now
		return out
	}
	// remaining / (used per unit of time)
	left := time.Duration(remaining / out.UsedPct * float64(elapsed))
	if at := now.Add(left); at.Before(*resetsAt) {
		out.ExhaustsAt = &at
	}
	return out
}

// Month is spend in one calendar month.
type Month struct {
	Month string  `json:"month"` // YYYY-MM
	Spend float64 `json:"spend"` // so far
	// Projected is where the month ends up: what was spent, plus the
	// expected burn for the days that are left.
	Projected   float64 `json:"projected"`
	DaysElapsed float64 `json:"days_elapsed"`
	DaysInMonth int     `json:"days_in_month"`
}

// ForMonth sums the month containing now and extends it to the month's end.
// burn is the expected daily spend, or nil when there is no estimate, in
// which case the month so far is extended in a straight line.
func ForMonth(flows []Flow, burn *float64, now time.Time, loc *time.Location) Month {
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 1, 0)
	elapsed := local.Sub(start).Hours() / 24
	total := end.Sub(start).Hours() / 24

	out := Month{
		Month:       start.Format("2006-01"),
		Spend:       SpendBetween(flows, start, now),
		DaysElapsed: round(elapsed, 2),
		DaysInMonth: int(math.Round(total)),
	}
	out.Projected = out.Spend
	switch remaining := total - elapsed; {
	case burn != nil:
		out.Projected += *burn * remaining
	case elapsed >= 1:
		out.Projected = out.Spend / elapsed * total
	}
	return out
}

// Tidy returns the month rounded for a reader; see Balance.Tidy.
func (m Month) Tidy() Month {
	m.Spend, m.Projected = round(m.Spend, 6), round(m.Projected, 6)
	return m
}

// Tidy returns the quota forecast with its time cut to the second.
func (q Quota) Tidy() Quota {
	if q.ExhaustsAt != nil {
		q.ExhaustsAt = core.Ptr(q.ExhaustsAt.Truncate(time.Second))
	}
	return q
}

func clamp(v, low, high float64) float64 { return math.Max(low, math.Min(high, v)) }

func round(v float64, decimals int) float64 {
	shift := math.Pow(10, float64(decimals))
	return math.Round(v*shift) / shift
}

func roundPtr(v *float64, decimals int) *float64 {
	if v == nil {
		return nil
	}
	return core.Ptr(round(*v, decimals))
}

// Round rounds an amount for a reader, the way Tidy does.
func Round(v float64) float64 { return round(v, 6) }

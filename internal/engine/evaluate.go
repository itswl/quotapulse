package engine

import (
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/forecast"
	"github.com/itswl/quotapulse/internal/recur"
)

// condition is one fact about the present that deserves an incident.
type condition struct {
	Kind     string
	Dedup    string
	Severity core.Severity
	Reason   string
	Data     map[string]any
}

// Budget reasons.
const (
	ReasonProjected = "projected" // on course to pass the budget
	ReasonExceeded  = "exceeded"  // already past it
)

// Thresholds that decide when a fact is worth an incident.
const (
	// An open incident closes a little past where it opened. Without the
	// margin a runway hovering at exactly seven days would open and resolve
	// on alternate samples, and each flip is a notification.
	stickyRunway = 1.15
	stickyLow    = 1.05

	// A burst only counts when money is leaving at least this many times
	// faster than before.
	burstRatio = 3.0
	// The short burst window is about a leaked key: it matters when the
	// balance would be gone within days at that rate, whatever the lead time.
	shortBurstHorizonDays = 2.0
	// When the runway rule is off, bursts still need a horizon to be judged by.
	fallbackLeadDays = 7.0
	// An exhausted quota that resets within a day will fix itself; one that
	// stays empty longer needs a person.
	quotaSelfHeals = 24 * time.Hour
	// A straight-line projection from the first days of a month is noise.
	minDaysForProjection = 3.0
)

// LevelData is what a level incident knows about its meter.
type LevelData struct {
	MeterKind core.Kind `json:"meter_kind"`
	Unit      string    `json:"unit"`
	Value     float64   `json:"value"`
	Limit     *float64  `json:"limit,omitempty"`
	Low       *float64  `json:"low,omitempty"`

	LeadDays   float64         `json:"lead_days,omitempty"`
	Burn       float64         `json:"burn_per_day,omitempty"`
	RunwayDays *float64        `json:"runway_days,omitempty"`
	DepletesAt *time.Time      `json:"depletes_at,omitempty"`
	Burst      *forecast.Burst `json:"burst,omitempty"`

	RemainingPct *float64   `json:"remaining_pct,omitempty"`
	ResetsAt     *time.Time `json:"resets_at,omitempty"`
	ExhaustsAt   *time.Time `json:"exhausts_at,omitempty"`
}

// BudgetData is what a budget incident knows.
type BudgetData struct {
	Unit      string  `json:"unit"`
	Month     string  `json:"month"`
	Spend     float64 `json:"spend"`
	Projected float64 `json:"projected"`
	Budget    float64 `json:"budget"`
}

// StaleData is what a stale incident knows.
type StaleData struct {
	LastAt     time.Time `json:"last_at"`
	StaleAfter int64     `json:"stale_after_seconds"`
}

// FailingData is what a failing incident knows about its account.
type FailingData struct {
	Provider string     `json:"provider"`
	Error    string     `json:"error"`
	Failures int        `json:"failures"`
	Since    *time.Time `json:"since,omitempty"`
}

// DueData is what a due incident knows about its reminder's occurrence.
type DueData struct {
	ReminderKind string     `json:"reminder_kind"`
	Date         recur.Date `json:"date"`
	Days         int        `json:"days"`
	Lunar        string     `json:"lunar,omitempty"`
	Ordinal      int        `json:"ordinal,omitempty"`
	Amount       *float64   `json:"amount,omitempty"`
	Currency     string     `json:"currency,omitempty"`
	Notes        string     `json:"notes,omitempty"`
	// Sent lists the lead days already announced for this occurrence.
	Sent []int `json:"sent"`
}

// DeliveryData is what a delivery incident knows about its channel.
type DeliveryData struct {
	ChannelType string `json:"channel_type"`
	Error       string `json:"error"`
	Failures    int    `json:"failures"`
}

// meterInput is everything the judgement of one meter depends on.
type meterInput struct {
	Meter    core.Meter
	Readings []core.Reading
	Settings core.Settings
	Now      time.Time
	// Open are the meter's open incidents by kind. They only lend
	// stickiness: the facts are judged fresh every time.
	Open map[string]core.Incident
}

// meterConditions judges one meter. It is pure: the same input always gives
// the same conditions.
func meterConditions(in meterInput) []condition {
	m := in.Meter
	if m.Hidden || m.Value == nil {
		return nil
	}
	var out []condition
	flows := forecast.Flows(in.Readings, m.Kind)

	switch m.Kind {
	case core.Quota:
		quota := forecast.ForQuota(*m.Value, m.Limit, m.ResetsAt, m.Window, in.Now)
		if c := quotaLevel(in, quota); c != nil {
			out = append(out, *c)
		}
	default:
		fc := forecast.ForBalance(in.Readings, m.Kind)
		if m.Kind == core.Balance {
			if c := balanceLevel(in, fc); c != nil {
				out = append(out, *c)
			}
		}
		if c := budgetCondition(in, flows, fc); c != nil {
			out = append(out, *c)
		}
	}
	if c := staleCondition(in); c != nil {
		out = append(out, *c)
	}
	return out
}

// lead is the meter's runway lead time in days; 0 means the rule is off.
func lead(m core.Meter, s core.Settings) float64 {
	if m.Rules.RunwayDays != nil {
		return *m.Rules.RunwayDays
	}
	return s.RunwayDays
}

// criticalRunway is the runway at which "soon" becomes "now".
func criticalRunway(leadDays float64) float64 { return min(2, leadDays/2) }

func balanceLevel(in meterInput, fc forecast.Balance) *condition {
	m, value := in.Meter, *in.Meter.Value
	open, isOpen := in.Open[core.IncidentLevel]
	leadDays := lead(m, in.Settings)
	data := LevelData{
		MeterKind: m.Kind, Unit: m.Unit, Value: value, Low: m.Rules.Low,
		LeadDays: leadDays, Burn: fc.Burn, RunwayDays: fc.RunwayDays, DepletesAt: fc.DepletesAt,
	}
	done := func(severity core.Severity, reason string) *condition {
		return &condition{Kind: core.IncidentLevel, Severity: severity, Reason: reason, Data: toMap(data)}
	}

	if value <= 0 {
		return done(core.Critical, core.ReasonEmpty)
	}
	if m.Rules.Low != nil {
		threshold := *m.Rules.Low
		if isOpen && (open.Reason == core.ReasonLow || open.Reason == core.ReasonEmpty) {
			threshold *= stickyLow
		}
		if value <= threshold {
			return done(core.Critical, core.ReasonLow)
		}
	}
	if !fc.Alertable() {
		return nil
	}

	// Two ways of reading the same history can each say "it is running
	// out": the expected pace, and a recent burst. Take the more pressing.
	var best *condition
	if leadDays > 0 && fc.RunwayDays != nil {
		limit := leadDays
		if isOpen && open.Reason == core.ReasonRunway {
			limit *= stickyRunway
		}
		if *fc.RunwayDays <= limit {
			severity := core.Warning
			if *fc.RunwayDays <= criticalRunway(leadDays) {
				severity = core.Critical
			}
			best = done(severity, core.ReasonRunway)
		}
	}

	anomaly := in.Settings.Anomaly
	if m.Rules.Anomaly != nil {
		anomaly = *m.Rules.Anomaly
	}
	if anomaly {
		if burst := worstBurst(fc.Bursts, leadDays, isOpen && open.Reason == core.ReasonBurst); burst != nil {
			severity := core.Warning
			if burst.RunwayDays <= 1 {
				severity = core.Critical
			}
			if best == nil || severity.Rank() > best.Severity.Rank() {
				data.Burst = burst
				best = done(severity, core.ReasonBurst)
			}
		}
	}
	return best
}

// worstBurst picks the burst that would empty the balance soonest, among
// those that are both a real jump over the baseline and soon enough to matter.
//
// Both tests are needed. A jump alone is noise: spending 2.50 after a week
// of 0.10 a day is "25 times the baseline" on a balance that still lasts two
// hundred days. Soon alone is the runway rule's business.
func worstBurst(bursts []forecast.Burst, leadDays float64, sticky bool) *forecast.Burst {
	if leadDays <= 0 {
		leadDays = fallbackLeadDays
	}
	var worst *forecast.Burst
	for i := range bursts {
		b := bursts[i]
		if b.Ratio != nil && *b.Ratio < burstRatio {
			continue
		}
		horizon := leadDays
		if b.WindowHours < 24 {
			horizon = shortBurstHorizonDays
		}
		if sticky {
			horizon *= stickyRunway
		}
		if b.RunwayDays > horizon {
			continue
		}
		if worst == nil || b.RunwayDays < worst.RunwayDays {
			worst = &b
		}
	}
	return worst
}

func quotaLevel(in meterInput, q forecast.Quota) *condition {
	m, value := in.Meter, *in.Meter.Value
	open, isOpen := in.Open[core.IncidentLevel]
	data := LevelData{
		MeterKind: m.Kind, Unit: m.Unit, Value: value, Limit: m.Limit, Low: m.Rules.Low,
		RemainingPct: &q.RemainingPct, ResetsAt: m.ResetsAt, ExhaustsAt: q.ExhaustsAt,
	}
	done := func(severity core.Severity, reason string) *condition {
		return &condition{Kind: core.IncidentLevel, Severity: severity, Reason: reason, Data: toMap(data)}
	}

	if q.RemainingPct <= 0 {
		severity := core.Critical
		if m.ResetsAt != nil && m.ResetsAt.Sub(in.Now) <= quotaSelfHeals && m.ResetsAt.After(in.Now) {
			severity = core.Warning
		}
		return done(severity, core.ReasonEmpty)
	}
	if m.Rules.Low != nil {
		threshold := *m.Rules.Low
		if isOpen && (open.Reason == core.ReasonLow || open.Reason == core.ReasonEmpty) {
			threshold *= stickyLow
		}
		if value <= threshold {
			return done(core.Warning, core.ReasonLow)
		}
	}
	if m.Rules.Pace && q.ExhaustsAt != nil {
		return done(core.Warning, core.ReasonPace)
	}
	return nil
}

func budgetCondition(in meterInput, flows []forecast.Flow, fc forecast.Balance) *condition {
	m := in.Meter
	if m.Rules.Budget == nil || *m.Rules.Budget <= 0 {
		return nil
	}
	budget := *m.Rules.Budget
	var burn *float64
	if fc.Confidence != forecast.ConfidenceNone {
		burn = &fc.Burn
	}
	month := forecast.ForMonth(flows, burn, in.Now, in.Settings.Location())
	data := BudgetData{Unit: m.Unit, Month: month.Month, Spend: month.Spend, Projected: month.Projected, Budget: budget}

	reason := ""
	switch open, isOpen := in.Open[core.IncidentBudget]; {
	case month.Spend > budget:
		reason = ReasonExceeded
	case month.DaysElapsed < minDaysForProjection:
	case month.Projected > budget:
		reason = ReasonProjected
	case isOpen && open.Reason == ReasonProjected && month.Projected > budget/stickyLow:
		reason = ReasonProjected
	}
	if reason == "" {
		return nil
	}
	return &condition{Kind: core.IncidentBudget, Severity: core.Warning, Reason: reason, Data: toMap(data)}
}

func staleCondition(in meterInput) *condition {
	m := in.Meter
	if m.Rules.StaleAfter == nil || *m.Rules.StaleAfter <= 0 || m.SampledAt == nil {
		return nil
	}
	if in.Now.Sub(*m.SampledAt) <= time.Duration(*m.Rules.StaleAfter)*time.Second {
		return nil
	}
	return &condition{
		Kind: core.IncidentStale, Severity: core.Warning,
		Data: toMap(StaleData{LastAt: *m.SampledAt, StaleAfter: *m.Rules.StaleAfter}),
	}
}

// A single failed sample is usually the network. Two in a row is worth
// saying — unless the first already says the credential was refused, which
// no amount of retrying will fix.
const failuresBeforeIncident = 2

// An account that has been unreadable this long is no longer a hiccup:
// whatever it watches could run out unseen.
const blindTooLong = 24 * time.Hour

// accountConditions judges whether an account can be read at all. openedAt
// is when its failing incident opened, or nil when none is open.
func accountConditions(a core.Account, openedAt *time.Time, now time.Time) []condition {
	if !a.Enabled || a.Status != core.StatusFailing {
		return nil
	}
	if a.Failures < failuresBeforeIncident && !credentialRefused(a.LastError) && !a.Unreadable {
		return nil
	}
	severity := core.Warning
	if openedAt != nil && now.Sub(*openedAt) >= blindTooLong {
		severity = core.Critical
	}
	return []condition{{
		Kind: core.IncidentFailing, Severity: severity,
		Data: toMap(FailingData{Provider: a.Provider, Error: a.LastError, Failures: a.Failures, Since: a.SucceededAt}),
	}}
}

// credentialRefused recognises an answer that means "this key is not
// accepted", as opposed to "try again later".
func credentialRefused(message string) bool {
	return strings.HasPrefix(message, "HTTP 401") || strings.HasPrefix(message, "HTTP 403") ||
		strings.HasPrefix(message, "missing ")
}

// reasonRank orders reasons within a kind, so that getting worse without
// changing severity still counts as an escalation.
func reasonRank(reason string) int {
	switch reason {
	case core.ReasonEmpty, ReasonExceeded:
		return 5
	case core.ReasonLow:
		return 4
	case core.ReasonRunway:
		return 3
	case core.ReasonBurst:
		return 2
	case core.ReasonPace, ReasonProjected:
		return 1
	}
	return 0
}

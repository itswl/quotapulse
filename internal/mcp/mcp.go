// Package mcp lets an AI agent ask QuotaPulse the questions a person would:
// what needs attention, is there headroom for the job I am about to run,
// what is coming up — and, with the right scope, act on the answers.
//
// The tools are few and shaped around those questions rather than around the
// database. An agent that calls `attention` first knows what matters; one
// that calls `list_meters` before a large job knows which account can take it.
//
// Every tool goes through the same engine calls as the dashboard, under the
// scopes of the token that was presented. There is nothing an agent can do
// here that the token's owner could not do with curl.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/itswl/quotapulse/internal/api"
	"github.com/itswl/quotapulse/internal/buildinfo"
	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/engine"
	"github.com/itswl/quotapulse/internal/i18n"
	"github.com/itswl/quotapulse/internal/recur"
	"github.com/itswl/quotapulse/internal/store"
)

const instructions = `QuotaPulse watches things that run out (meters: prepaid balances, quotas that reset, running totals of spend) and things that come due (dates: renewals, birthdays, expiries).

Start with "attention": it lists what is open right now and what is coming up.
Before starting work that will consume a lot of an API or a plan, call "list_meters" and read "headroom" for the account you intend to use; prefer one that says "ok".
Amounts are in each meter's own unit — a currency code, "%", or a label such as "calls". Never add amounts across different units.
Times are RFC 3339 in UTC; dates are YYYY-MM-DD in the user's time zone, which every result names.
Tools that change something exist only if the token allows it.`

var schemas = sdk.NewSchemaCache()

// Handler serves MCP over streamable HTTP. It expects to be mounted behind
// the API's token check: each request carries the scopes of its token, and
// the tools offered are decided from them.
func Handler(e *engine.Engine, log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	protocol := protocolLogger(log)
	return sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		name, allows := api.PrincipalScopes(r.Context())
		return newServer(&tools{e: e, log: log, protocol: protocol, caller: name, allows: allows})
	}, &sdk.StreamableHTTPOptions{
		// Stateless: every request stands alone, so any replica — or a
		// restart — can answer it.
		Stateless: true, JSONResponse: true, Logger: protocol,
		// The SDK refuses requests that arrive on loopback with a foreign
		// Host header, to protect unauthenticated local servers from DNS
		// rebinding. This endpoint only accepts a bearer token — nothing a
		// browser sends on its own — and that check would break the common
		// setup of a reverse proxy on the same machine.
		DisableLocalhostProtection: true,
	})
}

// protocolLogger is what the protocol library logs through. At Info it
// announces every session connecting and disconnecting, and here every
// request is a session of its own: three lines of log for one question from
// an agent. Its warnings are kept; the rest is shown only when debugging.
func protocolLogger(log *slog.Logger) *slog.Logger {
	if log.Enabled(context.Background(), slog.LevelDebug) {
		return log
	}
	return slog.New(atLeast{Handler: log.Handler(), level: slog.LevelWarn})
}

// atLeast drops records below a level.
type atLeast struct {
	slog.Handler
	level slog.Level
}

func (h atLeast) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level && h.Handler.Enabled(ctx, level)
}

func (h atLeast) WithAttrs(attrs []slog.Attr) slog.Handler {
	return atLeast{Handler: h.Handler.WithAttrs(attrs), level: h.level}
}

func (h atLeast) WithGroup(name string) slog.Handler {
	return atLeast{Handler: h.Handler.WithGroup(name), level: h.level}
}

type tools struct {
	e   *engine.Engine
	log *slog.Logger
	// protocol is the logger handed to the protocol library.
	protocol *slog.Logger
	caller   string
	allows   func(scope string) bool
}

func newServer(t *tools) *sdk.Server {
	server := sdk.NewServer(
		&sdk.Implementation{Name: "quotapulse", Title: "QuotaPulse", Version: buildinfo.Version},
		&sdk.ServerOptions{Instructions: instructions, Logger: t.protocol, SchemaCache: schemas},
	)
	if t.allows(core.ScopeRead) {
		add(t, server, readOnly("attention", "What needs attention",
			"What is open right now and what is coming up. Call this first. Returns the overall status, every open incident with a plain-language headline, and the dates inside the look-ahead window."),
			t.attention)
		add(t, server, readOnly("list_meters", "Meters and headroom",
			"Every balance, quota and spend counter with its current value, forecast and a one-word headroom verdict (ok, low, exhausted, unknown). Use it before a large job to pick an account that can take it."),
			t.listMeters)
		add(t, server, readOnly("get_meter", "One meter in detail",
			"A meter's forecast, rules, open incidents and recent daily spend. Identify it by id, or by name as shown in list_meters."),
			t.getMeter)
		add(t, server, readOnly("list_dates", "Upcoming dates",
			"Renewals, birthdays, anniversaries and expiries, soonest first, with how many days are left. Lunar dates are already converted."),
			t.listDates)
		add(t, server, readOnly("get_spend", "Spend for a month",
			"What the meters burned in a calendar month, converted to the base currency where a rate is known, with the month-end projection and the renewals that fall in the month."),
			t.getSpend)
		add(t, server, readOnly("list_activity", "Incident history",
			"Past and present incidents, newest first: what happened, when it opened and when it was resolved."),
			t.listActivity)
	}
	if t.allows(core.ScopeAct) {
		add(t, server, changing("add_date", "Add a date",
			"Add a reminder for a renewal, birthday, anniversary, holiday, expiry or anything else with a date. Supports weekly, monthly, yearly and one-off dates, and the lunar calendar for monthly and yearly ones.", false),
			t.addDate)
		add(t, server, changing("complete_date", "Mark a date as done",
			"Mark the upcoming occurrence of a date as dealt with — paid, renewed, celebrated. Its reminders stop and it moves on to the next occurrence.", true),
			t.completeDate)
		add(t, server, changing("acknowledge", "Acknowledge an incident",
			"Acknowledge an open incident: it stays open until its cause is gone, but stops repeating its notification.", true),
			t.acknowledge)
		add(t, server, changing("sample_now", "Read accounts now",
			"Ask for a fresh reading of one account, or of all of them, instead of waiting for the schedule. The reading arrives within seconds; call list_meters again to see it.", true),
			t.sampleNow)
	}
	if t.allows(core.ScopeIngest) {
		add(t, server, changing("record_reading", "Report a reading",
			"Report the current value of something QuotaPulse cannot read itself: what a local tool has cost so far (kind counter), a balance or a quota. The first reading for a new meter must say its kind.", false),
			t.recordReading)
	}
	return server
}

func readOnly(name, title, description string) *sdk.Tool {
	no := false
	return &sdk.Tool{Name: name, Title: title, Description: description, Annotations: &sdk.ToolAnnotations{
		Title: title, ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no,
	}}
}

func changing(name, title, description string, idempotent bool) *sdk.Tool {
	no := false
	return &sdk.Tool{Name: name, Title: title, Description: description, Annotations: &sdk.ToolAnnotations{
		Title: title, IdempotentHint: idempotent, DestructiveHint: &no, OpenWorldHint: &no,
	}}
}

// add registers a tool. A failure the caller can fix comes back as a tool
// error with the engine's own sentence; anything else is logged and reported
// without its details.
func add[In, Out any](t *tools, server *sdk.Server, tool *sdk.Tool, handle func(context.Context, In) (Out, error)) {
	sdk.AddTool(server, tool, func(ctx context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, Out, error) {
		started := time.Now()
		out, err := handle(ctx, in)
		t.log.Info("mcp tool call", "caller", t.caller, "tool", tool.Name,
			"duration_ms", time.Since(started).Milliseconds(), "ok", err == nil)
		switch {
		case err == nil:
			return nil, out, nil
		case engine.IsInvalid(err):
			return nil, out, err
		case errors.Is(err, store.ErrNotFound):
			return nil, out, errors.New("there is no such thing; check the id or name")
		default:
			t.log.Error("mcp tool failed", "tool", tool.Name, "error", err)
			return nil, out, errors.New("QuotaPulse could not answer that right now")
		}
	})
}

// ---------- shapes ----------

// Context says what moment and what time zone a result describes.
type Context struct {
	Now      time.Time `json:"now" jsonschema:"when this result was produced"`
	Timezone string    `json:"timezone" jsonschema:"the IANA time zone that dates and day boundaries are in"`
}

// Incident is an open or past condition, told in words.
type Incident struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind" jsonschema:"level (a meter is running out), budget, stale, failing (an account cannot be read), due (a date is near) or delivery (a notification channel keeps failing)"`
	Severity     string     `json:"severity" jsonschema:"info, warning or critical"`
	State        string     `json:"state" jsonschema:"open or resolved"`
	Headline     string     `json:"headline"`
	Detail       string     `json:"detail,omitempty"`
	SubjectType  string     `json:"subject_type" jsonschema:"meter, account, reminder or channel"`
	SubjectID    string     `json:"subject_id"`
	OpenedAt     time.Time  `json:"opened_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
	Acknowledged bool       `json:"acknowledged"`
}

func incidentOf(v engine.IncidentView) Incident {
	return Incident{
		ID: v.ID, Kind: v.Kind, Severity: string(v.Severity), State: v.State, Headline: v.Headline, Detail: v.Detail,
		SubjectType: v.SubjectType, SubjectID: v.SubjectID, OpenedAt: v.OpenedAt, ResolvedAt: v.ResolvedAt,
		Acknowledged: v.AckedAt != nil,
	}
}

// Date is a reminder placed on the calendar.
type Date struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Kind     string   `json:"kind" jsonschema:"renewal, birthday, anniversary, holiday, expiry or custom"`
	Next     string   `json:"next,omitempty" jsonschema:"the next occurrence, YYYY-MM-DD; absent when nothing is ahead"`
	Days     *int     `json:"days,omitempty" jsonschema:"days until the next occurrence; 0 is today"`
	Lunar    string   `json:"lunar,omitempty" jsonschema:"the lunar date of the next occurrence, for lunar rules"`
	Ordinal  int      `json:"ordinal,omitempty" jsonschema:"which one this is — the age at a birthday — when the starting year is known"`
	Rule     string   `json:"rule" jsonschema:"the rule in words"`
	Amount   *float64 `json:"amount,omitempty"`
	Currency string   `json:"currency,omitempty"`
	State    string   `json:"state" jsonschema:"upcoming, soon (inside its reminder window), today, snoozed, past or disabled"`
	Notes    string   `json:"notes,omitempty"`
}

func dateOf(v engine.ReminderView) Date {
	out := Date{
		ID: v.ID, Title: v.Title, Kind: v.Kind, Days: v.Days, Ordinal: v.Ordinal, Rule: ruleInWords(v.Rule),
		Amount: v.Amount, Currency: v.Currency, State: v.State, Notes: v.Notes,
	}
	if v.Next != nil {
		out.Next = v.Next.String()
	}
	if v.Lunar != nil {
		out.Lunar = v.Lunar.Label
	}
	return out
}

func ruleInWords(r recur.Rule) string {
	every := func(unit string) string {
		if r.Interval > 1 {
			return fmt.Sprintf("every %d %ss", r.Interval, unit)
		}
		return "every " + unit
	}
	lunar := ""
	if r.Calendar == recur.LunarCal {
		lunar = "lunar "
	}
	switch r.Freq {
	case recur.Once:
		return "once on " + r.Date.String()
	case recur.Weekly:
		days := []string{"", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
		weekday := r.Weekday
		if weekday == 0 && r.Date != nil {
			weekday = r.Date.Weekday()
		}
		return every("week") + " on " + days[weekday]
	case recur.Monthly:
		return fmt.Sprintf("%s on %sday %d", every("month"), lunar, r.Day)
	default:
		return fmt.Sprintf("%s on %smonth %d day %d", every("year"), lunar, r.Month, r.Day)
	}
}

// Meter is a meter with its forecast, flattened for reading.
type Meter struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Account  string   `json:"account,omitempty" jsonschema:"the account it is read from; absent for a meter that is reported from outside"`
	Provider string   `json:"provider,omitempty"`
	Kind     string   `json:"kind" jsonschema:"balance (spent down and topped up), quota (an allowance that resets) or counter (a running total of spend)"`
	Unit     string   `json:"unit" jsonschema:"a currency code, % or a label such as calls"`
	Value    *float64 `json:"value,omitempty" jsonschema:"the current value: what is left of a balance or quota, the total so far of a counter"`
	Limit    *float64 `json:"limit,omitempty" jsonschema:"the size of a quota"`
	Status   string   `json:"status" jsonschema:"ok, warning, critical, failing (its account cannot be read; the value is the last one seen), pending (never read) or paused"`
	Headroom string   `json:"headroom" jsonschema:"ok, low, exhausted or unknown: whether it is safe to lean on this meter now"`
	Summary  string   `json:"summary" jsonschema:"the state in one sentence"`

	RemainingPct *float64   `json:"remaining_pct,omitempty" jsonschema:"share of a quota that is left"`
	ResetsAt     *time.Time `json:"resets_at,omitempty" jsonschema:"when a quota resets"`
	ExhaustsAt   *time.Time `json:"exhausts_at,omitempty" jsonschema:"when a quota runs out at the current pace, if that is before it resets"`
	BurnPerDay   *float64   `json:"burn_per_day,omitempty" jsonschema:"expected spend per day, weighted towards recent days"`
	RunwayDays   *float64   `json:"runway_days,omitempty" jsonschema:"days a balance lasts at the expected burn; absent when nothing is being spent"`
	DepletesAt   *time.Time `json:"depletes_at,omitempty"`
	MonthSpend   *float64   `json:"month_spend,omitempty" jsonschema:"spent so far this calendar month"`
	Confidence   string     `json:"forecast_confidence,omitempty" jsonschema:"none, low, medium or high: how much history the forecast rests on"`
	SampledAt    *time.Time `json:"sampled_at,omitempty"`
}

func meterOf(v engine.MeterView, l i18n.Lang, now time.Time) Meter {
	out := Meter{
		ID: v.ID, Name: v.Display, Kind: string(v.Kind), Unit: v.Unit, Value: v.Value, Limit: v.Limit,
		Status: v.Status, Headroom: "unknown", SampledAt: v.SampledAt,
	}
	if v.Account != nil {
		out.Account, out.Provider = v.Account.Name, v.Account.Provider
	}
	if v.Value == nil {
		out.Summary = "No reading yet."
		return out
	}
	amount := i18n.Amount(*v.Value, v.Unit)
	switch {
	case v.Quota != nil:
		q := v.Quota
		out.RemainingPct, out.ResetsAt, out.ExhaustsAt = &q.RemainingPct, v.ResetsAt, q.ExhaustsAt
		out.Headroom = "ok"
		switch {
		case q.RemainingPct <= 0:
			out.Headroom = "exhausted"
		case q.RemainingPct <= 10 || q.ExhaustsAt != nil:
			out.Headroom = "low"
		}
		out.Summary = amount + " left"
		if v.Limit != nil && v.Unit != "%" {
			out.Summary += " of " + i18n.Amount(*v.Limit, v.Unit)
		}
		if v.ResetsAt != nil && v.ResetsAt.After(now) {
			out.Summary += ", resets in " + l.Span(v.ResetsAt.Sub(now))
		}
		out.Summary += "."
	case v.Forecast != nil && v.Kind == core.Balance:
		fc := v.Forecast
		out.Confidence, out.RunwayDays, out.DepletesAt = fc.Confidence, fc.RunwayDays, fc.DepletesAt
		if fc.Confidence != "none" {
			out.BurnPerDay = &fc.Burn
		}
		out.Headroom = "ok"
		switch {
		case *v.Value <= 0:
			out.Headroom = "exhausted"
		case v.Status == "critical" || v.Status == "warning":
			out.Headroom = "low"
		}
		out.Summary = amount + " left"
		if fc.RunwayDays != nil {
			out.Summary += ", about " + l.Days(*fc.RunwayDays) + " at the recent pace"
		}
		out.Summary += "."
	default:
		out.Summary = amount + " so far."
		if v.Forecast != nil && v.Forecast.Confidence != "none" {
			out.Confidence, out.BurnPerDay = v.Forecast.Confidence, &v.Forecast.Burn
		}
	}
	if v.Month != nil && v.Kind != core.Quota {
		out.MonthSpend = &v.Month.Spend
	}
	// A meter that cannot be read right now is not known to be fine.
	if v.Status == "failing" || v.Status == "paused" {
		out.Headroom = "unknown"
		out.Summary += " Its account cannot be read right now; this is the last value seen."
	}
	return out
}

// ---------- read tools ----------

type attentionIn struct {
	Days int `json:"days,omitempty" jsonschema:"how many days ahead to look for dates, 1-366; the user's setting when omitted"`
}

type attentionOut struct {
	Context
	Status   string     `json:"status" jsonschema:"ok, warning or critical: the worst thing that is open"`
	Summary  string     `json:"summary" jsonschema:"the situation in one sentence"`
	Open     []Incident `json:"open"`
	Upcoming []Date     `json:"upcoming"`
}

func (t *tools) attention(ctx context.Context, in attentionIn) (attentionOut, error) {
	if in.Days < 0 || in.Days > 366 {
		return attentionOut{}, &engine.InvalidError{Reason: "days must be between 1 and 366"}
	}
	overview, err := t.e.Overview(ctx, "en")
	if err != nil {
		return attentionOut{}, err
	}
	out := attentionOut{
		Context: Context{Now: overview.Now, Timezone: overview.Timezone}, Status: overview.Status,
		Open: []Incident{}, Upcoming: []Date{},
	}
	for _, inc := range overview.Attention {
		// A date that is merely near is "upcoming", not a problem.
		if inc.Kind == core.IncidentDue {
			continue
		}
		out.Open = append(out.Open, incidentOf(inc))
	}
	dates := overview.Upcoming
	if in.Days > 0 {
		if dates, err = t.datesWithin(ctx, in.Days); err != nil {
			return out, err
		}
	}
	for _, d := range dates {
		out.Upcoming = append(out.Upcoming, dateOf(d))
	}
	switch {
	case len(out.Open) == 0 && len(out.Upcoming) == 0:
		out.Summary = "Nothing needs attention."
	case len(out.Open) == 0:
		out.Summary = fmt.Sprintf("Nothing is wrong. Next date: %s, %s.", out.Upcoming[0].Title, i18n.Of("en").Until(*out.Upcoming[0].Days))
	default:
		out.Summary = fmt.Sprintf("%d open: %s", len(out.Open), out.Open[0].Headline)
		if len(out.Open) > 1 {
			out.Summary += fmt.Sprintf(" (and %d more)", len(out.Open)-1)
		}
		out.Summary += "."
	}
	return out, nil
}

func (t *tools) datesWithin(ctx context.Context, days int) ([]engine.ReminderView, error) {
	all, err := t.e.Reminders(ctx, "en")
	if err != nil {
		return nil, err
	}
	var out []engine.ReminderView
	for _, r := range all {
		if r.Days != nil && *r.Days <= days && r.State != "disabled" {
			out = append(out, r)
		}
	}
	return out, nil
}

type listMetersIn struct {
	Query string `json:"query,omitempty" jsonschema:"only meters whose name, account or provider contains this text"`
	Kind  string `json:"kind,omitempty" jsonschema:"only this kind: balance, quota or counter"`
}

type listMetersOut struct {
	Context
	Meters []Meter `json:"meters"`
}

func (t *tools) listMeters(ctx context.Context, in listMetersIn) (listMetersOut, error) {
	if in.Kind != "" && !core.Kind(in.Kind).Valid() {
		return listMetersOut{}, &engine.InvalidError{Reason: "kind must be balance, quota or counter"}
	}
	views, settings, err := t.meters(ctx)
	if err != nil {
		return listMetersOut{}, err
	}
	now := t.e.Now()
	out := listMetersOut{Context: Context{Now: now.UTC(), Timezone: settings.Timezone}, Meters: []Meter{}}
	query := strings.ToLower(strings.TrimSpace(in.Query))
	for _, v := range views {
		if v.Hidden || (in.Kind != "" && string(v.Kind) != in.Kind) {
			continue
		}
		m := meterOf(v, i18n.Of("en"), now)
		if query != "" && !strings.Contains(strings.ToLower(m.Name+" "+m.Account+" "+m.Provider), query) {
			continue
		}
		out.Meters = append(out.Meters, m)
	}
	return out, nil
}

func (t *tools) meters(ctx context.Context) ([]engine.MeterView, core.Settings, error) {
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return nil, settings, err
	}
	views, err := t.e.Meters(ctx, "en")
	return views, settings, err
}

// findMeter resolves an id, or a name that matches exactly one meter.
func (t *tools) findMeter(ctx context.Context, ref string) (engine.MeterView, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return engine.MeterView{}, &engine.InvalidError{Reason: "meter is required: an id or a name from list_meters"}
	}
	views, _, err := t.meters(ctx)
	if err != nil {
		return engine.MeterView{}, err
	}
	var matches []engine.MeterView
	for _, v := range views {
		if v.ID == ref {
			return v, nil
		}
		if strings.EqualFold(v.Display, ref) || strings.EqualFold(v.Name, ref) || strings.EqualFold(v.Key, ref) {
			matches = append(matches, v)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return engine.MeterView{}, &engine.InvalidError{Reason: fmt.Sprintf("no meter is called %q; see list_meters", ref)}
	}
	return engine.MeterView{}, &engine.InvalidError{Reason: fmt.Sprintf("%d meters match %q; use the id", len(matches), ref)}
}

type getMeterIn struct {
	Meter string `json:"meter" jsonschema:"the meter's id, or its name as shown in list_meters"`
	Days  int    `json:"days,omitempty" jsonschema:"how many days of daily spend to include, 1-90 (default 14)"`
}

type dailySpend struct {
	Date  string  `json:"date"`
	Spend float64 `json:"spend"`
	TopUp float64 `json:"top_up,omitempty"`
}

type getMeterOut struct {
	Context
	Meter     Meter        `json:"meter"`
	Rules     core.Rules   `json:"rules" jsonschema:"the conditions that open an incident for this meter; absent ones use the defaults"`
	Incidents []Incident   `json:"incidents" jsonschema:"what is open on this meter"`
	Daily     []dailySpend `json:"daily" jsonschema:"spend per day in the user's time zone, oldest first; empty for a quota"`
}

func (t *tools) getMeter(ctx context.Context, in getMeterIn) (getMeterOut, error) {
	if in.Days < 0 || in.Days > 90 {
		return getMeterOut{}, &engine.InvalidError{Reason: "days must be between 1 and 90"}
	}
	view, err := t.findMeter(ctx, in.Meter)
	if err != nil {
		return getMeterOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return getMeterOut{}, err
	}
	now := t.e.Now()
	out := getMeterOut{
		Context: Context{Now: now.UTC(), Timezone: settings.Timezone},
		Meter:   meterOf(view, i18n.Of("en"), now), Rules: view.Rules, Incidents: []Incident{}, Daily: []dailySpend{},
	}
	for _, inc := range view.Incidents {
		out.Incidents = append(out.Incidents, incidentOf(inc))
	}
	if view.Kind == core.Quota {
		return out, nil
	}
	rangeName := "30d"
	if in.Days > 30 {
		rangeName = "90d"
	}
	series, err := t.e.Series(ctx, view.ID, rangeName, "en")
	if err != nil {
		return out, err
	}
	days := in.Days
	if days == 0 {
		days = 14
	}
	daily := series.Daily
	if len(daily) > days {
		daily = daily[len(daily)-days:]
	}
	for _, d := range daily {
		out.Daily = append(out.Daily, dailySpend{Date: d.Date.String(), Spend: round2(d.Spend), TopUp: round2(d.TopUp)})
	}
	return out, nil
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5*sign(v))) / 100
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

type listDatesIn struct {
	Days  int    `json:"days,omitempty" jsonschema:"only dates within this many days, 1-3660 (default 90)"`
	Query string `json:"query,omitempty" jsonschema:"only dates whose title contains this text"`
}

type listDatesOut struct {
	Context
	Today string `json:"today" jsonschema:"today's date in the user's time zone"`
	Dates []Date `json:"dates"`
}

func (t *tools) listDates(ctx context.Context, in listDatesIn) (listDatesOut, error) {
	if in.Days < 0 || in.Days > 3660 {
		return listDatesOut{}, &engine.InvalidError{Reason: "days must be between 1 and 3660"}
	}
	days := in.Days
	if days == 0 {
		days = 90
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return listDatesOut{}, err
	}
	views, err := t.datesWithin(ctx, days)
	if err != nil {
		return listDatesOut{}, err
	}
	now := t.e.Now()
	out := listDatesOut{
		Context: Context{Now: now.UTC(), Timezone: settings.Timezone},
		Today:   recur.DateOf(now.In(settings.Location())).String(), Dates: []Date{},
	}
	query := strings.ToLower(strings.TrimSpace(in.Query))
	for _, v := range views {
		if query == "" || strings.Contains(strings.ToLower(v.Title), query) {
			out.Dates = append(out.Dates, dateOf(v))
		}
	}
	return out, nil
}

type getSpendIn struct {
	Month string `json:"month,omitempty" jsonschema:"a calendar month as YYYY-MM; the current month when omitted"`
}

type spendRow struct {
	Meter     string   `json:"meter"`
	Unit      string   `json:"unit"`
	Spend     float64  `json:"spend" jsonschema:"in the meter's own unit"`
	Projected float64  `json:"projected" jsonschema:"where the month ends up at the expected burn"`
	Budget    *float64 `json:"budget,omitempty"`
	Base      *float64 `json:"spend_in_base,omitempty" jsonschema:"the spend in the base currency; absent when the unit is not a currency or has no rate"`
}

type renewalRow struct {
	Title    string   `json:"title"`
	Date     string   `json:"date"`
	Amount   float64  `json:"amount"`
	Currency string   `json:"currency"`
	Base     *float64 `json:"amount_in_base,omitempty"`
}

type getSpendOut struct {
	Context
	Month       string       `json:"month"`
	Base        string       `json:"base_currency"`
	Total       float64      `json:"total" jsonschema:"metered spend so far, in the base currency"`
	Projected   float64      `json:"projected" jsonschema:"metered spend expected by the end of the month"`
	Renewals    float64      `json:"renewals_total" jsonschema:"sum of the dated renewals that fall in the month, in the base currency"`
	Rows        []spendRow   `json:"meters"`
	RenewalRows []renewalRow `json:"renewals"`
	Unconverted []string     `json:"unconverted" jsonschema:"currencies with no exchange rate, which are therefore not in the totals"`
}

func (t *tools) getSpend(ctx context.Context, in getSpendIn) (getSpendOut, error) {
	spend, err := t.e.Spend(ctx, strings.TrimSpace(in.Month), "en")
	if err != nil {
		return getSpendOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return getSpendOut{}, err
	}
	out := getSpendOut{
		Context: Context{Now: t.e.Now().UTC(), Timezone: settings.Timezone},
		Month:   spend.Month, Base: spend.Base, Total: round2(spend.Total), Projected: round2(spend.Projected),
		Renewals: round2(spend.Fixed), Rows: []spendRow{}, RenewalRows: []renewalRow{}, Unconverted: spend.Unconverted,
	}
	for _, row := range spend.Rows {
		out.Rows = append(out.Rows, spendRow{
			Meter: row.Display, Unit: row.Unit, Spend: round2(row.Spend), Projected: round2(row.Projected),
			Budget: row.Budget, Base: rounded(row.Base),
		})
	}
	for _, r := range spend.Renewals {
		out.RenewalRows = append(out.RenewalRows, renewalRow{
			Title: r.Title, Date: r.Date.String(), Amount: r.Amount, Currency: r.Currency, Base: rounded(r.Base),
		})
	}
	return out, nil
}

func rounded(v *float64) *float64 {
	if v == nil {
		return nil
	}
	r := round2(*v)
	return &r
}

type listActivityIn struct {
	State string `json:"state,omitempty" jsonschema:"open or resolved; both when omitted"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many to return, 1-100 (default 20)"`
}

type listActivityOut struct {
	Context
	Incidents []Incident `json:"incidents"`
}

func (t *tools) listActivity(ctx context.Context, in listActivityIn) (listActivityOut, error) {
	if in.State != "" && in.State != core.StateOpen && in.State != core.StateResolved {
		return listActivityOut{}, &engine.InvalidError{Reason: "state must be open or resolved"}
	}
	if in.Limit < 0 || in.Limit > 100 {
		return listActivityOut{}, &engine.InvalidError{Reason: "limit must be between 1 and 100"}
	}
	limit := in.Limit
	if limit == 0 {
		limit = 20
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return listActivityOut{}, err
	}
	views, err := t.e.Incidents(ctx, store.IncidentFilter{State: in.State, Limit: limit}, "en")
	if err != nil {
		return listActivityOut{}, err
	}
	out := listActivityOut{Context: Context{Now: t.e.Now().UTC(), Timezone: settings.Timezone}, Incidents: []Incident{}}
	for _, v := range views {
		out.Incidents = append(out.Incidents, incidentOf(v))
	}
	return out, nil
}

// ---------- tools that change something ----------

type addDateIn struct {
	Title    string   `json:"title" jsonschema:"what the date is, e.g. a person's name or a subscription"`
	Kind     string   `json:"kind,omitempty" jsonschema:"renewal, birthday, anniversary, holiday, expiry or custom (the default)"`
	Freq     string   `json:"freq" jsonschema:"once, weekly, monthly or yearly"`
	Calendar string   `json:"calendar,omitempty" jsonschema:"solar (the default) or lunar; lunar works with monthly and yearly"`
	Date     string   `json:"date,omitempty" jsonschema:"for once: the day, as YYYY-MM-DD. With interval above 1: the day to count from"`
	Weekday  int      `json:"weekday,omitempty" jsonschema:"for weekly: 1 is Monday, 7 is Sunday"`
	Month    int      `json:"month,omitempty" jsonschema:"for yearly: the month, 1-12 (a lunar month when calendar is lunar)"`
	Day      int      `json:"day,omitempty" jsonschema:"for monthly and yearly: the day of the month"`
	Interval int      `json:"interval,omitempty" jsonschema:"every N weeks, months or years; needs date to count from"`
	Since    int      `json:"since,omitempty" jsonschema:"the year it started — a birth year — so reminders can say which one it is"`
	LeadDays []int    `json:"lead_days,omitempty" jsonschema:"days before the date on which to remind; 0 is the day itself. Default [0]"`
	Amount   float64  `json:"amount,omitempty" jsonschema:"what a renewal costs"`
	Currency string   `json:"currency,omitempty" jsonschema:"three-letter code for amount; the base currency when omitted"`
	Notes    string   `json:"notes,omitempty"`
	_        struct{} `json:"-"`
}

type dateOut struct {
	Context
	Date Date `json:"date"`
}

func (t *tools) addDate(ctx context.Context, in addDateIn) (dateOut, error) {
	rule := recur.Rule{
		Freq: in.Freq, Calendar: in.Calendar, Weekday: in.Weekday, Month: in.Month, Day: in.Day,
		Interval: in.Interval, Since: in.Since,
	}
	if in.Date != "" {
		day, err := recur.ParseDate(in.Date)
		if err != nil {
			return dateOut{}, &engine.InvalidError{Reason: err.Error()}
		}
		rule.Date = &day
	}
	input := engine.ReminderInput{Title: &in.Title, Rule: &rule}
	if in.Kind != "" {
		input.Kind = &in.Kind
	}
	if in.LeadDays != nil {
		input.LeadDays = &in.LeadDays
	}
	if in.Amount != 0 {
		input.Amount = &in.Amount
	}
	if in.Currency != "" {
		input.Currency = &in.Currency
	}
	if in.Notes != "" {
		input.Notes = &in.Notes
	}
	view, err := t.e.CreateReminder(ctx, input, "en")
	if err != nil {
		return dateOut{}, err
	}
	return t.dateResult(ctx, view)
}

func (t *tools) dateResult(ctx context.Context, view engine.ReminderView) (dateOut, error) {
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return dateOut{}, err
	}
	return dateOut{Context: Context{Now: t.e.Now().UTC(), Timezone: settings.Timezone}, Date: dateOf(view)}, nil
}

type completeDateIn struct {
	Date string `json:"date" jsonschema:"the date's id, or its title as shown in list_dates"`
}

func (t *tools) completeDate(ctx context.Context, in completeDateIn) (dateOut, error) {
	ref := strings.TrimSpace(in.Date)
	if ref == "" {
		return dateOut{}, &engine.InvalidError{Reason: "date is required: an id or a title from list_dates"}
	}
	all, err := t.e.Reminders(ctx, "en")
	if err != nil {
		return dateOut{}, err
	}
	var matches []engine.ReminderView
	for _, r := range all {
		if r.ID == ref {
			matches = []engine.ReminderView{r}
			break
		}
		if strings.EqualFold(r.Title, ref) {
			matches = append(matches, r)
		}
	}
	switch len(matches) {
	case 0:
		return dateOut{}, &engine.InvalidError{Reason: fmt.Sprintf("no date is called %q; see list_dates", ref)}
	case 1:
	default:
		return dateOut{}, &engine.InvalidError{Reason: fmt.Sprintf("%d dates are called %q; use the id", len(matches), ref)}
	}
	view, err := t.e.CompleteReminder(ctx, matches[0].ID, "en")
	if err != nil {
		return dateOut{}, err
	}
	return t.dateResult(ctx, view)
}

type acknowledgeIn struct {
	IncidentID string `json:"incident_id" jsonschema:"the id of an open incident, from attention or list_activity"`
}

type acknowledgeOut struct {
	Acknowledged bool   `json:"acknowledged"`
	Headline     string `json:"headline"`
}

func (t *tools) acknowledge(ctx context.Context, in acknowledgeIn) (acknowledgeOut, error) {
	inc, err := t.e.Ack(ctx, strings.TrimSpace(in.IncidentID), true)
	if err != nil {
		return acknowledgeOut{}, err
	}
	return acknowledgeOut{Acknowledged: true, Headline: inc.Title}, nil
}

type sampleNowIn struct {
	Account string `json:"account,omitempty" jsonschema:"an account's id or name; every account when omitted"`
}

type sampleNowOut struct {
	Requested string `json:"requested" jsonschema:"what will be read"`
}

func (t *tools) sampleNow(ctx context.Context, in sampleNowIn) (sampleNowOut, error) {
	ref := strings.TrimSpace(in.Account)
	if ref == "" {
		return sampleNowOut{Requested: "every account"}, t.e.RequestSample(ctx, "")
	}
	accounts, err := t.e.Accounts(ctx, "en")
	if err != nil {
		return sampleNowOut{}, err
	}
	for _, a := range accounts {
		if a.ID == ref || strings.EqualFold(a.Name, ref) {
			return sampleNowOut{Requested: a.Name}, t.e.RequestSample(ctx, a.ID)
		}
	}
	return sampleNowOut{}, &engine.InvalidError{Reason: fmt.Sprintf("no account is called %q", ref)}
}

type recordReadingIn struct {
	Meter    string   `json:"meter" jsonschema:"a short key for the meter: lowercase letters, digits, dots, dashes, underscores"`
	Value    float64  `json:"value" jsonschema:"the current value"`
	Kind     string   `json:"kind,omitempty" jsonschema:"balance, quota or counter; required the first time a meter is reported"`
	Unit     string   `json:"unit,omitempty" jsonschema:"a currency code, % or a label"`
	Name     string   `json:"name,omitempty" jsonschema:"a display name for a new meter"`
	Limit    *float64 `json:"limit,omitempty" jsonschema:"the size of a quota"`
	ResetsAt string   `json:"resets_at,omitempty" jsonschema:"when a quota resets, RFC 3339"`
}

type recordReadingOut struct {
	Meter Meter `json:"meter"`
}

func (t *tools) recordReading(ctx context.Context, in recordReadingIn) (recordReadingOut, error) {
	push := engine.Push{Meter: in.Meter, Name: in.Name, Kind: core.Kind(in.Kind), Unit: in.Unit, Value: &in.Value, Limit: in.Limit}
	if in.ResetsAt != "" {
		at, err := time.Parse(time.RFC3339, in.ResetsAt)
		if err != nil {
			return recordReadingOut{}, &engine.InvalidError{Reason: "resets_at must be an RFC 3339 time"}
		}
		push.ResetsAt = &at
	}
	meters, err := t.e.Ingest(ctx, []engine.Push{push})
	if err != nil {
		return recordReadingOut{}, err
	}
	view, err := t.e.MeterView(ctx, meters[0].ID, "en")
	if err != nil {
		return recordReadingOut{}, err
	}
	return recordReadingOut{Meter: meterOf(view, i18n.Of("en"), t.e.Now())}, nil
}

package engine

import (
	"context"
	"sort"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/forecast"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/recur"
	"github.com/itswl/quotapulse/internal/store"
)

// The views are what the dashboard and the MCP tools read. They are
// computed on demand from the stored facts: nothing here is cached state
// that could disagree with the database.

// Display statuses of a meter.
const (
	statusOK       = "ok"
	statusWarning  = "warning"
	statusCritical = "critical"
	statusFailing  = "failing" // its account cannot be read; the value is the last one seen
	statusPending  = "pending" // no reading yet
	statusPaused   = "paused"  // its account is disabled
)

// Point is one sample on a chart.
type Point struct {
	T time.Time `json:"t"`
	V float64   `json:"v"`
}

// AccountRef is the part of an account a meter needs to show.
type AccountRef struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	ProviderName string `json:"provider_name"`
	Group        string `json:"group"`
	Status       string `json:"status"`
	LastError    string `json:"last_error"`
}

// EffectiveRules are a meter's rules with the defaults filled in.
type EffectiveRules struct {
	RunwayDays float64 `json:"runway_days"`
	Anomaly    bool    `json:"anomaly"`
}

// MeterView is a meter with everything derived from its history.
type MeterView struct {
	core.Meter
	// Display is the name to show: the account's when that says it all.
	Display string      `json:"display"`
	Status  string      `json:"status"`
	Muted   bool        `json:"muted"`
	Account *AccountRef `json:"account"`

	Forecast  *forecast.Balance `json:"forecast"`
	Quota     *forecast.Quota   `json:"quota"`
	Month     *forecast.Month   `json:"month"`
	Effective EffectiveRules    `json:"effective"`

	Incidents []IncidentView `json:"incidents"`
	// Spark is the last two weeks, thinned for a small chart.
	Spark []Point `json:"spark"`
}

// AccountView is an account as the dashboard shows it. The credential is
// masked: secret fields never leave the server.
type AccountView struct {
	core.Account
	ProviderName      string            `json:"provider_name"`
	Credential        map[string]string `json:"credential"`
	EffectiveInterval int               `json:"effective_interval_seconds"`
	MeterIDs          []string          `json:"meter_ids"`
	Incident          *IncidentView     `json:"incident"`
}

// IncidentView is an incident with its story told in words.
type IncidentView struct {
	core.Incident
	Headline string        `json:"headline"`
	Detail   string        `json:"detail"`
	Facts    []notify.Fact `json:"facts"`
	Path     string        `json:"path"`
}

// Reminder states for display.
const (
	reminderUpcoming = "upcoming"
	reminderSoon     = "soon" // inside its lead window
	reminderToday    = "today"
	reminderSnoozed  = "snoozed"
	reminderPast     = "past" // a one-off whose day has gone, or was dealt with
	reminderDisabled = "disabled"
)

// ReminderView is a reminder placed on the calendar.
type ReminderView struct {
	core.Reminder
	Next *recur.Date `json:"next"`
	// Days until Next; 0 is today.
	Days    *int         `json:"days"`
	Lunar   *recur.Lunar `json:"lunar"`
	Ordinal int          `json:"ordinal"`
	State   string       `json:"state"`
	// Handled is the occurrence, today or later, that was marked done and
	// is therefore being skipped; nil when nothing is.
	Handled *recur.Date `json:"handled"`
	// Following are the occurrences after Next, for a preview of the rule.
	Following []recur.Date `json:"following"`
	// MonthlyAmount spreads the amount over a month, so a yearly plan and a
	// weekly one can be compared.
	MonthlyAmount *float64 `json:"monthly_amount"`
}

// SpendSummary is the month in one line.
type SpendSummary struct {
	Month string `json:"month"`
	Base  string `json:"base"`
	// Total and Projected cover metered spend, converted to the base currency.
	Total     float64 `json:"total"`
	Projected float64 `json:"projected"`
	// Fixed is the sum of renewals that fall in the month.
	Fixed float64 `json:"fixed"`
	// Unconverted lists currencies that have no rate and so are not in the totals.
	Unconverted []string `json:"unconverted"`
}

// Counts says how much is configured; the dashboard uses it to decide what
// to suggest on an empty screen.
type Counts struct {
	Accounts  int `json:"accounts"`
	Meters    int `json:"meters"`
	Reminders int `json:"reminders"`
	Channels  int `json:"channels"`
}

// Overview is the whole picture on one screen.
type Overview struct {
	Now      time.Time `json:"now"`
	Timezone string    `json:"timezone"`
	// Status is the worst thing that is open: ok, warning or critical.
	Status    string         `json:"status"`
	Attention []IncidentView `json:"attention"`
	Meters    []MeterView    `json:"meters"`
	Accounts  []AccountView  `json:"accounts"`
	Upcoming  []ReminderView `json:"upcoming"`
	Spend     SpendSummary   `json:"spend"`
	Counts    Counts         `json:"counts"`
	// Muted is true while notifications are recorded but not sent.
	Muted bool `json:"muted"`
}

// viewer carries what every view needs: a moment, the settings, a language.
type viewer struct {
	e   *Engine
	s   core.Settings
	now time.Time
	loc *time.Location
}

// view prepares a viewer. lang overrides the notification language, so the
// dashboard can read incidents in the language it is being used in.
func (e *Engine) view(ctx context.Context, lang string) (viewer, error) {
	s, err := e.store.Settings(ctx)
	if err != nil {
		return viewer{}, err
	}
	if lang == "zh" || lang == "en" {
		s.Language = lang
	}
	return viewer{e: e, s: s, now: e.now(), loc: s.Location()}, nil
}

func (v viewer) incident(inc core.Incident) IncidentView {
	// An incident carried over from before 1.0 has no numbers to build a
	// sentence from, only what was said at the time. Show that.
	if said, ok := inc.Data[importedKey].(string); ok && said != "" {
		return IncidentView{
			Incident: inc, Headline: importedHeadline(inc, v.s.Language), Detail: said,
			Facts: []notify.Fact{}, Path: pathOf(inc),
		}
	}
	// History reads better as what happened than as how it ended, so a
	// resolved incident is told the way it was when it opened.
	msg := v.e.compose(inc, core.EventOpened, v.s)
	facts := msg.Facts
	if facts == nil {
		facts = []notify.Fact{}
	}
	// The sentence above is written from the exact measurements; what is
	// handed out next to it is rounded like every other number shown.
	inc.Data = tidyData(inc.Data)
	return IncidentView{Incident: inc, Headline: msg.Title, Detail: msg.Body, Facts: facts, Path: pathOf(inc)}
}

// tidyData returns an incident's measurements rounded for a reader: numbers
// to six decimals, times to the second. The stored incident keeps them exact.
func tidyData(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	out := make(map[string]any, len(data))
	for key, value := range data {
		out[key] = tidyValue(value)
	}
	return out
}

func tidyValue(value any) any {
	switch value := value.(type) {
	case float64:
		return forecast.Round(value)
	case string:
		// Times arrive as text after their trip through the database.
		if len(value) > 20 && value[4] == '-' && value[10] == 'T' {
			if at, err := time.Parse(time.RFC3339Nano, value); err == nil {
				return at.Truncate(time.Second).Format(time.RFC3339)
			}
		}
		return value
	case map[string]any:
		return tidyData(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = tidyValue(item)
		}
		return out
	}
	return value
}

// Overview assembles the overview.
func (e *Engine) Overview(ctx context.Context, lang ...string) (Overview, error) {
	v, err := e.view(ctx, first(lang))
	if err != nil {
		return Overview{}, err
	}
	out := Overview{Now: v.now.UTC().Truncate(time.Second), Timezone: v.s.Timezone, Status: statusOK, Muted: e.mute}

	open, err := e.store.OpenIncidents(ctx)
	if err != nil {
		return out, err
	}
	sort.SliceStable(open, func(i, j int) bool {
		if a, b := open[i].Severity.Rank(), open[j].Severity.Rank(); a != b {
			return a > b
		}
		return open[i].OpenedAt.After(open[j].OpenedAt)
	})
	out.Attention = make([]IncidentView, 0, len(open))
	for _, inc := range open {
		out.Attention = append(out.Attention, v.incident(inc))
		switch {
		case inc.Severity == core.Critical:
			out.Status = statusCritical
		case inc.Severity == core.Warning && out.Status != statusCritical:
			out.Status = statusWarning
		}
	}

	if out.Accounts, err = v.accounts(ctx, open); err != nil {
		return out, err
	}
	if out.Meters, err = v.meters(ctx, open); err != nil {
		return out, err
	}
	reminders, err := v.reminders(ctx)
	if err != nil {
		return out, err
	}
	out.Upcoming = []ReminderView{}
	for _, r := range reminders {
		if r.Days != nil && *r.Days <= v.s.UpcomingDays && r.State != reminderDisabled {
			out.Upcoming = append(out.Upcoming, r)
		}
	}
	spend, err := v.spend(ctx, "", out.Meters)
	if err != nil {
		return out, err
	}
	out.Spend = SpendSummary{
		Month: spend.Month, Base: spend.Base, Total: spend.Total, Projected: spend.Projected,
		Fixed: spend.Fixed, Unconverted: spend.Unconverted,
	}
	channels, err := e.store.Channels(ctx)
	if err != nil {
		return out, err
	}
	out.Counts = Counts{Accounts: len(out.Accounts), Meters: len(out.Meters), Reminders: len(reminders), Channels: len(channels)}
	return out, nil
}

func first(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

// providerNames maps provider IDs to the name to show in this language.
func (v viewer) providerNames(ctx context.Context) (map[string]provider.Definition, error) {
	defs, err := v.e.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]provider.Definition, len(defs))
	for _, def := range defs {
		byID[def.ID] = def
	}
	return byID, nil
}

func (v viewer) providerName(defs map[string]provider.Definition, id string) string {
	def, ok := defs[id]
	if !ok {
		return id
	}
	if v.s.Language == "zh" && def.NameZH != "" {
		return def.NameZH
	}
	return def.Name
}

func (v viewer) accounts(ctx context.Context, open []core.Incident) ([]AccountView, error) {
	accounts, err := v.e.store.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	meters, err := v.e.store.Meters(ctx)
	if err != nil {
		return nil, err
	}
	defs, err := v.providerNames(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AccountView, 0, len(accounts))
	for _, a := range accounts {
		view := AccountView{
			Account: a, ProviderName: v.providerName(defs, a.Provider),
			Credential: map[string]string{}, EffectiveInterval: v.s.SampleSeconds, MeterIDs: []string{},
		}
		if a.Interval > 0 {
			view.EffectiveInterval = a.Interval
		}
		if def, ok := defs[a.Provider]; ok {
			view.Credential = def.MaskCredential(a.Credential)
		}
		for _, m := range meters {
			if m.AccountID == a.ID {
				view.MeterIDs = append(view.MeterIDs, m.ID)
			}
		}
		for _, inc := range open {
			if inc.SubjectType == core.SubjectAccount && inc.SubjectID == a.ID {
				iv := v.incident(inc)
				view.Incident = &iv
			}
		}
		out = append(out, view)
	}
	return out, nil
}

// Accounts lists the accounts.
func (e *Engine) Accounts(ctx context.Context, lang string) ([]AccountView, error) {
	v, err := e.view(ctx, lang)
	if err != nil {
		return nil, err
	}
	open, err := e.store.OpenIncidents(ctx)
	if err != nil {
		return nil, err
	}
	return v.accounts(ctx, open)
}

// AccountView loads one account.
func (e *Engine) AccountView(ctx context.Context, id, lang string) (AccountView, error) {
	accounts, err := e.Accounts(ctx, lang)
	if err != nil {
		return AccountView{}, err
	}
	for _, a := range accounts {
		if a.ID == id {
			return a, nil
		}
	}
	return AccountView{}, ErrNotFound
}

func (v viewer) meters(ctx context.Context, open []core.Incident) ([]MeterView, error) {
	meters, err := v.e.store.Meters(ctx)
	if err != nil {
		return nil, err
	}
	accounts, err := v.e.store.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	defs, err := v.providerNames(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]core.Account{}
	for _, a := range accounts {
		byID[a.ID] = a
	}
	siblings := map[string]int{}
	for _, m := range meters {
		if !m.Hidden {
			siblings[m.AccountID]++
		}
	}

	out := make([]MeterView, 0, len(meters))
	for _, m := range meters {
		account, hasAccount := byID[m.AccountID]
		readings, err := v.e.store.Readings(ctx, m.ID, historyStart(v.now, v.loc))
		if err != nil {
			return nil, err
		}
		view := v.meter(m, readings)
		if hasAccount {
			view.Display = displayName(m, account.Name, siblings[m.AccountID])
			view.Account = &AccountRef{
				ID: account.ID, Name: account.Name, Provider: account.Provider,
				ProviderName: v.providerName(defs, account.Provider), Group: account.Group,
				Status: account.Status, LastError: account.LastError,
			}
			switch {
			case !account.Enabled:
				view.Status = statusPaused
			case account.Status == core.StatusFailing && view.Status == statusOK:
				view.Status = statusFailing
			}
		}
		for _, inc := range open {
			if inc.SubjectType != core.SubjectMeter || inc.SubjectID != m.ID {
				continue
			}
			view.Incidents = append(view.Incidents, v.incident(inc))
			switch {
			case inc.Severity == core.Critical:
				view.Status = statusCritical
			case inc.Severity == core.Warning && view.Status != statusCritical:
				view.Status = statusWarning
			}
		}
		out = append(out, view)
	}
	// Whatever needs attention first; then by group, as configured.
	sort.SliceStable(out, func(i, j int) bool {
		return statusRank(out[i].Status) > statusRank(out[j].Status)
	})
	return out, nil
}

func statusRank(status string) int {
	switch status {
	case statusCritical:
		return 3
	case statusWarning:
		return 2
	case statusFailing:
		return 1
	}
	return 0
}

// meter derives everything about one meter from its readings.
func (v viewer) meter(m core.Meter, readings []core.Reading) MeterView {
	view := MeterView{
		Meter: m, Display: m.Name, Status: statusOK, Incidents: []IncidentView{}, Spark: []Point{},
		Muted:     m.MutedUntil != nil && m.MutedUntil.After(v.now),
		Effective: EffectiveRules{RunwayDays: lead(m, v.s), Anomaly: v.s.Anomaly},
	}
	if m.Rules.Anomaly != nil {
		view.Effective.Anomaly = *m.Rules.Anomaly
	}
	if m.Value == nil {
		view.Status = statusPending
		return view
	}
	switch m.Kind {
	case core.Quota:
		q := forecast.ForQuota(*m.Value, m.Limit, m.ResetsAt, m.Window, v.now).Tidy()
		view.Quota = &q
	default:
		fc := forecast.ForBalance(readings, m.Kind)
		var burn *float64
		if fc.Confidence != forecast.ConfidenceNone {
			burn = &fc.Burn
		}
		month := forecast.ForMonth(forecast.Flows(readings, m.Kind), burn, v.now, v.loc).Tidy()
		// Rounded last: the month above was worked out from the exact burn.
		fc = fc.Tidy()
		view.Forecast, view.Month = &fc, &month
	}
	view.Spark = thin(clip(readings, v.now.Add(-14*24*time.Hour)), 48)
	return view
}

// Meters lists every meter with its forecast.
func (e *Engine) Meters(ctx context.Context, lang string) ([]MeterView, error) {
	v, err := e.view(ctx, lang)
	if err != nil {
		return nil, err
	}
	open, err := e.store.OpenIncidents(ctx)
	if err != nil {
		return nil, err
	}
	return v.meters(ctx, open)
}

// MeterView loads one meter with its forecast.
func (e *Engine) MeterView(ctx context.Context, id, lang string) (MeterView, error) {
	meters, err := e.Meters(ctx, lang)
	if err != nil {
		return MeterView{}, err
	}
	for _, m := range meters {
		if m.ID == id {
			return m, nil
		}
	}
	return MeterView{}, ErrNotFound
}

// clip turns readings into chart points from a start time on. The reading
// just before the start is moved onto the start, so the line begins at the
// edge of the chart at the right height.
func clip(readings []core.Reading, from time.Time) []Point {
	points := make([]Point, 0, len(readings))
	for i, r := range readings {
		if r.At.Before(from) {
			if i+1 < len(readings) && !readings[i+1].At.Before(from) {
				points = append(points, Point{T: from.UTC().Truncate(time.Second), V: r.Value})
			}
			continue
		}
		points = append(points, Point{T: r.At.Truncate(time.Second), V: r.Value})
	}
	return points
}

// thin reduces a series to roughly limit points for drawing. Each slice of
// time keeps its first, lowest, highest and last point, so a brief dip or a
// top-up survives however far the chart is zoomed out.
func thin(points []Point, limit int) []Point {
	if len(points) <= limit || limit < 8 {
		return points
	}
	buckets := limit / 4
	start, end := points[0].T, points[len(points)-1].T
	span := end.Sub(start)
	if span <= 0 {
		return points[:1]
	}
	out := make([]Point, 0, limit)
	i := 0
	for b := 0; b < buckets; b++ {
		edge := start.Add(span * time.Duration(b+1) / time.Duration(buckets))
		from := i
		for i < len(points) && (points[i].T.Before(edge) || b == buckets-1) {
			i++
		}
		if i == from {
			continue
		}
		lo, hi := from, from
		for k := from; k < i; k++ {
			if points[k].V < points[lo].V {
				lo = k
			}
			if points[k].V > points[hi].V {
				hi = k
			}
		}
		keep := []int{from, lo, hi, i - 1}
		sort.Ints(keep)
		for n, k := range keep {
			if n > 0 && k == keep[n-1] {
				continue
			}
			out = append(out, points[k])
		}
	}
	return out
}

// Series is a meter's history prepared for a chart.
type Series struct {
	Meter  MeterView        `json:"meter"`
	Range  string           `json:"range"`
	From   time.Time        `json:"from"`
	To     time.Time        `json:"to"`
	Points []Point          `json:"points"`
	Daily  []forecast.Day   `json:"daily"`
	TopUps []forecast.Event `json:"top_ups"`
	// Projection continues the line at the expected burn: from the last
	// reading to where the balance runs out, or to the edge of the chart.
	Projection []Point `json:"projection"`
}

var ranges = map[string]time.Duration{
	"24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour,
	"90d": 90 * 24 * time.Hour, "1y": 365 * 24 * time.Hour,
}

// Series loads a meter's history over a named range.
func (e *Engine) Series(ctx context.Context, id, rangeName, lang string) (Series, error) {
	span, ok := ranges[rangeName]
	if !ok {
		return Series{}, invalid("range must be one of 24h, 7d, 30d, 90d, 1y")
	}
	meter, err := e.MeterView(ctx, id, lang)
	if err != nil {
		return Series{}, err
	}
	v, err := e.view(ctx, lang)
	if err != nil {
		return Series{}, err
	}
	from := v.now.Add(-span)
	readings, err := e.store.Readings(ctx, id, from)
	if err != nil {
		return Series{}, err
	}
	out := Series{
		Meter: meter, Range: rangeName, From: from.UTC().Truncate(time.Second), To: v.now.UTC().Truncate(time.Second),
		Points: thin(clip(readings, from), 400), Daily: []forecast.Day{}, TopUps: []forecast.Event{}, Projection: []Point{},
	}
	flows := forecast.Flows(readings, meter.Kind)
	if meter.Kind != core.Quota {
		// A meter younger than the range has no days before its first
		// reading: zeroes there would read as "nothing was spent".
		since := from
		if len(readings) > 0 && readings[0].At.After(since) {
			since = readings[0].At
		}
		for _, day := range forecast.Daily(flows, since, v.now, v.loc) {
			out.Daily = append(out.Daily, day.Tidy())
		}
		for _, event := range forecast.TopUps(flows) {
			if !event.At.Before(from) && meter.Kind == core.Balance {
				out.TopUps = append(out.TopUps, event.Tidy())
			}
		}
	}
	if fc := meter.Forecast; fc != nil && meter.Kind == core.Balance && fc.Burn > 0 && meter.Value != nil && !fc.AsOf.IsZero() {
		horizon := fc.AsOf.Add(span / 2)
		end := Point{T: horizon.UTC(), V: forecast.Round(max(*meter.Value-fc.Burn*horizon.Sub(fc.AsOf).Hours()/24, 0))}
		if fc.DepletesAt != nil && fc.DepletesAt.Before(horizon) {
			end = Point{T: fc.DepletesAt.UTC(), V: 0}
		}
		out.Projection = []Point{{T: fc.AsOf, V: *meter.Value}, end}
	}
	return out, nil
}

// ---------- reminders ----------

func (v viewer) reminder(r core.Reminder) ReminderView {
	today := dateOf(v.now.In(v.loc))
	view := ReminderView{Reminder: r, State: reminderUpcoming, Following: []recur.Date{}}
	if r.LeadDays == nil {
		view.LeadDays = []int{}
	}
	if r.Amount != nil && *r.Amount > 0 {
		view.MonthlyAmount = monthlyAmount(r)
	}
	occ, ok := current(r, today)
	if !ok {
		view.State = reminderPast
		if !r.Enabled {
			view.State = reminderDisabled
		}
		return view
	}
	view.Next, view.Days, view.Ordinal = &occ.Date, &occ.Days, r.Rule.Ordinal(occ.Date)
	if r.Rule.Calendar == recur.LunarCal {
		lunar := recur.LunarOf(occ.Date)
		view.Lunar = &lunar
	}
	if natural, ok := r.Rule.Next(today); ok && natural != occ.Date && r.DoneThrough != nil {
		view.Handled = r.DoneThrough
	}
	following := occ.Date
	for range 3 {
		next, ok := r.Rule.After(following)
		if !ok {
			break
		}
		view.Following = append(view.Following, next)
		following = next
	}
	switch {
	case !r.Enabled:
		view.State = reminderDisabled
	case occ.InWindow && snoozed(r, today):
		view.State = reminderSnoozed
	case occ.Days == 0:
		view.State = reminderToday
	case occ.InWindow:
		view.State = reminderSoon
	}
	return view
}

// monthlyAmount spreads a reminder's amount over an average month.
func monthlyAmount(r core.Reminder) *float64 {
	every := float64(max(r.Rule.Interval, 1))
	var perMonth float64
	switch r.Rule.Freq {
	case recur.Weekly:
		perMonth = *r.Amount * (365.25 / 12 / 7) / every
	case recur.Monthly:
		perMonth = *r.Amount / every
	case recur.Yearly:
		perMonth = *r.Amount / 12 / every
	default:
		return nil // a one-off is not a running cost
	}
	perMonth = forecast.Round(perMonth)
	return &perMonth
}

func (v viewer) reminders(ctx context.Context) ([]ReminderView, error) {
	reminders, err := v.e.store.Reminders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ReminderView, 0, len(reminders))
	for _, r := range reminders {
		out = append(out, v.reminder(r))
	}
	// Soonest first; reminders with nothing ahead go last.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Days, out[j].Days
		switch {
		case a == nil || b == nil:
			return a != nil
		default:
			return *a < *b
		}
	})
	return out, nil
}

// Reminders lists every reminder, soonest first.
func (e *Engine) Reminders(ctx context.Context, lang string) ([]ReminderView, error) {
	v, err := e.view(ctx, lang)
	if err != nil {
		return nil, err
	}
	return v.reminders(ctx)
}

// ReminderView loads one reminder.
func (e *Engine) ReminderView(ctx context.Context, id, lang string) (ReminderView, error) {
	v, err := e.view(ctx, lang)
	if err != nil {
		return ReminderView{}, err
	}
	r, err := e.store.Reminder(ctx, id)
	if err != nil {
		return ReminderView{}, err
	}
	return v.reminder(r), nil
}

// PreviewRule shows where a rule lands without saving anything: the form
// uses it to answer "is this the date I meant?" while it is being filled in.
func (e *Engine) PreviewRule(ctx context.Context, rule recur.Rule, lang string) (ReminderView, error) {
	if err := rule.Validate(); err != nil {
		return ReminderView{}, invalid("%s", err.Error())
	}
	v, err := e.view(ctx, lang)
	if err != nil {
		return ReminderView{}, err
	}
	return v.reminder(core.Reminder{Rule: rule, Enabled: true, LeadDays: []int{0}}), nil
}

// ---------- incidents and deliveries ----------

// Incidents lists incident history with each one's story.
func (e *Engine) Incidents(ctx context.Context, f store.IncidentFilter, lang string) ([]IncidentView, error) {
	v, err := e.view(ctx, lang)
	if err != nil {
		return nil, err
	}
	incidents, err := e.store.Incidents(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]IncidentView, 0, len(incidents))
	for _, inc := range incidents {
		out = append(out, v.incident(inc))
	}
	return out, nil
}

// IncidentView tells one incident's story in the given language.
func (e *Engine) IncidentView(ctx context.Context, inc core.Incident, lang string) (IncidentView, error) {
	v, err := e.view(ctx, lang)
	if err != nil {
		return IncidentView{}, err
	}
	return v.incident(inc), nil
}

// Deliveries lists the notification log.
func (e *Engine) Deliveries(ctx context.Context, f store.DeliveryFilter) ([]core.Delivery, error) {
	return e.store.Deliveries(ctx, f)
}

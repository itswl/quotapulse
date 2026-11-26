// Package mcp lets an AI agent use QuotaPulse the way a person does from the
// dashboard: ask what needs attention, check headroom before a large job,
// add and change dates — and, with an admin token, manage accounts, meters,
// channels, tokens, settings and backups.
//
// Every tool goes through the same engine calls as the dashboard, under the
// scopes of the token that was presented. The tools are shaped for reading
// by a model rather than around the database: an agent that calls
// `attention` first knows what matters, and one that calls `list_meters`
// before a large job knows which account can take it. There is nothing an
// agent can do here that the token's owner could not do with curl.
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
	"github.com/itswl/quotapulse/internal/runtime"
	"github.com/itswl/quotapulse/internal/store"
)

const instructions = `QuotaPulse watches things that run out (meters: prepaid balances, quotas that reset, running totals of spend) and things that come due (dates: renewals, birthdays, expiries).

Start with "attention": it lists what is open right now and what is coming up.
Before starting work that will consume a lot of an API or a plan, call "list_meters" and read "headroom" for the account you intend to use; prefer one that says "ok".
Amounts are in each meter's own unit — a currency code, "%", or a label such as "calls". Never add amounts across different units.
Times are RFC 3339 in UTC; dates are YYYY-MM-DD in the user's time zone, which every result names.
Tools that change something exist only if the token allows it. With an admin token there are also tools for accounts, meters, notification channels, tokens, settings, export and backup; one that deletes something says so in its description.`

var schemas = sdk.NewSchemaCache()

// Option adds what a few tools need beyond the engine.
type Option func(*tools)

// WithRunner gives get_system the state of the background loops and lets
// backup_now write a database backup. Without it both say they are not
// available here.
func WithRunner(r *runtime.Runner) Option {
	return func(t *tools) { t.runner = r }
}

// WithDemo marks the instance as running on generated data, which
// get_system reports.
func WithDemo(yes bool) Option {
	return func(t *tools) { t.demo = yes }
}

// Handler serves MCP over streamable HTTP. It expects to be mounted behind
// the API's token check: each request carries the scopes of its token, and
// the tools offered are decided from them.
//
// A request may name its language as the HTTP API's requests do, with
// ?lang=zh or an X-QuotaPulse-Lang header: what the tools then say in words
// — headlines, summaries, a rule in words — is written in it. A client that
// asks for nothing is answered in English.
func Handler(e *engine.Engine, log *slog.Logger, opts ...Option) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	protocol := protocolLogger(log)
	return sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		name, allows := api.PrincipalScopes(r.Context())
		t := &tools{e: e, log: log, protocol: protocol, caller: name, allows: allows, lang: "en"}
		if asked := api.LanguageOf(r); asked != "" {
			t.lang = asked
		}
		for _, opt := range opts {
			opt(t)
		}
		return newServer(t)
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
	// runner runs the background loops; nil when there is none, which
	// leaves get_system without their state and backup_now unavailable.
	runner *runtime.Runner
	demo   bool
	// lang is what the request asked for, zh or en, in its query
	// (?lang=zh) or its X-QuotaPulse-Lang header. Everything a result says
	// in words is written in it; English when it asked for nothing.
	lang string
}

func newServer(t *tools) *sdk.Server {
	server := sdk.NewServer(
		&sdk.Implementation{Name: "quotapulse", Title: "QuotaPulse", Version: buildinfo.Version},
		&sdk.ServerOptions{Instructions: instructions, Logger: t.protocol, SchemaCache: schemas},
	)
	if t.allows(core.ScopeRead) {
		add(t, server, readOnly("attention", "What needs attention",
			"What is open right now and what is coming up. Call this first. Returns the overall status, every open incident with a plain-language headline, the dates inside the look-ahead window, and whether notifications are muted."),
			t.attention)
		add(t, server, readOnly("list_meters", "Meters and headroom",
			"Every balance, quota and spend counter with its current value, forecast and a one-word headroom verdict (ok, low, exhausted, unknown). Use it before a large job to pick an account that can take it."),
			t.listMeters)
		add(t, server, readOnly("get_meter", "One meter in detail",
			"A meter's forecast, rules, open incidents, recent daily spend and top-ups. Identify it by id, or by name as shown in list_meters."),
			t.getMeter)
		add(t, server, readOnly("list_dates", "Upcoming dates",
			"Renewals, birthdays, anniversaries and expiries, soonest first, with how many days are left and when they remind. Lunar dates are already converted."),
			t.listDates)
		add(t, server, readOnly("get_spend", "Spend for a month",
			"What the meters burned in a calendar month, converted to the base currency where a rate is known, day by day, with the month-end projection, the renewals that fall in the month and the totals of the months before."),
			t.getSpend)
		add(t, server, readOnly("list_activity", "Incident history",
			"Past and present incidents, newest first: what happened, when it opened and when it was resolved. Filter by state or by the thing it is about, and page back through a long history."),
			t.listActivity)
		add(t, server, readOnly("list_accounts", "Accounts",
			"The accounts that are read from their platforms: which provider, how often, whether the last read succeeded and why not, and the credential as the dashboard shows it, secrets masked."),
			t.listAccounts)
		add(t, server, readOnly("get_settings", "Settings",
			"The user's settings: time zone, language, base currency and exchange rates, how often accounts are read, reminder time, quiet hours, the digest, retention and more."),
			t.getSettings)
		add(t, server, readOnly("preview_date", "Where a rule lands",
			"Show where a date's rule would land, without saving anything: the next occurrence, the three after it and, for a lunar rule, the lunar date. Takes the same rule fields as add_date. Use it to check a rule before adding it."),
			t.previewDate)
		add(t, server, readOnly("list_deliveries", "Notification log",
			"The messages QuotaPulse sent, or tried to send, newest first: which incident, which channel, whether it went, and the error when it did not. This is where to look when someone was not notified."),
			t.listDeliveries)
		add(t, server, readOnly("list_channel_types", "Kinds of notification channel",
			"Every kind of channel that can be created, with the settings each one needs."),
			t.listChannelTypes)
		add(t, server, readOnly("list_providers", "Providers",
			"Every provider an account can be read through: the built-in catalog and the user's own definitions, with the credential fields each one needs."),
			t.listProviders)
	}
	if t.allows(core.ScopeAct) {
		add(t, server, changing("add_date", "Add a date",
			"Add a reminder for a renewal, birthday, anniversary, holiday, expiry or anything else with a date. Supports weekly, monthly, yearly and one-off dates, and the lunar calendar for monthly and yearly ones.", false),
			t.addDate)
		add(t, server, changing("update_date", "Change a date",
			"Change a date's title, kind, rule, reminder days, amount, group, notes or whether it is enabled. Send only what should change; everything else is left as it is.", false),
			t.updateDate)
		add(t, server, changing("complete_date", "Mark a date as done",
			"Mark the upcoming occurrence of a date as dealt with — paid, renewed, celebrated. Its reminders stop and it moves on to the next occurrence.", true),
			t.completeDate)
		add(t, server, changing("reopen_date", "Take back a done mark",
			"Take back the last \"done\" mark on a date, so the occurrence it covered is upcoming again.", true),
			t.reopenDate)
		add(t, server, changing("snooze_date", "Hold back a date's reminders",
			"Hold back a date's reminders until after a day. The date still shows as upcoming.", true),
			t.snoozeDate)
		add(t, server, changing("unsnooze_date", "Wake a date up",
			"Stop holding back a date's reminders.", true),
			t.unsnoozeDate)
		add(t, server, destructive("delete_date", "Delete a date",
			"Delete a date and its reminders for good. To silence it for a while, snooze it; to switch it off without losing it, disable it with update_date."),
			t.deleteDate)
		add(t, server, changing("mute_meter", "Mute a meter",
			"Hold back notifications about a meter until a moment. Its incidents keep opening and resolving.", true),
			t.muteMeter)
		add(t, server, changing("unmute_meter", "Unmute a meter",
			"Stop holding back a meter's notifications.", true),
			t.unmuteMeter)
		add(t, server, changing("acknowledge", "Acknowledge an incident",
			"Acknowledge an open incident: it stays open until its cause is gone, but stops repeating its notification.", true),
			t.acknowledge)
		add(t, server, changing("unacknowledge", "Take back an acknowledgement",
			"Take back an acknowledgement: the incident repeats its notification again.", true),
			t.unacknowledge)
		add(t, server, changing("sample_now", "Read accounts now",
			"Ask for a fresh reading of one account, or of all of them, instead of waiting for the schedule. The reading arrives within seconds; call list_meters again to see it.", true),
			t.sampleNow)
	}
	if t.allows(core.ScopeIngest) {
		add(t, server, changing("record_reading", "Report a reading",
			"Report the current value of something QuotaPulse cannot read itself: what a local tool has cost so far (kind counter), a balance or a quota. The first reading for a new meter must say its kind.", false),
			t.recordReading)
		add(t, server, changing("record_readings", "Report several readings",
			"Report up to 500 readings at once, each in the shape record_reading takes.", false),
			t.recordReadings)
	}
	if t.allows(core.ScopeAdmin) {
		addAdminTools(t, server)
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

// destructive marks a tool that removes something for good, so a client can
// ask before calling it. Calling it a second time fails with \"no such
// thing\", so it is not idempotent either.
func destructive(name, title, description string) *sdk.Tool {
	yes, no := true, false
	return &sdk.Tool{Name: name, Title: title, Description: description, Annotations: &sdk.ToolAnnotations{
		Title: title, IdempotentHint: false, DestructiveHint: &yes, OpenWorldHint: &no,
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

// contextOf is the Context of a result produced now.
func (t *tools) contextOf(settings core.Settings) Context {
	return Context{Now: t.e.Now().UTC(), Timezone: settings.Timezone}
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

// schedule is a recurrence rule as an object: the shape the HTTP API uses,
// and the one update_date takes.
type schedule struct {
	Freq     string `json:"freq" jsonschema:"once, weekly, monthly or yearly"`
	Calendar string `json:"calendar,omitempty" jsonschema:"lunar, or empty for the solar calendar; lunar works with monthly and yearly"`
	Date     string `json:"date,omitempty" jsonschema:"for once: the day, as YYYY-MM-DD. With interval above 1: the day to count from"`
	Weekday  int    `json:"weekday,omitempty" jsonschema:"for weekly: 1 is Monday, 7 is Sunday"`
	Month    int    `json:"month,omitempty" jsonschema:"for yearly: the month, 1-12 (a lunar month when calendar is lunar)"`
	Day      int    `json:"day,omitempty" jsonschema:"for monthly and yearly: the day of the month"`
	Interval int    `json:"interval,omitempty" jsonschema:"every N weeks, months or years; needs date to count from"`
	Since    int    `json:"since,omitempty" jsonschema:"the year it started — a birth year — so reminders can say which one it is"`
}

func scheduleOf(r recur.Rule) schedule {
	out := schedule{Freq: r.Freq, Calendar: r.Calendar, Weekday: r.Weekday, Month: r.Month, Day: r.Day, Interval: r.Interval, Since: r.Since}
	if r.Date != nil {
		out.Date = r.Date.String()
	}
	return out
}

// rule reads the schedule back as a rule.
func (s schedule) rule() (recur.Rule, error) {
	return ruleOf(s.Freq, s.Calendar, s.Date, s.Weekday, s.Month, s.Day, s.Interval, s.Since)
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
	Schedule schedule `json:"schedule" jsonschema:"the rule as fields, in the shape update_date takes"`
	Amount   *float64 `json:"amount,omitempty"`
	Currency string   `json:"currency,omitempty"`
	LeadDays []int    `json:"lead_days" jsonschema:"the days before the date on which to remind; 0 is the day itself, soonest last"`
	Group    string   `json:"group,omitempty"`
	Notes    string   `json:"notes,omitempty"`
	Enabled  bool     `json:"enabled" jsonschema:"a disabled date is not reminded about"`
	State    string   `json:"state" jsonschema:"upcoming, soon (inside its reminder window), today, snoozed, past or disabled"`
	// Handled is the occurrence, today or later, that was marked done and is
	// therefore being skipped.
	Handled string `json:"handled,omitempty" jsonschema:"the occurrence that was marked done and is being skipped, when it is today or later"`
	// Following are the occurrences after Next, for a preview of the rule.
	Following         []string `json:"following,omitempty" jsonschema:"the occurrences after the next one, at most three"`
	MonthlyAmount     *float64 `json:"monthly_amount,omitempty" jsonschema:"the amount spread over an average month, so plans that renew at different intervals can be compared"`
	NotificationsHeld string   `json:"notifications_held_until,omitempty" jsonschema:"reminders are held back until after this day"`
}

func (t *tools) dateOf(v engine.ReminderView) Date {
	out := Date{
		ID: v.ID, Title: v.Title, Kind: v.Kind, Days: v.Days, Ordinal: v.Ordinal, Rule: ruleInWords(v.Rule, i18n.Of(t.lang)),
		Schedule: scheduleOf(v.Rule), Amount: v.Amount, Currency: v.Currency, State: v.State, Notes: v.Notes,
		LeadDays: v.LeadDays, Group: v.Group, Enabled: v.Enabled,
	}
	if out.LeadDays == nil {
		out.LeadDays = []int{}
	}
	if v.Next != nil {
		out.Next = v.Next.String()
	}
	if v.Lunar != nil {
		out.Lunar = v.Lunar.Label
	}
	if v.Handled != nil {
		out.Handled = v.Handled.String()
	}
	for _, d := range v.Following {
		out.Following = append(out.Following, d.String())
	}
	out.MonthlyAmount = rounded(v.MonthlyAmount)
	if v.SnoozedUntil != nil {
		out.NotificationsHeld = v.SnoozedUntil.String()
	}
	return out
}

// ruleInWords says a rule in a sentence, in the request's language.
func ruleInWords(r recur.Rule, l i18n.Lang) string {
	// every is "every month" or "every 3 months", with the unit given in
	// both languages: the English one is pluralised.
	every := func(en, zh string) string {
		if r.Interval > 1 {
			return l.Pick(fmt.Sprintf("every %d %ss", r.Interval, en), fmt.Sprintf("每 %d %s", r.Interval, zh))
		}
		return l.Pick("every "+en, "每"+zh)
	}
	lunar := ""
	if r.Calendar == recur.LunarCal {
		lunar = l.Pick("lunar ", "农历")
	}
	switch r.Freq {
	case recur.Once:
		return l.Pick("once on "+r.Date.String(), r.Date.String()+" 这一天")
	case recur.Weekly:
		days := []string{"", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
		daysZH := []string{"", "一", "二", "三", "四", "五", "六", "日"}
		weekday := r.Weekday
		if weekday == 0 && r.Date != nil {
			weekday = r.Date.Weekday()
		}
		if l == i18n.ZH {
			if r.Interval > 1 {
				return fmt.Sprintf("每 %d 周的周%s", r.Interval, daysZH[weekday])
			}
			return "每周" + daysZH[weekday]
		}
		return every("week", "周") + " on " + days[weekday]
	case recur.Monthly:
		return l.Pick(
			fmt.Sprintf("%s on %sday %d", every("month", "月"), lunar, r.Day),
			fmt.Sprintf("%s%s %d 日", every("month", "月"), lunar, r.Day),
		)
	default:
		return l.Pick(
			fmt.Sprintf("%s on %smonth %d day %d", every("year", "年"), lunar, r.Month, r.Day),
			fmt.Sprintf("%s%s %d 月 %d 日", every("year", "年"), lunar, r.Month, r.Day),
		)
	}
}

// ruleOf builds a rule from the fields add_date and preview_date take.
func ruleOf(freq, calendar, date string, weekday, month, day, interval, since int) (recur.Rule, error) {
	r := recur.Rule{Freq: freq, Calendar: calendar, Weekday: weekday, Month: month, Day: day, Interval: interval, Since: since}
	if date != "" {
		on, err := recur.ParseDate(date)
		if err != nil {
			return r, &engine.InvalidError{Reason: err.Error()}
		}
		r.Date = &on
	}
	return r, nil
}

// topUp is money added to a balance.
type topUp struct {
	At     time.Time `json:"at"`
	Amount float64   `json:"amount" jsonschema:"in the meter's unit"`
}

// burst is a run of spend well above the usual pace.
type burst struct {
	WindowHours float64  `json:"window_hours" jsonschema:"over how long it was measured"`
	Spend       float64  `json:"spend" jsonschema:"in the meter's unit"`
	RatePerDay  float64  `json:"rate_per_day"`
	RunwayDays  float64  `json:"runway_days" jsonschema:"how long the balance would last at that pace"`
	Ratio       *float64 `json:"ratio,omitempty" jsonschema:"how many times the usual pace that is"`
}

// Meter is a meter with its forecast, flattened for reading.
type Meter struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Key      string   `json:"key" jsonschema:"the meter's stable key within its account, or the key it is reported under"`
	Account  string   `json:"account,omitempty" jsonschema:"the account it is read from; absent for a meter that is reported from outside"`
	Provider string   `json:"provider,omitempty"`
	Group    string   `json:"group,omitempty" jsonschema:"the group its account was put in"`
	Kind     string   `json:"kind" jsonschema:"balance (spent down and topped up), quota (an allowance that resets) or counter (a running total of spend)"`
	Unit     string   `json:"unit" jsonschema:"a currency code, % or a label such as calls"`
	Value    *float64 `json:"value,omitempty" jsonschema:"the current value: what is left of a balance or quota, the total so far of a counter"`
	Limit    *float64 `json:"limit,omitempty" jsonschema:"the size of a quota"`
	Status   string   `json:"status" jsonschema:"ok, warning, critical, failing (its account cannot be read; the value is the last one seen), pending (never read) or paused"`
	Headroom string   `json:"headroom" jsonschema:"ok, low, exhausted or unknown: whether it is safe to lean on this meter now"`
	Summary  string   `json:"summary" jsonschema:"the state in one sentence"`

	AccountStatus string     `json:"account_status,omitempty" jsonschema:"ok or failing: whether the account behind it can be read"`
	AccountError  string     `json:"account_error,omitempty" jsonschema:"why the account cannot be read, when it cannot"`
	Hidden        bool       `json:"hidden" jsonschema:"a hidden meter is not judged: nothing opens on it"`
	Muted         bool       `json:"muted" jsonschema:"notifications about it are held back right now"`
	MutedUntil    *time.Time `json:"muted_until,omitempty" jsonschema:"until when they are held back"`
	WindowSeconds *int64     `json:"window_seconds,omitempty" jsonschema:"how long a quota's window is"`
	// Effective is what is in force: the meter's own rule, or the default
	// from the settings.
	EffectiveRunwayDays float64 `json:"effective_runway_days" jsonschema:"days of runway that opens a warning, as in force; 0 when the rule is off"`
	EffectiveAnomaly    bool    `json:"effective_anomaly" jsonschema:"whether burst detection is in force"`

	RemainingPct *float64   `json:"remaining_pct,omitempty" jsonschema:"share of a quota that is left"`
	ResetsAt     *time.Time `json:"resets_at,omitempty" jsonschema:"when a quota resets"`
	ExhaustsAt   *time.Time `json:"exhausts_at,omitempty" jsonschema:"when a quota runs out at the current pace, if that is before it resets"`
	BurnPerDay   *float64   `json:"burn_per_day,omitempty" jsonschema:"expected spend per day, weighted towards recent days"`
	PeakPerDay   *float64   `json:"peak_per_day,omitempty" jsonschema:"the most spent in one day recently"`
	RunwayDays   *float64   `json:"runway_days,omitempty" jsonschema:"days a balance lasts at the expected burn; absent when nothing is being spent"`
	DepletesAt   *time.Time `json:"depletes_at,omitempty"`
	LastTopUp    *topUp     `json:"last_top_up,omitempty"`
	Bursts       []burst    `json:"bursts,omitempty" jsonschema:"recent runs of spend well above the usual pace"`
	MonthSpend   *float64   `json:"month_spend,omitempty" jsonschema:"spent so far this calendar month"`
	Confidence   string     `json:"forecast_confidence,omitempty" jsonschema:"none, low, medium or high: how much history the forecast rests on"`
	SampledAt    *time.Time `json:"sampled_at,omitempty"`
}

func meterOf(v engine.MeterView, l i18n.Lang, now time.Time) Meter {
	out := Meter{
		ID: v.ID, Name: v.Display, Key: v.Key, Kind: string(v.Kind), Unit: v.Unit, Value: v.Value, Limit: v.Limit,
		Status: v.Status, Headroom: "unknown", SampledAt: v.SampledAt,
		AccountStatus: accountStatus(v), Hidden: v.Hidden, Muted: v.Muted, MutedUntil: v.MutedUntil, WindowSeconds: v.Window,
		EffectiveRunwayDays: v.Effective.RunwayDays, EffectiveAnomaly: v.Effective.Anomaly,
	}
	if v.Account != nil {
		out.Account, out.Provider, out.Group = v.Account.Name, v.Account.Provider, v.Account.Group
		out.AccountError = v.Account.LastError
	}
	if v.Value == nil {
		out.Summary = l.Pick("No reading yet.", "还没有读数。")
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
		out.Summary = l.Pick(amount+" left", "剩余 "+amount)
		if v.Limit != nil && v.Unit != "%" {
			out.Summary += l.Pick(" of ", "，上限 ") + i18n.Amount(*v.Limit, v.Unit)
		}
		if v.ResetsAt != nil && v.ResetsAt.After(now) {
			out.Summary += l.Pick(", resets in ", "，") + l.Span(v.ResetsAt.Sub(now)) + l.Pick("", "后重置")
		}
		out.Summary += l.Pick(".", "。")
	case v.Forecast != nil && v.Kind == core.Balance:
		fc := v.Forecast
		out.Confidence, out.RunwayDays, out.DepletesAt = fc.Confidence, fc.RunwayDays, fc.DepletesAt
		if fc.Confidence != "none" {
			out.BurnPerDay, out.PeakPerDay = &fc.Burn, &fc.Peak
		}
		if fc.LastTopUp != nil {
			out.LastTopUp = &topUp{At: fc.LastTopUp.At.UTC(), Amount: round2(fc.LastTopUp.Amount)}
		}
		for _, b := range fc.Bursts {
			out.Bursts = append(out.Bursts, burst{
				WindowHours: b.WindowHours, Spend: round2(b.Spend), RatePerDay: round2(b.RatePerDay),
				RunwayDays: round2(b.RunwayDays), Ratio: rounded(b.Ratio),
			})
		}
		out.Headroom = "ok"
		switch {
		case *v.Value <= 0:
			out.Headroom = "exhausted"
		case v.Status == "critical" || v.Status == "warning":
			out.Headroom = "low"
		}
		out.Summary = l.Pick(amount+" left", "剩余 "+amount)
		if fc.RunwayDays != nil {
			out.Summary += l.Pick(", about ", "，按近期用量约可用 ") + l.Days(*fc.RunwayDays) + l.Pick(" at the recent pace", "")
		}
		out.Summary += l.Pick(".", "。")
	default:
		out.Summary = l.Pick(amount+" so far.", "目前累计 "+amount+"。")
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
		out.Summary += l.Pick(" Its account cannot be read right now; this is the last value seen.", " 其账户当前读不到，这是最后一次读到的值。")
	}
	return out
}

// accountStatus says in one word whether the account behind a meter can be
// read. A disabled account is paused rather than failing.
func accountStatus(v engine.MeterView) string {
	if v.Account == nil {
		return ""
	}
	if v.Status == "paused" {
		return "paused"
	}
	return v.Account.Status
}

// ---------- read tools ----------

type attentionIn struct {
	Days int `json:"days,omitempty" jsonschema:"how many days ahead to look for dates, 1-366; the user's setting when omitted"`
}

// counts says how much is configured.
type counts struct {
	Accounts int `json:"accounts"`
	Meters   int `json:"meters"`
	Dates    int `json:"dates"`
	Channels int `json:"channels"`
}

type attentionOut struct {
	Context
	Status             string     `json:"status" jsonschema:"ok, warning or critical: the worst thing that is open"`
	Summary            string     `json:"summary" jsonschema:"the situation in one sentence"`
	Open               []Incident `json:"open"`
	Upcoming           []Date     `json:"upcoming"`
	NotificationsMuted bool       `json:"notifications_muted" jsonschema:"true while notifications are recorded but not sent"`
	Counts             counts     `json:"counts" jsonschema:"how much is configured"`
}

func (t *tools) attention(ctx context.Context, in attentionIn) (attentionOut, error) {
	if in.Days < 0 || in.Days > 366 {
		return attentionOut{}, &engine.InvalidError{Reason: "days must be between 1 and 366"}
	}
	overview, err := t.e.Overview(ctx, t.lang)
	if err != nil {
		return attentionOut{}, err
	}
	out := attentionOut{
		Context: Context{Now: overview.Now, Timezone: overview.Timezone}, Status: overview.Status,
		Open: []Incident{}, Upcoming: []Date{}, NotificationsMuted: overview.Muted,
		Counts: counts{Accounts: overview.Counts.Accounts, Meters: overview.Counts.Meters, Dates: overview.Counts.Reminders, Channels: overview.Counts.Channels},
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
		out.Upcoming = append(out.Upcoming, t.dateOf(d))
	}
	l := i18n.Of(t.lang)
	switch {
	case len(out.Open) == 0 && len(out.Upcoming) == 0:
		out.Summary = l.Pick("Nothing needs attention.", "没有需要关注的事。")
	case len(out.Open) == 0:
		out.Summary = fmt.Sprintf(l.Pick("Nothing is wrong. Next date: %s, %s.", "一切正常。下一个日期：%s，%s。"),
			out.Upcoming[0].Title, l.Until(*out.Upcoming[0].Days))
	default:
		out.Summary = fmt.Sprintf(l.Pick("%d open: %s", "%d 个未处理：%s"), len(out.Open), out.Open[0].Headline)
		if len(out.Open) > 1 {
			out.Summary += fmt.Sprintf(l.Pick(" (and %d more)", "（还有 %d 个）"), len(out.Open)-1)
		}
		out.Summary += l.Pick(".", "。")
	}
	return out, nil
}

func (t *tools) datesWithin(ctx context.Context, days int) ([]engine.ReminderView, error) {
	all, err := t.e.Reminders(ctx, t.lang)
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
		m := meterOf(v, i18n.Of(t.lang), now)
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
	views, err := t.e.Meters(ctx, t.lang)
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

// seriesRanges is how far back a meter's history can be read.
var seriesRanges = map[string]bool{"24h": true, "7d": true, "30d": true, "90d": true, "1y": true}

type getMeterIn struct {
	Meter string `json:"meter" jsonschema:"the meter's id, or its name as shown in list_meters"`
	Range string `json:"range,omitempty" jsonschema:"how far back to read: 24h, 7d, 30d (the default), 90d or 1y"`
	Days  int    `json:"days,omitempty" jsonschema:"how many days of daily spend to include, 1-366 (default 14; the whole range when range is given)"`
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
	TopUps    []topUp      `json:"top_ups" jsonschema:"recent top-ups, oldest first; empty for a quota"`
}

func (t *tools) getMeter(ctx context.Context, in getMeterIn) (getMeterOut, error) {
	view, err := t.findMeter(ctx, in.Meter)
	if err != nil {
		return getMeterOut{}, err
	}
	return t.meterDetail(ctx, view, in)
}

// meterDetail is what get_meter and update_meter answer with.
func (t *tools) meterDetail(ctx context.Context, view engine.MeterView, in getMeterIn) (getMeterOut, error) {
	if in.Days < 0 || in.Days > 366 {
		return getMeterOut{}, &engine.InvalidError{Reason: "days must be between 1 and 366"}
	}
	rangeName := strings.TrimSpace(in.Range)
	switch {
	case rangeName == "":
		rangeName = "30d"
		if in.Days > 30 {
			rangeName = "90d"
		}
	case !seriesRanges[rangeName]:
		return getMeterOut{}, &engine.InvalidError{Reason: "range must be one of 24h, 7d, 30d, 90d, 1y"}
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return getMeterOut{}, err
	}
	now := t.e.Now()
	out := getMeterOut{
		Context: Context{Now: now.UTC(), Timezone: settings.Timezone},
		Meter:   meterOf(view, i18n.Of(t.lang), now), Rules: view.Rules, Incidents: []Incident{}, Daily: []dailySpend{}, TopUps: []topUp{},
	}
	for _, inc := range view.Incidents {
		out.Incidents = append(out.Incidents, incidentOf(inc))
	}
	if view.Kind == core.Quota {
		return out, nil
	}
	series, err := t.e.Series(ctx, view.ID, rangeName, t.lang)
	if err != nil {
		return out, err
	}
	days := in.Days
	if days == 0 {
		days = 14
		if in.Range != "" {
			days = 366
		}
	}
	daily := series.Daily
	if len(daily) > days {
		daily = daily[len(daily)-days:]
	}
	for _, d := range daily {
		out.Daily = append(out.Daily, dailySpend{Date: d.Date.String(), Spend: round2(d.Spend), TopUp: round2(d.TopUp)})
	}
	for _, ev := range series.TopUps {
		out.TopUps = append(out.TopUps, topUp{At: ev.At.UTC(), Amount: round2(ev.Amount)})
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
	// IncludeInactive lists every date, and ignores days: also the ones
	// that are disabled, snoozed, or have nothing ahead.
	IncludeInactive bool `json:"include_inactive,omitempty" jsonschema:"list every date, whatever its state, and ignore days: also the disabled ones and those with nothing ahead"`
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
	var views []engine.ReminderView
	if in.IncludeInactive {
		if views, err = t.e.Reminders(ctx, t.lang); err != nil {
			return listDatesOut{}, err
		}
	} else if views, err = t.datesWithin(ctx, days); err != nil {
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
			out.Dates = append(out.Dates, t.dateOf(v))
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

// spendDay is one day's metered total, in the base currency.
type spendDay struct {
	Date   string  `json:"date"`
	Amount float64 `json:"amount"`
}

// spendMonth is a past month's total.
type spendMonth struct {
	Month string  `json:"month"`
	Total float64 `json:"total" jsonschema:"metered spend, in the base currency"`
	Fixed float64 `json:"fixed" jsonschema:"the renewals that fell in that month"`
}

type getSpendOut struct {
	Context
	Month       string       `json:"month"`
	Base        string       `json:"base_currency"`
	IsCurrent   bool         `json:"is_current" jsonschema:"whether this is the month in progress"`
	Total       float64      `json:"total" jsonschema:"metered spend so far, in the base currency"`
	Projected   float64      `json:"projected" jsonschema:"metered spend expected by the end of the month"`
	Renewals    float64      `json:"renewals_total" jsonschema:"sum of the dated renewals that fall in the month, in the base currency"`
	DaysElapsed float64      `json:"days_elapsed,omitempty" jsonschema:"how far into the month, in days; only for the month in progress"`
	DaysInMonth int          `json:"days_in_month,omitempty"`
	Rows        []spendRow   `json:"meters"`
	RenewalRows []renewalRow `json:"renewals"`
	Daily       []spendDay   `json:"daily" jsonschema:"each day's metered total so far, in the base currency"`
	History     []spendMonth `json:"history" jsonschema:"the months before, newest last"`
	Unconverted []string     `json:"unconverted" jsonschema:"currencies with no exchange rate, which are therefore not in the totals"`
}

func (t *tools) getSpend(ctx context.Context, in getSpendIn) (getSpendOut, error) {
	spend, err := t.e.Spend(ctx, strings.TrimSpace(in.Month), t.lang)
	if err != nil {
		return getSpendOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return getSpendOut{}, err
	}
	out := getSpendOut{
		Context: Context{Now: t.e.Now().UTC(), Timezone: settings.Timezone},
		Month:   spend.Month, Base: spend.Base, IsCurrent: spend.IsCurrent,
		Total: round2(spend.Total), Projected: round2(spend.Projected),
		Renewals: round2(spend.Fixed), Rows: []spendRow{}, RenewalRows: []renewalRow{}, Unconverted: spend.Unconverted,
		Daily: []spendDay{}, History: []spendMonth{},
	}
	if spend.IsCurrent {
		out.DaysElapsed, out.DaysInMonth = spend.DaysElapsed, spend.DaysInMonth
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
	for _, d := range spend.Daily {
		out.Daily = append(out.Daily, spendDay{Date: d.Date.String(), Amount: round2(d.Amount)})
	}
	for _, m := range spend.History {
		out.History = append(out.History, spendMonth{Month: m.Month, Total: round2(m.Total), Fixed: round2(m.Fixed)})
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
	State       string `json:"state,omitempty" jsonschema:"open or resolved; both when omitted"`
	SubjectType string `json:"subject_type,omitempty" jsonschema:"only incidents about this kind of thing: meter, account, reminder or channel"`
	SubjectID   string `json:"subject_id,omitempty" jsonschema:"only incidents about this one thing, by id"`
	Limit       int    `json:"limit,omitempty" jsonschema:"how many to return, 1-500 (default 20)"`
	Before      string `json:"before,omitempty" jsonschema:"page backwards: only incidents opened before this moment, RFC 3339 — the opened_at of the last one you have"`
}

type listActivityOut struct {
	Context
	Incidents []Incident `json:"incidents"`
}

func (t *tools) listActivity(ctx context.Context, in listActivityIn) (listActivityOut, error) {
	if in.State != "" && in.State != core.StateOpen && in.State != core.StateResolved {
		return listActivityOut{}, &engine.InvalidError{Reason: "state must be open or resolved"}
	}
	switch in.SubjectType {
	case "", core.SubjectMeter, core.SubjectAccount, core.SubjectReminder, core.SubjectChannel:
	default:
		return listActivityOut{}, &engine.InvalidError{Reason: "subject_type must be meter, account, reminder or channel"}
	}
	if in.Limit < 0 || in.Limit > 500 {
		return listActivityOut{}, &engine.InvalidError{Reason: "limit must be between 1 and 500"}
	}
	limit := in.Limit
	if limit == 0 {
		limit = 20
	}
	filter := store.IncidentFilter{State: in.State, SubjectType: in.SubjectType, SubjectID: strings.TrimSpace(in.SubjectID), Limit: limit}
	if in.Before != "" {
		before, err := time.Parse(time.RFC3339, in.Before)
		if err != nil {
			return listActivityOut{}, &engine.InvalidError{Reason: "before must be an RFC 3339 time"}
		}
		filter.Before = before
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return listActivityOut{}, err
	}
	views, err := t.e.Incidents(ctx, filter, t.lang)
	if err != nil {
		return listActivityOut{}, err
	}
	out := listActivityOut{Context: t.contextOf(settings), Incidents: []Incident{}}
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
	rule, err := ruleOf(in.Freq, in.Calendar, in.Date, in.Weekday, in.Month, in.Day, in.Interval, in.Since)
	if err != nil {
		return dateOut{}, err
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
	view, err := t.e.CreateReminder(ctx, input, t.lang)
	if err != nil {
		return dateOut{}, err
	}
	return t.dateResult(ctx, view)
}

func (t *tools) dateResult(ctx context.Context, view engine.ReminderView) (dateOut, error) {
	return t.dated(ctx, t.dateOf(view))
}

// dated wraps a date with the moment it describes.
func (t *tools) dated(ctx context.Context, d Date) (dateOut, error) {
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return dateOut{}, err
	}
	return dateOut{Context: t.contextOf(settings), Date: d}, nil
}

type completeDateIn struct {
	Date string `json:"date" jsonschema:"the date's id, or its title as shown in list_dates"`
}

func (t *tools) completeDate(ctx context.Context, in completeDateIn) (dateOut, error) {
	view, err := t.findDate(ctx, in.Date)
	if err != nil {
		return dateOut{}, err
	}
	done, err := t.e.CompleteReminder(ctx, view.ID, t.lang)
	if err != nil {
		return dateOut{}, err
	}
	return t.dateResult(ctx, done)
}

type acknowledgeIn struct {
	IncidentID string `json:"incident_id" jsonschema:"the id of an incident, from attention or list_activity"`
}

type acknowledgeOut struct {
	Acknowledged bool   `json:"acknowledged"`
	Headline     string `json:"headline"`
}

func (t *tools) acknowledge(ctx context.Context, in acknowledgeIn) (acknowledgeOut, error) {
	return t.setAcknowledged(ctx, in, true)
}

func (t *tools) unacknowledge(ctx context.Context, in acknowledgeIn) (acknowledgeOut, error) {
	return t.setAcknowledged(ctx, in, false)
}

func (t *tools) setAcknowledged(ctx context.Context, in acknowledgeIn, acked bool) (acknowledgeOut, error) {
	inc, err := t.e.Ack(ctx, strings.TrimSpace(in.IncidentID), acked)
	if err != nil {
		return acknowledgeOut{}, err
	}
	return acknowledgeOut{Acknowledged: acked, Headline: inc.Title}, nil
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
	account, err := t.findAccount(ctx, ref)
	if err != nil {
		return sampleNowOut{}, err
	}
	return sampleNowOut{Requested: account.Name}, t.e.RequestSample(ctx, account.ID)
}

type recordReadingIn struct {
	Meter         string   `json:"meter" jsonschema:"a short key for the meter: lowercase letters, digits, dots, dashes, underscores"`
	Value         float64  `json:"value" jsonschema:"the current value"`
	Kind          string   `json:"kind,omitempty" jsonschema:"balance, quota or counter; required the first time a meter is reported"`
	Unit          string   `json:"unit,omitempty" jsonschema:"a currency code, % or a label"`
	Name          string   `json:"name,omitempty" jsonschema:"a display name for a new meter"`
	Limit         *float64 `json:"limit,omitempty" jsonschema:"the size of a quota"`
	ResetsAt      string   `json:"resets_at,omitempty" jsonschema:"when a quota resets, RFC 3339"`
	WindowSeconds *int64   `json:"window_seconds,omitempty" jsonschema:"how long a quota's window is, in seconds"`
	At            string   `json:"at,omitempty" jsonschema:"when the reading was taken, RFC 3339; now when omitted. It may be in the past, never in the future"`
}

type recordReadingOut struct {
	Meter Meter `json:"meter"`
}

// push turns one reported reading into what the engine takes.
func (in recordReadingIn) push() (engine.Push, error) {
	push := engine.Push{Meter: in.Meter, Name: in.Name, Kind: core.Kind(in.Kind), Unit: in.Unit, Value: &in.Value, Limit: in.Limit, Window: in.WindowSeconds}
	for _, field := range []struct {
		name string
		text string
		into **time.Time
	}{{"resets_at", in.ResetsAt, &push.ResetsAt}, {"at", in.At, &push.At}} {
		if field.text == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, field.text)
		if err != nil {
			return push, &engine.InvalidError{Reason: field.name + " must be an RFC 3339 time"}
		}
		*field.into = &at
	}
	return push, nil
}

func (t *tools) recordReading(ctx context.Context, in recordReadingIn) (recordReadingOut, error) {
	push, err := in.push()
	if err != nil {
		return recordReadingOut{}, err
	}
	meters, err := t.e.Ingest(ctx, []engine.Push{push})
	if err != nil {
		return recordReadingOut{}, err
	}
	view, err := t.e.MeterView(ctx, meters[0].ID, t.lang)
	if err != nil {
		return recordReadingOut{}, err
	}
	return recordReadingOut{Meter: meterOf(view, i18n.Of(t.lang), t.e.Now())}, nil
}

type recordReadingsIn struct {
	Readings []recordReadingIn `json:"readings" jsonschema:"the readings, each in the shape record_reading takes; at most 500"`
}

type recordReadingsOut struct {
	Context
	Accepted int     `json:"accepted" jsonschema:"how many readings were taken"`
	Meters   []Meter `json:"meters" jsonschema:"the meters they belong to, each once"`
}

func (t *tools) recordReadings(ctx context.Context, in recordReadingsIn) (recordReadingsOut, error) {
	if len(in.Readings) == 0 || len(in.Readings) > 500 {
		return recordReadingsOut{}, &engine.InvalidError{Reason: "give between 1 and 500 readings"}
	}
	pushes := make([]engine.Push, 0, len(in.Readings))
	for _, r := range in.Readings {
		push, err := r.push()
		if err != nil {
			return recordReadingsOut{}, err
		}
		pushes = append(pushes, push)
	}
	meters, err := t.e.Ingest(ctx, pushes)
	if err != nil {
		return recordReadingsOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return recordReadingsOut{}, err
	}
	now := t.e.Now()
	out := recordReadingsOut{Context: t.contextOf(settings), Accepted: len(pushes), Meters: []Meter{}}
	for _, m := range meters {
		view, err := t.e.MeterView(ctx, m.ID, t.lang)
		if err != nil {
			return out, err
		}
		out.Meters = append(out.Meters, meterOf(view, i18n.Of(t.lang), now))
	}
	return out, nil
}

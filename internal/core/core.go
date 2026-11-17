// Package core is the vocabulary of QuotaPulse.
//
// The product does one thing: it tells you before something you depend on
// runs out or comes due. Two kinds of things are watched:
//
//   - a Meter is something that runs out — a prepaid balance, a quota that
//     resets, a running total of spend. It is sampled over time.
//   - a Reminder is something that comes due — a renewal, a birthday, an
//     expiry. It follows a recurrence rule.
//
// Both feed one stream of Incidents, which is the only thing that is ever
// notified about. An Account is where meters come from: a credential at a
// provider.
package core

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/recur"
)

// Kind says how a meter's number behaves over time, which decides how its
// history is read.
type Kind string

const (
	// Balance is a stock that is spent down and topped up: a drop is spend,
	// a rise is a top-up.
	Balance Kind = "balance"
	// Quota is an allowance that resets on a schedule: the value is what is
	// left of Limit until ResetsAt.
	Quota Kind = "quota"
	// Counter is a running total of spend, like an odometer: a rise is
	// spend, a drop means the counter started over.
	Counter Kind = "counter"
)

func (k Kind) Valid() bool { return k == Balance || k == Quota || k == Counter }

// Severity ranks how soon something needs a person.
type Severity string

const (
	Info     Severity = "info"     // worth knowing
	Warning  Severity = "warning"  // act soon
	Critical Severity = "critical" // act now
)

// Rank orders severities; unknown values rank lowest.
func (s Severity) Rank() int {
	switch s {
	case Critical:
		return 3
	case Warning:
		return 2
	case Info:
		return 1
	}
	return 0
}

func (s Severity) Valid() bool { return s.Rank() > 0 }

// Account statuses.
const (
	StatusPending = "pending" // never sampled
	StatusOK      = "ok"
	StatusFailing = "failing"
)

// Account is a credential at a provider, sampled on a schedule.
type Account struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	// Credential holds the provider's fields, secret or not. It is encrypted
	// at rest and never serialized as it is; the API shows a masked view.
	Credential map[string]string `json:"-"`
	// Unreadable is set when the stored credential could not be decrypted:
	// the master secret is not the one it was sealed with.
	Unreadable bool   `json:"credential_unreadable"`
	Group      string `json:"group"`
	// Interval overrides the default sampling interval; 0 means default.
	Interval int  `json:"interval_seconds"`
	Enabled  bool `json:"enabled"`

	Status      string     `json:"status"`
	LastError   string     `json:"last_error"`
	Failures    int        `json:"consecutive_failures"`
	SampledAt   *time.Time `json:"sampled_at"`
	SucceededAt *time.Time `json:"succeeded_at"`
	NextAt      *time.Time `json:"next_sample_at"`
	LatencyMs   *int64     `json:"latency_ms"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Meter is one measured quantity.
type Meter struct {
	ID string `json:"id"`
	// AccountID is empty for a pushed meter, one that is reported through the
	// ingest API instead of being sampled from a provider.
	AccountID string `json:"account_id"`
	// Key is stable within the account: the provider definition names it.
	Key  string `json:"key"`
	Name string `json:"name"`
	// Renamed is true once a person has given the meter a name of their own.
	// It is then shown under that name everywhere, instead of under its
	// account's.
	Renamed bool `json:"renamed"`
	Kind    Kind `json:"kind"`
	// Unit is a currency code (CNY, USD), "%", or any label (credits, calls).
	Unit string `json:"unit"`

	Value     *float64   `json:"value"`
	Limit     *float64   `json:"limit"`
	ResetsAt  *time.Time `json:"resets_at"`
	Window    *int64     `json:"window_seconds"`
	SampledAt *time.Time `json:"sampled_at"`

	Rules      Rules      `json:"rules"`
	MutedUntil *time.Time `json:"muted_until"`
	Hidden     bool       `json:"hidden"`
	Sort       int        `json:"sort"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Rules are the per-meter conditions worth an incident. A nil pointer means
// "use the default from Settings"; an explicit zero turns the rule off.
type Rules struct {
	// Low fires when the value is at or below it.
	Low *float64 `json:"low"`
	// RunwayDays fires when the balance is forecast to run out within this
	// many days.
	RunwayDays *float64 `json:"runway_days"`
	// Anomaly fires when a recent burst of spend would empty the balance far
	// sooner than the usual pace.
	Anomaly *bool `json:"anomaly"`
	// Budget is a monthly spend ceiling, in the meter's unit.
	Budget *float64 `json:"budget"`
	// Pace fires when a quota is on course to run out before it resets.
	Pace bool `json:"pace"`
	// StaleAfter fires when a pushed meter has been silent this long.
	StaleAfter *int64 `json:"stale_after_seconds"`
}

// For returns the rules that can apply to a meter of this kind, dropping the
// rest: a counter has no level to fall below, a quota is not money, only a
// balance runs out, and a sampled meter's silence is its account's failure.
// What is stored is then what is in force, rather than a setting that looks
// set and does nothing.
func (r Rules) For(kind Kind, pushed bool) Rules {
	if kind == Counter {
		r.Low = nil
	}
	if kind != Balance {
		r.RunwayDays, r.Anomaly = nil, nil
	}
	if kind == Quota {
		r.Budget = nil
	}
	if kind != Quota {
		r.Pace = false
	}
	if !pushed {
		r.StaleAfter = nil
	}
	return r
}

// Reading is one sample of a meter.
type Reading struct {
	At       time.Time  `json:"at"`
	Value    float64    `json:"value"`
	Limit    *float64   `json:"limit,omitempty"`
	ResetsAt *time.Time `json:"resets_at,omitempty"`
}

// Reminder kinds only change wording and icons; they all follow the same rule
// engine.
const (
	ReminderRenewal     = "renewal"
	ReminderBirthday    = "birthday"
	ReminderAnniversary = "anniversary"
	ReminderHoliday     = "holiday"
	ReminderExpiry      = "expiry"
	ReminderCustom      = "custom"
)

// ReminderKinds lists the kinds in display order.
var ReminderKinds = []string{
	ReminderRenewal, ReminderBirthday, ReminderAnniversary,
	ReminderHoliday, ReminderExpiry, ReminderCustom,
}

// Reminder is a date that comes around.
type Reminder struct {
	ID    string     `json:"id"`
	Title string     `json:"title"`
	Kind  string     `json:"kind"`
	Rule  recur.Rule `json:"rule"`
	// LeadDays are the days before the date on which to notify; 0 is the
	// day itself. [7, 1, 0] means a week ahead, the day before, and the day.
	LeadDays []int    `json:"lead_days"`
	Amount   *float64 `json:"amount"`
	Currency string   `json:"currency"`
	Group    string   `json:"group"`
	Notes    string   `json:"notes"`
	// DoneThrough is the last occurrence the user has dealt with: nothing is
	// said about it, or about any occurrence before it.
	DoneThrough *recur.Date `json:"done_through"`
	// SnoozedUntil holds notifications back until this day; the reminder
	// still shows on the dashboard.
	SnoozedUntil *recur.Date `json:"snoozed_until"`
	Enabled      bool        `json:"enabled"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NeedsAction reports whether a reminder asks for something to be done —
// paying, renewing — as opposed to merely remembered.
func (r Reminder) NeedsAction() bool {
	return r.Kind == ReminderRenewal || r.Kind == ReminderExpiry
}

// What an incident is about.
const (
	SubjectMeter    = "meter"
	SubjectAccount  = "account"
	SubjectReminder = "reminder"
	SubjectChannel  = "channel"
)

// Incident kinds.
const (
	IncidentLevel    = "level"    // a meter is running out
	IncidentBudget   = "budget"   // a meter's monthly spend is past its budget pace
	IncidentStale    = "stale"    // a pushed meter went silent
	IncidentFailing  = "failing"  // an account cannot be sampled
	IncidentDue      = "due"      // a reminder's date is near
	IncidentDelivery = "delivery" // a channel keeps failing to deliver
)

// Reasons a level incident is open, from most to least pressing.
const (
	ReasonEmpty  = "empty"  // nothing left
	ReasonLow    = "low"    // at or below the threshold
	ReasonRunway = "runway" // forecast to run out within the lead time
	ReasonBurst  = "burst"  // a recent burst would empty it far sooner than usual
	ReasonPace   = "pace"   // a quota on course to run out before it resets
)

// Incident states.
const (
	StateOpen     = "open"
	StateResolved = "resolved"
)

// Incident is a condition that held for a while. It opens when the condition
// first holds, and resolves when it stops; notifications happen at those
// edges, and when it gets worse — not on every check in between.
type Incident struct {
	ID          string `json:"id"`
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Kind        string `json:"kind"`
	// Dedup tells apart incidents of one kind on one subject: for a
	// reminder it is the occurrence date.
	Dedup    string   `json:"dedup"`
	Severity Severity `json:"severity"`
	State    string   `json:"state"`
	Reason   string   `json:"reason"`
	// Title is the subject's name when the incident opened, so the history
	// still reads after a rename or a delete.
	Title string `json:"title"`
	// Data are the facts the message was built from.
	Data map[string]any `json:"data"`

	OpenedAt    time.Time  `json:"opened_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ResolvedAt  *time.Time `json:"resolved_at"`
	AckedAt     *time.Time `json:"acked_at"`
	NotifiedAt  *time.Time `json:"notified_at"`
	NotifyCount int        `json:"notify_count"`
}

// Topics let a channel choose what it wants to hear about.
const (
	TopicMeters = "meters"
	TopicDates  = "dates"
	TopicSystem = "system"
	TopicDigest = "digest"
)

// Topics lists every topic.
var Topics = []string{TopicMeters, TopicDates, TopicSystem, TopicDigest}

// TopicOf maps an incident kind to the topic it is delivered under.
func TopicOf(kind string) string {
	switch kind {
	case IncidentLevel, IncidentBudget, IncidentStale:
		return TopicMeters
	case IncidentDue:
		return TopicDates
	default:
		return TopicSystem
	}
}

// Channel is somewhere notifications go.
type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	// Config is driver-specific and holds secrets (webhook URLs, tokens).
	Config      map[string]string `json:"-"`
	Topics      []string          `json:"topics"`
	MinSeverity Severity          `json:"min_severity"`
	Enabled     bool              `json:"enabled"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Wants reports whether the channel should receive a message.
func (c Channel) Wants(topic string, severity Severity) bool {
	if !c.Enabled || severity.Rank() < c.MinSeverity.Rank() {
		return false
	}
	for _, t := range c.Topics {
		if t == topic {
			return true
		}
	}
	return false
}

// Delivery events.
const (
	EventOpened    = "opened"
	EventEscalated = "escalated"
	EventReminder  = "reminder"
	EventResolved  = "resolved"
	EventDigest    = "digest"
	EventTest      = "test"
)

// Delivery statuses.
const (
	DeliveryPending = "pending"
	DeliverySent    = "sent"
	DeliveryFailed  = "failed"  // gave up after retrying
	DeliveryDropped = "dropped" // deliberately not sent: muted, or the channel is gone
)

// Delivery is one message bound for one channel. It is written before it
// is sent and kept afterwards: the outbox and the notification log are the
// same rows.
type Delivery struct {
	ID         int64  `json:"id"`
	IncidentID string `json:"incident_id"`
	Event      string `json:"event"`
	ChannelID  string `json:"channel_id"`
	// Channel is the channel's name when the message was queued.
	Channel string `json:"channel"`
	// Message is the rendered notification, as JSON.
	Message   json.RawMessage `json:"message"`
	Status    string          `json:"status"`
	Attempts  int             `json:"attempts"`
	NotBefore time.Time       `json:"not_before"`
	LastError string          `json:"last_error"`
	CreatedAt time.Time       `json:"created_at"`
	SentAt    *time.Time      `json:"sent_at"`
}

// Token scopes.
const (
	// ScopeRead sees everything except credentials.
	ScopeRead = "read"
	// ScopeAct acknowledges, snoozes and completes, and manages reminders.
	ScopeAct = "act"
	// ScopeIngest pushes readings.
	ScopeIngest = "ingest"
	// ScopeAdmin is everything the dashboard can do.
	ScopeAdmin = "admin"
)

// Scopes lists every scope.
var Scopes = []string{ScopeRead, ScopeAct, ScopeIngest, ScopeAdmin}

// Token is an API credential for a script or an agent. Only its hash is kept.
type Token struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Allows reports whether the token carries a scope; admin carries them all.
func (t Token) Allows(scope string) bool {
	for _, s := range t.Scopes {
		if s == scope || s == ScopeAdmin {
			return true
		}
	}
	return false
}

// Expired reports whether the token is past its expiry.
func (t Token) Expired(now time.Time) bool {
	return t.ExpiresAt != nil && !now.Before(*t.ExpiresAt)
}

const idAlphabet = "0123456789abcdefghjkmnpqrstvwxyz" // Crockford base32, lowercase

var idEncoding = base32.NewEncoding(idAlphabet).WithPadding(base32.NoPadding)

// NewID returns a random identifier such as "mtr_3f9k2m7q1x8d". The prefix
// makes an ID say what it is when it turns up in a log or an agent's tool call.
func NewID(prefix string) string {
	var raw [8]byte
	_, _ = rand.Read(raw[:]) // crypto/rand.Read does not fail
	return prefix + "_" + idEncoding.EncodeToString(raw[:])
}

// Ptr takes the address of a value.
func Ptr[T any](v T) *T { return &v }

// IsCurrency reports whether a unit looks like an ISO currency code — three
// capital letters. Units that do are converted and summed in the spend views;
// everything else ("%", "credits") stays on its own.
func IsCurrency(unit string) bool {
	if len(unit) != 3 {
		return false
	}
	return strings.ToUpper(unit) == unit && strings.Trim(unit, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == ""
}

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/recur"
	"github.com/itswl/quotapulse/internal/store"
)

// Everything a person can change goes through this file. The rules for what
// is acceptable live here once, whether the change arrives from the
// dashboard, an API token, an MCP tool or an imported bundle.

const (
	maxNameLength  = 80
	maxNotesLength = 2000
	maxLeadDays    = 12
)

// cleanName trims a name and checks that it is usable as one.
func cleanName(what, value string) (string, error) {
	value = strings.TrimSpace(value)
	switch {
	case value == "":
		return "", invalid("%s is required", what)
	case utf8.RuneCountInString(value) > maxNameLength:
		return "", invalid("%s is too long (at most %d characters)", what, maxNameLength)
	case strings.ContainsAny(value, "\n\r\t"):
		return "", invalid("%s must be a single line", what)
	}
	return value, nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// ---------- accounts ----------

// AccountInput creates or edits an account. Nil fields are left unchanged.
type AccountInput struct {
	Name     *string `json:"name"`
	Provider *string `json:"provider"`
	// Credential holds the provider's fields. On an edit, a secret field left
	// empty keeps its stored value: the dashboard cannot show it to send back.
	Credential map[string]string `json:"credential"`
	Group      *string           `json:"group"`
	Interval   *int              `json:"interval_seconds"`
	Enabled    *bool             `json:"enabled"`
}

// mergeCredential combines what was typed with what is stored.
func mergeCredential(def provider.Definition, stored, typed map[string]string) map[string]string {
	secrets := def.SecretKeys()
	out := map[string]string{}
	for _, f := range def.Fields {
		value, given := typed[f.Key]
		value = strings.TrimSpace(value)
		switch {
		case value != "":
			out[f.Key] = value
		case secrets[f.Key] || !given:
			if kept := stored[f.Key]; kept != "" {
				out[f.Key] = kept
			}
		}
	}
	return out
}

func checkInterval(seconds int) error {
	if seconds != 0 && (seconds < core.MinSampleSeconds || seconds > core.MaxSampleSeconds) {
		return invalid("interval_seconds must be 0 (the default) or between %d and %d", core.MinSampleSeconds, core.MaxSampleSeconds)
	}
	return nil
}

// CreateAccount adds an account. It is sampled for the first time within
// seconds, by the background loop.
func (e *Engine) CreateAccount(ctx context.Context, in AccountInput, lang string) (AccountView, error) {
	var account core.Account
	var err error
	if in.Name == nil || in.Provider == nil {
		return AccountView{}, invalid("name and provider are required")
	}
	if account.Name, err = cleanName("name", *in.Name); err != nil {
		return AccountView{}, err
	}
	def, err := e.Definition(ctx, strings.TrimSpace(*in.Provider))
	if errors.Is(err, store.ErrNotFound) {
		return AccountView{}, invalid("there is no provider called %q", *in.Provider)
	} else if err != nil {
		return AccountView{}, err
	}
	account.Provider = def.ID
	if account.Credential, err = def.CheckCredential(mergeCredential(def, nil, in.Credential)); err != nil {
		return AccountView{}, invalid("%s", err.Error())
	}
	if in.Group != nil {
		account.Group = strings.TrimSpace(*in.Group)
	}
	if in.Interval != nil {
		if err := checkInterval(*in.Interval); err != nil {
			return AccountView{}, err
		}
		account.Interval = *in.Interval
	}
	account.Enabled = in.Enabled == nil || *in.Enabled
	if err := e.store.CreateAccount(ctx, &account, e.now()); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return AccountView{}, invalid("an account named %q already exists", account.Name)
		}
		return AccountView{}, err
	}
	e.hub.Publish(ChangedMeters)
	e.nudge()
	return e.AccountView(ctx, account.ID, lang)
}

// UpdateAccount edits an account. The provider cannot be changed: its meters
// belong to the definition that produced them.
func (e *Engine) UpdateAccount(ctx context.Context, id string, in AccountInput, lang string) (AccountView, error) {
	account, err := e.store.Account(ctx, id)
	if err != nil {
		return AccountView{}, err
	}
	if in.Provider != nil && strings.TrimSpace(*in.Provider) != account.Provider {
		return AccountView{}, invalid("an account's provider cannot be changed; add a new account instead")
	}
	if in.Name != nil {
		if account.Name, err = cleanName("name", *in.Name); err != nil {
			return AccountView{}, err
		}
	}
	credentialChanged := false
	if in.Credential != nil {
		def, err := e.Definition(ctx, account.Provider)
		if err != nil {
			return AccountView{}, invalid("the provider %q is no longer defined", account.Provider)
		}
		merged, err := def.CheckCredential(mergeCredential(def, account.Credential, in.Credential))
		if err != nil {
			return AccountView{}, invalid("%s", err.Error())
		}
		account.Credential, credentialChanged = merged, true
	} else if account.Unreadable {
		return AccountView{}, invalid("the saved credential cannot be read; enter it again")
	}
	if in.Group != nil {
		account.Group = strings.TrimSpace(*in.Group)
	}
	if in.Interval != nil {
		if err := checkInterval(*in.Interval); err != nil {
			return AccountView{}, err
		}
		account.Interval = *in.Interval
	}
	wasEnabled := account.Enabled
	if in.Enabled != nil {
		account.Enabled = *in.Enabled
	}
	if err := e.store.UpdateAccount(ctx, account, e.now()); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return AccountView{}, invalid("an account named %q already exists", account.Name)
		}
		return AccountView{}, err
	}

	if !account.Enabled {
		// A disabled account is not watched: it cannot be failing, and what
		// its meters last said is no longer a live warning.
		if err := e.closeAccount(ctx, account.ID); err != nil {
			return AccountView{}, err
		}
	}
	if credentialChanged || (account.Enabled && !wasEnabled) {
		_ = e.RequestSample(ctx, account.ID) // a new key deserves an immediate verdict
	}
	e.hub.Publish(ChangedMeters)
	return e.AccountView(ctx, account.ID, lang)
}

// closeAccount quietly ends everything open on an account and its meters.
func (e *Engine) closeAccount(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	meters, err := e.store.MetersOf(ctx, id)
	if err != nil {
		return err
	}
	if err := e.closeSubject(ctx, core.SubjectAccount, id); err != nil {
		return err
	}
	for _, m := range meters {
		if err := e.closeSubject(ctx, core.SubjectMeter, m.ID); err != nil {
			return err
		}
	}
	e.hub.Publish(ChangedIncidents)
	return nil
}

// DeleteAccount removes an account, its meters and their history.
func (e *Engine) DeleteAccount(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	meters, err := e.store.MetersOf(ctx, id)
	if err != nil {
		return err
	}
	if err := e.store.DeleteAccount(ctx, id); err != nil {
		return err
	}
	if err := e.closeSubject(ctx, core.SubjectAccount, id); err != nil {
		return err
	}
	for _, m := range meters {
		if err := e.closeSubject(ctx, core.SubjectMeter, m.ID); err != nil {
			return err
		}
	}
	e.hub.Publish(ChangedMeters)
	e.hub.Publish(ChangedIncidents)
	return nil
}

// TestInput tries a credential without saving anything.
type TestInput struct {
	Provider   string            `json:"provider"`
	Credential map[string]string `json:"credential"`
	// AccountID lets an edit form test with the stored secrets it cannot see.
	AccountID string `json:"account_id"`
	// Definition tests a definition that has not been saved yet.
	Definition *provider.Definition `json:"definition"`
}

// TestResult is what a test fetch found.
type TestResult struct {
	OK        bool              `json:"ok"`
	Error     string            `json:"error,omitempty"`
	Status    int               `json:"status"`
	LatencyMs int64             `json:"latency_ms"`
	Samples   []provider.Sample `json:"samples"`
	// Body is the platform's response, for writing a definition against.
	Body any `json:"body,omitempty"`
}

// maxTestBody caps how much of a response is echoed back to the dashboard.
const maxTestBody = 64 << 10

// TestAccount performs a definition's request once and reports what came back.
func (e *Engine) TestAccount(ctx context.Context, in TestInput) (TestResult, error) {
	var def provider.Definition
	var err error
	if in.Definition != nil {
		def = *in.Definition
		def.Builtin = false
		if err := def.Validate(); err != nil {
			return TestResult{}, invalid("%s", err.Error())
		}
	} else if def, err = e.Definition(ctx, strings.TrimSpace(in.Provider)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TestResult{}, invalid("there is no provider called %q", in.Provider)
		}
		return TestResult{}, err
	}
	var stored map[string]string
	if in.AccountID != "" {
		account, err := e.store.Account(ctx, in.AccountID)
		if err != nil {
			return TestResult{}, err
		}
		stored = account.Credential
	}
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return TestResult{}, err
	}
	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	client := *e.fetch
	client.Now, client.Location = e.now, settings.Location()
	result, fetchErr := client.Fetch(fetchCtx, def, mergeCredential(def, stored, in.Credential))

	out := TestResult{OK: fetchErr == nil, Status: result.Status, LatencyMs: result.Latency.Milliseconds(), Samples: result.Samples}
	if out.Samples == nil {
		out.Samples = []provider.Sample{}
	}
	if fetchErr != nil {
		out.Error = fetchErr.Error()
	}
	if raw, err := json.Marshal(result.Body); err == nil && len(raw) <= maxTestBody {
		out.Body = result.Body
	}
	return out, nil
}

// ---------- meters ----------

// MeterInput edits a meter. Nil fields are left unchanged; Rules, when
// given, replaces the meter's rules as a whole.
type MeterInput struct {
	Name   *string     `json:"name"`
	Rules  *core.Rules `json:"rules"`
	Hidden *bool       `json:"hidden"`
	Sort   *int        `json:"sort"`
	// Unit can be corrected on a pushed meter; a sampled one takes its unit
	// from the provider definition.
	Unit *string `json:"unit"`
}

func checkRules(r core.Rules) error {
	switch {
	case r.Low != nil && !finite(*r.Low):
		return invalid("rules.low must be a number")
	case r.RunwayDays != nil && (!finite(*r.RunwayDays) || *r.RunwayDays < 0 || *r.RunwayDays > 365):
		return invalid("rules.runway_days must be between 0 and 365")
	case r.Budget != nil && (!finite(*r.Budget) || *r.Budget < 0):
		return invalid("rules.budget cannot be negative")
	case r.StaleAfter != nil && *r.StaleAfter != 0 && *r.StaleAfter < 60:
		return invalid("rules.stale_after_seconds must be at least 60")
	}
	return nil
}

// UpdateMeter edits a meter and judges it again under its new rules.
func (e *Engine) UpdateMeter(ctx context.Context, id string, in MeterInput, lang string) (MeterView, error) {
	meter, err := e.store.Meter(ctx, id)
	if err != nil {
		return MeterView{}, err
	}
	if in.Name != nil {
		name, err := cleanName("name", *in.Name)
		if err != nil {
			return MeterView{}, err
		}
		// Naming it is what counts, even when the name typed is the one the
		// provider gave: until now it may have been shown under its account's.
		shown, err := e.meterTitle(ctx, meter)
		if err != nil {
			return MeterView{}, err
		}
		if name != meter.Name || name != shown {
			meter.Name, meter.Renamed = name, true
		}
	}
	if in.Rules != nil {
		if err := checkRules(*in.Rules); err != nil {
			return MeterView{}, err
		}
		meter.Rules = in.Rules.For(meter.Kind, meter.AccountID == "")
		// Zeroes that mean "off" are stored as absent, so the form shows an
		// empty field rather than a 0 that looks like a threshold.
		if meter.Rules.Budget != nil && *meter.Rules.Budget == 0 {
			meter.Rules.Budget = nil
		}
		if meter.Rules.StaleAfter != nil && *meter.Rules.StaleAfter == 0 {
			meter.Rules.StaleAfter = nil
		}
	}
	if in.Hidden != nil {
		meter.Hidden = *in.Hidden
	}
	if in.Sort != nil {
		meter.Sort = *in.Sort
	}
	if in.Unit != nil {
		if meter.AccountID != "" {
			return MeterView{}, invalid("this meter's unit comes from its provider definition")
		}
		meter.Unit = strings.TrimSpace(*in.Unit)
	}
	if err := e.saveAndJudge(ctx, meter); err != nil {
		return MeterView{}, err
	}
	return e.MeterView(ctx, id, lang)
}

// MuteMeter silences a meter's notifications until a moment; nil unmutes.
// Its incidents keep opening and resolving on the dashboard.
func (e *Engine) MuteMeter(ctx context.Context, id string, until *time.Time, lang string) (MeterView, error) {
	meter, err := e.store.Meter(ctx, id)
	if err != nil {
		return MeterView{}, err
	}
	if until != nil && !until.After(e.now()) {
		return MeterView{}, invalid("until must be in the future")
	}
	meter.MutedUntil = until
	if err := e.saveAndJudge(ctx, meter); err != nil {
		return MeterView{}, err
	}
	return e.MeterView(ctx, id, lang)
}

func (e *Engine) saveAndJudge(ctx context.Context, meter core.Meter) error {
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.store.UpdateMeter(ctx, meter, e.now()); err != nil {
		return err
	}
	if err := e.judgeMeter(ctx, meter, settings); err != nil {
		return err
	}
	e.hub.Publish(ChangedMeters)
	return nil
}

// DeleteMeter removes a meter and its history. A meter that its provider
// still reports would simply come back at the next sample, so only pushed
// meters, and ones the provider has stopped reporting, can be deleted; hide
// the others.
func (e *Engine) DeleteMeter(ctx context.Context, id string) error {
	meter, err := e.store.Meter(ctx, id)
	if err != nil {
		return err
	}
	if meter.AccountID != "" {
		account, err := e.store.Account(ctx, meter.AccountID)
		if err != nil {
			return err
		}
		if meter.SampledAt != nil && account.SucceededAt != nil && !meter.SampledAt.Before(*account.SucceededAt) {
			return invalid("this meter is still reported by its account; hide it instead")
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.store.DeleteMeter(ctx, id); err != nil {
		return err
	}
	if err := e.closeSubject(ctx, core.SubjectMeter, id); err != nil {
		return err
	}
	e.hub.Publish(ChangedMeters)
	e.hub.Publish(ChangedIncidents)
	return nil
}

// ---------- reminders ----------

// ReminderInput creates or edits a reminder. Nil fields are left unchanged.
type ReminderInput struct {
	Title    *string     `json:"title"`
	Kind     *string     `json:"kind"`
	Rule     *recur.Rule `json:"rule"`
	LeadDays *[]int      `json:"lead_days"`
	// Amount of 0 clears the amount.
	Amount   *float64 `json:"amount"`
	Currency *string  `json:"currency"`
	Group    *string  `json:"group"`
	Notes    *string  `json:"notes"`
	Enabled  *bool    `json:"enabled"`
}

func (e *Engine) applyReminder(r *core.Reminder, in ReminderInput, s core.Settings) error {
	var err error
	if in.Title != nil {
		if r.Title, err = cleanName("title", *in.Title); err != nil {
			return err
		}
	}
	if in.Kind != nil {
		if !slices.Contains(core.ReminderKinds, *in.Kind) {
			return invalid("kind must be one of %s", strings.Join(core.ReminderKinds, ", "))
		}
		r.Kind = *in.Kind
	}
	if in.Rule != nil {
		if err := in.Rule.Validate(); err != nil {
			return invalid("rule: %s", err.Error())
		}
		if in.Rule.Since < 0 || in.Rule.Since > 9999 {
			return invalid("rule: since must be a year")
		}
		if !ruleEqual(r.Rule, *in.Rule) {
			// A different rule has different occurrences; a mark made
			// against the old ones means nothing now.
			r.DoneThrough = nil
		}
		r.Rule = *in.Rule
	}
	if in.LeadDays != nil {
		leads := append([]int(nil), *in.LeadDays...)
		sort.Sort(sort.Reverse(sort.IntSlice(leads)))
		leads = slices.Compact(leads)
		if len(leads) == 0 {
			leads = []int{0}
		}
		if len(leads) > maxLeadDays {
			return invalid("at most %d lead days", maxLeadDays)
		}
		for _, lead := range leads {
			if lead < 0 || lead > 366 {
				return invalid("lead_days must be between 0 and 366")
			}
		}
		r.LeadDays = leads
	}
	if in.Amount != nil {
		switch {
		case !finite(*in.Amount) || *in.Amount < 0:
			return invalid("amount cannot be negative")
		case *in.Amount == 0:
			r.Amount = nil
		default:
			r.Amount = in.Amount
		}
	}
	if in.Currency != nil {
		r.Currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
	}
	if r.Amount != nil {
		if r.Currency == "" {
			r.Currency = s.BaseCurrency
		}
		if !core.IsCurrency(r.Currency) {
			return invalid("currency must be a three-letter code such as USD")
		}
	}
	if in.Group != nil {
		r.Group = strings.TrimSpace(*in.Group)
	}
	if in.Notes != nil {
		if utf8.RuneCountInString(*in.Notes) > maxNotesLength {
			return invalid("notes are too long (at most %d characters)", maxNotesLength)
		}
		r.Notes = strings.TrimSpace(*in.Notes)
	}
	if in.Enabled != nil {
		r.Enabled = *in.Enabled
	}
	return nil
}

func ruleEqual(a, b recur.Rule) bool {
	a.Since, b.Since = 0, 0 // the origin year does not move any occurrence
	return sameData(toMap(a), toMap(b))
}

// CreateReminder adds a reminder.
func (e *Engine) CreateReminder(ctx context.Context, in ReminderInput, lang string) (ReminderView, error) {
	if in.Title == nil || in.Rule == nil {
		return ReminderView{}, invalid("title and rule are required")
	}
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return ReminderView{}, err
	}
	r := core.Reminder{Kind: core.ReminderCustom, LeadDays: []int{0}, Enabled: true}
	if err := e.applyReminder(&r, in, settings); err != nil {
		return ReminderView{}, err
	}
	if err := e.store.CreateReminder(ctx, &r, e.now()); err != nil {
		return ReminderView{}, err
	}
	return e.afterReminder(ctx, r, settings, lang)
}

// UpdateReminder edits a reminder.
func (e *Engine) UpdateReminder(ctx context.Context, id string, in ReminderInput, lang string) (ReminderView, error) {
	return e.changeReminder(ctx, id, lang, func(r *core.Reminder, s core.Settings) error {
		return e.applyReminder(r, in, s)
	})
}

// CompleteReminder marks the occurrence a reminder is currently about as
// dealt with: it stops reminding, and the reminder moves on to the next one.
func (e *Engine) CompleteReminder(ctx context.Context, id, lang string) (ReminderView, error) {
	return e.changeReminder(ctx, id, lang, func(r *core.Reminder, s core.Settings) error {
		occ, ok := current(*r, dateOf(e.now().In(s.Location())))
		if !ok {
			return invalid("this reminder has no occurrence ahead")
		}
		r.DoneThrough = &occ.Date
		return nil
	})
}

// ReopenReminder takes back the last "done" mark.
func (e *Engine) ReopenReminder(ctx context.Context, id, lang string) (ReminderView, error) {
	return e.changeReminder(ctx, id, lang, func(r *core.Reminder, s core.Settings) error {
		if r.DoneThrough == nil {
			return invalid("nothing is marked as done")
		}
		// Step back to the occurrence before, so earlier ones stay done.
		if previous, ok := r.Rule.Prev(*r.DoneThrough); ok {
			r.DoneThrough = &previous
		} else {
			r.DoneThrough = nil
		}
		return nil
	})
}

// SnoozeReminder holds a reminder's notifications back until a day; nil
// wakes it.
func (e *Engine) SnoozeReminder(ctx context.Context, id string, until *recur.Date, lang string) (ReminderView, error) {
	return e.changeReminder(ctx, id, lang, func(r *core.Reminder, s core.Settings) error {
		if until != nil && until.Before(dateOf(e.now().In(s.Location()))) {
			return invalid("until must be today or later")
		}
		r.SnoozedUntil = until
		return nil
	})
}

func (e *Engine) changeReminder(ctx context.Context, id, lang string, change func(*core.Reminder, core.Settings) error) (ReminderView, error) {
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return ReminderView{}, err
	}
	r, err := e.store.Reminder(ctx, id)
	if err != nil {
		return ReminderView{}, err
	}
	if err := change(&r, settings); err != nil {
		return ReminderView{}, err
	}
	if err := e.store.UpdateReminder(ctx, r, e.now()); err != nil {
		return ReminderView{}, err
	}
	// Reload: the store stamps updated_at, and judging depends on it.
	if r, err = e.store.Reminder(ctx, id); err != nil {
		return ReminderView{}, err
	}
	return e.afterReminder(ctx, r, settings, lang)
}

func (e *Engine) afterReminder(ctx context.Context, r core.Reminder, s core.Settings, lang string) (ReminderView, error) {
	e.mu.Lock()
	_, err := e.judgeReminder(ctx, r, s)
	e.mu.Unlock()
	if err != nil {
		return ReminderView{}, err
	}
	e.hub.Publish(ChangedReminders)
	e.hub.Publish(ChangedIncidents)
	return e.ReminderView(ctx, r.ID, lang)
}

// DeleteReminder removes a reminder.
func (e *Engine) DeleteReminder(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.store.DeleteReminder(ctx, id); err != nil {
		return err
	}
	if err := e.closeSubject(ctx, core.SubjectReminder, id); err != nil {
		return err
	}
	e.hub.Publish(ChangedReminders)
	e.hub.Publish(ChangedIncidents)
	return nil
}

// ---------- channels ----------

// ChannelInput creates or edits a channel. Nil fields are left unchanged.
type ChannelInput struct {
	Name *string `json:"name"`
	Type *string `json:"type"`
	// Config holds the type's settings. On an edit, a secret left empty
	// keeps its stored value.
	Config      map[string]string `json:"config"`
	Topics      *[]string         `json:"topics"`
	MinSeverity *string           `json:"min_severity"`
	Enabled     *bool             `json:"enabled"`
}

// ChannelView is a channel as the dashboard shows it, secrets masked.
type ChannelView struct {
	core.Channel
	TypeName string              `json:"type_name"`
	Config   map[string]string   `json:"config"`
	Health   store.ChannelHealth `json:"health"`
	// Device identifies the browser behind a webpush channel without giving
	// its subscription away; see DeviceOf. Empty for every other type.
	Device string `json:"device,omitempty"`
}

// DeviceOf is the fingerprint of a push subscription: the first half of the
// SHA-256 of its endpoint, in hex. A browser hashes its own endpoint the
// same way to learn which channel in the list is itself — also after its
// subscription was carried over from another install, when nothing in the
// browser remembers a channel ID.
func DeviceOf(endpoint string) string {
	sum := sha256.Sum256([]byte(endpoint))
	return hex.EncodeToString(sum[:16])
}

func mergeConfig(t notify.Type, stored, typed map[string]string) map[string]string {
	out := map[string]string{}
	for _, f := range t.Fields {
		value, given := typed[f.Key]
		value = strings.TrimSpace(value)
		switch {
		case value != "":
			out[f.Key] = value
		case f.Secret || !given:
			if kept := stored[f.Key]; kept != "" {
				out[f.Key] = kept
			}
		}
	}
	return out
}

func (e *Engine) applyChannel(c *core.Channel, in ChannelInput) error {
	var err error
	if in.Name != nil {
		if c.Name, err = cleanName("name", *in.Name); err != nil {
			return err
		}
	}
	if in.Type != nil && *in.Type != c.Type {
		if c.Type != "" {
			return invalid("a channel's type cannot be changed; add a new channel instead")
		}
		c.Type = *in.Type
	}
	t, ok := notify.TypeOf(c.Type)
	if !ok {
		return invalid("there is no channel type called %q", c.Type)
	}
	if in.Config != nil || c.Config == nil {
		config, err := t.CheckConfig(mergeConfig(t, c.Config, in.Config))
		if err != nil {
			return invalid("%s", err.Error())
		}
		c.Config = config
	}
	if in.Topics != nil {
		topics := []string{}
		for _, topic := range core.Topics { // keep the canonical order
			if slices.Contains(*in.Topics, topic) {
				topics = append(topics, topic)
			}
		}
		if len(topics) != len(slices.Compact(slices.Sorted(slices.Values(*in.Topics)))) {
			return invalid("topics must be among %s", strings.Join(core.Topics, ", "))
		}
		c.Topics = topics
	}
	if in.MinSeverity != nil {
		severity := core.Severity(*in.MinSeverity)
		if !severity.Valid() {
			return invalid("min_severity must be info, warning or critical")
		}
		c.MinSeverity = severity
	}
	if in.Enabled != nil {
		c.Enabled = *in.Enabled
	}
	return nil
}

// Channels lists the channels with how their recent deliveries went.
func (e *Engine) Channels(ctx context.Context, lang string) ([]ChannelView, error) {
	channels, err := e.store.Channels(ctx)
	if err != nil {
		return nil, err
	}
	health, err := e.store.ChannelsHealth(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ChannelView, 0, len(channels))
	for _, c := range channels {
		view := ChannelView{Channel: c, TypeName: c.Type, Config: map[string]string{}, Health: health[c.ID]}
		if c.Type == notify.TypeWebPush {
			view.Device = DeviceOf(c.Config["endpoint"])
		}
		if t, ok := notify.TypeOf(c.Type); ok {
			view.Config = t.MaskConfig(c.Config)
			view.TypeName = t.Name
			if lang == "zh" && t.NameZH != "" {
				view.TypeName = t.NameZH
			}
		}
		out = append(out, view)
	}
	return out, nil
}

func (e *Engine) channelView(ctx context.Context, id, lang string) (ChannelView, error) {
	channels, err := e.Channels(ctx, lang)
	if err != nil {
		return ChannelView{}, err
	}
	for _, c := range channels {
		if c.ID == id {
			return c, nil
		}
	}
	return ChannelView{}, ErrNotFound
}

// CreateChannel adds a channel. A new channel hears about everything unless
// told otherwise.
func (e *Engine) CreateChannel(ctx context.Context, in ChannelInput, lang string) (ChannelView, error) {
	if in.Name == nil || in.Type == nil {
		return ChannelView{}, invalid("name and type are required")
	}
	if t, ok := notify.TypeOf(*in.Type); ok && t.Managed {
		return ChannelView{}, invalid("a %s channel is created by the browser itself, from the dashboard", t.ID)
	}
	c := core.Channel{Topics: append([]string(nil), core.Topics...), MinSeverity: core.Info, Enabled: true}
	if err := e.applyChannel(&c, in); err != nil {
		return ChannelView{}, err
	}
	if err := e.store.CreateChannel(ctx, &c, e.now()); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return ChannelView{}, invalid("a channel named %q already exists", c.Name)
		}
		return ChannelView{}, err
	}
	e.hub.Publish(ChangedSettings)
	return e.channelView(ctx, c.ID, lang)
}

// UpdateChannel edits a channel.
func (e *Engine) UpdateChannel(ctx context.Context, id string, in ChannelInput, lang string) (ChannelView, error) {
	c, err := e.store.Channel(ctx, id)
	if err != nil {
		return ChannelView{}, err
	}
	if err := e.applyChannel(&c, in); err != nil {
		return ChannelView{}, err
	}
	if err := e.store.UpdateChannel(ctx, c, e.now()); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return ChannelView{}, invalid("a channel named %q already exists", c.Name)
		}
		return ChannelView{}, err
	}
	e.hub.Publish(ChangedSettings)
	return e.channelView(ctx, id, lang)
}

// DeleteChannel removes a channel.
func (e *Engine) DeleteChannel(ctx context.Context, id string) error {
	if err := e.store.DeleteChannel(ctx, id); err != nil {
		return err
	}
	e.mu.Lock()
	err := e.closeSubject(ctx, core.SubjectChannel, id)
	e.mu.Unlock()
	e.hub.Publish(ChangedSettings)
	e.hub.Publish(ChangedIncidents)
	return err
}

// ---------- tokens ----------

// TokenInput creates a token.
type TokenInput struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// NewToken is a token together with its plaintext, which exists only in
// this one response.
type NewToken struct {
	core.Token
	Secret string `json:"token"`
}

// Tokens lists the API tokens.
func (e *Engine) Tokens(ctx context.Context) ([]core.Token, error) { return e.store.Tokens(ctx) }

// CreateToken mints an API token.
func (e *Engine) CreateToken(ctx context.Context, in TokenInput) (NewToken, error) {
	name, err := cleanName("name", in.Name)
	if err != nil {
		return NewToken{}, err
	}
	scopes := []string{}
	for _, scope := range core.Scopes { // canonical order, duplicates dropped
		if slices.Contains(in.Scopes, scope) {
			scopes = append(scopes, scope)
		}
	}
	for _, scope := range in.Scopes {
		if !slices.Contains(core.Scopes, scope) {
			return NewToken{}, invalid("scopes must be among %s", strings.Join(core.Scopes, ", "))
		}
	}
	if len(scopes) == 0 {
		return NewToken{}, invalid("a token needs at least one scope")
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(e.now()) {
		return NewToken{}, invalid("expires_at must be in the future")
	}
	token := core.Token{Name: name, Scopes: scopes, ExpiresAt: in.ExpiresAt}
	plaintext, err := e.store.CreateToken(ctx, &token, e.now())
	if errors.Is(err, store.ErrConflict) {
		return NewToken{}, invalid("a token named %q already exists", name)
	} else if err != nil {
		return NewToken{}, err
	}
	return NewToken{Token: token, Secret: plaintext}, nil
}

// DeleteToken revokes a token.
func (e *Engine) DeleteToken(ctx context.Context, id string) error {
	return e.store.DeleteToken(ctx, id)
}

// Authenticate resolves an API token.
func (e *Engine) Authenticate(ctx context.Context, plaintext string) (core.Token, bool) {
	return e.store.Authenticate(ctx, plaintext, e.now())
}

// ---------- provider definitions ----------

// SaveProvider stores a custom definition.
func (e *Engine) SaveProvider(ctx context.Context, def provider.Definition) (provider.Definition, error) {
	def.ID = strings.TrimSpace(def.ID)
	if _, builtin := provider.Builtin(def.ID); builtin {
		return def, invalid("%q is a built-in provider; save your version under another id", def.ID)
	}
	if def.Fields == nil {
		def.Fields = []provider.Field{}
	}
	if err := def.Validate(); err != nil {
		return def, invalid("%s", err.Error())
	}
	if err := e.store.SaveProvider(ctx, def, e.now()); err != nil {
		return def, err
	}
	e.hub.Publish(ChangedSettings)
	return e.store.CustomProvider(ctx, def.ID)
}

// DeleteProvider removes a custom definition that no account uses.
func (e *Engine) DeleteProvider(ctx context.Context, id string) error {
	if _, builtin := provider.Builtin(id); builtin {
		return invalid("built-in providers cannot be deleted")
	}
	inUse, err := e.store.ProviderInUse(ctx, id)
	if err != nil {
		return err
	}
	if inUse > 0 {
		return invalid("%d account(s) still use this provider", inUse)
	}
	if err := e.store.DeleteProvider(ctx, id); err != nil {
		return err
	}
	e.hub.Publish(ChangedSettings)
	return nil
}

// ---------- settings ----------

// Settings returns the settings.
func (e *Engine) Settings(ctx context.Context) (core.Settings, error) { return e.store.Settings(ctx) }

// LearnPublicURL sets the address notifications link to, if none has been
// set, and reports whether it did. It never replaces one that is there.
func (e *Engine) LearnPublicURL(ctx context.Context, address string) (bool, error) {
	s, err := e.store.Settings(ctx)
	if err != nil || s.PublicURL != "" {
		return false, err
	}
	s.PublicURL = strings.TrimRight(strings.TrimSpace(address), "/")
	if err := e.store.SaveSettings(ctx, s); err != nil {
		return false, err
	}
	e.hub.Publish(ChangedSettings)
	return true, nil
}

// UpdateSettings validates and saves the settings, then judges everything
// again: a changed default can open or close incidents by itself.
func (e *Engine) UpdateSettings(ctx context.Context, s core.Settings) (core.Settings, error) {
	s.Timezone = strings.TrimSpace(s.Timezone)
	s.BaseCurrency = strings.ToUpper(strings.TrimSpace(s.BaseCurrency))
	s.PublicURL = strings.TrimRight(strings.TrimSpace(s.PublicURL), "/")
	fx := map[string]float64{}
	for code, rate := range s.FX {
		fx[strings.ToUpper(strings.TrimSpace(code))] = rate
	}
	delete(fx, s.BaseCurrency) // the base is worth exactly one of itself
	s.FX = fx
	if err := e.store.SaveSettings(ctx, s); err != nil {
		return s, invalid("%s", err.Error())
	}
	if err := e.EvaluateAll(ctx); err != nil {
		return s, err
	}
	if err := e.EvaluateReminders(ctx); err != nil {
		return s, err
	}
	e.hub.Publish(ChangedSettings)
	e.hub.Publish(ChangedMeters)
	return e.store.Settings(ctx)
}

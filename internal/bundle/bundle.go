// Package bundle moves a whole QuotaPulse in and out of one JSON document.
//
// The same format serves three purposes: a backup you can read, a
// declarative starting point for a new install (QUOTAPULSE_BOOTSTRAP), and
// the hand-over format for anything that wants to bring data in — the tool
// that migrates a pre-1.0 database produces exactly this.
//
// A bundle refers to things by name, not by database ID, so it can be written
// by hand and merged into an install that already has data. Whatever already
// exists under the same name is left alone and reported.
package bundle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"quotapulse/internal/core"
	"quotapulse/internal/engine"
	"quotapulse/internal/notify"
	"quotapulse/internal/provider"
	"quotapulse/internal/recur"
	"quotapulse/internal/store"
)

// Format and Version identify the document.
const (
	Format  = "quotapulse-bundle"
	Version = 1
)

// Bundle is the document.
type Bundle struct {
	Format     string    `json:"format"`
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exported_at"`
	// Source says what wrote the bundle.
	Source string `json:"source,omitempty"`
	// Notes are remarks from whatever wrote the bundle, shown on import: a
	// migration lists what it could not carry over.
	Notes []string `json:"notes,omitempty"`

	// Settings may hold any subset of the settings; what is left out keeps
	// its default.
	Settings json.RawMessage `json:"settings,omitempty"`
	// Push is the key pair browser subscriptions are bound to. Without it,
	// the browser channels in the bundle could never be delivered to.
	Push      *notify.VAPID         `json:"push,omitempty"`
	Providers []provider.Definition `json:"providers,omitempty"`
	Channels  []Channel             `json:"channels,omitempty"`
	Accounts  []Account             `json:"accounts,omitempty"`
	Meters    []Meter               `json:"meters,omitempty"`
	Reminders []Reminder            `json:"reminders,omitempty"`
	Tokens    []Token               `json:"tokens,omitempty"`
	Incidents []Incident            `json:"incidents,omitempty"`
}

// Channel is a notification channel. Config is present only in a bundle
// exported with secrets; without it the channel is imported disabled.
type Channel struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Config      map[string]string `json:"config,omitempty"`
	Topics      []string          `json:"topics,omitempty"`
	MinSeverity core.Severity     `json:"min_severity,omitempty"`
	Enabled     bool              `json:"enabled"`
}

// Account is a credential at a provider. Credential is present only in a
// bundle exported with secrets; without it the account is imported disabled.
type Account struct {
	Name       string            `json:"name"`
	Provider   string            `json:"provider"`
	Credential map[string]string `json:"credential,omitempty"`
	Group      string            `json:"group,omitempty"`
	Interval   int               `json:"interval_seconds,omitempty"`
	Enabled    bool              `json:"enabled"`
}

// Meter carries a meter's rules and, optionally, its history. Account names
// the account it belongs to; empty means a pushed meter.
type Meter struct {
	Account string `json:"account,omitempty"`
	Key     string `json:"key"`
	Name    string `json:"name,omitempty"`
	// Renamed says the name is one a person chose, to be shown as it is.
	Renamed bool       `json:"renamed,omitempty"`
	Kind    core.Kind  `json:"kind"`
	Unit    string     `json:"unit,omitempty"`
	Rules   core.Rules `json:"rules"`
	Hidden  bool       `json:"hidden,omitempty"`
	Sort    int        `json:"sort,omitempty"`
	// Readings are [unix seconds, value] or [unix seconds, value, limit],
	// oldest first. Arrays keep a year of history small.
	Readings [][]float64 `json:"readings,omitempty"`
}

// Reminder is a date.
type Reminder struct {
	Title        string      `json:"title"`
	Kind         string      `json:"kind,omitempty"`
	Rule         recur.Rule  `json:"rule"`
	LeadDays     []int       `json:"lead_days,omitempty"`
	Amount       *float64    `json:"amount,omitempty"`
	Currency     string      `json:"currency,omitempty"`
	Group        string      `json:"group,omitempty"`
	Notes        string      `json:"notes,omitempty"`
	DoneThrough  *recur.Date `json:"done_through,omitempty"`
	SnoozedUntil *recur.Date `json:"snoozed_until,omitempty"`
	Enabled      bool        `json:"enabled"`
}

// Token is an API token. A backup carries Hash; something migrating keys
// that are already in use carries the key itself in Token.
type Token struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Hash      string     `json:"hash,omitempty"`
	Prefix    string     `json:"prefix,omitempty"`
	Token     string     `json:"token,omitempty"`
}

// Incident is a closed incident, kept so the activity history survives a
// move. Subject names the account, meter ("account/key", or a pushed
// meter's key), reminder or channel it was about.
type Incident struct {
	SubjectType string         `json:"subject_type"`
	Subject     string         `json:"subject"`
	Kind        string         `json:"kind"`
	Severity    core.Severity  `json:"severity"`
	Reason      string         `json:"reason,omitempty"`
	Title       string         `json:"title"`
	Data        map[string]any `json:"data,omitempty"`
	OpenedAt    time.Time      `json:"opened_at"`
	ResolvedAt  time.Time      `json:"resolved_at"`
}

// ExportOptions decides how much goes into an export.
type ExportOptions struct {
	// Secrets includes credentials and channel settings in the clear. A
	// bundle exported with secrets is as sensitive as the database.
	Secrets bool
	// History includes meter readings and closed incidents.
	History bool
	Source  string
}

// Export writes the current state as a bundle.
func Export(ctx context.Context, e *engine.Engine, opts ExportOptions) (Bundle, error) {
	st := e.Store()
	out := Bundle{Format: Format, Version: Version, ExportedAt: e.Now().UTC().Truncate(time.Second), Source: opts.Source}

	settings, err := st.Settings(ctx)
	if err != nil {
		return out, err
	}
	out.Settings = SettingsOf(settings)
	if out.Providers, err = st.CustomProviders(ctx); err != nil {
		return out, err
	}

	channels, err := st.Channels(ctx)
	if err != nil {
		return out, err
	}
	if opts.Secrets {
		if out.Push, err = e.ExportPushKeys(ctx); err != nil {
			return out, err
		}
	}
	for _, c := range channels {
		// A browser's subscription is all secret and useless without the
		// keys: without secrets there is nothing of it to export.
		if c.Type == notify.TypeWebPush && !opts.Secrets {
			continue
		}
		item := Channel{Name: c.Name, Type: c.Type, Topics: c.Topics, MinSeverity: c.MinSeverity, Enabled: c.Enabled}
		if opts.Secrets {
			item.Config = c.Config
		}
		out.Channels = append(out.Channels, item)
	}

	accounts, err := st.Accounts(ctx)
	if err != nil {
		return out, err
	}
	accountName := map[string]string{}
	for _, a := range accounts {
		accountName[a.ID] = a.Name
		item := Account{Name: a.Name, Provider: a.Provider, Group: a.Group, Interval: a.Interval, Enabled: a.Enabled}
		if opts.Secrets && !a.Unreadable {
			item.Credential = a.Credential
		}
		out.Accounts = append(out.Accounts, item)
	}

	meters, err := st.Meters(ctx)
	if err != nil {
		return out, err
	}
	meterRef := map[string]string{}
	for _, m := range meters {
		item := Meter{
			Account: accountName[m.AccountID], Key: m.Key, Name: m.Name, Renamed: m.Renamed, Kind: m.Kind, Unit: m.Unit,
			Rules: m.Rules, Hidden: m.Hidden, Sort: m.Sort,
		}
		meterRef[m.ID] = meterSubject(item.Account, m.Key)
		if opts.History {
			readings, err := st.Readings(ctx, m.ID, time.Unix(0, 0))
			if err != nil {
				return out, err
			}
			for _, r := range readings {
				row := []float64{float64(r.At.Unix()), r.Value}
				if r.Limit != nil {
					row = append(row, *r.Limit)
				}
				item.Readings = append(item.Readings, row)
			}
		}
		out.Meters = append(out.Meters, item)
	}

	reminders, err := st.Reminders(ctx)
	if err != nil {
		return out, err
	}
	reminderTitle := map[string]string{}
	for _, r := range reminders {
		reminderTitle[r.ID] = r.Title
		out.Reminders = append(out.Reminders, Reminder{
			Title: r.Title, Kind: r.Kind, Rule: r.Rule, LeadDays: r.LeadDays, Amount: r.Amount, Currency: r.Currency,
			Group: r.Group, Notes: r.Notes, DoneThrough: r.DoneThrough, SnoozedUntil: r.SnoozedUntil, Enabled: r.Enabled,
		})
	}

	tokens, err := st.Tokens(ctx)
	if err != nil {
		return out, err
	}
	// A token's hash lets a restore keep it working, and is useless to
	// anyone without the token itself — but it still only goes out when
	// secrets were asked for.
	if opts.Secrets {
		hashes, err := st.TokenHashes(ctx)
		if err != nil {
			return out, err
		}
		for _, t := range tokens {
			out.Tokens = append(out.Tokens, Token{Name: t.Name, Scopes: t.Scopes, ExpiresAt: t.ExpiresAt, Hash: hashes[t.ID], Prefix: t.Prefix})
		}
	}

	if opts.History {
		channelName := map[string]string{}
		for _, c := range channels {
			channelName[c.ID] = c.Name
		}
		closed, err := st.Incidents(ctx, store.IncidentFilter{State: core.StateResolved, Limit: 500})
		if err != nil {
			return out, err
		}
		for i := len(closed) - 1; i >= 0; i-- {
			inc := closed[i]
			subject := ""
			switch inc.SubjectType {
			case core.SubjectAccount:
				subject = accountName[inc.SubjectID]
			case core.SubjectMeter:
				subject = meterRef[inc.SubjectID]
			case core.SubjectReminder:
				subject = reminderTitle[inc.SubjectID]
			case core.SubjectChannel:
				subject = channelName[inc.SubjectID]
			}
			if subject == "" || inc.ResolvedAt == nil {
				continue // about something that no longer exists
			}
			out.Incidents = append(out.Incidents, Incident{
				SubjectType: inc.SubjectType, Subject: subject, Kind: inc.Kind, Severity: inc.Severity,
				Reason: inc.Reason, Title: inc.Title, Data: inc.Data, OpenedAt: inc.OpenedAt, ResolvedAt: *inc.ResolvedAt,
			})
		}
	}
	return out, nil
}

// SettingsOf encodes settings for a bundle.
func SettingsOf(s core.Settings) json.RawMessage {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	return raw
}

// ReadSettings lays a bundle's settings over the defaults, so a hand-written
// bundle can say only what it wants to change. Fields it does not know are
// refused, like everywhere else.
func (b Bundle) ReadSettings() (core.Settings, error) {
	settings := core.DefaultSettings()
	if len(b.Settings) == 0 {
		return settings, nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(b.Settings)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return settings, fmt.Errorf("settings: %w", err)
	}
	if settings.FX == nil {
		settings.FX = map[string]float64{}
	}
	return settings, nil
}

func meterSubject(account, key string) string {
	if account == "" {
		return key
	}
	return account + "/" + key
}

// Report says what an import did.
type Report struct {
	Created map[string]int `json:"created"`
	Skipped map[string]int `json:"skipped"`
	// Notes explain anything that needs a person's attention.
	Notes []string `json:"notes"`
}

func (r *Report) created(kind string) { r.Created[kind]++ }

func (r *Report) skipped(kind, format string, args ...any) {
	r.Skipped[kind]++
	r.Notes = append(r.Notes, fmt.Sprintf(format, args...))
}

func (r *Report) note(format string, args ...any) {
	r.Notes = append(r.Notes, fmt.Sprintf(format, args...))
}

// Summary is the report in one line.
func (r Report) Summary() string {
	var parts []string
	for _, kind := range []string{"providers", "channels", "accounts", "meters", "readings", "reminders", "tokens", "incidents"} {
		if n := r.Created[kind]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, kind))
		}
	}
	if len(parts) == 0 {
		return "nothing new"
	}
	return strings.Join(parts, ", ")
}

// Parse reads and checks a bundle.
func Parse(raw []byte) (Bundle, error) {
	var b Bundle
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&b); err != nil {
		return b, fmt.Errorf("not a valid bundle: %w", err)
	}
	if b.Format != Format {
		return b, fmt.Errorf("not a QuotaPulse bundle (format %q)", b.Format)
	}
	if b.Version < 1 || b.Version > Version {
		return b, fmt.Errorf("bundle version %d is not supported by this build (up to %d)", b.Version, Version)
	}
	return b, nil
}

// ImportOptions decides how a bundle is applied.
type ImportOptions struct {
	// Settings applies the bundle's settings. Usually only wanted on an
	// empty install: an import into a running one should not move its
	// time zone.
	Settings bool
}

// Import merges a bundle into the database. It never overwrites: anything
// that already exists under the same name is skipped and reported.
func Import(ctx context.Context, e *engine.Engine, b Bundle, opts ImportOptions) (Report, error) {
	st := e.Store()
	now := e.Now()
	report := Report{Created: map[string]int{}, Skipped: map[string]int{}, Notes: append([]string{}, b.Notes...)}

	if opts.Settings && len(b.Settings) > 0 {
		settings, err := b.ReadSettings()
		if err != nil {
			return report, err
		}
		if err := st.SaveSettings(ctx, settings); err != nil {
			return report, fmt.Errorf("settings: %w", err)
		}
	}

	for _, def := range b.Providers {
		if _, builtin := provider.Builtin(def.ID); builtin {
			report.skipped("providers", "provider %q: built in, not replaced", def.ID)
			continue
		}
		if _, err := st.CustomProvider(ctx, def.ID); err == nil {
			report.skipped("providers", "provider %q: already exists", def.ID)
			continue
		}
		if err := def.Validate(); err != nil {
			report.skipped("providers", "provider %q: %v", def.ID, err)
			continue
		}
		if err := st.SaveProvider(ctx, def, now); err != nil {
			return report, err
		}
		report.created("providers")
	}

	// Browser subscriptions only work with the keys they were made for.
	browsers := false
	if b.Push != nil {
		var err error
		if browsers, err = e.ImportPushKeys(ctx, *b.Push); err != nil {
			return report, err
		}
		if !browsers {
			report.note("this install already has push keys of its own, so the browser subscriptions in the bundle cannot be used; enable notifications again on each device")
		}
	}

	channelIDs := map[string]string{}
	existingChannels, err := st.Channels(ctx)
	if err != nil {
		return report, err
	}
	for _, c := range existingChannels {
		channelIDs[strings.ToLower(c.Name)] = c.ID
	}
	for _, c := range b.Channels {
		if _, exists := channelIDs[strings.ToLower(c.Name)]; exists {
			report.skipped("channels", "channel %q: already exists", c.Name)
			continue
		}
		if c.Type == notify.TypeWebPush && !browsers {
			if b.Push == nil {
				report.skipped("channels", "channel %q: a browser subscription without its push keys cannot be used", c.Name)
			} else {
				report.Skipped["channels"]++
			}
			continue
		}
		kind, ok := notify.TypeOf(c.Type)
		if !ok {
			report.skipped("channels", "channel %q: unknown type %q", c.Name, c.Type)
			continue
		}
		channel := core.Channel{Name: c.Name, Type: c.Type, Topics: c.Topics, MinSeverity: c.MinSeverity, Enabled: c.Enabled, Config: map[string]string{}}
		if channel.Topics == nil {
			channel.Topics = append([]string(nil), core.Topics...)
		}
		if !channel.MinSeverity.Valid() {
			channel.MinSeverity = core.Info
		}
		if config, err := kind.CheckConfig(c.Config); err == nil {
			channel.Config = config
		} else {
			channel.Enabled = false
			report.note("channel %q: imported disabled — %v", c.Name, err)
		}
		if err := st.CreateChannel(ctx, &channel, now); err != nil {
			return report, err
		}
		channelIDs[strings.ToLower(c.Name)] = channel.ID
		report.created("channels")
	}

	accountIDs := map[string]string{}
	existingAccounts, err := st.Accounts(ctx)
	if err != nil {
		return report, err
	}
	for _, a := range existingAccounts {
		accountIDs[strings.ToLower(a.Name)] = a.ID
	}
	for _, a := range b.Accounts {
		if _, exists := accountIDs[strings.ToLower(a.Name)]; exists {
			report.skipped("accounts", "account %q: already exists", a.Name)
			continue
		}
		def, err := e.Definition(ctx, a.Provider)
		if err != nil {
			report.skipped("accounts", "account %q: there is no provider called %q", a.Name, a.Provider)
			continue
		}
		account := core.Account{
			Name: a.Name, Provider: a.Provider, Group: a.Group, Interval: a.Interval, Enabled: a.Enabled,
			Credential: map[string]string{},
		}
		if credential, err := def.CheckCredential(a.Credential); err == nil {
			account.Credential = credential
		} else {
			account.Enabled = false
			report.note("account %q: imported disabled — %v", a.Name, err)
		}
		if err := st.CreateAccount(ctx, &account, now); err != nil {
			return report, err
		}
		accountIDs[strings.ToLower(a.Name)] = account.ID
		report.created("accounts")
	}

	meterIDs := map[string]string{}
	for _, m := range b.Meters {
		ref := meterSubject(m.Account, m.Key)
		if !m.Kind.Valid() {
			report.skipped("meters", "meter %q: kind must be balance, quota or counter", ref)
			continue
		}
		meter := core.Meter{
			Key: m.Key, Name: m.Name, Renamed: m.Renamed && m.Name != "", Kind: m.Kind, Unit: m.Unit,
			Rules: m.Rules.For(m.Kind, m.Account == ""), Hidden: m.Hidden, Sort: m.Sort,
		}
		if meter.Name == "" {
			meter.Name = m.Key
		}
		var existing core.Meter
		var lookupErr error
		if m.Account == "" {
			existing, lookupErr = st.PushedMeter(ctx, m.Key)
		} else {
			accountID, ok := accountIDs[strings.ToLower(m.Account)]
			if !ok {
				report.skipped("meters", "meter %q: its account is not in the database", ref)
				continue
			}
			meter.AccountID = accountID
			existing, lookupErr = st.MeterByKey(ctx, accountID, m.Key)
		}
		switch {
		case lookupErr == nil:
			meterIDs[ref] = existing.ID
			report.skipped("meters", "meter %q: already exists", ref)
			continue
		case !errors.Is(lookupErr, store.ErrNotFound):
			return report, lookupErr
		}
		if err := st.CreateMeter(ctx, &meter, now); err != nil {
			return report, err
		}
		meterIDs[ref] = meter.ID
		report.created("meters")

		readings := make([]core.Reading, 0, len(m.Readings))
		for _, row := range m.Readings {
			if len(row) < 2 {
				continue
			}
			reading := core.Reading{At: time.Unix(int64(row[0]), 0).UTC(), Value: row[1]}
			if len(row) > 2 {
				limit := row[2]
				reading.Limit = &limit
			}
			readings = append(readings, reading)
		}
		if len(readings) > 0 {
			if err := st.ImportReadings(ctx, &meter, readings); err != nil {
				return report, err
			}
			report.Created["readings"] += len(readings)
		}
	}

	reminderIDs := map[string]string{}
	existingReminders, err := st.Reminders(ctx)
	if err != nil {
		return report, err
	}
	seen := map[string]bool{}
	for _, r := range existingReminders {
		reminderIDs[r.Title] = r.ID
		seen[reminderKey(r.Title, r.Rule)] = true
	}
	for _, r := range b.Reminders {
		if err := r.Rule.Validate(); err != nil {
			report.skipped("reminders", "reminder %q: %v", r.Title, err)
			continue
		}
		if seen[reminderKey(r.Title, r.Rule)] {
			report.skipped("reminders", "reminder %q: already exists", r.Title)
			continue
		}
		reminder := core.Reminder{
			Title: r.Title, Kind: r.Kind, Rule: r.Rule, LeadDays: r.LeadDays, Amount: r.Amount, Currency: r.Currency,
			Group: r.Group, Notes: r.Notes, DoneThrough: r.DoneThrough, SnoozedUntil: r.SnoozedUntil, Enabled: r.Enabled,
		}
		if reminder.Kind == "" {
			reminder.Kind = core.ReminderCustom
		}
		if len(reminder.LeadDays) == 0 {
			reminder.LeadDays = []int{0}
		}
		if err := st.CreateReminder(ctx, &reminder, now); err != nil {
			return report, err
		}
		reminderIDs[r.Title] = reminder.ID
		seen[reminderKey(r.Title, r.Rule)] = true
		report.created("reminders")
	}

	for _, t := range b.Tokens {
		token := core.Token{Name: t.Name, Scopes: t.Scopes, ExpiresAt: t.ExpiresAt}
		var err error
		switch {
		case t.Token != "":
			err = st.ImportToken(ctx, &token, t.Token, now)
		case t.Hash != "":
			err = st.RestoreToken(ctx, &token, t.Hash, t.Prefix, now)
		default:
			report.skipped("tokens", "token %q: the bundle carries neither the token nor its hash", t.Name)
			continue
		}
		if errors.Is(err, store.ErrConflict) {
			report.skipped("tokens", "token %q: already exists", t.Name)
			continue
		} else if err != nil {
			return report, err
		}
		report.created("tokens")
	}

	for _, inc := range b.Incidents {
		var subjectID string
		switch inc.SubjectType {
		case core.SubjectAccount:
			subjectID = accountIDs[strings.ToLower(inc.Subject)]
		case core.SubjectMeter:
			subjectID = meterIDs[inc.Subject]
		case core.SubjectReminder:
			subjectID = reminderIDs[inc.Subject]
		case core.SubjectChannel:
			subjectID = channelIDs[strings.ToLower(inc.Subject)]
		}
		if subjectID == "" || !inc.Severity.Valid() {
			report.Skipped["incidents"]++
			continue
		}
		resolved := inc.ResolvedAt
		err := st.SaveIncident(ctx, core.Incident{
			ID: core.NewID("inc"), SubjectType: inc.SubjectType, SubjectID: subjectID, Kind: inc.Kind,
			Severity: inc.Severity, State: core.StateResolved, Reason: inc.Reason, Title: inc.Title, Data: inc.Data,
			OpenedAt: inc.OpenedAt, UpdatedAt: resolved, ResolvedAt: &resolved,
		})
		if err != nil {
			return report, err
		}
		report.created("incidents")
	}

	// Judge what was imported. Accounts are sampled by the background loop
	// within seconds; this covers pushed meters and dates.
	if err := e.EvaluateAll(ctx); err != nil {
		return report, err
	}
	if err := e.EvaluateReminders(ctx); err != nil {
		return report, err
	}
	e.Changes().Publish(engine.ChangedMeters)
	e.Changes().Publish(engine.ChangedReminders)
	e.Changes().Publish(engine.ChangedSettings)
	return report, nil
}

// reminderKey identifies a reminder for the purpose of not importing it twice.
func reminderKey(title string, rule recur.Rule) string {
	raw, _ := json.Marshal(rule)
	return title + "\x00" + string(raw)
}

// Redacted reports whether a bundle carries no secrets, which decides how
// carefully its file must be handled.
func (b Bundle) Redacted() bool {
	for _, a := range b.Accounts {
		if len(a.Credential) > 0 {
			return false
		}
	}
	for _, c := range b.Channels {
		if len(c.Config) > 0 {
			return false
		}
	}
	for _, t := range b.Tokens {
		if t.Token != "" || t.Hash != "" {
			return false
		}
	}
	return b.Push == nil
}

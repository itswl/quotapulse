package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/itswl/quotapulse/internal/buildinfo"
	"github.com/itswl/quotapulse/internal/bundle"
	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/engine"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/runtime"
	"github.com/itswl/quotapulse/internal/store"
)

// addAdminTools registers what only an admin token may use: everything the
// dashboard can do that read, act and ingest do not already cover. Each
// tool says what it does; the ones that remove something for good are
// marked destructive.
func addAdminTools(t *tools, server *sdk.Server) {
	add(t, server, changing("update_meter", "Change a meter",
		"Change a meter's name, whether it is hidden, its sort order, its unit (for a meter reported from outside) or its rules, and answer with the meter as get_meter shows it. A field left out keeps its value; rules, when sent, replace the old ones as a whole.", false),
		t.updateMeter)
	add(t, server, destructive("delete_meter", "Delete a meter",
		"Delete a meter and its history for good. Only a meter reported from outside, or one its provider has stopped reporting, can be deleted; hide the others with update_meter."),
		t.deleteMeter)

	add(t, server, changing("add_account", "Add an account",
		"Add an account whose balances and quotas are read from a platform. Its credential is the platform's key: it is stored encrypted and shown masked from then on. Try it first with test_account.", false),
		t.addAccount)
	add(t, server, changing("update_account", "Change an account",
		"Change an account's name, credential, group, how often it is read, or whether it is enabled. A field left out keeps its value; a secret left out keeps its stored value.", false),
		t.updateAccount)
	add(t, server, destructive("delete_account", "Delete an account",
		"Delete an account together with its meters and their history. To stop reading it for a while, disable it with update_account."),
		t.deleteAccount)
	add(t, server, readOnly("test_account", "Try a credential",
		"Try a credential against a platform without saving anything, and report what came back: whether it worked, the status, the latency, the meters it found and the raw response."),
		t.testAccount)

	add(t, server, readOnly("list_channels", "Notification channels",
		"The channels notifications go through, with their settings — secrets masked — and how their recent deliveries went."),
		t.listChannels)
	add(t, server, changing("add_channel", "Add a notification channel",
		"Add a channel notifications go through. A new channel hears about everything unless told otherwise.", false),
		t.addChannel)
	add(t, server, changing("update_channel", "Change a notification channel",
		"Change a channel's name, settings, what it hears about, the least severity it wants, or whether it is enabled. A field left out keeps its value; a secret left out keeps its stored value.", false),
		t.updateChannel)
	add(t, server, destructive("delete_channel", "Delete a notification channel",
		"Delete a notification channel for good. To silence it for a while, disable it with update_channel."),
		t.deleteChannel)
	add(t, server, changing("test_channel", "Send a test message",
		"Send a test message through a channel now. A refusal by the platform comes back as the answer rather than as a failure.", true),
		t.testChannel)
	add(t, server, changing("send_digest", "Send the digest now",
		"Send the digest — the periodic summary — now, in the background, instead of waiting for its schedule.", true),
		t.sendDigest)
	add(t, server, changing("retry_delivery", "Send a message again",
		"Queue a failed delivery again, from the notification log.", true),
		t.retryDelivery)

	add(t, server, changing("update_settings", "Change settings",
		"Change the user's settings. Send only what should change: time zone, language, base currency, exchange rates, how often accounts are read, defaults for warnings, reminder time, quiet hours, the digest, retention, how far ahead dates are listed, or the dashboard's public address.", true),
		t.updateSettings)

	add(t, server, readOnly("list_tokens", "API tokens",
		"The API tokens, with what each may do and when it was last used. The tokens themselves are never shown again."),
		t.listTokens)
	add(t, server, changing("create_token", "Create an API token",
		"Create an API token for a script or another agent. The answer carries the token itself this one time: hand it on, and never store it anywhere else.", false),
		t.createToken)
	add(t, server, destructive("revoke_token", "Revoke an API token",
		"Revoke an API token, so that whatever used it can no longer get in."),
		t.revokeToken)

	add(t, server, changing("save_provider", "Save a provider definition",
		"Save a provider definition of the user's own — how to call a platform and how to read the meters out of its answer. A built-in provider cannot be replaced; save a changed copy under another id.", true),
		t.saveProvider)
	add(t, server, destructive("delete_provider", "Delete a provider definition",
		"Delete a provider definition of the user's own. Built-in providers cannot be deleted, and one that an account still uses is refused."),
		t.deleteProvider)

	add(t, server, readOnly("get_system", "System state",
		"The version, whether notifications are muted, how much is stored, and what each background loop last did."),
		t.getSystem)
	add(t, server, changing("backup_now", "Back the database up now",
		"Write a backup of the database now, instead of waiting for the schedule.", false),
		t.backupNow)
	add(t, server, readOnly("export_bundle", "Export everything as a bundle",
		"Export the configuration — settings, providers, channels, accounts, meters, dates — as a bundle, the same file the dashboard's export gives. With secrets it carries credentials in the clear; with history it carries the readings too."),
		t.exportBundle)
	add(t, server, changing("import_bundle", "Import a bundle",
		"Merge a bundle — an export file's contents — into this instance. Nothing that already exists under the same name is overwritten: it is skipped and reported.", false),
		t.importBundle)
}

// ---------- settings ----------

type settingsOut struct {
	Context
	Settings core.Settings `json:"settings"`
}

func (t *tools) getSettings(ctx context.Context, _ noIn) (settingsOut, error) {
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return settingsOut{}, err
	}
	return settingsOut{Context: t.contextOf(settings), Settings: settings}, nil
}

// updateSettingsIn is a partial update of the settings: a field left out
// keeps its value.
type updateSettingsIn struct {
	Timezone       *string             `json:"timezone,omitempty" jsonschema:"an IANA time zone. It decides what today is: day boundaries, the reminder time, quiet hours"`
	Language       *string             `json:"language,omitempty" jsonschema:"en or zh: the language notifications are written in"`
	BaseCurrency   *string             `json:"base_currency,omitempty" jsonschema:"a three-letter code; monetary totals are converted to it"`
	FX             *map[string]float64 `json:"fx,omitempty" jsonschema:"exchange rates: one unit of the key is worth this much of the base currency. Replaced as a whole when sent, so send {} to clear them"`
	SampleSeconds  *int                `json:"sample_seconds,omitempty" jsonschema:"how often accounts are read, unless one says otherwise"`
	RunwayDays     *float64            `json:"runway_days,omitempty" jsonschema:"the default lead time, in days, for a running-out warning"`
	Anomaly        *bool               `json:"anomaly,omitempty" jsonschema:"burst detection on by default"`
	ReminderTime   *string             `json:"reminder_time,omitempty" jsonschema:"the local time of day date reminders go out, HH:MM"`
	Quiet          *core.QuietHours    `json:"quiet,omitempty" jsonschema:"holds back everything that is not critical between start and end, local time, across midnight if need be. Replaced as a whole when sent"`
	Digest         *core.Digest        `json:"digest,omitempty" jsonschema:"the periodic summary: enabled; weekday, 0 for every day or 1 (Monday) to 7 (Sunday); and time, HH:MM. Replaced as a whole when sent"`
	RenotifyHours  *int                `json:"renotify_hours,omitempty" jsonschema:"how often an unacknowledged critical incident repeats its notification; 0 never"`
	NotifyResolved *bool               `json:"notify_resolved,omitempty" jsonschema:"send a message when something recovers"`
	RetentionDays  *int                `json:"retention_days,omitempty" jsonschema:"how long readings and closed incidents are kept"`
	UpcomingDays   *int                `json:"upcoming_days,omitempty" jsonschema:"how far ahead the overview looks for dates"`
	PublicURL      *string             `json:"public_url,omitempty" jsonschema:"where the dashboard is reachable; notifications link to it"`
}

func (t *tools) updateSettings(ctx context.Context, in updateSettingsIn) (settingsOut, error) {
	if in.Timezone == nil && in.Language == nil && in.BaseCurrency == nil && in.FX == nil && in.SampleSeconds == nil &&
		in.RunwayDays == nil && in.Anomaly == nil && in.ReminderTime == nil && in.Quiet == nil && in.Digest == nil &&
		in.RenotifyHours == nil && in.NotifyResolved == nil && in.RetentionDays == nil && in.UpcomingDays == nil && in.PublicURL == nil {
		return settingsOut{}, &engine.InvalidError{Reason: "nothing to change: give at least one setting"}
	}
	current, err := t.e.Settings(ctx)
	if err != nil {
		return settingsOut{}, err
	}
	s := current
	if in.Timezone != nil {
		s.Timezone = *in.Timezone
	}
	if in.Language != nil {
		s.Language = *in.Language
	}
	if in.BaseCurrency != nil {
		s.BaseCurrency = *in.BaseCurrency
	}
	if in.FX != nil {
		s.FX = *in.FX
		if s.FX == nil {
			s.FX = map[string]float64{}
		}
	}
	if in.SampleSeconds != nil {
		s.SampleSeconds = *in.SampleSeconds
	}
	if in.RunwayDays != nil {
		s.RunwayDays = *in.RunwayDays
	}
	if in.Anomaly != nil {
		s.Anomaly = *in.Anomaly
	}
	if in.ReminderTime != nil {
		s.ReminderTime = *in.ReminderTime
	}
	if in.Quiet != nil {
		s.Quiet = *in.Quiet
	}
	if in.Digest != nil {
		s.Digest = *in.Digest
	}
	if in.RenotifyHours != nil {
		s.RenotifyHours = *in.RenotifyHours
	}
	if in.NotifyResolved != nil {
		s.NotifyResolved = *in.NotifyResolved
	}
	if in.RetentionDays != nil {
		s.RetentionDays = *in.RetentionDays
	}
	if in.UpcomingDays != nil {
		s.UpcomingDays = *in.UpcomingDays
	}
	if in.PublicURL != nil {
		s.PublicURL = *in.PublicURL
	}
	saved, err := t.e.UpdateSettings(ctx, s)
	if err != nil {
		return settingsOut{}, err
	}
	return settingsOut{Context: t.contextOf(saved), Settings: saved}, nil
}

// ---------- tokens ----------

type listTokensOut struct {
	Tokens []core.Token `json:"tokens" jsonschema:"the tokens by id, name, scopes and last use; never the tokens themselves"`
}

func (t *tools) listTokens(ctx context.Context, _ noIn) (listTokensOut, error) {
	tokens, err := t.e.Tokens(ctx)
	if err != nil {
		return listTokensOut{}, err
	}
	if tokens == nil {
		tokens = []core.Token{}
	}
	return listTokensOut{Tokens: tokens}, nil
}

type createTokenIn struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes" jsonschema:"what it may do, any of read, act, ingest and admin. Admin carries them all"`
	ExpiresAt string   `json:"expires_at,omitempty" jsonschema:"RFC 3339; the token stops working then. It lives until revoked when omitted"`
}

// tokenOut is a new token. The token itself is in it this one time.
type tokenOut struct {
	Context
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix" jsonschema:"the first characters of the token, to recognise it by"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	Secret     string     `json:"token" jsonschema:"the token itself, shown this one time only: hand it on to whoever needs it, and never store it anywhere else"`
}

func (t *tools) createToken(ctx context.Context, in createTokenIn) (tokenOut, error) {
	input := engine.TokenInput{Name: in.Name, Scopes: in.Scopes}
	if in.ExpiresAt != "" {
		at, err := time.Parse(time.RFC3339, in.ExpiresAt)
		if err != nil {
			return tokenOut{}, &engine.InvalidError{Reason: "expires_at must be an RFC 3339 time in the future"}
		}
		input.ExpiresAt = &at
	}
	created, err := t.e.CreateToken(ctx, input)
	if err != nil {
		return tokenOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return tokenOut{}, err
	}
	return tokenOut{
		Context: t.contextOf(settings), ID: created.ID, Name: created.Name, Prefix: created.Prefix, Scopes: created.Scopes,
		ExpiresAt: created.ExpiresAt, LastUsedAt: created.LastUsedAt, CreatedAt: created.CreatedAt, Secret: created.Secret,
	}, nil
}

type tokenRefIn struct {
	Token string `json:"token" jsonschema:"the token's id, or its name as shown in list_tokens"`
}

// revokedOut is the answer of revoke_token.
type revokedOut struct {
	Revoked bool `json:"revoked"`
}

func (t *tools) revokeToken(ctx context.Context, in tokenRefIn) (revokedOut, error) {
	ref := strings.TrimSpace(in.Token)
	if ref == "" {
		return revokedOut{}, &engine.InvalidError{Reason: "token is required: an id or a name from list_tokens"}
	}
	tokens, err := t.e.Tokens(ctx)
	if err != nil {
		return revokedOut{}, err
	}
	var matches []core.Token
	for _, tk := range tokens {
		if tk.ID == ref {
			matches = []core.Token{tk}
			break
		}
		if strings.EqualFold(tk.Name, ref) {
			matches = append(matches, tk)
		}
	}
	switch len(matches) {
	case 1:
	default:
		return revokedOut{}, &engine.InvalidError{Reason: fmt.Sprintf("no token is called %q; see list_tokens", ref)}
	}
	if err := t.e.DeleteToken(ctx, matches[0].ID); err != nil {
		return revokedOut{}, err
	}
	return revokedOut{Revoked: true}, nil
}

// ---------- provider definitions ----------

type listProvidersOut struct {
	Providers []provider.Definition `json:"providers" jsonschema:"the built-in catalog and the user's own definitions; each names the credential fields an account of that provider needs"`
}

func (t *tools) listProviders(ctx context.Context, _ noIn) (listProvidersOut, error) {
	definitions, err := t.e.Definitions(ctx)
	if err != nil {
		return listProvidersOut{}, err
	}
	if definitions == nil {
		definitions = []provider.Definition{}
	}
	return listProvidersOut{Providers: definitions}, nil
}

type saveProviderIn struct {
	Definition map[string]any `json:"definition" jsonschema:"the whole definition, in the shape list_providers shows. Its id is the key it is saved under; a built-in id is refused, so save a changed copy of a built-in one under another id. Fields that are not part of a definition are refused"`
}

type providerOut struct {
	Context
	Definition provider.Definition `json:"definition"`
}

// definitionOf reads a provider definition out of what an agent sent. The
// object is free-form in the tool's schema — a definition is too deep to
// spell out there, and list_providers already shows the shape — but it is
// read strictly: a field that is not part of a definition is refused, as
// the HTTP API refuses it.
func definitionOf(raw map[string]any) (provider.Definition, error) {
	var def provider.Definition
	encoded, err := json.Marshal(raw)
	if err != nil {
		return def, &engine.InvalidError{Reason: "definition must be a JSON object"}
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&def); err != nil {
		return def, &engine.InvalidError{Reason: "definition is not shaped like one: " + err.Error()}
	}
	return def, nil
}

func (t *tools) saveProvider(ctx context.Context, in saveProviderIn) (providerOut, error) {
	def, err := definitionOf(in.Definition)
	if err != nil {
		return providerOut{}, err
	}
	saved, err := t.e.SaveProvider(ctx, def)
	if err != nil {
		return providerOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return providerOut{}, err
	}
	return providerOut{Context: t.contextOf(settings), Definition: saved}, nil
}

type deleteProviderIn struct {
	Provider string `json:"provider" jsonschema:"the definition's id; a built-in provider cannot be deleted"`
}

func (t *tools) deleteProvider(ctx context.Context, in deleteProviderIn) (deletedOut, error) {
	ref := strings.TrimSpace(in.Provider)
	if ref == "" {
		return deletedOut{}, &engine.InvalidError{Reason: "provider is required: a definition's id from list_providers"}
	}
	if err := t.e.DeleteProvider(ctx, ref); err != nil {
		return deletedOut{}, err
	}
	return deletedOut{Deleted: true}, nil
}

// ---------- system ----------

type systemOut struct {
	Context
	Version            string        `json:"version"`
	Demo               bool          `json:"demo" jsonschema:"true when this instance runs on generated data"`
	NotificationsMuted bool          `json:"notifications_muted" jsonschema:"true while notifications are recorded but not sent"`
	Stats              store.Stats   `json:"stats" jsonschema:"how much is stored"`
	Jobs               []runtime.Job `json:"jobs" jsonschema:"the background loops: when each last ran, whether it failed, and how often it is due"`
}

func (t *tools) getSystem(ctx context.Context, _ noIn) (systemOut, error) {
	stats, err := t.e.Store().Stats(ctx)
	if err != nil {
		return systemOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return systemOut{}, err
	}
	jobs := []runtime.Job{}
	if t.runner != nil {
		jobs = t.runner.Jobs()
	}
	return systemOut{
		Context: t.contextOf(settings), Version: buildinfo.Version, Demo: t.demo,
		NotificationsMuted: t.e.Muted(), Stats: stats, Jobs: jobs,
	}, nil
}

type backupOut struct {
	Context
	Path string `json:"path" jsonschema:"the backup file, on the server"`
}

func (t *tools) backupNow(ctx context.Context, _ noIn) (backupOut, error) {
	if t.runner == nil {
		return backupOut{}, &engine.InvalidError{Reason: "backups are not available here"}
	}
	path, err := t.runner.Backup(ctx)
	if err != nil {
		return backupOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return backupOut{}, err
	}
	return backupOut{Context: t.contextOf(settings), Path: path}, nil
}

// ---------- export and import ----------

type exportBundleIn struct {
	Secrets bool `json:"secrets,omitempty" jsonschema:"include credentials and channel settings in the clear. The bundle is then as sensitive as the database: pass it on, do not store it"`
	History bool `json:"history,omitempty" jsonschema:"include the meter readings and the closed incidents; left out by default, as they are large"`
}

// exportBundleOut is a bundle, the same file the dashboard's export gives.
// It is passed on as it was built: a bundle holds raw JSON inside, which
// has no shape worth spelling out in a schema.
type exportBundleOut struct {
	Context
	Bundle map[string]any `json:"bundle" jsonschema:"the bundle, exactly as the export file holds it"`
}

func (t *tools) exportBundle(ctx context.Context, in exportBundleIn) (exportBundleOut, error) {
	exported, err := bundle.Export(ctx, t.e, bundle.ExportOptions{Secrets: in.Secrets, History: in.History, Source: "quotapulse " + buildinfo.Version})
	if err != nil {
		return exportBundleOut{}, err
	}
	raw, err := json.Marshal(exported)
	if err != nil {
		return exportBundleOut{}, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return exportBundleOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return exportBundleOut{}, err
	}
	return exportBundleOut{Context: t.contextOf(settings), Bundle: out}, nil
}

type importBundleIn struct {
	Bundle       map[string]any `json:"bundle" jsonschema:"the contents of an export file"`
	WithSettings bool           `json:"with_settings,omitempty" jsonschema:"also apply its settings. Usually wanted only on an empty install: an import into a running one should not move its time zone"`
}

// importBundleOut reports what was created and what was skipped because it
// already existed.
type importBundleOut struct {
	Context
	bundle.Report
}

func (t *tools) importBundle(ctx context.Context, in importBundleIn) (importBundleOut, error) {
	if in.Bundle == nil {
		return importBundleOut{}, &engine.InvalidError{Reason: "bundle is required: the contents of an export file"}
	}
	raw, err := json.Marshal(in.Bundle)
	if err != nil {
		return importBundleOut{}, &engine.InvalidError{Reason: "bundle must be a JSON object"}
	}
	parsed, err := bundle.Parse(raw)
	if err != nil {
		return importBundleOut{}, &engine.InvalidError{Reason: err.Error()}
	}
	report, err := bundle.Import(ctx, t.e, parsed, bundle.ImportOptions{Settings: in.WithSettings})
	if err != nil {
		return importBundleOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return importBundleOut{}, err
	}
	return importBundleOut{Context: t.contextOf(settings), Report: report}, nil
}

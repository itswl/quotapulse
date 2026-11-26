package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/engine"
)

// Accounts: the places meters are read from. Listing them needs read; the
// rest needs admin, because an account's credential is a platform's key.

// account is an account as the dashboard shows it. The credential is
// masked: secret fields never leave the server.
type account struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	Provider             string            `json:"provider" jsonschema:"the provider's id"`
	ProviderName         string            `json:"provider_name,omitempty"`
	Group                string            `json:"group,omitempty"`
	Enabled              bool              `json:"enabled" jsonschema:"a disabled account is not read, and nothing on it is judged"`
	Credential           map[string]string `json:"credential" jsonschema:"the credential by field, secrets masked"`
	CredentialUnreadable bool              `json:"credential_unreadable,omitempty" jsonschema:"the stored credential cannot be decrypted; it has to be entered again"`
	IntervalSeconds      int               `json:"interval_seconds" jsonschema:"how often the account is read, in seconds"`
	Status               string            `json:"status" jsonschema:"ok or failing"`
	LastError            string            `json:"last_error,omitempty"`
	Failures             int               `json:"consecutive_failures" jsonschema:"reads in a row that failed"`
	SampledAt            *time.Time        `json:"sampled_at,omitempty"`
	SucceededAt          *time.Time        `json:"succeeded_at,omitempty" jsonschema:"when it was last read successfully"`
	NextSampleAt         *time.Time        `json:"next_sample_at,omitempty"`
	LatencyMs            *int64            `json:"latency_ms,omitempty"`
	Meters               []string          `json:"meters" jsonschema:"the ids of this account's meters"`
	Incident             *Incident         `json:"incident,omitempty" jsonschema:"what is open on the account — usually that it cannot be read"`
}

func accountOf(v engine.AccountView) account {
	out := account{
		ID: v.ID, Name: v.Name, Provider: v.Provider, ProviderName: v.ProviderName, Group: v.Group,
		Enabled: v.Enabled, Credential: v.Credential, CredentialUnreadable: v.Unreadable,
		IntervalSeconds: v.EffectiveInterval, Status: v.Status, LastError: v.LastError, Failures: v.Failures,
		SampledAt: v.SampledAt, SucceededAt: v.SucceededAt, NextSampleAt: v.NextAt, LatencyMs: v.LatencyMs,
		Meters: v.MeterIDs,
	}
	if out.Credential == nil {
		out.Credential = map[string]string{}
	}
	if out.Meters == nil {
		out.Meters = []string{}
	}
	if v.Incident != nil {
		open := incidentOf(*v.Incident)
		out.Incident = &open
	}
	return out
}

// findAccount resolves an id, or a name that matches exactly one account.
func (t *tools) findAccount(ctx context.Context, ref string) (engine.AccountView, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return engine.AccountView{}, &engine.InvalidError{Reason: "account is required: an id or a name from list_accounts"}
	}
	accounts, err := t.e.Accounts(ctx, t.lang)
	if err != nil {
		return engine.AccountView{}, err
	}
	var matches []engine.AccountView
	for _, a := range accounts {
		if a.ID == ref {
			return a, nil
		}
		if strings.EqualFold(a.Name, ref) {
			matches = append(matches, a)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return engine.AccountView{}, &engine.InvalidError{Reason: fmt.Sprintf("no account is called %q; see list_accounts", ref)}
	}
	return engine.AccountView{}, &engine.InvalidError{Reason: fmt.Sprintf("%d accounts are called %q; use the id", len(matches), ref)}
}

type listAccountsIn struct {
	Query string `json:"query,omitempty" jsonschema:"only accounts whose name, provider or group contains this text"`
}

type listAccountsOut struct {
	Context
	Accounts []account `json:"accounts"`
}

func (t *tools) listAccounts(ctx context.Context, in listAccountsIn) (listAccountsOut, error) {
	views, err := t.e.Accounts(ctx, t.lang)
	if err != nil {
		return listAccountsOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return listAccountsOut{}, err
	}
	query := strings.ToLower(strings.TrimSpace(in.Query))
	out := listAccountsOut{Context: t.contextOf(settings), Accounts: []account{}}
	for _, v := range views {
		a := accountOf(v)
		if query == "" || strings.Contains(strings.ToLower(a.Name+" "+a.Provider+" "+a.ProviderName+" "+a.Group), query) {
			out.Accounts = append(out.Accounts, a)
		}
	}
	return out, nil
}

type accountOut struct {
	Context
	Account account `json:"account"`
}

func (t *tools) accountResult(ctx context.Context, view engine.AccountView) (accountOut, error) {
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return accountOut{}, err
	}
	return accountOut{Context: t.contextOf(settings), Account: accountOf(view)}, nil
}

// ---------- admin ----------

type addAccountIn struct {
	Name            string            `json:"name"`
	Provider        string            `json:"provider" jsonschema:"a provider's id, from list_providers"`
	Credential      map[string]string `json:"credential,omitempty" jsonschema:"the provider's fields by key, as list_providers describes them"`
	Group           string            `json:"group,omitempty"`
	IntervalSeconds int               `json:"interval_seconds,omitempty" jsonschema:"how often to read it, in seconds; 0 (the default) uses the setting"`
	Enabled         *bool             `json:"enabled,omitempty" jsonschema:"a disabled account is not read; the default is enabled"`
}

func (t *tools) addAccount(ctx context.Context, in addAccountIn) (accountOut, error) {
	input := engine.AccountInput{Name: &in.Name, Provider: &in.Provider, Credential: in.Credential, Enabled: in.Enabled}
	if in.Group != "" {
		input.Group = &in.Group
	}
	if in.IntervalSeconds != 0 {
		input.Interval = &in.IntervalSeconds
	}
	view, err := t.e.CreateAccount(ctx, input, t.lang)
	if err != nil {
		return accountOut{}, err
	}
	return t.accountResult(ctx, view)
}

// updateAccountIn edits an account. A field left out keeps its value.
type updateAccountIn struct {
	Account         string            `json:"account" jsonschema:"the account's id, or its name as shown in list_accounts"`
	Name            *string           `json:"name,omitempty"`
	Provider        *string           `json:"provider,omitempty" jsonschema:"only the account's current provider is accepted: a provider cannot be changed, so add a new account instead"`
	Credential      map[string]string `json:"credential,omitempty" jsonschema:"the fields to change, by key; a secret left out keeps its stored value"`
	Group           *string           `json:"group,omitempty"`
	IntervalSeconds *int              `json:"interval_seconds,omitempty" jsonschema:"how often to read it, in seconds; 0 uses the setting"`
	Enabled         *bool             `json:"enabled,omitempty"`
}

func (t *tools) updateAccount(ctx context.Context, in updateAccountIn) (accountOut, error) {
	view, err := t.findAccount(ctx, in.Account)
	if err != nil {
		return accountOut{}, err
	}
	updated, err := t.e.UpdateAccount(ctx, view.ID, engine.AccountInput{
		Name: in.Name, Provider: in.Provider, Credential: in.Credential,
		Group: in.Group, Interval: in.IntervalSeconds, Enabled: in.Enabled,
	}, t.lang)
	if err != nil {
		return accountOut{}, err
	}
	return t.accountResult(ctx, updated)
}

type accountRefIn struct {
	Account string `json:"account" jsonschema:"the account's id, or its name as shown in list_accounts"`
}

func (t *tools) deleteAccount(ctx context.Context, in accountRefIn) (deletedOut, error) {
	view, err := t.findAccount(ctx, in.Account)
	if err != nil {
		return deletedOut{}, err
	}
	if err := t.e.DeleteAccount(ctx, view.ID); err != nil {
		return deletedOut{}, err
	}
	return deletedOut{Deleted: true}, nil
}

// testAccountIn tries a credential without saving anything.
type testAccountIn struct {
	Provider   string            `json:"provider,omitempty" jsonschema:"try the credential against a provider, built-in or saved, by id"`
	Definition map[string]any    `json:"definition,omitempty" jsonschema:"try it against a definition that is not saved yet, in the shape list_providers shows"`
	Credential map[string]string `json:"credential,omitempty" jsonschema:"the credential's fields by key; a secret left out is taken from the account, when one is given"`
	Account    string            `json:"account,omitempty" jsonschema:"test with this account's stored secrets, by id or name — the way the dashboard's edit form does"`
}

type testAccountOut struct {
	Context
	engine.TestResult
}

func (t *tools) testAccount(ctx context.Context, in testAccountIn) (testAccountOut, error) {
	input := engine.TestInput{Provider: strings.TrimSpace(in.Provider), Credential: in.Credential}
	if input.Provider == "" && in.Definition == nil {
		return testAccountOut{}, &engine.InvalidError{Reason: "provider or definition is required"}
	}
	if in.Definition != nil {
		def, err := definitionOf(in.Definition)
		if err != nil {
			return testAccountOut{}, err
		}
		input.Definition = &def
	}
	if in.Account != "" {
		view, err := t.findAccount(ctx, in.Account)
		if err != nil {
			return testAccountOut{}, err
		}
		input.AccountID = view.ID
	}
	result, err := t.e.TestAccount(ctx, input)
	if err != nil {
		return testAccountOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return testAccountOut{}, err
	}
	return testAccountOut{Context: t.contextOf(settings), TestResult: result}, nil
}

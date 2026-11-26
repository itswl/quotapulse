package mcp_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/core"
)

type settingsView struct {
	Timezone, Language string
	BaseCurrency       string `json:"base_currency"`
	ReminderTime       string `json:"reminder_time"`
	PublicURL          string `json:"public_url"`
	FX                 map[string]float64
	SampleSeconds      int `json:"sample_seconds"`
	Quiet              struct {
		Enabled bool
		Start   string
	}
}

type settingsEnvelope struct {
	Timezone string
	Settings settingsView `json:"settings"`
}

type tokenView struct {
	ID, Name, Token string
	Scopes          []string
	ExpiresAt       *time.Time `json:"expires_at"`
}

func TestAnAdminManagesSettingsTokensAndProviders(t *testing.T) {
	w := newWorld(t)
	admin, acting := w.connect(core.ScopeAdmin), w.connect(core.ScopeRead, core.ScopeAct)

	// Settings: read by anyone who can read, changed by an admin.
	before, result := call[settingsEnvelope](t, acting, "get_settings", nil)
	if result.IsError || before.Settings.Timezone != "Asia/Shanghai" || before.Settings.BaseCurrency != "CNY" ||
		before.Settings.FX["USD"] != 7 || !before.Settings.Quiet.Enabled || before.Settings.ReminderTime != "09:00" {
		t.Fatalf("get_settings: %+v %s", before, errorText(result))
	}
	after, result := call[settingsEnvelope](t, admin, "update_settings", map[string]any{
		"timezone": "Asia/Tokyo", "fx": map[string]any{}, "quiet": map[string]any{"enabled": false, "start": "22:00", "end": "07:00"},
		"public_url": "https://pulse.example/",
	})
	if result.IsError {
		t.Fatalf("update_settings: %s", errorText(result))
	}
	if s := after.Settings; s.Timezone != "Asia/Tokyo" || len(s.FX) != 0 || s.Quiet.Enabled || s.Language != "en" || s.PublicURL != "https://pulse.example" {
		t.Errorf("settings after = %+v", s)
	}
	if after.Timezone != "Asia/Tokyo" {
		t.Errorf("the result still names the old time zone: %q", after.Timezone)
	}
	for name, args := range map[string]map[string]any{
		"nothing":              {},
		"a too-short interval": {"sample_seconds": 5},
		"an unknown zone":      {"timezone": "Mars/Olympus"},
	} {
		if _, result := call[settingsEnvelope](t, admin, "update_settings", args); !result.IsError {
			t.Errorf("update_settings with %s succeeded", name)
		}
	}
	if !refused(t, acting, "update_settings", map[string]any{"language": "zh"}) {
		t.Error("an act token changed the settings")
	}

	// Tokens: list, mint, revoke. The secret is in the answer once.
	tokens, result := call[struct{ Tokens []tokenView }](t, admin, "list_tokens", nil)
	if result.IsError || len(tokens.Tokens) == 0 {
		t.Fatalf("list_tokens: %+v %s", tokens, errorText(result))
	}
	for _, tk := range tokens.Tokens {
		if tk.Token != "" {
			t.Errorf("list_tokens hands out the token %q", tk.Token)
		}
	}
	created, result := call[struct {
		tokenView
		Timezone string
	}](t, admin, "create_token", map[string]any{"name": "script", "scopes": []string{"ingest", "read"}})
	if result.IsError {
		t.Fatalf("create_token: %s", errorText(result))
	}
	if !strings.HasPrefix(created.Token, "qp_") || len(created.Token) < 10 || created.ID == "" || created.Name != "script" {
		t.Errorf("created = %+v", created.tokenView)
	}
	if scopes := created.Scopes; len(scopes) != 2 || scopes[0] != "read" || scopes[1] != "ingest" {
		t.Errorf("scopes are not in the canonical order: %v", scopes)
	}
	temporary, result := call[tokenView](t, admin, "create_token", map[string]any{"name": "temp", "scopes": []string{"read"}, "expires_at": "2027-01-01T00:00:00Z"})
	if result.IsError || temporary.ExpiresAt == nil || !temporary.ExpiresAt.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("an expiring token: %+v %s", temporary, errorText(result))
	}
	for name, args := range map[string]map[string]any{
		"no scope":                  {"name": "x", "scopes": []string{}},
		"an unknown scope":          {"name": "x", "scopes": []string{"root"}},
		"expiry in the past":        {"name": "x", "scopes": []string{"read"}, "expires_at": "2020-01-01T00:00:00Z"},
		"expiry that is not a time": {"name": "x", "scopes": []string{"read"}, "expires_at": "soon"},
	} {
		if _, result := call[tokenView](t, admin, "create_token", args); !result.IsError {
			t.Errorf("create_token with %s succeeded", name)
		}
	}
	if listed, _ := call[struct{ Tokens []tokenView }](t, admin, "list_tokens", nil); len(listed.Tokens) != len(tokens.Tokens)+2 {
		t.Errorf("tokens after = %+v", listed.Tokens)
	}
	if revoked, result := call[struct{ Revoked bool }](t, admin, "revoke_token", map[string]any{"token": "Script"}); result.IsError || !revoked.Revoked {
		t.Fatalf("revoke_token: %+v %s", revoked, errorText(result))
	}
	if listed, _ := call[struct{ Tokens []tokenView }](t, admin, "list_tokens", nil); len(listed.Tokens) != len(tokens.Tokens)+1 {
		t.Errorf("tokens after revoking = %+v", listed.Tokens)
	}
	if _, result := call[struct{ Revoked bool }](t, admin, "revoke_token", map[string]any{"token": "script"}); !result.IsError {
		t.Error("revoked it twice")
	}
	if !refused(t, acting, "revoke_token", map[string]any{"token": "temp"}) {
		t.Error("an act token revoked a token")
	}
	// The revoked token no longer gets in.
	req, _ := http.NewRequest("POST", w.url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+created.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a revoked token is still accepted: status %d", resp.StatusCode)
	}

	// Provider definitions of the user's own.
	definition := map[string]any{
		"id": "mine", "name": "Mine",
		"fields":  []map[string]any{{"key": "api_key", "label": "API Key", "secret": true}},
		"request": map[string]any{"url": "https://example.invalid/v1/meters", "auth": map[string]any{"type": "bearer"}},
		"extract": ".meters[]",
	}
	type providerView struct{ ID string }
	type providerList struct {
		Providers []providerView `json:"providers"`
	}
	catalog, result := call[providerList](t, acting, "list_providers", nil)
	if result.IsError || len(catalog.Providers) == 0 {
		t.Fatalf("list_providers: %+v %s", catalog, errorText(result))
	}
	saved, result := call[struct {
		Definition providerView `json:"definition"`
	}](t, admin, "save_provider", map[string]any{"definition": definition})
	if result.IsError || saved.Definition.ID != "mine" {
		t.Fatalf("save_provider: %+v %s", saved, errorText(result))
	}
	if listed, _ := call[providerList](t, acting, "list_providers", nil); len(listed.Providers) != len(catalog.Providers)+1 {
		t.Errorf("the definition is not listed: %+v", listed.Providers)
	}
	definition["id"] = catalog.Providers[0].ID
	if _, result := call[struct{ Definition providerView }](t, admin, "save_provider", map[string]any{"definition": definition}); !result.IsError {
		t.Errorf("overwrote the built-in provider %q", catalog.Providers[0].ID)
	}
	if _, result := call[struct{ Deleted bool }](t, admin, "delete_provider", map[string]any{"provider": catalog.Providers[0].ID}); !result.IsError {
		t.Error("deleted a built-in provider")
	}
	if deleted, result := call[struct{ Deleted bool }](t, admin, "delete_provider", map[string]any{"provider": "mine"}); result.IsError || !deleted.Deleted {
		t.Fatalf("delete_provider: %s", errorText(result))
	}
	if !refused(t, acting, "save_provider", map[string]any{"definition": definition}) {
		t.Error("an act token saved a provider")
	}

	// System, backup, export, import.
	if _, result := call[struct{}](t, admin, "add_date", map[string]any{"title": "Domain", "freq": "yearly", "month": 12, "day": 1}); result.IsError {
		t.Fatalf("add_date: %s", errorText(result))
	}
	system, result := call[struct {
		Version, Timezone string
		Demo              bool
		Muted             bool `json:"notifications_muted"`
		Stats             struct{ Reminders int }
		Jobs              []map[string]any
	}](t, admin, "get_system", nil)
	if result.IsError || system.Version != "dev" || system.Demo || system.Muted || system.Jobs == nil || system.Stats.Reminders != 1 || system.Timezone != "Asia/Tokyo" {
		t.Fatalf("get_system: %+v %s", system, errorText(result))
	}
	if _, result := call[struct{ Path string }](t, admin, "backup_now", nil); !result.IsError || !strings.Contains(errorText(result), "not available") {
		t.Errorf("backup_now without a runner: %s", errorText(result))
	}
	if !refused(t, acting, "get_system", nil) {
		t.Error("an act token read the system state")
	}

	exported, result := call[struct {
		Bundle map[string]any `json:"bundle"`
	}](t, admin, "export_bundle", nil)
	if result.IsError || exported.Bundle["format"] != "quotapulse-bundle" {
		t.Fatalf("export_bundle: %v %s", exported.Bundle["format"], errorText(result))
	}
	if reminders, ok := exported.Bundle["reminders"].([]any); !ok || len(reminders) != 1 {
		t.Errorf("the bundle's dates: %v", exported.Bundle["reminders"])
	}
	imported, result := call[struct {
		Created map[string]int `json:"created"`
		Skipped map[string]int `json:"skipped"`
	}](t, admin, "import_bundle", map[string]any{"bundle": exported.Bundle})
	if result.IsError {
		t.Fatalf("import_bundle: %s", errorText(result))
	}
	if sum(imported.Created) != 0 || sum(imported.Skipped) == 0 {
		t.Errorf("importing what is already there: created %v, skipped %v", imported.Created, imported.Skipped)
	}
	for name, args := range map[string]map[string]any{
		"nothing":      {},
		"not a bundle": {"bundle": map[string]any{"format": "nope"}},
	} {
		if _, result := call[struct{}](t, admin, "import_bundle", args); !result.IsError {
			t.Errorf("import_bundle with %s succeeded", name)
		}
	}
	if !refused(t, acting, "export_bundle", nil) {
		t.Error("an act token exported everything")
	}
}

func sum(counts map[string]int) int {
	total := 0
	for _, n := range counts {
		total += n
	}
	return total
}

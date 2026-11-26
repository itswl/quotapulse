package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/provider"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type accountView struct {
	ID, Name, Provider, Group, Status string
	ProviderName                      string `json:"provider_name"`
	Enabled                           bool
	Credential                        map[string]string
	Meters                            []string
	IntervalSeconds                   int `json:"interval_seconds"`
}

type accountEnvelope struct {
	Account accountView `json:"account"`
}

type triedView struct {
	OK      bool             `json:"ok"`
	Error   string           `json:"error"`
	Status  int              `json:"status"`
	Samples []map[string]any `json:"samples"`
}

func TestAnAgentManagesAccounts(t *testing.T) {
	w := newWorld(t)
	// A platform that accepts one key, and a provider definition of the
	// user's own that points at it.
	platform := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k-1" {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"meters":[{"key":"balance","name":"Balance","kind":"balance","unit":"CNY","value":12.5}]}`))
	}))
	t.Cleanup(platform.Close)
	must(t, w.engine.Store().SaveProvider(ctx, provider.Definition{
		ID: "fake", Name: "Fake",
		Fields:  []provider.Field{{Key: "api_key", Label: "API Key", Secret: true}, {Key: "team", Label: "Team", Optional: true}},
		Request: provider.Request{URL: platform.URL, Auth: provider.Auth{Type: provider.AuthBearer}},
		Extract: `.meters[]`,
	}, w.now))
	admin, acting := w.connect(core.ScopeAdmin), w.connect(core.ScopeRead, core.ScopeAct)

	if none, result := call[struct{ Accounts []accountView }](t, acting, "list_accounts", nil); result.IsError || len(none.Accounts) != 0 || none.Accounts == nil {
		t.Fatalf("list_accounts on an empty install: %+v %s", none, errorText(result))
	}

	// Try a credential before trusting the platform with it.
	tried, result := call[triedView](t, admin, "test_account", map[string]any{"provider": "fake", "credential": map[string]any{"api_key": "k-1"}})
	if result.IsError || !tried.OK || tried.Status != http.StatusOK || len(tried.Samples) != 1 {
		t.Fatalf("test_account: %+v %s", tried, errorText(result))
	}
	if refused, result := call[triedView](t, admin, "test_account", map[string]any{"provider": "fake", "credential": map[string]any{"api_key": "wrong"}}); result.IsError || refused.OK {
		t.Fatalf("a wrong key: %+v %s", refused, errorText(result))
	}
	// A credential without its key is sent as it is: the platform's
	// refusal is the answer, not a failure of the tool.
	if blank, result := call[triedView](t, admin, "test_account", map[string]any{"provider": "fake", "credential": map[string]any{}}); result.IsError || blank.OK {
		t.Errorf("a credential without the key: %+v %s", blank, errorText(result))
	}
	for name, args := range map[string]map[string]any{
		"an unknown provider":             {"provider": "nope", "credential": map[string]any{"api_key": "k-1"}},
		"neither provider nor definition": {"credential": map[string]any{"api_key": "k-1"}},
	} {
		if _, result := call[triedView](t, admin, "test_account", args); !result.IsError {
			t.Errorf("test_account with %s succeeded", name)
		}
	}

	// Add one.
	added, result := call[accountEnvelope](t, admin, "add_account", map[string]any{
		"name": "Main", "provider": "fake", "credential": map[string]any{"api_key": "k-1"}, "group": "AI",
	})
	if result.IsError {
		t.Fatalf("add_account: %s", errorText(result))
	}
	a := added.Account
	if a.ID == "" || a.Name != "Main" || a.Provider != "fake" || a.ProviderName != "Fake" || a.Group != "AI" || !a.Enabled || a.Status != "pending" {
		t.Errorf("added = %+v", a)
	}
	if shown := a.Credential["api_key"]; shown == "" || shown == "k-1" {
		t.Errorf("the secret is not masked: %q", shown)
	}
	if _, result := call[accountEnvelope](t, admin, "add_account", map[string]any{"name": "Main", "provider": "fake", "credential": map[string]any{"api_key": "k-1"}}); !result.IsError {
		t.Error("added an account twice")
	}
	if _, result := call[accountEnvelope](t, admin, "add_account", map[string]any{"name": "Half", "provider": "fake", "credential": map[string]any{}}); !result.IsError {
		t.Error("added an account without a key")
	}

	listed, result := call[struct{ Accounts []accountView }](t, acting, "list_accounts", map[string]any{"query": "ai"})
	if result.IsError || len(listed.Accounts) != 1 || listed.Accounts[0].Name != "Main" {
		t.Fatalf("list_accounts: %+v %s", listed, errorText(result))
	}
	if elsewhere, _ := call[struct{ Accounts []accountView }](t, acting, "list_accounts", map[string]any{"query": "volc"}); len(elsewhere.Accounts) != 0 {
		t.Errorf("a query that matches nothing: %+v", elsewhere.Accounts)
	}

	// Read it now, by name, whatever the case.
	if sampled, result := call[struct{ Requested string }](t, acting, "sample_now", map[string]any{"account": "main"}); result.IsError || sampled.Requested != "Main" {
		t.Errorf("sample_now: %+v %s", sampled, errorText(result))
	}

	// Change it.
	renamed, result := call[accountEnvelope](t, admin, "update_account", map[string]any{"account": "main", "name": "Main account", "interval_seconds": 900})
	if result.IsError || renamed.Account.Name != "Main account" || renamed.Account.IntervalSeconds != 900 {
		t.Fatalf("update_account: %+v %s", renamed, errorText(result))
	}
	rekeyed, result := call[accountEnvelope](t, admin, "update_account", map[string]any{"account": a.ID, "credential": map[string]any{"team": "core"}})
	if result.IsError || rekeyed.Account.Credential["team"] != "core" || rekeyed.Account.Credential["api_key"] == "" {
		t.Fatalf("a field added to the credential: %+v %s", rekeyed.Account.Credential, errorText(result))
	}
	if off, result := call[accountEnvelope](t, admin, "update_account", map[string]any{"account": "Main account", "enabled": false}); result.IsError || off.Account.Enabled {
		t.Fatalf("disabled: %+v %s", off, errorText(result))
	}
	if _, result := call[accountEnvelope](t, admin, "update_account", map[string]any{"account": "Main account", "provider": "deepseek"}); !result.IsError {
		t.Error("changed the provider")
	}
	if _, result := call[accountEnvelope](t, admin, "update_account", map[string]any{"account": "nope"}); !result.IsError {
		t.Error("changed an account that does not exist")
	}

	// Test with the stored secret, as the dashboard's edit form does.
	stored, result := call[triedView](t, admin, "test_account", map[string]any{"provider": "fake", "account": "Main account"})
	if result.IsError || !stored.OK {
		t.Fatalf("test with the stored secret: %+v %s", stored, errorText(result))
	}

	// Remove it.
	deleted, result := call[struct{ Deleted bool }](t, admin, "delete_account", map[string]any{"account": "Main account"})
	if result.IsError || !deleted.Deleted {
		t.Fatalf("delete_account: %s", errorText(result))
	}
	if left, _ := call[struct{ Accounts []accountView }](t, acting, "list_accounts", nil); len(left.Accounts) != 0 {
		t.Errorf("still listed: %+v", left.Accounts)
	}
	if _, result := call[struct{ Deleted bool }](t, admin, "delete_account", map[string]any{"account": "Main account"}); !result.IsError {
		t.Error("deleted it twice")
	}
	// None of this is for a token that merely acts.
	for _, tool := range []string{"add_account", "update_account", "delete_account", "test_account"} {
		if !refused(t, acting, tool, map[string]any{"name": "x", "provider": "fake", "account": "Main"}) {
			t.Errorf("an act token called %s", tool)
		}
	}
}

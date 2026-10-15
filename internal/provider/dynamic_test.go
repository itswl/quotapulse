package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/itswl/quotapulse/internal/model"
)

func TestDigPath(t *testing.T) {
	data := map[string]any{
		"data": map[string]any{
			"items": []any{
				map[string]any{"amount": 12.5},
				map[string]any{"amount": 30},
			},
			"nested": map[string]any{"balance": "77.5"},
		},
	}
	for path, want := range map[string]any{
		"data.items.0.amount":  12.5,
		"data.items.1.amount":  30,
		"data.nested.balance":  "77.5",
		"data.items.5.amount":  nil,
		"data.items.0.missing": nil,
		"data..amount":         nil,
	} {
		if got := DigPath(data, path); got != want {
			t.Errorf("DigPath(%q) = %v, want %v", path, got, want)
		}
	}
	// containers come back as-is
	if got := DigPath(data, "data.nested"); got == nil {
		t.Error("DigPath(data.nested) should return the nested object")
	}
}

func TestValidateCustomProvider(t *testing.T) {
	base := model.CustomProvider{
		Name: "acme", URL: "https://api.acme.test/balance",
		AuthMode: CustomAuthBearer, DefaultType: model.TypeBalance,
		BalancePath: "data.balance",
	}
	if problems := ValidateCustomProvider(base); len(problems) > 0 {
		t.Fatalf("a valid spec reported problems: %v", problems)
	}
	for name, mutate := range map[string]func(*model.CustomProvider){
		"bad name":       func(p *model.CustomProvider) { p.Name = "Acme!" },
		"bad url":        func(p *model.CustomProvider) { p.URL = "ftp://x" },
		"bad auth mode":  func(p *model.CustomProvider) { p.AuthMode = "cookie" },
		"missing param":  func(p *model.CustomProvider) { p.AuthMode = CustomAuthHeader },
		"bad type":       func(p *model.CustomProvider) { p.DefaultType = "points" },
		"bad currency":   func(p *model.CustomProvider) { p.Currency = "DOLLAR" },
		"missing path":   func(p *model.CustomProvider) { p.BalancePath = "  " },
		"negative scale": func(p *model.CustomProvider) { p.BalanceScale = -1 },
	} {
		p := base
		mutate(&p)
		if problems := ValidateCustomProvider(p); len(problems) == 0 {
			t.Errorf("%s: expected problems, got none", name)
		}
	}
	// quota type never carries a currency, and that is fine
	quota := base
	quota.DefaultType = model.TypeQuota
	if problems := ValidateCustomProvider(quota); len(problems) > 0 {
		t.Errorf("quota spec should validate: %v", problems)
	}
}

// A full round trip through the test server: header auth, scale, success
// check and error message path.
func TestCustomProviderFetchRoundTrip(t *testing.T) {
	srv, rec := serveJSON(t, 200, `{"status":"ok","data":{"wallet":{"amount":1234.5}},"err":"boom"}`)
	spec, err := SpecFromCustomProvider(model.CustomProvider{
		Name: "acme", DisplayName: "Acme Cloud",
		URL: srv.URL + "/balance", AuthMode: CustomAuthHeader, AuthParam: "X-Api-Key",
		DefaultType: model.TypeBalance, Currency: "usd",
		BalancePath: "data.wallet.amount", BalanceScale: 0.01,
		SuccessPath: "status", SuccessValues: "ok", ErrorPath: "err",
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Key != "custom:acme" || spec.Currency != "USD" {
		t.Fatalf("spec built wrong: %+v", spec)
	}
	provider := specProvider{spec: spec, apiKey: "acme-key", client: NewClient(DefaultTimeout)}
	value, err := provider.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if value != 12.35 { // 1234.5 * 0.01
		t.Errorf("value = %v, want 12.35", value)
	}
	if got := rec.header.Get("X-Api-Key"); got != "acme-key" {
		t.Errorf("auth header = %q", got)
	}
}

func TestCustomProviderCheckFailures(t *testing.T) {
	spec, err := SpecFromCustomProvider(model.CustomProvider{
		Name: "acme", URL: "https://api.acme.test/balance", AuthMode: CustomAuthNone,
		DefaultType: model.TypeBalance, BalancePath: "data.balance",
		SuccessPath: "success", SuccessValues: "true,1", ErrorPath: "error",
	})
	if err != nil {
		t.Fatal(err)
	}
	p := specProvider{spec: spec, client: NewClient(DefaultTimeout)}
	srv, _ := serveJSON(t, 200, `{"success":"false","data":{"balance":5},"error":"denied"}`)
	p.spec.URL = srv.URL
	if _, err := p.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Errorf("expected the error path to surface, got %v", err)
	}
	// a non-numeric balance is a config error, not a zero
	srv2, _ := serveJSON(t, 200, `{"success":1,"data":{"balance":"lots"}}`)
	p.spec.URL = srv2.URL
	if _, err := p.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "no number at") {
		t.Errorf("expected a no-number error, got %v", err)
	}
}

// The dynamic registry serves New() and All(), and replacing the set removes
// the old entries.
func TestReplaceDynamicRegistry(t *testing.T) {
	spec, err := SpecFromCustomProvider(model.CustomProvider{
		Name: "acme", URL: "https://api.acme.test/balance", AuthMode: CustomAuthNone,
		DefaultType: model.TypeBalance, BalancePath: "data.balance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceDynamic([]Spec{spec}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ReplaceDynamic(nil) })

	if !IsDynamic("custom:acme") {
		t.Fatal("custom:acme should be registered")
	}
	if _, err := New("custom:acme", "", NewClient(DefaultTimeout)); err != nil {
		t.Errorf("New(custom:acme) failed: %v", err)
	}
	found := false
	for _, info := range All() {
		if info.Key == "custom:acme" && info.Name == "acme" {
			found = true
		}
	}
	if !found {
		t.Error("All() should list the custom provider")
	}

	if err := ReplaceDynamic(nil); err != nil {
		t.Fatal(err)
	}
	if IsDynamic("custom:acme") {
		t.Error("replacing with an empty set should unregister")
	}

	// a builtin key can never be shadowed
	builtin := spec
	builtin.Key = "deepseek"
	if err := ReplaceDynamic([]Spec{builtin}); err == nil {
		t.Error("colliding with a builtin key must fail")
	}
}

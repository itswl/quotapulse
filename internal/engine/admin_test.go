package engine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/recur"
)

func TestAccountLifecycle(t *testing.T) {
	w := newWorld(t)
	w.api.balance(100)

	bad := []AccountInput{
		{},
		{Name: ptr("x")},
		{Name: ptr(""), Provider: ptr("fake"), Credential: map[string]string{"api_key": "k"}},
		{Name: ptr("x"), Provider: ptr("nope"), Credential: map[string]string{"api_key": "k"}},
		{Name: ptr("x"), Provider: ptr("fake")},
		{Name: ptr("x"), Provider: ptr("fake"), Credential: map[string]string{"api_key": "k"}, Interval: ptr(5)},
	}
	for i, in := range bad {
		if _, err := w.e.CreateAccount(ctx, in, "zh"); !IsInvalid(err) {
			t.Errorf("input %d: err = %v", i, err)
		}
	}

	view, err := w.e.CreateAccount(ctx, AccountInput{
		Name: ptr("  Main  "), Provider: ptr("fake"), Group: ptr("AI"),
		Credential: map[string]string{"api_key": "sk-secret-value-0001", "team": "blue"},
	}, "zh")
	w.must(err)
	if view.Name != "Main" || view.Group != "AI" || !view.Enabled || view.Status != core.StatusPending || view.ProviderName != "假平台" {
		t.Fatalf("view = %+v", view)
	}
	// The secret is masked on the way out; the plain field is not.
	if view.Credential["api_key"] != "sk-••••0001" || view.Credential["team"] != "blue" {
		t.Fatalf("credential = %v", view.Credential)
	}
	if view.EffectiveInterval != 1800 {
		t.Errorf("effective interval = %d", view.EffectiveInterval)
	}
	if _, err := w.e.CreateAccount(ctx, AccountInput{Name: ptr("main"), Provider: ptr("fake"), Credential: map[string]string{"api_key": "k"}}, "zh"); !IsInvalid(err) {
		t.Errorf("duplicate name: %v", err)
	}

	// Editing without retyping the secret keeps it; clearing a plain field clears it.
	view, err = w.e.UpdateAccount(ctx, view.ID, AccountInput{
		Name: ptr("Renamed"), Interval: ptr(600), Credential: map[string]string{"api_key": "", "team": ""},
	}, "zh")
	w.must(err)
	stored, _ := w.store.Account(ctx, view.ID)
	if stored.Credential["api_key"] != "sk-secret-value-0001" {
		t.Fatalf("the stored secret was lost: %v", stored.Credential)
	}
	if _, kept := stored.Credential["team"]; kept || view.EffectiveInterval != 600 || view.Name != "Renamed" {
		t.Fatalf("after edit: stored %v, view %+v", stored.Credential, view)
	}
	if _, err := w.e.UpdateAccount(ctx, view.ID, AccountInput{Provider: ptr("deepseek")}, "zh"); !IsInvalid(err) {
		t.Errorf("changing the provider: %v", err)
	}
	if _, err := w.e.UpdateAccount(ctx, "acc_missing", AccountInput{Name: ptr("x")}, "zh"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing account: %v", err)
	}

	// A new key makes the account due at once.
	w.sample(view.ID)
	w.advance(time.Minute)
	_, err = w.e.UpdateAccount(ctx, view.ID, AccountInput{Credential: map[string]string{"api_key": "sk-secret-value-0002"}}, "zh")
	w.must(err)
	if due, _ := w.store.DueAccounts(ctx, w.now); len(due) != 1 {
		t.Fatalf("due after a credential change = %+v", due)
	}

	// Disabling quietly closes what was open on it and its meters.
	w.api.balance(0)
	w.sample(view.ID)
	if len(w.open()) != 1 {
		t.Fatalf("open = %+v", w.open())
	}
	w.dispatch()
	_, err = w.e.UpdateAccount(ctx, view.ID, AccountInput{Enabled: ptr(false)}, "zh")
	w.must(err)
	if len(w.open()) != 0 {
		t.Fatal("a disabled account still has an open incident")
	}
	expect(t, w.dispatch())
	meters, _ := w.e.Meters(ctx, "zh")
	if meters[0].Status != statusPaused {
		t.Errorf("status = %s", meters[0].Status)
	}

	w.must(w.e.DeleteAccount(ctx, view.ID))
	if all, _ := w.e.Meters(ctx, "zh"); len(all) != 0 {
		t.Fatalf("meters outlived the account: %+v", all)
	}
}

func TestTestAccount(t *testing.T) {
	w := newWorld(t)
	w.api.balance(42)

	result, err := w.e.TestAccount(ctx, TestInput{Provider: "fake", Credential: map[string]string{"api_key": "sk-test-0001"}})
	w.must(err)
	if !result.OK || len(result.Samples) != 1 || result.Samples[0].Value != 42 || result.Status != 200 || result.Body == nil {
		t.Fatalf("result = %+v", result)
	}
	// Nothing was saved.
	if accounts, _ := w.store.Accounts(ctx); len(accounts) != 0 {
		t.Fatal("a test created an account")
	}

	// A failure is a result, not an error: the form shows it.
	w.api.set(401, `{"error":"bad key sk-test-0001"}`)
	result, err = w.e.TestAccount(ctx, TestInput{Provider: "fake", Credential: map[string]string{"api_key": "sk-test-0001"}})
	w.must(err)
	if result.OK || result.Error != "HTTP 401: bad key •••" || len(result.Samples) != 0 {
		t.Fatalf("result = %+v", result)
	}

	// An edit form can test with the secret it cannot see.
	w.api.balance(7)
	account := w.account("Main")
	result, err = w.e.TestAccount(ctx, TestInput{Provider: "fake", AccountID: account.ID, Credential: map[string]string{"api_key": ""}})
	w.must(err)
	if !result.OK {
		t.Fatalf("result = %+v", result)
	}

	// A definition can be tried before it is saved.
	draft := provider.Definition{
		ID: "draft", Name: "Draft",
		Fields:  []provider.Field{{Key: "api_key", Label: "API Key", Secret: true}},
		Request: provider.Request{URL: w.api.srv.URL, Auth: provider.Auth{Type: provider.AuthBearer}},
		Extract: `{value: (.meters[0].value * 2), unit: "USD"}`,
	}
	result, err = w.e.TestAccount(ctx, TestInput{Definition: &draft, Credential: map[string]string{"api_key": "k-0001"}})
	w.must(err)
	if !result.OK || result.Samples[0].Value != 14 {
		t.Fatalf("result = %+v", result)
	}
	draft.Extract = "{value: "
	if _, err := w.e.TestAccount(ctx, TestInput{Definition: &draft}); !IsInvalid(err) {
		t.Errorf("an invalid draft: %v", err)
	}
	if _, err := w.e.TestAccount(ctx, TestInput{Provider: "nope"}); !IsInvalid(err) {
		t.Errorf("unknown provider: %v", err)
	}
}

func TestCustomProviders(t *testing.T) {
	w := newWorld(t)
	def := provider.Definition{
		ID: "relay", Name: "Relay",
		Fields:  []provider.Field{{Key: "api_key", Label: "API Key", Secret: true}},
		Request: provider.Request{URL: "https://relay.example/v1/balance", Auth: provider.Auth{Type: provider.AuthBearer}},
		Extract: "{value: .balance}",
	}
	saved, err := w.e.SaveProvider(ctx, def)
	w.must(err)
	if saved.Builtin {
		t.Error("a saved definition claims to be built in")
	}
	builtin := def
	builtin.ID = "deepseek"
	if _, err := w.e.SaveProvider(ctx, builtin); !IsInvalid(err) {
		t.Errorf("shadowing a built-in: %v", err)
	}
	broken := def
	broken.Extract = "{value:"
	if _, err := w.e.SaveProvider(ctx, broken); !IsInvalid(err) {
		t.Errorf("invalid jq: %v", err)
	}

	all, _ := w.e.Definitions(ctx)
	ids := map[string]bool{}
	for _, d := range all {
		ids[d.ID] = true
	}
	if !ids["relay"] || !ids["fake"] || !ids["deepseek"] || !ids["glm-coding"] {
		t.Fatalf("definitions = %v", ids)
	}

	name, prov := "R", "relay"
	account, err := w.e.CreateAccount(ctx, AccountInput{Name: &name, Provider: &prov, Credential: map[string]string{"api_key": "k-0001"}}, "en")
	w.must(err)
	if err := w.e.DeleteProvider(ctx, "relay"); !IsInvalid(err) || !strings.Contains(err.Error(), "1 account") {
		t.Fatalf("deleting a provider in use: %v", err)
	}
	w.must(w.e.DeleteAccount(ctx, account.ID))
	w.must(w.e.DeleteProvider(ctx, "relay"))
	if err := w.e.DeleteProvider(ctx, "deepseek"); !IsInvalid(err) {
		t.Errorf("deleting a built-in: %v", err)
	}
}

func TestAnAccountWhoseProviderIsGoneFailsLoudly(t *testing.T) {
	w := newWorld(t)
	w.api.balance(1)
	account := w.account("Main")
	w.must(w.store.DeleteProvider(ctx, "fake"))
	w.sample(account.ID)
	got := w.dispatch()
	if len(got) != 0 { // one failure is not yet an incident…
		t.Fatalf("messages = %q", titles(got))
	}
	w.advance(time.Minute)
	w.sample(account.ID)
	got = w.dispatch()
	expect(t, got, "无法读取 · Main")
	contains(t, got[0].Text, "is no longer defined")
}

func TestMeterEditing(t *testing.T) {
	w := newWorld(t)
	w.api.balance(100)
	account := w.account("Main")
	w.sample(account.ID)
	meter := func() MeterView {
		views, err := w.e.Meters(ctx, "zh")
		w.must(err)
		return views[0]
	}
	id := meter().ID
	// An account's only meter goes by the account's name until someone
	// names it; then it goes by that.
	if first := meter(); first.Display != "Main" || first.Renamed {
		t.Fatalf("before a rename: display %q, renamed %v", first.Display, first.Renamed)
	}

	for i, rules := range []core.Rules{
		{RunwayDays: ptr(-1.0)}, {RunwayDays: ptr(999.0)}, {Budget: ptr(-1.0)}, {StaleAfter: ptr(int64(5))},
	} {
		if _, err := w.e.UpdateMeter(ctx, id, MeterInput{Rules: &rules}, "zh"); !IsInvalid(err) {
			t.Errorf("rules %d: err = %v", i, err)
		}
	}

	view, err := w.e.UpdateMeter(ctx, id, MeterInput{
		Name: ptr("主余额"), Sort: ptr(3),
		Rules: &core.Rules{Low: ptr(20.0), RunwayDays: ptr(14.0), Anomaly: ptr(false), Budget: ptr(0.0)},
	}, "zh")
	w.must(err)
	if view.Name != "主余额" || view.Sort != 3 || view.Effective.RunwayDays != 14 || view.Effective.Anomaly {
		t.Fatalf("view = %+v", view)
	}
	if view.Display != "主余额" || !view.Renamed {
		t.Errorf("after a rename: display %q, renamed %v", view.Display, view.Renamed)
	}
	if view.Rules.Budget != nil {
		t.Error("a zero budget should be stored as no budget")
	}
	// A rule that cannot apply is not kept, so nothing looks set that is not:
	// a balance has no pace, and a sampled meter's silence is its account's.
	view, err = w.e.UpdateMeter(ctx, id, MeterInput{Rules: &core.Rules{Low: ptr(20.0), Pace: true, StaleAfter: ptr(int64(3600))}}, "zh")
	w.must(err)
	if view.Rules.Low == nil || view.Rules.Pace || view.Rules.StaleAfter != nil {
		t.Errorf("rules kept on a sampled balance = %+v", view.Rules)
	}
	_, err = w.e.Ingest(ctx, []Push{{Meter: "spent", Kind: core.Counter, Unit: "USD", Value: ptr(3.0)}})
	w.must(err)
	spent, err := w.store.PushedMeter(ctx, "spent")
	w.must(err)
	counter, err := w.e.UpdateMeter(ctx, spent.ID, MeterInput{
		Rules: &core.Rules{Low: ptr(1.0), RunwayDays: ptr(3.0), Anomaly: ptr(true), Budget: ptr(50.0), StaleAfter: ptr(int64(3600))},
	}, "zh")
	w.must(err)
	if r := counter.Rules; r.Low != nil || r.RunwayDays != nil || r.Anomaly != nil || r.Budget == nil || r.StaleAfter == nil {
		t.Errorf("rules kept on a pushed counter = %+v", r)
	}
	// Defaults show through where the meter says nothing.
	view, _ = w.e.UpdateMeter(ctx, id, MeterInput{Rules: &core.Rules{}}, "zh")
	if view.Effective.RunwayDays != 7 || !view.Effective.Anomaly {
		t.Errorf("effective = %+v", view.Effective)
	}

	if _, err := w.e.UpdateMeter(ctx, id, MeterInput{Unit: ptr("USD")}, "zh"); !IsInvalid(err) {
		t.Errorf("a sampled meter's unit: %v", err)
	}
	if err := w.e.DeleteMeter(ctx, id); !IsInvalid(err) {
		t.Errorf("deleting a meter that is still reported: %v", err)
	}
	if _, err := w.e.MuteMeter(ctx, id, ptr(w.now.Add(-time.Hour)), "zh"); !IsInvalid(err) {
		t.Errorf("muting until the past: %v", err)
	}
	muted, err := w.e.MuteMeter(ctx, id, ptr(w.now.Add(time.Hour)), "zh")
	w.must(err)
	if !muted.Muted {
		t.Error("not muted")
	}
	unmuted, _ := w.e.MuteMeter(ctx, id, nil, "zh")
	if unmuted.Muted || unmuted.MutedUntil != nil {
		t.Error("still muted")
	}

	// The provider stops reporting the meter: now it can go.
	w.advance(time.Hour)
	w.api.set(200, `{"meters":[{"key":"other","value":1}]}`)
	w.sample(account.ID)
	w.must(w.e.DeleteMeter(ctx, id))
}

func TestIngestValidation(t *testing.T) {
	w := newWorld(t)
	one := 1.0
	bad := [][]Push{
		nil,
		{{Meter: "Bad Key", Kind: core.Counter, Value: &one}},
		{{Meter: "ok", Kind: core.Counter}},
		{{Meter: "ok", Value: &one}}, // a new meter needs a kind
		{{Meter: "ok", Kind: "gauge", Value: &one}},
		{{Meter: "ok", Kind: core.Counter, Value: &one, Limit: ptr(-1.0)}},
		{{Meter: "ok", Kind: core.Counter, Value: &one, At: ptr(w.now.Add(time.Hour))}},
	}
	for i, pushes := range bad {
		if _, err := w.e.Ingest(ctx, pushes); !IsInvalid(err) {
			t.Errorf("batch %d: err = %v", i, err)
		}
	}
	if all, _ := w.e.Meters(ctx, "zh"); len(all) != 0 {
		t.Fatalf("a rejected batch left a meter behind: %+v", all)
	}

	// A batch with history, oldest first; the meter ends on the newest.
	meters, err := w.e.Ingest(ctx, []Push{
		{Meter: "claude-code", Name: "Claude Code", Kind: core.Counter, Unit: "USD", Value: ptr(10.0), At: ptr(w.now.Add(-2 * time.Hour))},
		{Meter: "claude-code", Value: ptr(12.0), At: ptr(w.now.Add(-time.Hour))},
		{Meter: "claude-code", Value: ptr(15.0)},
	})
	w.must(err)
	if len(meters) != 1 || meters[0].Name != "Claude Code" || *meters[0].Value != 15 || meters[0].Unit != "USD" {
		t.Fatalf("meters = %+v", meters)
	}
	if n, _ := w.store.ReadingCount(ctx, meters[0].ID); n != 3 {
		t.Errorf("readings = %d", n)
	}

	// A name chosen in the dashboard is not overwritten by the reporter.
	_, err = w.e.UpdateMeter(ctx, meters[0].ID, MeterInput{Name: ptr("我的 Claude")}, "zh")
	w.must(err)
	meters, err = w.e.Ingest(ctx, []Push{{Meter: "claude-code", Name: "Claude Code", Unit: "CNY", Value: ptr(16.0)}})
	w.must(err)
	if meters[0].Name != "我的 Claude" || meters[0].Unit != "CNY" {
		t.Fatalf("after a second push = %+v", meters[0])
	}
}

func TestChannelEditing(t *testing.T) {
	w := newWorld(t)
	hook := newHook(t)
	bad := []ChannelInput{
		{},
		{Name: ptr("x"), Type: ptr("pigeon"), Config: map[string]string{"url": hook.srv.URL}},
		{Name: ptr("x"), Type: ptr("feishu")},
		{Name: ptr("x"), Type: ptr("feishu"), Config: map[string]string{"url": "not a url"}},
		{Name: ptr("x"), Type: ptr("webhook"), Config: map[string]string{"url": hook.srv.URL}, Topics: &[]string{"gossip"}},
		{Name: ptr("x"), Type: ptr("webhook"), Config: map[string]string{"url": hook.srv.URL}, MinSeverity: ptr("loud")},
		{Name: ptr("ops"), Type: ptr("webhook"), Config: map[string]string{"url": hook.srv.URL}},
	}
	for i, in := range bad {
		if _, err := w.e.CreateChannel(ctx, in, "zh"); !IsInvalid(err) {
			t.Errorf("input %d: err = %v", i, err)
		}
	}

	view, err := w.e.CreateChannel(ctx, ChannelInput{
		Name: ptr("飞书群"), Type: ptr("feishu"),
		Config: map[string]string{"url": "https://open.feishu.cn/open-apis/bot/v2/hook/secret-0001", "secret": "sign-secret-0001"},
	}, "zh")
	w.must(err)
	if view.TypeName != "飞书" || view.Config["url"] != "https://open.feishu.cn/••••" || len(view.Topics) != 4 || view.MinSeverity != core.Info {
		t.Fatalf("view = %+v", view)
	}

	// An edit that leaves the secrets blank keeps them.
	view, err = w.e.UpdateChannel(ctx, view.ID, ChannelInput{
		Name: ptr("飞书"), Config: map[string]string{"url": "", "secret": ""},
		Topics: &[]string{core.TopicDigest, core.TopicDates}, MinSeverity: ptr("warning"),
	}, "zh")
	w.must(err)
	stored, _ := w.store.Channel(ctx, view.ID)
	if stored.Config["url"] != "https://open.feishu.cn/open-apis/bot/v2/hook/secret-0001" || stored.Config["secret"] != "sign-secret-0001" {
		t.Fatalf("stored config = %v", stored.Config)
	}
	// Topics come back in their canonical order.
	if len(view.Topics) != 2 || view.Topics[0] != core.TopicDates || view.Topics[1] != core.TopicDigest {
		t.Errorf("topics = %v", view.Topics)
	}
	if _, err := w.e.UpdateChannel(ctx, view.ID, ChannelInput{Type: ptr("slack")}, "zh"); !IsInvalid(err) {
		t.Errorf("changing the type: %v", err)
	}
	w.must(w.e.DeleteChannel(ctx, view.ID))
}

func TestTokens(t *testing.T) {
	w := newWorld(t)
	bad := []TokenInput{
		{}, {Name: "x"}, {Name: "x", Scopes: []string{"root"}},
		{Name: "x", Scopes: []string{core.ScopeRead}, ExpiresAt: ptr(w.now.Add(-time.Hour))},
	}
	for i, in := range bad {
		if _, err := w.e.CreateToken(ctx, in); !IsInvalid(err) {
			t.Errorf("input %d: err = %v", i, err)
		}
	}
	token, err := w.e.CreateToken(ctx, TokenInput{Name: "hermes", Scopes: []string{core.ScopeAct, core.ScopeRead, core.ScopeRead}})
	w.must(err)
	if !strings.HasPrefix(token.Secret, "qp_") || len(token.Scopes) != 2 || token.Scopes[0] != core.ScopeRead {
		t.Fatalf("token = %+v", token)
	}
	got, ok := w.e.Authenticate(ctx, token.Secret)
	if !ok || got.Name != "hermes" {
		t.Fatalf("authenticate = %+v, %v", got, ok)
	}
	if _, err := w.e.CreateToken(ctx, TokenInput{Name: "Hermes", Scopes: []string{core.ScopeRead}}); !IsInvalid(err) {
		t.Errorf("duplicate name: %v", err)
	}
	list, _ := w.e.Tokens(ctx)
	if len(list) != 1 || strings.Contains(list[0].Prefix, token.Secret[10:]) {
		t.Fatalf("list = %+v", list)
	}
	w.must(w.e.DeleteToken(ctx, token.ID))
	if _, ok := w.e.Authenticate(ctx, token.Secret); ok {
		t.Fatal("a revoked token still works")
	}
}

func TestSettingsChangeRejudgesEverything(t *testing.T) {
	w := newWorld(t)
	w.seed("wallet", core.Balance, "CNY", 10*24, 120, func(int) float64 { return 0.5 }) // ten days of runway
	w.push("wallet", 120)
	if len(w.open()) != 0 {
		t.Fatal("open at ten days with a seven-day lead")
	}

	s, _ := w.e.Settings(ctx)
	s.RunwayDays = 14
	s.BaseCurrency = " cny "
	s.FX = map[string]float64{"usd": 7.2, "CNY": 1}
	s.PublicURL = "https://pulse.example/"
	saved, err := w.e.UpdateSettings(ctx, s)
	w.must(err)
	if saved.BaseCurrency != "CNY" || saved.FX["USD"] != 7.2 || len(saved.FX) != 1 || saved.PublicURL != "https://pulse.example" {
		t.Fatalf("saved = %+v", saved)
	}
	// With a two-week lead, ten days is already short.
	if open := w.open(); len(open) != 1 || open[0].Reason != core.ReasonRunway {
		t.Fatalf("open = %+v", open)
	}

	s.Timezone = "Nowhere/Land"
	if _, err := w.e.UpdateSettings(ctx, s); !IsInvalid(err) {
		t.Errorf("invalid settings: %v", err)
	}
}

func TestOverview(t *testing.T) {
	w := newWorld(t)
	empty, err := w.e.Overview(ctx, "zh")
	w.must(err)
	if empty.Status != statusOK || empty.Counts.Accounts != 0 || empty.Counts.Channels != 1 || empty.Timezone != "Asia/Shanghai" {
		t.Fatalf("empty overview = %+v", empty)
	}
	// Lists are never null: the dashboard iterates them without checking.
	if empty.Attention == nil || empty.Meters == nil || empty.Accounts == nil || empty.Upcoming == nil || empty.Spend.Unconverted == nil {
		t.Fatal("an empty overview has null lists")
	}

	w.api.balance(100)
	account := w.account("DeepSeek")
	w.sample(account.ID)
	w.seed("slow", core.Balance, "CNY", 10*24, 80, func(int) float64 { return 0.5 })
	w.push("slow", 80)
	w.empty("gone")
	_, err = w.e.CreateReminder(ctx, ReminderInput{Title: ptr("soon"), Rule: &recur.Rule{Freq: recur.Monthly, Day: 20}}, "zh")
	w.must(err)
	_, err = w.e.CreateReminder(ctx, ReminderInput{Title: ptr("far"), Rule: &recur.Rule{Freq: recur.Yearly, Month: 6, Day: 1}}, "zh")
	w.must(err)

	o, err := w.e.Overview(ctx, "en")
	w.must(err)
	if o.Status != statusCritical || len(o.Attention) != 2 {
		t.Fatalf("status = %s, attention = %d", o.Status, len(o.Attention))
	}
	// Worst first, and in the language asked for.
	if o.Attention[0].Headline != "Balance is empty · gone" || o.Attention[1].Severity != core.Warning {
		t.Errorf("attention = %q, %q", o.Attention[0].Headline, o.Attention[1].Headline)
	}
	if len(o.Meters) != 3 || o.Meters[0].Status != statusCritical || o.Meters[1].Status != statusWarning || o.Meters[2].Status != statusOK {
		t.Fatalf("meters = %s %s %s", o.Meters[0].Status, o.Meters[1].Status, o.Meters[2].Status)
	}
	if len(o.Meters[1].Incidents) != 1 || len(o.Meters[1].Spark) == 0 || o.Meters[1].Forecast == nil {
		t.Errorf("warning meter = %+v", o.Meters[1])
	}
	if o.Meters[2].Display != "DeepSeek" || o.Meters[2].Account == nil || o.Meters[2].Account.ID != account.ID {
		t.Errorf("sampled meter = %+v", o.Meters[2])
	}
	// Only dates within the horizon are "upcoming".
	if len(o.Upcoming) != 1 || o.Upcoming[0].Title != "soon" {
		t.Fatalf("upcoming = %+v", o.Upcoming)
	}
	if o.Counts.Meters != 3 || o.Counts.Reminders != 2 || o.Counts.Accounts != 1 {
		t.Errorf("counts = %+v", o.Counts)
	}
}

func TestSpend(t *testing.T) {
	w := newWorld(t)
	// 12 CNY a day, 1.2 USD a day, a quota meter, and some unconvertible credits.
	w.seed("cny", core.Balance, "CNY", 20*24, 500, func(int) float64 { return 0.5 })
	w.seed("usd", core.Balance, "USD", 20*24, 50, func(int) float64 { return 0.05 })
	w.seed("eur", core.Balance, "EUR", 20*24, 50, func(int) float64 { return 0.05 })
	w.seed("credits", core.Balance, "credits", 20*24, 9000, func(int) float64 { return 10 })
	w.seed("quota", core.Quota, "%", 20*24, 40, func(int) float64 { return 1 })
	for key, value := range map[string]float64{"cny": 500, "usd": 50, "eur": 50, "credits": 9000, "quota": 40} {
		w.push(key, value)
	}
	leads := []int{0}
	_, err := w.e.CreateReminder(ctx, ReminderInput{
		Title: ptr("VPS"), Kind: ptr(core.ReminderRenewal), Rule: &recur.Rule{Freq: recur.Weekly, Weekday: 5},
		LeadDays: &leads, Amount: ptr(10.0), Currency: ptr("USD"),
	}, "zh")
	w.must(err)

	spend, err := w.e.Spend(ctx, "", "zh")
	w.must(err)
	if spend.Month != "2026-11" || spend.Base != "CNY" || !spend.IsCurrent || spend.DaysInMonth != 30 {
		t.Fatalf("spend = %+v", spend)
	}
	// 15 days and 10 hours into November.
	elapsed := 15 + 10.0/24
	wantTotal := elapsed*12 + elapsed*1.2*7
	if diff := spend.Total - wantTotal; diff > 0.01 || diff < -0.01 {
		t.Errorf("total = %v, want %v", spend.Total, wantTotal)
	}
	if want := 30 * (12 + 1.2*7); spend.Projected < want-0.5 || spend.Projected > want+0.5 {
		t.Errorf("projected = %v, want about %v", spend.Projected, want)
	}

	rows := map[string]SpendRow{}
	for _, row := range spend.Rows {
		rows[row.Display] = row
	}
	if _, listed := rows["quota"]; listed {
		t.Fatal("a quota was counted as spend")
	}
	if rows["credits"].Base != nil || rows["credits"].Spend < 3700 {
		t.Errorf("credits = %+v", rows["credits"])
	}
	if rows["eur"].Base != nil {
		t.Error("EUR has no rate and must not be converted")
	}
	if len(spend.Unconverted) != 1 || spend.Unconverted[0] != "EUR" {
		t.Errorf("unconverted = %v", spend.Unconverted)
	}
	if spend.Rows[0].Display != "cny" {
		t.Errorf("rows are ordered by size: %s first", spend.Rows[0].Display)
	}

	// Fridays in November 2026: the 6th, 13th, 20th, 27th.
	if len(spend.Renewals) != 4 || spend.Renewals[0].Date.String() != "2026-11-06" || spend.Fixed != 4*10*7 {
		t.Fatalf("renewals = %+v, fixed = %v", spend.Renewals, spend.Fixed)
	}

	// One bar per day so far, and they add up to the total.
	if len(spend.Daily) != 16 {
		t.Fatalf("daily = %d days", len(spend.Daily))
	}
	sum := 0.0
	for _, day := range spend.Daily {
		sum += day.Amount
	}
	if diff := sum - spend.Total; diff > 0.01 || diff < -0.01 {
		t.Errorf("daily bars sum to %v, total is %v", sum, spend.Total)
	}

	// Six months of history, this month last.
	if len(spend.History) != 6 || spend.History[5].Month != "2026-11" || spend.History[4].Month != "2026-10" {
		t.Fatalf("history = %+v", spend.History)
	}
	// October only has data from the 27th at 10:00.
	if october := spend.History[4].Total; october < 4.5*20.4 || october > 4.7*20.4 {
		t.Errorf("october = %v", october)
	}

	past, err := w.e.Spend(ctx, "2026-10", "zh")
	w.must(err)
	if past.IsCurrent || past.Projected != past.Total || past.DaysElapsed != 31 {
		t.Errorf("past month = %+v", past)
	}
	if _, err := w.e.Spend(ctx, "November", "zh"); !IsInvalid(err) {
		t.Errorf("bad month: %v", err)
	}

	// The overview carries the same headline numbers.
	o, _ := w.e.Overview(ctx, "zh")
	if diff := o.Spend.Total - spend.Total; diff > 0.01 || diff < -0.01 || o.Spend.Fixed != spend.Fixed {
		t.Errorf("overview spend = %+v", o.Spend)
	}
}

func TestSeries(t *testing.T) {
	w := newWorld(t)
	meter := w.seed("wallet", core.Balance, "CNY", 60*24, 300, func(ago int) float64 {
		if ago == 100 {
			return -500 // a top-up a hundred hours ago
		}
		return 0.5
	})
	w.push("wallet", 300)

	week, err := w.e.Series(ctx, meter.ID, "7d", "zh")
	w.must(err)
	if len(week.Points) < 100 || len(week.Points) > 400 {
		t.Fatalf("points = %d", len(week.Points))
	}
	if !week.Points[0].T.Equal(week.From) {
		t.Errorf("the line should start at the edge of the chart: %v vs %v", week.Points[0].T, week.From)
	}
	if len(week.Daily) != 8 || len(week.TopUps) != 1 || week.TopUps[0].Amount != 500 {
		t.Fatalf("daily = %d, top-ups = %+v", len(week.Daily), week.TopUps)
	}
	// 300 left at 12 a day: the projection ends at zero in 25 days — beyond
	// this chart's horizon, so it stops at the edge instead.
	if len(week.Projection) != 2 || week.Projection[0].V != 300 || week.Projection[1].V <= 0 {
		t.Fatalf("projection = %+v", week.Projection)
	}
	year, err := w.e.Series(ctx, meter.ID, "1y", "zh")
	w.must(err)
	if len(year.Points) > 400 {
		t.Errorf("a long range must be thinned: %d points", len(year.Points))
	}
	if year.Projection[1].V != 0 {
		t.Errorf("within a year it does run out: %+v", year.Projection)
	}
	// Thinning keeps the extremes: the top-up is still visible.
	peak := 0.0
	for _, p := range year.Points {
		peak = max(peak, p.V)
	}
	if peak < 340 {
		t.Errorf("the top-up was thinned away: peak %v", peak)
	}

	if _, err := w.e.Series(ctx, meter.ID, "5m", "zh"); !IsInvalid(err) {
		t.Errorf("bad range: %v", err)
	}
	if _, err := w.e.Series(ctx, "mtr_missing", "7d", "zh"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing meter: %v", err)
	}
}

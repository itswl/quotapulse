package mcp_test

import (
	"strings"
	"testing"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/engine"
	"github.com/itswl/quotapulse/internal/recur"
)

// A client names its language the way the HTTP API's clients do — here by
// the endpoint's query — and everything a tool says in words follows.
func TestAToolAnswersInTheLanguageTheClientAskedFor(t *testing.T) {
	w := newWorld(t)
	_, err := w.engine.Ingest(ctx, []engine.Push{
		{Meter: "deepseek", Name: "DeepSeek", Kind: core.Balance, Unit: "CNY", Value: ptr(547.67)},
		{Meter: "gone", Name: "Wallet", Kind: core.Balance, Unit: "USD", Value: ptr(0.0)},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.engine.CreateReminder(ctx, engine.ReminderInput{
		Title: ptr("除夕"), Kind: ptr(core.ReminderHoliday),
		Rule: &recur.Rule{Freq: recur.Yearly, Calendar: recur.LunarCal, Month: 12, Day: 30},
	}, "en")
	if err != nil {
		t.Fatal(err)
	}
	token, err := w.engine.CreateToken(ctx, engine.TokenInput{Name: "reader", Scopes: []string{core.ScopeRead}})
	if err != nil {
		t.Fatal(err)
	}
	english, chinese := w.connectTo(w.url, token.Secret), w.connectTo(w.url+"?lang=zh", token.Secret)

	type attention struct {
		Summary string
		Open    []struct{ Headline string }
	}
	type dates struct {
		Dates []struct{ Rule string }
	}
	calm, result := call[attention](t, english, "attention", nil)
	if result.IsError || calm.Summary != "1 open: Balance is empty · Wallet." {
		t.Fatalf("in English: %+v %s", calm, errorText(result))
	}
	if listed, _ := call[dates](t, english, "list_dates", map[string]any{"query": "除夕"}); len(listed.Dates) != 1 || listed.Dates[0].Rule != "every year on lunar month 12 day 30" {
		t.Errorf("the rule in English: %+v", listed.Dates)
	}

	aloud, result := call[attention](t, chinese, "attention", nil)
	if result.IsError || !strings.HasPrefix(aloud.Summary, "1 个未处理：") || len(aloud.Open) != 1 || !strings.HasPrefix(aloud.Open[0].Headline, "余额已用尽") {
		t.Fatalf("in Chinese: %+v %s", aloud, errorText(result))
	}
	if listed, _ := call[dates](t, chinese, "list_dates", map[string]any{"query": "除夕"}); len(listed.Dates) != 1 || listed.Dates[0].Rule != "每年农历 12 月 30 日" {
		t.Errorf("the rule in Chinese: %+v", listed.Dates)
	}

	type one struct {
		Meter meterOut
	}
	deepseek, _ := call[one](t, chinese, "get_meter", map[string]any{"meter": "DeepSeek"})
	if deepseek.Meter.Summary != "剩余 547.67 CNY。" {
		t.Errorf("the meter in Chinese: %q", deepseek.Meter.Summary)
	}
	wallet, _ := call[one](t, chinese, "get_meter", map[string]any{"meter": "Wallet"})
	if !strings.HasPrefix(wallet.Meter.Summary, "剩余 0.00 USD") || !strings.HasSuffix(wallet.Meter.Summary, "。") {
		t.Errorf("the empty wallet in Chinese: %q", wallet.Meter.Summary)
	}

	weekly, result := call[struct {
		Date struct{ Rule string }
	}](t, chinese, "preview_date", map[string]any{"freq": "weekly", "weekday": 1})
	if result.IsError || weekly.Date.Rule != "每周一" {
		t.Errorf("weekly in Chinese: %+v %s", weekly, errorText(result))
	}
	fortnightly, result := call[struct {
		Date struct{ Rule string }
	}](t, chinese, "preview_date", map[string]any{"freq": "weekly", "weekday": 1, "interval": 2, "date": "2026-11-16"})
	if result.IsError || fortnightly.Date.Rule != "每 2 周的周一" {
		t.Errorf("fortnightly in Chinese: %+v %s", fortnightly, errorText(result))
	}
	monthly, result := call[struct {
		Date struct{ Rule string }
	}](t, chinese, "preview_date", map[string]any{"freq": "monthly", "day": 20})
	if result.IsError || monthly.Date.Rule != "每月 20 日" {
		t.Errorf("monthly in Chinese: %+v %s", monthly, errorText(result))
	}
	once, result := call[struct {
		Date struct{ Rule string }
	}](t, chinese, "preview_date", map[string]any{"freq": "once", "date": "2027-02-05"})
	if result.IsError || once.Date.Rule != "2027-02-05 这一天" {
		t.Errorf("once in Chinese: %+v %s", once, errorText(result))
	}
	// The other session, on the same token, is not affected.
	if again, _ := call[attention](t, english, "attention", nil); again.Summary != calm.Summary {
		t.Errorf("the English session changed: %q", again.Summary)
	}
}

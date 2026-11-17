package demo

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"quotapulse/internal/core"
	"quotapulse/internal/engine"
	"quotapulse/internal/notify"
	"quotapulse/internal/provider"
	"quotapulse/internal/secret"
	"quotapulse/internal/store"
)

// seed builds a demo at a pinned moment, as `quotapulse demo --at` does.
func seed(t *testing.T, lang string) *engine.Engine {
	t.Helper()
	return seedAt(t, lang, time.Date(2026, 11, 16, 2, 0, 0, 0, time.UTC))
}

func seedAt(t *testing.T, lang string, now time.Time) *engine.Engine {
	t.Helper()
	clock := func() time.Time { return now }
	world := New(lang, clock)
	st, err := store.Open(filepath.Join(t.TempDir(), "demo.db"), secret.FromPassphrase("demo"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	fetcher := provider.NewClient()
	fetcher.HTTP = world.HTTPClient()
	sender := notify.NewSender()
	sender.HTTP = world.HTTPClient()
	e := engine.New(engine.Options{
		Store: st, Fetcher: fetcher, Sender: sender, Now: clock,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err := world.Seed(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	return e
}

// The demo is what people try first and what the screenshots are taken
// from. It has to show every state the product has, every time.
func TestTheDemoShowsEveryState(t *testing.T) {
	ctx := context.Background()
	e := seed(t, "en")
	overview, err := e.Overview(ctx, "en")
	if err != nil {
		t.Fatal(err)
	}
	if overview.Status != "critical" || overview.Timezone != "Asia/Shanghai" {
		t.Errorf("status = %s, zone = %s", overview.Status, overview.Timezone)
	}

	statuses := map[string]int{}
	kinds := map[core.Kind]int{}
	for _, m := range overview.Meters {
		statuses[m.Status]++
		kinds[m.Kind]++
		if m.Value == nil {
			t.Errorf("%s has no value", m.Display)
		}
		if m.Kind != core.Quota && len(m.Spark) < 10 {
			t.Errorf("%s has no history to draw: %d points", m.Display, len(m.Spark))
		}
	}
	for _, status := range []string{"ok", "warning", "critical"} {
		if statuses[status] == 0 {
			t.Errorf("no meter is %s: %v", status, statuses)
		}
	}
	if kinds[core.Balance] < 4 || kinds[core.Quota] != 2 || kinds[core.Counter] != 1 {
		t.Errorf("kinds = %v", kinds)
	}

	open := map[string]bool{}
	for _, inc := range overview.Attention {
		open[inc.Kind+"/"+inc.Reason] = true
		if inc.Headline == "" {
			t.Errorf("an incident has no headline: %+v", inc.Incident)
		}
	}
	for _, want := range []string{"level/low", "level/runway", "budget/projected", "failing/", "due/"} {
		if !open[want] {
			t.Errorf("the demo has no open %q incident: %v", want, open)
		}
	}

	failing := 0
	for _, a := range overview.Accounts {
		if a.Status == core.StatusFailing {
			failing++
		}
	}
	if failing != 1 || len(overview.Accounts) != 7 {
		t.Errorf("%d of %d accounts are failing", failing, len(overview.Accounts))
	}

	// Dates: something tomorrow, something lunar, something with a price,
	// something with an age.
	var tomorrow, lunar, priced, aged bool
	for _, r := range overview.Upcoming {
		tomorrow = tomorrow || (r.Days != nil && *r.Days == 1)
		lunar = lunar || r.Lunar != nil
		priced = priced || r.Amount != nil
		aged = aged || r.Ordinal > 0
	}
	if !tomorrow || !lunar || !priced || !aged || len(overview.Upcoming) < 6 {
		t.Errorf("upcoming lacks variety: tomorrow=%v lunar=%v priced=%v aged=%v n=%d", tomorrow, lunar, priced, aged, len(overview.Upcoming))
	}
	if overview.Spend.Total <= 0 || overview.Spend.Fixed <= 0 || len(overview.Spend.Unconverted) != 0 {
		t.Errorf("spend = %+v", overview.Spend)
	}

	// Notifications went out — to the fake internet — and there is history.
	deliveries, _ := e.Deliveries(ctx, store.DeliveryFilter{Limit: 100})
	sent := 0
	for _, d := range deliveries {
		if d.Status == core.DeliverySent {
			sent++
		}
		if d.Status == core.DeliveryFailed {
			t.Errorf("a demo delivery failed: %s", d.LastError)
		}
	}
	if sent < 5 {
		t.Errorf("only %d deliveries were sent", sent)
	}
	closed, _ := e.Incidents(ctx, store.IncidentFilter{State: core.StateResolved}, "en")
	if len(closed) == 0 {
		t.Error("the demo has no past incident to show")
	}
	tokens, _ := e.Tokens(ctx)
	if len(tokens) != 2 {
		t.Errorf("tokens = %d", len(tokens))
	}
}

func TestTheDemoSpeaksChinese(t *testing.T) {
	ctx := context.Background()
	e := seed(t, "zh")
	settings, _ := e.Settings(ctx)
	if settings.Language != "zh" {
		t.Fatalf("language = %s", settings.Language)
	}
	reminders, err := e.Reminders(ctx, "zh")
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]bool{}
	for _, r := range reminders {
		titles[r.Title] = true
	}
	for _, want := range []string{"妈妈", "春节", "结婚纪念日"} {
		if !titles[want] {
			t.Errorf("no reminder called %s: %v", want, titles)
		}
	}
}

// Nothing in a demo may reach the network: sampling again must still be
// answered from inside.
func TestTheDemoKeepsWorkingWhenSampledAgain(t *testing.T) {
	ctx := context.Background()
	e := seed(t, "en")
	accounts, _ := e.Accounts(ctx, "en")
	for _, a := range accounts {
		if err := e.SampleAccount(ctx, a.ID); err != nil {
			t.Fatalf("%s: %v", a.Name, err)
		}
	}
	after, _ := e.Accounts(ctx, "en")
	for _, a := range after {
		if a.Status == core.StatusFailing && a.Provider != "moonshot" {
			t.Errorf("%s failed on a second sample: %s", a.Name, a.LastError)
		}
	}
}

// What the API hands out is read by people, charts and agents. None of them
// wants 1.6007670387311534 or a timestamp to the nanosecond, and an agent
// pays for every digit.
func TestWhatIsShownIsRounded(t *testing.T) {
	ctx := context.Background()
	// A real clock is never on the second.
	e := seedAt(t, "en", time.Date(2026, 11, 16, 2, 0, 0, 123456789, time.UTC))

	shown := map[string]any{}
	var err error
	if shown["overview"], err = e.Overview(ctx, "en"); err != nil {
		t.Fatal(err)
	}
	if shown["spend"], err = e.Spend(ctx, "", "en"); err != nil {
		t.Fatal(err)
	}
	if shown["dates"], err = e.Reminders(ctx, "en"); err != nil {
		t.Fatal(err)
	}
	meters, err := e.Meters(ctx, "en")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range meters {
		for _, span := range []string{"24h", "30d"} {
			if shown["series "+m.Display+" "+span], err = e.Series(ctx, m.ID, span, "en"); err != nil {
				t.Fatal(err)
			}
		}
	}

	longFraction := regexp.MustCompile(`[0-9]\.[0-9]{7,}`)
	subSecond := regexp.MustCompile(`T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]+`)
	for name, value := range shown {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if found := longFraction.Find(raw); found != nil {
			t.Errorf("%s: a number with a long tail: %s…", name, found)
		}
		if found := subSecond.Find(raw); found != nil {
			t.Errorf("%s: a time finer than a second: %s", name, found)
		}
	}
}

package engine

import (
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/store"
)

func TestLowBalanceIsAnnouncedOnceAndRecoveryToo(t *testing.T) {
	w := newWorld(t)
	w.api.balance(80)
	account := w.account("DeepSeek")
	w.sample(account.ID)
	meters, _ := w.store.MetersOf(ctx, account.ID)
	if len(meters) != 1 || meters[0].Name != "余额" {
		t.Fatalf("meters = %+v", meters)
	}
	w.rules(meters[0].ID, core.Rules{Low: ptr(50.0)})
	expect(t, w.dispatch())

	// It drops below the threshold: one message.
	w.advance(30 * time.Minute)
	w.api.balance(43.2)
	w.sample(account.ID)
	got := w.dispatch()
	expect(t, got, "余额不足 · DeepSeek")
	if got[0].Event != core.EventOpened || got[0].Severity != "critical" || got[0].Topic != core.TopicMeters {
		t.Errorf("message = %+v", got[0])
	}
	contains(t, got[0].Text, "余额 43.20 CNY，已低于提醒线 50.00 CNY。")
	if got[0].Link != "https://pulse.example/meters/"+meters[0].ID {
		t.Errorf("link = %q", got[0].Link)
	}

	// It stays low for hours: nothing more is said.
	for range 6 {
		w.advance(30 * time.Minute)
		w.api.balance(43.2)
		w.sample(account.ID)
	}
	expect(t, w.dispatch())
	if open := w.open(); len(open) != 1 || open[0].NotifyCount != 1 {
		t.Fatalf("open = %+v", open)
	}

	// Topped up: the incident closes, and whoever heard it open hears it close,
	// with the balance as it is now.
	w.advance(30 * time.Minute)
	w.api.balance(500)
	w.sample(account.ID)
	got = w.dispatch()
	expect(t, got, "已恢复 · DeepSeek")
	contains(t, got[0].Text, "余额现在是 500.00 CNY。")
	if got[0].Event != core.EventResolved {
		t.Errorf("event = %s", got[0].Event)
	}
	if len(w.open()) != 0 {
		t.Fatal("still open after the top-up")
	}
}

func TestEmptyBalanceNeedsNoThreshold(t *testing.T) {
	w := newWorld(t)
	w.seed("wallet", core.Balance, "USD", 5, 3, func(int) float64 { return 0 })
	w.push("wallet", 0)
	got := w.dispatch()
	expect(t, got, "余额已用尽 · wallet")
	if got[0].Severity != "critical" {
		t.Errorf("severity = %s", got[0].Severity)
	}
}

func TestRunwayWarnsThenEscalates(t *testing.T) {
	w := newWorld(t)
	// Ten days at 12 a day, 96 left: eight days of runway. Nothing yet.
	meter := w.seed("wallet", core.Balance, "CNY", 10*24, 96, func(int) float64 { return 0.5 })
	w.push("wallet", 96)
	expect(t, w.dispatch())

	// It opens the moment the runway reaches the seven-day lead time — a
	// day later, at 84 — and the message describes that moment.
	balance := 96.0
	burn := func(hours int) {
		for range hours {
			w.advance(time.Hour)
			balance -= 0.5
			w.push("wallet", balance)
		}
	}
	burn(36)
	got := w.dispatch()
	if len(got) != 1 {
		t.Fatalf("messages = %q", titles(got))
	}
	contains(t, got[0].Title, "余额约 ", "天后用尽 · wallet")
	contains(t, got[0].Title, "余额约 7 天后用尽 · wallet")
	contains(t, got[0].Text, "余额 84.00 CNY，近期日均消耗 12.00 CNY。照这个速度预计在 11月24日 用尽。")
	if got[0].Severity != "warning" || got[0].Event != core.EventOpened {
		t.Errorf("message = %+v", got[0])
	}
	open := w.open()
	if len(open) != 1 || open[0].Reason != core.ReasonRunway || open[0].SubjectID != meter.ID {
		t.Fatalf("open = %+v", open)
	}

	// Four more days pass. A warning is said once — no daily countdown —
	// until under two days are left, which is a different message.
	burn(4*24 + 12)
	got = w.dispatch()
	if len(got) != 1 || got[0].Event != core.EventEscalated || got[0].Severity != "critical" {
		t.Fatalf("messages = %q", titles(got))
	}
	contains(t, got[0].Title, "余额约 2 天后用尽 · wallet")

	// Critical and ignored: now it does repeat, once a day.
	burn(25)
	got = w.dispatch()
	if len(got) != 1 || got[0].Event != core.EventReminder {
		t.Fatalf("messages = %q", titles(got))
	}
	contains(t, got[0].Title, "仍未解决：余额约 ")
}

func TestRunwayDoesNotFlapAtTheBoundary(t *testing.T) {
	w := newWorld(t)
	w.seed("wallet", core.Balance, "CNY", 10*24, 80, func(int) float64 { return 0.5 })
	w.push("wallet", 80) // 6.7 days: opens
	expect(t, w.dispatch(), "余额约 6.7 天后用尽 · wallet")

	// A small top-up lifts it to 7.5 days — just past the line it opened at.
	// It stays open: closing here would reopen at the next sample.
	w.advance(time.Minute)
	w.push("wallet", 90)
	expect(t, w.dispatch())
	if len(w.open()) != 1 {
		t.Fatal("resolved inside the margin")
	}

	// A real top-up clears it.
	w.advance(time.Minute)
	w.push("wallet", 300)
	expect(t, w.dispatch(), "已恢复 · wallet")
}

// The false alarm that prompted the rewrite of the spike rule: 2.53 spent in
// a day against a usual 0.10, with 547 in the account.
func TestSmallBurstOnALargeBalanceIsNotAnIncident(t *testing.T) {
	w := newWorld(t)
	w.seed("deepseek", core.Balance, "CNY", 14*24, 547.67, func(ago int) float64 {
		if ago < 24 {
			return 2.53 / 24
		}
		return 0.1 / 24
	})
	w.push("deepseek", 547.67)
	expect(t, w.dispatch())
	if open := w.open(); len(open) != 0 {
		t.Fatalf("open = %+v", open)
	}
}

// A leaked key: fifty an hour for three hours.
func TestLeakOpensACriticalBurstIncident(t *testing.T) {
	w := newWorld(t)
	w.seed("deepseek", core.Balance, "CNY", 14*24, 397, func(ago int) float64 {
		if ago < 3 {
			return 50
		}
		return 0.1 / 24
	})
	w.push("deepseek", 397)
	got := w.dispatch()
	expect(t, got, "消耗突增 · deepseek")
	if got[0].Severity != "critical" {
		t.Errorf("severity = %s", got[0].Severity)
	}
	contains(t, got[0].Text, "最近 3 小时花了 150.00 CNY，照这个速度余额 397.00 CNY 只够 7 小时 56 分钟。",
		"超过平时的 100 倍。", "按平时的速度可用 超过 2 年。")

	// The burst rule can be turned off per meter.
	open := w.open()
	w.rules(open[0].SubjectID, core.Rules{Anomaly: ptr(false), RunwayDays: ptr(0.0)})
	if len(w.open()) != 0 {
		t.Fatal("still open with both rules off")
	}
}

func TestThinHistoryNeverAlerts(t *testing.T) {
	w := newWorld(t)
	// Twelve hours of heavy burn: an estimate exists, but it is not acted on.
	w.seed("wallet", core.Balance, "CNY", 12, 10, func(int) float64 { return 5 })
	w.push("wallet", 10)
	expect(t, w.dispatch())
}

func TestQuotaRules(t *testing.T) {
	w := newWorld(t)
	reset := w.now.Add(90 * time.Minute)
	window := int64(5 * 3600)
	quota := func(value float64) {
		t.Helper()
		_, err := w.e.Ingest(ctx, []Push{{
			Meter: "plan", Name: "GLM · 5 小时窗口", Kind: core.Quota, Unit: "%", Value: &value,
			Limit: ptr(100.0), ResetsAt: &reset, Window: &window,
		}})
		w.must(err)
	}
	quota(60)
	meter, _ := w.store.PushedMeter(ctx, "plan")
	w.rules(meter.ID, core.Rules{Low: ptr(10.0)})
	expect(t, w.dispatch())

	w.advance(10 * time.Minute)
	quota(8)
	got := w.dispatch()
	expect(t, got, "配额剩余 8% · GLM · 5 小时窗口")
	contains(t, got[0].Text, "已低于提醒线 10%。", "11:30 重置（1 小时 20 分钟后）。")
	if got[0].Severity != "warning" {
		t.Errorf("a quota that resets in an hour is not critical: %s", got[0].Severity)
	}

	// Used up. It resets within the day, so still only a warning — but the
	// reason is worse, and that is worth saying.
	w.advance(10 * time.Minute)
	quota(0)
	got = w.dispatch()
	expect(t, got, "配额已用尽 · GLM · 5 小时窗口")
	if got[0].Event != core.EventEscalated || got[0].Severity != "warning" {
		t.Errorf("message = %+v", got[0])
	}

	// The window resets.
	w.advance(2 * time.Hour)
	reset = w.now.Add(5 * time.Hour)
	quota(100)
	expect(t, w.dispatch(), "配额已恢复 · GLM · 5 小时窗口")
}

func TestQuotaThatStaysEmptyForDaysIsCritical(t *testing.T) {
	w := newWorld(t)
	reset := w.now.Add(6 * 24 * time.Hour)
	_, err := w.e.Ingest(ctx, []Push{{Meter: "weekly", Kind: core.Quota, Unit: "%", Value: ptr(0.0), Limit: ptr(100.0), ResetsAt: &reset}})
	w.must(err)
	got := w.dispatch()
	expect(t, got, "配额已用尽 · weekly")
	if got[0].Severity != "critical" {
		t.Errorf("severity = %s", got[0].Severity)
	}
}

func TestQuotaPaceIsOptIn(t *testing.T) {
	w := newWorld(t)
	reset := w.now.Add(3 * time.Hour) // two of five hours gone
	window := int64(5 * 3600)
	push := func() {
		_, err := w.e.Ingest(ctx, []Push{{Meter: "plan", Kind: core.Quota, Unit: "%", Value: ptr(20.0), Limit: ptr(100.0), ResetsAt: &reset, Window: &window}})
		w.must(err)
	}
	push() // 80% used in 40% of the window
	expect(t, w.dispatch())

	meter, _ := w.store.PushedMeter(ctx, "plan")
	w.rules(meter.ID, core.Rules{Pace: true})
	got := w.dispatch()
	expect(t, got, "配额将提前用尽 · plan")
	contains(t, got[0].Text, "按当前的速度将在 10:30 用尽，比 13:00 的重置早 2 小时 30 分钟。")
}

func TestBudget(t *testing.T) {
	w := newWorld(t)
	// A counter that has climbed 4 a day all month: 62 by the 16th at 10:00.
	total := 0.0
	meter := w.seed("claude-code", core.Counter, "USD", 20*24, 0, func(int) float64 { return 0 })
	_ = meter
	// rebuild as a rising counter
	w.must(w.store.DeleteMeter(ctx, meter.ID))
	meter = core.Meter{Key: "claude-code", Name: "Claude Code", Kind: core.Counter, Unit: "USD"}
	w.must(w.store.CreateMeter(ctx, &meter, w.now.Add(-20*24*time.Hour)))
	var readings []core.Reading
	for ago := 20 * 24; ago >= 0; ago-- {
		readings = append(readings, core.Reading{At: w.now.Add(-time.Duration(ago) * time.Hour), Value: total})
		total += 4.0 / 24
	}
	w.must(w.store.ImportReadings(ctx, &meter, readings))
	total -= 4.0 / 24

	// Month to date is about 61.7; at 4 a day the month ends near 120.
	w.rules(meter.ID, core.Rules{Budget: ptr(200.0)})
	expect(t, w.dispatch())

	w.rules(meter.ID, core.Rules{Budget: ptr(100.0)})
	got := w.dispatch()
	expect(t, got, "预算预计超支 · Claude Code")
	contains(t, got[0].Text, "本月已花 61.67 USD", "预计月底达到 120.00 USD", "超出预算 100.00 USD")
	if got[0].Severity != "warning" {
		t.Errorf("severity = %s", got[0].Severity)
	}

	// Actually past it: a worse reason, said once more.
	w.rules(meter.ID, core.Rules{Budget: ptr(50.0)})
	got = w.dispatch()
	expect(t, got, "预算已超支 · Claude Code")
	if got[0].Event != core.EventEscalated {
		t.Errorf("event = %s", got[0].Event)
	}

	// A counter never "runs out": no level incident, whatever its value.
	for _, inc := range w.open() {
		if inc.Kind != core.IncidentBudget {
			t.Errorf("unexpected incident %+v", inc)
		}
	}
}

func TestStalePushedMeter(t *testing.T) {
	w := newWorld(t)
	_, err := w.e.Ingest(ctx, []Push{{Meter: "reporter", Kind: core.Counter, Unit: "USD", Value: ptr(1.0)}})
	w.must(err)
	meter, _ := w.store.PushedMeter(ctx, "reporter")
	w.rules(meter.ID, core.Rules{StaleAfter: ptr(int64(3600))})

	w.advance(50 * time.Minute)
	w.must(w.e.EvaluateStale(ctx))
	expect(t, w.dispatch())

	w.advance(3 * time.Hour)
	w.must(w.e.EvaluateStale(ctx))
	got := w.dispatch()
	expect(t, got, "数据中断 · reporter")
	contains(t, got[0].Text, "已有 3 小时 50 分钟没有收到新的读数")

	w.push("reporter", 2)
	expect(t, w.dispatch(), "数据已恢复 · reporter")
}

func TestFailingAccount(t *testing.T) {
	w := newWorld(t)
	w.api.balance(100)
	account := w.account("DeepSeek")
	w.sample(account.ID)

	// One failure is usually the network: nothing is said, but it is looked
	// at again within a minute.
	w.advance(30 * time.Minute)
	w.api.set(502, `{"message":"bad gateway"}`)
	w.sample(account.ID)
	expect(t, w.dispatch())
	got, _ := w.store.Account(ctx, account.ID)
	if got.Status != core.StatusFailing || got.NextAt.Sub(w.now) != time.Minute {
		t.Fatalf("account = %+v", got)
	}

	// Twice in a row is an incident.
	w.advance(time.Minute)
	w.sample(account.ID)
	messages := w.dispatch()
	expect(t, messages, "无法读取 · DeepSeek")
	contains(t, messages[0].Text, "连续 2 次读取失败：HTTP 502: bad gateway。", "恢复之前，这个账户的额度不会更新。", "上次成功: 10:00")
	if messages[0].Topic != core.TopicSystem || messages[0].Severity != "warning" {
		t.Errorf("message = %+v", messages[0])
	}

	// The meter keeps its last value and says where it stands.
	views, err := w.e.Meters(ctx, "zh")
	w.must(err)
	if views[0].Status != statusFailing || *views[0].Value != 100 {
		t.Fatalf("meter view = %+v", views[0])
	}

	// Back again.
	w.advance(5 * time.Minute)
	w.api.balance(99)
	w.sample(account.ID)
	messages = w.dispatch()
	expect(t, messages, "恢复读取 · DeepSeek")
	contains(t, messages[0].Text, "中断了 5 分钟。")
}

func TestRefusedCredentialIsAnIncidentAtOnce(t *testing.T) {
	w := newWorld(t)
	w.api.set(401, `{"error":{"message":"Incorrect API key provided: sk-test-0001"}}`)
	account := w.account("DeepSeek")
	w.sample(account.ID)
	got := w.dispatch()
	expect(t, got, "无法读取 · DeepSeek")
	contains(t, got[0].Text, "读取失败：HTTP 401: Incorrect API key provided: •••。", "上次成功: 从未成功")
	for _, d := range w.deliveries() {
		if string(d.Message) == "" || containsText(string(d.Message), "sk-test-0001") {
			t.Fatal("the key reached a stored message")
		}
	}
}

func containsText(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestOneAccountSeveralMeters(t *testing.T) {
	w := newWorld(t)
	reset := w.now.Add(3 * time.Hour).Unix()
	w.api.set(200, `{"meters":[
		{"key":"tokens-5h","name":"5-hour window","name_zh":"5 小时窗口","kind":"quota","unit":"%","value":99,"limit":100,"resets_at":`+itoa(reset)+`,"window":18000},
		{"key":"tools-1mo","name":"MCP tools","name_zh":"MCP 工具","kind":"quota","unit":"calls","value":923,"limit":1000}]}`)
	account := w.account("智谱")
	w.sample(account.ID)

	views, err := w.e.Meters(ctx, "zh")
	w.must(err)
	if len(views) != 2 {
		t.Fatalf("views = %+v", views)
	}
	names := map[string]MeterView{}
	for _, v := range views {
		names[v.Display] = v
	}
	window, ok := names["智谱 · 5 小时窗口"]
	if !ok || window.Quota == nil || window.Quota.RemainingPct != 99 || *window.Quota.ElapsedPct != 40 {
		t.Fatalf("window = %+v (names %v)", window.Quota, names)
	}
	tools := names["智谱 · MCP 工具"]
	if tools.Quota.RemainingPct != 92.3 || tools.Account.ProviderName != "假平台" {
		t.Fatalf("tools = %+v", tools)
	}

	// Hide one: the other is then simply "智谱".
	_, err = w.e.UpdateMeter(ctx, tools.ID, MeterInput{Hidden: ptr(true)}, "zh")
	w.must(err)
	views, _ = w.e.Meters(ctx, "zh")
	for _, v := range views {
		if v.ID == window.ID && v.Display != "智谱" {
			t.Errorf("display = %q", v.Display)
		}
	}
}

func itoa(n int64) string {
	raw, _ := time.Unix(n, 0).UTC().MarshalText()
	_ = raw
	digits := ""
	if n == 0 {
		return "0"
	}
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func TestHidingAMeterEndsItsIncidentQuietly(t *testing.T) {
	w := newWorld(t)
	w.seed("wallet", core.Balance, "CNY", 5, 3, func(int) float64 { return 0 })
	w.push("wallet", 0)
	expect(t, w.dispatch(), "余额已用尽 · wallet")
	meter, _ := w.store.PushedMeter(ctx, "wallet")
	_, err := w.e.UpdateMeter(ctx, meter.ID, MeterInput{Hidden: ptr(true)}, "zh")
	w.must(err)
	expect(t, w.dispatch()) // no "recovered": nothing recovered
	if len(w.open()) != 0 {
		t.Fatal("still open")
	}
	// And it is not judged while hidden.
	w.push("wallet", 0)
	if len(w.open()) != 0 {
		t.Fatal("a hidden meter opened an incident")
	}
}

func TestMutedMeterOpensSilentlyAndSpeaksWhenUnmuted(t *testing.T) {
	w := newWorld(t)
	w.seed("wallet", core.Balance, "CNY", 5, 3, func(int) float64 { return 0 })
	meter, _ := w.store.PushedMeter(ctx, "wallet")
	_, err := w.e.MuteMeter(ctx, meter.ID, ptr(w.now.Add(2*time.Hour)), "zh")
	w.must(err)

	w.push("wallet", 0)
	expect(t, w.dispatch())
	if open := w.open(); len(open) != 1 || open[0].NotifiedAt != nil {
		t.Fatalf("open = %+v", open)
	}

	// The mute runs out and the problem is still there: now it is said.
	w.advance(3 * time.Hour)
	w.push("wallet", 0)
	expect(t, w.dispatch(), "余额已用尽 · wallet")
}

func TestRenotifyUntilAcknowledged(t *testing.T) {
	w := newWorld(t)
	w.seed("wallet", core.Balance, "CNY", 5, 3, func(int) float64 { return 0 })
	w.push("wallet", 0)
	expect(t, w.dispatch(), "余额已用尽 · wallet")

	w.advance(23 * time.Hour)
	w.push("wallet", 0)
	expect(t, w.dispatch())

	w.advance(2 * time.Hour)
	w.push("wallet", 0)
	got := w.dispatch()
	expect(t, got, "仍未解决：余额已用尽 · wallet")
	if got[0].Event != core.EventReminder {
		t.Errorf("event = %s", got[0].Event)
	}

	// Acknowledged: it stays open, and stays quiet.
	_, err := w.e.Ack(ctx, w.open()[0].ID, true)
	w.must(err)
	w.advance(48 * time.Hour)
	w.push("wallet", 0)
	expect(t, w.dispatch())
	if len(w.open()) != 1 {
		t.Fatal("an acknowledged incident must stay open")
	}
}

func TestQuietHoursHoldBackWhatCanWait(t *testing.T) {
	w := newWorld(t)
	w.settings(func(s *core.Settings) { s.Quiet = core.QuietHours{Enabled: true, Start: "23:00", End: "08:00"} })
	w.at(2026, 11, 17, 2, 0)

	// A warning at two in the morning waits for eight.
	w.seed("slow", core.Balance, "CNY", 10*24, 80, func(int) float64 { return 0.5 })
	w.push("slow", 80)
	expect(t, w.dispatch())
	pending := w.deliveries()
	if len(pending) != 1 || pending[0].Status != core.DeliveryPending || !pending[0].NotBefore.Equal(time.Date(2026, 11, 17, 8, 0, 0, 0, shanghai)) {
		t.Fatalf("deliveries = %+v", pending)
	}

	// Something critical does not wait.
	w.seed("empty", core.Balance, "CNY", 5, 3, func(int) float64 { return 0 })
	w.push("empty", 0)
	expect(t, w.dispatch(), "余额已用尽 · empty")

	// At eight the held-back warning goes out.
	w.at(2026, 11, 17, 8, 0)
	expect(t, w.dispatch(), "余额约 6.7 天后用尽 · slow")
}

func TestAProblemThatClearsDuringQuietHoursIsNeverAnnounced(t *testing.T) {
	w := newWorld(t)
	w.settings(func(s *core.Settings) { s.Quiet = core.QuietHours{Enabled: true, Start: "23:00", End: "08:00"} })
	w.at(2026, 11, 17, 2, 0)
	w.seed("slow", core.Balance, "CNY", 10*24, 80, func(int) float64 { return 0.5 })
	w.push("slow", 80)

	w.advance(time.Hour)
	w.push("slow", 500) // topped up at three
	w.at(2026, 11, 17, 9, 0)
	expect(t, w.dispatch()) // neither the warning nor a recovery nobody was waiting for

	statuses := map[string]int{}
	for _, d := range w.deliveries() {
		statuses[d.Status]++
	}
	if statuses[core.DeliveryDropped] != 1 || len(statuses) != 1 {
		t.Fatalf("deliveries = %v", statuses)
	}
}

func TestMuteModeRecordsWithoutSending(t *testing.T) {
	w := newWorld(t, func(o *Options) { o.Mute = true })
	w.seed("wallet", core.Balance, "CNY", 5, 3, func(int) float64 { return 0 })
	w.push("wallet", 0)
	w.advance(time.Hour)
	w.push("wallet", 100)
	expect(t, w.dispatch())

	// The log shows what would have gone out, including the recovery.
	var events []string
	for _, d := range w.deliveries() {
		if d.Status != core.DeliveryDropped || d.LastError != mutedReason {
			t.Errorf("delivery = %+v", d)
		}
		events = append([]string{d.Event}, events...)
	}
	if len(events) != 2 || events[0] != core.EventOpened || events[1] != core.EventResolved {
		t.Fatalf("events = %v", events)
	}
}

func TestChannelFilters(t *testing.T) {
	w := newWorld(t)
	critical := newHook(t)
	dates := newHook(t)
	only := w.channel("pager", critical)
	_, err := w.e.UpdateChannel(ctx, only.ID, ChannelInput{MinSeverity: ptr("critical")}, "zh")
	w.must(err)
	calendar := w.channel("calendar", dates)
	_, err = w.e.UpdateChannel(ctx, calendar.ID, ChannelInput{Topics: &[]string{core.TopicDates}}, "zh")
	w.must(err)

	// A warning: ops only.
	w.seed("slow", core.Balance, "CNY", 10*24, 80, func(int) float64 { return 0.5 })
	w.push("slow", 80)
	w.e.Dispatch(ctx)
	if len(w.hook.take()) != 1 || len(critical.take()) != 0 || len(dates.take()) != 0 {
		t.Fatal("a warning went to the wrong channels")
	}

	// Critical: ops and the pager.
	w.seed("empty", core.Balance, "CNY", 5, 3, func(int) float64 { return 0 })
	w.push("empty", 0)
	w.e.Dispatch(ctx)
	if len(w.hook.take()) != 1 || len(critical.take()) != 1 || len(dates.take()) != 0 {
		t.Fatal("a critical incident went to the wrong channels")
	}

	// The recovery goes to whoever was told, even though "resolved" is not
	// itself critical.
	w.advance(time.Minute)
	w.push("empty", 50)
	w.e.Dispatch(ctx)
	if len(w.hook.take()) != 1 || len(critical.take()) != 1 || len(dates.take()) != 0 {
		t.Fatal("the recovery went to the wrong channels")
	}
}

// History brought in from another system has no numbers to build a sentence
// from — only what was said at the time. That is what is shown.
func TestImportedIncidentsKeepTheirOriginalWords(t *testing.T) {
	w := newWorld(t)
	resolved := w.now.Add(-time.Hour)
	for kind, reason := range map[string]string{core.IncidentLevel: core.ReasonBurst, core.IncidentFailing: "", core.IncidentDue: ""} {
		w.must(w.store.SaveIncident(ctx, core.Incident{
			ID: core.NewID("inc"), SubjectType: core.SubjectMeter, SubjectID: "mtr_gone", Kind: kind, Reason: reason,
			Severity: core.Warning, State: core.StateResolved, Title: "deepseek",
			Data:     map[string]any{importedKey: "Spending spike: today 2.53 is 25.3x the baseline"},
			OpenedAt: resolved, UpdatedAt: resolved, ResolvedAt: &resolved,
		}))
	}
	zh, err := w.e.Incidents(ctx, store.IncidentFilter{}, "zh")
	w.must(err)
	headlines := map[string]bool{}
	for _, inc := range zh {
		headlines[inc.Headline] = true
		if inc.Detail != "Spending spike: today 2.53 is 25.3x the baseline" || len(inc.Facts) != 0 {
			t.Errorf("imported incident = %+v", inc)
		}
	}
	for _, want := range []string{"消耗突增 · deepseek", "无法读取 · deepseek", "日期提醒 · deepseek"} {
		if !headlines[want] {
			t.Errorf("no headline %q in %v", want, headlines)
		}
	}
	en, _ := w.e.Incidents(ctx, store.IncidentFilter{}, "en")
	if en[0].Headline == zh[0].Headline {
		t.Error("the headline did not follow the language")
	}
}

func TestIncidentHistoryReadsInBothLanguages(t *testing.T) {
	w := newWorld(t)
	w.seed("wallet", core.Balance, "CNY", 5, 3, func(int) float64 { return 0 })
	w.push("wallet", 0)

	zh, err := w.e.Incidents(ctx, store.IncidentFilter{}, "zh")
	w.must(err)
	en, err := w.e.Incidents(ctx, store.IncidentFilter{}, "en")
	w.must(err)
	if zh[0].Headline != "余额已用尽 · wallet" || en[0].Headline != "Balance is empty · wallet" {
		t.Fatalf("headlines = %q / %q", zh[0].Headline, en[0].Headline)
	}
	if en[0].Detail != "The balance is 0.00 CNY." || en[0].Path != "/meters/"+en[0].SubjectID {
		t.Errorf("en = %+v", en[0])
	}
}

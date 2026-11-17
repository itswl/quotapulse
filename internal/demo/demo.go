// Package demo fills a QuotaPulse with believable data and stands in for the
// outside world, so the whole product can be tried — and photographed for the
// documentation — without a single real credential.
//
// Nothing here is a mock of QuotaPulse itself. The demo runs the real engine
// against a fake internet: the platforms it samples and the chat apps it
// notifies are answered from this package.
package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"quotapulse/internal/core"
	"quotapulse/internal/engine"
	"quotapulse/internal/recur"
)

// Password signs in to a demo instance.
const Password = "demo"

// World is the fake internet.
type World struct {
	lang string
	now  func() time.Time

	mu       sync.Mutex
	balances map[string]float64 // by platform host
	started  time.Time
}

// New builds a world. lang picks the language of the sample names.
func New(lang string, now func() time.Time) *World {
	if lang != "zh" {
		lang = "en"
	}
	return &World{lang: lang, now: now, started: now(), balances: map[string]float64{
		"api.deepseek.com":       547.67,
		"openrouter.ai":          18.40,
		"api.siliconflow.cn":     6.20,
		"api.tikhub.dev":         212.50,
		"open.bigmodel.cn":       38, // percent of the five-hour window left
		"api.moonshot.cn":        0,
		"open.volcengineapi.com": 1280.00,
	}}
}

func (w *World) pick(en, zh string) string {
	if w.lang == "zh" {
		return zh
	}
	return en
}

// RoundTrip answers every outgoing request locally. Nothing in a demo ever
// reaches the network.
func (w *World) RoundTrip(req *http.Request) (*http.Response, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	status, body := http.StatusOK, `{"code":0,"errcode":0,"ok":true}` // any chat app: accepted

	if balance, known := w.balances[req.URL.Host]; known {
		switch req.URL.Host {
		case "api.deepseek.com":
			body = fmt.Sprintf(`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"%.2f","granted_balance":"0.00","topped_up_balance":"%.2f"}]}`, balance, balance)
		case "openrouter.ai":
			body = fmt.Sprintf(`{"data":{"total_credits":150,"total_usage":%.4f}}`, 150-balance)
		case "api.siliconflow.cn":
			body = fmt.Sprintf(`{"code":20000,"message":"OK","status":true,"data":{"balance":"0.00","chargeBalance":"%.2f","totalBalance":"%.2f"}}`, balance, balance)
		case "api.tikhub.dev":
			body = fmt.Sprintf(`{"code":200,"user_data":{"balance":%.2f}}`, balance)
		case "open.volcengineapi.com":
			body = fmt.Sprintf(`{"ResponseMetadata":{"Action":"QueryBalanceAcct"},"Result":{"AvailableBalance":"%.2f"}}`, balance)
		case "open.bigmodel.cn":
			now := w.now()
			windowReset := w.started.Add(100 * time.Minute)
			for !windowReset.After(now) {
				windowReset = windowReset.Add(5 * time.Hour)
				balance = 100
			}
			monthReset := time.Date(now.Year(), now.Month()+1, 3, 0, 0, 0, 0, time.UTC)
			body = fmt.Sprintf(`{"code":200,"msg":"ok","success":true,"data":{"level":"pro","limits":[
				{"type":"TIME_LIMIT","unit":5,"number":1,"usage":1000,"currentValue":77,"remaining":923,"percentage":7,"nextResetTime":%d},
				{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":%d,"nextResetTime":%d}]}}`,
				monthReset.UnixMilli(), int(100-balance), windowReset.UnixMilli())
		case "api.moonshot.cn":
			status, body = http.StatusUnauthorized, `{"error":{"message":"Invalid Authentication","type":"invalid_authentication_error"}}`
		}
	}
	return &http.Response{
		StatusCode: status, Status: strconv.Itoa(status) + " " + http.StatusText(status),
		Header:  http.Header{"Content-Type": []string{"application/json"}},
		Body:    io.NopCloser(bytes.NewBufferString(body)),
		Request: req, ProtoMajor: 1, ProtoMinor: 1,
	}, nil
}

// HTTPClient is a client whose every request is answered by the world.
func (w *World) HTTPClient() *http.Client {
	return &http.Client{Transport: w, Timeout: 5 * time.Second}
}

func ptr[T any](v T) *T { return &v }

// history writes hourly readings for a pushed-in meter: `days` of them,
// ending at `final` now. spend returns what went in the hour that ended
// `ago` hours ago; a negative amount is a top-up.
func (w *World) history(ctx context.Context, e *engine.Engine, meter *core.Meter, days int, final float64, spend func(ago int, r *rand.Rand) float64) error {
	random := rand.New(rand.NewPCG(uint64(len(meter.Key))*7919, 42))
	hours := days * 24
	now := w.now().Truncate(time.Hour)
	readings := make([]core.Reading, hours+1)
	value := final
	for ago := 0; ago <= hours; ago++ {
		readings[hours-ago] = core.Reading{At: now.Add(-time.Duration(ago) * time.Hour), Value: math.Round(value*100) / 100, Limit: meter.Limit}
		amount := spend(ago, random)
		if meter.Kind == core.Counter {
			value -= amount
		} else {
			value += amount
		}
		value = math.Max(value, 0)
	}
	// The last reading is "now", so the meter reads as freshly sampled.
	readings[hours].At = w.now().Add(-4 * time.Minute)
	return e.Store().ImportReadings(ctx, meter, readings)
}

// workday shapes spend like a person's day: busy in the afternoon, quiet at
// night, lighter at the weekend.
func workday(at time.Time) float64 {
	hour := float64(at.Hour())
	shape := math.Exp(-math.Pow((hour-15)/5, 2)) + 0.08
	if weekday := at.Weekday(); weekday == time.Saturday || weekday == time.Sunday {
		shape *= 0.35
	}
	return shape
}

// Seed fills an empty install.
func (w *World) Seed(ctx context.Context, e *engine.Engine) error {
	st := e.Store()
	now := w.now()

	settings, err := e.Settings(ctx)
	if err != nil {
		return err
	}
	settings.Timezone, settings.Language = "Asia/Shanghai", w.lang
	settings.BaseCurrency, settings.FX = "CNY", map[string]float64{"USD": 7.16}
	settings.PublicURL = ""
	if _, err := e.UpdateSettings(ctx, settings); err != nil {
		return err
	}
	loc := settings.Location()
	local := now.In(loc)

	channel, err := e.CreateChannel(ctx, engine.ChannelInput{
		Name: ptr(w.pick("Feishu · me", "飞书 · 我")), Type: ptr("feishu"),
		Config: map[string]string{"url": "https://open.feishu.cn/open-apis/bot/v2/hook/demo-0000"},
	}, w.lang)
	if err != nil {
		return err
	}
	if _, err := e.CreateChannel(ctx, engine.ChannelInput{
		Name: ptr(w.pick("Phone (critical only)", "手机（仅严重）")), Type: ptr("ntfy"),
		Config: map[string]string{"topic": "demo-topic"}, MinSeverity: ptr("critical"),
		Topics: &[]string{core.TopicMeters, core.TopicSystem},
	}, w.lang); err != nil {
		return err
	}

	type account struct {
		name, provider, group string
		credential            map[string]string
	}
	ai := w.pick("AI", "AI 平台")
	accounts := []account{
		{"DeepSeek", "deepseek", ai, map[string]string{"api_key": "sk-demo-deepseek-0000"}},
		{"OpenRouter", "openrouter", ai, map[string]string{"api_key": "sk-or-demo-0000"}},
		{w.pick("GLM Coding Plan", "智谱 Coding Plan"), "glm-coding", ai, map[string]string{"api_key": "demo.glm-0000"}},
		{w.pick("SiliconFlow", "硅基流动"), "siliconflow", ai, map[string]string{"api_key": "sk-demo-sf-0000"}},
		{"Kimi", "moonshot", ai, map[string]string{"api_key": "sk-demo-kimi-0000"}},
		{"TikHub", "tikhub", w.pick("Data", "数据"), map[string]string{"api_key": "demo-tikhub-0000"}},
		{w.pick("Volcengine", "火山引擎"), "volcengine", w.pick("Cloud", "云服务"), map[string]string{"access_key_id": "AKLTdemo", "secret_access_key": "demo-secret-0000"}},
	}
	ids := map[string]string{}
	for _, a := range accounts {
		view, err := e.CreateAccount(ctx, engine.AccountInput{Name: &a.name, Provider: &a.provider, Group: &a.group, Credential: a.credential}, w.lang)
		if err != nil {
			return err
		}
		ids[a.provider] = view.ID
	}

	// History first, so that the first sample lands on a meter with a past.
	seed := func(provider, key, name string, kind core.Kind, unit string, rules core.Rules, days int, spend func(ago int, r *rand.Rand) float64) error {
		meter := core.Meter{AccountID: ids[provider], Key: key, Name: name, Kind: kind, Unit: unit, Rules: rules}
		if err := st.CreateMeter(ctx, &meter, now.AddDate(0, 0, -days)); err != nil {
			return err
		}
		var host string
		switch provider {
		case "deepseek":
			host = "api.deepseek.com"
		case "openrouter":
			host = "openrouter.ai"
		case "siliconflow":
			host = "api.siliconflow.cn"
		case "tikhub":
			host = "api.tikhub.dev"
		case "volcengine":
			host = "open.volcengineapi.com"
		}
		return w.history(ctx, e, &meter, days, w.balances[host], spend)
	}
	hourAt := func(ago int) time.Time { return local.Add(-time.Duration(ago) * time.Hour) }
	balance, name := core.Balance, w.pick("Balance", "余额")

	// Barely used: a long runway.
	if err := seed("deepseek", "balance-cny", name, balance, "CNY", core.Rules{Low: ptr(50.0)}, 60, func(ago int, r *rand.Rand) float64 {
		if ago == 31*24 {
			return -500 // a top-up a month ago
		}
		return 0.12 * workday(hourAt(ago)) * (0.4 + r.Float64()*1.2)
	}); err != nil {
		return err
	}
	// Burning steadily: under a week left.
	if err := seed("openrouter", "credits", w.pick("Credits", "额度"), balance, "USD", core.Rules{Low: ptr(5.0)}, 45, func(ago int, r *rand.Rand) float64 {
		if ago == 19*24+7 {
			return -50
		}
		return 0.38 * workday(hourAt(ago)) * (0.5 + r.Float64())
	}); err != nil {
		return err
	}
	// Already below its threshold.
	if err := seed("siliconflow", "balance", name, balance, "CNY", core.Rules{Low: ptr(10.0)}, 40, func(ago int, r *rand.Rand) float64 {
		return 0.055 * workday(hourAt(ago)) * (0.5 + r.Float64())
	}); err != nil {
		return err
	}
	if err := seed("tikhub", "balance", name, balance, "USD", core.Rules{Low: ptr(20.0)}, 60, func(ago int, r *rand.Rand) float64 {
		return 0.11 * workday(hourAt(ago)) * (0.3 + r.Float64()*1.4)
	}); err != nil {
		return err
	}
	if err := seed("volcengine", "balance", name, balance, "CNY", core.Rules{Low: ptr(200.0), Budget: ptr(600.0)}, 60, func(ago int, r *rand.Rand) float64 {
		if ago == 12*24+3 {
			return -1000
		}
		return 2.1 * workday(hourAt(ago)) * (0.6 + r.Float64()*0.8)
	}); err != nil {
		return err
	}

	// A local tool reporting what it costs: a counter that starts over each month.
	claude := core.Meter{Key: "claude-code", Name: "Claude Code", Kind: core.Counter, Unit: "USD", Rules: core.Rules{Budget: ptr(100.0), StaleAfter: ptr(int64(6 * 3600))}}
	if err := st.CreateMeter(ctx, &claude, now.AddDate(0, 0, -45)); err != nil {
		return err
	}
	monthStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	hoursIntoMonth := int(local.Sub(monthStart).Hours())
	var readings []core.Reading
	total, random := 0.0, rand.New(rand.NewPCG(7, 7))
	for ago := 45 * 24; ago >= 0; ago-- {
		at := local.Add(-time.Duration(ago) * time.Hour)
		if ago == hoursIntoMonth {
			total = 0
		}
		total += 0.52 * workday(at) * (0.4 + random.Float64()*1.2)
		readings = append(readings, core.Reading{At: at.Truncate(time.Hour), Value: math.Round(total*100) / 100})
	}
	readings[len(readings)-1].At = now.Add(-9 * time.Minute)
	if err := st.ImportReadings(ctx, &claude, readings); err != nil {
		return err
	}

	// Dates: a spread from tomorrow to months out, with every kind present.
	today := recur.DateOf(local)
	in := func(days int) recur.Date { return today.AddDays(days) }
	lunarOf := func(d recur.Date) recur.Rule {
		l := recur.LunarOf(d)
		return recur.Rule{Freq: recur.Yearly, Calendar: recur.LunarCal, Month: l.Month, Day: l.Day}
	}
	solar := func(d recur.Date, since int) recur.Rule {
		return recur.Rule{Freq: recur.Yearly, Month: d.M, Day: d.D, Since: since}
	}
	monthly := func(d recur.Date) recur.Rule { return recur.Rule{Freq: recur.Monthly, Day: d.D} }
	usd := "USD"
	reminders := []engine.ReminderInput{
		{Title: ptr(w.pick("Mom", "妈妈")), Kind: ptr(core.ReminderBirthday), Rule: ptr(lunarOf(in(1))), LeadDays: &[]int{3, 1, 0}},
		{Title: ptr("iCloud+ 2TB"), Kind: ptr(core.ReminderRenewal), Rule: ptr(monthly(in(3))), LeadDays: &[]int{3, 0}, Amount: ptr(68.0)},
		{Title: ptr("Claude Max"), Kind: ptr(core.ReminderRenewal), Rule: ptr(monthly(in(9))), LeadDays: &[]int{1, 0}, Amount: ptr(100.0), Currency: &usd},
		{Title: ptr("GitHub Copilot"), Kind: ptr(core.ReminderRenewal), Rule: ptr(monthly(in(17))), LeadDays: &[]int{0}, Amount: ptr(10.0), Currency: &usd},
		{Title: ptr(w.pick("Alex", "李雷")), Kind: ptr(core.ReminderBirthday), Rule: ptr(solar(in(24), today.Y-30)), LeadDays: &[]int{7, 0}},
		{Title: ptr(w.pick("Wedding anniversary", "结婚纪念日")), Kind: ptr(core.ReminderAnniversary), Rule: ptr(solar(in(38), today.Y-5)), LeadDays: &[]int{7, 1, 0}},
		{Title: ptr(w.pick("example.com domain", "example.com 域名")), Kind: ptr(core.ReminderExpiry), Rule: ptr(solar(in(52), 0)), LeadDays: &[]int{30, 7, 1}, Amount: ptr(68.5)},
		{Title: ptr(w.pick("Lunar New Year", "春节")), Kind: ptr(core.ReminderHoliday), Rule: &recur.Rule{Freq: recur.Yearly, Calendar: recur.LunarCal, Month: 1, Day: 1}, LeadDays: &[]int{7, 0}},
		{Title: ptr(w.pick("VPS, yearly", "服务器年付")), Kind: ptr(core.ReminderRenewal), Rule: ptr(solar(in(141), 0)), LeadDays: &[]int{14, 3, 0}, Amount: ptr(59.0), Currency: &usd},
		{Title: ptr(w.pick("Dad", "爸爸")), Kind: ptr(core.ReminderBirthday), Rule: ptr(lunarOf(in(203))), LeadDays: &[]int{3, 0}},
		{Title: ptr(w.pick("Passport expires", "护照到期")), Kind: ptr(core.ReminderExpiry), Rule: &recur.Rule{Freq: recur.Once, Date: ptr(recur.NewDate(today.Y+4, 5, 1))}, LeadDays: &[]int{180, 30}},
	}
	for _, in := range reminders {
		if _, err := e.CreateReminder(ctx, in, w.lang); err != nil {
			return err
		}
	}

	// A little past: something that went wrong last week and was fixed.
	if err := w.past(ctx, e, ids["deepseek"], channel.ID, channel.Name); err != nil {
		return err
	}

	// Now let the real engine look at all of it.
	for _, a := range accounts {
		if err := e.SampleAccount(ctx, ids[a.provider]); err != nil {
			return err
		}
	}
	if err := e.EvaluateAll(ctx); err != nil {
		return err
	}
	if err := e.EvaluateReminders(ctx); err != nil {
		return err
	}
	e.Dispatch(ctx)

	_, err = e.CreateToken(ctx, engine.TokenInput{Name: w.pick("coding-agent", "编程助手"), Scopes: []string{core.ScopeRead, core.ScopeAct}})
	if err != nil {
		return err
	}
	_, err = e.CreateToken(ctx, engine.TokenInput{Name: "claude-code-reporter", Scopes: []string{core.ScopeIngest}})
	return err
}

// past records a closed incident with its two messages, so the activity
// page has a story to show.
func (w *World) past(ctx context.Context, e *engine.Engine, accountID, channelID, channelName string) error {
	st := e.Store()
	opened := w.now().Add(-6*24*time.Hour - 3*time.Hour).UTC().Truncate(time.Second)
	resolved := opened.Add(47 * time.Minute)
	incident := core.Incident{
		ID: core.NewID("inc"), SubjectType: core.SubjectAccount, SubjectID: accountID, Kind: core.IncidentFailing,
		Severity: core.Warning, State: core.StateResolved, Title: "DeepSeek",
		Data:     map[string]any{"provider": "deepseek", "error": "HTTP 503 Service Unavailable", "failures": 2, "since": opened.Add(-30 * time.Minute)},
		OpenedAt: opened, UpdatedAt: resolved, ResolvedAt: &resolved, NotifiedAt: &opened, NotifyCount: 2,
	}
	if err := st.SaveIncident(ctx, incident); err != nil {
		return err
	}
	for event, at := range map[string]time.Time{core.EventOpened: opened, core.EventResolved: resolved} {
		title := w.pick("Cannot read · DeepSeek", "无法读取 · DeepSeek")
		if event == core.EventResolved {
			title = w.pick("Readable again · DeepSeek", "恢复读取 · DeepSeek")
		}
		message, _ := json.Marshal(map[string]any{"event": event, "severity": "warning", "topic": core.TopicSystem, "title": title, "at": at})
		sent := at.Add(time.Second)
		if err := st.Enqueue(ctx, &core.Delivery{
			IncidentID: incident.ID, Event: event, ChannelID: channelID, Channel: channelName, Message: message,
			Status: core.DeliverySent, Attempts: 1, NotBefore: at, CreatedAt: at, SentAt: &sent,
		}); err != nil {
			return err
		}
	}
	return nil
}

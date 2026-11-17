package main

import (
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"quotapulse/internal/bundle"
	"quotapulse/internal/core"
	"quotapulse/internal/notify"
	"quotapulse/internal/provider"
	"quotapulse/internal/recur"
)

// platform says how one of the old built-in platforms maps onto 1.0: which
// definition reads it now, and which of that definition's meters the old
// single number was.
type platform struct {
	id       string
	meterKey string
	kind     core.Kind
	unit     string
	limit    float64
	nameEN   string
	nameZH   string
	// pair names the two fields an "ID:Secret" key splits into.
	pair [2]string
}

var platforms = map[string]platform{
	"deepseek":   {id: "deepseek", meterKey: "balance-cny", kind: core.Balance, unit: "CNY", nameEN: "Balance", nameZH: "余额"},
	"glm":        {id: "glm-coding", meterKey: "tokens-5h", kind: core.Quota, unit: "%", limit: 100, nameEN: "5-hour window", nameZH: "5 小时窗口"},
	"openrouter": {id: "openrouter", meterKey: "credits", kind: core.Balance, unit: "USD", nameEN: "Credits", nameZH: "额度"},
	"tikhub":     {id: "tikhub", meterKey: "balance", kind: core.Balance, unit: "USD", nameEN: "Balance", nameZH: "余额"},
	"uniapi":     {id: "uniapi", meterKey: "credits", kind: core.Balance, unit: "USD", nameEN: "Credits", nameZH: "额度"},
	"wxrank":     {id: "wxrank", meterKey: "credits", kind: core.Balance, unit: "credits", nameEN: "Credits", nameZH: "积分"},
	"volc":       {id: "volcengine", meterKey: "balance", kind: core.Balance, unit: "CNY", nameEN: "Balance", nameZH: "余额", pair: [2]string{"access_key_id", "secret_access_key"}},
	"aliyun":     {id: "aliyun", meterKey: "balance", kind: core.Balance, unit: "CNY", nameEN: "Balance", nameZH: "余额", pair: [2]string{"access_key_id", "access_key_secret"}},
}

// platformOrder is the order the old version discovered platforms in.
var platformOrder = []string{"aliyun", "deepseek", "glm", "openrouter", "tikhub", "uniapi", "volc", "wxrank"}

const maxEnvAccounts = 10

// source is everything the old install knew.
type source struct {
	env  map[string]string
	db   *sql.DB
	lang string
	now  time.Time
	loc  *time.Location
}

// project is one monitored account of the old version.
type project struct {
	name, provider, group string
	apiKey                string
	threshold             *float64
	budget                *float64
	enabled               bool
	sealed                bool // the key is encrypted and could not be opened
}

type converter struct {
	source
	out   bundle.Bundle
	conf  core.Settings
	notes []string
	open  decrypter
	// custom maps an old custom provider's name to its new definition.
	custom map[string]provider.Definition
	// meterOf and accountOf resolve an old history ID to what it became.
	meterOf   map[string]string
	accountOf map[string]string
	reminders map[string]string
}

func (c *converter) note(format string, args ...any) {
	c.notes = append(c.notes, fmt.Sprintf(format, args...))
}

func (c *converter) pick(en, zh string) string {
	if c.lang == "zh" {
		return zh
	}
	return en
}

func (c *converter) get(key string) string { return strings.TrimSpace(c.env[key]) }

// convert turns the old install into a bundle.
func convert(src source) (bundle.Bundle, error) {
	c := &converter{
		source: src, open: newDecrypter(src.env["CONFIG_ENCRYPTION_KEY"]),
		custom: map[string]provider.Definition{}, meterOf: map[string]string{}, accountOf: map[string]string{},
		reminders: map[string]string{},
	}
	c.out = bundle.Bundle{Format: bundle.Format, Version: bundle.Version, ExportedAt: src.now.UTC().Truncate(time.Second), Source: "quotapulse 0.x (legacy import)"}
	c.settings()
	c.channel()
	c.tokens()
	for _, step := range []func() error{c.providers, c.projects, c.usage, c.subscriptions, c.alerts, c.browsers, c.leftBehind} {
		if err := step(); err != nil {
			return c.out, err
		}
	}
	c.out.Notes = c.notes
	return c.out, nil
}

// ---------- settings, channel, tokens: all from the environment ----------

var chineseZones = map[string]bool{
	"Asia/Shanghai": true, "Asia/Chongqing": true, "Asia/Harbin": true, "Asia/Urumqi": true, "PRC": true,
	"Asia/Hong_Kong": true, "Asia/Macau": true, "Asia/Taipei": true,
}

func (c *converter) settings() {
	s := core.DefaultSettings()
	if tz := c.get("TZ"); tz != "" {
		if _, err := time.LoadLocation(tz); err == nil {
			s.Timezone = tz
		} else {
			c.note("TZ=%s is not a known time zone; the time zone was left at UTC", tz)
		}
	} else {
		// The old image defaulted to Asia/Shanghai when TZ was not set.
		s.Timezone = "Asia/Shanghai"
	}
	c.loc, _ = time.LoadLocation(s.Timezone)
	if c.lang == "" {
		c.lang = "en"
		if chineseZones[s.Timezone] {
			c.lang = "zh"
		}
	}
	s.Language = c.lang

	// FX_RATES said "one unit of X is worth N of the base", and the base was CNY.
	s.BaseCurrency = "CNY"
	for _, pair := range strings.Split(c.get("FX_RATES"), ",") {
		code, rate, ok := strings.Cut(strings.TrimSpace(pair), "=")
		value, err := strconv.ParseFloat(strings.TrimSpace(rate), 64)
		if ok && err == nil && value > 0 && core.IsCurrency(strings.ToUpper(code)) {
			s.FX[strings.ToUpper(strings.TrimSpace(code))] = value
		}
	}
	if seconds, err := strconv.Atoi(c.get("BALANCE_REFRESH_INTERVAL_SECONDS")); err == nil {
		s.SampleSeconds = min(max(seconds, core.MinSampleSeconds), core.MaxSampleSeconds)
	}
	if days, err := strconv.ParseFloat(c.get("RUNWAY_ALERT_DAYS"), 64); err == nil && days >= 0 && days <= 365 {
		s.RunwayDays = days
	}
	if ratio, err := strconv.ParseFloat(c.get("SPEND_SPIKE_RATIO"), 64); err == nil && ratio == 0 {
		s.Anomaly = false
	}
	if days, err := strconv.Atoi(c.get("HISTORY_RETENTION_DAYS")); err == nil && days >= 30 && days <= 3650 {
		s.RetentionDays = days
	}
	// Alerts used to go out at fixed times of day; the first of them becomes
	// the time date reminders are sent.
	if schedule := c.get("ALERT_SCHEDULE"); schedule != "" && !strings.EqualFold(schedule, "off") {
		first, _, _ := strings.Cut(schedule, ",")
		if _, err := core.ParseClock(first); err == nil {
			s.ReminderTime = strings.TrimSpace(first)
		}
	}
	switch report := c.get("WEEKLY_REPORT_SCHEDULE"); {
	case strings.EqualFold(report, "off"):
		s.Digest.Enabled = false
	case report != "":
		day, clock, _ := strings.Cut(report, " ")
		if weekday, ok := weekdays[strings.ToLower(strings.TrimSpace(day))]; ok {
			s.Digest.Weekday = weekday
		}
		if _, err := core.ParseClock(clock); err == nil {
			s.Digest.Time = strings.TrimSpace(clock)
		}
	}
	c.conf = s
	c.out.Settings = bundle.SettingsOf(s)
}

var weekdays = map[string]int{
	"mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6, "sun": 7,
	"monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4, "friday": 5, "saturday": 6, "sunday": 7,
	"周一": 1, "周二": 2, "周三": 3, "周四": 4, "周五": 5, "周六": 6, "周日": 7, "周天": 7,
	"1": 1, "2": 2, "3": 3, "4": 4, "5": 5, "6": 6, "7": 7, "0": 7,
}

func (c *converter) channel() {
	hook := c.get("WEBHOOK_URL")
	if hook == "" {
		c.note("WEBHOOK_URL was not set, so there is no notification channel yet; add one in the dashboard")
		return
	}
	kind := strings.ToLower(c.get("WEBHOOK_TYPE"))
	names := map[string]string{"feishu": c.pick("Feishu", "飞书"), "dingtalk": c.pick("DingTalk", "钉钉"), "wecom": c.pick("WeCom", "企业微信"), "webhook": "Webhook"}
	if kind == "" || kind == "custom" {
		kind = "webhook"
		c.note("the custom webhook now receives a different JSON body (see docs/API.md, \"Webhook payload\"); whatever reads it needs updating")
	}
	if _, known := names[kind]; !known {
		c.note("WEBHOOK_TYPE=%s is not a channel type; the webhook was not migrated", kind)
		return
	}
	c.out.Channels = append(c.out.Channels, bundle.Channel{
		Name: names[kind], Type: kind, Config: map[string]string{"url": hook},
		Topics: append([]string(nil), core.Topics...), MinSeverity: core.Info, Enabled: true,
	})
}

// tokens carries the MCP keys over as they are, so agents that already hold
// one keep working. The old scopes were all ways of reading.
func (c *converter) tokens() {
	for _, entry := range strings.Split(c.get("MCP_API_KEYS"), ";") {
		parts := strings.Split(strings.TrimSpace(entry), ":")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			continue
		}
		token := bundle.Token{Name: parts[0], Scopes: []string{core.ScopeRead}, Token: parts[1]}
		// name:key:scopes:expiry — and an RFC 3339 expiry has colons of its own.
		if len(parts) > 3 {
			expiry := strings.Join(parts[3:], ":")
			if at, err := time.Parse(time.RFC3339, expiry); err == nil {
				token.ExpiresAt = &at
			} else if day, err := time.Parse("2006-01-02", expiry); err == nil {
				at := day.Add(24 * time.Hour) // a date was valid through its last day, UTC
				token.ExpiresAt = &at
			}
		}
		if token.ExpiresAt != nil && !token.ExpiresAt.After(c.now) {
			c.note("MCP key %q had expired and was not migrated", token.Name)
			continue
		}
		c.out.Tokens = append(c.out.Tokens, token)
	}
	if len(c.out.Tokens) > 0 {
		c.note("%d MCP key(s) keep working as read-only API tokens; the MCP tools themselves have new names (start with \"attention\")", len(c.out.Tokens))
	}
	if c.get("WEB_API_KEY") != "" {
		c.note("WEB_API_KEY is replaced by QUOTAPULSE_PASSWORD: set it to the same value to keep signing in with what you know")
	}
}

// ---------- accounts and their history ----------

func (c *converter) projects() error {
	var all []project
	declared := map[string]bool{}

	if c.db != nil {
		rows, err := table(c.db, "project_config", "id")
		if err != nil {
			return err
		}
		ordinal := map[string]int{}
		for _, r := range rows {
			p := project{
				name: r.str("name"), provider: strings.TrimPrefix(strings.ToLower(r.str("provider")), "custom:"),
				group: r.str("owner_project"), enabled: r.truthy("enabled"),
			}
			if v, ok := r.num("threshold"); ok && v > 0 {
				p.threshold = &v
			}
			if v, ok := r.num("monthly_budget"); ok && v > 0 {
				p.budget = &v
			}
			declared[p.provider] = true
			ordinal[p.provider]++
			key, opened := c.open.open(r.str("api_key"))
			switch {
			case !opened:
				p.sealed = true
			case key != "":
				p.apiKey = key
			default:
				// An empty key meant "use the environment", in order of appearance.
				p.apiKey = c.envKey(strings.ToUpper(p.provider), ordinal[p.provider])
			}
			all = append(all, p)
		}
	}

	// Anything with {PROVIDER}_API_KEY set and not already declared was
	// watched too.
	known := append([]string(nil), platformOrder...)
	for name := range c.custom {
		known = append(known, name)
	}
	sort.Strings(known)
	claimed := map[string]bool{"WEB": true}
	for _, name := range known {
		prefix := strings.ToUpper(name)
		claimed[prefix] = true
		if declared[name] {
			continue
		}
		var found []int
		for ordinal := 1; ordinal <= maxEnvAccounts; ordinal++ {
			if c.envKey(prefix, ordinal) != "" {
				found = append(found, ordinal)
			}
		}
		for _, ordinal := range found {
			p := project{name: name, provider: name, apiKey: c.envKey(prefix, ordinal), enabled: true, group: c.envField(prefix, "OWNER_PROJECT", ordinal)}
			if len(found) > 1 {
				p.name = fmt.Sprintf("%s-%d", name, ordinal)
			}
			if v, err := strconv.ParseFloat(c.envField(prefix, "THRESHOLD", ordinal), 64); err == nil && v > 0 {
				p.threshold = &v
			}
			all = append(all, p)
		}
	}

	// A key for a platform the old version had no definition for was never
	// watched, silently. Say so now.
	var orphans []string
	for key := range c.env {
		prefix, isKey := strings.CutSuffix(key, "_API_KEY")
		if !isKey || c.get(key) == "" {
			continue
		}
		prefix = strings.TrimRight(strings.TrimRight(prefix, "0123456789"), "_")
		if !claimed[prefix] {
			orphans = append(orphans, key)
		}
	}
	sort.Strings(orphans)
	for _, key := range orphans {
		c.note("%s is set, but the old version had no platform by that name, so nothing was ever watching it; it was not migrated", key)
	}

	var history map[string][]record
	if c.db != nil {
		rows, err := table(c.db, "balance_history", "timestamp")
		if err != nil {
			return err
		}
		history = map[string][]record{}
		for _, r := range rows {
			history[r.str("project_id")] = append(history[r.str("project_id")], r)
		}
	}

	names := map[string]bool{}
	for _, p := range all {
		if names[strings.ToLower(p.name)] {
			c.note("account %q appears twice; only the first was migrated", p.name)
			continue
		}
		names[strings.ToLower(p.name)] = true
		c.project(p, history[oldProjectID(p.provider, p.name)])
	}
	return nil
}

// envKey reads {PREFIX}_API_KEY for an ordinal the way the old version did:
// the first account may be written with or without its number.
func (c *converter) envKey(prefix string, ordinal int) string {
	return c.envField(prefix, "API_KEY", ordinal)
}

func (c *converter) envField(prefix, suffix string, ordinal int) string {
	names := []string{fmt.Sprintf("%s_%d_%s", prefix, ordinal, suffix)}
	if ordinal <= 1 {
		names = []string{prefix + "_" + suffix, prefix + "_1_" + suffix}
	}
	for _, name := range names {
		if value := c.get(name); value != "" {
			return value
		}
	}
	return ""
}

func (c *converter) project(p project, history []record) {
	account := bundle.Account{Name: p.name, Group: p.group, Enabled: p.enabled}
	meter := bundle.Meter{Account: p.name}
	if p.threshold != nil {
		meter.Rules.Low = p.threshold
	}
	if p.budget != nil {
		meter.Rules.Budget = p.budget
	}

	switch known, builtin := platforms[p.provider]; {
	case builtin:
		account.Provider = known.id
		meter.Key, meter.Kind, meter.Unit = known.meterKey, known.kind, known.unit
		meter.Name = c.pick(known.nameEN, known.nameZH)
		switch {
		case p.sealed || p.apiKey == "":
		case known.pair[0] != "":
			id, secret, ok := strings.Cut(p.apiKey, ":")
			if ok && id != "" && secret != "" {
				account.Credential = map[string]string{known.pair[0]: id, known.pair[1]: secret}
			} else {
				c.note("account %q: its key is not in the ID:Secret form; enter the credential again", p.name)
			}
		default:
			account.Credential = map[string]string{"api_key": p.apiKey}
		}
		if known.limit > 0 && meter.Rules.Budget != nil {
			meter.Rules.Budget = nil // a budget on a percentage never meant anything
		}
		c.readings(&meter, history, known.limit)
	default:
		def, converted := c.custom[p.provider]
		if !converted {
			c.note("account %q used the platform %q, which no longer has a definition; it was not migrated", p.name, p.provider)
			return
		}
		account.Provider = def.ID
		meter.Key, meter.Name = "main", c.pick("Balance", "余额")
		meter.Kind, meter.Unit = customKind(def), customUnit(def)
		if !p.sealed && p.apiKey != "" {
			account.Credential = credentialFor(def, p.apiKey)
		}
		limit := 0.0
		if meter.Kind == core.Quota {
			limit = 100
		}
		c.readings(&meter, history, limit)
	}

	switch {
	case p.sealed:
		c.note("account %q: its key is encrypted and CONFIG_ENCRYPTION_KEY did not open it; enter the credential again", p.name)
	case p.apiKey == "":
		c.note("account %q: no key was found for it; enter the credential", p.name)
	}
	c.out.Accounts = append(c.out.Accounts, account)
	c.out.Meters = append(c.out.Meters, meter)
	id := oldProjectID(p.provider, p.name)
	c.meterOf[id] = p.name + "/" + meter.Key
	c.accountOf[id] = p.name
}

func (c *converter) readings(meter *bundle.Meter, history []record, limit float64) {
	for _, r := range history {
		at, ok := r.when("timestamp")
		value, hasValue := r.num("balance")
		if !ok || !hasValue {
			continue
		}
		row := []float64{float64(at.Unix()), value}
		if limit > 0 {
			row = append(row, limit)
		}
		meter.Readings = append(meter.Readings, row)
	}
}

// usage turns the spend a local tool reported — one row per day and model —
// into a counter that starts over each month, which is what a reporter sends
// in 1.0.
func (c *converter) usage() error {
	if c.db == nil {
		return nil
	}
	rows, err := table(c.db, "usage_events", "entry_date")
	if err != nil || len(rows) == 0 {
		return err
	}
	type day struct {
		date string
		cost float64
	}
	perProject := map[string][]day{}
	currency := map[string]string{}
	var order []string
	for _, r := range rows {
		name := r.str("project_name")
		cost, _ := r.num("cost")
		if _, seen := perProject[name]; !seen {
			order = append(order, name)
		}
		if days := perProject[name]; len(days) > 0 && days[len(days)-1].date == r.str("entry_date") {
			perProject[name][len(days)-1].cost += cost
		} else {
			perProject[name] = append(perProject[name], day{r.str("entry_date"), cost})
		}
		if code := strings.ToUpper(r.str("currency")); code != "" {
			currency[name] = code
		}
	}
	for _, name := range order {
		meter := bundle.Meter{Key: slug(name), Name: name, Kind: core.Counter, Unit: currency[name]}
		// The budget for reported spend lived on a project of the same name.
		for i := range c.out.Meters {
			if existing := c.out.Meters[i]; strings.EqualFold(existing.Account, name) && existing.Rules.Budget != nil {
				meter.Rules.Budget = existing.Rules.Budget
			}
		}
		month, total := "", 0.0
		for _, d := range perProject[name] {
			date, err := recur.ParseDate(d.date)
			if err != nil {
				continue
			}
			if d.date[:7] != month {
				month, total = d.date[:7], 0
				meter.Readings = append(meter.Readings, []float64{float64(date.In(c.loc).Unix()), 0})
			}
			total += d.cost
			// The day's spend is known once the day is over.
			meter.Readings = append(meter.Readings, []float64{float64(date.AddDays(1).In(c.loc).Unix() - 1), total})
		}
		c.out.Meters = append(c.out.Meters, meter)
	}
	c.note("reported usage is now a pushed counter; point the reporter at POST /api/v1/ingest (see scripts/claude-code-usage.py)")
	return nil
}

var slugPattern = regexp.MustCompile(`[^a-z0-9._-]+`)

func slug(name string) string {
	s := strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(name), "-"), "-.")
	if s == "" {
		return "reported"
	}
	return s
}

// ---------- subscriptions become dates ----------

// holidays recognises reminders that were holidays rather than someone's day.
var holidays = []string{
	"元旦", "春节", "除夕", "元宵", "清明", "劳动节", "五一", "端午", "七夕", "中秋", "国庆", "重阳", "冬至", "腊八", "小年",
	"圣诞", "平安夜", "情人节", "母亲节", "父亲节", "儿童节", "妇女节", "教师节", "感恩节", "万圣", "新年",
	"new year", "christmas", "spring festival", "thanksgiving", "halloween", "easter", "valentine",
}

func (c *converter) subscriptions() error {
	if c.db == nil {
		return nil
	}
	rows, err := table(c.db, "subscription_config", "id")
	if err != nil {
		return err
	}
	today := recur.DateOf(c.now.In(c.loc))
	overrides := 0
	for _, r := range rows {
		title := r.str("name")
		amount, _ := r.num("amount")
		renewalDay, _ := r.num("renewal_day")
		day := int(renewalDay)
		lead, _ := r.num("alert_days_before")
		lastRenewed, hasLast := recur.Date{}, false
		if parsed, err := recur.ParseDate(r.str("last_renewed_date")); err == nil {
			lastRenewed, hasLast = parsed, true
		}

		var rule recur.Rule
		switch cycle := r.str("cycle_type"); cycle {
		case "weekly":
			rule = recur.Rule{Freq: recur.Weekly, Weekday: day}
		case "yearly", "lunar_yearly":
			rule = recur.Rule{Freq: recur.Yearly, Month: day / 100, Day: day % 100}
			if cycle == "lunar_yearly" {
				rule.Calendar = recur.LunarCal
			}
			// A yearly item with only a day renewed on the anniversary of
			// its last renewal.
			if day <= 31 && hasLast && cycle == "yearly" {
				rule.Month, rule.Day = lastRenewed.M, lastRenewed.D
			}
		default:
			rule = recur.Rule{Freq: recur.Monthly, Day: day}
		}
		if err := rule.Validate(); err != nil {
			c.note("reminder %q: its date could not be understood (%v); it was not migrated", title, err)
			continue
		}

		reminder := bundle.Reminder{
			Title: title, Rule: rule, Group: r.str("owner_project"), Enabled: r.truthy("enabled"),
			LeadDays: leadDays(int(lead)), Kind: core.ReminderCustom,
		}
		lower := strings.ToLower(title)
		switch {
		case amount > 0:
			reminder.Kind, reminder.Amount, reminder.Currency = core.ReminderRenewal, &amount, c.conf.BaseCurrency
		case containsAny(lower, holidays):
			reminder.Kind = core.ReminderHoliday
		case rule.Freq == recur.Yearly:
			// A yearly date with no price and a name that is not a
			// holiday: in practice, somebody's birthday.
			reminder.Kind = core.ReminderBirthday
		}
		// A renewal mark made inside the reminder window paid for the
		// occurrence that followed it.
		if hasLast {
			if next, ok := rule.Next(lastRenewed); ok && next.Sub(lastRenewed) <= int(lead) && !next.Before(today) {
				reminder.DoneThrough = &next
			}
		}
		if until, err := recur.ParseDate(r.str("snoozed_until")); err == nil && !until.Before(today) {
			reminder.SnoozedUntil = &until
		}
		if r.str("webhook_url") != "" {
			overrides++
		}
		c.out.Reminders = append(c.out.Reminders, reminder)
		c.reminders[oldSubscriptionID(title)] = title
	}
	if overrides > 0 {
		c.note("%d reminder(s) had a webhook of their own; in 1.0 a channel chooses its topics instead, so those now go to every channel that takes dates", overrides)
	}
	return nil
}

// leadDays turns "start reminding N days before, then daily" into the list
// of days 1.0 keeps.
func leadDays(n int) []int {
	if n <= 0 {
		return []int{0}
	}
	if n > 11 {
		return []int{n, 7, 3, 1, 0}
	}
	days := make([]int, 0, n+1)
	for d := n; d >= 0; d-- {
		days = append(days, d)
	}
	return days
}

func containsAny(text string, words []string) bool {
	for _, word := range words {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

// ---------- the alert log becomes incident history ----------

// alertKinds maps an old alert type to the incident it would be today.
var alertKinds = map[string]struct {
	kind, reason string
	severity     core.Severity
	subject      string
}{
	"low_balance":          {core.IncidentLevel, core.ReasonLow, core.Critical, core.SubjectMeter},
	"low_runway":           {core.IncidentLevel, core.ReasonRunway, core.Warning, core.SubjectMeter},
	"spend_spike":          {core.IncidentLevel, core.ReasonBurst, core.Warning, core.SubjectMeter},
	"budget":               {core.IncidentBudget, "projected", core.Warning, core.SubjectMeter},
	"check_failed":         {core.IncidentFailing, "", core.Warning, core.SubjectAccount},
	"subscription_renewal": {core.IncidentDue, "", core.Info, core.SubjectReminder},
}

func (c *converter) alerts() error {
	if c.db == nil {
		return nil
	}
	rows, err := table(c.db, "alert_history", "timestamp")
	if err != nil {
		return err
	}
	for _, r := range rows {
		mapping, known := alertKinds[r.str("alert_type")]
		at, ok := r.when("timestamp")
		if !known || !ok {
			continue
		}
		id := strings.TrimSuffix(r.str("project_id"), "-usage")
		subject := ""
		switch mapping.subject {
		case core.SubjectMeter:
			subject = c.meterOf[id]
		case core.SubjectAccount:
			subject = c.accountOf[id]
		case core.SubjectReminder:
			subject = c.reminders[id]
		}
		if subject == "" {
			continue // about something that was not migrated
		}
		// What the old version said at the time is kept verbatim; the
		// dashboard shows it as the incident's detail.
		data := map[string]any{"imported": r.str("message")}
		if value, ok := r.num("balance_value"); ok && mapping.subject == core.SubjectMeter {
			data["value"] = value
		}
		c.out.Incidents = append(c.out.Incidents, bundle.Incident{
			SubjectType: mapping.subject, Subject: subject, Kind: mapping.kind, Severity: mapping.severity,
			Reason: mapping.reason, Title: r.str("project_name"), Data: data, OpenedAt: at, ResolvedAt: at,
		})
	}
	return nil
}

// browsers carries over the devices that asked for browser notifications,
// together with the key pair their subscriptions are bound to. With both in
// place they go on receiving without anyone touching them.
func (c *converter) browsers() error {
	if c.db == nil {
		return nil
	}
	subscriptions, err := table(c.db, "push_subscriptions", "id")
	if err != nil || len(subscriptions) == 0 {
		return err
	}
	settings, err := table(c.db, "app_settings", "")
	if err != nil {
		return err
	}
	keys := notify.VAPID{}
	for _, r := range settings {
		switch r.str("setting_key") {
		case "vapid_public_key":
			keys.Public = r.str("setting_value")
		case "vapid_private_key":
			keys.Private = r.str("setting_value")
		}
	}
	if keys.Public == "" || keys.Private == "" {
		c.note("%d browser push subscription(s) were found without the keys they were made for; enable notifications again on each device", len(subscriptions))
		return nil
	}
	c.out.Push = &keys
	named := map[string]int{}
	for _, r := range subscriptions {
		if r.str("endpoint") == "" || r.str("p256dh") == "" || r.str("auth") == "" {
			continue
		}
		name := pushBrowser(r.str("endpoint"))
		if name == "" {
			name = c.pick("Browser", "浏览器")
		}
		if named[name]++; named[name] > 1 {
			name = fmt.Sprintf("%s %d", name, named[name])
		}
		c.out.Channels = append(c.out.Channels, bundle.Channel{
			Name: name, Type: notify.TypeWebPush,
			Config: map[string]string{"endpoint": r.str("endpoint"), "p256dh": r.str("p256dh"), "auth": r.str("auth")},
			Topics: append([]string(nil), core.Topics...), MinSeverity: core.Info, Enabled: true,
		})
	}
	c.note("%d browser(s) that had notifications turned on keep receiving them; each is now a channel of its own that you can rename, limit to some topics, or remove", len(subscriptions))
	return nil
}

// pushBrowser guesses which browser a subscription belongs to from the push
// service it goes through. The old table kept nothing else about the device,
// and "Safari" tells three devices apart better than "Browser 2" does.
func pushBrowser(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case strings.HasSuffix(host, "googleapis.com"):
		return "Chrome"
	case strings.HasSuffix(host, "push.apple.com"):
		return "Safari"
	case strings.HasSuffix(host, "mozilla.com"), strings.HasSuffix(host, "mozaws.net"):
		return "Firefox"
	case strings.HasSuffix(host, "notify.windows.com"):
		return "Edge"
	}
	return ""
}

// leftBehind says what 1.0 has no place for.
func (c *converter) leftBehind() error {
	if c.db == nil {
		c.note("no database was read: only what the environment declares was migrated")
		return nil
	}
	rows, err := table(c.db, "email_config", "")
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		c.note("%d mailbox(es): scanning mail for billing notices is gone from 1.0; anything can still report into it through POST /api/v1/ingest", len(rows))
	}
	return nil
}

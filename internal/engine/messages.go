package engine

import (
	"fmt"
	"strings"
	"time"

	"quotapulse/internal/core"
	"quotapulse/internal/i18n"
	"quotapulse/internal/notify"
)

// compose writes the message for an incident event. Everything it says comes
// from the incident's own data, so a message can be rebuilt at any time and
// reads the same in a test as in production.
func (e *Engine) compose(inc core.Incident, event string, s core.Settings) notify.Message {
	l := i18n.Of(s.Language)
	now := e.now().In(s.Location())
	msg := notify.Message{
		Event: event, Severity: inc.Severity, Topic: core.TopicOf(inc.Kind), At: e.now().UTC(), Path: pathOf(inc),
		Footer: "QuotaPulse" + i18n.Sep + l.ShortDate(dateOf(now)) + " " + now.Format("15:04"),
		Subject: &notify.Subject{
			IncidentID: inc.ID, Kind: inc.Kind, Reason: inc.Reason,
			Type: inc.SubjectType, ID: inc.SubjectID, Name: inc.Title,
		},
	}
	if s.PublicURL != "" {
		msg.Link = strings.TrimRight(s.PublicURL, "/") + pathOf(inc)
		msg.LinkLabel = l.Pick("Open QuotaPulse", "打开 QuotaPulse")
	}

	w := writer{l: l, now: now, name: inc.Title}
	if event == core.EventResolved {
		w.resolved(&msg, inc)
		return msg
	}
	switch inc.Kind {
	case core.IncidentLevel:
		var d LevelData
		fromMap(inc.Data, &d)
		if d.MeterKind == core.Quota {
			w.quota(&msg, inc.Reason, d)
		} else {
			w.balance(&msg, inc.Reason, d)
		}
	case core.IncidentBudget:
		var d BudgetData
		fromMap(inc.Data, &d)
		w.budget(&msg, inc.Reason, d)
	case core.IncidentStale:
		var d StaleData
		fromMap(inc.Data, &d)
		w.stale(&msg, d)
	case core.IncidentFailing:
		var d FailingData
		fromMap(inc.Data, &d)
		w.failing(&msg, d)
	case core.IncidentDelivery:
		var d DeliveryData
		fromMap(inc.Data, &d)
		w.delivery(&msg, d)
	case core.IncidentDue:
		var d DueData
		fromMap(inc.Data, &d)
		w.due(&msg, d)
	}
	if event == core.EventReminder && inc.Kind != core.IncidentDue {
		msg.Title = l.Pick("Still open: ", "仍未解决：") + msg.Title
	}
	return msg
}

// pathOf is where on the dashboard an incident is best looked at.
func pathOf(inc core.Incident) string {
	switch inc.SubjectType {
	case core.SubjectMeter:
		return "/meters/" + inc.SubjectID
	case core.SubjectAccount:
		// The meters page lists accounts too; the fragment finds the row.
		return "/meters#" + inc.SubjectID
	case core.SubjectReminder:
		return "/dates"
	case core.SubjectChannel:
		return "/settings/channels"
	}
	return "/"
}

// writer holds what every sentence needs: the language, the time, the name.
type writer struct {
	l    i18n.Lang
	now  time.Time
	name string
}

func (w writer) title(en, zh string) string { return w.l.Pick(en, zh) + i18n.Sep + w.name }

func (w writer) fact(en, zh, value string) notify.Fact {
	return notify.Fact{Label: w.l.Pick(en, zh), Value: value}
}

// clock writes a moment in the user's zone.
func (w writer) clock(t time.Time) string { return w.l.Clock(t.In(w.now.Location()), w.now) }

func (w writer) balance(msg *notify.Message, reason string, d LevelData) {
	l := w.l
	value := i18n.Amount(d.Value, d.Unit)
	runway := ""
	if d.RunwayDays != nil {
		runway = l.Days(*d.RunwayDays)
	}

	switch reason {
	case core.ReasonEmpty:
		msg.Title = w.title("Balance is empty", "余额已用尽")
		msg.Body = l.Pick("The balance is "+value+".", "余额为 "+value+"。")
	case core.ReasonLow:
		msg.Title = w.title("Low balance", "余额不足")
		low := i18n.Amount(deref(d.Low), d.Unit)
		msg.Body = l.Pick(
			fmt.Sprintf("The balance is %s, at or below the threshold of %s.", value, low),
			fmt.Sprintf("余额 %s，已低于提醒线 %s。", value, low))
		if runway != "" {
			msg.Body += l.Pick(" At the recent pace it lasts about "+runway+".", "按近期的消耗还可用约 "+runway+"。")
		}
	case core.ReasonRunway:
		msg.Title = w.title("Balance runs out in about "+runway, "余额约 "+runway+"后用尽")
		burn := i18n.Amount(d.Burn, d.Unit)
		msg.Body = l.Pick(
			fmt.Sprintf("The balance is %s and about %s goes a day.", value, burn),
			fmt.Sprintf("余额 %s，近期日均消耗 %s。", value, burn))
		if d.DepletesAt != nil {
			date := l.ShortDate(dateOf(d.DepletesAt.In(w.now.Location())))
			msg.Body += l.Pick(" At that pace it runs out around "+date+".", "照这个速度预计在 "+date+" 用尽。")
		}
	case core.ReasonBurst:
		msg.Title = w.title("Spending burst", "消耗突增")
		if b := d.Burst; b != nil {
			window := l.Span(time.Duration(b.WindowHours * float64(time.Hour)))
			left := l.Span(time.Duration(b.RunwayDays * 24 * float64(time.Hour)))
			msg.Body = l.Pick(
				fmt.Sprintf("%s went in the last %s. At that rate the balance of %s lasts %s.", i18n.Amount(b.Spend, d.Unit), window, value, left),
				fmt.Sprintf("最近 %s花了 %s，照这个速度余额 %s 只够 %s。", window, i18n.Amount(b.Spend, d.Unit), value, left))
			switch {
			case b.Ratio == nil:
			case *b.Ratio >= 100:
				// Past a hundred the exact figure is trivia.
				msg.Body += l.Pick(" That is more than 100 times the usual pace.", "超过平时的 100 倍。")
			default:
				ratio := i18n.Number(*b.Ratio, 0, false)
				msg.Body += l.Pick(" That is "+ratio+" times the usual pace.", "是平时的 "+ratio+" 倍。")
			}
			if b.BaselinePerDay > 0 {
				usual := l.Days(d.Value / b.BaselinePerDay)
				msg.Body += l.Pick(" At the usual pace it would last "+usual+".", "按平时的速度可用 "+usual+"。")
			}
		}
	}

	msg.Facts = append(msg.Facts, w.fact("Balance", "余额", value))
	if d.Low != nil {
		msg.Facts = append(msg.Facts, w.fact("Threshold", "提醒线", i18n.Amount(*d.Low, d.Unit)))
	}
	if d.Burn > 0 {
		msg.Facts = append(msg.Facts, w.fact("Daily burn", "日均消耗", i18n.Amount(d.Burn, d.Unit)))
	}
	if runway != "" {
		msg.Facts = append(msg.Facts, w.fact("Runway", "预计可用", runway))
	}
}

func (w writer) quota(msg *notify.Message, reason string, d LevelData) {
	l := w.l
	value := i18n.Amount(d.Value, d.Unit)
	reset := ""
	if d.ResetsAt != nil && d.ResetsAt.After(w.now) {
		span := l.Span(d.ResetsAt.Sub(w.now))
		reset = l.Pick(
			fmt.Sprintf("It resets at %s, in %s.", w.clock(*d.ResetsAt), span),
			fmt.Sprintf("%s 重置（%s后）。", w.clock(*d.ResetsAt), span))
	}

	switch reason {
	case core.ReasonEmpty:
		msg.Title = w.title("Quota used up", "配额已用尽")
		msg.Body = orText(reset, l.Pick("There is no known reset time.", "没有已知的重置时间。"))
	case core.ReasonLow:
		msg.Title = w.title(value+" of quota left", "配额剩余 "+value)
		msg.Body = l.Pick(
			"That is at or below the threshold of "+i18n.Amount(deref(d.Low), d.Unit)+".",
			"已低于提醒线 "+i18n.Amount(deref(d.Low), d.Unit)+"。")
		if reset != "" {
			msg.Body += l.Pick(" ", "") + reset
		}
	case core.ReasonPace:
		msg.Title = w.title("Quota will run out early", "配额将提前用尽")
		if d.ExhaustsAt != nil && d.ResetsAt != nil {
			early := l.Span(d.ResetsAt.Sub(*d.ExhaustsAt))
			msg.Body = l.Pick(
				fmt.Sprintf("At this pace it runs out at %s, %s before it resets at %s.", w.clock(*d.ExhaustsAt), early, w.clock(*d.ResetsAt)),
				fmt.Sprintf("按当前的速度将在 %s 用尽，比 %s 的重置早 %s。", w.clock(*d.ExhaustsAt), w.clock(*d.ResetsAt), early))
		}
	}

	left := value
	if d.Limit != nil && d.Unit != "%" {
		left += " / " + i18n.Amount(*d.Limit, d.Unit)
	}
	msg.Facts = append(msg.Facts, w.fact("Remaining", "剩余", left))
	if d.ResetsAt != nil {
		msg.Facts = append(msg.Facts, w.fact("Resets", "重置时间", w.clock(*d.ResetsAt)))
	}
}

func (w writer) budget(msg *notify.Message, reason string, d BudgetData) {
	l := w.l
	spend, projected, budget := i18n.Amount(d.Spend, d.Unit), i18n.Amount(d.Projected, d.Unit), i18n.Amount(d.Budget, d.Unit)
	if reason == ReasonExceeded {
		msg.Title = w.title("Over budget", "预算已超支")
		msg.Body = l.Pick(
			fmt.Sprintf("%s spent this month, past the budget of %s.", spend, budget),
			fmt.Sprintf("本月已花 %s，超出预算 %s。", spend, budget))
	} else {
		msg.Title = w.title("On course to pass the budget", "预算预计超支")
		msg.Body = l.Pick(
			fmt.Sprintf("%s spent this month and heading for %s, against a budget of %s.", spend, projected, budget),
			fmt.Sprintf("本月已花 %s，预计月底达到 %s，超出预算 %s。", spend, projected, budget))
	}
	msg.Facts = []notify.Fact{
		w.fact("Spent this month", "本月已花", spend),
		w.fact("Projected", "预计月底", projected),
		w.fact("Budget", "预算", budget),
	}
}

func (w writer) stale(msg *notify.Message, d StaleData) {
	msg.Title = w.title("No data", "数据中断")
	span := w.l.Span(w.now.Sub(d.LastAt))
	msg.Body = w.l.Pick(
		fmt.Sprintf("No reading for %s; the last one came at %s.", span, w.clock(d.LastAt)),
		fmt.Sprintf("已有 %s没有收到新的读数，上一次是 %s。", span, w.clock(d.LastAt)))
}

func (w writer) failing(msg *notify.Message, d FailingData) {
	l := w.l
	msg.Title = w.title("Cannot read", "无法读取")
	if d.Failures > 1 {
		msg.Body = l.Pick(
			fmt.Sprintf("%d checks in a row failed: %s.", d.Failures, d.Error),
			fmt.Sprintf("连续 %d 次读取失败：%s。", d.Failures, d.Error))
	} else {
		msg.Body = l.Pick("The check failed: "+d.Error+".", "读取失败："+d.Error+"。")
	}
	msg.Body += l.Pick(" Its meters will not update until this is fixed.", "恢复之前，这个账户的额度不会更新。")
	last := l.Pick("never", "从未成功")
	if d.Since != nil {
		last = w.clock(*d.Since)
	}
	msg.Facts = []notify.Fact{w.fact("Last success", "上次成功", last)}
}

func (w writer) delivery(msg *notify.Message, d DeliveryData) {
	msg.Title = w.title("Channel keeps failing", "通知渠道连续失败")
	msg.Body = w.l.Pick(
		fmt.Sprintf("The last %d notifications could not be delivered: %s.", d.Failures, d.Error),
		fmt.Sprintf("最近 %d 条通知未能送达：%s。", d.Failures, d.Error))
}

// due writes a date reminder: what it is, when, and what day that is.
func (w writer) due(msg *notify.Message, d DueData) {
	l := w.l
	parts := []string{w.name}
	icon := "📌"
	switch d.ReminderKind {
	case core.ReminderBirthday:
		icon = "🎂"
		label := l.Pick("birthday", "生日")
		if d.Ordinal > 0 {
			label = l.Pick(l.Ordinal(d.Ordinal)+" birthday", fmt.Sprintf("%d 岁生日", d.Ordinal))
		}
		parts = append(parts, label)
	case core.ReminderAnniversary:
		icon = "💝"
		label := l.Pick("anniversary", "纪念日")
		if d.Ordinal > 0 {
			label = l.Pick(l.Ordinal(d.Ordinal)+" anniversary", fmt.Sprintf("%d 周年", d.Ordinal))
		}
		parts = append(parts, label)
	case core.ReminderRenewal:
		icon = "💳"
		parts = append(parts, l.Pick("renews", "续费"))
	case core.ReminderExpiry:
		icon = "⏳"
		parts = append(parts, l.Pick("expires", "到期"))
	case core.ReminderHoliday:
		icon = "🎉"
	}
	parts = append(parts, l.Until(d.Days))
	msg.Title = icon + " " + strings.Join(parts, i18n.Sep)

	msg.Body = l.Date(d.Date, w.now.Year())
	if d.Lunar != "" {
		msg.Body += i18n.Sep + l.Pick("lunar ", "农历") + d.Lunar
	}
	if d.Amount != nil && *d.Amount > 0 {
		msg.Facts = append(msg.Facts, w.fact("Amount", "金额", i18n.Amount(*d.Amount, d.Currency)))
	}
	if d.Notes != "" {
		msg.Facts = append(msg.Facts, w.fact("Notes", "备注", d.Notes))
	}
}

// resolved writes the good news, with the numbers as they are now.
func (w writer) resolved(msg *notify.Message, inc core.Incident) {
	l := w.l
	switch inc.Kind {
	case core.IncidentLevel:
		var d LevelData
		fromMap(inc.Data, &d)
		value := i18n.Amount(d.Value, d.Unit)
		if d.MeterKind == core.Quota {
			msg.Title = w.title("Quota is back", "配额已恢复")
			msg.Body = l.Pick(value+" left.", "剩余 "+value+"。")
			return
		}
		msg.Title = w.title("Recovered", "已恢复")
		msg.Body = l.Pick("The balance is now "+value+".", "余额现在是 "+value+"。")
		if d.RunwayDays != nil {
			msg.Body += l.Pick(" At the recent pace it lasts about "+l.Days(*d.RunwayDays)+".", "按近期的消耗可用约 "+l.Days(*d.RunwayDays)+"。")
		}
	case core.IncidentBudget:
		msg.Title = w.title("Back within budget", "预算回到正常")
	case core.IncidentStale:
		msg.Title = w.title("Data is flowing again", "数据已恢复")
	case core.IncidentFailing:
		msg.Title = w.title("Readable again", "恢复读取")
		msg.Body = l.Pick("It was unreadable for "+l.Span(w.now.Sub(inc.OpenedAt))+".", "中断了 "+l.Span(w.now.Sub(inc.OpenedAt))+"。")
	case core.IncidentDelivery:
		msg.Title = w.title("Channel is delivering again", "通知渠道已恢复")
	default:
		msg.Title = w.title("Resolved", "已解决")
	}
}

// importedKey marks an incident brought in from another system, with the
// text that system wrote for it.
const importedKey = "imported"

// importedHeadline names an imported incident by its kind alone.
func importedHeadline(inc core.Incident, language string) string {
	l := i18n.Of(language)
	title := l.Pick("Incident", "事件")
	switch {
	case inc.Kind == core.IncidentLevel && inc.Reason == core.ReasonRunway:
		title = l.Pick("Balance was running out", "余额即将用尽")
	case inc.Kind == core.IncidentLevel && inc.Reason == core.ReasonBurst:
		title = l.Pick("Spending burst", "消耗突增")
	case inc.Kind == core.IncidentLevel && inc.Reason == core.ReasonEmpty:
		title = l.Pick("Balance was empty", "余额已用尽")
	case inc.Kind == core.IncidentLevel:
		title = l.Pick("Low balance", "余额不足")
	case inc.Kind == core.IncidentBudget:
		title = l.Pick("Budget pace", "预算预计超支")
	case inc.Kind == core.IncidentFailing:
		title = l.Pick("Could not read", "无法读取")
	case inc.Kind == core.IncidentStale:
		title = l.Pick("No data", "数据中断")
	case inc.Kind == core.IncidentDue:
		title = l.Pick("Date reminder", "日期提醒")
	case inc.Kind == core.IncidentDelivery:
		title = l.Pick("Channel kept failing", "通知渠道连续失败")
	}
	return title + i18n.Sep + inc.Title
}

// testMessage is what the "send a test" button delivers.
func (e *Engine) testMessage(s core.Settings, channel string) notify.Message {
	l := i18n.Of(s.Language)
	now := e.now().In(s.Location())
	msg := notify.Message{
		Event: core.EventTest, Severity: core.Info, Topic: core.TopicSystem, At: e.now().UTC(), Path: "/settings/channels",
		Title:  l.Pick("QuotaPulse test message", "QuotaPulse 测试消息"),
		Body:   l.Pick("If you can read this, the channel “"+channel+"” works.", "看到这条消息，说明渠道「"+channel+"」工作正常。"),
		Footer: "QuotaPulse" + i18n.Sep + l.ShortDate(dateOf(now)) + " " + now.Format("15:04"),
	}
	if s.PublicURL != "" {
		msg.Link = strings.TrimRight(s.PublicURL, "/") + "/"
		msg.LinkLabel = l.Pick("Open QuotaPulse", "打开 QuotaPulse")
	}
	return msg
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func orText(text, fallback string) string {
	if text == "" {
		return fallback
	}
	return text
}

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"quotapulse/internal/core"
	"quotapulse/internal/i18n"
	"quotapulse/internal/notify"
	"quotapulse/internal/recur"
	"quotapulse/internal/store"
)

// How long to wait before each retry of a delivery. After the last one the
// delivery is marked failed and shown as such; a message that could not be
// delivered in three hours is no longer a timely warning anyway.
var deliveryBackoff = []time.Duration{
	30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, 2 * time.Hour,
}

const (
	sendTimeout = 30 * time.Second
	// A channel that failed this many deliveries in a row gets an incident
	// of its own.
	channelFailureStreak = 3
)

// Dispatch sends what is due in the outbox and returns how many deliveries
// it handled.
func (e *Engine) Dispatch(ctx context.Context) int {
	due, err := e.store.DueDeliveries(ctx, e.now(), 50)
	if err != nil {
		e.log.Error("could not read the outbox", "error", err)
		return 0
	}
	touched := map[string]bool{}
	for _, d := range due {
		if ctx.Err() != nil {
			break
		}
		e.deliver(ctx, d)
		touched[d.ChannelID] = true
	}
	if len(due) > 0 {
		e.hub.Publish(ChangedActivity)
	}
	for channelID := range touched {
		if err := e.judgeChannel(ctx, channelID); err != nil {
			e.log.Error("could not judge a channel", "error", err)
		}
	}
	return len(due)
}

// deliver makes one attempt at one delivery and records how it went.
func (e *Engine) deliver(ctx context.Context, d core.Delivery) {
	now := e.now().UTC().Truncate(time.Second)
	channel, err := e.store.Channel(ctx, d.ChannelID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !channel.Enabled) {
		d.Status, d.LastError = core.DeliveryDropped, "the channel is gone or disabled"
		_ = e.store.SaveDelivery(ctx, d)
		return
	}
	if err != nil {
		e.log.Error("could not load a channel", "error", err)
		return
	}
	var msg notify.Message
	if err := json.Unmarshal(d.Message, &msg); err != nil {
		d.Status, d.LastError = core.DeliveryFailed, "the stored message cannot be read"
		_ = e.store.SaveDelivery(ctx, d)
		return
	}

	err = e.send(ctx, channel, msg)
	if ctx.Err() != nil {
		return // shutting down mid-send: leave it pending and try again on the next start
	}

	d.Attempts++
	switch {
	case err == nil:
		d.Status, d.LastError, d.SentAt = core.DeliverySent, "", &now
	case notify.IsGone(err) && channel.Type == notify.TypeWebPush:
		// The browser is no longer listening. That is not a failing channel
		// to raise an incident about; it is a channel that has ended.
		d.Status, d.LastError = core.DeliveryDropped, err.Error()
		_ = e.store.SaveDelivery(ctx, d)
		e.forgetBrowser(ctx, channel)
		return
	case notify.IsPermanent(err) || d.Attempts > len(deliveryBackoff):
		d.Status, d.LastError = core.DeliveryFailed, err.Error()
		e.log.Warn("notification failed", "channel", channel.Name, "attempts", d.Attempts, "error", err)
	default:
		d.LastError = err.Error()
		d.NotBefore = now.Add(deliveryBackoff[d.Attempts-1])
		e.log.Warn("notification will be retried", "channel", channel.Name, "attempt", d.Attempts, "error", err)
	}
	if err := e.store.SaveDelivery(ctx, d); err != nil {
		e.log.Error("could not record a delivery", "error", err)
	}
}

// send delivers one message through one channel.
func (e *Engine) send(ctx context.Context, channel core.Channel, msg notify.Message) error {
	if channel.Type == notify.TypeWebPush {
		if _, err := e.pushKeys(ctx); err != nil {
			return err
		}
	}
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	return e.sender.Send(sendCtx, channel.Type, channel.Config, msg)
}

// judgeChannel opens an incident for a channel that keeps failing and
// resolves it when a delivery gets through again. The news travels over the
// other channels.
func (e *Engine) judgeChannel(ctx context.Context, channelID string) error {
	channel, err := e.store.Channel(ctx, channelID)
	if errors.Is(err, store.ErrNotFound) {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.closeSubject(ctx, core.SubjectChannel, channelID)
	}
	if err != nil {
		return err
	}
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return err
	}
	health, err := e.store.ChannelsHealth(ctx)
	if err != nil {
		return err
	}
	var conds []condition
	if h := health[channelID]; h.FailedInARow >= channelFailureStreak {
		conds = append(conds, condition{
			Kind: core.IncidentDelivery, Severity: core.Warning,
			Data: toMap(DeliveryData{ChannelType: channel.Type, Error: h.LastError, Failures: h.FailedInARow}),
		})
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	sub := subject{Type: core.SubjectChannel, ID: channel.ID, Title: channel.Name, SkipChannel: channel.ID}
	changed, err := e.reconcile(ctx, sub, []string{core.IncidentDelivery}, conds, nil, settings)
	if changed {
		e.hub.Publish(ChangedIncidents)
	}
	return err
}

// TestChannel sends a test message through a channel right now and reports
// the platform's answer. It bypasses the outbox on purpose: the person who
// pressed the button is waiting for the result.
func (e *Engine) TestChannel(ctx context.Context, id string) error {
	channel, err := e.store.Channel(ctx, id)
	if err != nil {
		return err
	}
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return err
	}
	msg := e.testMessage(settings, channel.Name)
	raw, _ := json.Marshal(msg)
	now := e.now().UTC().Truncate(time.Second)

	sendErr := e.send(ctx, channel, msg)

	d := core.Delivery{
		Event: core.EventTest, ChannelID: channel.ID, Channel: channel.Name, Message: raw,
		Status: core.DeliverySent, Attempts: 1, NotBefore: now, CreatedAt: now, SentAt: &now,
	}
	if sendErr != nil {
		d.Status, d.LastError, d.SentAt = core.DeliveryFailed, sendErr.Error(), nil
	}
	if err := e.store.Enqueue(ctx, &d); err != nil {
		return err
	}
	e.hub.Publish(ChangedActivity)
	if err := e.judgeChannel(ctx, channel.ID); err != nil {
		e.log.Error("could not judge a channel", "error", err)
	}
	if notify.IsGone(sendErr) && channel.Type == notify.TypeWebPush {
		e.forgetBrowser(ctx, channel)
	}
	if sendErr != nil {
		return invalid("%s", sendErr.Error())
	}
	return nil
}

// RetryDelivery puts a failed delivery back in the outbox.
func (e *Engine) RetryDelivery(ctx context.Context, id int64) (core.Delivery, error) {
	d, err := e.store.Delivery(ctx, id)
	if err != nil {
		return d, err
	}
	if d.Status != core.DeliveryFailed {
		return d, invalid("only a failed delivery can be retried")
	}
	d.Status, d.Attempts, d.LastError, d.NotBefore = core.DeliveryPending, 0, "", e.now()
	if err := e.store.SaveDelivery(ctx, d); err != nil {
		return d, err
	}
	e.hub.Publish(ChangedActivity)
	e.nudge()
	return d, nil
}

// ---------- digest ----------

const (
	// digestStateKey holds the day the last scheduled digest went out.
	digestStateKey = "digest.last"
	// digestScheduleKey holds the schedule in force and when it was first seen.
	digestScheduleKey = "digest.schedule"
)

// MaybeDigest sends the periodic summary if its time has come and it has not
// gone out yet today. Returning true means it was sent.
func (e *Engine) MaybeDigest(ctx context.Context) (bool, error) {
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return false, err
	}
	now := e.now().In(settings.Location())
	since, err := e.digestSince(ctx, settings, now)
	if err != nil || !settings.Digest.Enabled {
		return false, err
	}
	today := dateOf(now)
	clock, err := core.ParseClock(settings.Digest.Time)
	if err != nil {
		return false, nil
	}
	// Late is fine — the server may have been down at nine — but only for a
	// digest that was already due to be sent when its hour came.
	if slot := clock.On(now); now.Before(slot) || slot.Before(since) {
		return false, nil
	}
	if settings.Digest.Weekday != 0 && settings.Digest.Weekday != today.Weekday() {
		return false, nil
	}
	if last, _, err := e.store.State(ctx, digestStateKey); err != nil || last == today.String() {
		return false, err
	}
	// Write the marker first. If queuing then fails, one digest is lost;
	// the other order could send the same digest every minute.
	if err := e.store.SetState(ctx, digestStateKey, today.String()); err != nil {
		return false, err
	}
	return true, e.SendDigest(ctx)
}

// digestSince returns the moment the present digest schedule came into
// force: the first time the engine saw it. Only a time after that is one the
// digest can be late for. Without this, a database created on a Monday
// afternoon — a new install, a migration — would send "this morning's"
// weekly digest the minute it started, and so would switching the digest on
// at two for nine o'clock.
func (e *Engine) digestSince(ctx context.Context, s core.Settings, now time.Time) (time.Time, error) {
	schedule := "off"
	if s.Digest.Enabled {
		schedule = fmt.Sprintf("%d %s %s", s.Digest.Weekday, s.Digest.Time, s.Timezone)
	}
	stored, _, err := e.store.State(ctx, digestScheduleKey)
	if err != nil {
		return time.Time{}, err
	}
	if at, rest, ok := strings.Cut(stored, " "); ok && rest == schedule {
		if since, err := time.Parse(time.RFC3339, at); err == nil {
			return since, nil
		}
	}
	since := now.UTC().Truncate(time.Second)
	return since, e.store.SetState(ctx, digestScheduleKey, since.Format(time.RFC3339)+" "+schedule)
}

// SendDigest queues the summary for every channel that takes digests.
func (e *Engine) SendDigest(ctx context.Context) error {
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return err
	}
	msg, err := e.composeDigest(ctx, settings)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	channels, err := e.store.Channels(ctx)
	if err != nil {
		return err
	}
	now := e.now().UTC().Truncate(time.Second)
	for _, ch := range channels {
		if !ch.Wants(core.TopicDigest, core.Info) {
			continue
		}
		d := core.Delivery{
			Event: core.EventDigest, ChannelID: ch.ID, Channel: ch.Name, Message: raw,
			Status: core.DeliveryPending, NotBefore: now, CreatedAt: now,
		}
		if e.mute {
			d.Status, d.LastError = core.DeliveryDropped, mutedReason
		}
		if err := e.store.Enqueue(ctx, &d); err != nil {
			return err
		}
	}
	e.hub.Publish(ChangedActivity)
	e.nudge()
	return nil
}

// composeDigest writes the summary: what is open, where every meter stands,
// what was spent, and which dates are coming.
func (e *Engine) composeDigest(ctx context.Context, s core.Settings) (notify.Message, error) {
	l := i18n.Of(s.Language)
	now := e.now().In(s.Location())
	today := dateOf(now)
	overview, err := e.Overview(ctx)
	if err != nil {
		return notify.Message{}, err
	}

	title := l.Pick("QuotaPulse daily", "QuotaPulse 日报") + i18n.Sep + l.ShortDate(today)
	if s.Digest.Weekday != 0 {
		title = l.Pick("QuotaPulse weekly", "QuotaPulse 周报") + i18n.Sep +
			l.ShortDate(today.AddDays(-6)) + " – " + l.ShortDate(today)
	}
	msg := notify.Message{
		Event: core.EventDigest, Severity: core.Info, Topic: core.TopicDigest, Title: title, At: e.now().UTC(),
		Footer: "QuotaPulse" + i18n.Sep + l.ShortDate(today) + " " + now.Format("15:04"),
	}
	if s.PublicURL != "" {
		msg.Link = strings.TrimRight(s.PublicURL, "/") + "/"
		msg.LinkLabel = l.Pick("Open QuotaPulse", "打开 QuotaPulse")
	}

	attention := notify.Section{Title: l.Pick("Needs attention", "需要关注")}
	for _, item := range overview.Attention {
		if len(attention.Lines) == 8 {
			attention.Lines = append(attention.Lines, l.Pick(
				fmt.Sprintf("… and %d more", len(overview.Attention)-8), fmt.Sprintf("… 还有 %d 项", len(overview.Attention)-8)))
			break
		}
		attention.Lines = append(attention.Lines, "• "+item.Headline)
	}
	if len(attention.Lines) == 0 {
		attention.Lines = []string{l.Pick("Nothing. All clear.", "没有，一切正常。")}
	}
	msg.Sections = append(msg.Sections, attention)

	if len(overview.Meters) > 0 {
		section := notify.Section{Title: l.Pick("Meters", "额度")}
		for _, m := range overview.Meters {
			if m.Hidden {
				continue
			}
			line := "**" + m.Display + "** "
			switch {
			case m.Value == nil:
				line += l.Pick("no data yet", "还没有数据")
			case m.Kind == core.Quota:
				line += i18n.Amount(*m.Value, m.Unit)
				if m.Limit != nil && m.Unit != "%" {
					line += " / " + i18n.Number(*m.Limit, 2, false)
				}
				if m.ResetsAt != nil && m.ResetsAt.After(now) {
					line += i18n.Sep + l.Pick("resets in "+l.Span(m.ResetsAt.Sub(now)), l.Span(m.ResetsAt.Sub(now))+"后重置")
				}
			default:
				line += i18n.Amount(*m.Value, m.Unit)
				if m.Forecast != nil && m.Forecast.RunwayDays != nil {
					line += i18n.Sep + l.Pick("about "+l.Days(*m.Forecast.RunwayDays)+" left", "约可用 "+l.Days(*m.Forecast.RunwayDays))
				}
			}
			if m.Status == statusFailing {
				line += i18n.Sep + l.Pick("cannot be read", "无法读取")
			}
			section.Lines = append(section.Lines, line)
		}
		if len(section.Lines) > 0 {
			msg.Sections = append(msg.Sections, section)
		}
	}

	if spend := overview.Spend; spend.Total > 0 || spend.Fixed > 0 {
		section := notify.Section{Title: l.Pick("Spend this month", "本月花费")}
		line := i18n.Amount(spend.Total, spend.Base)
		if spend.Projected > spend.Total {
			line += i18n.Sep + l.Pick("heading for "+i18n.Amount(spend.Projected, spend.Base), "预计月底 "+i18n.Amount(spend.Projected, spend.Base))
		}
		section.Lines = append(section.Lines, line)
		if spend.Fixed > 0 {
			section.Lines = append(section.Lines, l.Pick("Renewals due this month: ", "本月续费：")+i18n.Amount(spend.Fixed, spend.Base))
		}
		msg.Sections = append(msg.Sections, section)
	}

	upcoming := notify.Section{Title: l.Pick("Coming up", "即将到来")}
	for _, r := range overview.Upcoming {
		if r.Next == nil || r.Days == nil || *r.Days > 30 || len(upcoming.Lines) == 10 {
			continue
		}
		line := l.ShortDate(*r.Next) + " " + weekday(l, *r.Next) + i18n.Sep + "**" + r.Title + "**" + i18n.Sep + l.Until(*r.Days)
		if r.Amount != nil && *r.Amount > 0 {
			line += i18n.Sep + i18n.Amount(*r.Amount, r.Currency)
		}
		upcoming.Lines = append(upcoming.Lines, line)
	}
	if len(upcoming.Lines) > 0 {
		msg.Sections = append(msg.Sections, upcoming)
	}
	return msg, nil
}

func weekday(l i18n.Lang, d recur.Date) string {
	full := l.Date(d, d.Y)
	if l == i18n.ZH {
		return full[strings.LastIndex(full, " ")+1:]
	}
	return full[:strings.Index(full, ",")]
}

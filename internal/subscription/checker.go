package subscription

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/6tail/lunar-go/calendar"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/push"
	"github.com/itswl/quotapulse/internal/store"
)

// Implementation note.
type Checker struct {
	Store    store.Store
	Notifier notify.Notifier
	Log      *slog.Logger
	Cooldown time.Duration
	Push     *push.Manager
	// WebhookType and Source feed per-subscription webhook overrides.
	WebhookType string
	Source      string

	// Implementation note.
	OnNotify func(kind string, ok bool)
}

// Notification states surfaced on the subscription page. not_due is intentionally
// omitted from results: with need_alert false there is nothing to report.
const (
	AlertStateSent            = "sent"
	AlertStateCooldownSkipped = "cooldown_skipped"
	AlertStateFailed          = "failed"
	AlertStateDryRun          = "dry_run"
	AlertStateSnoozed         = "snoozed"
)

// Implementation note.
func (c *Checker) Check(ctx context.Context, subs []model.Subscription, dryRun bool) []model.SubscriptionResult {
	if len(subs) == 0 {
		c.log().Info("operation,operation(operationdatabase dynamic configuration)")
		return []model.SubscriptionResult{}
	}
	c.log().Info("operation", "count", len(subs), "dry_run", dryRun)

	results := make([]model.SubscriptionResult, 0, len(subs))
	for _, sub := range subs {
		results = append(results, c.checkOne(ctx, sub, dryRun))
	}
	c.logSummary(results)
	return results
}

func (c *Checker) checkOne(ctx context.Context, sub model.Subscription, dryRun bool) model.SubscriptionResult {
	loc := time.Local
	if sub.Timezone != "" {
		if loaded, loadErr := time.LoadLocation(sub.Timezone); loadErr == nil {
			loc = loaded
		} else {
			c.log().Warn("Invalid timezone; using server local time", "name", sub.Name, "timezone", sub.Timezone)
		}
	}
	today := time.Now().In(loc)
	lastRenewed := parseRenewedDate(sub.LastRenewedDate, today.Location(), c.log())
	days, next := NextRenewal(sub.CycleType, sub.RenewalDay, today, lastRenewed)

	alreadyRenewed := false
	if lastRenewed != nil {
		start := cycleStart(sub.CycleType, sub.RenewalDay, startOfDay(today), next)
		alreadyRenewed = !lastRenewed.Before(start)
	}
	needAlert := days >= 0 && days <= sub.AlertDaysBefore && !alreadyRenewed

	result := model.SubscriptionResult{
		Name:             sub.Name,
		OwnerProject:     sub.OwnerProject,
		RenewalDay:       sub.RenewalDay,
		CycleType:        sub.CycleType,
		DaysUntilRenewal: days,
		NextRenewalDate:  next.Format("2006-01-02"),
		NeedAlert:        needAlert,
		Amount:           sub.Amount,
		AlreadyRenewed:   alreadyRenewed,
		LastRenewedDate:  sub.LastRenewedDate,
	}

	c.log().Info("operation",
		"name", sub.Name,
		"cycle", notify.FormatSubscriptionCycle(sub.CycleType, sub.RenewalDay),
		"amount", sub.Amount, "days_until_renewal", days, "next", result.NextRenewalDate)

	snoozed := sub.SnoozedUntil != nil && *sub.SnoozedUntil >= today.Format("2006-01-02")
	switch {
	case alreadyRenewed:
		c.log().Info("operation,operation", "name", sub.Name)
	case !needAlert:
		c.log().Info("operation", "name", sub.Name)
	case snoozed:
		c.log().Info("Subscription reminder snoozed", "name", sub.Name, "until", *sub.SnoozedUntil)
		result.AlertState = AlertStateSnoozed
		result.SnoozedUntil = sub.SnoozedUntil
	case dryRun:
		c.log().Warn("operation,operation", "name", sub.Name, "before_days", sub.AlertDaysBefore)
		result.AlertState = AlertStateDryRun
	default:
		c.log().Warn("operation", "name", sub.Name, "before_days", sub.AlertDaysBefore)
		result.AlertState, result.NextEligibleAt, result.LastError = c.dispatch(ctx, sub, days)
		result.AlertSent = result.AlertState == AlertStateSent
	}
	return result
}

// dispatch decides, sends, and records one renewal reminder. The cooldown is derived
// from the absolute timestamp of the newest sent alert, so a daily schedule can never
// slip past the window by a second the way a trailing count could.
func (c *Checker) dispatch(ctx context.Context, sub model.Subscription, days int) (string, *string, string) {
	alertID := model.SubscriptionID(sub.Name)
	now := time.Now()
	last, err := c.Store.LastSentAlert(ctx, alertID, "subscription_renewal", c.Cooldown)
	if err != nil {
		c.log().Warn("Failed to query alert cooldown; treating it as not cooling down", "name", sub.Name, "error", err)
	}
	if last != nil {
		next := last.Add(c.Cooldown)
		if now.Before(next) {
			nextStr := next.UTC().Format(time.RFC3339)
			c.log().Info("Subscription alert is cooling down", "name", sub.Name, "next_eligible_at", nextStr)
			return AlertStateCooldownSkipped, &nextStr, ""
		}
	}
	if c.Push != nil {
		c.Push.Notify(ctx, "Subscription reminder: "+sub.Name, fmt.Sprintf("Renews in %d days (%s)", days, sub.CycleType))
	}

	notifier := c.Notifier
	if sub.WebhookURL != "" {
		// Per-subscription channel override: same payload style, different destination.
		override, overrideErr := notify.New(sub.WebhookURL, c.WebhookType, c.Source, nil)
		if overrideErr != nil {
			c.log().Error("Invalid subscription webhook URL", "name", sub.Name, "error", overrideErr)
			c.record(ctx, alertID, sub, days, "failed", overrideErr.Error())
			return AlertStateFailed, nil, overrideErr.Error()
		}
		notifier = override
	}
	if notifier == nil {
		c.log().Error("Webhook URL is not configured")
		return AlertStateFailed, nil, "Webhook URL is not configured"
	}

	msg := notify.SubscriptionAlert(sub.Name, sub.OwnerProject, sub.CycleType, sub.RenewalDay, days, sub.Amount)
	sendErr := notifier.Send(ctx, msg)
	if c.OnNotify != nil {
		c.OnNotify(msg.Kind, sendErr == nil)
	}
	if sendErr != nil {
		c.log().Error("Failed to send subscription reminder", "name", sub.Name, "error", sendErr)
		c.record(ctx, alertID, sub, days, "failed", sendErr.Error())
		return AlertStateFailed, nil, sendErr.Error()
	}

	next := now.Add(c.Cooldown).UTC().Format(time.RFC3339)
	c.record(ctx, alertID, sub, days, "sent", "")
	return AlertStateSent, &next, ""
}

// record persists the outcome; failures land in the same history as successes so a
// silent channel is visible instead of only living in the logs.
func (c *Checker) record(ctx context.Context, alertID string, sub model.Subscription, days int, status, errText string) {
	message := fmt.Sprintf("Subscription renewal reminder: %s renews in %d days", sub.Name, days)
	if status == "failed" && errText != "" {
		message = fmt.Sprintf("%s — send failed: %s", message, errText)
	}
	if err := c.Store.SaveAlert(ctx, store.AlertRecord{
		AlertID: alertID, Name: sub.Name, AlertType: "subscription_renewal", Status: status,
		Message:   message,
		Value:     &sub.Amount,
		Threshold: model.Ptr(float64(sub.AlertDaysBefore)),
	}); err != nil {
		c.log().Warn("Failed to record subscription alert", "name", sub.Name, "error", err)
	}
}

// Implementation note.
func parseRenewedDate(value *string, loc *time.Location, log *slog.Logger) *time.Time {
	if value == nil || *value == "" {
		return nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", *value, loc)
	if err != nil {
		log.Warn("Invalid renewal date format", "value", *value)
		return nil
	}
	return &parsed
}

func (c *Checker) logSummary(results []model.SubscriptionResult) {
	needAlert, sent := 0, 0
	for _, r := range results {
		if r.NeedAlert {
			needAlert++
		}
		if r.AlertSent {
			sent++
		}
	}
	c.log().Info("operationCheck summary", "total", len(results), "need_alert", needAlert, "sent", sent)
}

func (c *Checker) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}

// Implementation note.
func NextRenewalFrom(cycleType string, renewalDay int, from time.Time) (time.Time, error) {
	switch cycleType {
	case model.CycleWeekly:
		ahead := renewalDay - isoWeekday(from)
		if ahead <= 0 {
			ahead += 7
		}
		return from.AddDate(0, 0, ahead), nil
	case model.CycleMonthly:
		return shiftMonth(from, 1, renewalDay), nil
	case model.CycleYearly:
		month, day, ok := SplitMMDD(renewalDay)
		if !ok {
			// Implementation note.
			return safeReplaceYear(from, from.Year()+1), nil
		}
		return safeMonthDate(from.Year()+1, time.Month(month), day, from.Location()), nil
	case model.CycleLunarYearly:
		from = startOfDay(from)
		next := nextLunarYearlyDate(renewalDay, from)
		if !next.After(from) {
			lunarToday := calendar.NewLunarFromDate(from)
			if candidate, ok := lunarDate(lunarToday.GetYear()+1, renewalDay, from.Location()); ok {
				next = candidate
			}
		}
		return next, nil
	}
	return time.Time{}, fmt.Errorf("Unsupported cycle type: %s", cycleType)
}

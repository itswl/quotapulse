package subscription

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/6tail/lunar-go/calendar"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/store"
)

// Implementation note.
type Checker struct {
	Store    store.Store
	Notifier notify.Notifier
	Log      *slog.Logger
	Cooldown time.Duration

	// Implementation note.
	OnNotify func(kind string, ok bool)
}

// Implementation note.
func (c *Checker) Check(ctx context.Context, subs []model.Subscription, dryRun bool) []model.SubscriptionResult {
	if len(subs) == 0 {
		c.log().Info("operation,operation(operationdatabase dynamic configuration)")
		return []model.SubscriptionResult{}
	}
	c.log().Info("operation", "count", len(subs), "dry_run", dryRun)

	today := time.Now()
	results := make([]model.SubscriptionResult, 0, len(subs))
	for _, sub := range subs {
		results = append(results, c.checkOne(ctx, sub, today, dryRun))
	}
	c.logSummary(results)
	return results
}

func (c *Checker) checkOne(ctx context.Context, sub model.Subscription, today time.Time, dryRun bool) model.SubscriptionResult {
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

	switch {
	case alreadyRenewed:
		c.log().Info("operation,operation", "name", sub.Name)
	case !needAlert:
		c.log().Info("operation", "name", sub.Name)
	case dryRun:
		c.log().Warn("operation,operation", "name", sub.Name, "before_days", sub.AlertDaysBefore)
	default:
		c.log().Warn("operation", "name", sub.Name, "before_days", sub.AlertDaysBefore)
		result.AlertSent = c.send(ctx, sub, days)
	}
	return result
}

// Implementation note.
func (c *Checker) send(ctx context.Context, sub model.Subscription, days int) bool {
	alertID := model.SubscriptionID(sub.Name)
	cooling, err := c.Store.HasRecentAlert(ctx, alertID, "subscription_renewal", c.Cooldown)
	if err != nil {
		c.log().Warn("operation,operation", "name", sub.Name, "error", err)
	}
	if cooling {
		c.log().Info("operation,operation", "name", sub.Name, "cooldown", c.Cooldown)
		return false
	}
	if c.Notifier == nil {
		c.log().Error("Webhook URL is not configured")
		return false
	}

	msg := notify.SubscriptionAlert(sub.Name, sub.OwnerProject, sub.CycleType, sub.RenewalDay, days, sub.Amount)
	sendErr := c.Notifier.Send(ctx, msg)
	if c.OnNotify != nil {
		c.OnNotify(msg.Kind, sendErr == nil)
	}
	if sendErr != nil {
		c.log().Error("Failed to send subscription reminder", "name", sub.Name, "error", sendErr)
		return false
	}

	if err := c.Store.SaveAlert(ctx, store.AlertRecord{
		AlertID: alertID, Name: sub.Name, AlertType: "subscription_renewal",
		Message:   fmt.Sprintf("Subscription renewal reminder: %s operation %d operation", sub.Name, days),
		Value:     &sub.Amount,
		Threshold: model.Ptr(float64(sub.AlertDaysBefore)),
	}); err != nil {
		c.log().Warn("Failed to record subscription alert", "name", sub.Name, "error", err)
	}
	return true
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

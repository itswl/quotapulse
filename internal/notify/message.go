package notify

import (
	"fmt"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

// BalanceAlert: balance is low.
//
// The balanceAlert helper below also takes balanceType and unit parameters,
// but callers only ever pass "余额" and an empty string — the real balance
// type (credits / quota) affects only the dashboard, not alert copy.
// Keep them off the signature; extend balanceAlert when needed.
func BalanceAlert(projectName string, ownerProject *string, provider string, currentValue, threshold float64) Message {
	return balanceAlert(projectName, ownerProject, provider, "balance", currentValue, threshold, "")
}

func balanceAlert(projectName string, ownerProject *string, provider, balanceType string,
	currentValue, threshold float64, unit string) Message {
	current := unit + formatAmount(currentValue)
	limit := unit + formatAmount(threshold)

	lines := []string{"API call: " + projectName}
	lines = appendOwner(lines, ownerProject)
	lines = append(lines,
		"Provider: "+provider,
		"Current "+balanceType+": "+current,
		"Alert threshold: "+limit,
		"Status: ⚠️ "+balanceType+" insufficient",
	)

	return Message{
		Title: "Balance alert",
		Lines: lines,
		Kind:  KindBalance,
		envelope: &envelope{
			Type:     "AlarmNotification",
			RuleName: projectName + " balance alert",
			Level:    "critical",
			Resources: []any{balanceResource{
				ProjectName:  projectName,
				OwnerProject: ownerProject,
				Provider:     provider,
				BalanceType:  balanceType,
				CurrentValue: currentValue,
				Threshold:    threshold,
				Unit:         unit,
				Message: fmt.Sprintf("Project [%s] has insufficient %s; current: %s, threshold: %s",
					projectName, balanceType, current, limit),
			}},
		},
	}
}

// CheckFailedAlert reports an account whose balance could not be read, such as a revoked
// or expired key or a provider outage. Without it the account would silently drop out
// of monitoring.
func CheckFailedAlert(projectName string, ownerProject *string, provider, reason string) Message {
	lines := []string{"API call: " + projectName}
	lines = appendOwner(lines, ownerProject)
	lines = append(lines,
		"Provider: "+provider,
		"Error: "+reason,
		"Status: ❌ balance unavailable; this account is not monitored until a check succeeds",
	)

	return Message{
		Title: "Balance check failed",
		Lines: lines,
		Kind:  KindCheckFailed,
		envelope: &envelope{
			Type:     "AlarmNotification",
			RuleName: projectName + " balance check failed",
			Level:    "critical",
			Resources: []any{checkFailedResource{
				ProjectName:  projectName,
				OwnerProject: ownerProject,
				Provider:     provider,
				Error:        reason,
				Message:      fmt.Sprintf("Project [%s] balance check failed: %s", projectName, reason),
			}},
		},
	}
}

// SubscriptionAlert: a subscription is due soon.
func SubscriptionAlert(name string, ownerProject *string, cycleType string,
	renewalDay, daysUntilRenewal int, amount float64) Message {
	cycle := FormatSubscriptionCycle(cycleType, renewalDay)

	lines := []string{"Subscription: " + name}
	lines = appendOwner(lines, ownerProject)
	lines = append(lines,
		"Renewal cycle: "+cycle,
		"Time until renewal: "+formatDays(daysUntilRenewal),
		"Renewal amount: "+formatFloat(amount),
	)

	// today or overdue goes out as critical; a few days out is just a reminder
	level := "warning"
	if daysUntilRenewal <= 0 {
		level = "critical"
	}

	return Message{
		Title: "Subscription reminder",
		Lines: lines,
		Kind:  KindSubscription,
		envelope: &envelope{
			Type:     "SubscriptionReminder",
			RuleName: name + " subscription reminder",
			Level:    level,
			Resources: []any{subscriptionResource{
				SubscriptionName: name,
				OwnerProject:     ownerProject,
				RenewalDay:       renewalDay,
				CycleType:        cycleType,
				DaysUntilRenewal: daysUntilRenewal,
				Amount:           amount,
				Message: fmt.Sprintf("Subscription reminder for [%s]: due %s (%s), amount: %s",
					name, model.RenewsIn(daysUntilRenewal), cycle, formatFloat(amount)),
			}},
		},
	}
}

// Custom is a rich-text alert shared by the runway-exhausted, spend-spike and
// weekly-report paths; the caller builds the Markdown body. kind only feeds
// metric classification, not the payload.
func Custom(title string, lines []string, kind string) Message {
	return Message{Title: title, Lines: lines, Kind: kind}
}

// EmailAlert: a mail hit a keyword.
func EmailAlert(mailbox, subject, sender, date string, keywords []string,
	serviceName *string, amount *float64) Message {
	if mailbox == "" {
		mailbox = "Unknown"
	}

	lines := []string{
		"**Mailbox**: " + mailbox,
		"**Sender**: " + sender,
		"**Date**: " + date,
		"**Service**: " + serviceText(serviceName),
	}
	// an unrecognized amount (or 0) omits the line; an empty number in the group helps nobody
	if amount != nil && *amount != 0 {
		lines = append(lines, "**Amount**: "+formatFloat(*amount))
	}
	lines = append(lines, "**Keywords**: "+strings.Join(keywords, ", "))

	return Message{Title: "📧 Email alert: " + subject, Lines: lines, Kind: KindEmail}
}

// MailboxError: a mailbox is unreachable.
//
// host is context the caller already has, but the body never mentions it —
// the on-call group knows these exact three lines; one more line is a
// template change. The parameter stays so callers stay untouched.
func MailboxError(mailbox, host, reason string) Message {
	return mailboxError(mailbox, reason, time.Now())
}

// mailboxError splits out the clock so tests can pin the "**time**" line.
func mailboxError(mailbox, reason string, at time.Time) Message {
	return Message{
		Title: "❌ Mailbox connection failed",
		Lines: []string{
			"**Mailbox**: " + mailbox,
			"**Error message**: " + reason,
			"**Time**: " + at.Format("2006-01-02 15:04:05"),
		},
		Kind: KindMailboxError,
	}
}

// FormatSubscriptionCycle humanizes the renewal cycle; the subscription
// checker and the config self-check use it too.
//
// It returns "未知周期" for unrecognized cycle types so callers can surface
// the config error. Do not "fix" it into a monthly fallback ("每月 15 号"):
// a mistyped cycle would then keep sending plausible-looking reminders until
// someone notices the renewal dates never match.
func FormatSubscriptionCycle(cycleType string, renewalDay int) string {
	switch cycleType {
	case model.CycleWeekly:
		weekdays := []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
		if renewalDay >= 1 && renewalDay <= 7 {
			return "Weekly " + weekdays[renewalDay-1]
		}
		return fmt.Sprintf("Weekly day %d", renewalDay)
	case model.CycleYearly:
		if month, day, ok := splitMMDD(renewalDay); ok {
			return fmt.Sprintf("Annually on %02d-%02d", month, day)
		}
		// not a valid MMDD: a stable label beats guessing a date
		return "Fixed annual date"
	case model.CycleLunarYearly:
		if month, day, ok := splitMMDD(renewalDay); ok {
			return fmt.Sprintf("Annually on lunar %02d-%02d", month, day)
		}
		return "Fixed lunar date"
	case model.CycleMonthly:
		return fmt.Sprintf("Monthly on day %d", renewalDay)
	}
	return "Unknown cycle"
}

// splitMMDD splits a yearly MMDD integer (like 315) into month and day.
//
// Same rules as internal/subscription.SplitMMDD, rewritten here instead of
// importing: subscription needs notify to send reminders, and depending back
// on it would close the cycle.
func splitMMDD(renewalDay int) (month, day int, ok bool) {
	// values <= 31 are "day only" from old configs, not MMDD
	if renewalDay <= 31 {
		return 0, 0, false
	}
	month, day = renewalDay/100, renewalDay%100
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return 0, 0, false
	}
	return month, day, true
}

// formatDays humanizes day counts: 0 is today, 1 is tomorrow, the rest "in N days".
func formatDays(days int) string {
	switch days {
	case 0:
		return "Today"
	case 1:
		return "Tomorrow"
	}
	return fmt.Sprintf("In %d days", days)
}

// appendOwner omits the whole line without an owning project, rather than an empty "Owning project: " line.
func appendOwner(lines []string, ownerProject *string) []string {
	if ownerProject == nil || *ownerProject == "" {
		return lines
	}
	return append(lines, "Owner project: "+*ownerProject)
}

// serviceText writes the literal "None" when no service was recognized, so the
// group sees "**Service**: None". It looks odd, but the on-call group knows this
// exact word; "unknown" would be a copy change pinned by message_test.go.
func serviceText(name *string) string {
	if name == nil {
		return "None"
	}
	return *name
}

package notify

import (
	"fmt"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

// Implementation note.
//
// Implementation note.
// Implementation note.
// Implementation note.
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

// Implementation note.
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

	// Implementation note.
	level := "warning"
	if daysUntilRenewal <= 0 {
		level = "critical"
	}

	return Message{
		Title: "Subscription renewal reminder",
		Lines: lines,
		Kind:  KindSubscription,
		envelope: &envelope{
			Type:     "SubscriptionReminder",
			RuleName: name + " renewal reminder",
			Level:    level,
			Resources: []any{subscriptionResource{
				SubscriptionName: name,
				OwnerProject:     ownerProject,
				RenewalDay:       renewalDay,
				CycleType:        cycleType,
				DaysUntilRenewal: daysUntilRenewal,
				Amount:           amount,
				Message: fmt.Sprintf("Subscription [%s] renews in %d days (%s), amount: %s",
					name, daysUntilRenewal, cycle, formatFloat(amount)),
			}},
		},
	}
}

// Implementation note.
// Implementation note.
func Custom(title string, lines []string, kind string) Message {
	return Message{Title: title, Lines: lines, Kind: kind}
}

// Implementation note.
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
	// Implementation note.
	if amount != nil && *amount != 0 {
		lines = append(lines, "**Amount**: "+formatFloat(*amount))
	}
	lines = append(lines, "**Keywords**: "+strings.Join(keywords, ", "))

	return Message{Title: "📧 Email alert: " + subject, Lines: lines, Kind: KindEmail}
}

// Implementation note.
//
// Implementation note.
// Implementation note.
func MailboxError(mailbox, host, reason string) Message {
	return mailboxError(mailbox, reason, time.Now())
}

// Implementation note.
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

// Implementation note.
//
// Implementation note.
// Implementation note.
// Implementation note.
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
		// Implementation note.
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

// Implementation note.
//
// Implementation note.
// Implementation note.
func splitMMDD(renewalDay int) (month, day int, ok bool) {
	// Implementation note.
	if renewalDay <= 31 {
		return 0, 0, false
	}
	month, day = renewalDay/100, renewalDay%100
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return 0, 0, false
	}
	return month, day, true
}

// Implementation note.
func formatDays(days int) string {
	switch days {
	case 0:
		return "Today"
	case 1:
		return "Tomorrow"
	}
	return fmt.Sprintf("In %d days", days)
}

// Implementation note.
func appendOwner(lines []string, ownerProject *string) []string {
	if ownerProject == nil || *ownerProject == "" {
		return lines
	}
	return append(lines, "Owner project: "+*ownerProject)
}

// Implementation note.
// Implementation note.
func serviceText(name *string) string {
	if name == nil {
		return "None"
	}
	return *name
}

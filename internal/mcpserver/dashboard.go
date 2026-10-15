package mcpserver

// // dashboard_summary: one condensed card of everything an agent needs first.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/store"
)

func (d *deps) dashboard(ctx context.Context) (DashboardSummaryOutput, string, *ErrorInfo) {
	health := d.health(ctx)
	out := DashboardSummaryOutput{
		Meta: health.Meta, Attention: []AttentionItem{},
		Service: ServiceOverview{Status: health.Status, Version: health.Version, IsStale: health.IsStale, FailingJobs: health.FailedJobs},
	}
	var critical, warnings []AttentionItem
	add := func(severe bool, kind, subject string, projectID *string, message string) {
		item := AttentionItem{Severity: "warning", Kind: kind, Subject: subject, ProjectID: projectID, Message: message}
		if severe {
			item.Severity = "critical"
			critical = append(critical, item)
			return
		}
		warnings = append(warnings, item)
	}

	if d.allows(config.ScopeBalance) {
		balances := d.balanceStatus(ctx, balanceStatusInput{})
		out.Balances = &BalanceOverview{Counts: balances.Counts}
		for _, p := range balances.Projects {
			id := optional(p.ProjectID)
			switch p.Status {
			case "check_failed":
				add(true, "check_failed", p.ProjectName, id, "the balance can't be read: "+p.Check.Error.Message)
			case "below_threshold":
				add(true, "below_threshold", p.ProjectName, id, fmt.Sprintf("balance %s is below the threshold %s", amount(p.Balance), amount(p.Threshold)))
			}
			if p.Runway == nil || p.Runway.Days == nil {
				continue
			}
			days := *p.Runway.Days
			if s := out.Balances.ShortestRunway; s == nil || days < s.Days {
				out.Balances.ShortestRunway = &RunwayBrief{ProjectID: p.ProjectID, ProjectName: p.ProjectName, Provider: p.Provider, Days: days, DepletionDate: p.Runway.DepletionDate}
			}
			if limit := d.settings.RunwayAlertDays; limit > 0 && days <= limit {
				message := fmt.Sprintf("about %.1f days of balance left", days)
				if p.Runway.DepletionDate != nil {
					message += ", running out around " + *p.Runway.DepletionDate
				}
				add(days <= 1, "runway_short", p.ProjectName, id, message)
			}
		}
	}

	if d.allows(config.ScopeSubscriptions) {
		subs := d.subscriptionStatus()
		overview := &SubscriptionOverview{Total: subs.Counts.Total, Due: subs.Counts.Due}
		for _, s := range subs.Subscriptions {
			if s.Reminder.Due {
				add(s.DaysUntilRenewal <= 1, "renewal_due", s.Name, nil,
					fmt.Sprintf("renews %s (%s), %s", model.RenewsIn(s.DaysUntilRenewal), s.NextRenewalDate, strconv.FormatFloat(s.Amount, 'f', -1, 64)))
			}
			if s.AlreadyRenewed {
				continue
			}
			if s.DaysUntilRenewal <= 7 {
				overview.Within7Days++
			}
			if s.DaysUntilRenewal <= 30 {
				overview.Within30Days++
			}
			if overview.Next == nil || s.DaysUntilRenewal < overview.Next.DaysUntilRenewal {
				overview.Next = &RenewalBrief{SubscriptionID: s.SubscriptionID, Name: s.Name, NextRenewalDate: s.NextRenewalDate, DaysUntilRenewal: s.DaysUntilRenewal, Amount: s.Amount}
			}
		}
		out.Subscriptions = overview
	}

	if d.allows(config.ScopeEmail) {
		scan := d.emailScan(ctx)
		overview := &EmailOverview{Status: scan.Status, LastScanAt: scan.LastScanAt, FailedMailboxes: scan.Summary.FailedMailboxes}
		var latest string
		for _, a := range scan.Alerts {
			if !a.Duplicate {
				overview.AlertEmails++
				latest = a.Subject
			}
		}
		if overview.AlertEmails > 0 {
			add(false, "email_alerts", "email", nil, fmt.Sprintf("%s in the last scan, such as %q", model.Quantity(overview.AlertEmails, "new alert email", "new alert emails"), latest))
		}
		for _, m := range scan.Mailboxes {
			if !m.Success {
				message := "the last scan could not read it"
				if m.Error != nil {
					message += ": " + *m.Error
				}
				add(false, "mailbox_failed", m.Name, nil, message)
			}
		}
		out.Email = overview
	}

	if d.allows(config.ScopeAlerts) && d.historyAvailable() {
		overview := &AlertOverview{Days: 7, ByType: map[string]int{}, Latest: []AlertView{}}
		if stats, err := d.history.AlertStats(ctx, 7); err == nil && stats != nil {
			overview.Total = stats.TotalAlerts
			for kind, n := range stats.ByType {
				overview.ByType[kind] = n
			}
		}
		if rows, err := d.history.RecentAlerts(ctx, store.AlertQuery{Days: 7, Limit: 5}); err == nil {
			providerOf := d.providerOf()
			for _, row := range rows {
				overview.Latest = append(overview.Latest, alertView(row, providerOf))
			}
		}
		out.Alerts = overview
	}

	for _, job := range health.Jobs {
		if job.Status == "failing" {
			add(false, "job_failing", job.Name, nil, "the last run failed: "+deref(job.LastError))
		}
	}
	if !health.HasData {
		add(false, "no_data", "balances", nil, "no balance has been checked yet; set a {PROVIDER}_API_KEY or add a project on the dashboard")
	}
	if health.IsStale && health.Meta.DataAgeSeconds != nil {
		add(false, "stale_data", "balances", nil, fmt.Sprintf("the last successful refresh was %s ago", (time.Duration(*health.Meta.DataAgeSeconds)*time.Second).String()))
	}

	out.Attention = append(append(out.Attention, critical...), warnings...)
	switch {
	case health.Status == "degraded":
		out.Status = "degraded"
	case len(out.Attention) > 0:
		out.Status = "attention"
	default:
		out.Status = "ok"
	}
	return out, dashboardSummary(out), nil
}

func dashboardSummary(out DashboardSummaryOutput) string {
	if len(out.Attention) == 0 {
		return out.Status + ": nothing needs attention"
	}
	parts := make([]string, 0, 3)
	for _, item := range out.Attention[:min(3, len(out.Attention))] {
		parts = append(parts, item.Subject+" "+strings.ReplaceAll(item.Kind, "_", " "))
	}
	summary := out.Status + ": " + strings.Join(parts, ", ")
	if more := len(out.Attention) - len(parts); more > 0 {
		summary += fmt.Sprintf(" and %d more", more)
	}
	return summary
}

func amount(value *float64) string {
	if value == nil {
		return "unknown"
	}
	return strconv.FormatFloat(*value, 'f', 2, 64)
}

func deref(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}

// ==================== Resources ====================

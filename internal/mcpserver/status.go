package mcpserver

// // Live status views: balance, subscriptions, email scan, jobs, health and capabilities.

import (
	"context"
	"slices"
	"strings"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/model"
)

func (d *deps) balanceStatus(ctx context.Context, f balanceStatusInput) BalanceStatusOutput {
	snapshot := d.runtime.Balance()
	name := strings.ToLower(strings.TrimSpace(f.Project))
	keep := func(p ProjectStatus) bool {
		return (name == "" || strings.Contains(strings.ToLower(p.ProjectName), name)) &&
			(len(f.ProjectIDs) == 0 || slices.Contains(f.ProjectIDs, p.ProjectID)) &&
			(len(f.ProjectNames) == 0 || slices.Contains(f.ProjectNames, p.ProjectName)) &&
			(f.Provider == "" || p.Provider == f.Provider) &&
			(f.OwnerProject == "" || (p.OwnerProject != nil && *p.OwnerProject == f.OwnerProject)) &&
			(f.Status == "" || p.Status == f.Status)
	}
	projects := make([]ProjectStatus, 0, len(snapshot.Projects))
	seen := map[string]bool{}
	for _, r := range snapshot.Projects {
		p := d.projectStatus(r)
		seen[p.ProjectID] = true
		if keep(p) {
			projects = append(projects, p)
		}
	}
	if f.IncludeDisabled || f.Status == "disabled" {
		for _, c := range d.config(ctx).Projects {
			if c.Enabled || seen[c.ID()] {
				continue
			}
			threshold := c.Threshold
			p := ProjectStatus{
				ProjectID: c.ID(), ProjectName: c.Name, Provider: c.Provider, OwnerProject: c.OwnerProject,
				BalanceType: c.Type, Unit: unitOf(c.Type), Status: "disabled", Threshold: &threshold,
				RunwayNote: "disabled projects are not checked", Check: CheckView{Status: "disabled"},
			}
			if keep(p) {
				projects = append(projects, p)
			}
		}
	}
	return BalanceStatusOutput{Meta: d.meta("live_state", snapshot.LastUpdate), Counts: countBalances(projects), Projects: projects}
}

func (d *deps) projectStatus(r model.CheckResult) ProjectStatus {
	ps := ProjectStatus{
		ProjectID: model.ProjectID(r.Provider, r.Project), ProjectName: r.Project, Provider: r.Provider,
		OwnerProject: r.OwnerProject, BalanceType: r.Type, Unit: unitOf(r.Type),
		Balance: r.Credits, Threshold: r.Threshold, AlertSent: r.AlarmSent,
		Check: CheckView{Status: "ok", CheckedAt: r.CheckedAt, LatencyMs: r.LatencyMs, Cached: r.Cached},
	}
	switch {
	case !r.Success:
		ps.Status, ps.Check.Status = "check_failed", "failed"
		ps.Check.Error = d.checkError(r)
	case r.NeedAlarm:
		ps.Status = "below_threshold"
	default:
		ps.Status = "healthy"
	}
	ps.Runway, ps.RunwayNote = d.runwayView(r)
	return ps
}

func (d *deps) checkError(r model.CheckResult) *ErrorInfo {
	message := "The balance check failed"
	if r.Error != nil && *r.Error != "" {
		message = *r.Error
	}
	category, retryable := classifyCheckError(message)
	info := &ErrorInfo{Category: category, Message: message, Retryable: retryable, Provider: r.Provider, UsedCachedData: r.Cached}
	if retryable {
		info.RetryAfterSeconds = d.secondsUntilNextCheck()
	}
	return info
}

func (d *deps) runwayView(r model.CheckResult) (*RunwayView, string) {
	switch {
	case !r.Success:
		return nil, "no estimate while the balance can't be read"
	case r.Type == model.TypeQuota:
		return nil, "quota plans are not estimated; they reset on the provider's schedule"
	case r.Runway == nil && !d.historyAvailable():
		return nil, "estimates need balance history; set ENABLE_DATABASE=true"
	case r.Runway == nil || r.Runway.Confidence == model.ConfidenceNone:
		return nil, "not enough balance history yet; estimates appear after a few hours of checks"
	}
	rw := r.Runway
	return &RunwayView{
		Days: rw.RunwayDays, DepletionDate: rw.DepletionDate, BurnPerDay: rw.BurnPerDay,
		MonthlyProjection: rw.MonthlyProjection, Confidence: rw.Confidence, WindowDays: rw.WindowDays,
		ToppedUp: rw.ToppedUp, SpikeRatio: rw.SpikeRatio,
	}, ""
}

func countBalances(projects []ProjectStatus) BalanceCounts {
	counts := BalanceCounts{Total: len(projects)}
	for _, p := range projects {
		switch p.Status {
		case "healthy":
			counts.Healthy++
		case "below_threshold":
			counts.BelowThreshold++
		case "check_failed":
			counts.CheckFailed++
		case "disabled":
			counts.Disabled++
		}
	}
	return counts
}

func (d *deps) subscriptionStatus() SubscriptionStatusOutput {
	snapshot := d.runtime.Subscriptions()
	out := SubscriptionStatusOutput{Meta: d.meta("live_state", snapshot.LastUpdate), Subscriptions: make([]SubscriptionView, 0, len(snapshot.Subscriptions))}
	for _, s := range snapshot.Subscriptions {
		view := subscriptionView(s)
		out.Subscriptions = append(out.Subscriptions, view)
		if view.Reminder.Due {
			out.Counts.Due++
		}
		if view.AlreadyRenewed {
			out.Counts.Renewed++
		}
	}
	out.Counts.Total = len(out.Subscriptions)
	return out
}

func subscriptionView(s model.SubscriptionResult) SubscriptionView {
	reminder := s.AlertState
	switch {
	case reminder != "":
	case s.NeedAlert:
		reminder = "pending"
	default:
		reminder = "not_due"
	}
	return SubscriptionView{
		SubscriptionID: model.SubscriptionID(s.Name), Name: s.Name, OwnerProject: s.OwnerProject,
		CycleType: s.CycleType, RenewalDay: s.RenewalDay, NextRenewalDate: s.NextRenewalDate,
		DaysUntilRenewal: s.DaysUntilRenewal, Amount: s.Amount, AlreadyRenewed: s.AlreadyRenewed,
		LastRenewedDate: s.LastRenewedDate,
		Reminder: ReminderView{
			Due: s.NeedAlert, State: reminder, NextEligibleAt: s.NextEligibleAt,
			SnoozedUntil: s.SnoozedUntil, LastError: optional(s.LastError),
		},
	}
}

func (d *deps) emailScan(ctx context.Context) EmailScanOutput {
	scan := d.runtime.EmailScan()
	out := EmailScanOutput{
		Meta: d.meta("live_state", scan.LastUpdate), Days: scan.Days, DryRun: scan.DryRun, LastScanAt: scan.LastUpdate,
		Mailboxes: make([]MailboxScanView, 0, len(scan.Mailboxes)), Alerts: make([]EmailAlertView, 0, len(scan.Alerts)),
		Summary: EmailSummaryView{
			TotalMailboxes: scan.Summary.TotalMailboxes, FailedMailboxes: scan.Summary.FailedMailboxes,
			TotalEmails: scan.Summary.TotalEmails, TotalAlerts: scan.Summary.TotalAlerts, AlertsSent: scan.Summary.AlertsSent,
		},
	}
	for _, m := range scan.Mailboxes {
		out.Mailboxes = append(out.Mailboxes, MailboxScanView{
			Name: m.Name, Host: m.Host, Port: m.Port, Username: maskEmail(m.Username), Success: m.Success,
			Error: m.Error, TotalEmails: m.TotalEmails, AlertCount: m.AlertCount,
		})
	}
	for _, a := range scan.Alerts {
		out.Alerts = append(out.Alerts, EmailAlertView{
			Mailbox: a.Mailbox, Subject: a.Subject, Sender: a.Sender, Date: a.Date, Keywords: nonNil(a.Keywords),
			ServiceName: a.ServiceName, Amount: a.Amount, AlertSent: a.AlertSent, Duplicate: a.Duplicate,
		})
	}
	switch {
	case len(d.config(ctx).EnabledMailboxes()) == 0:
		out.Status, out.Reason = "configured_but_inactive", "no mailbox is configured and enabled"
	case scan.LastUpdate == nil && len(d.settings.EmailScanTimes) == 0:
		out.Status, out.Reason = "configured_but_inactive", "EMAIL_SCAN_SCHEDULE is empty, so scans only run on demand"
	case scan.LastUpdate == nil:
		out.Status, out.Reason = "not_scanned_yet", "the first scan runs at the next EMAIL_SCAN_SCHEDULE time"
	default:
		out.Status = "active"
	}
	return out
}

func (d *deps) jobViews(ctx context.Context) []JobView {
	mailboxes := len(d.config(ctx).EnabledMailboxes())
	jobs := d.runtime.Jobs().Jobs
	views := make([]JobView, 0, len(jobs))
	for _, job := range jobs {
		view := JobView{
			Name: job.Name, Description: job.Description, Schedule: job.Schedule, Enabled: job.Enabled,
			NextRun: job.NextRun, LastRun: job.LastRun, LastSuccess: job.LastSuccess, LastError: job.LastError,
			LastDurationSeconds: job.LastDuration, Runs: job.Runs, Failures: job.Failures, Status: "ok",
		}
		switch {
		case !job.Enabled:
			view.Status, view.Reason = "disabled", "no schedule is set"
		case job.LastError != nil:
			view.Status, view.Reason = "failing", "the last run failed; see last_error"
		case job.Name == "email_scan" && mailboxes == 0:
			view.Status, view.Reason = "configured_but_inactive", "scheduled, but no mailbox is configured and enabled"
		case job.LastRun == nil:
			view.Status, view.Reason = "scheduled_not_yet_run", "the first run is at next_run"
		}
		views = append(views, view)
	}
	return views
}

func (d *deps) jobStatus(ctx context.Context) JobStatusOutput {
	return JobStatusOutput{Meta: d.meta("live_state", nil), Healthy: d.runtime.Jobs().Healthy, Jobs: d.jobViews(ctx)}
}

func (d *deps) health(ctx context.Context) HealthOutput {
	balance := d.runtime.Balance()
	jobs := d.runtime.Jobs()
	meta := d.meta("live_state", balance.LastUpdate)
	status := "healthy"
	if len(balance.Projects) == 0 || !jobs.Healthy || meta.IsStale {
		status = "degraded"
	}
	return HealthOutput{
		Meta: meta, Status: status, Version: d.settings.AppVersion, HasData: len(balance.Projects) > 0,
		IsStale: meta.IsStale, LastUpdate: balance.LastUpdate, JobsHealthy: jobs.Healthy,
		FailedJobs: nonNil(d.runtime.FailedJobs()), Jobs: d.jobViews(ctx), UptimeSeconds: d.runtime.UptimeSeconds(),
	}
}

func (d *deps) capabilities(ctx context.Context) CapabilitiesOutput {
	cfg := d.config(ctx)
	scopes := make([]string, 0, len(config.AllScopes))
	for _, scope := range config.AllScopes {
		if d.caller.Allows(scope) {
			scopes = append(scopes, scope)
		}
	}
	return CapabilitiesOutput{
		Meta:    d.meta("configuration", nil),
		Version: d.settings.AppVersion,
		Features: map[string]bool{
			"database": d.historyAvailable(), "dynamic_config": d.settings.EnableDynamicConfig,
			"history": d.settings.EnableHistoryAPI, "subscriptions": d.settings.EnableSubscriptions,
			"mcp": d.settings.EnableMCP, "prometheus": d.settings.EnablePrometheus,
			// Same rule as /api/features.
			"email_scan": d.settings.EnableEmailScan || (len(d.settings.EmailScanTimes) > 0 && len(cfg.EnabledMailboxes()) > 0),
		},
		ConfigCounts: map[string]int{"projects": len(cfg.Projects), "subscriptions": len(cfg.Subscriptions), "mailboxes": len(cfg.Mailboxes)},
		Caller:       d.caller.Name,
		Scopes:       scopes,
		Conventions: Conventions{
			Units:       map[string]string{"balance": unitBalance, "credits": unitCredits, "quota": unitQuota},
			Timezone:    "Dates (YYYY-MM-DD), schedules and renewal days use the server timezone in meta.timezone; timestamps are RFC 3339.",
			RenewalDays: "weekly 1-7 (Monday-Sunday), monthly 1-31 (past a month's end means its last day), yearly MMDD, lunar_yearly a Chinese lunar MMDD; next_renewal_date is always Gregorian.",
			ProjectIDs:  "project_id is derived from provider and project name, so renaming a project gives it a new ID; it is the same in every tool and in the REST API.",
		},
	}
}

// ==================== Tools ====================

func subscriptionConfigView(s model.Subscription) SubscriptionConfigView {
	return SubscriptionConfigView{
		Name: s.Name, OwnerProject: s.OwnerProject, CycleType: s.CycleType, RenewalDay: s.RenewalDay,
		AlertDaysBefore: s.AlertDaysBefore, Amount: s.Amount, Enabled: s.Enabled,
		LastRenewedDate: s.LastRenewedDate, Timezone: s.Timezone, SnoozedUntil: s.SnoozedUntil,
		WebhookOverride: s.WebhookURL != "",
	}
}

// ==================== History tools ====================

// window parses a history tool's time range; a bad one is an invalid argument.

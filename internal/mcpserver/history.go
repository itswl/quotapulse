package mcpserver

// // History-backed tools: time windows, paging and the alert / trend / spend queries.

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/store"
)

func (d *deps) window(r timeRange) (window, *ErrorInfo) {
	w, err := r.parse(d.now(), time.Local)
	if err != nil {
		return w, invalidArgument(err.Error())
	}
	return w, nil
}

// providerOf maps the project IDs of the live state to their providers, so history rows
// can name the provider their table doesn't store.
func (d *deps) providerOf() func(projectID string) *string {
	providers := map[string]string{}
	for _, r := range d.runtime.Balance().Projects {
		providers[model.ProjectID(r.Provider, r.Project)] = r.Provider
	}
	return func(projectID string) *string { return optional(providers[projectID]) }
}

func (d *deps) historyMeta(truncated bool) Meta {
	m := d.meta("persisted_history", nil)
	m.Truncated = truncated
	return m
}

func (d *deps) balanceHistory(ctx context.Context, in balanceHistoryInput) (BalanceHistoryOutput, string, *ErrorInfo) {
	if !d.historyAvailable() {
		return BalanceHistoryOutput{}, "", &errHistoryUnavailable
	}
	w, failure := d.window(in.timeRange)
	if failure != nil {
		return BalanceHistoryOutput{}, "", failure
	}
	rows, err := d.history.BalanceHistory(ctx, store.BalanceQuery{ProjectID: in.ProjectID, Provider: in.Provider, Days: w.days, Limit: historyReadLimit})
	if err != nil {
		return BalanceHistoryOutput{}, "", d.historyFailed("balance_history", err)
	}
	entries := make([]entry[BalanceSnapshotView], 0, len(rows))
	for _, row := range rows {
		entries = append(entries, newEntry(row.Timestamp, rowID("balance", row.ID), BalanceSnapshotView{
			ID: row.ID, ProjectID: row.ProjectID, ProjectName: row.ProjectName, Provider: row.Provider,
			BalanceType: row.BalanceType, Unit: unitOf(row.BalanceType), Balance: row.Balance,
			Threshold: row.Threshold, BelowThreshold: row.NeedAlarm, Timestamp: row.Timestamp,
		}))
	}
	p := paginate(entries, w, bounded(in.Limit, 100, 1, 100), len(rows) >= historyReadLimit)
	out := BalanceHistoryOutput{Meta: d.historyMeta(p.truncated), Count: len(p.items), Snapshots: p.items, NextCursor: optional(p.next)}
	return out, pageSummary(out.Count, "snapshot", "snapshots", p), nil
}

// pageSummary is "N rows", noting when more pages follow.
func pageSummary[T any](n int, one, many string, p page[T]) string {
	summary := model.Quantity(n, one, many)
	switch {
	case p.next != "":
		summary += "; more with next_cursor"
	case p.truncated:
		summary += "; older rows may exist, narrow the range with since and until"
	}
	return summary
}

func (d *deps) trend(ctx context.Context, in trendInput) (TrendOutput, string, *ErrorInfo) {
	if strings.TrimSpace(in.ProjectID) == "" {
		return TrendOutput{}, "", invalidArgument("project_id is required; balance_status lists every project's")
	}
	if !d.historyAvailable() {
		return TrendOutput{}, "", &errHistoryUnavailable
	}
	interval := in.Interval
	if interval == "" {
		interval = "day"
	}
	var after *position
	if in.Cursor != "" {
		var err error
		if after, err = decodeCursor(in.Cursor); err != nil {
			return TrendOutput{}, "", invalidArgument(err.Error())
		}
	}
	days := bounded(in.Days, 30, 1, 365)
	trend, err := d.history.BalanceTrend(ctx, in.ProjectID, days)
	if err != nil {
		return TrendOutput{}, "", d.historyFailed("balance_trend", err)
	}
	if trend == nil {
		return TrendOutput{}, "", &ErrorInfo{Category: "not_found", Message: fmt.Sprintf("No balance history for project %s in the last %s", in.ProjectID, model.Quantity(days, "day", "days"))}
	}
	samples := make([]sample, 0, len(trend.History))
	for _, p := range trend.History {
		at, _ := time.Parse(time.RFC3339Nano, p.Timestamp)
		samples = append(samples, sample{at: at, balance: p.Balance, below: p.NeedAlarm})
	}
	out := TrendOutput{
		Meta: d.meta("persisted_history", optional(trend.LastTimestamp)), ProjectID: in.ProjectID, ProjectName: trend.ProjectName,
		Days: days, Interval: interval, Threshold: nonZero(trend.Threshold), Summary: summarize(samples), Points: []TrendPointView{},
	}
	for _, r := range d.runtime.Balance().Projects {
		if model.ProjectID(r.Provider, r.Project) == in.ProjectID {
			out.Provider, out.Unit = optional(r.Provider), optional(unitOf(r.Type))
		}
	}
	limit := bounded(in.Limit, 500, 1, 1000)
	for _, point := range bucket(samples, interval, time.Local) {
		at, _ := time.Parse(time.RFC3339Nano, point.Timestamp)
		if after != nil && !at.After(after.at) {
			continue
		}
		if len(out.Points) == limit {
			last, _ := time.Parse(time.RFC3339Nano, out.Points[limit-1].Timestamp)
			out.NextCursor = optional(encodeCursor(position{at: last}))
			break
		}
		out.Points = append(out.Points, point)
	}
	summary := fmt.Sprintf("%s over %s, net change %s", model.Quantity(len(out.Points), "point", "points"),
		model.Quantity(days, "day", "days"), signed(out.Summary.Change))
	if out.NextCursor != nil {
		summary += "; more with next_cursor"
	}
	return out, summary, nil
}

func signed(value float64) string {
	text := strconv.FormatFloat(value, 'f', -1, 64)
	if value > 0 {
		text = "+" + text
	}
	return text
}

func (d *deps) recentAlerts(ctx context.Context, in alertHistoryInput) (AlertsOutput, string, *ErrorInfo) {
	if !d.historyAvailable() {
		return AlertsOutput{}, "", &errHistoryUnavailable
	}
	w, failure := d.window(in.timeRange)
	if failure != nil {
		return AlertsOutput{}, "", failure
	}
	rows, err := d.history.RecentAlerts(ctx, store.AlertQuery{ProjectID: in.ProjectID, AlertType: in.AlertType, Days: w.days, Limit: historyReadLimit})
	if err != nil {
		return AlertsOutput{}, "", d.historyFailed("recent_alerts", err)
	}
	providerOf := d.providerOf()
	entries := make([]entry[AlertView], 0, len(rows))
	for _, row := range rows {
		entries = append(entries, newEntry(row.Timestamp, rowID("alert", row.ID), alertView(row, providerOf)))
	}
	p := paginate(entries, w, bounded(in.Limit, 50, 1, 100), len(rows) >= historyReadLimit)
	out := AlertsOutput{Meta: d.historyMeta(p.truncated), Count: len(p.items), Alerts: p.items, NextCursor: optional(p.next)}
	return out, pageSummary(out.Count, "alert", "alerts", p), nil
}

func alertView(row store.AlertRow, providerOf func(string) *string) AlertView {
	return AlertView{
		ID: row.ID, Type: row.AlertType, Status: row.Status, ProjectID: row.ProjectID, ProjectName: row.ProjectName,
		Provider: providerOf(row.ProjectID), Message: row.Message, Value: row.BalanceValue, Threshold: row.ThresholdValue,
		Timestamp: row.Timestamp,
	}
}

func (d *deps) recentEmailAlerts(ctx context.Context, in emailAlertHistoryInput) (EmailAlertsOutput, string, *ErrorInfo) {
	if !d.historyAvailable() {
		return EmailAlertsOutput{}, "", &errHistoryUnavailable
	}
	w, failure := d.window(in.timeRange)
	if failure != nil {
		return EmailAlertsOutput{}, "", failure
	}
	rows, err := d.history.EmailAlerts(ctx, store.EmailAlertQuery{Mailbox: in.Mailbox, Days: w.days, Limit: historyReadLimit})
	if err != nil {
		return EmailAlertsOutput{}, "", d.historyFailed("recent_email_alerts", err)
	}
	entries := make([]entry[EmailAlertRecordView], 0, len(rows))
	for _, row := range rows {
		entries = append(entries, newEntry(row.Timestamp, rowID("email", row.ID), EmailAlertRecordView{
			ID: row.ID, Mailbox: row.Mailbox, Sender: row.Sender, Subject: row.Subject, Date: row.Date,
			ServiceName: row.ServiceName, Amount: row.Amount, Keywords: nonNil(row.MatchedKeywords),
			AlertSent: row.AlertSent, Timestamp: row.Timestamp,
		}))
	}
	p := paginate(entries, w, bounded(in.Limit, 50, 1, 100), len(rows) >= historyReadLimit)
	out := EmailAlertsOutput{Meta: d.historyMeta(p.truncated), Count: len(p.items), EmailAlerts: p.items, NextCursor: optional(p.next)}
	return out, pageSummary(out.Count, "email alert", "email alerts", p), nil
}

func (d *deps) events(ctx context.Context, in eventsInput) (EventsOutput, string, *ErrorInfo) {
	if !d.historyAvailable() {
		return EventsOutput{}, "", &errHistoryUnavailable
	}
	w, failure := d.window(in.timeRange)
	if failure != nil {
		return EventsOutput{}, "", failure
	}
	var entries []entry[EventView]
	capped := false
	if in.Type != "email_alert" {
		rows, err := d.history.RecentAlerts(ctx, store.AlertQuery{AlertType: in.Type, Days: w.days, Limit: historyReadLimit})
		if err != nil {
			return EventsOutput{}, "", d.historyFailed("events", err)
		}
		capped = len(rows) >= historyReadLimit
		providerOf := d.providerOf()
		for _, row := range rows {
			source := row.ProjectName
			if source == "" {
				source = row.ProjectID
			}
			entries = append(entries, newEntry(row.Timestamp, rowID("alert", row.ID), EventView{
				ID: fmt.Sprintf("alert:%d", row.ID), Type: row.AlertType, Status: row.Status, ProjectID: optional(row.ProjectID),
				Provider: providerOf(row.ProjectID), Source: source, Message: row.Message, Timestamp: row.Timestamp,
			}))
		}
	}
	if (in.Type == "" || in.Type == "email_alert") && d.caller.Allows(config.ScopeEmail) {
		rows, err := d.history.EmailAlerts(ctx, store.EmailAlertQuery{Days: w.days, Limit: historyReadLimit})
		if err != nil {
			return EventsOutput{}, "", d.historyFailed("events", err)
		}
		capped = capped || len(rows) >= historyReadLimit
		for _, row := range rows {
			status := "sent"
			if !row.AlertSent {
				status = "not_sent"
			}
			entries = append(entries, newEntry(row.Timestamp, rowID("email", row.ID), EventView{
				ID: fmt.Sprintf("email:%d", row.ID), Type: "email_alert", Status: status,
				Source: row.Mailbox, Message: row.Subject, Timestamp: row.Timestamp,
			}))
		}
	}
	p := paginate(entries, w, bounded(in.Limit, 100, 1, 500), capped)
	out := EventsOutput{Meta: d.historyMeta(p.truncated), Count: len(p.items), Events: p.items, NextCursor: optional(p.next)}
	return out, pageSummary(out.Count, "event", "events", p), nil
}

func (d *deps) alertStats(ctx context.Context, in statsInput) (AlertStatsOutput, string, *ErrorInfo) {
	if !d.historyAvailable() {
		return AlertStatsOutput{}, "", &errHistoryUnavailable
	}
	days := bounded(in.Days, 30, 1, 365)
	stats, err := d.history.AlertStats(ctx, days)
	if err != nil {
		return AlertStatsOutput{}, "", d.historyFailed("alert_stats", err)
	}
	out := AlertStatsOutput{Meta: d.meta("persisted_history", nil), Days: days, ByType: map[string]int{}, TopProjects: []TopProjectView{}}
	if stats != nil {
		out.TotalAlerts = stats.TotalAlerts
		for kind, n := range stats.ByType {
			out.ByType[kind] = n
		}
		for _, p := range stats.TopProjects {
			out.TopProjects = append(out.TopProjects, TopProjectView{ProjectName: p.Project, Alerts: p.Count})
		}
	}
	return out, fmt.Sprintf("%s in %s", model.Quantity(out.TotalAlerts, "alert", "alerts"), model.Quantity(days, "day", "days")), nil
}

// ==================== Providers, spending and renewals ====================

func (d *deps) providerStatus(ctx context.Context, _ emptyInput) (ProviderStatusOutput, string, *ErrorInfo) {
	// keyed by both the canonical custom: key and the bare alias, so
	// adapter-era provider values keep resolving
	names := map[string]string{}
	for _, p := range provider.All() {
		names[p.Key] = p.Name
		if bare := provider.EnvPrefixOf(p.Key); bare != p.Key {
			names[bare] = p.Name
		}
	}
	var views []*ProviderStatusView
	byKey := map[string]*ProviderStatusView{}
	latency := map[string][2]int64{} // sum, count
	for _, r := range d.runtime.Balance().Projects {
		v := byKey[r.Provider]
		if v == nil {
			v = &ProviderStatusView{Provider: r.Provider, Name: names[r.Provider], Errors: []ProviderErrorView{}}
			byKey[r.Provider] = v
			views = append(views, v)
		}
		v.Projects++
		if r.CheckedAt != nil && (v.LastCheckedAt == nil || *r.CheckedAt > *v.LastCheckedAt) {
			v.LastCheckedAt = r.CheckedAt
		}
		if r.Cached {
			v.Cached++
		}
		if r.LatencyMs != nil {
			l := latency[r.Provider]
			latency[r.Provider] = [2]int64{l[0] + *r.LatencyMs, l[1] + 1}
			if v.MaxLatencyMs == nil || *r.LatencyMs > *v.MaxLatencyMs {
				v.MaxLatencyMs = r.LatencyMs
			}
		}
		if !r.Success {
			v.Failing++
			id := model.ProjectID(r.Provider, r.Project)
			v.Errors = append(v.Errors, ProviderErrorView{ProjectID: id, ProjectName: r.Project, Error: *d.checkError(r), LastSuccessAt: d.newestSnapshot(ctx, store.BalanceQuery{ProjectID: id})})
		}
	}
	rank := map[string]int{"down": 0, "degraded": 1, "ok": 2}
	out := ProviderStatusOutput{Meta: d.meta("live_state", d.runtime.Balance().LastUpdate), Providers: make([]ProviderStatusView, 0, len(views))}
	for _, v := range views {
		switch {
		case v.Failing == v.Projects:
			v.Status = "down"
		case v.Failing > 0:
			v.Status = "degraded"
		default:
			v.Status = "ok"
		}
		if l := latency[v.Provider]; l[1] > 0 {
			avg := l[0] / l[1]
			v.AvgLatencyMs = &avg
		}
		v.LastSuccessAt = d.newestSnapshot(ctx, store.BalanceQuery{Provider: v.Provider})
		out.Providers = append(out.Providers, *v)
	}
	sort.SliceStable(out.Providers, func(i, j int) bool {
		a, b := out.Providers[i], out.Providers[j]
		if rank[a.Status] != rank[b.Status] {
			return rank[a.Status] < rank[b.Status]
		}
		return a.Provider < b.Provider
	})
	out.Count = len(out.Providers)
	failing := 0
	for _, v := range out.Providers {
		if v.Status != "ok" {
			failing++
		}
	}
	return out, fmt.Sprintf("%s, %d with failing checks", model.Quantity(out.Count, "provider", "providers"), failing), nil
}

// newestSnapshot is the timestamp of the newest balance snapshot matching q in the last
// year, or nil without the database or a snapshot. Snapshots are only stored for checks
// that read a balance, so this is the last success.
func (d *deps) newestSnapshot(ctx context.Context, q store.BalanceQuery) *string {
	if !d.historyAvailable() {
		return nil
	}
	q.Days, q.Limit = 365, 1
	rows, err := d.history.BalanceHistory(ctx, q)
	if err != nil || len(rows) == 0 {
		return nil
	}
	return &rows[0].Timestamp
}

func (d *deps) spendSummary(ctx context.Context, in spendInput) (SpendSummaryOutput, string, *ErrorInfo) {
	if !d.historyAvailable() {
		return SpendSummaryOutput{}, "", &errHistoryUnavailable
	}
	days := bounded(in.Days, max(1, d.settings.BurnRateWindowDays), 1, 90)
	points, err := d.history.BalanceSeries(ctx, days)
	if err != nil {
		return SpendSummaryOutput{}, "", d.historyFailed("spend_summary", err)
	}
	var order []string
	series := map[string]*spendSeries{}
	excluded := map[string]bool{}
	for _, p := range points {
		if (in.Provider != "" && p.Provider != in.Provider) || (in.ProjectID != "" && p.ProjectID != in.ProjectID) || p.Timestamp == 0 {
			continue
		}
		if p.BalanceType == model.TypeQuota {
			excluded[p.ProjectName] = true
			continue
		}
		s := series[p.ProjectID]
		if s == nil {
			s = &spendSeries{projectID: p.ProjectID}
			series[p.ProjectID] = s
			order = append(order, p.ProjectID)
		}
		// The newest snapshot names the project.
		s.projectName, s.provider, s.balanceType = p.ProjectName, p.Provider, p.BalanceType
		s.samples = append(s.samples, sample{at: time.Unix(p.Timestamp, 0), balance: p.Balance})
	}
	out := SpendSummaryOutput{Meta: d.meta("persisted_history", nil), Days: days, Projects: make([]SpendProjectView, 0, len(order)), Excluded: []string{}}
	now := d.now()
	for _, id := range order {
		s := series[id]
		sort.SliceStable(s.samples, func(i, j int) bool { return s.samples[i].at.Before(s.samples[j].at) })
		out.Projects = append(out.Projects, spendView(*s, now, time.Local))
	}
	sort.SliceStable(out.Projects, func(i, j int) bool { return out.Projects[i].Consumed > out.Projects[j].Consumed })
	out.Totals = spendTotals(out.Projects)
	for name := range excluded {
		out.Excluded = append(out.Excluded, name)
	}
	sort.Strings(out.Excluded)
	summary := model.Quantity(len(out.Projects), "project", "projects") + " over " + model.Quantity(days, "day", "days")
	if len(out.Projects) > 0 {
		top := out.Projects[0]
		summary += fmt.Sprintf("; most spent: %s, %s", top.ProjectName, strconv.FormatFloat(top.Consumed, 'f', -1, 64))
	}
	return out, summary, nil
}

func (d *deps) upcomingRenewals(in upcomingInput) UpcomingRenewalsOutput {
	days := bounded(in.Days, 30, 1, 366)
	status := d.subscriptionStatus()
	out := UpcomingRenewalsOutput{Meta: status.Meta, Days: days, Renewals: []SubscriptionView{}}
	for _, s := range status.Subscriptions {
		if s.DaysUntilRenewal <= days && (in.IncludeRenewed || !s.AlreadyRenewed) {
			out.Renewals = append(out.Renewals, s)
		}
	}
	sort.SliceStable(out.Renewals, func(i, j int) bool {
		a, b := out.Renewals[i], out.Renewals[j]
		if a.DaysUntilRenewal != b.DaysUntilRenewal {
			return a.DaysUntilRenewal < b.DaysUntilRenewal
		}
		return a.Name < b.Name
	})
	out.Count = len(out.Renewals)
	return out
}

// ==================== Dashboard ====================

// dashboard answers "does anything need me?" in one call: what needs attention, most
// urgent first, and a short overview of each area the key may see.

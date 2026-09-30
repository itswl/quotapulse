// Package mcpserver exposes the read-only quotapulse state through MCP.
//
// It is deliberately attached to the existing authenticated HTTP service. The
// MCP server therefore observes the same in-memory state as the dashboard and
// does not create a second scheduler, database writer, or credential path.
//
// The httpapi middleware attaches each request's caller: an MCP key with its scopes, or
// WEB_API_KEY with all of them. A request's server lists and serves only what those
// scopes allow. Every tool is read-only and returns structured content with an output
// schema; failures are isError results that carry an ErrorOutput as JSON text.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/state"
	"github.com/itswl/quotapulse/internal/store"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	stateResourceTemplate   = "quotapulse://state/{kind}"
	projectResourceTemplate = "quotapulse://projects/{project_id}/status"
)

const instructions = "Read-only access to quotapulse: API balances and runways, subscription renewals, " +
	"mailbox scans, alert history, jobs and health. Start with dashboard_summary, which lists what needs " +
	"attention. Identify projects by project_id. Every result has a meta block that says how fresh its " +
	"data is. Amounts are in each provider's own unit and currency, so never add them up across " +
	"providers; capabilities explains the units and conventions. History lists are newest first and " +
	"page with next_cursor."

type emptyInput struct{}

type balanceStatusInput struct {
	Project         string   `json:"project,omitempty" jsonschema:"optional project name filter, case-insensitive substring"`
	ProjectIDs      []string `json:"project_ids,omitempty" jsonschema:"only these project IDs"`
	ProjectNames    []string `json:"project_names,omitempty" jsonschema:"only these project names, exactly"`
	Provider        string   `json:"provider,omitempty" jsonschema:"optional provider filter"`
	OwnerProject    string   `json:"owner_project,omitempty" jsonschema:"only projects in this owner_project group"`
	Status          string   `json:"status,omitempty" jsonschema:"only projects in this state"`
	IncludeDisabled bool     `json:"include_disabled,omitempty" jsonschema:"also list configured projects that are disabled, and so not checked"`
}

type spendInput struct {
	Days      int    `json:"days,omitempty" jsonschema:"how many days back, 1-90 (default BURN_RATE_WINDOW_DAYS, the runway window)"`
	Provider  string `json:"provider,omitempty" jsonschema:"optional provider filter"`
	ProjectID string `json:"project_id,omitempty" jsonschema:"optional stable project ID filter"`
}

type upcomingInput struct {
	Days           int  `json:"days,omitempty" jsonschema:"how many days ahead, 1-366 (default 30)"`
	IncludeRenewed bool `json:"include_renewed,omitempty" jsonschema:"also list renewals already marked as paid"`
}

type balanceHistoryInput struct {
	timeRange
	Limit     int    `json:"limit,omitempty" jsonschema:"rows per page, 1-100 (default 100)"`
	ProjectID string `json:"project_id,omitempty" jsonschema:"optional stable project ID filter"`
	Provider  string `json:"provider,omitempty" jsonschema:"optional provider filter"`
}

type alertHistoryInput struct {
	timeRange
	Limit     int    `json:"limit,omitempty" jsonschema:"rows per page, 1-100 (default 50)"`
	ProjectID string `json:"project_id,omitempty" jsonschema:"optional stable project ID filter"`
	AlertType string `json:"alert_type,omitempty" jsonschema:"optional alert type filter"`
}

type emailAlertHistoryInput struct {
	timeRange
	Limit   int    `json:"limit,omitempty" jsonschema:"rows per page, 1-100 (default 50)"`
	Mailbox string `json:"mailbox,omitempty" jsonschema:"optional mailbox name filter"`
}

type eventsInput struct {
	timeRange
	Limit int    `json:"limit,omitempty" jsonschema:"events per page, 1-500 (default 100)"`
	Type  string `json:"type,omitempty" jsonschema:"optional event type filter"`
}

type statsInput struct {
	Days int `json:"days,omitempty" jsonschema:"number of days to search, between 1 and 365"`
}

type trendInput struct {
	ProjectID string `json:"project_id" jsonschema:"stable project ID from balance_status"`
	Days      int    `json:"days,omitempty" jsonschema:"how many days back, 1-365 (default 30)"`
	Interval  string `json:"interval,omitempty" jsonschema:"raw for every snapshot, or one point per hour, day (the default) or week"`
	Limit     int    `json:"limit,omitempty" jsonschema:"points per page, 1-1000 (default 500)"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page, to continue there"`
}

// Alert and event types, for the enum filters.
var (
	alertTypes = []string{"low_balance", "check_failed", "low_runway", "spend_spike", "subscription_renewal"}
	eventTypes = append(slices.Clone(alertTypes), "email_alert")
	statuses   = []string{"healthy", "below_threshold", "check_failed", "disabled"}
)

// deps is what one request's tools read. history and resolver may be nil: the history
// tools then answer "unavailable" and the configuration views are empty.
type deps struct {
	settings *config.Settings
	runtime  *state.Manager
	history  store.Store
	resolver *config.Resolver
	log      *slog.Logger
	caller   config.MCPCaller
	now      func() time.Time
}

// NewHandler returns a stateless Streamable HTTP MCP handler. Authentication is
// applied by the surrounding httpapi middleware, just like /api/*.
func NewHandler(settings *config.Settings, runtime *state.Manager, history store.Store, resolver *config.Resolver, log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	serverFactory := func(r *http.Request) *sdkmcp.Server {
		caller, ok := config.MCPCallerFrom(r.Context())
		if !ok {
			// Only reachable when the handler is mounted without the key middleware.
			caller = config.MCPCaller{Name: "anonymous", Scopes: map[string]bool{}}
		}
		return newServer(&deps{
			settings: settings, runtime: runtime, history: history, resolver: resolver,
			log: log, caller: caller, now: time.Now,
		})
	}
	return sdkmcp.NewStreamableHTTPHandler(serverFactory, &sdkmcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		Logger:       log,
	})
}

func newServer(d *deps) *sdkmcp.Server {
	server := sdkmcp.NewServer(
		&sdkmcp.Implementation{Name: "quotapulse", Version: d.settings.AppVersion},
		&sdkmcp.ServerOptions{
			Instructions: instructions,
			Logger:       d.log,
			SchemaCache:  schemaCache,
			// Every result belongs to the caller's account, the lists included: what a
			// key may see depends on its scopes.
			SetCacheable: func(_ context.Context, _ sdkmcp.Request, c *sdkmcp.Cacheable) {
				c.CacheScope = "private"
				c.TTLMs = 0
			},
			// The lists are fixed for a request, and a stateless server never sends
			// list_changed notifications.
			Capabilities: &sdkmcp.ServerCapabilities{
				Logging:   &sdkmcp.LoggingCapabilities{},
				Tools:     &sdkmcp.ToolCapabilities{ListChanged: false},
				Resources: &sdkmcp.ResourceCapabilities{ListChanged: false},
			},
		},
	)
	d.addTools(server)
	d.addResources(server)
	return server
}

// ==================== Tool plumbing ====================

// toolSpec describes one tool. It is registered only when the caller has every one of
// its scopes; a tool without scopes carries no account data and is always there.
type toolSpec struct {
	name, title, description string
	scopes                   []string
	enums                    map[string][]string
}

func (d *deps) allows(scopes ...string) bool {
	for _, scope := range scopes {
		if !d.caller.Allows(scope) {
			return false
		}
	}
	return true
}

// Schemas are inferred once per tool and shared by every per-request server; the SDK's
// schema cache then resolves each of them once as well.
var (
	schemaCache = sdkmcp.NewSchemaCache()
	toolSchemas sync.Map // tool name → *toolSchema
)

type toolSchema struct{ input, output *jsonschema.Schema }

func schemasFor[In, Out any](spec toolSpec) *toolSchema {
	if cached, ok := toolSchemas.Load(spec.name); ok {
		return cached.(*toolSchema)
	}
	schemas := &toolSchema{input: withEnums(schemaFor[In](), spec.enums), output: schemaFor[Out]()}
	actual, _ := toolSchemas.LoadOrStore(spec.name, schemas)
	return actual.(*toolSchema)
}

// addTool registers a read-only tool with explicit input and output schemas. A result
// carries the structured output, and as text a one-line summary plus the same JSON for
// clients that ignore structured content. A failure is an isError result whose text is
// the message plus an ErrorOutput as JSON; it has no structured content, because that
// would have to match the output schema.
func addTool[In, Out any](d *deps, server *sdkmcp.Server, spec toolSpec, handle func(context.Context, In) (Out, string, *ErrorInfo)) {
	if !d.allows(spec.scopes...) {
		return
	}
	schemas := schemasFor[In, Out](spec)
	tool := &sdkmcp.Tool{
		Name: spec.name, Title: spec.title, Description: spec.description,
		Annotations: &sdkmcp.ToolAnnotations{
			Title: spec.title, ReadOnlyHint: true, IdempotentHint: true,
			DestructiveHint: new(bool), OpenWorldHint: new(bool),
		},
		InputSchema:  schemas.input,
		OutputSchema: schemas.output,
	}
	sdkmcp.AddTool(server, tool, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in In) (*sdkmcp.CallToolResult, any, error) {
		started := time.Now()
		out, summary, failure := handle(ctx, in)
		var body []byte
		if failure == nil {
			var err error
			if body, err = json.Marshal(out); err != nil {
				failure = &ErrorInfo{Category: "internal", Message: "The result could not be encoded"}
			}
		}
		d.logCall(spec.name, started, failure)
		if failure != nil {
			return errorResult(*failure), nil, nil
		}
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: summary},
			&sdkmcp.TextContent{Text: string(body)},
		}}, out, nil
	})
}

func errorResult(info ErrorInfo) *sdkmcp.CallToolResult {
	body, _ := json.Marshal(ErrorOutput{Error: info})
	return &sdkmcp.CallToolResult{IsError: true, Content: []sdkmcp.Content{
		&sdkmcp.TextContent{Text: info.Message},
		&sdkmcp.TextContent{Text: string(body)},
	}}
}

// logCall is the per-key access log: which key called which tool, how long it took and
// whether it failed. Arguments and results stay out of the log.
func (d *deps) logCall(tool string, started time.Time, failure *ErrorInfo) {
	attrs := []any{"caller", d.caller.Name, "tool", tool, "duration_ms", time.Since(started).Milliseconds(), "ok", failure == nil}
	if failure != nil {
		attrs = append(attrs, "error_category", failure.Category)
	}
	d.log.Info("MCP tool call", attrs...)
}

// schemaFor infers a JSON schema from a Go type. A type that can't be described is a
// programming error, which the tests catch.
func schemaFor[T any]() *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("mcpserver: no JSON schema for %T: %v", *new(T), err))
	}
	return schema
}

// withEnums restricts string properties to fixed values, so an agent picks a valid
// provider or alert type instead of guessing a spelling.
func withEnums(schema *jsonschema.Schema, enums map[string][]string) *jsonschema.Schema {
	for name, values := range enums {
		property := schema.Properties[name]
		if property == nil {
			panic("mcpserver: enum for unknown property " + name)
		}
		if property.Items != nil { // a list of values: restrict each one
			property = property.Items
		}
		property.Enum = make([]any, len(values))
		for i, value := range values {
			property.Enum[i] = value
		}
	}
	return schema
}

// ==================== Meta, errors and small helpers ====================

// meta describes a result: when it was made, how old its data is, and whether the live
// state has gone stale.
func (d *deps) meta(source string, updatedAt *string) Meta {
	now := d.now().UTC()
	m := Meta{AsOf: now.Format(time.RFC3339), Timezone: timezoneName(), Source: source}
	if updatedAt != nil {
		if at, err := time.Parse(time.RFC3339Nano, *updatedAt); err == nil {
			age := max(0, int(now.Sub(at).Seconds()))
			m.DataUpdatedAt, m.DataAgeSeconds = updatedAt, &age
		}
	}
	m.IsStale = isStale(d.runtime.Balance().LastUpdate, d.settings.RefreshInterval(), now)
	return m
}

// timezoneName is the server's IANA zone (from TZ), or its UTC offset when unnamed.
func timezoneName() string {
	if name := time.Local.String(); name != "Local" {
		return name
	}
	return "UTC" + time.Now().Format("-07:00")
}

func isStale(lastUpdate *string, refreshSeconds int, now time.Time) bool {
	if lastUpdate == nil || refreshSeconds <= 0 {
		return false
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, *lastUpdate)
	return err == nil && now.Sub(updatedAt) > 3*time.Duration(refreshSeconds)*time.Second
}

func (d *deps) historyAvailable() bool { return d.history != nil && d.history.Enabled() }

var errHistoryUnavailable = ErrorInfo{
	Category: "unavailable",
	Message:  "History needs the database; set ENABLE_DATABASE=true",
}

// historyFailed keeps the storage error out of the result; it goes to the server log.
func (d *deps) historyFailed(tool string, err error) *ErrorInfo {
	d.log.Warn("MCP history query failed", "tool", tool, "error", err)
	retry := 60
	return &ErrorInfo{Category: "unavailable", Message: "History storage is unavailable right now", Retryable: true, RetryAfterSeconds: &retry}
}

func invalidArgument(message string) *ErrorInfo {
	return &ErrorInfo{Category: "invalid_argument", Message: message}
}

// secondsUntilNextCheck is when a failed check is tried again: the next balance refresh.
func (d *deps) secondsUntilNextCheck() *int {
	for _, job := range d.runtime.Jobs().Jobs {
		if job.Name != "dashboard_refresh" || job.NextRun == nil {
			continue
		}
		if at, err := time.Parse(time.RFC3339Nano, *job.NextRun); err == nil {
			seconds := max(0, int(at.Sub(d.now()).Seconds()))
			return &seconds
		}
	}
	return nil
}

func bounded(value, fallback, min, max int) int {
	if value == 0 {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func optional(text string) *string {
	if text == "" {
		return nil
	}
	return &text
}

func nonZero(value float64) *float64 {
	if value == 0 {
		return nil
	}
	return &value
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

func (d *deps) config(ctx context.Context) model.Config {
	if d.resolver == nil {
		return model.Config{}
	}
	return d.resolver.Load(ctx)
}

// ==================== Live state ====================

// balanceStatus is every monitored project that passes the filter; the zero filter
// keeps them all.
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

func (d *deps) addTools(server *sdkmcp.Server) {
	balance, alerts := []string{config.ScopeBalance}, []string{config.ScopeAlerts}
	subscriptions, email := []string{config.ScopeSubscriptions}, []string{config.ScopeEmail}
	providers := provider.Keys()

	addTool(d, server, toolSpec{
		name: "dashboard_summary", title: "What needs attention",
		description: "Start here: what needs attention now (failed checks, low balances, short runways, renewals due, alert emails, failing jobs), critical first, with an overview of balances, subscriptions, email and the last week's alerts, as far as this key's scopes allow.",
	}, func(ctx context.Context, _ emptyInput) (DashboardSummaryOutput, string, *ErrorInfo) {
		return d.dashboard(ctx)
	})

	addTool(d, server, toolSpec{
		name: "health", title: "Service health",
		description: "Readiness summary: data freshness, job health and version.",
	}, func(ctx context.Context, _ emptyInput) (HealthOutput, string, *ErrorInfo) {
		out := d.health(ctx)
		return out, fmt.Sprintf("%s, %s", out.Status, model.Quantity(len(out.FailedJobs), "failing job", "failing jobs")), nil
	})

	addTool(d, server, toolSpec{
		name: "capabilities", title: "Capabilities and conventions",
		description: "Feature flags, configuration counts, this key's scopes, and the units and conventions results use.",
	}, func(ctx context.Context, _ emptyInput) (CapabilitiesOutput, string, *ErrorInfo) {
		out := d.capabilities(ctx)
		return out, fmt.Sprintf("version %s, scopes: %s", out.Version, strings.Join(out.Scopes, ", ")), nil
	})

	addTool(d, server, toolSpec{
		name: "providers", title: "Provider catalog",
		description: "Supported balance providers with their identifiers, default balance types and units.",
	}, func(context.Context, emptyInput) (ProvidersOutput, string, *ErrorInfo) {
		all := provider.All()
		list := make([]ProviderInfo, 0, len(all))
		for _, p := range all {
			list = append(list, ProviderInfo{Provider: p.Key, Name: p.Name, DefaultType: p.DefaultType, Unit: unitOf(p.DefaultType)})
		}
		return ProvidersOutput{Meta: d.meta("catalog", nil), Count: len(list), Providers: list}, model.Quantity(len(list), "provider", "providers"), nil
	})

	addTool(d, server, toolSpec{
		name: "job_status", title: "Scheduled jobs",
		description: "Scheduler status: last and next runs and failures, with a status that says when a job is inactive or has not run yet.",
	}, func(ctx context.Context, _ emptyInput) (JobStatusOutput, string, *ErrorInfo) {
		out := d.jobStatus(ctx)
		health := "healthy"
		if !out.Healthy {
			health = "not healthy"
		}
		return out, fmt.Sprintf("%s, %s", model.Quantity(len(out.Jobs), "job", "jobs"), health), nil
	})

	addTool(d, server, toolSpec{
		name: "balance_status", title: "Balances and runways", scopes: balance,
		enums:       map[string][]string{"provider": providers, "status": statuses},
		description: "Current balance, threshold status, runway estimate and last check of every monitored project, optionally filtered by project, provider, owner_project or status.",
	}, func(ctx context.Context, in balanceStatusInput) (BalanceStatusOutput, string, *ErrorInfo) {
		out := d.balanceStatus(ctx, in)
		c := out.Counts
		summary := fmt.Sprintf("%s: %d healthy, %d below threshold, %d failing their check",
			model.Quantity(c.Total, "project", "projects"), c.Healthy, c.BelowThreshold, c.CheckFailed)
		if c.Disabled > 0 {
			summary += fmt.Sprintf(", %d disabled", c.Disabled)
		}
		return out, summary, nil
	})

	addTool(d, server, toolSpec{
		name: "provider_status", title: "Provider health", scopes: balance,
		description: "Per provider: how many of its projects fail their check and why, when it last answered, its latency, and its newest snapshot in the history.",
	}, d.providerStatus)

	addTool(d, server, toolSpec{
		name: "spend_summary", title: "Spending", scopes: balance,
		enums:       map[string][]string{"provider": providers},
		description: "Spending and top-ups per project over a number of days, with a daily breakdown and today's spike ratio, plus totals per provider. Amounts of different providers are never added up.",
	}, d.spendSummary)

	addTool(d, server, toolSpec{
		name: "upcoming_renewals", title: "Upcoming renewals", scopes: subscriptions,
		description: "Subscriptions renewing in the next days, soonest first, with their reminder state; paid ones only on request.",
	}, func(_ context.Context, in upcomingInput) (UpcomingRenewalsOutput, string, *ErrorInfo) {
		out := d.upcomingRenewals(in)
		summary := model.Quantity(out.Count, "renewal", "renewals") + " in the next " + model.Quantity(out.Days, "day", "days")
		if out.Count > 0 {
			next := out.Renewals[0]
			summary += fmt.Sprintf("; next: %s, %s", next.Name, model.RenewsIn(next.DaysUntilRenewal))
		}
		return out, summary, nil
	})

	addTool(d, server, toolSpec{
		name: "subscription_status", title: "Subscription renewals", scopes: subscriptions,
		description: "Every subscription's next renewal and reminder state.",
	}, func(context.Context, emptyInput) (SubscriptionStatusOutput, string, *ErrorInfo) {
		out := d.subscriptionStatus()
		return out, fmt.Sprintf("%s, %d due for a reminder", model.Quantity(out.Counts.Total, "subscription", "subscriptions"), out.Counts.Due), nil
	})

	addTool(d, server, toolSpec{
		name: "email_scan_status", title: "Mailbox scan", scopes: email,
		description: "The latest mailbox scan: per-mailbox results and the alert emails it found. Mailbox addresses are masked.",
	}, func(ctx context.Context, _ emptyInput) (EmailScanOutput, string, *ErrorInfo) {
		out := d.emailScan(ctx)
		return out, fmt.Sprintf("%s, %s in the last scan", out.Status, model.Quantity(len(out.Alerts), "alert email", "alert emails")), nil
	})

	addTool(d, server, toolSpec{
		name: "project_config", title: "Project configuration", scopes: []string{config.ScopeConfig, config.ScopeBalance},
		description: "Configured projects. API keys are never returned, only whether one is set.",
	}, func(ctx context.Context, _ emptyInput) (ProjectConfigOutput, string, *ErrorInfo) {
		cfg := d.config(ctx)
		views := make([]ProjectConfigView, 0, len(cfg.Projects))
		for _, p := range cfg.Projects {
			views = append(views, ProjectConfigView{
				ProjectID: p.ID(), ProjectName: p.Name, Provider: p.Provider, BalanceType: p.Type,
				Threshold: p.Threshold, OwnerProject: p.OwnerProject, Enabled: p.Enabled,
				APIKeyConfigured: p.APIKey != "", FromEnv: p.FromEnv,
			})
		}
		return ProjectConfigOutput{Meta: d.meta("configuration", nil), Count: len(views), Projects: views}, model.Quantity(len(views), "project", "projects"), nil
	})

	addTool(d, server, toolSpec{
		name: "mailbox_config", title: "Mailbox configuration", scopes: []string{config.ScopeConfig, config.ScopeEmail},
		description: "Configured mailboxes. Passwords are never returned, and addresses are masked.",
	}, func(ctx context.Context, _ emptyInput) (MailboxConfigOutput, string, *ErrorInfo) {
		cfg := d.config(ctx)
		views := make([]MailboxConfigView, 0, len(cfg.Mailboxes))
		for _, m := range cfg.Mailboxes {
			views = append(views, MailboxConfigView{
				Name: m.Name, Host: m.Host, Port: m.Port, Username: maskEmail(m.Username), UseSSL: m.UseSSL,
				Enabled: m.Enabled, PasswordConfigured: m.Password != "", FromEnv: m.FromEnv,
			})
		}
		return MailboxConfigOutput{Meta: d.meta("configuration", nil), Count: len(views), Mailboxes: views}, model.Quantity(len(views), "mailbox", "mailboxes"), nil
	})

	addTool(d, server, toolSpec{
		name: "subscription_config", title: "Subscription configuration", scopes: []string{config.ScopeConfig, config.ScopeSubscriptions},
		description: "Configured subscriptions. A subscription's own webhook URL is never returned, only whether it has one.",
	}, func(ctx context.Context, _ emptyInput) (SubscriptionConfigOutput, string, *ErrorInfo) {
		cfg := d.config(ctx)
		views := make([]SubscriptionConfigView, 0, len(cfg.Subscriptions))
		for _, s := range cfg.Subscriptions {
			views = append(views, subscriptionConfigView(s))
		}
		return SubscriptionConfigOutput{Meta: d.meta("configuration", nil), Count: len(views), Subscriptions: views}, model.Quantity(len(views), "subscription", "subscriptions"), nil
	})

	addTool(d, server, toolSpec{
		name: "balance_history", title: "Balance snapshots", scopes: balance,
		enums:       map[string][]string{"provider": providers},
		description: "Persisted balance snapshots, newest first, optionally for one project or provider.",
	}, d.balanceHistory)

	addTool(d, server, toolSpec{
		name: "balance_trend", title: "Balance trend", scopes: balance,
		enums:       map[string][]string{"interval": trendIntervals},
		description: "One project's balance over a number of days: every snapshot, or one point per hour, day or week, with a summary of the change, spending and top-ups.",
	}, d.trend)

	addTool(d, server, toolSpec{
		name: "recent_alerts", title: "Alert history", scopes: alerts,
		enums:       map[string][]string{"alert_type": alertTypes},
		description: "Persisted balance, check-failure, runway, spend-spike and subscription alerts, newest first.",
	}, d.recentAlerts)

	addTool(d, server, toolSpec{
		name: "recent_email_alerts", title: "Email alert history", scopes: email,
		description: "Persisted email alerts, newest first, optionally for one mailbox.",
	}, d.recentEmailAlerts)

	addTool(d, server, toolSpec{
		name: "events", title: "Event timeline", scopes: alerts,
		enums:       map[string][]string{"type": eventTypes},
		description: "One timeline of alerts, and email alerts when this key has the email scope, newest first.",
	}, d.events)

	addTool(d, server, toolSpec{
		name: "alert_stats", title: "Alert statistics", scopes: alerts,
		description: "Alert counts by type, and the projects that alerted most, over a number of days.",
	}, d.alertStats)
}

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
	names := map[string]string{}
	for _, p := range provider.All() {
		names[p.Key] = p.Name
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

func (d *deps) addResources(server *sdkmcp.Server) {
	d.addResource(server, "quotapulse://dashboard/summary", "dashboard-summary",
		"What needs attention now, as dashboard_summary returns it.", nil,
		func(ctx context.Context) (any, *ErrorInfo) { out, _, failure := d.dashboard(ctx); return out, failure })
	d.addResource(server, "quotapulse://alerts/recent", "recent-alerts",
		"The 50 newest alerts, as recent_alerts returns them.", []string{config.ScopeAlerts},
		func(ctx context.Context) (any, *ErrorInfo) {
			out, _, failure := d.recentAlerts(ctx, alertHistoryInput{})
			return out, failure
		})
	d.addResource(server, "quotapulse://subscriptions/upcoming", "upcoming-renewals",
		"Unpaid renewals in the next 30 days, as upcoming_renewals returns them.", []string{config.ScopeSubscriptions},
		func(context.Context) (any, *ErrorInfo) { return d.upcomingRenewals(upcomingInput{}), nil })

	if d.allows(config.ScopeBalance) {
		server.AddResourceTemplate(&sdkmcp.ResourceTemplate{
			Name:        "project-status",
			URITemplate: projectResourceTemplate,
			Description: "One project's current balance, runway and last check, by project_id.",
			MIMEType:    "application/json",
		}, func(ctx context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			id, ok := strings.CutPrefix(req.Params.URI, "quotapulse://projects/")
			id, ok2 := strings.CutSuffix(id, "/status")
			if !ok || !ok2 || id == "" || strings.Contains(id, "/") {
				return nil, sdkmcp.ResourceNotFoundError(req.Params.URI)
			}
			status := d.balanceStatus(ctx, balanceStatusInput{ProjectIDs: []string{id}})
			if len(status.Projects) == 0 {
				return nil, sdkmcp.ResourceNotFoundError(req.Params.URI)
			}
			return jsonResource(req.Params.URI, struct {
				Meta    Meta          `json:"meta"`
				Project ProjectStatus `json:"project"`
			}{status.Meta, status.Projects[0]})
		})
	}

	server.AddResourceTemplate(&sdkmcp.ResourceTemplate{
		Name:        "quotapulse-state",
		URITemplate: stateResourceTemplate,
		Description: "The current state as JSON: balance, subscriptions, email or jobs, as far as this key's scopes allow.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		kind, err := stateKind(req.Params.URI)
		if err != nil {
			return nil, err
		}
		var value any
		switch {
		case kind == "balance" && d.allows(config.ScopeBalance):
			value = d.balanceStatus(ctx, balanceStatusInput{})
		case kind == "subscriptions" && d.allows(config.ScopeSubscriptions):
			value = d.subscriptionStatus()
		case kind == "email" && d.allows(config.ScopeEmail):
			value = d.emailScan(ctx)
		case kind == "jobs":
			value = d.jobStatus(ctx)
		default:
			return nil, sdkmcp.ResourceNotFoundError(req.Params.URI)
		}
		return jsonResource(req.Params.URI, value)
	})
}

// addResource registers a fixed resource when the caller has its scopes. A read that
// fails, such as history without a database, is a resource error with the message.
func (d *deps) addResource(server *sdkmcp.Server, uri, name, description string, scopes []string, read func(context.Context) (any, *ErrorInfo)) {
	if !d.allows(scopes...) {
		return
	}
	server.AddResource(&sdkmcp.Resource{URI: uri, Name: name, Description: description, MIMEType: "application/json"},
		func(ctx context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			value, failure := read(ctx)
			if failure != nil {
				return nil, errors.New(failure.Message)
			}
			return jsonResource(req.Params.URI, value)
		})
}

func jsonResource(uri string, value any) (*sdkmcp.ReadResourceResult, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(body)}}}, nil
}

func stateKind(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "quotapulse" || u.Host != "state" {
		return "", fmt.Errorf("invalid state resource URI")
	}
	kind := strings.Trim(strings.TrimPrefix(u.Path, "/"), " ")
	if strings.Contains(kind, "/") || kind == "" {
		return "", fmt.Errorf("invalid state resource URI")
	}
	return kind, nil
}

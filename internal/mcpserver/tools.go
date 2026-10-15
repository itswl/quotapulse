package mcpserver

// // Tool registration: the spec table, the generic wiring and the schema helpers.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/provider"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

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

func (d *deps) addTools(server *sdkmcp.Server) {
	balance, alerts := []string{config.ScopeBalance}, []string{config.ScopeAlerts}
	subscriptions, email := []string{config.ScopeSubscriptions}, []string{config.ScopeEmail}
	// Agents speak bare platform names; the alias layer resolves them onto the
	// custom: definitions, so the enum lists the bare names only.
	providers := make([]string, 0, 8)
	for _, key := range provider.Keys() {
		providers = append(providers, provider.EnvPrefixOf(key))
	}

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

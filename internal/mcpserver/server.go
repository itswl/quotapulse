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
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/model"
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

// Package mcpserver exposes the read-only quotapulse state through MCP.
//
// It is deliberately attached to the existing authenticated HTTP service. The
// MCP server therefore observes the same in-memory state as the dashboard and
// does not create a second scheduler, database writer, or credential path.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/state"
	"github.com/itswl/quotapulse/internal/store"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const stateResourceTemplate = "quotapulse://state/{kind}"

type emptyInput struct{}

type balanceHistoryInput struct {
	Days      int    `json:"days,omitempty" jsonschema:"number of days to search, between 1 and 365"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum number of rows, between 1 and 100"`
	ProjectID string `json:"project_id,omitempty" jsonschema:"optional stable project ID filter"`
	Provider  string `json:"provider,omitempty" jsonschema:"optional provider filter"`
}

type alertHistoryInput struct {
	Days      int    `json:"days,omitempty" jsonschema:"number of days to search, between 1 and 365"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum number of rows, between 1 and 100"`
	ProjectID string `json:"project_id,omitempty" jsonschema:"optional stable project ID filter"`
	AlertType string `json:"alert_type,omitempty" jsonschema:"optional alert type filter"`
}

type emailAlertHistoryInput struct {
	Days    int    `json:"days,omitempty" jsonschema:"number of days to search, between 1 and 365"`
	Limit   int    `json:"limit,omitempty" jsonschema:"maximum number of rows, between 1 and 100"`
	Mailbox string `json:"mailbox,omitempty" jsonschema:"optional mailbox filter"`
}

type statsInput struct {
	Days int `json:"days,omitempty" jsonschema:"number of days to search, between 1 and 365"`
}

type trendInput struct {
	ProjectID string `json:"project_id" jsonschema:"stable project ID from balance_status"`
	Days      int    `json:"days,omitempty" jsonschema:"number of days to inspect, between 1 and 365"`
}

// NewHandler returns a stateless Streamable HTTP MCP handler. Authentication is
// applied by the surrounding httpapi middleware, just like /api/*.
func NewHandler(settings *config.Settings, runtime *state.Manager, history store.Store, resolver *config.Resolver, log *slog.Logger) http.Handler {
	serverFactory := func(_ *http.Request) *sdkmcp.Server {
		server := sdkmcp.NewServer(
			&sdkmcp.Implementation{Name: "quotapulse", Version: settings.AppVersion},
			&sdkmcp.ServerOptions{
				Instructions: "Read quotapulse status, subscriptions, email scan results, jobs, health, and alert history. This server is read-only.",
				Logger:       log,
			},
		)
		addStateTools(server, settings, runtime, history, resolver)
		addStateResources(server, runtime)
		return server
	}

	return sdkmcp.NewStreamableHTTPHandler(serverFactory, &sdkmcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		Logger:       log,
	})
}

func addStateTools(server *sdkmcp.Server, settings *config.Settings, runtime *state.Manager, history store.Store, resolver *config.Resolver) {
	addJSONTool(server, "balance_status", "Current balance and runway results for all monitored projects.", runtime.Balance)
	addJSONTool(server, "subscription_status", "Current subscription renewal status.", runtime.Subscriptions)
	addJSONTool(server, "email_scan_status", "Latest mailbox scan results and detected alert emails.", runtime.EmailScan)
	addJSONTool(server, "job_status", "Scheduler status, last runs, failures, and next runs.", runtime.Jobs)
	addJSONTool(server, "health", "Readiness-style health summary for the running service.", func() any {
		balance := runtime.Balance()
		jobs := runtime.Jobs()
		return healthSnapshot(settings, runtime, balance, jobs)
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "capabilities", Description: "Current feature flags and non-sensitive configuration counts.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ emptyInput) (*sdkmcp.CallToolResult, any, error) {
		return jsonResult(capabilitySnapshot(ctx, settings, resolver))
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "subscription_config", Description: "Read subscription configuration without modifying it.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ emptyInput) (*sdkmcp.CallToolResult, any, error) {
		cfg := resolver.Load(ctx)
		return jsonResult(map[string]any{"count": len(cfg.Subscriptions), "data": cfg.Subscriptions})
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "project_config", Description: "Read project configuration with API keys omitted.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ emptyInput) (*sdkmcp.CallToolResult, any, error) {
		cfg := resolver.Load(ctx)
		data := make([]projectConfigView, 0, len(cfg.Projects))
		for _, project := range cfg.Projects {
			data = append(data, projectConfigView{
				Name: project.Name, Provider: project.Provider, Type: project.Type,
				Threshold: project.Threshold, OwnerProject: project.OwnerProject,
				Enabled: project.Enabled, APIKeyConfigured: project.APIKey != "",
			})
		}
		return jsonResult(map[string]any{"count": len(data), "data": data})
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "mailbox_config", Description: "Read mailbox configuration with passwords omitted.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ emptyInput) (*sdkmcp.CallToolResult, any, error) {
		cfg := resolver.Load(ctx)
		data := make([]mailboxConfigView, 0, len(cfg.Mailboxes))
		for _, mailbox := range cfg.Mailboxes {
			data = append(data, mailboxConfigView{
				Name: mailbox.Name, Host: mailbox.Host, Port: mailbox.Port,
				Username: mailbox.Username, UseSSL: mailbox.UseSSL,
				Enabled: mailbox.Enabled, PasswordConfigured: mailbox.Password != "",
			})
		}
		return jsonResult(map[string]any{"count": len(data), "data": data})
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "balance_history",
		Description: "Read persisted balance snapshots, optionally filtered by stable project ID or provider.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in balanceHistoryInput) (*sdkmcp.CallToolResult, any, error) {
		rows, err := history.BalanceHistory(ctx, store.BalanceQuery{
			ProjectID: in.ProjectID,
			Provider:  in.Provider,
			Days:      bounded(in.Days, 30, 1, 365),
			Limit:     bounded(in.Limit, 100, 1, 100),
		})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"count": len(rows), "data": rows})
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "balance_trend",
		Description: "Read the persisted balance trend and runway inputs for one stable project ID.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in trendInput) (*sdkmcp.CallToolResult, any, error) {
		if strings.TrimSpace(in.ProjectID) == "" {
			return nil, nil, fmt.Errorf("project_id is required")
		}
		trend, err := history.BalanceTrend(ctx, in.ProjectID, bounded(in.Days, 30, 1, 365))
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"project_id": in.ProjectID, "data": trend})
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "recent_alerts",
		Description: "Read recent persisted balance, subscription, runway, and spend-spike alerts. Returns an empty list when database history is disabled.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in alertHistoryInput) (*sdkmcp.CallToolResult, any, error) {
		q := store.AlertQuery{
			ProjectID: in.ProjectID,
			AlertType: in.AlertType,
			Days:      bounded(in.Days, 30, 1, 365),
			Limit:     bounded(in.Limit, 50, 1, 100),
		}
		rows, err := history.RecentAlerts(ctx, q)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"count": len(rows), "data": rows})
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "recent_email_alerts",
		Description: "Read recent persisted email alert records. Returns an empty list when database history is disabled.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in emailAlertHistoryInput) (*sdkmcp.CallToolResult, any, error) {
		q := store.EmailAlertQuery{Mailbox: in.Mailbox, Days: bounded(in.Days, 30, 1, 365), Limit: bounded(in.Limit, 50, 1, 100)}
		rows, err := history.EmailAlerts(ctx, q)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"count": len(rows), "data": rows})
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "alert_stats", Description: "Read alert counts by type and project for a bounded time window.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in statsInput) (*sdkmcp.CallToolResult, any, error) {
		stats, err := history.AlertStats(ctx, bounded(in.Days, 30, 1, 365))
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"data": stats})
	})
}

type projectConfigView struct {
	Name             string  `json:"name"`
	Provider         string  `json:"provider"`
	Type             string  `json:"type"`
	Threshold        float64 `json:"threshold"`
	OwnerProject     *string `json:"owner_project"`
	Enabled          bool    `json:"enabled"`
	APIKeyConfigured bool    `json:"api_key_configured"`
}

type mailboxConfigView struct {
	Name               string `json:"name"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	UseSSL             bool   `json:"use_ssl"`
	Enabled            bool   `json:"enabled"`
	PasswordConfigured bool   `json:"password_configured"`
}

func healthSnapshot(settings *config.Settings, runtime *state.Manager, balance state.BalanceState, jobs state.JobState) map[string]any {
	lastUpdate := balance.LastUpdate
	stale := isStale(lastUpdate, settings.RefreshInterval())
	status := healthStatus(balance, jobs)
	if stale {
		status = "degraded"
	}
	return map[string]any{
		"status":         status,
		"version":        settings.AppVersion,
		"has_data":       len(balance.Projects) > 0,
		"is_stale":       stale,
		"last_update":    lastUpdate,
		"jobs_healthy":   jobs.Healthy,
		"failed_jobs":    runtime.FailedJobs(),
		"jobs":           jobs,
		"uptime_seconds": runtime.UptimeSeconds(),
	}
}

func capabilitySnapshot(ctx context.Context, settings *config.Settings, resolver *config.Resolver) map[string]any {
	features := map[string]bool{
		"database":       settings.EnableDatabase,
		"dynamic_config": settings.EnableDynamicConfig,
		"history":        settings.EnableHistoryAPI,
		"subscriptions":  settings.EnableSubscriptions,
		"mcp":            settings.EnableMCP,
		"prometheus":     settings.EnablePrometheus,
		"email_scan":     len(settings.EmailScanTimes) > 0,
	}
	counts := map[string]int{}
	if resolver != nil {
		cfg := resolver.Load(ctx)
		counts = map[string]int{
			"projects": len(cfg.Projects), "subscriptions": len(cfg.Subscriptions), "mailboxes": len(cfg.Mailboxes),
		}
		features["email_scan"] = features["email_scan"] && len(cfg.EnabledMailboxes()) > 0
	}
	return map[string]any{"version": settings.AppVersion, "features": features, "config_counts": counts}
}

func isStale(lastUpdate *string, refreshSeconds int) bool {
	if lastUpdate == nil || refreshSeconds <= 0 {
		return false
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, *lastUpdate)
	return err == nil && time.Since(updatedAt) > 3*time.Duration(refreshSeconds)*time.Second
}

func addJSONTool[T any](server *sdkmcp.Server, name, description string, read func() T) {
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: name, Description: description},
		func(context.Context, *sdkmcp.CallToolRequest, emptyInput) (*sdkmcp.CallToolResult, any, error) {
			return jsonResult(read())
		})
}

func addStateResources(server *sdkmcp.Server, runtime *state.Manager) {
	server.AddResourceTemplate(&sdkmcp.ResourceTemplate{
		Name:        "quotapulse-state",
		URITemplate: stateResourceTemplate,
		Description: "Read-only JSON resources for the current quotapulse runtime state.",
		MIMEType:    "application/json",
	}, func(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		kind, err := stateKind(req.Params.URI)
		if err != nil {
			return nil, err
		}
		var value any
		switch kind {
		case "balance":
			value = runtime.Balance()
		case "subscriptions":
			value = runtime.Subscriptions()
		case "email":
			value = runtime.EmailScan()
		case "jobs":
			value = runtime.Jobs()
		default:
			return nil, fmt.Errorf("unknown state resource %q", kind)
		}
		body, marshalErr := json.MarshalIndent(value, "", "  ")
		if marshalErr != nil {
			return nil, marshalErr
		}
		return &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{
			URI: req.Params.URI, MIMEType: "application/json", Text: string(body),
		}}}, nil
	})
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

func healthStatus(balance state.BalanceState, jobs state.JobState) string {
	if len(balance.Projects) == 0 || !jobs.Healthy {
		return "degraded"
	}
	return "healthy"
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

func jsonResult(value any) (*sdkmcp.CallToolResult, any, error) {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{
		&sdkmcp.TextContent{Text: string(body)},
	}}, nil, nil
}

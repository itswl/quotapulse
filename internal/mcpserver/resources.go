package mcpserver

// // MCP resources: scoped read-only JSON views over the live state.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/itswl/quotapulse/internal/config"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

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

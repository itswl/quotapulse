package config

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// MCP scopes. An MCP key sees the tools and resources of its scopes only; the health,
// capability and provider-catalog tools carry no account data and are always visible.
const (
	ScopeBalance       = "balance"       // balances, runways, trends, spending, provider status
	ScopeAlerts        = "alerts"        // alert history, events, alert statistics
	ScopeSubscriptions = "subscriptions" // renewal status and upcoming renewals
	ScopeEmail         = "email"         // mailbox scan results and email alert history
	ScopeConfig        = "config"        // the configuration views of the key's other scopes
)

// AllScopes lists every MCP scope; "all" in MCP_API_KEYS expands to it.
var AllScopes = []string{ScopeBalance, ScopeAlerts, ScopeSubscriptions, ScopeEmail, ScopeConfig}

// MCPKey is one read-only agent credential from MCP_API_KEYS. Unlike WEB_API_KEY it is
// only accepted on /mcp, so an agent holding it cannot reach the write endpoints.
type MCPKey struct {
	Name   string
	Key    string
	Scopes map[string]bool
	// Expires is the first instant the key is no longer valid; nil means no expiry.
	Expires *time.Time
}

// Expired reports whether the key has passed its expiry at now.
func (k MCPKey) Expired(now time.Time) bool { return k.Expires != nil && !now.Before(*k.Expires) }

// MCPCaller is who is calling /mcp, as established by the API key middleware.
type MCPCaller struct {
	// Name is the MCP key's name, or "web" for WEB_API_KEY.
	Name   string
	Scopes map[string]bool
}

// Allows reports whether the caller may read data of the given scope.
func (c MCPCaller) Allows(scope string) bool { return c.Scopes[scope] }

// FullMCPCaller is a caller with every scope, used for WEB_API_KEY.
func FullMCPCaller(name string) MCPCaller {
	scopes := make(map[string]bool, len(AllScopes))
	for _, scope := range AllScopes {
		scopes[scope] = true
	}
	return MCPCaller{Name: name, Scopes: scopes}
}

type mcpCallerKey struct{}

// WithMCPCaller attaches the authenticated MCP caller to a request context.
func WithMCPCaller(ctx context.Context, caller MCPCaller) context.Context {
	return context.WithValue(ctx, mcpCallerKey{}, caller)
}

// MCPCallerFrom returns the caller attached by WithMCPCaller.
func MCPCallerFrom(ctx context.Context) (MCPCaller, bool) {
	caller, ok := ctx.Value(mcpCallerKey{}).(MCPCaller)
	return caller, ok
}

var mcpKeyName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)

// minMCPKeyLength keeps keys long enough that guessing one is not an option.
const minMCPKeyLength = 16

// ParseMCPKeys reads MCP_API_KEYS: entries separated by ";" or newlines, each written
// name:key[:scopes[:expires]]. Scopes are comma-separated from AllScopes, or "all"
// (the default); expires is a date (YYYY-MM-DD, valid through that day in UTC) or an
// RFC 3339 timestamp.
func ParseMCPKeys(raw string) ([]MCPKey, error) {
	var keys []MCPKey
	names, secrets := map[string]bool{}, map[string]bool{}
	for _, entry := range strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == '\n' }) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, ":")
		// An RFC 3339 expiry has colons of its own; everything after the scopes is the time.
		if len(parts) > 4 {
			parts = append(parts[:3], strings.Join(parts[3:], ":"))
		}
		if len(parts) < 2 {
			return nil, fmt.Errorf("MCP_API_KEYS: %q must look like name:key[:scopes[:expires]]", redactEntry(entry))
		}
		key := MCPKey{Name: strings.TrimSpace(parts[0]), Key: strings.TrimSpace(parts[1])}
		if !mcpKeyName.MatchString(key.Name) {
			return nil, fmt.Errorf("MCP_API_KEYS: key name %q may only use letters, digits, '.', '_' and '-' (up to 40)", key.Name)
		}
		if len(key.Key) < minMCPKeyLength || strings.ContainsAny(key.Key, " \t") {
			return nil, fmt.Errorf("MCP_API_KEYS: the key for %q must be at least %d characters without spaces", key.Name, minMCPKeyLength)
		}
		if names[key.Name] {
			return nil, fmt.Errorf("MCP_API_KEYS: key name %q is used twice", key.Name)
		}
		if secrets[key.Key] {
			return nil, fmt.Errorf("MCP_API_KEYS: %q reuses another entry's key", key.Name)
		}
		names[key.Name], secrets[key.Key] = true, true

		scopes := "all"
		if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
			scopes = parts[2]
		}
		var err error
		if key.Scopes, err = parseScopes(scopes); err != nil {
			return nil, fmt.Errorf("MCP_API_KEYS: %q: %w", key.Name, err)
		}
		if len(parts) > 3 && strings.TrimSpace(parts[3]) != "" {
			expires, err := parseExpiry(strings.TrimSpace(parts[3]))
			if err != nil {
				return nil, fmt.Errorf("MCP_API_KEYS: %q: %w", key.Name, err)
			}
			key.Expires = &expires
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func parseScopes(raw string) (map[string]bool, error) {
	scopes := map[string]bool{}
	for _, scope := range strings.Split(raw, ",") {
		scope = strings.ToLower(strings.TrimSpace(scope))
		switch {
		case scope == "":
		case scope == "all":
			for _, s := range AllScopes {
				scopes[s] = true
			}
		case slices.Contains(AllScopes, scope):
			scopes[scope] = true
		default:
			return nil, fmt.Errorf("unknown scope %q; use %s or all", scope, strings.Join(AllScopes, ", "))
		}
	}
	if len(scopes) == 0 {
		return nil, fmt.Errorf("no scopes given")
	}
	return scopes, nil
}

func parseExpiry(raw string) (time.Time, error) {
	if day, err := time.Parse("2006-01-02", raw); err == nil {
		return day.Add(24 * time.Hour), nil // valid through that whole day, UTC
	}
	if at, err := time.Parse(time.RFC3339, raw); err == nil {
		return at, nil
	}
	return time.Time{}, fmt.Errorf("expiry %q must be YYYY-MM-DD or an RFC 3339 time", raw)
}

// redactEntry keeps error messages from echoing a key: only the name part survives.
func redactEntry(entry string) string {
	if name, _, found := strings.Cut(entry, ":"); found {
		return name + ":***"
	}
	return "***"
}

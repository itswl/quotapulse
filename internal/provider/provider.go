// Package provider is the adapter layer for platform balance queries.
//
// Most platforms are "one GET, read one number from JSON" and are declared
// with a Spec — a few lines add a new platform; the signed ones (Volcengine,
// Aliyun) implement the Provider interface themselves.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultTimeout is the per-request timeout; retries belong to Client.
const DefaultTimeout = 15 * time.Second

// MaxRetries is the retry count after failure.
const MaxRetries = 3

// Provider queries one account's current balance.
type Provider interface {
	// Fetch returns the current balance. The semantics are the platform's: currency amount, credits, or remaining percent.
	Fetch(ctx context.Context) (float64, error)
}

// Factory builds an adapter from an API key; malformed keys return an error.
type Factory func(apiKey string, client *Client) (Provider, error)

// Info is one platform's metadata in the registry, feeding /api/providers and the page dropdown.
type Info struct {
	Key         string `json:"value"`
	Name        string `json:"label"`
	DefaultType string `json:"default_type"`
	Currency    string `json:"currency,omitempty"`
}

var registry = map[string]struct {
	info    Info
	factory Factory
}{}

// Register adds a platform; called from each adapter's init.
func Register(key, name, defaultType string, factory Factory) {
	RegisterWithCurrency(key, name, defaultType, "", factory)
}

// RegisterWithCurrency adds a platform whose balance is a monetary amount in
// the given ISO currency; empty means quota percent or credits without a
// known currency.
func RegisterWithCurrency(key, name, defaultType, currency string, factory Factory) {
	registry[key] = struct {
		info    Info
		factory Factory
	}{Info{Key: key, Name: name, DefaultType: defaultType, Currency: currency}, factory}
}

// New creates an adapter by platform name. A bare name resolves to its
// custom: definition, so projects created against the retired built-in
// adapters keep working unmodified.
func New(key, apiKey string, client *Client) (Provider, error) {
	if client == nil {
		client = NewClient(DefaultTimeout)
	}
	if spec, ok := lookupSpec(canonicalKey(key)); ok {
		return &specProvider{spec: spec, apiKey: apiKey, client: client}, nil
	}
	entry, ok := registry[key]
	if !ok {
		return nil, fmt.Errorf("Unknown provider: %s. Supported providers: %s", key, strings.Join(Keys(), ", "))
	}
	return entry.factory(apiKey, client)
}

// Keys returns all registered platform names, sorted.
func Keys() []string {
	keys := make([]string, 0, len(registry))
	for k := range registry {
		keys = append(keys, k)
	}
	keys = append(keys, dynamicKeys()...)
	sort.Strings(keys)
	return keys
}

// All returns every platform's metadata, sorted by name.
func All() []Info {
	out := make([]Info, 0, len(registry)+8)
	for _, k := range Keys() {
		if spec, ok := lookupSpec(k); ok {
			out = append(out, Info{Key: spec.Key, Name: spec.Name, DefaultType: spec.DefaultType, Currency: spec.Currency})
			continue
		}
		out = append(out, registry[k].info)
	}
	return out
}

// Lookup returns one platform's metadata.
func Lookup(key string) (Info, bool) {
	if spec, ok := lookupSpec(canonicalKey(key)); ok {
		return Info{Key: spec.Key, Name: spec.Name, DefaultType: spec.DefaultType, Currency: spec.Currency}, true
	}
	entry, ok := registry[key]
	return entry.info, ok
}

// trimCustomPrefix drops the custom: namespace for env-var derivation and
// identity purposes.
func trimCustomPrefix(key string) string {
	return strings.TrimPrefix(key, "custom:")
}

// canonicalKey maps a bare platform name onto its custom: definition when one
// exists; anything else passes through unchanged.
func canonicalKey(key string) string {
	if _, ok := registry[key]; ok {
		return key
	}
	if lookupSpecExact("custom:" + key) {
		return "custom:" + key
	}
	return key
}

// DisplayName returns the platform's display name, or the input unchanged when unregistered.
func DisplayName(key string) string {
	if info, ok := Lookup(key); ok {
		return info.Name
	}
	return key
}

// ---------- HTTP ----------

// Client is an HTTP client with retries, shared by adapters so pooling pays off.
type Client struct {
	http *http.Client
}

// NewClient creates a client with a timeout.
func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{http: &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        64,
			MaxIdleConnsPerHost: 8,
			IdleConnTimeout:     90 * time.Second,
		},
	}}
}

// These status codes may succeed on retry.
var retriableStatus = map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true}

// Do sends the request, retrying with backoff on retryable statuses and network
// errors. Only idempotent methods retry: a non-idempotent request may already
// have taken effect server-side, and resending duplicates it.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	idempotent := req.Method == http.MethodGet || req.Method == http.MethodHead || req.Method == http.MethodOptions

	var lastErr error
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(float64(time.Second) * 0.5 * math.Pow(2, float64(attempt-1)))
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(wait):
			}
		}

		resp, err := c.http.Do(req.Clone(req.Context()))
		if err != nil {
			lastErr = err
			if !idempotent || req.Context().Err() != nil {
				break
			}
			continue
		}
		if !idempotent || !retriableStatus[resp.StatusCode] || attempt == MaxRetries {
			return resp, nil
		}
		// drain the body so the connection can be reused
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		resp.Body.Close()
		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil, classify(lastErr)
}

// GetJSON sends a GET and parses the response into a map.
func (c *Client) GetJSON(ctx context.Context, rawURL string, headers map[string]string, params map[string]string) (map[string]any, error) {
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		if strings.Contains(rawURL, "?") {
			rawURL += "&" + q.Encode()
		} else {
			rawURL += "?" + q.Encode()
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read provider response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, errors.New("Response is not valid JSON")
	}
	return data, nil
}

// classify translates low-level network errors into user-facing messages:
// timeouts say "request timed out", HTTP statuses pass through, everything else
// becomes "network connection error".
func classify(err error) error {
	if err == nil {
		return nil
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return errors.New("Request timed out")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("Request timed out")
	}
	if strings.HasPrefix(err.Error(), "HTTP ") {
		return err
	}
	return fmt.Errorf("Network connection error: %w", err)
}

// ---------- Declarative adapters ----------

// AuthMode decides where the API key goes.
type AuthMode int

const (
	AuthBearer AuthMode = iota // Authorization: Bearer <key>
	AuthQuery                  // appended to the query string
	AuthHeader                 // a named request header, e.g. X-API-Key
	AuthNone                   // key never leaves the project row
)

// Spec declares a "one GET, read one number from JSON" balance API.
type Spec struct {
	Key         string // registry name, e.g. deepseek
	Name        string // display name, e.g. DeepSeek
	DefaultType string // balance type: balance / credits / quota
	Currency    string // native currency of the balance; empty for quota percents
	URL         string
	Auth        AuthMode
	AuthParam   string // parameter name when Auth is AuthQuery
	Headers     map[string]string
	Params      map[string]string

	// Check validates business-level success; an error means the query failed. Optional.
	Check func(data map[string]any) error
	// Extract pulls the balance from the response, erroring when absent.
	Extract func(data map[string]any) (float64, error)
	// Sign, when set, builds the per-request auth: signed headers and query
	// parameters from the endpoint URL and key (the Aliyun RPC and Volcengine
	// V4 presets). Optional.
	Sign func(rawURL, apiKey string) (headers map[string]string, params map[string]string, err error)
}

// RegisterSpec registers a platform declaratively via Spec.
func RegisterSpec(spec Spec) {
	RegisterWithCurrency(spec.Key, spec.Name, spec.DefaultType, spec.Currency, func(apiKey string, client *Client) (Provider, error) {
		return &specProvider{spec: spec, apiKey: apiKey, client: client}, nil
	})
}

type specProvider struct {
	spec   Spec
	apiKey string
	client *Client
}

func (p *specProvider) Fetch(ctx context.Context) (float64, error) {
	headers := make(map[string]string, len(p.spec.Headers)+1)
	for k, v := range p.spec.Headers {
		headers[k] = v
	}
	params := make(map[string]string, len(p.spec.Params)+1)
	for k, v := range p.spec.Params {
		params[k] = v
	}
	switch p.spec.Auth {
	case AuthNone:
	case AuthQuery:
		params[p.spec.AuthParam] = p.apiKey
	case AuthHeader:
		headers[p.spec.AuthParam] = p.apiKey
	default: // AuthBearer
		headers["Authorization"] = "Bearer " + p.apiKey
	}
	if p.spec.Sign != nil {
		signedHeaders, signedParams, err := p.spec.Sign(p.spec.URL, p.apiKey)
		if err != nil {
			return 0, err
		}
		for key, value := range signedHeaders {
			headers[key] = value
		}
		for key, value := range signedParams {
			params[key] = value
		}
	}

	data, err := p.client.GetJSON(ctx, p.spec.URL, headers, params)
	if err != nil {
		return 0, err
	}
	if p.spec.Check != nil {
		if err := p.spec.Check(data); err != nil {
			return 0, err
		}
	}
	return p.spec.Extract(data)
}

// ---------- JSON access helpers ----------

// Dig walks a path into nested fields; any non-object level returns nil.
func Dig(data map[string]any, path ...string) any {
	var current any = data
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = obj[key]
	}
	return current
}

// Object asserts a value into an object, nil on failure.
func Object(v any) map[string]any {
	obj, _ := v.(map[string]any)
	return obj
}

// Num converts a JSON number (float64 or a thousands-separated string) into float64.
func Num(v any) (float64, bool) {
	switch value := v.(type) {
	case float64:
		return value, true
	case json.Number:
		f, err := value.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(value), ",", ""), 64)
		return f, err == nil
	case int:
		return float64(value), true
	case bool:
		return 0, false
	}
	return 0, false
}

// Str reads the value as a string; non-strings return empty.
func Str(v any) string {
	s, _ := v.(string)
	return s
}

// ---------- Log masking ----------

var sensitiveQueryKeys = map[string]bool{
	"access_token": true, "ak": true, "api_key": true, "authorization": true,
	"key": true, "secret": true, "signature": true, "sk": true, "token": true,
}

// MaskURL masks keys and webhook tokens in URLs for logging.
func MaskURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	for _, marker := range []string{"/hook/", "/token/", "/access_token/"} {
		idx := strings.Index(parsed.Path, marker)
		if idx < 0 {
			continue
		}
		prefix := parsed.Path[:idx+len(marker)]
		rest := parsed.Path[idx+len(marker):]
		token, tail, hasTail := strings.Cut(rest, "/")
		if token != "" {
			masked := "***"
			if len(token) > 8 {
				masked = token[:4] + "***" + token[len(token)-4:]
			}
			parsed.Path = prefix + masked
			if hasTail {
				parsed.Path += "/" + tail
			}
			// String() escapes * into %2A by default, making log entries like
			// abcd%2A%2A%2A7890 unreadable; RawPath keeps the raw form
			parsed.RawPath = parsed.Path
		}
		break
	}

	query := parsed.Query()
	for key, values := range query {
		if !sensitiveQueryKeys[strings.ToLower(key)] {
			continue
		}
		for i, v := range values {
			if v == "" {
				continue
			}
			if len(v) > 8 {
				values[i] = v[:4] + "***" + v[len(v)-4:]
			} else {
				values[i] = "***"
			}
		}
		query[key] = values
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// SplitKeyPair splits an "ID:Secret" key, used by Volcengine and Aliyun. label is
// the platform name and format the key's notation (e.g. "AK:SK"); both go into
// the user-facing hint.
func SplitKeyPair(apiKey, label, format string) (string, string, error) {
	id, secret, ok := strings.Cut(apiKey, ":")
	if !ok || id == "" || secret == "" {
		return "", "", fmt.Errorf("%s: invalid API key format; expected '%s'", label, format)
	}
	return id, secret, nil
}

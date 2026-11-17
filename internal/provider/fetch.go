package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// attemptTimeout bounds one HTTP attempt; retries each get their own.
	attemptTimeout = 15 * time.Second
	// maxBody is far more than any balance response; it only exists so a
	// misbehaving endpoint cannot fill memory.
	maxBody = 4 << 20
)

// Waits before the second and third attempt. Only requests that are safe to
// repeat are retried.
var retryWaits = []time.Duration{400 * time.Millisecond, 1200 * time.Millisecond}

var retriable = map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true}

// Client fetches meters according to a definition.
type Client struct {
	HTTP *http.Client
	// Now and Location feed the template built-ins; time.Now and UTC when unset.
	Now      func() time.Time
	Location *time.Location
	// UserAgent identifies the caller to platforms.
	UserAgent string
}

// NewClient returns a client that does not follow redirects: a redirect to
// another host would carry the credential along with it.
func NewClient() *Client {
	return &Client{HTTP: &http.Client{
		Timeout: attemptTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: attemptTimeout,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       90 * time.Second,
		},
	}}
}

// Result is the outcome of one fetch.
type Result struct {
	Samples []Sample      `json:"samples"`
	Status  int           `json:"status"`
	Latency time.Duration `json:"-"`
	// Body is the parsed response. The dashboard shows it when a definition
	// is being written or tested.
	Body any `json:"body,omitempty"`
}

// Fetch performs the definition's request with a credential and extracts its
// meters. Every error it returns is safe to show: secrets are removed.
func (c *Client) Fetch(ctx context.Context, def Definition, credential map[string]string) (Result, error) {
	secrets := make([]string, 0, len(credential))
	for _, value := range credential {
		secrets = append(secrets, value)
	}
	result, err := c.fetch(ctx, def, credential)
	if err != nil {
		return result, errors.New(Redact(err.Error(), secrets...))
	}
	return result, nil
}

func (c *Client) fetch(ctx context.Context, def Definition, credential map[string]string) (Result, error) {
	var result Result
	fields, err := def.CheckCredential(credential)
	if err != nil {
		return result, err
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	req, err := c.build(ctx, def, fields, now)
	if err != nil {
		return result, err
	}

	started := time.Now()
	status, raw, err := c.do(req)
	result.Latency = time.Since(started)
	result.Status = status
	if err != nil {
		return result, err
	}

	var body any
	if err := json.Unmarshal(raw, &body); err != nil {
		if status < 200 || status > 299 {
			return result, fmt.Errorf("HTTP %d %s", status, http.StatusText(status))
		}
		return result, fmt.Errorf("the response is not JSON: %s", snippet(raw, 120))
	}
	result.Body = body
	if status < 200 || status > 299 {
		if message := apiMessage(body); message != "" {
			return result, fmt.Errorf("HTTP %d: %s", status, message)
		}
		return result, fmt.Errorf("HTTP %d %s", status, http.StatusText(status))
	}

	visible := map[string]string{}
	secret := def.SecretKeys()
	for key, value := range fields {
		if !secret[key] {
			visible[key] = value
		}
	}
	result.Samples, err = Extract(ctx, def.Extract, body, visible, now)
	return result, err
}

// build assembles the request: templates expanded, credential applied.
func (c *Client) build(ctx context.Context, def Definition, fields map[string]string, now time.Time) (*http.Request, error) {
	loc := c.Location
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	monthStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	vars := map[string]string{
		"now":              strconv.FormatInt(now.Unix(), 10),
		"today":            local.Format("2006-01-02"),
		"month_start":      strconv.FormatInt(monthStart.Unix(), 10),
		"month_start_date": monthStart.Format("2006-01-02"),
	}
	for key, value := range fields {
		vars[key] = value
	}
	expand := func(text string) string {
		return placeholder.ReplaceAllStringFunc(text, func(match string) string {
			return vars[placeholder.FindStringSubmatch(match)[1]]
		})
	}
	// In a URL a field is usually a base address; "https://host/" + "/v1/x"
	// should not come out with two slashes.
	expandURL := func(text string) string {
		return placeholder.ReplaceAllStringFunc(text, func(match string) string {
			return strings.TrimRight(vars[placeholder.FindStringSubmatch(match)[1]], "/")
		})
	}

	target, err := url.Parse(strings.TrimSpace(expandURL(def.Request.URL)))
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, errors.New("the request URL is not a valid http(s) address")
	}
	query := map[string]string{}
	for key, values := range target.Query() {
		if len(values) > 0 {
			query[key] = values[0]
		}
	}
	for key, value := range def.Request.Query {
		query[key] = expand(value)
	}
	headers := map[string]string{"Accept": "application/json"}
	if c.UserAgent != "" {
		headers["User-Agent"] = c.UserAgent
	}
	for key, value := range def.Request.Headers {
		headers[key] = expand(value)
	}
	method := strings.ToUpper(def.Request.Method)
	if method == "" {
		method = http.MethodGet
	}
	body := expand(def.Request.Body)
	if body != "" {
		if _, set := headers["Content-Type"]; !set {
			headers["Content-Type"] = "application/json"
		}
	}

	auth := def.Request.Auth
	value := auth.Value
	if value == "" {
		value = "{{api_key}}"
	}
	value = expand(value)
	signedQuery := false
	switch auth.Type {
	case AuthBearer:
		headers["Authorization"] = "Bearer " + value
	case AuthHeader:
		headers[auth.Name] = value
	case AuthQuery:
		query[auth.Name] = value
	case AuthBasic:
		headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(value))
	case AuthAliyunRPC:
		query = aliyunRPCQuery(fields["access_key_id"], fields["access_key_secret"], query, now, nonce())
		signedQuery = true
	case AuthVolcV4:
		path := target.EscapedPath()
		if path == "" {
			path = "/"
		}
		signature := volcV4{
			Method: method, Host: target.Host, Path: path,
			Region: auth.Region, Service: auth.Service,
			AK: fields["access_key_id"], SK: fields["secret_access_key"],
			Query: query, Body: body,
		}
		for key, value := range signature.headers(now) {
			headers[key] = value
		}
		signedQuery = true
	}

	if signedQuery {
		// The signature covers this exact encoding.
		target.RawQuery = canonicalQuery(query)
	} else {
		values := url.Values{}
		for key, value := range query {
			values.Set(key, value)
		}
		target.RawQuery = values.Encode()
	}

	var reader io.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return nil, errors.New("the request could not be built")
	}
	if body != "" {
		payload := []byte(body)
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(payload)), nil }
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return req, nil
}

// do sends the request, retrying what is safe to retry.
func (c *Client) do(req *http.Request) (int, []byte, error) {
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	attempts := 1
	if req.Method == http.MethodGet {
		attempts += len(retryWaits)
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-req.Context().Done():
				return 0, nil, lastErr
			case <-time.After(retryWaits[attempt-1]):
			}
		}
		resp, err := client.Do(req.Clone(req.Context()))
		if err != nil {
			lastErr = networkError(err)
			if req.Context().Err() != nil {
				break
			}
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		resp.Body.Close()
		if readErr != nil {
			lastErr = errors.New("the response could not be read")
			continue
		}
		if retriable[resp.StatusCode] && attempt < attempts-1 {
			lastErr = fmt.Errorf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
			continue
		}
		return resp.StatusCode, raw, nil
	}
	return 0, nil, lastErr
}

// networkError says what went wrong in words a person can act on. The
// wrapped *url.Error is dropped on purpose: it repeats the full URL, and with
// query auth that includes the key.
func networkError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return errors.New("the request timed out")
		}
		err = urlErr.Err
	}
	var dnsErr *net.DNSError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("the request timed out")
	case errors.Is(err, context.Canceled):
		return errors.New("the request was cancelled")
	case errors.As(err, &dnsErr):
		return fmt.Errorf("could not resolve %s", dnsErr.Name)
	}
	return fmt.Errorf("could not connect: %v", err)
}

// apiMessage digs the human-readable part out of an error response. Every
// platform nests it differently; these are the shapes seen in practice.
func apiMessage(body any) string {
	object, ok := body.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"error", "Error", "ResponseMetadata"} {
		switch nested := object[key].(type) {
		case string:
			if nested != "" {
				return snippet([]byte(nested), 300)
			}
		case map[string]any:
			if message := apiMessage(nested); message != "" {
				return message
			}
		}
	}
	for _, key := range []string{"message", "Message", "msg", "error_msg", "error_description", "detail"} {
		if text, ok := object[key].(string); ok && text != "" {
			return snippet([]byte(text), 300)
		}
	}
	return ""
}

// snippet shortens by characters, so a multi-byte message is never cut in half.
func snippet(raw []byte, limit int) string {
	runes := []rune(strings.TrimSpace(string(raw)))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

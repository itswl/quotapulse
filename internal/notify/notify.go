// Package notify provides the package implementation.
//
// Implementation note.
// Implementation note.
// Implementation note.
// Implementation note.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Implementation note.
const (
	TypeFeishu   = "feishu"
	TypeCustom   = "custom"
	TypeDingTalk = "dingtalk"
	TypeWeCom    = "wecom"
)

// Implementation note.
// Implementation note.
const (
	KindBalance      = "balance"
	KindSubscription = "subscription"
	KindEmail        = "email"
	KindMailboxError = "mailbox_error"
	KindRunway       = "runway"
	KindSpendSpike   = "spend_spike"
	KindWeeklyReport = "weekly_report"
	KindTest         = "test"
	KindJobFailure   = "job_failure"
)

// Implementation note.
var defaultBackoff = []time.Duration{2 * time.Second, 4 * time.Second}

// Implementation note.
const maxResponseBody = 4096

// Implementation note.
//
// Implementation note.
// Implementation note.
type Message struct {
	Title string
	Lines []string
	Kind  string

	// Implementation note.
	// Implementation note.
	// Implementation note.
	envelope *envelope
}

// Implementation note.
type Notifier interface {
	Send(ctx context.Context, msg Message) error
}

// Implementation note.
func SupportedTypes() []string {
	return []string{TypeFeishu, TypeCustom, TypeDingTalk, TypeWeCom}
}

// Implementation note.
//
// Implementation note.
// Implementation note.
// Implementation note.
func New(url, webhookType, source string, client *http.Client) (Notifier, error) {
	if strings.TrimSpace(url) == "" {
		return nil, nil
	}

	typ := strings.ToLower(strings.TrimSpace(webhookType))
	if typ == "" {
		// Implementation note.
		typ = TypeCustom
	}
	if !supported(typ) {
		return nil, fmt.Errorf("Unsupported Webhook type %q,operation %s",
			webhookType, strings.Join(SupportedTypes(), "/"))
	}
	if client == nil {
		client = http.DefaultClient
	}

	return &notifier{
		url:     url,
		typ:     typ,
		source:  source,
		client:  client,
		now:     time.Now,
		backoff: defaultBackoff,
	}, nil
}

func supported(typ string) bool {
	for _, t := range SupportedTypes() {
		if t == typ {
			return true
		}
	}
	return false
}

type notifier struct {
	url    string
	typ    string
	source string
	client *http.Client

	now     func() time.Time // operation;operation
	backoff []time.Duration  // operation,operation
}

// Implementation note.
func (n *notifier) Send(ctx context.Context, msg Message) error {
	body, err := json.Marshal(n.payload(msg))
	if err != nil {
		return fmt.Errorf("Failed to serialize Webhook payload: %w", err)
	}

	log := slog.Default()
	log.Info("Preparing Webhook", "url", MaskURL(n.url), "type", n.typ, "kind", msg.Kind)
	log.Debug("operation", "payload", snippet(body, 500))

	var last error
	for attempt := 0; ; attempt++ {
		err := n.post(ctx, body)
		if err == nil {
			return nil
		}
		last = err

		var retry retryError
		if !errors.As(err, &retry) || attempt >= len(n.backoff) {
			return attemptedError(attempt+1, last)
		}
		if waitErr := wait(ctx, n.backoff[attempt]); waitErr != nil {
			return fmt.Errorf("after %d attempt(s): %w (wait cancelled: %v)", attempt+1, last, waitErr)
		}
	}
}

func (n *notifier) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("Failed to construct Webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := n.client.Do(req)
	if err != nil {
		// Implementation note.
		return retryError{fmt.Errorf("Webhook request failed: %w", err)}
	}
	defer resp.Body.Close()
	// Implementation note.
	text, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	elapsed := time.Since(start)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		slog.Default().Info("Alert sent successfully", "type", n.typ, "status", resp.StatusCode, "elapsed", elapsed)
		return nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return retryError{fmt.Errorf("Webhook rate limited: HTTP 429, Retry-After: %s, %s",
			resp.Header.Get("Retry-After"), snippet(text, 200))}
	case resp.StatusCode >= 500:
		return retryError{fmt.Errorf("Webhook server error: HTTP %d, %s", resp.StatusCode, snippet(text, 200))}
	default:
		// Implementation note.
		return fmt.Errorf("Webhook returned HTTP %d: %s", resp.StatusCode, snippet(text, 500))
	}
}

// Implementation note.
// attemptedError records how many HTTP attempts were made before giving up; the
// webhook HTTP status itself is already part of the underlying message.
func attemptedError(attempts int, err error) error {
	return fmt.Errorf("after %d attempt(s): %w", attempts, err)
}

type retryError struct{ err error }

func (e retryError) Error() string { return e.err.Error() }
func (e retryError) Unwrap() error { return e.err }

// Implementation note.
func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Implementation note.
//
// Implementation note.
// Implementation note.
// Implementation note.
func MaskURL(raw string) string {
	if raw == "" {
		return ""
	}

	for _, marker := range []string{"hook/", "access_token=", "key="} {
		prefix, secret, found := strings.Cut(raw, marker)
		if !found {
			continue
		}
		if secret == "" {
			return prefix + marker + "***"
		}
		return prefix + marker + head(secret, 4) + "***"
	}

	// Implementation note.
	runes := []rune(raw)
	if len(runes) <= 16 {
		return "***"
	}
	return string(runes[:12]) + "***" + string(runes[len(runes)-4:])
}

func head(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// Implementation note.
func snippet(b []byte, limit int) string {
	return head(strings.TrimSpace(string(b)), limit)
}

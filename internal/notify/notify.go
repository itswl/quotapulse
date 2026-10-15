// Package notify delivers one alert to a group bot (Feishu / DingTalk / WeCom
// / custom HTTP).
//
// Payload structure, message templates, field order and number formatting
// are external contracts: the on-call group is used to this exact format,
// and systems behind custom webhooks read values by field name — changing a
// single character is a regression. payload_test.go and message_test.go pin
// them byte for byte. This package only delivers; callers record success and
// failure metrics — the same message may come from balance, runway, weekly
// report and other scenarios.
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

// Supported platforms. The order is fixed; the config self-check lists it to
// users verbatim.
const (
	TypeFeishu   = "feishu"
	TypeCustom   = "custom"
	TypeDingTalk = "dingtalk"
	TypeWeCom    = "wecom"
)

// Metric kinds. Balance and subscription go through plain-text/structured payloads,
// the rest through rich-text cards; Send routes on Kind because their body
// formats differ by nature.
const (
	KindBalance      = "balance"
	KindSubscription = "subscription"
	KindEmail        = "email"
	KindMailboxError = "mailbox_error"
	KindRunway       = "runway"
	KindSpendSpike   = "spend_spike"
	KindWeeklyReport = "weekly_report"
	KindBudget       = "budget"
	KindTest         = "test"
	KindJobFailure   = "job_failure"
	KindCheckFailed  = "check_failed"
)

// Retry cadence: three attempts total, waiting 2s then 4s after failures.
var defaultBackoff = []time.Duration{2 * time.Second, 4 * time.Second}

// Read at most this much of the response body for error messages; bot error responses are short.
const maxResponseBody = 4096

// Message is one alert ready to send.
//
// Lines are the body lines: rich-text alerts (runway, mail, weekly report)
// write "**field**: value", balance and subscription use plain "field:
// value" — DingTalk adds list markup and bolding itself.
type Message struct {
	Title string
	Lines []string
	Kind  string

	// custom-type balance/subscription alerts send a structured envelope with raw
	// values like CurrentValue, unrecoverable from Lines, so the constructors here
	// attach it. Externally assembled Messages leave it nil and degrade to the
	// generic envelope — runway and weekly reports use that envelope anyway.
	envelope *envelope
}

// Notifier sends one alert.
type Notifier interface {
	Send(ctx context.Context, msg Message) error
}

// SupportedTypes returns the supported platform list in fixed order; the config self-check shows it to users verbatim.
func SupportedTypes() []string {
	return []string{TypeFeishu, TypeCustom, TypeDingTalk, TypeWeCom}
}

// New builds a notifier from settings.
//
// No URL configured means this deployment sends no alerts at all: return
// nil, nil and let callers skip sending. An unrecognized type errors instead
// of falling back to custom: the fallback would deliver JSON nobody can
// parse, and nobody would notice until a real alert day — better to name the
// config problem at startup.
func New(url, webhookType, source string, client *http.Client) (Notifier, error) {
	if strings.TrimSpace(url) == "" {
		return nil, nil
	}

	typ := strings.ToLower(strings.TrimSpace(webhookType))
	if typ == "" {
		// an empty type means custom; only a genuinely wrong type name errors
		typ = TypeCustom
	}
	if !supported(typ) {
		return nil, fmt.Errorf("Unsupported Webhook type %q; options: %s",
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

	now     func() time.Time // timestamp in custom payloads; tests must be able to pin it
	backoff []time.Duration  // wait before each retry; length is the retry count
}

// Send delivers one alert. Failures always return an error — no degrading here; callers record metrics and decide whether to persist.
func (n *notifier) Send(ctx context.Context, msg Message) error {
	body, err := json.Marshal(n.payload(msg))
	if err != nil {
		return fmt.Errorf("Failed to serialize Webhook payload: %w", err)
	}

	log := slog.Default()
	log.Info("Preparing Webhook", "url", MaskURL(n.url), "type", n.typ, "kind", msg.Kind)
	log.Debug("Request body", "payload", snippet(body, 500))

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
		// timeouts, DNS and refused connections land here; waiting a beat usually fixes it
		return retryError{fmt.Errorf("Webhook request failed: %w", err)}
	}
	defer resp.Body.Close()
	// read the body before closing so the connection returns to the pool for reuse
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
		// 4xx means a wrong address or payload; retrying never helps
		return fmt.Errorf("Webhook returned HTTP %d: %s", resp.StatusCode, snippet(text, 500))
	}
}

// attemptedError records how many HTTP attempts were made before giving up; the
// webhook HTTP status itself is already part of the underlying message.
func attemptedError(attempts int, err error) error {
	return fmt.Errorf("after %d attempt(s): %w", attempts, err)
}

type retryError struct{ err error }

func (e retryError) Error() string { return e.err.Error() }
func (e retryError) Unwrap() error { return e.err }

// wait sleeps, returning immediately when ctx is cancelled; the scheduler must not stall here.
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

// MaskURL masks the secret inside a webhook address before logging.
//
// Markers are tried in order hook/ then access_token= then key=, keeping the
// first 4 characters of the secret. provider.MaskURL follows different rules
// (path segments only) and is deliberately neither reused nor imported: notify
// sits above provider in the layering, and a reverse dependency would loop
// back.
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

	// mask the whole thing when the key position is unrecognized; short URLs get fully covered
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

// snippet truncates by characters (not bytes) so Chinese error messages never cut mid-character.
func snippet(b []byte, limit int) string {
	return head(strings.TrimSpace(string(b)), limit)
}

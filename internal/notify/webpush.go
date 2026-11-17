package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/SherClockHolmes/webpush-go"

	"quotapulse/internal/core"
)

// TypeWebPush is a browser that asked for notifications. Unlike the other
// channel types it is not filled in on a form: the browser creates it when
// someone presses "notify me on this device".
const TypeWebPush = "webpush"

// vapidSubject is the contact the push services are given. It must be a bare
// address: the library adds "mailto:" itself, and Apple rejects both a doubled
// prefix and an address on a host that does not exist publicly. It is never
// used for delivery.
const vapidSubject = "push@users.noreply.github.com"

// VAPID is the key pair that identifies this server to browser push
// services. A browser's subscription is bound to the public key, so the pair
// must stay the same for as long as subscriptions should keep working.
type VAPID struct {
	Public  string `json:"public_key"`
	Private string `json:"private_key"`
}

// NewVAPID generates a key pair.
func NewVAPID() (VAPID, error) {
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return VAPID{}, err
	}
	return VAPID{Public: public, Private: private}, nil
}

// pushPayload is what the dashboard's service worker receives.
type pushPayload struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	URL      string `json:"url"`
	Tag      string `json:"tag"`
	Severity string `json:"severity"`
}

func sendWebPush(ctx context.Context, s *Sender, config map[string]string, m Message) error {
	keys, ok := s.VAPID()
	if !ok {
		return &PermanentError{Reason: "browser push is not set up on this server"}
	}
	payload := pushPayload{Title: m.Title, Body: m.Plain(), URL: orDefault(m.Path, "/"), Tag: m.Event, Severity: string(m.Severity)}
	if m.Subject != nil && m.Subject.IncidentID != "" {
		// One notification per incident: an escalation replaces the message
		// it follows instead of stacking under it.
		payload.Tag = m.Subject.IncidentID
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return &PermanentError{Reason: "the message could not be encoded"}
	}
	urgency := webpush.UrgencyNormal
	if m.Severity == core.Critical && !m.Resolved() {
		urgency = webpush.UrgencyHigh
	}
	resp, err := webpush.SendNotificationWithContext(ctx, raw,
		&webpush.Subscription{Endpoint: config["endpoint"], Keys: webpush.Keys{P256dh: config["p256dh"], Auth: config["auth"]}},
		&webpush.Options{
			HTTPClient: s.HTTP, Subscriber: vapidSubject, VAPIDPublicKey: keys.Public, VAPIDPrivateKey: keys.Private,
			// A day: a phone that was off overnight still gets the message,
			// and one that was off for a week is spared the backlog.
			TTL: 24 * 3600, Urgency: urgency,
		})
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			return errors.New("the request timed out")
		}
		var netErr interface{ Timeout() bool }
		if errors.As(err, &netErr) {
			return fmt.Errorf("could not reach the push service: %v", unwrapURL(err))
		}
		// Anything else is the subscription itself: keys that do not decode.
		return &PermanentError{Reason: "the browser's subscription is not valid: " + unwrapURL(err).Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	reason := strings.TrimSpace(string(body))
	detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if reason != "" {
		// Push services explain themselves in the body (Apple: BadJwtToken).
		detail += ": " + reason
	}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return &PermanentError{Reason: "the browser's subscription has expired (" + detail + ")", Gone: true}
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return errors.New("the push service answered " + detail)
	default:
		return &PermanentError{Reason: "the push service refused the message: " + detail}
	}
}

// unwrapURL drops the URL a *url.Error repeats: the endpoint is the secret.
func unwrapURL(err error) error {
	var wrapped interface{ Unwrap() error }
	if errors.As(err, &wrapped) {
		if inner := wrapped.Unwrap(); inner != nil {
			return inner
		}
	}
	return err
}

// IsGone reports whether an error means the destination no longer exists
// and will never accept a message again.
func IsGone(err error) bool {
	var permanent *PermanentError
	return errors.As(err, &permanent) && permanent.Gone
}

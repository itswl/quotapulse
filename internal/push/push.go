// Package push fans QuotaPulse alerts out to browsers over the Web Push protocol.
//
// VAPID keys are generated once and persisted in app_settings, so subscriptions stay
// valid across restarts without extra configuration. Dead endpoints (HTTP 404/410) are
// pruned on the next broadcast.
package push

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/store"
)

const (
	settingVapidPublic  = "vapid_public_key"
	settingVapidPrivate = "vapid_private_key"
	// vapidSubscriber becomes the VAPID JWT "sub" claim. webpush-go prepends "mailto:" to
	// any value that is not an https URL, so this must be a bare address — passing
	// "mailto:x@y" signs "mailto:mailto:x@y". Apple rejects that, and non-public hosts
	// such as https://quotapulse.local, with 403 BadJwtToken (checked against
	// web.push.apple.com); Chrome and Firefox accept both, which is how the bug hid.
	// It is a contact for the push service and is never used for delivery.
	vapidSubscriber = "push@users.noreply.github.com"
)

// Manager stores browser push subscriptions and fans alert notifications out to them.
type Manager struct {
	Store store.Store
	Log   *slog.Logger

	mu         sync.Mutex
	privateKey string
	publicKey  string
}

func New(st store.Store) *Manager {
	return &Manager{Store: st, Log: slog.Default()}
}

func (m *Manager) log() *slog.Logger {
	if m.Log != nil {
		return m.Log
	}
	return slog.Default()
}

// PublicKey returns the VAPID public key browsers subscribe with, generating and
// persisting the pair on first use.
func (m *Manager) PublicKey(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, publicKey, err := m.keysLocked(ctx)
	return publicKey, err
}

func (m *Manager) keys(ctx context.Context) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.keysLocked(ctx)
}

// keysLocked resolves the VAPID pair; the caller must hold m.mu. Nested locking here
// would deadlock, which is exactly what the first version did.
func (m *Manager) keysLocked(ctx context.Context) (string, string, error) {
	if m.privateKey != "" && m.publicKey != "" {
		return m.privateKey, m.publicKey, nil
	}
	storedPublic, err := m.Store.GetAppSetting(ctx, settingVapidPublic)
	if err == nil && storedPublic != "" {
		storedPrivate, privErr := m.Store.GetAppSetting(ctx, settingVapidPrivate)
		if privErr == nil && storedPrivate != "" {
			m.privateKey, m.publicKey = storedPrivate, storedPublic
			return m.privateKey, m.publicKey, nil
		}
	}
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", fmt.Errorf("generate VAPID keys: %w", err)
	}
	if err := m.Store.SetAppSetting(ctx, settingVapidPublic, publicKey); err != nil {
		return "", "", fmt.Errorf("persist VAPID public key: %w", err)
	}
	if err := m.Store.SetAppSetting(ctx, settingVapidPrivate, privateKey); err != nil {
		return "", "", fmt.Errorf("persist VAPID private key: %w", err)
	}
	m.privateKey, m.publicKey = privateKey, publicKey
	return privateKey, publicKey, nil
}

// Notify fans one alert out to every registered browser. Delivery failures are logged
// without blocking the alert pipeline.
func (m *Manager) Notify(ctx context.Context, title, body string) {
	if m == nil || m.Store == nil {
		return
	}
	subs, err := m.Store.ListPushSubscriptions(ctx)
	if err != nil {
		m.log().Warn("Failed to list push subscriptions", "error", err)
		return
	}
	for _, sub := range subs {
		m.SendTo(ctx, sub, title, body)
	}
}

// SendTo delivers one notification to one browser and reports the delivery error, so
// the enable flow can confirm the channel end to end.
func (m *Manager) SendTo(ctx context.Context, sub model.PushSubscription, title, body string) error {
	if m == nil || m.Store == nil {
		return fmt.Errorf("push is not available")
	}
	privateKey, publicKey, err := m.keys(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"title": title, "body": body, "url": "/"})
	if err != nil {
		return err
	}
	options := &webpush.Options{
		Subscriber:      vapidSubscriber,
		VAPIDPublicKey:  publicKey,
		VAPIDPrivateKey: privateKey,
		TTL:             3600,
		Urgency:         webpush.UrgencyHigh,
	}
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{Auth: sub.Auth, P256dh: sub.P256dh},
	}, options)
	if err != nil {
		m.log().Warn("Push notification failed", "endpoint", shortEndpoint(sub.Endpoint), "error", err)
		return fmt.Errorf("push service rejected the notification: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 300 {
		return nil
	}
	// Push services explain rejections in the body (Apple: {"reason":"BadJwtToken"}).
	// Keep it: a bare status code made the iOS failure undiagnosable from the logs.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	reason := strings.TrimSpace(string(raw))
	m.log().Warn("Push service rejected the notification",
		"endpoint", shortEndpoint(sub.Endpoint), "status", resp.StatusCode, "reason", reason)
	detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if reason != "" {
		detail += ": " + reason
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		if delErr := m.Store.DeletePushSubscription(ctx, sub.Endpoint); delErr != nil {
			m.log().Warn("Failed to prune expired push subscription", "error", delErr)
		}
		return fmt.Errorf("push endpoint is gone (%s); the browser registration was pruned", detail)
	}
	return fmt.Errorf("push service returned %s", detail)
}

// Subscribe registers or refreshes one browser.
func (m *Manager) Subscribe(ctx context.Context, sub model.PushSubscription) error {
	return m.Store.UpsertPushSubscription(ctx, sub)
}

// Unsubscribe removes one browser registration.
func (m *Manager) Unsubscribe(ctx context.Context, endpoint string) error {
	return m.Store.DeletePushSubscription(ctx, endpoint)
}

func shortEndpoint(endpoint string) string {
	if len(endpoint) > 48 {
		return endpoint[:48] + "…"
	}
	return endpoint
}

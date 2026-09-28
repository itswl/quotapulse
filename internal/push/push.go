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
	"log/slog"
	"net/http"
	"sync"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/store"
)

const (
	settingVapidPublic  = "vapid_public_key"
	settingVapidPrivate = "vapid_private_key"
	// The Web Push spec requires the JWT sub claim to be an https URL or mailto address.
	vapidSubscriber = "https://quotapulse.local"
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
	if m.publicKey != "" {
		return m.publicKey, nil
	}

	storedPublic, err := m.Store.GetAppSetting(ctx, settingVapidPublic)
	if err == nil && storedPublic != "" {
		storedPrivate, privErr := m.Store.GetAppSetting(ctx, settingVapidPrivate)
		if privErr == nil && storedPrivate != "" {
			m.privateKey, m.publicKey = storedPrivate, storedPublic
			return m.publicKey, nil
		}
	}

	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", fmt.Errorf("generate VAPID keys: %w", err)
	}
	if err := m.Store.SetAppSetting(ctx, settingVapidPublic, publicKey); err != nil {
		return "", fmt.Errorf("persist VAPID public key: %w", err)
	}
	if err := m.Store.SetAppSetting(ctx, settingVapidPrivate, privateKey); err != nil {
		return "", fmt.Errorf("persist VAPID private key: %w", err)
	}
	m.privateKey, m.publicKey = privateKey, publicKey
	return publicKey, nil
}

func (m *Manager) keys(ctx context.Context) (private, public string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.privateKey != "" && m.publicKey != "" {
		return m.privateKey, m.publicKey, nil
	}
	public, pubErr := m.PublicKey(ctx)
	if pubErr != nil {
		return "", "", pubErr
	}
	return m.privateKey, m.publicKey, nil
}

// Notify fans one alert out to every registered browser. Expired endpoints are pruned
// and delivery failures are logged without blocking the alert pipeline.
func (m *Manager) Notify(ctx context.Context, title, body string) {
	if m == nil || m.Store == nil {
		return
	}
	subs, err := m.Store.ListPushSubscriptions(ctx)
	if err != nil {
		m.log().Warn("Failed to list push subscriptions", "error", err)
		return
	}
	if len(subs) == 0 {
		return
	}
	privateKey, publicKey, err := m.keys(ctx)
	if err != nil {
		m.log().Warn("Push notification skipped; VAPID keys unavailable", "error", err)
		return
	}
	payload, err := json.Marshal(map[string]string{"title": title, "body": body, "url": "/"})
	if err != nil {
		return
	}

	for _, sub := range subs {
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
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			if delErr := m.Store.DeletePushSubscription(ctx, sub.Endpoint); delErr != nil {
				m.log().Warn("Failed to prune expired push subscription", "error", delErr)
			}
		}
	}
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

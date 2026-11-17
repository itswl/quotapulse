package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"quotapulse/internal/core"
	"quotapulse/internal/notify"
	"quotapulse/internal/secret"
)

// Browser push. A browser that asks for notifications becomes a channel of
// its own, so each device gets the same controls as any other destination:
// which topics, how severe, a test button, a delivery log.

const pushKeysName = "push.vapid"

// pushKeys returns the key pair browsers subscribe with, creating it the
// first time anything needs it.
func (e *Engine) pushKeys(ctx context.Context) (notify.VAPID, error) {
	e.pushMu.Lock()
	defer e.pushMu.Unlock()
	if keys, ok := e.sender.VAPID(); ok {
		return keys, nil
	}
	var keys notify.VAPID
	found, err := e.store.Secret(ctx, pushKeysName, &keys)
	if errors.Is(err, secret.ErrUnreadable) {
		// Sealed under another secret: every browser subscribed with the old
		// pair is lost either way, so start over rather than stay broken.
		e.log.Warn("the stored push keys cannot be read; browsers will need to subscribe again")
		found, err = false, nil
	}
	if err != nil {
		return keys, err
	}
	if !found || keys.Public == "" || keys.Private == "" {
		if keys, err = notify.NewVAPID(); err != nil {
			return keys, fmt.Errorf("generate push keys: %w", err)
		}
		if err := e.store.SetSecret(ctx, pushKeysName, keys); err != nil {
			return keys, err
		}
	}
	e.sender.SetVAPID(keys)
	return keys, nil
}

// PushKey is the public key a browser needs to subscribe.
func (e *Engine) PushKey(ctx context.Context) (string, error) {
	keys, err := e.pushKeys(ctx)
	return keys.Public, err
}

// ExportPushKeys returns the stored key pair, if there is one. A backup
// needs it: without the same keys, restored browser subscriptions are dead.
func (e *Engine) ExportPushKeys(ctx context.Context) (*notify.VAPID, error) {
	var keys notify.VAPID
	found, err := e.store.Secret(ctx, pushKeysName, &keys)
	if err != nil || !found {
		return nil, err
	}
	return &keys, nil
}

// ImportPushKeys installs a key pair, unless one is already in place: the
// browsers subscribed here are bound to the existing pair. It reports
// whether the given pair is now the one in use.
func (e *Engine) ImportPushKeys(ctx context.Context, keys notify.VAPID) (bool, error) {
	if keys.Public == "" || keys.Private == "" {
		return false, nil
	}
	e.pushMu.Lock()
	defer e.pushMu.Unlock()
	var existing notify.VAPID
	found, err := e.store.Secret(ctx, pushKeysName, &existing)
	if err != nil && !errors.Is(err, secret.ErrUnreadable) {
		return false, err
	}
	if found {
		return existing.Public == keys.Public, nil
	}
	if err := e.store.SetSecret(ctx, pushKeysName, keys); err != nil {
		return false, err
	}
	e.sender.SetVAPID(keys)
	return true, nil
}

// PushSubscription is what a browser hands over when it subscribes: where to
// send, and the keys to encrypt with.
type PushSubscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// SubscribePush registers a browser as a channel, or refreshes the one it
// already has: a browser that subscribes twice is still one device.
func (e *Engine) SubscribePush(ctx context.Context, name string, sub PushSubscription, lang string) (ChannelView, error) {
	endpoint := strings.TrimSpace(sub.Endpoint)
	if u, err := url.Parse(endpoint); err != nil || u.Scheme != "https" || u.Host == "" {
		return ChannelView{}, invalid("the subscription's endpoint must be an https address")
	}
	if sub.Keys.P256dh == "" || sub.Keys.Auth == "" {
		return ChannelView{}, invalid("the subscription is missing its keys")
	}
	if _, err := e.pushKeys(ctx); err != nil {
		return ChannelView{}, err
	}
	config := map[string]string{"endpoint": endpoint, "p256dh": sub.Keys.P256dh, "auth": sub.Keys.Auth}

	channels, err := e.store.Channels(ctx)
	if err != nil {
		return ChannelView{}, err
	}
	taken := map[string]bool{}
	for _, c := range channels {
		taken[strings.ToLower(c.Name)] = true
		if c.Type == notify.TypeWebPush && c.Config["endpoint"] == endpoint {
			c.Config, c.Enabled = config, true
			if err := e.store.UpdateChannel(ctx, c, e.now()); err != nil {
				return ChannelView{}, err
			}
			e.hub.Publish(ChangedSettings)
			return e.channelView(ctx, c.ID, lang)
		}
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = "Browser"
		if lang == "zh" {
			name = "浏览器"
		}
	}
	if name, err = cleanName("name", name); err != nil {
		return ChannelView{}, err
	}
	// Two phones of the same model give the same name; number the second.
	unique := name
	for n := 2; taken[strings.ToLower(unique)]; n++ {
		unique = fmt.Sprintf("%s %d", name, n)
	}
	channel := core.Channel{
		Name: unique, Type: notify.TypeWebPush, Config: config,
		Topics: append([]string(nil), core.Topics...), MinSeverity: core.Info, Enabled: true,
	}
	if err := e.store.CreateChannel(ctx, &channel, e.now()); err != nil {
		return ChannelView{}, err
	}
	e.hub.Publish(ChangedSettings)
	return e.channelView(ctx, channel.ID, lang)
}

// forgetBrowser removes a browser channel whose subscription the push
// service says is gone: the browser unsubscribed, or was uninstalled.
func (e *Engine) forgetBrowser(ctx context.Context, channel core.Channel) {
	if err := e.store.DeleteChannel(ctx, channel.ID); err != nil {
		e.log.Error("could not remove an expired browser channel", "channel", channel.Name, "error", err)
		return
	}
	e.mu.Lock()
	_ = e.closeSubject(ctx, core.SubjectChannel, channel.ID)
	e.mu.Unlock()
	e.log.Info("removed a browser channel whose subscription expired", "channel", channel.Name)
	e.hub.Publish(ChangedSettings)
}

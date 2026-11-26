package mcp

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/engine"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/store"
)

// Notifications: the log of what was sent and what was not — which is where
// to look when someone expected a message — and, with admin, the channels
// things are sent through.

var deliveryStatuses = []string{core.DeliveryPending, core.DeliverySent, core.DeliveryFailed, core.DeliveryDropped}

// delivery is one message bound for one channel, without the message
// itself.
type delivery struct {
	ID         int64      `json:"id"`
	IncidentID string     `json:"incident_id"`
	Event      string     `json:"event" jsonschema:"opened, escalated, reminder, resolved, digest or test"`
	ChannelID  string     `json:"channel_id"`
	Channel    string     `json:"channel" jsonschema:"the channel's name when the message was queued"`
	Status     string     `json:"status" jsonschema:"pending, sent, failed (gave up after retrying) or dropped (deliberately not sent: muted, or the channel is gone)"`
	Attempts   int        `json:"attempts"`
	NotBefore  time.Time  `json:"not_before" jsonschema:"when the next attempt is due, while it is pending"`
	LastError  string     `json:"last_error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	SentAt     *time.Time `json:"sent_at,omitempty"`
}

func deliveryOf(d core.Delivery) delivery {
	return delivery{
		ID: d.ID, IncidentID: d.IncidentID, Event: d.Event, ChannelID: d.ChannelID, Channel: d.Channel,
		Status: d.Status, Attempts: d.Attempts, NotBefore: d.NotBefore, LastError: d.LastError,
		CreatedAt: d.CreatedAt, SentAt: d.SentAt,
	}
}

type listDeliveriesIn struct {
	Incident string `json:"incident,omitempty" jsonschema:"only the messages about this incident, by id"`
	Channel  string `json:"channel,omitempty" jsonschema:"only the messages sent to this channel, by id or name"`
	Status   string `json:"status,omitempty" jsonschema:"only the messages with this status: pending, sent, failed or dropped"`
	Limit    int    `json:"limit,omitempty" jsonschema:"how many to return, 1-500 (default 100)"`
	Before   int64  `json:"before,omitempty" jsonschema:"page backwards: only the messages with a smaller id — the id of the last one you have"`
}

type listDeliveriesOut struct {
	Context
	Deliveries []delivery `json:"deliveries"`
}

func (t *tools) listDeliveries(ctx context.Context, in listDeliveriesIn) (listDeliveriesOut, error) {
	if in.Status != "" && !slices.Contains(deliveryStatuses, in.Status) {
		return listDeliveriesOut{}, &engine.InvalidError{Reason: "status must be pending, sent, failed or dropped"}
	}
	if in.Limit < 0 || in.Limit > 500 {
		return listDeliveriesOut{}, &engine.InvalidError{Reason: "limit must be between 1 and 500"}
	}
	limit := in.Limit
	if limit == 0 {
		limit = 100
	}
	// The log has no status filter of its own, so a status is picked out
	// after reading: read the most the log will give, then stop at the limit.
	filter := store.DeliveryFilter{IncidentID: strings.TrimSpace(in.Incident), Limit: limit, BeforeID: in.Before}
	if in.Status != "" {
		filter.Limit = 500
	}
	if ref := strings.TrimSpace(in.Channel); ref != "" {
		channel, err := t.findChannel(ctx, ref)
		if err != nil {
			return listDeliveriesOut{}, err
		}
		filter.ChannelID = channel.ID
	}
	rows, err := t.e.Deliveries(ctx, filter)
	if err != nil {
		return listDeliveriesOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return listDeliveriesOut{}, err
	}
	out := listDeliveriesOut{Context: t.contextOf(settings), Deliveries: []delivery{}}
	for _, d := range rows {
		if (in.Status == "" || d.Status == in.Status) && len(out.Deliveries) < limit {
			out.Deliveries = append(out.Deliveries, deliveryOf(d))
		}
	}
	return out, nil
}

// ---------- admin ----------

// noIn is the input of a tool that takes nothing.
type noIn struct{}

type sendDigestOut struct {
	Queued bool `json:"queued" jsonschema:"true: the digest is on its way now, in the background"`
}

func (t *tools) sendDigest(ctx context.Context, _ noIn) (sendDigestOut, error) {
	if err := t.e.SendDigest(ctx); err != nil {
		return sendDigestOut{}, err
	}
	return sendDigestOut{Queued: true}, nil
}

type retryDeliveryIn struct {
	Delivery int64 `json:"delivery" jsonschema:"the id of a failed delivery, from list_deliveries"`
}

type deliveryOut struct {
	Context
	Delivery delivery `json:"delivery"`
}

func (t *tools) retryDelivery(ctx context.Context, in retryDeliveryIn) (deliveryOut, error) {
	if in.Delivery <= 0 {
		return deliveryOut{}, &engine.InvalidError{Reason: "delivery is required: the id of a delivery from list_deliveries"}
	}
	d, err := t.e.RetryDelivery(ctx, in.Delivery)
	if err != nil {
		return deliveryOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return deliveryOut{}, err
	}
	return deliveryOut{Context: t.contextOf(settings), Delivery: deliveryOf(d)}, nil
}

type listChannelTypesOut struct {
	Types []notify.Type `json:"types" jsonschema:"each kind of channel, with the settings — the fields — it needs"`
}

func (t *tools) listChannelTypes(_ context.Context, _ noIn) (listChannelTypesOut, error) {
	return listChannelTypesOut{Types: notify.Types()}, nil
}

type listChannelsOut struct {
	Context
	Channels []engine.ChannelView `json:"channels" jsonschema:"each channel with its settings, secrets masked, and how its recent deliveries went"`
}

func (t *tools) listChannels(ctx context.Context, _ noIn) (listChannelsOut, error) {
	channels, err := t.e.Channels(ctx, t.lang)
	if err != nil {
		return listChannelsOut{}, err
	}
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return listChannelsOut{}, err
	}
	return listChannelsOut{Context: t.contextOf(settings), Channels: channels}, nil
}

// findChannel resolves an id, or a name that matches exactly one channel.
func (t *tools) findChannel(ctx context.Context, ref string) (engine.ChannelView, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return engine.ChannelView{}, &engine.InvalidError{Reason: "channel is required: an id or a name from list_channels"}
	}
	channels, err := t.e.Channels(ctx, t.lang)
	if err != nil {
		return engine.ChannelView{}, err
	}
	var matches []engine.ChannelView
	for _, c := range channels {
		if c.ID == ref {
			return c, nil
		}
		if strings.EqualFold(c.Name, ref) {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return engine.ChannelView{}, &engine.InvalidError{Reason: fmt.Sprintf("no channel is called %q; see list_channels", ref)}
	}
	return engine.ChannelView{}, &engine.InvalidError{Reason: fmt.Sprintf("%d channels are called %q; use the id", len(matches), ref)}
}

type channelRefIn struct {
	Channel string `json:"channel" jsonschema:"the channel's id, or its name as shown in list_channels"`
}

type channelOut struct {
	Context
	Channel engine.ChannelView `json:"channel"`
}

func (t *tools) channelResult(ctx context.Context, view engine.ChannelView) (channelOut, error) {
	settings, err := t.e.Settings(ctx)
	if err != nil {
		return channelOut{}, err
	}
	return channelOut{Context: t.contextOf(settings), Channel: view}, nil
}

type addChannelIn struct {
	Name        string            `json:"name"`
	Type        string            `json:"type" jsonschema:"a channel type's id, from list_channel_types"`
	Config      map[string]string `json:"config,omitempty" jsonschema:"the type's settings by key, as list_channel_types describes them"`
	Topics      []string          `json:"topics,omitempty" jsonschema:"what it hears about: meters, dates, system, digest; everything when omitted"`
	MinSeverity string            `json:"min_severity,omitempty" jsonschema:"the least severe incident it wants: info, warning or critical; info when omitted"`
	Enabled     *bool             `json:"enabled,omitempty"`
}

func (t *tools) addChannel(ctx context.Context, in addChannelIn) (channelOut, error) {
	input := engine.ChannelInput{Name: &in.Name, Type: &in.Type, Config: in.Config, Enabled: in.Enabled}
	if in.Topics != nil {
		input.Topics = &in.Topics
	}
	if in.MinSeverity != "" {
		input.MinSeverity = &in.MinSeverity
	}
	view, err := t.e.CreateChannel(ctx, input, t.lang)
	if err != nil {
		return channelOut{}, err
	}
	return t.channelResult(ctx, view)
}

// updateChannelIn edits a channel. A field left out keeps its value. A
// channel's type cannot be changed.
type updateChannelIn struct {
	Channel     string            `json:"channel" jsonschema:"the channel's id, or its name as shown in list_channels"`
	Name        *string           `json:"name,omitempty"`
	Config      map[string]string `json:"config,omitempty" jsonschema:"the settings to change, by key; a secret left out keeps its stored value"`
	Topics      *[]string         `json:"topics,omitempty" jsonschema:"what it hears about: meters, dates, system, digest"`
	MinSeverity *string           `json:"min_severity,omitempty" jsonschema:"the least severe incident it wants: info, warning or critical"`
	Enabled     *bool             `json:"enabled,omitempty"`
}

func (t *tools) updateChannel(ctx context.Context, in updateChannelIn) (channelOut, error) {
	view, err := t.findChannel(ctx, in.Channel)
	if err != nil {
		return channelOut{}, err
	}
	updated, err := t.e.UpdateChannel(ctx, view.ID, engine.ChannelInput{
		Name: in.Name, Config: in.Config, Topics: in.Topics, MinSeverity: in.MinSeverity, Enabled: in.Enabled,
	}, t.lang)
	if err != nil {
		return channelOut{}, err
	}
	return t.channelResult(ctx, updated)
}

func (t *tools) deleteChannel(ctx context.Context, in channelRefIn) (deletedOut, error) {
	view, err := t.findChannel(ctx, in.Channel)
	if err != nil {
		return deletedOut{}, err
	}
	if err := t.e.DeleteChannel(ctx, view.ID); err != nil {
		return deletedOut{}, err
	}
	return deletedOut{Deleted: true}, nil
}

type testChannelOut struct {
	OK    bool   `json:"ok" jsonschema:"false when the platform refused the test message"`
	Error string `json:"error,omitempty"`
}

// testChannel sends a test message. A refusal by the platform is the
// answer, not a failure of the tool.
func (t *tools) testChannel(ctx context.Context, in channelRefIn) (testChannelOut, error) {
	view, err := t.findChannel(ctx, in.Channel)
	if err != nil {
		return testChannelOut{}, err
	}
	switch err := t.e.TestChannel(ctx, view.ID); {
	case err == nil:
		return testChannelOut{OK: true}, nil
	case engine.IsInvalid(err):
		return testChannelOut{OK: false, Error: err.Error()}, nil
	default:
		return testChannelOut{}, err
	}
}

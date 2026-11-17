// Package engine is where QuotaPulse makes up its mind.
//
// Everything else is plumbing around one loop:
//
//	sample → evaluate → reconcile → announce
//
// A sample is a fresh reading of a meter. Evaluating it produces conditions —
// "runs out in five days", "below the threshold" — which are plain facts
// about the present. Reconciling compares those facts with the incidents
// already open: a new condition opens one, a worse condition escalates it, a
// vanished condition resolves it. Only those edges are announced. A balance
// that has been low for a week was news once.
//
// Dates take the same path: a reminder whose day is near is a condition, and
// the incident it opens is what carries the notifications.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/store"
)

// Engine holds the pieces together.
type Engine struct {
	store  *store.Store
	fetch  *provider.Client
	sender *notify.Sender
	log    *slog.Logger
	now    func() time.Time
	mute   bool
	hub    *Hub

	// mu serializes evaluation and the incident state machine. Fetching —
	// the slow part — happens outside it.
	mu sync.Mutex
	// inflight holds the accounts being sampled, so a slow fetch is not
	// started a second time by the next tick.
	inflight sync.Map
	// wake nudges the background loop when something is due right now.
	wake chan struct{}
	// pushMu guards the lazily loaded browser push keys.
	pushMu sync.Mutex
}

// Options configures an Engine.
type Options struct {
	Store   *store.Store
	Fetcher *provider.Client
	Sender  *notify.Sender
	Log     *slog.Logger
	// Now is the clock; time.Now when nil. Tests and the demo pin it.
	Now func() time.Time
	// Mute records notifications without sending them. A migration runs
	// muted first, to see what would have gone out.
	Mute bool
}

// New builds an Engine.
func New(o Options) *Engine {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Fetcher == nil {
		o.Fetcher = provider.NewClient()
	}
	if o.Sender == nil {
		o.Sender = notify.NewSender()
	}
	return &Engine{
		store: o.Store, fetch: o.Fetcher, sender: o.Sender, log: o.Log, now: o.Now, mute: o.Mute,
		hub: newHub(), wake: make(chan struct{}, 1),
	}
}

// Store gives the few callers that move data in bulk — export, import, the
// demo — direct access to storage.
func (e *Engine) Store() *store.Store { return e.store }

// Now is the engine's clock.
func (e *Engine) Now() time.Time { return e.now() }

// Muted reports whether notifications are being recorded instead of sent.
func (e *Engine) Muted() bool { return e.mute }

// Changes is where the dashboard listens for "something changed".
func (e *Engine) Changes() *Hub { return e.hub }

// Wake returns the channel the background loop waits on.
func (e *Engine) Wake() <-chan struct{} { return e.wake }

// nudge wakes the background loop without blocking.
func (e *Engine) nudge() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Errors the API maps to status codes.
var (
	ErrNotFound = store.ErrNotFound
	ErrConflict = store.ErrConflict
)

// InvalidError is a request that cannot be carried out as given. Its text is
// meant for the person who made it.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return e.Reason }

func invalid(format string, args ...any) error {
	return &InvalidError{Reason: fmt.Sprintf(format, args...)}
}

// IsInvalid reports whether an error is the caller's to fix.
func IsInvalid(err error) bool {
	var target *InvalidError
	return errors.As(err, &target)
}

// Hub fans out change notifications. It carries no data: a message means
// "this part of the picture is stale, fetch it again", so a slow listener
// that misses one loses nothing.
type Hub struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
}

func newHub() *Hub { return &Hub{subs: map[chan string]struct{}{}} }

// Subscribe returns a channel of topics and a function to stop listening.
func (h *Hub) Subscribe() (<-chan string, func()) {
	ch := make(chan string, 16)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

// Publish tells every listener that a topic changed.
func (h *Hub) Publish(topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- topic:
		default: // the listener is behind; it will refetch anyway
		}
	}
}

// Topics the dashboard can react to.
const (
	ChangedMeters    = "meters"
	ChangedReminders = "reminders"
	ChangedIncidents = "incidents"
	ChangedActivity  = "activity"
	ChangedSettings  = "settings"
)

// toMap turns a typed value into the JSON-shaped map an incident stores.
func toMap(v any) map[string]any {
	raw, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// fromMap reads an incident's stored data back into its typed form.
func fromMap(m map[string]any, v any) {
	raw, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = json.Unmarshal(raw, v)
}

// Definition resolves a provider ID: the built-in catalog first, then the
// definitions saved from the dashboard.
func (e *Engine) Definition(ctx context.Context, id string) (provider.Definition, error) {
	if def, ok := provider.Builtin(id); ok {
		return def, nil
	}
	return e.store.CustomProvider(ctx, id)
}

// Definitions lists every provider definition, built-in first.
func (e *Engine) Definitions(ctx context.Context) ([]provider.Definition, error) {
	custom, err := e.store.CustomProviders(ctx)
	if err != nil {
		return nil, err
	}
	return append(provider.Catalog(), custom...), nil
}

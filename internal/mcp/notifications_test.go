package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/engine"
)

// receiver is a webhook endpoint that can be made to refuse.
type receiver struct {
	mu     sync.Mutex
	status int
	got    int
	url    string
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{status: http.StatusOK}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.got++
		w.WriteHeader(r.status)
	}))
	t.Cleanup(srv.Close)
	r.url = srv.URL
	return r
}

func (r *receiver) answer(status int) {
	r.mu.Lock()
	r.status = status
	r.mu.Unlock()
}

func (r *receiver) received() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.got
}

type channelView struct {
	ID, Name, Type string
	TypeName       string `json:"type_name"`
	Topics         []string
	MinSeverity    string `json:"min_severity"`
	Enabled        bool
	Config         map[string]string
}

type deliveryView struct {
	ID         int64      `json:"id"`
	IncidentID string     `json:"incident_id"`
	Event      string     `json:"event"`
	Channel    string     `json:"channel"`
	Status     string     `json:"status"`
	Attempts   int        `json:"attempts"`
	SentAt     *time.Time `json:"sent_at"`
}

type deliveries struct {
	Deliveries []deliveryView `json:"deliveries"`
}

func TestAnAgentReadsTheNotificationLogAndRunsChannels(t *testing.T) {
	w := newWorld(t)
	hook := newReceiver(t)
	admin, read := w.connect(core.ScopeAdmin), w.connect(core.ScopeRead, core.ScopeAct)

	kinds, result := call[struct {
		Types []struct {
			ID      string
			Managed bool
			Fields  []struct {
				Key    string
				Secret bool
			}
		}
	}](t, read, "list_channel_types", nil)
	if result.IsError {
		t.Fatalf("list_channel_types: %s", errorText(result))
	}
	var webhook bool
	for _, kind := range kinds.Types {
		if kind.ID != "webhook" || kind.Managed {
			continue
		}
		for _, field := range kind.Fields {
			webhook = webhook || field.Key == "url" && field.Secret
		}
	}
	if !webhook {
		t.Errorf("the webhook type is not described: %+v", kinds.Types)
	}
	if none, result := call[struct{ Channels []channelView }](t, admin, "list_channels", nil); result.IsError || len(none.Channels) != 0 || none.Channels == nil {
		t.Fatalf("list_channels on an empty install: %+v %s", none, errorText(result))
	}

	// A channel the platform refuses, then accepts.
	hook.answer(http.StatusInternalServerError)
	added, result := call[struct {
		Channel channelView `json:"channel"`
	}](t, admin, "add_channel", map[string]any{"name": "ops", "type": "webhook", "config": map[string]any{"url": hook.url}})
	if result.IsError {
		t.Fatalf("add_channel: %s", errorText(result))
	}
	c := added.Channel
	if c.ID == "" || c.Name != "ops" || c.Type != "webhook" || c.TypeName != "Webhook" || !c.Enabled || len(c.Topics) != 4 {
		t.Errorf("added = %+v", c)
	}
	if shown := c.Config["url"]; shown == "" || shown == hook.url {
		t.Errorf("the webhook URL is not masked: %q", shown)
	}
	if _, result := call[struct{ Channel channelView }](t, admin, "add_channel", map[string]any{"name": "ops", "type": "webhook", "config": map[string]any{"url": hook.url}}); !result.IsError {
		t.Error("added a channel twice")
	}
	if _, result := call[struct{ Channel channelView }](t, admin, "add_channel", map[string]any{"name": "fax", "type": "carrier-pigeon"}); !result.IsError {
		t.Error("added a channel of a type that does not exist")
	}
	turnedAway, result := call[struct {
		OK    bool
		Error string
	}](t, admin, "test_channel", map[string]any{"channel": "ops"})
	if result.IsError || turnedAway.OK || turnedAway.Error == "" {
		t.Fatalf("test_channel against a refusing platform: %+v %s", turnedAway, errorText(result))
	}
	hook.answer(http.StatusOK)
	if accepted, result := call[struct{ OK bool }](t, admin, "test_channel", map[string]any{"channel": "ops"}); result.IsError || !accepted.OK {
		t.Fatalf("test_channel: %+v %s", accepted, errorText(result))
	}

	changed, result := call[struct {
		Channel channelView `json:"channel"`
	}](t, admin, "update_channel", map[string]any{"channel": "ops", "min_severity": "critical", "topics": []string{"meters"}})
	if result.IsError || changed.Channel.MinSeverity != "critical" || len(changed.Channel.Topics) != 1 || changed.Channel.Topics[0] != "meters" {
		t.Fatalf("update_channel: %+v %s", changed, errorText(result))
	}
	if _, result := call[struct{ Channel channelView }](t, admin, "update_channel", map[string]any{"channel": "ops", "min_severity": "loud"}); !result.IsError {
		t.Error("set a severity that does not exist")
	}
	if _, result := call[struct{ OK bool }](t, admin, "test_channel", map[string]any{"channel": "nope"}); !result.IsError {
		t.Error("tested a channel that does not exist")
	}

	// Something goes wrong, and the log says whether anyone was told.
	if _, err := w.engine.Ingest(ctx, []engine.Push{{Meter: "wallet", Name: "Wallet", Kind: core.Balance, Unit: "USD", Value: ptr(0.0)}}); err != nil {
		t.Fatal(err)
	}
	queued, result := call[deliveries](t, read, "list_deliveries", map[string]any{"channel": "ops", "status": "pending"})
	if result.IsError || len(queued.Deliveries) != 1 {
		t.Fatalf("list_deliveries: %+v %s", queued, errorText(result))
	}
	if d := queued.Deliveries[0]; d.Event != "opened" || d.Channel != "ops" || d.Status != "pending" || d.SentAt != nil {
		t.Errorf("queued = %+v", d)
	}
	// The test message is in the log too, already sent; it is about no incident.
	if quiet, _ := call[deliveries](t, read, "list_deliveries", map[string]any{"incident": queued.Deliveries[0].IncidentID, "status": "sent"}); len(quiet.Deliveries) != 0 {
		t.Errorf("sent before dispatch: %+v", quiet.Deliveries)
	}
	if tests, _ := call[deliveries](t, read, "list_deliveries", map[string]any{"channel": "ops", "status": "sent"}); len(tests.Deliveries) != 1 || tests.Deliveries[0].Event != "test" {
		t.Errorf("the test message in the log: %+v", tests.Deliveries)
	}
	if n := w.engine.Dispatch(ctx); n < 1 {
		t.Fatalf("dispatched %d", n)
	}
	delivered, result := call[deliveries](t, read, "list_deliveries", map[string]any{"status": "sent", "incident": queued.Deliveries[0].IncidentID})
	if result.IsError || len(delivered.Deliveries) != 1 {
		t.Fatalf("after dispatch: %+v %s", delivered, errorText(result))
	}
	if d := delivered.Deliveries[0]; d.Attempts != 1 || d.SentAt == nil || d.ID != queued.Deliveries[0].ID {
		t.Errorf("delivered = %+v", d)
	}
	if hook.received() < 2 {
		t.Errorf("the channel received %d requests", hook.received())
	}
	if _, result := call[deliveries](t, read, "list_deliveries", map[string]any{"status": "loud"}); !result.IsError {
		t.Error("filtered by a status that does not exist")
	}
	if _, result := call[struct{ Delivery deliveryView }](t, admin, "retry_delivery", map[string]any{"delivery": delivered.Deliveries[0].ID}); !result.IsError {
		t.Error("retried a delivery that had already gone out")
	}

	if digest, result := call[struct{ Queued bool }](t, admin, "send_digest", nil); result.IsError || !digest.Queued {
		t.Errorf("send_digest: %+v %s", digest, errorText(result))
	}
	deleted, result := call[struct{ Deleted bool }](t, admin, "delete_channel", map[string]any{"channel": "ops"})
	if result.IsError || !deleted.Deleted {
		t.Fatalf("delete_channel: %s", errorText(result))
	}
	if left, _ := call[struct{ Channels []channelView }](t, admin, "list_channels", nil); len(left.Channels) != 0 {
		t.Errorf("still listed: %+v", left.Channels)
	}
	// None of this is for a token that merely acts or reads.
	for _, tool := range []string{"list_channels", "add_channel", "update_channel", "delete_channel", "test_channel", "send_digest", "retry_delivery"} {
		if !refused(t, read, tool, map[string]any{"name": "x", "type": "webhook", "channel": "ops", "delivery": 1}) {
			t.Errorf("a non-admin token called %s", tool)
		}
	}
}

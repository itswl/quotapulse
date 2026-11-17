package engine

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/secret"
	"github.com/itswl/quotapulse/internal/store"
)

var (
	ctx      = context.Background()
	shanghai = mustLocation("Asia/Shanghai")
)

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// world is a QuotaPulse with a clock that only moves when told to, a
// platform that answers what the test says, and a webhook that remembers
// everything it receives.
type world struct {
	t     *testing.T
	e     *Engine
	store *store.Store
	now   time.Time
	api   *fakeAPI
	hook  *fakeHook
	ops   ChannelView
}

// fakeAPI is a platform. It answers with whatever meters it was last given.
type fakeAPI struct {
	mu     sync.Mutex
	srv    *httptest.Server
	status int
	body   string
	calls  int
}

func (a *fakeAPI) set(status int, body string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status, a.body = status, body
}

// balance makes the platform report one CNY balance.
func (a *fakeAPI) balance(value float64) {
	raw, _ := json.Marshal(map[string]any{"meters": []map[string]any{
		{"key": "balance", "name": "Balance", "name_zh": "余额", "kind": "balance", "unit": "CNY", "value": value},
	}})
	a.set(200, string(raw))
}

// fakeHook is a webhook receiver.
type fakeHook struct {
	mu       sync.Mutex
	srv      *httptest.Server
	status   int
	received []hookMessage
}

type hookMessage struct {
	Event    string        `json:"event"`
	Severity string        `json:"severity"`
	Topic    string        `json:"topic"`
	Title    string        `json:"title"`
	Text     string        `json:"text"`
	Facts    []notify.Fact `json:"facts"`
	Link     string        `json:"link"`
}

func (h *fakeHook) fail(status int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status = status
}

// take returns what arrived since the last call.
func (h *fakeHook) take() []hookMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.received
	h.received = nil
	return out
}

func newHook(t *testing.T) *fakeHook {
	t.Helper()
	h := &fakeHook{status: 200}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.status != 200 {
			w.WriteHeader(h.status)
			return
		}
		var msg hookMessage
		_ = json.Unmarshal(raw, &msg)
		h.received = append(h.received, msg)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func newWorld(t *testing.T, options ...func(*Options)) *world {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "quotapulse.db"), secret.FromPassphrase("test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	w := &world{t: t, store: st, now: time.Date(2026, 11, 16, 10, 0, 0, 0, shanghai)}

	w.api = &fakeAPI{status: 200, body: `{"meters":[]}`}
	w.api.srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.api.mu.Lock()
		defer w.api.mu.Unlock()
		w.api.calls++
		rw.WriteHeader(w.api.status)
		_, _ = rw.Write([]byte(w.api.body))
	}))
	t.Cleanup(w.api.srv.Close)
	w.hook = newHook(t)

	opts := Options{
		Store: st, Now: func() time.Time { return w.now },
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, option := range options {
		option(&opts)
	}
	w.e = New(opts)

	// Chinese notifications, Shanghai time, and no quiet hours or digest
	// unless a test turns them on.
	settings := core.DefaultSettings()
	settings.Timezone, settings.Language, settings.BaseCurrency = "Asia/Shanghai", "zh", "CNY"
	settings.FX = map[string]float64{"USD": 7}
	settings.Quiet.Enabled, settings.Digest.Enabled = false, false
	settings.PublicURL = "https://pulse.example"
	w.must(st.SaveSettings(ctx, settings))

	w.must(st.SaveProvider(ctx, provider.Definition{
		ID: "fake", Name: "Fake", NameZH: "假平台",
		Fields:  []provider.Field{{Key: "api_key", Label: "API Key", Secret: true}, {Key: "team", Label: "Team", Optional: true}},
		Request: provider.Request{URL: w.api.srv.URL, Auth: provider.Auth{Type: provider.AuthBearer}},
		Extract: `.meters[]`,
	}, w.now))

	w.ops = w.channel("ops", w.hook)
	return w
}

func (w *world) must(err error) {
	w.t.Helper()
	if err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) channel(name string, hook *fakeHook) ChannelView {
	w.t.Helper()
	kind := "webhook"
	view, err := w.e.CreateChannel(ctx, ChannelInput{Name: &name, Type: &kind, Config: map[string]string{"url": hook.srv.URL}}, "zh")
	w.must(err)
	return view
}

func (w *world) settings(change func(*core.Settings)) {
	w.t.Helper()
	s, err := w.store.Settings(ctx)
	w.must(err)
	change(&s)
	w.must(w.store.SaveSettings(ctx, s))
}

func (w *world) advance(d time.Duration) { w.now = w.now.Add(d) }

// at moves the clock to a local time on a date.
func (w *world) at(year int, month time.Month, day, hour, minute int) {
	w.now = time.Date(year, month, day, hour, minute, 0, 0, shanghai)
}

func (w *world) account(name string) AccountView {
	w.t.Helper()
	prov := "fake"
	view, err := w.e.CreateAccount(ctx, AccountInput{Name: &name, Provider: &prov, Credential: map[string]string{"api_key": "sk-test-0001"}}, "zh")
	w.must(err)
	return view
}

func (w *world) sample(id string) {
	w.t.Helper()
	w.must(w.e.SampleAccount(ctx, id))
}

// seed gives a pushed meter a history without judging it: readings one hour
// apart ending now. spend(ago) is what went in the hour that ended that long ago.
func (w *world) seed(key string, kind core.Kind, unit string, hours int, final float64, spend func(ago int) float64) core.Meter {
	w.t.Helper()
	meter := core.Meter{Key: key, Name: key, Kind: kind, Unit: unit}
	w.must(w.store.CreateMeter(ctx, &meter, w.now.Add(-time.Duration(hours)*time.Hour)))
	readings := make([]core.Reading, hours+1)
	value := final
	for ago := 0; ago <= hours; ago++ {
		readings[hours-ago] = core.Reading{At: w.now.Add(-time.Duration(ago) * time.Hour), Value: value}
		if kind == core.Counter {
			value -= spend(ago)
		} else {
			value += spend(ago)
		}
	}
	w.must(w.store.ImportReadings(ctx, &meter, readings))
	return meter
}

// push reports a reading for a pushed meter now, which judges it.
func (w *world) push(key string, value float64) {
	w.t.Helper()
	_, err := w.e.Ingest(ctx, []Push{{Meter: key, Value: &value}})
	w.must(err)
}

func (w *world) rules(meterID string, rules core.Rules) {
	w.t.Helper()
	_, err := w.e.UpdateMeter(ctx, meterID, MeterInput{Rules: &rules}, "zh")
	w.must(err)
}

func (w *world) dispatch() []hookMessage {
	w.t.Helper()
	w.e.Dispatch(ctx)
	return w.hook.take()
}

func (w *world) open() []core.Incident {
	w.t.Helper()
	open, err := w.store.OpenIncidents(ctx)
	w.must(err)
	return open
}

func (w *world) deliveries() []core.Delivery {
	w.t.Helper()
	all, err := w.store.Deliveries(ctx, store.DeliveryFilter{Limit: 500})
	w.must(err)
	return all
}

func titles(messages []hookMessage) []string {
	out := make([]string, len(messages))
	for i, m := range messages {
		out[i] = m.Title
	}
	return out
}

// expect checks that exactly these messages arrived, by title.
func expect(t *testing.T, got []hookMessage, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d messages %q, want %d %q", len(got), titles(got), len(want), want)
	}
	for i := range want {
		if got[i].Title != want[i] {
			t.Errorf("message %d: got %q, want %q", i, got[i].Title, want[i])
		}
	}
}

func contains(t *testing.T, text string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			t.Errorf("%q does not mention %q", text, fragment)
		}
	}
}

func ptr[T any](v T) *T { return &v }

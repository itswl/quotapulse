package engine

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/notify"
)

// pushService stands in for a browser vendor's push service.
type pushService struct {
	mu     sync.Mutex
	srv    *httptest.Server
	seen   map[string]int // deliveries by endpoint path
	status map[string]int // answer per endpoint path; 201 when absent
}

func newPushService(t *testing.T, w *world) *pushService {
	t.Helper()
	p := &pushService{seen: map[string]int{}, status: map[string]int{}}
	// Browsers only hand out https endpoints, and only those are accepted.
	p.srv = httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		p.mu.Lock()
		defer p.mu.Unlock()
		p.seen[r.URL.Path]++
		status := p.status[r.URL.Path]
		if status == 0 {
			status = http.StatusCreated
		}
		rw.WriteHeader(status)
	}))
	t.Cleanup(p.srv.Close)
	w.e.sender.HTTP = p.srv.Client() // trusts the test certificate; plain http still works
	return p
}

func (p *pushService) count(path string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen[path]
}

func (p *pushService) answer(path string, status int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status[path] = status
}

// device is a browser's subscription for an endpoint path.
func (p *pushService) device(t *testing.T, path string) PushSubscription {
	t.Helper()
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	sub := PushSubscription{Endpoint: p.srv.URL + path}
	sub.Keys.P256dh = base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes())
	sub.Keys.Auth = base64.RawURLEncoding.EncodeToString(auth)
	return sub
}

func TestBrowserPush(t *testing.T) {
	w := newWorld(t)
	push := newPushService(t, w)

	// The key pair is made once and kept: a browser's subscription is bound to it.
	key, err := w.e.PushKey(ctx)
	w.must(err)
	again, _ := w.e.PushKey(ctx)
	restarted, err := New(Options{Store: w.store, Now: func() time.Time { return w.now }}).PushKey(ctx)
	w.must(err)
	if key == "" || again != key || restarted != key {
		t.Fatalf("keys: %q %q %q", key, again, restarted)
	}

	laptop, err := w.e.SubscribePush(ctx, "Chrome · macOS", push.device(t, "/send/laptop"), "zh")
	w.must(err)
	if laptop.Type != notify.TypeWebPush || laptop.Name != "Chrome · macOS" || laptop.TypeName != "浏览器推送" || !laptop.Enabled || len(laptop.Topics) != 4 {
		t.Fatalf("channel = %+v", laptop)
	}
	if strings.Contains(laptop.Config["endpoint"], "laptop") {
		t.Errorf("the endpoint is shown: %v", laptop.Config)
	}
	// A browser finds itself in the list by hashing its own endpoint.
	sum := sha256.Sum256([]byte(push.device(t, "/send/laptop").Endpoint))
	if laptop.Device != hex.EncodeToString(sum[:16]) || len(laptop.Device) != 32 {
		t.Errorf("device = %q", laptop.Device)
	}
	// The same browser subscribing again is still one device.
	refreshed, err := w.e.SubscribePush(ctx, "renamed by the browser", push.device(t, "/send/laptop"), "zh")
	w.must(err)
	if refreshed.ID != laptop.ID || refreshed.Name != "Chrome · macOS" {
		t.Fatalf("a second subscription made a second channel: %+v", refreshed)
	}
	// Two devices that describe themselves the same way stay apart.
	phone, err := w.e.SubscribePush(ctx, "Chrome · macOS", push.device(t, "/send/phone"), "zh")
	w.must(err)
	if phone.ID == laptop.ID || phone.Name != "Chrome · macOS 2" {
		t.Fatalf("phone = %+v", phone)
	}
	unnamed, err := w.e.SubscribePush(ctx, "  ", push.device(t, "/send/tablet"), "zh")
	w.must(err)
	if unnamed.Name != "浏览器" {
		t.Errorf("default name = %q", unnamed.Name)
	}

	bad := push.device(t, "/x")
	bad.Endpoint = "http://insecure.example/x"
	if _, err := w.e.SubscribePush(ctx, "x", bad, "zh"); !IsInvalid(err) {
		t.Errorf("a plain-http endpoint: %v", err)
	}
	bad = push.device(t, "/x")
	bad.Keys.Auth = ""
	if _, err := w.e.SubscribePush(ctx, "x", bad, "zh"); !IsInvalid(err) {
		t.Errorf("a subscription without keys: %v", err)
	}
	kind := notify.TypeWebPush
	if _, err := w.e.CreateChannel(ctx, ChannelInput{Name: ptr("form"), Type: &kind, Config: map[string]string{"endpoint": "https://x.example"}}, "zh"); !IsInvalid(err) {
		t.Errorf("a browser channel made from a form: %v", err)
	}

	// An incident reaches every device, and the webhook as before.
	w.empty("wallet")
	expect(t, w.dispatch(), "余额已用尽 · wallet")
	for _, path := range []string{"/send/laptop", "/send/phone", "/send/tablet"} {
		if push.count(path) != 1 {
			t.Errorf("%s received %d pushes", path, push.count(path))
		}
	}
	w.must(w.e.TestChannel(ctx, phone.ID))
	if push.count("/send/phone") != 2 {
		t.Error("the test push did not arrive")
	}

	// The laptop's browser unsubscribed. Its channel simply ends: no retries,
	// and no incident about a "failing channel" for something that is gone.
	push.answer("/send/laptop", http.StatusGone)
	w.advance(time.Minute)
	w.empty("second")
	w.dispatch()
	channels, _ := w.e.Channels(ctx, "zh")
	for _, c := range channels {
		if c.ID == laptop.ID {
			t.Fatal("a channel whose browser is gone was kept")
		}
	}
	if len(channels) != 3 { // ops, phone, tablet
		t.Fatalf("channels = %d", len(channels))
	}
	for _, inc := range w.open() {
		if inc.Kind == core.IncidentDelivery {
			t.Fatalf("an incident was opened for a browser that unsubscribed: %+v", inc)
		}
	}
	dropped := 0
	for _, d := range w.deliveries() {
		if d.ChannelID == laptop.ID && d.Status == core.DeliveryDropped && strings.Contains(d.LastError, "expired") {
			dropped++
		}
	}
	if dropped != 1 {
		t.Errorf("the last delivery to the gone browser should be recorded as dropped: %d", dropped)
	}
	// Pressing "test" on a dead browser removes it too, and says why.
	push.answer("/send/tablet", http.StatusNotFound)
	if err := w.e.TestChannel(ctx, unnamed.ID); !IsInvalid(err) || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("testing a dead browser: %v", err)
	}
	if channels, _ := w.e.Channels(ctx, "zh"); len(channels) != 2 {
		t.Fatalf("channels after the test = %d", len(channels))
	}
}

func TestImportedPushKeysDoNotReplaceExistingOnes(t *testing.T) {
	w := newWorld(t)
	foreign, err := notify.NewVAPID()
	w.must(err)

	// Nothing here yet: the imported pair becomes the pair.
	used, err := w.e.ImportPushKeys(ctx, foreign)
	w.must(err)
	if key, _ := w.e.PushKey(ctx); !used || key != foreign.Public {
		t.Fatalf("imported keys not in use: %v %q", used, key)
	}
	exported, err := w.e.ExportPushKeys(ctx)
	w.must(err)
	if exported == nil || exported.Private != foreign.Private {
		t.Fatalf("exported = %+v", exported)
	}
	// Importing the same pair again is fine; a different one is refused,
	// because browsers are already bound to the first.
	if used, _ := w.e.ImportPushKeys(ctx, foreign); !used {
		t.Error("the same pair was refused")
	}
	other, _ := notify.NewVAPID()
	if used, _ := w.e.ImportPushKeys(ctx, other); used {
		t.Error("existing keys were replaced")
	}
	if key, _ := w.e.PushKey(ctx); key != foreign.Public {
		t.Error("the key in use changed")
	}

	fresh := newWorld(t)
	if keys, err := fresh.e.ExportPushKeys(ctx); err != nil || keys != nil {
		t.Errorf("an install that never used push has no keys to export: %+v %v", keys, err)
	}
}

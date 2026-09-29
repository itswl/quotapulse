package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/store"
)

type fakeStore struct {
	store.Store
	deleted []string
}

func (f *fakeStore) DeletePushSubscription(_ context.Context, endpoint string) error {
	f.deleted = append(f.deleted, endpoint)
	return nil
}

// browserSubscription returns a subscription with a real P256 client key; webpush-go
// rejects keys that are not valid curve points before sending anything.
func browserSubscription(t *testing.T, endpoint string) model.PushSubscription {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	return model.PushSubscription{
		Endpoint: endpoint,
		P256dh:   base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		Auth:     base64.RawURLEncoding.EncodeToString(make([]byte, 16)),
	}
}

// TestSendToSignsSingleMailtoSubject pins the claim Apple validates: webpush-go adds
// "mailto:" itself, so a prefixed constant would sign "mailto:mailto:..." and Apple
// answers 403 BadJwtToken while Chrome and Firefox keep working.
func TestSendToSignsSingleMailtoSubject(t *testing.T) {
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	m := New(&fakeStore{Store: store.Null()})
	if err := m.SendTo(context.Background(), browserSubscription(t, server.URL+"/push/1"), "t", "b"); err != nil {
		t.Fatalf("SendTo: %v", err)
	}

	token := strings.TrimPrefix(strings.SplitN(authorization, ",", 2)[0], "vapid t=")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("unexpected Authorization header: %q", authorization)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("parse JWT claims: %v", err)
	}
	if got, want := claims["sub"], "mailto:push@users.noreply.github.com"; got != want {
		t.Errorf("sub claim = %v, want %s", got, want)
	}
	if got, want := claims["aud"], server.URL; got != want {
		t.Errorf("aud claim = %v, want %s", got, want)
	}
}

func TestSendToSurfacesPushServiceReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"reason":"BadJwtToken"}`))
	}))
	defer server.Close()

	m := New(&fakeStore{Store: store.Null()})
	err := m.SendTo(context.Background(), browserSubscription(t, server.URL+"/push/1"), "t", "b")
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "BadJwtToken") {
		t.Fatalf("error should carry the status and the push service reason, got %v", err)
	}
}

func TestSendToPrunesGoneEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer server.Close()

	st := &fakeStore{Store: store.Null()}
	endpoint := server.URL + "/push/1"
	err := New(st).SendTo(context.Background(), browserSubscription(t, endpoint), "t", "b")
	if err == nil || !strings.Contains(err.Error(), "pruned") {
		t.Fatalf("410 should report a pruned registration, got %v", err)
	}
	if len(st.deleted) != 1 || st.deleted[0] != endpoint {
		t.Fatalf("410 should delete the subscription, deleted=%v", st.deleted)
	}
}

package notify

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/itswl/quotapulse/internal/core"
)

// browser plays the receiving end of Web Push: it owns the keys a real
// browser would, and can read what was sent to it.
type browser struct {
	private *ecdh.PrivateKey
	auth    []byte
}

func newBrowser(t *testing.T) *browser {
	t.Helper()
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return &browser{private: private, auth: auth}
}

func (b *browser) config(endpoint string) map[string]string {
	return map[string]string{
		"endpoint": endpoint,
		"p256dh":   base64.RawURLEncoding.EncodeToString(b.private.PublicKey().Bytes()),
		"auth":     base64.RawURLEncoding.EncodeToString(b.auth),
	}
}

// decrypt opens an aes128gcm push message as RFC 8291 describes.
func (b *browser) decrypt(t *testing.T, body []byte) []byte {
	t.Helper()
	if len(body) < 21 {
		t.Fatalf("message too short: %d bytes", len(body))
	}
	salt, idLen := body[:16], int(body[20])
	senderPublic, ciphertext := body[21:21+idLen], body[21+idLen:]
	sender, err := ecdh.P256().NewPublicKey(senderPublic)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := b.private.ECDH(sender)
	if err != nil {
		t.Fatal(err)
	}
	expand := func(secret, salt []byte, info string, length int) []byte {
		prk, err := hkdf.Extract(sha256.New, secret, salt)
		if err != nil {
			t.Fatal(err)
		}
		out, err := hkdf.Expand(sha256.New, prk, info, length)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	info := "WebPush: info\x00" + string(b.private.PublicKey().Bytes()) + string(senderPublic)
	ikm := expand(shared, b.auth, info, 32)
	key := expand(ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	nonce := expand(ikm, salt, "Content-Encoding: nonce\x00", 12)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("the message does not decrypt with the browser's keys: %v", err)
	}
	// plaintext, a 0x02 delimiter, then padding
	return bytes.TrimRight(plain, "\x00")[:len(bytes.TrimRight(plain, "\x00"))-1]
}

func pushSender(t *testing.T) *Sender {
	t.Helper()
	keys, err := NewVAPID()
	if err != nil {
		t.Fatal(err)
	}
	s := sender()
	s.SetVAPID(keys)
	return s
}

func TestWebPushReachesTheBrowser(t *testing.T) {
	var got seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = seen{path: r.URL.Path, header: r.Header.Clone(), body: body}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	device := newBrowser(t)
	s := pushSender(t)

	m := message()
	m.Path = "/meters/mtr_1"
	// The endpoint must be https for a real browser; the driver itself does not care.
	if err := s.Send(context.Background(), TypeWebPush, device.config(srv.URL+"/send/device-secret-0001"), m); err != nil {
		t.Fatal(err)
	}
	if got.header.Get("Content-Encoding") != "aes128gcm" || got.header.Get("TTL") != "86400" || got.header.Get("Urgency") != "normal" {
		t.Errorf("headers = %v", got.header)
	}
	if !strings.HasPrefix(got.header.Get("Authorization"), "vapid t=") {
		t.Errorf("Authorization = %q", got.header.Get("Authorization"))
	}
	// Nothing readable travels in the clear.
	if bytes.Contains(got.body, []byte("DeepSeek")) {
		t.Fatal("the notification was sent unencrypted")
	}
	var payload map[string]string
	if err := json.Unmarshal(device.decrypt(t, got.body), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["title"] != m.Title || payload["url"] != "/meters/mtr_1" || payload["tag"] != "inc_1" || payload["severity"] != "warning" {
		t.Errorf("payload = %v", payload)
	}
	if !strings.Contains(payload["body"], "当前余额: 41.00 CNY") {
		t.Errorf("body = %q", payload["body"])
	}

	// Critical is urgent; a recovery is not, whatever the incident was.
	critical := m
	critical.Severity = core.Critical
	_ = s.Send(context.Background(), TypeWebPush, device.config(srv.URL), critical)
	if got.header.Get("Urgency") != "high" {
		t.Errorf("critical urgency = %q", got.header.Get("Urgency"))
	}
	critical.Event = core.EventResolved
	_ = s.Send(context.Background(), TypeWebPush, device.config(srv.URL), critical)
	if got.header.Get("Urgency") != "normal" {
		t.Errorf("resolved urgency = %q", got.header.Get("Urgency"))
	}
}

func TestWebPushOutcomes(t *testing.T) {
	device := newBrowser(t)
	s := pushSender(t)
	cases := []struct {
		status          int
		reply           string
		permanent, gone bool
		want            string
	}{
		{404, "", true, true, "the browser's subscription has expired (HTTP 404)"},
		{410, "push subscription has unsubscribed or expired.", true, true, "the browser's subscription has expired (HTTP 410: push subscription has unsubscribed or expired.)"},
		{403, `{"reason":"BadJwtToken"}`, true, false, `the push service refused the message: HTTP 403: {"reason":"BadJwtToken"}`},
		{429, "", false, false, "the push service answered HTTP 429"},
		{503, "", false, false, "the push service answered HTTP 503"},
	}
	for _, c := range cases {
		srv, _ := serve(t, c.status, c.reply)
		err := s.Send(context.Background(), TypeWebPush, device.config(srv.URL+"/x"), message())
		if err == nil || err.Error() != c.want || IsPermanent(err) != c.permanent || IsGone(err) != c.gone {
			t.Errorf("HTTP %d: err = %v (permanent %v, gone %v)", c.status, err, IsPermanent(err), IsGone(err))
		}
	}

	// Keys that are not keys: permanent, and found before anything is sent.
	bad := device.config("https://push.example/x")
	bad["p256dh"] = "not-a-key"
	if err := s.Send(context.Background(), TypeWebPush, bad, message()); err == nil || !IsPermanent(err) || IsGone(err) {
		t.Errorf("a broken subscription: %v", err)
	}
	// No key pair on the server yet.
	if err := sender().Send(context.Background(), TypeWebPush, device.config("https://push.example/x"), message()); err == nil || !IsPermanent(err) {
		t.Errorf("without VAPID keys: %v", err)
	}
	// The endpoint is the secret: it never appears in an error.
	err := s.Send(context.Background(), TypeWebPush, device.config("https://127.0.0.1:1/send/device-secret-0001"), message())
	if err == nil || strings.Contains(err.Error(), "device-secret-0001") {
		t.Errorf("err = %v", err)
	}
}

func TestWebPushIsNotAFormChannel(t *testing.T) {
	typ := mustType(t, TypeWebPush)
	if !typ.Managed {
		t.Error("a browser channel is created by the browser, not from a form")
	}
	masked := typ.MaskConfig(map[string]string{"endpoint": "https://fcm.googleapis.com/fcm/send/abc-secret-0001", "p256dh": "BPubKeyBPubKeyBPubKey", "auth": "authsecret0001"})
	// Enough to tell a Chrome from a Safari; nothing that could deliver.
	if masked["endpoint"] != "https://fcm.googleapis.com/••••" || strings.Contains(masked["auth"], "secret") {
		t.Errorf("masked = %v", masked)
	}
	if _, err := typ.CheckConfig(map[string]string{"endpoint": "ftp://x", "p256dh": "a", "auth": "b"}); err == nil {
		t.Error("a non-http endpoint was accepted")
	}
}

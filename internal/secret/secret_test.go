package secret

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSealRoundTrip(t *testing.T) {
	k := FromPassphrase("correct horse battery staple")
	sealed, err := k.Seal([]byte(`{"api_key":"sk-test-0001"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(sealed), "v1:") || bytes.Contains(sealed, []byte("sk-test")) {
		t.Fatalf("sealed = %s", sealed)
	}
	opened, err := k.Open(sealed)
	if err != nil || string(opened) != `{"api_key":"sk-test-0001"}` {
		t.Fatalf("opened = %s, %v", opened, err)
	}

	// Sealing twice must not give the same bytes: equal ciphertexts would
	// reveal that two accounts share a key.
	again, _ := k.Seal([]byte(`{"api_key":"sk-test-0001"}`))
	if bytes.Equal(sealed, again) {
		t.Error("the nonce was reused")
	}
}

func TestOpenRejectsWhatItDidNotSeal(t *testing.T) {
	k := FromPassphrase("one")
	sealed, _ := k.Seal([]byte("payload"))

	if _, err := FromPassphrase("two").Open(sealed); !errors.Is(err, ErrUnreadable) {
		t.Errorf("another key opened it: %v", err)
	}
	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-3] ^= 1
	if _, err := k.Open(tampered); !errors.Is(err, ErrUnreadable) {
		t.Errorf("a tampered value opened: %v", err)
	}
	for _, junk := range []string{"", "plaintext", "v1:", "v1:!!!", "v2:abcd"} {
		if _, err := k.Open([]byte(junk)); !errors.Is(err, ErrUnreadable) {
			t.Errorf("Open(%q) = %v", junk, err)
		}
	}
}

func TestSealJSON(t *testing.T) {
	k := FromPassphrase("x")
	sealed, err := k.SealJSON(map[string]string{"url": "https://hook.example/abc"})
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]string
	if err := k.OpenJSON(sealed, &back); err != nil || back["url"] != "https://hook.example/abc" {
		t.Fatalf("back = %v, %v", back, err)
	}
}

func TestKeyFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	first, err := Load("", dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, keyFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %v", info.Mode().Perm())
	}
	sealed, _ := first.Seal([]byte("payload"))

	// A restart reads the same key back.
	second, err := Load("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := second.Open(sealed); err != nil || string(opened) != "payload" {
		t.Fatalf("after restart: %s, %v", opened, err)
	}

	// A damaged key file must stop the start, not be silently replaced:
	// replacing it would orphan every stored credential.
	if err := os.WriteFile(filepath.Join(dir, keyFile), []byte("oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("", dir); err == nil {
		t.Fatal("a damaged key file was accepted")
	}
}

func TestPassphraseIsStable(t *testing.T) {
	sealed, _ := FromPassphrase("same").Seal([]byte("payload"))
	if opened, err := FromPassphrase("same").Open(sealed); err != nil || string(opened) != "payload" {
		t.Fatalf("the same passphrase must give the same key: %v", err)
	}
}

func TestSession(t *testing.T) {
	k := FromPassphrase("x")
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	expires := now.Add(30 * 24 * time.Hour)
	token := k.Session("hunter2", expires)

	got, ok := k.CheckSession("hunter2", token, now)
	if !ok || !got.Equal(expires) {
		t.Fatalf("CheckSession = %v, %v", got, ok)
	}
	if _, ok := k.CheckSession("hunter2", token, expires); ok {
		t.Error("an expired session passed")
	}
	if _, ok := k.CheckSession("changed", token, now); ok {
		t.Error("a session survived a password change")
	}
	if _, ok := FromPassphrase("y").CheckSession("hunter2", token, now); ok {
		t.Error("a session from another install passed")
	}

	// Pushing the expiry forward must break the signature.
	body, signature, _ := strings.Cut(token, ".")
	forged := k.Session("hunter2", expires.Add(365*24*time.Hour))
	forgedBody, _, _ := strings.Cut(forged, ".")
	if _, ok := k.CheckSession("hunter2", forgedBody+"."+signature, now); ok {
		t.Error("a forged expiry passed")
	}
	for _, junk := range []string{"", ".", body, body + ".", "a.b", token + "x"} {
		if _, ok := k.CheckSession("hunter2", junk, now); ok {
			t.Errorf("CheckSession(%q) passed", junk)
		}
	}
	if k.Session("hunter2", expires) == token {
		t.Error("two sessions must not be identical")
	}
}

func TestTokens(t *testing.T) {
	a, b := NewToken(), NewToken()
	if a == b || !strings.HasPrefix(a, TokenPrefix) || len(a) != len(TokenPrefix)+32 {
		t.Fatalf("tokens: %q %q", a, b)
	}
	if HashToken(a) == HashToken(b) || len(HashToken(a)) != 64 {
		t.Error("hashes")
	}
	if HashToken(a) != HashToken(a) {
		t.Error("hashing must be deterministic")
	}
}

func TestEqual(t *testing.T) {
	if !Equal("same", "same") || Equal("same", "other") || Equal("", "x") || Equal("x", "") {
		t.Error("Equal")
	}
	if !Equal("", "") {
		t.Error("two empty strings are equal; the caller must refuse an empty secret")
	}
}

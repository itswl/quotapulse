package store

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"github.com/fernet/fernet-go"
)

// encryptedPrefix marks a value as already encrypted; the ciphertext format is
// this prefix plus a Fernet token. This is the live format, not legacy baggage:
// production databases already hold values written this way, and changing the
// prefix or algorithm makes existing config unreadable.
const encryptedPrefix = "enc:v1:"

// cipher handles encryption of project_config.api_key and
// email_config.password.
//
// A nil key means CONFIG_ENCRYPTION_KEY is unset and values pass through
// unchanged — encryption is optional: no key must neither stop the service
// nor corrupt existing plaintext.
type cipher struct {
	key *fernet.Key
}

// newCipher derives the key: if CONFIG_ENCRYPTION_KEY is itself a valid Fernet
// key use it directly, otherwise treat it as a passphrase and take SHA-256 as
// the key. Neither the branching nor the derivation may change — stored
// ciphertexts were encrypted under exactly these rules.
func newCipher(rawKey string) *cipher {
	raw := strings.TrimSpace(rawKey)
	if raw == "" {
		return &cipher{}
	}

	var key fernet.Key
	if decoded, ok := decodeFernetKey(raw); ok {
		copy(key[:], decoded)
	} else {
		// a passphrase-derived key is the raw SHA-256 digest: base64-encoding
		// urlsafe and then having Fernet decode it is an identity transform, skipped
		// here
		sum := sha256.Sum256([]byte(raw))
		copy(key[:], sum[:])
	}
	return &cipher{key: &key}
}

// decodeFernetKey tells whether raw is a valid Fernet key, i.e. 32 bytes of
// urlsafe base64.
//
// It deliberately avoids fernet.DecodeKey: that also accepts 64-char
// hexadecimal, so a hexadecimal CONFIG_ENCRYPTION_KEY would classify as a
// key instead of a passphrase, changing the derived key and locking out
// existing ciphertext.
//
// The checks below are exactly the ones existing data relies on, and the
// leniency is intentional: characters outside the alphabet are ignored (a
// key with one '!' still counts), but padding is required — 43 characters
// without '=' classifies as a passphrase, not a key.
func decodeFernetKey(raw string) ([]byte, bool) {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '=':
			b.WriteRune(r)
		case r == '-', r == '+':
			b.WriteByte('+')
		case r == '_', r == '/':
			b.WriteByte('/')
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(b.String())
	if err != nil || len(decoded) != 32 {
		return nil, false
	}
	return decoded, true
}

// enabled means a key is configured: writes encrypt, and plaintext reads get rewritten.
func (c *cipher) enabled() bool { return c != nil && c.key != nil }

// isEncrypted tells whether a value is ciphertext in this project's format.
func isEncrypted(value string) bool { return strings.HasPrefix(value, encryptedPrefix) }

// encrypt turns plaintext into ciphertext. Empty and already-encrypted values
// pass through unchanged so re-saving one config never double-wraps it.
func (c *cipher) encrypt(value string) string {
	if value == "" || isEncrypted(value) || !c.enabled() {
		return value
	}
	token, err := fernet.EncryptAndSign([]byte(value), c.key)
	if err != nil {
		// Fernet only fails when the RNG is unavailable; better to store as-is than lose the config
		return value
	}
	return encryptedPrefix + string(token)
}

// decrypt turns ciphertext back into plaintext.
//
// Undecryptable values return as-is instead of erroring: with a changed or
// missing key the page shows ciphertext blobs, still better than the whole
// config list failing to load.
func (c *cipher) decrypt(value string) string {
	if !isEncrypted(value) || !c.enabled() {
		return value
	}
	token := strings.TrimPrefix(value, encryptedPrefix)
	// ttl 0 skips issue-time checks; Fernet tokens carry timestamps and expiry checks would suddenly invalidate old config
	plain := fernet.VerifyAndDecrypt([]byte(token), 0, []*fernet.Key{c.key})
	if plain == nil {
		return value
	}
	return string(plain)
}

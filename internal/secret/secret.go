// Package secret keeps what must stay secret: credentials at rest, session
// cookies, API tokens.
//
// Everything hangs off one master key. It comes from QUOTAPULSE_SECRET when
// that is set, and otherwise from a key file created next to the database on
// first start. The key file means a stolen database backup is useless on its
// own; it does not protect against someone who can read the whole data
// directory, and nothing here pretends otherwise.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	keyFile = "secret.key"
	// sealedPrefix versions the ciphertext format.
	sealedPrefix = "v1:"
	// A passphrase is stretched once at startup. The salt is fixed because
	// the same passphrase must give the same key on every start, on any
	// machine the data directory is moved to.
	stretchSalt       = "quotapulse/master/v1"
	stretchIterations = 200_000
)

// ErrUnreadable means a sealed value could not be opened: the master key is
// not the one it was sealed with.
var ErrUnreadable = errors.New("sealed with a different secret")

// Keyring holds the keys derived from the master key.
type Keyring struct {
	aead    cipher.AEAD
	session []byte
	// salt stretches the dashboard password (see stretch).
	salt []byte

	mu        sync.Mutex
	password  string // the password last stretched, and what that came to
	stretched []byte
}

// Load builds the keyring from a passphrase, or from the key file in dataDir
// when the passphrase is empty, creating the file if it does not exist.
func Load(passphrase, dataDir string) (*Keyring, error) {
	var master []byte
	if passphrase != "" {
		stretched, err := pbkdf2.Key(sha256.New, passphrase, []byte(stretchSalt), stretchIterations, 32)
		if err != nil {
			return nil, err
		}
		master = stretched
	} else {
		fromFile, err := loadOrCreateKeyFile(filepath.Join(dataDir, keyFile))
		if err != nil {
			return nil, err
		}
		master = fromFile
	}
	return fromMaster(master)
}

// FromPassphrase is Load without the file fallback; tests use it.
func FromPassphrase(passphrase string) *Keyring {
	k, err := Load(passphrase, "")
	if err != nil {
		panic(err) // unreachable with a non-empty passphrase
	}
	return k
}

func fromMaster(master []byte) (*Keyring, error) {
	derive := func(purpose string) ([]byte, error) {
		return hkdf.Key(sha256.New, master, nil, "quotapulse/"+purpose, 32)
	}
	dataKey, err := derive("data")
	if err != nil {
		return nil, err
	}
	sessionKey, err := derive("session")
	if err != nil {
		return nil, err
	}
	salt, err := derive("password")
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Keyring{aead: aead, session: sessionKey, salt: salt}, nil
}

func loadOrCreateKeyFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		key, decodeErr := hex.DecodeString(strings.TrimSpace(string(raw)))
		if decodeErr != nil || len(key) != 32 {
			return nil, fmt.Errorf("%s is not a valid key file; restore it from a backup or set QUOTAPULSE_SECRET", path)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	// O_EXCL: two processes starting at once must not each write their own key.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return loadOrCreateKeyFile(path)
		}
		return nil, fmt.Errorf("create key file: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(hex.EncodeToString(key) + "\n"); err != nil {
		return nil, fmt.Errorf("write key file: %w", err)
	}
	return key, nil
}

// Seal encrypts a value for storage.
func (k *Keyring) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := k.aead.Seal(nonce, nonce, plaintext, nil)
	return []byte(sealedPrefix + base64.StdEncoding.EncodeToString(sealed)), nil
}

// Open decrypts a value sealed by Seal.
func (k *Keyring) Open(sealed []byte) ([]byte, error) {
	encoded, ok := strings.CutPrefix(string(sealed), sealedPrefix)
	if !ok {
		return nil, ErrUnreadable
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) < k.aead.NonceSize() {
		return nil, ErrUnreadable
	}
	nonce, ciphertext := raw[:k.aead.NonceSize()], raw[k.aead.NonceSize():]
	plaintext, err := k.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrUnreadable
	}
	return plaintext, nil
}

// SealJSON seals a value as JSON.
func (k *Keyring) SealJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return k.Seal(raw)
}

// OpenJSON opens a value sealed by SealJSON.
func (k *Keyring) OpenJSON(sealed []byte, v any) error {
	raw, err := k.Open(sealed)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// Session returns a signed session token that expires at the given time.
//
// The password, stretched, is mixed into the signing key, so changing the
// password signs everyone out without keeping a list of sessions.
func (k *Keyring) Session(password string, expires time.Time) string {
	payload := make([]byte, 16)
	binary.BigEndian.PutUint64(payload, uint64(expires.Unix()))
	_, _ = rand.Read(payload[8:])
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + base64.RawURLEncoding.EncodeToString(k.sign(password, body))
}

// CheckSession verifies a session token and returns when it expires.
func (k *Keyring) CheckSession(password, token string, now time.Time) (time.Time, bool) {
	body, signature, ok := strings.Cut(token, ".")
	if !ok {
		return time.Time{}, false
	}
	got, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(got, k.sign(password, body)) {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || len(payload) != 16 {
		return time.Time{}, false
	}
	expires := time.Unix(int64(binary.BigEndian.Uint64(payload)), 0)
	if !now.Before(expires) {
		return time.Time{}, false
	}
	return expires, true
}

func (k *Keyring) sign(password, body string) []byte {
	key := hmac.New(sha256.New, k.session)
	key.Write(k.stretch(password))
	mac := hmac.New(sha256.New, key.Sum(nil))
	mac.Write([]byte(body))
	return mac.Sum(nil)
}

// CheckPassword reports whether a guess at signing in is the password. The
// guess is stretched as the password is and the two compared in constant
// time, so how long the answer takes says nothing about the password, not
// even its length, and every guess costs a stretch.
func (k *Keyring) CheckPassword(guess, password string) bool {
	return hmac.Equal(k.stretchOf(guess), k.stretch(password))
}

// stretch is the dashboard password as it is used: stretched as a
// passphrase is, with a salt of this installation's own. Nothing is checked
// against anything cheaper — not a session cookie, not a guess — so even
// with the master key and a cookie in hand, every guess at the password
// costs a stretch. The password is set once, so its stretch is kept.
func (k *Keyring) stretch(password string) []byte {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stretched == nil || k.password != password {
		k.password, k.stretched = password, k.stretchOf(password)
	}
	return k.stretched
}

func (k *Keyring) stretchOf(password string) []byte {
	stretched, err := pbkdf2.Key(sha256.New, password, k.salt, stretchIterations, 32)
	if err != nil {
		panic(err) // unreachable: SHA-256, a 32-byte salt and a 32-byte key are allowed everywhere
	}
	return stretched
}

// TokenPrefix marks an API token, so one can be recognised in a config file
// or a leaked log.
const TokenPrefix = "qp_"

var tokenEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NewToken returns a fresh API token: 160 random bits.
func NewToken() string {
	raw := make([]byte, 20)
	_, _ = rand.Read(raw)
	return TokenPrefix + tokenEncoding.EncodeToString(raw)
}

// HashToken is what is stored for a token. A token is random and long, so a
// plain hash is enough: there is nothing to brute-force.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

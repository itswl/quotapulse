package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The browser exchanges the API key once for an HttpOnly session cookie. The
// cookie is not the key: it is an HMAC over an expiry stamped with the key as
// the secret, so script on the page can never read the credential, and rotating
// WEB_API_KEY invalidates every session minted under the old one.
const (
	sessionCookieName = "quotapulse_session"
	sessionVersion    = "v1"
	// Long enough that the key is typed once a month, short enough that a
	// forgotten browser stops working without an operator revoking anything.
	sessionTTL = 30 * 24 * time.Hour
	// A session in its last week is reissued on use, so an active dashboard
	// never asks again and an abandoned one still expires.
	sessionRenewWithin = 7 * 24 * time.Hour
)

// sessionToken mints the cookie value for one key and expiry.
func sessionToken(key string, expires time.Time) string {
	stamp := strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(sessionVersion + "|" + stamp))
	return sessionVersion + ":" + stamp + ":" + hex.EncodeToString(mac.Sum(nil))
}

// sessionSigner reports which configured key signed token and when it expires.
// Multiple keys are accepted at once during rotation, the same as the header.
func sessionSigner(token string, keys []string, now time.Time) (string, time.Time, bool) {
	parts := strings.Split(token, ":")
	if len(parts) != 3 || parts[0] != sessionVersion {
		return "", time.Time{}, false
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || now.Unix() >= expires {
		return "", time.Time{}, false
	}
	signature, err := hex.DecodeString(parts[2])
	if err != nil {
		return "", time.Time{}, false
	}
	for _, key := range keys {
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(sessionVersion + "|" + parts[1]))
		if subtle.ConstantTimeCompare(signature, mac.Sum(nil)) == 1 {
			return key, time.Unix(expires, 0), true
		}
	}
	return "", time.Time{}, false
}

// setSessionCookie writes the cookie and returns its expiry, so the caller can
// tell the browser when it will have to ask for the key again.
func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, key string) time.Time {
	expires := time.Now().Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionToken(key, expires),
		Path:     "/api",
		Expires:  expires,
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
	return expires
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/api",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

// requestIsHTTPS covers both a direct TLS listener and the usual reverse proxy
// that terminates TLS in front of the service.
func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// safeMethod lists the methods that change nothing and so need no CSRF guard.
func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// sameSiteRequest rejects a cookie-authenticated write that a browser marked as
// cross-site. SameSite=Strict already withholds the cookie there; this is the
// second lock for browsers or proxies that ignore it. Requests without the
// header are non-browser clients, which carry no ambient cookie to abuse.
func sameSiteRequest(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	default:
		return false
	}
}

// handleSessionCreate trades a valid X-API-Key for the HttpOnly cookie. It is
// mounted under /api/, so the middleware has already checked the header.
func (s *Server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	token := extractAPIKey(r)
	for _, key := range s.Settings.APIKeys() {
		if subtle.ConstantTimeCompare([]byte(token), []byte(key)) == 1 {
			expires := s.setSessionCookie(w, r, key)
			writeJSON(w, http.StatusOK, map[string]any{
				"status":     "ok",
				"expires_at": expires.UTC().Format(time.RFC3339),
			})
			return
		}
	}
	fail(w, http.StatusUnauthorized, "API key is invalid or missing")
}

// handleSessionDelete ends the browser session; the in-memory key, if the page
// still holds one, is cleared separately by the client.
func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

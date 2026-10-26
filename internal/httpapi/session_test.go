package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/config"
)

func TestSessionTokenRoundTripsAndExpires(t *testing.T) {
	now := time.Now()
	keys := []string{"first", "second"}
	token := sessionToken("second", now.Add(time.Hour))

	signer, expires, ok := sessionSigner(token, keys, now)
	if !ok || signer != "second" {
		t.Fatalf("round trip failed: signer=%q ok=%v", signer, ok)
	}
	if !expires.Equal(time.Unix(now.Add(time.Hour).Unix(), 0)) {
		t.Fatalf("unexpected expiry: %v", expires)
	}

	if _, _, ok := sessionSigner(token, keys, now.Add(2*time.Hour)); ok {
		t.Fatal("an expired session was accepted")
	}
	if _, _, ok := sessionSigner(token, []string{"rotated"}, now); ok {
		t.Fatal("a session signed by a key no longer configured was accepted")
	}
	if _, _, ok := sessionSigner(token+"0", keys, now); ok {
		t.Fatal("a tampered session was accepted")
	}
	if _, _, ok := sessionSigner("garbage", keys, now); ok {
		t.Fatal("a malformed session was accepted")
	}
}

func TestSessionCookieAuthenticatesAPIWithoutAHeader(t *testing.T) {
	_, handler := newServer(t, nil)

	login := request(t, handler, http.MethodPost, "/api/session", "", true)
	if login.Code != http.StatusOK {
		t.Fatalf("login: got %d, body %s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != sessionCookieName || cookie.Value == "" {
		t.Fatalf("unexpected cookie: %+v", cookie)
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/api" {
		t.Fatalf("cookie is not HttpOnly/Strict//api: %+v", cookie)
	}
	if cookie.MaxAge <= 0 {
		t.Fatalf("cookie was not persistent: %+v", cookie)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/features", nil)
	req.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("cookie-only request: got %d, body %s", recorder.Code, recorder.Body.String())
	}
}

func TestSessionCookieRejectsTamperingAndRotation(t *testing.T) {
	_, handler := newServer(t, nil)
	login := request(t, handler, http.MethodPost, "/api/session", "", true)
	cookie := login.Result().Cookies()[0]

	tampered := *cookie
	tampered.Value += "0"
	req := httptest.NewRequest(http.MethodGet, "/api/features", nil)
	req.AddCookie(&tampered)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("tampered cookie: got %d, want 401", recorder.Code)
	}

	_, rotated := newServer(t, func(settings *config.Settings) { settings.WebAPIKey = "rotated-key" })
	req = httptest.NewRequest(http.MethodGet, "/api/features", nil)
	req.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	rotated.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("rotated key: got %d, want 401", recorder.Code)
	}
}

func TestSessionCookieRefusesCrossSiteWrites(t *testing.T) {
	_, handler := newServer(t, nil)
	login := request(t, handler, http.MethodPost, "/api/session", "", true)
	cookie := login.Result().Cookies()[0]

	req := httptest.NewRequest(http.MethodPost, "/api/refresh", nil)
	req.AddCookie(cookie)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-site write: got %d, want 403", recorder.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/refresh", nil)
	req.AddCookie(cookie)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code == http.StatusForbidden || recorder.Code == http.StatusUnauthorized {
		t.Fatalf("same-origin write was refused: %d, body %s", recorder.Code, recorder.Body.String())
	}
}

func TestSessionLogoutClearsTheCookie(t *testing.T) {
	_, handler := newServer(t, nil)
	login := request(t, handler, http.MethodPost, "/api/session", "", true)
	cookie := login.Result().Cookies()[0]

	req := httptest.NewRequest(http.MethodDelete, "/api/session", nil)
	req.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("logout: got %d, body %s", recorder.Code, recorder.Body.String())
	}
	cleared := recorder.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge >= 0 || cleared[0].Value != "" {
		t.Fatalf("logout did not clear the cookie: %+v", cleared)
	}
}

func TestSessionNeedsAValidKey(t *testing.T) {
	_, handler := newServer(t, nil)
	login := request(t, handler, http.MethodPost, "/api/session", "", false)
	if login.Code != http.StatusUnauthorized {
		t.Fatalf("login without a key: got %d, want 401", login.Code)
	}
}

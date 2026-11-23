package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/core"
)

// A site with no QUOTAPULSE_PASSWORD: the password is chosen in the dashboard.
func newUnclaimedSite(t *testing.T, change ...func(*Server)) *site {
	return newSite(t, append([]func(*Server){func(api *Server) { api.Password = "" }}, change...)...)
}

func (s *site) session() map[string]any {
	var info map[string]any
	s.browser().get("/api/v1/session", &info)
	return info
}

func TestANewInstallIsSetUpByWhoeverOpensItFirst(t *testing.T) {
	s := newUnclaimedSite(t)
	if info := s.session(); info["password_set"] != false || info["setup"] != "open" || info["fresh"] != true {
		t.Fatalf("a new install: %v", info)
	}

	first := s.browser()
	if status, body := first.do("POST", "/api/v1/setup", map[string]string{"password": "short"}); status != 422 || errorCode(body) != "password_short" {
		t.Fatalf("a short password: %d %s", status, body)
	}
	status, body := first.do("POST", "/api/v1/setup", map[string]string{"password": "correct horse", "timezone": "Asia/Shanghai", "language": "zh"})
	if status != 200 {
		t.Fatalf("setup: %d %s", status, body)
	}
	// Signed in by it, with the time zone and language chosen.
	var settings core.Settings
	first.get("/api/v1/settings", &settings)
	if settings.Timezone != "Asia/Shanghai" || settings.Language != "zh" || settings.BaseCurrency != "CNY" {
		t.Fatalf("settings after setup: %+v", settings)
	}

	// Once there is a password, it is the only way in.
	if status, body := s.browser().do("POST", "/api/v1/setup", map[string]string{"password": "another password"}); status != 409 || errorCode(body) != "password_set" {
		t.Fatalf("a second setup: %d %s", status, body)
	}
	if info := s.session(); info["password_set"] != true || info["setup"] != nil {
		t.Fatalf("after setup: %v", info)
	}
	c := s.browser()
	if status, _ := c.do("POST", "/api/v1/session", map[string]string{"password": "wrong password"}); status != 401 {
		t.Fatalf("a wrong password: %d", status)
	}
	if status, body := c.do("POST", "/api/v1/session", map[string]string{"password": "correct horse"}); status != 200 {
		t.Fatalf("sign in with the chosen password: %d %s", status, body)
	}
}

func TestLaterOrWithDataSetupTakesTheCode(t *testing.T) {
	later := newUnclaimedSite(t, func(api *Server) { api.Started = time.Now().Add(-2 * time.Hour) })
	if info := later.session(); info["setup"] != "code" || info["fresh"] != true {
		t.Fatalf("an hour after the start: %v", info)
	}

	withData := newUnclaimedSite(t)
	withData.withToken(core.ScopeRead) // something configured
	if info := withData.session(); info["setup"] != "code" || info["fresh"] != false {
		t.Fatalf("with something configured: %v", info)
	}

	ctx := context.Background()
	c := later.browser()
	for _, code := range []string{"", "nothing-like-it"} {
		if status, body := c.do("POST", "/api/v1/setup", map[string]string{"password": "correct horse", "code": code}); status != 403 || errorCode(body) != "setup_code" {
			t.Fatalf("setup with code %q: %d %s", code, status, body)
		}
	}
	expired, err := NewSetupCode(ctx, later.engine.Store(), time.Now().Add(-25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := c.do("POST", "/api/v1/setup", map[string]string{"password": "correct horse", "code": expired}); status != 403 {
		t.Fatalf("an expired code: %d", status)
	}
	code, err := NewSetupCode(ctx, later.engine.Store(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Copied out of a log with a stray space, or in capitals: still the code.
	if status, body := c.do("POST", "/api/v1/setup", map[string]string{"password": "correct horse", "code": " " + strings.ToUpper(code) + " ", "timezone": "Asia/Tokyo"}); status != 200 {
		t.Fatalf("setup with the code: %d %s", status, body)
	}
	if _, ok, _ := later.engine.Store().SetupCode(ctx); ok {
		t.Error("the code must be used up")
	}
}

func TestTheSetupOfAnInstallWithDataLeavesItsSettings(t *testing.T) {
	s := newUnclaimedSite(t)
	s.withToken(core.ScopeRead)
	ctx := context.Background()
	code, err := NewSetupCode(ctx, s.engine.Store(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c := s.browser()
	if status, body := c.do("POST", "/api/v1/setup", map[string]string{"password": "correct horse", "code": code, "timezone": "Asia/Tokyo", "language": "zh"}); status != 200 {
		t.Fatalf("setup: %d %s", status, body)
	}
	var settings core.Settings
	c.get("/api/v1/settings", &settings)
	if settings.Timezone != "UTC" || settings.Language != "en" {
		t.Fatalf("a new password must not change an install's settings: %+v", settings)
	}
}

func TestChangeThePassword(t *testing.T) {
	s := newUnclaimedSite(t)
	me, other := s.browser(), s.browser()
	if status, body := me.do("POST", "/api/v1/setup", map[string]string{"password": "correct horse"}); status != 200 {
		t.Fatalf("setup: %d %s", status, body)
	}
	if status, _ := other.do("POST", "/api/v1/session", map[string]string{"password": "correct horse"}); status != 200 {
		t.Fatal("the other browser signs in")
	}
	var info map[string]any
	me.get("/api/v1/session", &info)
	if info["password_from_env"] != false {
		t.Fatalf("session info: %v", info)
	}

	if status, body := me.do("PUT", "/api/v1/password", map[string]string{"current": "wrong password", "password": "battery staple"}); status != 403 || errorCode(body) != "wrong_password" {
		t.Fatalf("with a wrong current password: %d %s", status, body)
	}
	if status, body := me.do("PUT", "/api/v1/password", map[string]string{"current": "correct horse", "password": "short"}); status != 422 || errorCode(body) != "password_short" {
		t.Fatalf("a short new password: %d %s", status, body)
	}
	admin := s.withToken(core.ScopeAdmin)
	if status, _ := admin.do("PUT", "/api/v1/password", map[string]string{"current": "correct horse", "password": "battery staple"}); status != 403 {
		t.Fatalf("a token must not change the password: %d", status)
	}
	if status, body := me.do("PUT", "/api/v1/password", map[string]string{"current": "correct horse", "password": "battery staple"}); status != 200 {
		t.Fatalf("change: %d %s", status, body)
	}
	// The one who changed it stays in; everyone else is out.
	if status, _ := me.do("GET", "/api/v1/overview", nil); status != 200 {
		t.Errorf("the session that changed the password: %d", status)
	}
	if status, _ := other.do("GET", "/api/v1/overview", nil); status != 401 {
		t.Errorf("another session after the change: %d", status)
	}
	if status, _ := s.browser().do("POST", "/api/v1/session", map[string]string{"password": "battery staple"}); status != 200 {
		t.Error("sign in with the new password")
	}
}

func TestAPasswordFromTheEnvironmentStaysThere(t *testing.T) {
	s := newSite(t)
	c := s.signedIn()
	var info map[string]any
	c.get("/api/v1/session", &info)
	if info["password_from_env"] != true || info["setup"] != nil {
		t.Fatalf("session info: %v", info)
	}
	if status, body := c.do("PUT", "/api/v1/password", map[string]string{"current": password, "password": "battery staple"}); status != 409 || errorCode(body) != "password_from_env" {
		t.Fatalf("change: %d %s", status, body)
	}
	if status, body := s.browser().do("POST", "/api/v1/setup", map[string]string{"password": "battery staple"}); status != 409 || errorCode(body) != "password_set" {
		t.Fatalf("setup: %d %s", status, body)
	}
}

func TestAForgottenPasswordEndsEverySession(t *testing.T) {
	s := newUnclaimedSite(t)
	c := s.browser()
	if status, body := c.do("POST", "/api/v1/setup", map[string]string{"password": "correct horse"}); status != 200 {
		t.Fatalf("setup: %d %s", status, body)
	}
	// As `quotapulse reset-password` does.
	if err := s.engine.Store().ForgetPassword(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status, body := c.do("GET", "/api/v1/overview", nil); status != 401 || errorCode(body) != "no_password" {
		t.Fatalf("a session after the password is forgotten: %d %s", status, body)
	}
	if info := s.session(); info["password_set"] != false || info["setup"] != "open" {
		t.Fatalf("session info: %v", info)
	}
}

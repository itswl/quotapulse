package api

import (
	"context"
	"crypto/hmac"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/secret"
	"github.com/itswl/quotapulse/internal/store"
)

// The password, and how there comes to be one.
//
// QUOTAPULSE_PASSWORD, when it is set, is the password: as before, and the
// dashboard cannot change it. Otherwise the password is chosen in the
// dashboard, the first time it is opened, and kept in the database as its
// verifier (secret.Keyring.Verifier); it can be changed under Settings.
//
// Choosing it is open to whoever opens a new install — nothing configured
// yet — within an hour of the server starting, the way most things one
// installs oneself work. Later than that, or with something already in the
// database, it takes the one-time code the server writes to its log while
// there is no password, or that `quotapulse reset-password` prints: an
// install left unclaimed on the internet is not anybody's for the taking.

const (
	// setupWindow is how long after a start a new install stays open.
	setupWindow = time.Hour
	// setupCodeLife is how long a setup code works.
	setupCodeLife = 24 * time.Hour
	// minPassword is the shortest password the dashboard accepts.
	minPassword = 8
)

// verifier returns what the password is checked against: QUOTAPULSE_PASSWORD
// stretched, or the one chosen in the dashboard; nil while there is none.
func (s *Server) verifier(ctx context.Context) ([]byte, error) {
	if s.Password != "" {
		s.envOnce.Do(func() { s.envVerifier = s.Keys.Verifier(s.Password) })
		return s.envVerifier, nil
	}
	return s.Engine.Store().PasswordVerifier(ctx)
}

// setup says how a password can be chosen while there is none: "open" to
// anyone, or "code" with the one-time code. fresh is true for an install
// with nothing configured, whose setup also asks its time zone and language.
func (s *Server) setup(ctx context.Context) (mode string, fresh bool, err error) {
	fresh, err = s.Engine.Store().Empty(ctx)
	if err != nil {
		return "", false, err
	}
	if fresh && time.Since(s.Started) < setupWindow {
		return "open", true, nil
	}
	return "code", fresh, nil
}

// NewSetupCode issues the one-time code that lets its holder choose the
// password, in place of any earlier one, and returns it.
func NewSetupCode(ctx context.Context, st *store.Store, now time.Time) (string, error) {
	code := secret.NewSetupCode()
	return code, st.SetSetupCode(ctx, store.SetupCode{Hash: secret.HashToken(code), Expires: now.Add(setupCodeLife)})
}

func (s *Server) checkSetupCode(ctx context.Context, code string, now time.Time) (bool, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return false, nil
	}
	stored, ok, err := s.Engine.Store().SetupCode(ctx)
	if err != nil || !ok || !now.Before(stored.Expires) {
		return false, err
	}
	return hmac.Equal([]byte(secret.HashToken(code)), []byte(stored.Hash)), nil
}

// handleSetup chooses the password where there is none, and signs the one
// who chose it in. On a new install it sets the time zone and the language
// as well.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		fail(w, http.StatusForbidden, "cross_site", "cross-site request refused")
		return
	}
	client, now := clientKey(r), time.Now()
	if wait := s.logins.blocked(client, now); wait > 0 {
		w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
		fail(w, http.StatusTooManyRequests, "too_many_attempts", "too many attempts; wait a minute and try again")
		return
	}
	var body struct {
		Password string `json:"password"`
		Code     string `json:"code"`
		Timezone string `json:"timezone"`
		Language string `json:"language"`
	}
	if !decode(w, r, &body) {
		return
	}
	if len([]rune(body.Password)) < minPassword {
		fail(w, http.StatusUnprocessableEntity, "password_short", fmt.Sprintf("choose a password of at least %d characters", minPassword))
		return
	}

	ctx := r.Context()
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if verifier, err := s.verifier(ctx); err != nil {
		s.failWith(w, r, err)
		return
	} else if verifier != nil {
		fail(w, http.StatusConflict, "password_set", "a password has already been chosen; sign in with it")
		return
	}
	mode, fresh, err := s.setup(ctx)
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	if mode == "code" {
		ok, err := s.checkSetupCode(ctx, body.Code, now)
		if err != nil {
			s.failWith(w, r, err)
			return
		}
		if !ok {
			s.logins.failed(client, now)
			s.log().Warn("setup refused: no valid setup code", "client", client)
			fail(w, http.StatusForbidden, "setup_code", "setting this install up takes the link from the server's log")
			return
		}
	}
	if fresh && (body.Timezone != "" || body.Language != "") {
		if err := s.startSettings(ctx, body.Timezone, body.Language); err != nil {
			s.failWith(w, r, err)
			return
		}
	}

	verifier := s.Keys.Verifier(body.Password)
	if err := s.Engine.Store().SetPasswordVerifier(ctx, verifier); err != nil {
		s.failWith(w, r, err)
		return
	}
	s.log().Info("the dashboard password was chosen", "client", client)
	s.setSession(w, r, verifier)
	s.learnAddress(r)
	writeJSON(w, http.StatusOK, map[string]any{"signed_in": true})
}

// startSettings gives a new install the time zone and language chosen while
// setting it up. One that is not known is left as it was.
func (s *Server) startSettings(ctx context.Context, timezone, language string) error {
	settings, err := s.Engine.Settings(ctx)
	if err != nil {
		return err
	}
	if _, err := time.LoadLocation(timezone); timezone != "" && err == nil {
		settings.Timezone = timezone
	}
	switch language {
	case "zh":
		settings.Language, settings.BaseCurrency = "zh", "CNY"
	case "en":
		settings.Language, settings.BaseCurrency = "en", "USD"
	}
	_, err = s.Engine.UpdateSettings(ctx, settings)
	return err
}

// handleChangePassword replaces a password chosen in the dashboard. It asks
// for the current one, signs every other session out, and keeps the one that
// asked signed in.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if p, _ := r.Context().Value(principalKey{}).(principal); !p.Session {
		fail(w, http.StatusForbidden, "forbidden", "the password is changed from the dashboard, not with a token")
		return
	}
	if s.Password != "" {
		fail(w, http.StatusConflict, "password_from_env", "the password is QUOTAPULSE_PASSWORD: change it there, then restart")
		return
	}
	client, now := clientKey(r), time.Now()
	if wait := s.logins.blocked(client, now); wait > 0 {
		w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
		fail(w, http.StatusTooManyRequests, "too_many_attempts", "too many attempts; wait a minute and try again")
		return
	}
	var body struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	ctx := r.Context()
	verifier, err := s.verifier(ctx)
	if err != nil {
		s.failWith(w, r, err)
		return
	}
	// Not 401: that would read as the session having ended.
	if !s.Keys.CheckPassword(body.Current, verifier) {
		s.logins.failed(client, now)
		fail(w, http.StatusForbidden, "wrong_password", "that is not the current password")
		return
	}
	if len([]rune(body.Password)) < minPassword {
		fail(w, http.StatusUnprocessableEntity, "password_short", fmt.Sprintf("choose a password of at least %d characters", minPassword))
		return
	}
	next := s.Keys.Verifier(body.Password)
	if err := s.Engine.Store().SetPasswordVerifier(ctx, next); err != nil {
		s.failWith(w, r, err)
		return
	}
	s.log().Info("the dashboard password was changed; other sessions are signed out", "client", client)
	s.setSession(w, r, next)
	writeJSON(w, http.StatusOK, map[string]any{"changed": true})
}

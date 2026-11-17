package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"quotapulse/internal/core"
	"quotapulse/internal/engine"
	"quotapulse/internal/secret"
	"quotapulse/internal/store"
)

const password = "correct horse"

// site is a QuotaPulse behind a real HTTP server.
type site struct {
	t      *testing.T
	server *httptest.Server
	engine *engine.Engine
	api    *Server
}

func newSite(t *testing.T, change ...func(*Server)) *site {
	t.Helper()
	keys := secret.FromPassphrase("test")
	st, err := store.Open(filepath.Join(t.TempDir(), "quotapulse.db"), keys)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := engine.New(engine.Options{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	api := &Server{
		Engine: e, Keys: keys, Password: password, Version: "test",
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Assets: fstest.MapFS{
			"index.html":            {Data: []byte("<!doctype html><title>QuotaPulse</title>")},
			"assets/app-abc123.js":  {Data: []byte("console.log(1)")},
			"manifest.webmanifest":  {Data: []byte("{}")},
			"icons/icon-192.png":    {Data: []byte("png")},
			"assets/app-abc123.css": {Data: []byte("body{}")},
		},
	}
	for _, fn := range change {
		fn(api)
	}
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	return &site{t: t, server: server, engine: e, api: api}
}

// client is one caller: a browser with a cookie jar, or a script with a token.
type client struct {
	t     *testing.T
	site  *site
	http  *http.Client
	token string
	// headers are added to every request, to play a hostile page.
	headers map[string]string
}

func (s *site) browser() *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: s.t, site: s, http: &http.Client{Jar: jar}}
}

func (s *site) signedIn() *client {
	c := s.browser()
	if status, body := c.do("POST", "/api/v1/session", map[string]string{"password": password}); status != 200 {
		s.t.Fatalf("sign in: %d %s", status, body)
	}
	return c
}

func (s *site) withToken(scopes ...string) *client {
	token, err := s.engine.CreateToken(context.Background(), engine.TokenInput{Name: "t-" + strings.Join(scopes, "-"), Scopes: scopes})
	if err != nil {
		s.t.Fatal(err)
	}
	return &client{t: s.t, site: s, http: &http.Client{}, token: token.Secret}
}

func (c *client) do(method, path string, body any) (int, []byte) {
	c.t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.site.server.URL+path, reader)
	if err != nil {
		c.t.Fatal(err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// get decodes a successful JSON response.
func (c *client) get(path string, into any) {
	c.t.Helper()
	status, body := c.do("GET", path, nil)
	if status != 200 {
		c.t.Fatalf("GET %s: %d %s", path, status, body)
	}
	if err := json.Unmarshal(body, into); err != nil {
		c.t.Fatalf("GET %s: %v in %s", path, err, body)
	}
}

func errorCode(body []byte) string {
	var envelope struct {
		Error struct{ Code, Message string }
	}
	_ = json.Unmarshal(body, &envelope)
	return envelope.Error.Code
}

func TestProbesAreOpenAndEverythingElseIsNot(t *testing.T) {
	s := newSite(t)
	anonymous := s.browser()
	for _, path := range []string{"/healthz", "/readyz", "/api/v1/session"} {
		if status, body := anonymous.do("GET", path, nil); status != 200 {
			t.Errorf("%s: %d %s", path, status, body)
		}
	}
	for _, path := range []string{
		"/api/v1/overview", "/api/v1/meters", "/api/v1/accounts", "/api/v1/reminders", "/api/v1/incidents",
		"/api/v1/deliveries", "/api/v1/spend", "/api/v1/providers", "/api/v1/channels", "/api/v1/tokens",
		"/api/v1/settings", "/api/v1/system", "/api/v1/export", "/api/v1/stream", "/metrics",
	} {
		if status, body := anonymous.do("GET", path, nil); status != 401 || errorCode(body) != "unauthorized" {
			t.Errorf("%s: %d %s", path, status, body)
		}
	}
	if status, body := anonymous.do("GET", "/api/v1/nope", nil); status != 404 || errorCode(body) != "not_found" {
		t.Errorf("an unknown endpoint must be JSON 404, not the dashboard: %d %s", status, body)
	}
}

func TestSignInAndOut(t *testing.T) {
	s := newSite(t)
	c := s.browser()

	var info map[string]any
	c.get("/api/v1/session", &info)
	if info["signed_in"] != false || info["password_set"] != true || info["version"] != "test" {
		t.Fatalf("info = %v", info)
	}
	if _, told := info["timezone"]; told {
		t.Error("a stranger was told the time zone")
	}
	if status, body := c.do("POST", "/api/v1/session", map[string]string{"password": "wrong"}); status != 401 || errorCode(body) != "wrong_password" {
		t.Fatalf("wrong password: %d %s", status, body)
	}
	if status, _ := c.do("POST", "/api/v1/session", map[string]string{"password": ""}); status != 401 {
		t.Fatalf("empty password: %d", status)
	}
	if status, body := c.do("POST", "/api/v1/session", map[string]string{"password": password}); status != 200 {
		t.Fatalf("sign in: %d %s", status, body)
	}
	c.get("/api/v1/session", &info)
	if info["signed_in"] != true {
		t.Fatalf("after sign-in: %v", info)
	}
	// Signed in, the session also says what time it is and in which zone:
	// the dashboard shows every time by the server's clock.
	if _, err := time.Parse(time.RFC3339, fmt.Sprint(info["now"])); err != nil || info["timezone"] != "UTC" {
		t.Errorf("clock = %v in %v", info["now"], info["timezone"])
	}
	var overview map[string]any
	c.get("/api/v1/overview", &overview)
	if overview["status"] != "ok" {
		t.Errorf("overview = %v", overview)
	}

	if status, _ := c.do("DELETE", "/api/v1/session", nil); status != 200 {
		t.Fatal("sign out failed")
	}
	if status, _ := c.do("GET", "/api/v1/overview", nil); status != 401 {
		t.Fatalf("still signed in after signing out: %d", status)
	}
}

// Notifications link to the dashboard. Nobody should have to tell it where
// it lives: it learns that from the first browser that signs in.
func TestTheDashboardLearnsItsAddress(t *testing.T) {
	s := newSite(t)
	publicURL := func() string {
		settings, err := s.engine.Store().Settings(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return settings.PublicURL
	}
	signIn := func(host, origin string) {
		t.Helper()
		req, _ := http.NewRequest("POST", s.server.URL+"/api/v1/session", strings.NewReader(`{"password":"`+password+`"}`))
		if host != "" {
			req.Host = host
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("sign in at %q from %q: %d", host, origin, resp.StatusCode)
		}
	}

	// A script has no address to learn from, and one only this machine can
	// reach is of no use in a message read on a phone.
	signIn("", "")
	signIn("localhost:8080", "http://localhost:8080")
	signIn("127.0.0.1:8080", "http://127.0.0.1:8080")
	if got := publicURL(); got != "" {
		t.Fatalf("learned %q from a script or a loopback address", got)
	}
	signIn("pulse.example", "https://pulse.example")
	if got := publicURL(); got != "https://pulse.example" {
		t.Fatalf("public URL = %q", got)
	}
	// Learned once. Signing in somewhere else later does not move it.
	signIn("10.0.0.5:8080", "http://10.0.0.5:8080")
	if got := publicURL(); got != "https://pulse.example" {
		t.Fatalf("public URL moved to %q", got)
	}
}

func TestSessionCookieIsLockedDown(t *testing.T) {
	s := newSite(t)
	resp, err := http.Post(s.server.URL+"/api/v1/session", "application/json", strings.NewReader(`{"password":"`+password+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	cookie := resp.Cookies()[0]
	if cookie.Name != sessionCookie || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure {
		t.Fatalf("cookie = %+v", cookie)
	}
	if strings.Contains(cookie.Value, password) {
		t.Fatal("the cookie carries the password")
	}

	// Behind a TLS-terminating proxy the cookie must be Secure.
	req, _ := http.NewRequest("POST", s.server.URL+"/api/v1/session", strings.NewReader(`{"password":"`+password+`"}`))
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !resp.Cookies()[0].Secure {
		t.Fatal("the cookie is not Secure behind HTTPS")
	}
}

func TestACookieFromAnotherSiteCannotChangeAnything(t *testing.T) {
	s := newSite(t)
	c := s.signedIn()

	// The same browser, but the request was made by a page on another site.
	c.headers = map[string]string{"Sec-Fetch-Site": "cross-site"}
	if status, body := c.do("POST", "/api/v1/sample", nil); status != 403 || errorCode(body) != "cross_site" {
		t.Fatalf("cross-site POST: %d %s", status, body)
	}
	if status, _ := c.do("GET", "/api/v1/overview", nil); status != 200 {
		t.Fatal("reads do not need the check")
	}
	// An older browser that only sends Origin.
	c.headers = map[string]string{"Origin": "https://evil.example"}
	if status, _ := c.do("DELETE", "/api/v1/tokens/tok_x", nil); status != 403 {
		t.Fatalf("foreign Origin: %d", status)
	}
	// The dashboard itself.
	c.headers = map[string]string{"Sec-Fetch-Site": "same-origin"}
	if status, _ := c.do("POST", "/api/v1/sample", nil); status != 202 {
		t.Fatalf("same-origin POST: %d", status)
	}
	// A cross-site page cannot sign in on the user's behalf either.
	fresh := s.browser()
	fresh.headers = map[string]string{"Sec-Fetch-Site": "cross-site"}
	if status, _ := fresh.do("POST", "/api/v1/session", map[string]string{"password": password}); status != 403 {
		t.Fatalf("cross-site sign-in: %d", status)
	}
}

func TestPasswordGuessingIsSlowedDown(t *testing.T) {
	s := newSite(t)
	c := s.browser()
	for i := range loginPerClient {
		if status, _ := c.do("POST", "/api/v1/session", map[string]string{"password": "guess"}); status != 401 {
			t.Fatalf("attempt %d: %d", i, status)
		}
	}
	status, body := c.do("POST", "/api/v1/session", map[string]string{"password": password})
	if status != 429 || errorCode(body) != "too_many_attempts" {
		t.Fatalf("after %d failures even the right password waits: %d %s", loginPerClient, status, body)
	}
	// An open session is not affected by someone else's guessing.
	other := newSite(t)
	signed := other.signedIn()
	for range loginPerClient + 1 {
		other.browser().do("POST", "/api/v1/session", map[string]string{"password": "guess"})
	}
	if status, _ := signed.do("GET", "/api/v1/overview", nil); status != 200 {
		t.Fatal("a signed-in session was locked out")
	}
}

func TestNoPasswordLocksTheDashboardButNotTokens(t *testing.T) {
	s := newSite(t, func(api *Server) { api.Password = "" })
	c := s.browser()
	if status, body := c.do("POST", "/api/v1/session", map[string]string{"password": ""}); status != 503 || errorCode(body) != "no_password" {
		t.Fatalf("sign in without a configured password: %d %s", status, body)
	}
	if status, body := c.do("GET", "/api/v1/overview", nil); status != 503 || errorCode(body) != "no_password" {
		t.Fatalf("overview: %d %s", status, body)
	}
	if status, _ := s.withToken(core.ScopeRead).do("GET", "/api/v1/overview", nil); status != 200 {
		t.Fatalf("a token still works: %d", status)
	}
}

func TestTokenScopes(t *testing.T) {
	s := newSite(t)
	reader, actor, reporter, admin := s.withToken(core.ScopeRead), s.withToken(core.ScopeAct), s.withToken(core.ScopeIngest), s.withToken(core.ScopeAdmin)

	type call struct {
		method, path string
		body         any
	}
	read := call{"GET", "/api/v1/overview", nil}
	act := call{"POST", "/api/v1/sample", nil}
	ingest := call{"POST", "/api/v1/ingest", map[string]any{"meter": "m", "kind": "counter", "value": 1}}
	manage := call{"GET", "/api/v1/tokens", nil}

	allowed := map[*client][]call{reader: {read}, actor: {act}, reporter: {ingest}, admin: {read, act, ingest, manage}}
	denied := map[*client][]call{reader: {act, ingest, manage}, actor: {read, ingest, manage}, reporter: {read, act, manage}}
	for c, calls := range allowed {
		for _, k := range calls {
			if status, body := c.do(k.method, k.path, k.body); status >= 300 {
				t.Errorf("%s %s should be allowed: %d %s", k.method, k.path, status, body)
			}
		}
	}
	for c, calls := range denied {
		for _, k := range calls {
			if status, body := c.do(k.method, k.path, k.body); status != 403 || errorCode(body) != "forbidden" {
				t.Errorf("%s %s should be forbidden: %d %s", k.method, k.path, status, body)
			}
		}
	}

	bogus := &client{t: t, site: s, http: &http.Client{}, token: "qp_not-a-real-token"}
	if status, body := bogus.do("GET", "/api/v1/overview", nil); status != 401 || errorCode(body) != "unauthorized" {
		t.Errorf("a made-up token: %d %s", status, body)
	}
	// A token needs no same-origin check: nothing sends it automatically.
	actor.headers = map[string]string{"Sec-Fetch-Site": "cross-site"}
	if status, _ := actor.do("POST", "/api/v1/sample", nil); status != 202 {
		t.Errorf("token with a cross-site header: %d", status)
	}
}

func TestAccountsNeverLeakTheirSecrets(t *testing.T) {
	s := newSite(t)
	c := s.signedIn()
	status, body := c.do("POST", "/api/v1/accounts", map[string]any{
		"name": "DeepSeek", "provider": "deepseek", "group": "AI",
		"credential": map[string]string{"api_key": "sk-very-secret-value-0001"},
	})
	if status != 201 {
		t.Fatalf("create: %d %s", status, body)
	}
	var created map[string]any
	_ = json.Unmarshal(body, &created)
	id := created["id"].(string)

	for _, path := range []string{"/api/v1/accounts", "/api/v1/accounts/" + id, "/api/v1/overview", "/api/v1/export", "/api/v1/export?history=0"} {
		_, raw := c.do("GET", path, nil)
		if bytes.Contains(raw, []byte("sk-very-secret-value-0001")) {
			t.Errorf("%s returned the API key", path)
		}
	}
	// Only an export that was asked for secrets has them.
	if _, raw := c.do("GET", "/api/v1/export?secrets=1", nil); !bytes.Contains(raw, []byte("sk-very-secret-value-0001")) {
		t.Error("an export with secrets is missing the credential")
	}

	if status, body := c.do("POST", "/api/v1/accounts", map[string]any{"name": "DeepSeek", "provider": "deepseek", "credential": map[string]string{"api_key": "k"}}); status != 400 || errorCode(body) != "invalid" {
		t.Errorf("duplicate: %d %s", status, body)
	}
	if status, body := c.do("PATCH", "/api/v1/accounts/"+id, map[string]any{"nmae": "typo"}); status != 400 || !strings.Contains(string(body), "unknown field") {
		t.Errorf("a misspelt field must be an error: %d %s", status, body)
	}
	if status, _ := c.do("PATCH", "/api/v1/accounts/acc_missing", map[string]any{"name": "x"}); status != 404 {
		t.Errorf("missing account: %d", status)
	}
	if status, _ := c.do("DELETE", "/api/v1/accounts/"+id, nil); status != 204 {
		t.Errorf("delete: %d", status)
	}
}

func TestRemindersOverHTTP(t *testing.T) {
	s := newSite(t)
	c := s.withToken(core.ScopeRead, core.ScopeAct)

	status, body := c.do("POST", "/api/v1/reminders/preview", map[string]any{"rule": map[string]any{"freq": "yearly", "calendar": "lunar", "month": 1, "day": 1}})
	if status != 200 || !bytes.Contains(body, []byte(`"label":"正月初一"`)) {
		t.Fatalf("preview: %d %s", status, body)
	}
	status, body = c.do("POST", "/api/v1/reminders", map[string]any{
		"title": "Rent", "kind": "renewal", "rule": map[string]any{"freq": "monthly", "day": 1},
		"lead_days": []int{3, 0}, "amount": 4200, "currency": "cny",
	})
	if status != 201 {
		t.Fatalf("create: %d %s", status, body)
	}
	var reminder struct {
		ID       string
		Currency string
		Next     string
		Handled  *string
	}
	_ = json.Unmarshal(body, &reminder)
	if reminder.Currency != "CNY" || reminder.Next == "" {
		t.Fatalf("reminder = %+v", reminder)
	}

	first := reminder.Next
	status, body = c.do("POST", "/api/v1/reminders/"+reminder.ID+"/done", nil)
	_ = json.Unmarshal(body, &reminder)
	if status != 200 || reminder.Next == first || reminder.Handled == nil || *reminder.Handled != first {
		t.Fatalf("done: %d %s", status, body)
	}
	status, body = c.do("DELETE", "/api/v1/reminders/"+reminder.ID+"/done", nil)
	_ = json.Unmarshal(body, &reminder)
	if status != 200 || reminder.Next != first {
		t.Fatalf("undo: %d %s", status, body)
	}
	if status, _ := c.do("PUT", "/api/v1/reminders/"+reminder.ID+"/snooze", map[string]any{}); status != 400 {
		t.Errorf("snooze without a date: %d", status)
	}
	if status, _ := c.do("PUT", "/api/v1/reminders/"+reminder.ID+"/snooze", map[string]any{"until": "2099-01-01"}); status != 200 {
		t.Errorf("snooze: %d", status)
	}
	if status, _ := c.do("DELETE", "/api/v1/reminders/"+reminder.ID+"/snooze", nil); status != 200 {
		t.Errorf("wake: %d", status)
	}

	var listed struct{ Items []map[string]any }
	c.get("/api/v1/reminders", &listed)
	if len(listed.Items) != 1 {
		t.Fatalf("list = %v", listed)
	}
	if status, _ := c.do("DELETE", "/api/v1/reminders/"+reminder.ID, nil); status != 204 {
		t.Errorf("delete: %d", status)
	}
	// Lists are arrays even when empty.
	if _, raw := c.do("GET", "/api/v1/reminders", nil); strings.TrimSpace(string(raw)) != `{"items":[]}` {
		t.Errorf("empty list = %s", raw)
	}
}

func TestIngestAndReadBack(t *testing.T) {
	s := newSite(t)
	reporter, reader := s.withToken(core.ScopeIngest), s.withToken(core.ScopeRead)

	// One reading as a bare object.
	status, body := reporter.do("POST", "/api/v1/ingest", map[string]any{"meter": "claude-code", "name": "Claude Code", "kind": "counter", "unit": "USD", "value": 12.5})
	if status != 200 || !bytes.Contains(body, []byte(`"accepted":1`)) {
		t.Fatalf("ingest: %d %s", status, body)
	}
	// Several at once.
	status, body = reporter.do("POST", "/api/v1/ingest", map[string]any{"readings": []map[string]any{
		{"meter": "claude-code", "value": 13},
		{"meter": "disk", "kind": "quota", "unit": "%", "value": 41, "limit": 100},
	}})
	if status != 200 || !bytes.Contains(body, []byte(`"accepted":2`)) {
		t.Fatalf("batch: %d %s", status, body)
	}
	if status, body := reporter.do("POST", "/api/v1/ingest", map[string]any{"meter": "new-one", "value": 1}); status != 400 || !strings.Contains(string(body), "kind is required") {
		t.Fatalf("a new meter without a kind: %d %s", status, body)
	}

	var meters struct {
		Items []struct {
			ID, Display, Kind string
			Value             float64
		}
	}
	reader.get("/api/v1/meters", &meters)
	if len(meters.Items) != 2 {
		t.Fatalf("meters = %+v", meters)
	}
	var id string
	for _, m := range meters.Items {
		if m.Display == "Claude Code" && m.Value == 13 {
			id = m.ID
		}
	}
	if id == "" {
		t.Fatalf("meters = %+v", meters)
	}
	var series struct {
		Range  string
		Points []any
	}
	reader.get("/api/v1/meters/"+id+"/series?range=7d", &series)
	if series.Range != "7d" || len(series.Points) == 0 {
		t.Fatalf("series = %+v", series)
	}
	if status, _ := reader.do("GET", "/api/v1/meters/"+id+"/series?range=forever", nil); status != 400 {
		t.Errorf("a bad range: %d", status)
	}
	if status, _ := reader.do("GET", "/api/v1/meters/mtr_missing", nil); status != 404 {
		t.Errorf("missing meter: %d", status)
	}
}

func TestSettingsPatchIsPartial(t *testing.T) {
	s := newSite(t)
	c := s.signedIn()
	status, body := c.do("PATCH", "/api/v1/settings", map[string]any{
		"timezone": "Asia/Shanghai", "base_currency": "CNY",
		"fx":    map[string]float64{"USD": 7.16, "EUR": 7.8, "CNY": 1},
		"quiet": map[string]any{"start": "22:30"},
	})
	if status != 200 {
		t.Fatalf("patch: %d %s", status, body)
	}
	var settings core.Settings
	c.get("/api/v1/settings", &settings)
	// What was sent changed; what was not kept its value — also inside a nested object.
	if settings.Timezone != "Asia/Shanghai" || settings.Quiet.Start != "22:30" || settings.Quiet.End != "08:00" || !settings.Quiet.Enabled || settings.SampleSeconds != 1800 {
		t.Fatalf("settings = %+v", settings)
	}
	// The base currency needs no rate, so the one that was sent is dropped.
	if len(settings.FX) != 2 || settings.FX["USD"] != 7.16 {
		t.Fatalf("fx = %v", settings.FX)
	}
	// Sending the rates replaces them: that is the only way to remove one.
	status, _ = c.do("PATCH", "/api/v1/settings", map[string]any{"fx": map[string]float64{"USD": 7.2}})
	var replaced core.Settings
	c.get("/api/v1/settings", &replaced)
	if status != 200 || len(replaced.FX) != 1 || replaced.FX["USD"] != 7.2 {
		t.Fatalf("fx = %v", replaced.FX)
	}
	// A patch that says nothing about rates leaves them alone.
	c.do("PATCH", "/api/v1/settings", map[string]any{"runway_days": 10})
	var kept core.Settings
	c.get("/api/v1/settings", &kept)
	if kept.FX["USD"] != 7.2 || kept.RunwayDays != 10 {
		t.Fatalf("after an unrelated patch = %+v", kept)
	}
	if status, body := c.do("PATCH", "/api/v1/settings", map[string]any{"timezone": "Nowhere"}); status != 400 || !strings.Contains(string(body), "timezone") {
		t.Fatalf("an invalid zone: %d %s", status, body)
	}
	// A token that can only read may read settings, not change them.
	reader := s.withToken(core.ScopeRead)
	if status, _ := reader.do("PATCH", "/api/v1/settings", map[string]any{"language": "zh"}); status != 403 {
		t.Errorf("a read token changed settings: %d", status)
	}
}

func TestChannelsTokensAndProviders(t *testing.T) {
	s := newSite(t)
	c := s.signedIn()
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer hook.Close()

	var types struct{ Items []struct{ ID string } }
	c.get("/api/v1/channel-types", &types)
	if len(types.Items) < 8 {
		t.Fatalf("channel types = %+v", types)
	}
	status, body := c.do("POST", "/api/v1/channels", map[string]any{"name": "ops", "type": "webhook", "config": map[string]string{"url": hook.URL + "/secret-path-0001"}})
	if status != 201 || bytes.Contains(body, []byte("secret-path-0001")) {
		t.Fatalf("create channel: %d %s", status, body)
	}
	var channel struct{ ID string }
	_ = json.Unmarshal(body, &channel)
	if status, body := c.do("POST", "/api/v1/channels/"+channel.ID+"/test", nil); status != 200 || !bytes.Contains(body, []byte(`"ok":true`)) {
		t.Fatalf("test channel: %d %s", status, body)
	}
	hook.Close()
	// A channel that refuses is a result, not a server error.
	if status, body := c.do("POST", "/api/v1/channels/"+channel.ID+"/test", nil); status != 200 || !bytes.Contains(body, []byte(`"ok":false`)) {
		t.Fatalf("test a dead channel: %d %s", status, body)
	}

	status, body = c.do("POST", "/api/v1/tokens", map[string]any{"name": "agent", "scopes": []string{"read"}})
	var token struct{ ID, Token string }
	_ = json.Unmarshal(body, &token)
	if status != 201 || !strings.HasPrefix(token.Token, "qp_") {
		t.Fatalf("create token: %d %s", status, body)
	}
	// The plaintext is shown once and never again.
	if _, raw := c.do("GET", "/api/v1/tokens", nil); bytes.Contains(raw, []byte(token.Token)) {
		t.Fatal("the token list contains a token")
	}
	agent := &client{t: t, site: s, http: &http.Client{}, token: token.Token}
	if status, _ := agent.do("GET", "/api/v1/overview", nil); status != 200 {
		t.Fatal("the new token does not work")
	}
	c.do("DELETE", "/api/v1/tokens/"+token.ID, nil)
	if status, _ := agent.do("GET", "/api/v1/overview", nil); status != 401 {
		t.Fatal("a revoked token still works")
	}

	var providers struct {
		Items []struct {
			ID      string
			Builtin bool
		}
	}
	c.get("/api/v1/providers", &providers)
	if len(providers.Items) < 10 {
		t.Fatalf("providers = %d", len(providers.Items))
	}
	definition := map[string]any{
		"name": "Relay", "fields": []map[string]any{{"key": "api_key", "label": "API Key", "secret": true}},
		"request": map[string]any{"url": "https://relay.example/balance", "auth": map[string]any{"type": "bearer"}},
		"extract": "{value: .balance}",
	}
	if status, body := c.do("PUT", "/api/v1/providers/relay", definition); status != 200 {
		t.Fatalf("save provider: %d %s", status, body)
	}
	if status, _ := c.do("PUT", "/api/v1/providers/deepseek", definition); status != 400 {
		t.Errorf("overwriting a built-in: %d", status)
	}
	if status, _ := c.do("DELETE", "/api/v1/providers/relay", nil); status != 204 {
		t.Errorf("delete provider: %d", status)
	}
}

func TestBrowserPushOverHTTP(t *testing.T) {
	s := newSite(t)
	c := s.signedIn()
	var key struct {
		PublicKey string `json:"public_key"`
	}
	c.get("/api/v1/push", &key)
	if len(key.PublicKey) < 80 {
		t.Fatalf("public key = %q", key.PublicKey)
	}
	subscription := map[string]any{"endpoint": "https://push.example/send/device-secret-0001", "keys": map[string]string{"p256dh": "BKey", "auth": "auth"}}
	status, body := c.do("POST", "/api/v1/push/subscriptions", map[string]any{"name": "Safari · iPhone", "subscription": subscription})
	if status != 200 || !bytes.Contains(body, []byte(`"type":"webpush"`)) || bytes.Contains(body, []byte("device-secret-0001")) {
		t.Fatalf("subscribe: %d %s", status, body)
	}
	// What a browser really sends is JSON.stringify(subscription), and that
	// always carries expirationTime — null, or a time. 1.0.0 refused it as an
	// unknown field, so nobody could turn notifications on.
	for _, sent := range []string{
		`{"name":"Chrome · macOS","subscription":{"endpoint":"https://push.example/send/device-0002","expirationTime":null,"keys":{"p256dh":"BKey","auth":"auth"}}}`,
		`{"name":"Firefox · Linux","subscription":{"endpoint":"https://push.example/send/device-0003","expirationTime":1794900000000,"keys":{"p256dh":"BKey","auth":"auth"}}}`,
	} {
		if status, body := c.do("POST", "/api/v1/push/subscriptions", sent); status != 200 || !bytes.Contains(body, []byte(`"type":"webpush"`)) {
			t.Errorf("a browser's own subscription was refused: %d %s", status, body)
		}
	}
	subscription["endpoint"] = "http://push.example/x"
	if status, _ := c.do("POST", "/api/v1/push/subscriptions", map[string]any{"subscription": subscription}); status != 400 {
		t.Errorf("a plain-http endpoint: %d", status)
	}
	// A browser channel cannot be made by posting a form.
	if status, _ := c.do("POST", "/api/v1/channels", map[string]any{"name": "x", "type": "webpush", "config": map[string]string{"endpoint": "https://x.example"}}); status != 400 {
		t.Errorf("a browser channel from a form: %d", status)
	}
	// Only someone who can manage channels may add a device.
	if status, _ := s.withToken(core.ScopeRead, core.ScopeAct).do("GET", "/api/v1/push", nil); status != 403 {
		t.Errorf("a non-admin token read the push key: %d", status)
	}
}

func TestExportImportOverHTTP(t *testing.T) {
	source := newSite(t)
	c := source.signedIn()
	c.do("POST", "/api/v1/reminders", map[string]any{"title": "Rent", "rule": map[string]any{"freq": "monthly", "day": 1}})
	c.do("POST", "/api/v1/accounts", map[string]any{"name": "DeepSeek", "provider": "deepseek", "credential": map[string]string{"api_key": "sk-secret-0001"}})

	status, exported := c.do("GET", "/api/v1/export?secrets=1", nil)
	if status != 200 || !bytes.Contains(exported, []byte(`"format":"quotapulse-bundle"`)) {
		t.Fatalf("export: %d %s", status, exported)
	}

	target := newSite(t)
	d := target.signedIn()
	status, body := d.do("POST", "/api/v1/import?settings=1", string(exported))
	if status != 200 || !bytes.Contains(body, []byte(`"accounts":1`)) || !bytes.Contains(body, []byte(`"reminders":1`)) {
		t.Fatalf("import: %d %s", status, body)
	}
	if status, body := d.do("POST", "/api/v1/import", `{"format":"something else"}`); status != 400 {
		t.Fatalf("a foreign file: %d %s", status, body)
	}
}

func TestStream(t *testing.T) {
	s := newSite(t)
	token := s.withToken(core.ScopeRead, core.ScopeIngest)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", s.server.URL+"/api/v1/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	events := make(chan string, 4)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if data, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
				events <- data
			}
		}
	}()
	// Give the subscription a moment, then change something.
	time.Sleep(100 * time.Millisecond)
	token.do("POST", "/api/v1/ingest", map[string]any{"meter": "m", "kind": "balance", "value": 0})
	select {
	case data := <-events:
		// A reading that opens an incident touches several topics; they
		// arrive together, once each.
		if !strings.Contains(data, "meters") || !strings.Contains(data, "incidents") || strings.Count(data, "meters") != 1 {
			t.Fatalf("event = %q", data)
		}
	case <-ctx.Done():
		t.Fatal("no event arrived")
	}
}

func TestMetrics(t *testing.T) {
	s := newSite(t)
	c := s.withToken(core.ScopeRead, core.ScopeIngest)
	c.do("POST", "/api/v1/ingest", map[string]any{"meter": "wallet", "name": `My "wallet"`, "kind": "balance", "unit": "CNY", "value": 42.5})
	c.do("POST", "/api/v1/ingest", map[string]any{"meter": "plan", "kind": "quota", "unit": "%", "value": 25, "limit": 100})

	status, body := c.do("GET", "/metrics", nil)
	text := string(body)
	if status != 200 {
		t.Fatalf("metrics: %d %s", status, text)
	}
	for _, want := range []string{
		`quotapulse_build_info{version="test"} 1`,
		`quotapulse_meter_value{kind="balance",meter="My \"wallet\"",unit="CNY"} 42.5`,
		`quotapulse_meter_remaining_ratio{kind="quota",meter="plan",unit="%"} 0.25`,
		`quotapulse_incidents_open{severity="critical"} 0`,
		"# TYPE quotapulse_meter_value gauge",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("metrics lack %q in:\n%s", want, text)
		}
	}
}

func TestDashboardFiles(t *testing.T) {
	s := newSite(t)
	fetch := func(path string) *http.Response {
		t.Helper()
		resp, err := http.Get(s.server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	// Any page of the app is index.html, never cached, so a deploy shows up.
	for _, path := range []string{"/", "/meters", "/meters/mtr_123", "/settings/channels", "/index.html"} {
		resp := fetch(path)
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 || !bytes.Contains(body, []byte("<title>QuotaPulse</title>")) || resp.Header.Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: %d %q cache=%q", path, resp.StatusCode, body, resp.Header.Get("Cache-Control"))
		}
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
			t.Errorf("%s: content type %q", path, resp.Header.Get("Content-Type"))
		}
	}
	// Hashed files are cached forever.
	asset := fetch("/assets/app-abc123.js")
	if asset.StatusCode != 200 || !strings.Contains(asset.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %d cache=%q", asset.StatusCode, asset.Header.Get("Cache-Control"))
	}
	if got := fetch("/manifest.webmanifest").Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("manifest cache = %q", got)
	}
	// Every response carries the hardening headers.
	headers := fetch("/").Header
	if !strings.Contains(headers.Get("Content-Security-Policy"), "script-src 'self'") || headers.Get("X-Content-Type-Options") != "nosniff" || headers.Get("X-Frame-Options") != "DENY" {
		t.Errorf("headers = %v", headers)
	}
	// HSTS only when the browser actually came over TLS.
	if headers.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS was sent over plain HTTP")
	}
	secure, _ := http.NewRequest("GET", s.server.URL+"/", nil)
	secure.Header.Set("X-Forwarded-Proto", "https")
	viaProxy, err := http.DefaultClient.Do(secure)
	if err != nil {
		t.Fatal(err)
	}
	viaProxy.Body.Close()
	if viaProxy.Header.Get("Strict-Transport-Security") == "" {
		t.Error("no HSTS behind a TLS proxy")
	}
	// The dashboard is read-only as far as HTTP goes.
	resp, err := http.Post(s.server.URL+"/meters", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Errorf("POST to a page: %d", resp.StatusCode)
	}
}

func TestMissingDashboardSaysSo(t *testing.T) {
	s := newSite(t, func(api *Server) { api.Assets = nil })
	resp, err := http.Get(s.server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 404 || !bytes.Contains(body, []byte("npm --prefix web run build")) {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}

func TestChangesAreLoggedWithoutTheirBodies(t *testing.T) {
	var logged bytes.Buffer
	s := newSite(t, func(api *Server) { api.Log = slog.New(slog.NewTextHandler(&logged, nil)) })
	token := s.withToken(core.ScopeAdmin)
	token.do("GET", "/api/v1/overview", nil)
	token.do("POST", "/api/v1/accounts", map[string]any{"name": "DeepSeek", "provider": "deepseek", "credential": map[string]string{"api_key": "sk-secret-value-0001"}})
	s.signedIn().do("POST", "/api/v1/sample", nil)
	s.browser().do("POST", "/api/v1/sample", nil)

	text := logged.String()
	for _, want := range []string{
		`msg=change method=POST path=/api/v1/accounts by=token:t-admin status=201`,
		`path=/api/v1/sample by=session status=202`,
		`path=/api/v1/sample by=anonymous status=401`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the log lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "sk-secret-value-0001") || strings.Contains(text, "/api/v1/overview") {
		t.Errorf("the log holds a credential or a read:\n%s", text)
	}
}

func TestOversizedBodyIsRefused(t *testing.T) {
	s := newSite(t)
	c := s.signedIn()
	huge := `{"name":"` + strings.Repeat("x", maxBody) + `"}`
	if status, body := c.do("POST", "/api/v1/accounts", huge); status != 413 || errorCode(body) != "too_large" {
		t.Fatalf("%d %s", status, body[:min(len(body), 200)])
	}
}

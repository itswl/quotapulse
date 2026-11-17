package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quotapulse/internal/core"
)

var fixed = time.Date(2026, 11, 16, 2, 0, 0, 0, time.UTC)

type seen struct {
	path   string
	query  string
	header http.Header
	body   []byte
}

func (s *seen) json(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(s.body, &out); err != nil {
		t.Fatalf("the request body is not JSON: %s", s.body)
	}
	return out
}

func serve(t *testing.T, status int, reply string) (*httptest.Server, *seen) {
	t.Helper()
	got := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*got = seen{path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone(), body: body}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func sender() *Sender {
	s := NewSender()
	s.Now = func() time.Time { return fixed }
	return s
}

func message() Message {
	return Message{
		Event: core.EventOpened, Severity: core.Warning, Topic: core.TopicMeters,
		Title: "余额将在 5 天后用尽 · DeepSeek",
		Body:  "按近期日均 8.20 CNY 的消耗，余额预计在 11月21日 用尽。",
		Facts: []Fact{{"当前余额", "41.00 CNY"}, {"日均消耗", "8.20 CNY"}},
		Link:  "https://pulse.example/meters/mtr_1", LinkLabel: "打开看板",
		Footer: "QuotaPulse · 11-16 10:00", At: fixed,
		Subject: &Subject{IncidentID: "inc_1", Kind: core.IncidentLevel, Reason: core.ReasonRunway, Type: core.SubjectMeter, ID: "mtr_1", Name: "DeepSeek"},
	}
}

func TestRendering(t *testing.T) {
	m := message()
	if got := m.Markdown(); got != "按近期日均 8.20 CNY 的消耗，余额预计在 11月21日 用尽。\n\n**当前余额** 41.00 CNY\n**日均消耗** 8.20 CNY" {
		t.Errorf("markdown = %q", got)
	}
	if got := m.Plain(); got != "按近期日均 8.20 CNY 的消耗，余额预计在 11月21日 用尽。\n\n当前余额: 41.00 CNY\n日均消耗: 8.20 CNY" {
		t.Errorf("plain = %q", got)
	}
	digest := Message{Title: "Weekly", Sections: []Section{{Title: "Meters", Lines: []string{"**DeepSeek** 547.67 CNY", "GLM 99%"}}}}
	if got := digest.Markdown(); got != "**Meters**\n**DeepSeek** 547.67 CNY\nGLM 99%" {
		t.Errorf("digest markdown = %q", got)
	}
	if got := digest.Plain(); got != "Meters\nDeepSeek 547.67 CNY\nGLM 99%" {
		t.Errorf("digest plain = %q", got)
	}
	// Telegram parses HTML: anything from outside must be escaped.
	hostile := Message{Title: "<b>x</b> & y", Body: "a < b", Facts: []Fact{{"k<", "v>"}}, Link: "https://x.test/?a=1&b=2", LinkLabel: "Open"}
	html := hostile.HTML()
	if strings.Contains(html, "<b>x</b> &") || !strings.Contains(html, "&lt;b&gt;x&lt;/b&gt; &amp; y") || !strings.Contains(html, `href="https://x.test/?a=1&amp;b=2"`) {
		t.Errorf("html = %q", html)
	}
}

func TestFeishu(t *testing.T) {
	srv, got := serve(t, 200, `{"code":0,"msg":"success","data":{}}`)
	err := sender().Send(context.Background(), "feishu", map[string]string{"url": srv.URL + "/open-apis/bot/v2/hook/abc"}, message())
	if err != nil {
		t.Fatal(err)
	}
	payload := got.json(t)
	card := payload["card"].(map[string]any)
	header := card["header"].(map[string]any)
	if payload["msg_type"] != "interactive" || header["template"] != "orange" {
		t.Fatalf("payload = %s", got.body)
	}
	if header["title"].(map[string]any)["content"] != message().Title {
		t.Errorf("title = %v", header["title"])
	}
	elements := card["elements"].([]any)
	if len(elements) != 3 || elements[0].(map[string]any)["tag"] != "markdown" || elements[1].(map[string]any)["tag"] != "action" || elements[2].(map[string]any)["tag"] != "note" {
		t.Fatalf("elements = %v", elements)
	}
	if _, signed := payload["sign"]; signed {
		t.Error("no secret, no signature")
	}

	// colour follows what happened
	for want, m := range map[string]Message{
		"red":   {Severity: core.Critical, Event: core.EventOpened, Title: "x"},
		"blue":  {Severity: core.Info, Event: core.EventReminder, Title: "x"},
		"green": {Severity: core.Critical, Event: core.EventResolved, Title: "x"},
	} {
		if err := sender().Send(context.Background(), "feishu", map[string]string{"url": srv.URL}, m); err != nil {
			t.Fatal(err)
		}
		if got.json(t)["card"].(map[string]any)["header"].(map[string]any)["template"] != want {
			t.Errorf("%+v: want %s card", m, want)
		}
	}
}

func TestFeishuSignature(t *testing.T) {
	srv, got := serve(t, 200, `{"code":0}`)
	err := sender().Send(context.Background(), "feishu", map[string]string{"url": srv.URL, "secret": "sign-secret-0001"}, message())
	if err != nil {
		t.Fatal(err)
	}
	payload := got.json(t)
	if payload["timestamp"] != "1794794400" {
		t.Fatalf("timestamp = %v", payload["timestamp"])
	}
	mac := hmac.New(sha256.New, []byte("1794794400\nsign-secret-0001"))
	if want := base64.StdEncoding.EncodeToString(mac.Sum(nil)); payload["sign"] != want {
		t.Fatalf("sign = %v, want %s", payload["sign"], want)
	}
}

// Feishu, DingTalk and WeCom all answer 200 to a message they threw away.
func TestRefusalsHiddenBehindHTTP200(t *testing.T) {
	cases := []struct {
		channel, reply, want string
	}{
		{"feishu", `{"code":19024,"msg":"Key Words Not Found"}`, "Feishu refused the message: Key Words Not Found (code 19024)"},
		{"feishu", `{"StatusCode":19021,"StatusMessage":"sign match fail"}`, "Feishu refused the message: sign match fail (code 19021)"},
		{"dingtalk", `{"errcode":310000,"errmsg":"keywords not in content"}`, "DingTalk refused the message: keywords not in content (code 310000)"},
		{"wecom", `{"errcode":93000,"errmsg":"invalid webhook url"}`, "WeCom refused the message: invalid webhook url (code 93000)"},
	}
	for _, c := range cases {
		srv, _ := serve(t, 200, c.reply)
		err := sender().Send(context.Background(), c.channel, map[string]string{"url": srv.URL}, message())
		if err == nil || err.Error() != c.want || !IsPermanent(err) {
			t.Errorf("%s: err = %v (permanent %v)", c.channel, err, IsPermanent(err))
		}
	}
	for _, channel := range []string{"feishu", "dingtalk", "wecom"} {
		for _, ok := range []string{`{"code":0}`, `{"errcode":0,"errmsg":"ok"}`, `{"StatusCode":0,"StatusMessage":"success"}`, ``, `ok`} {
			srv, _ := serve(t, 200, ok)
			if err := sender().Send(context.Background(), channel, map[string]string{"url": srv.URL}, message()); err != nil {
				t.Errorf("%s with reply %q: %v", channel, ok, err)
			}
		}
	}
}

func TestDingTalk(t *testing.T) {
	srv, got := serve(t, 200, `{"errcode":0,"errmsg":"ok"}`)
	err := sender().Send(context.Background(), "dingtalk", map[string]string{"url": srv.URL + "/robot/send?access_token=tok-0001", "secret": "SEC-secret-0001"}, message())
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte("SEC-secret-0001"))
	mac.Write([]byte("1794794400000\nSEC-secret-0001"))
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	query := srvQuery(t, got.query)
	if query["access_token"] != "tok-0001" || query["timestamp"] != "1794794400000" || query["sign"] != want {
		t.Fatalf("query = %v, want sign %s", query, want)
	}
	payload := got.json(t)
	text := payload["markdown"].(map[string]any)["text"].(string)
	if payload["msgtype"] != "markdown" || !strings.HasPrefix(text, "### "+message().Title+"\n\n") {
		t.Fatalf("payload = %s", got.body)
	}
	// each fact on its own line needs the two-space break
	if !strings.Contains(text, "**当前余额** 41.00 CNY  \n**日均消耗** 8.20 CNY") || !strings.Contains(text, "[打开看板](https://pulse.example/meters/mtr_1)") {
		t.Errorf("text = %q", text)
	}
}

func srvQuery(t *testing.T, raw string) map[string]string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://x/?"+raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for key, values := range req.URL.Query() {
		out[key] = values[0]
	}
	return out
}

func TestWeComAndSlack(t *testing.T) {
	srv, got := serve(t, 200, `{"errcode":0,"errmsg":"ok"}`)
	if err := sender().Send(context.Background(), "wecom", map[string]string{"url": srv.URL}, message()); err != nil {
		t.Fatal(err)
	}
	payload := got.json(t)
	if payload["msgtype"] != "markdown" || !strings.HasPrefix(payload["markdown"].(map[string]any)["content"].(string), "### "+message().Title) {
		t.Fatalf("wecom payload = %s", got.body)
	}

	srv, got = serve(t, 200, `ok`)
	if err := sender().Send(context.Background(), "slack", map[string]string{"url": srv.URL}, message()); err != nil {
		t.Fatal(err)
	}
	text := got.json(t)["text"].(string)
	if !strings.HasPrefix(text, "*"+message().Title+"*\n") || strings.Contains(text, "**") || !strings.Contains(text, "<https://pulse.example/meters/mtr_1|打开看板>") {
		t.Fatalf("slack text = %q", text)
	}
}

func TestTelegram(t *testing.T) {
	srv, got := serve(t, 200, `{"ok":true,"result":{}}`)
	s := sender()
	s.TelegramAPI = srv.URL
	config := map[string]string{"bot_token": "123456:bot-secret-0001", "chat_id": "-100200300"}
	if err := s.Send(context.Background(), "telegram", config, message()); err != nil {
		t.Fatal(err)
	}
	if got.path != "/bot123456:bot-secret-0001/sendMessage" {
		t.Fatalf("path = %s", got.path)
	}
	payload := got.json(t)
	if payload["chat_id"] != "-100200300" || payload["parse_mode"] != "HTML" || !strings.HasPrefix(payload["text"].(string), "<b>") {
		t.Fatalf("payload = %s", got.body)
	}

	srv, _ = serve(t, 400, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`)
	s.TelegramAPI = srv.URL
	err := s.Send(context.Background(), "telegram", config, message())
	if err == nil || err.Error() != "Telegram refused the message: Bad Request: chat not found" || !IsPermanent(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestNtfy(t *testing.T) {
	srv, got := serve(t, 200, `{"id":"x"}`)
	critical := message()
	critical.Severity = core.Critical
	err := sender().Send(context.Background(), "ntfy", map[string]string{"server": srv.URL + "/", "topic": "my-topic-0001", "token": "tk_secret_0001"}, critical)
	if err != nil {
		t.Fatal(err)
	}
	payload := got.json(t)
	if got.path != "/" || payload["topic"] != "my-topic-0001" || payload["title"] != critical.Title || payload["priority"] != float64(5) {
		t.Fatalf("payload = %s", got.body)
	}
	if payload["tags"].([]any)[0] != "rotating_light" || payload["click"] != critical.Link {
		t.Errorf("payload = %s", got.body)
	}
	if got.header.Get("Authorization") != "Bearer tk_secret_0001" {
		t.Errorf("Authorization = %q", got.header.Get("Authorization"))
	}

	// the default server is filled in
	checked, err := mustType(t, "ntfy").CheckConfig(map[string]string{"topic": "t"})
	if err != nil || checked["server"] != "https://ntfy.sh" {
		t.Fatalf("checked = %v, %v", checked, err)
	}
}

func TestBark(t *testing.T) {
	srv, got := serve(t, 200, `{"code":200,"message":"success"}`)
	if err := sender().Send(context.Background(), "bark", map[string]string{"server": srv.URL, "device_key": "device-key-0001"}, message()); err != nil {
		t.Fatal(err)
	}
	payload := got.json(t)
	if got.path != "/push" || payload["device_key"] != "device-key-0001" || payload["level"] != "active" || payload["url"] != message().Link {
		t.Fatalf("payload = %s", got.body)
	}
	srv, _ = serve(t, 200, `{"code":400,"message":"failed to get device token"}`)
	if err := sender().Send(context.Background(), "bark", map[string]string{"server": srv.URL, "device_key": "device-key-0001"}, message()); err == nil || !IsPermanent(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestWebhook(t *testing.T) {
	srv, got := serve(t, 204, ``)
	if err := sender().Send(context.Background(), "webhook", map[string]string{"url": srv.URL, "secret": "hook-secret-0001"}, message()); err != nil {
		t.Fatal(err)
	}
	payload := got.json(t)
	if payload["version"] != float64(1) || payload["event"] != "opened" || payload["severity"] != "warning" || payload["topic"] != "meters" {
		t.Fatalf("payload = %s", got.body)
	}
	subject := payload["subject"].(map[string]any)
	if subject["incident_id"] != "inc_1" || subject["reason"] != "runway" || subject["name"] != "DeepSeek" {
		t.Errorf("subject = %v", subject)
	}
	if payload["at"] != "2026-11-16T02:00:00Z" || len(payload["facts"].([]any)) != 2 {
		t.Errorf("payload = %s", got.body)
	}
	mac := hmac.New(sha256.New, []byte("hook-secret-0001"))
	mac.Write(got.body)
	if want := "sha256=" + hex.EncodeToString(mac.Sum(nil)); got.header.Get("X-QuotaPulse-Signature") != want {
		t.Fatalf("signature = %q, want %q", got.header.Get("X-QuotaPulse-Signature"), want)
	}

	// facts is always an array, so a consumer can iterate without a nil check
	bare := Message{Event: core.EventTest, Severity: core.Info, Title: "Test", At: fixed}
	if err := sender().Send(context.Background(), "webhook", map[string]string{"url": srv.URL}, bare); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got.body), `"facts":[]`) || got.header.Get("X-QuotaPulse-Signature") != "" {
		t.Fatalf("bare payload = %s", got.body)
	}
}

func TestWhichFailuresAreWorthRetrying(t *testing.T) {
	cases := map[int]bool{400: true, 401: true, 404: true, 429: false, 500: false, 503: false}
	for status, permanent := range cases {
		srv, _ := serve(t, status, `nope`)
		err := sender().Send(context.Background(), "webhook", map[string]string{"url": srv.URL}, message())
		if err == nil || IsPermanent(err) != permanent {
			t.Errorf("HTTP %d: err = %v, permanent = %v, want %v", status, err, IsPermanent(err), permanent)
		}
	}
	// nothing listening: transient
	err := sender().Send(context.Background(), "webhook", map[string]string{"url": "http://127.0.0.1:1/hook/secret-path-0001"}, message())
	if err == nil || IsPermanent(err) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "secret-path-0001") {
		t.Fatalf("the webhook URL leaked into the error: %v", err)
	}
	// misconfiguration: permanent, and caught before any request
	for _, config := range []map[string]string{{}, {"url": "not a url"}, {"url": "ftp://x.test"}} {
		if err := sender().Send(context.Background(), "webhook", config, message()); err == nil || !IsPermanent(err) {
			t.Errorf("config %v: err = %v", config, err)
		}
	}
	if err := sender().Send(context.Background(), "pigeon", nil, message()); err == nil || !IsPermanent(err) {
		t.Errorf("unknown type: %v", err)
	}
}

func TestSecretsStayOutOfErrors(t *testing.T) {
	// a platform that echoes the URL it was called with
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden: " + r.URL.Path))
	}))
	defer srv.Close()
	err := sender().Send(context.Background(), "wecom", map[string]string{"url": srv.URL + "/cgi-bin/webhook/send-key-secret-0001"}, message())
	if err == nil || strings.Contains(err.Error(), "send-key-secret-0001") {
		t.Fatalf("err = %v", err)
	}
}

func mustType(t *testing.T, id string) Type {
	t.Helper()
	typ, ok := TypeOf(id)
	if !ok {
		t.Fatalf("no channel type %q", id)
	}
	return typ
}

func TestConfig(t *testing.T) {
	feishu := mustType(t, "feishu")
	if _, err := feishu.CheckConfig(map[string]string{"secret": "x"}); err == nil || err.Error() != "missing Webhook URL" {
		t.Fatalf("err = %v", err)
	}
	checked, err := feishu.CheckConfig(map[string]string{"url": "  https://open.feishu.cn/open-apis/bot/v2/hook/abc  ", "junk": "dropped"})
	if err != nil || checked["url"] != "https://open.feishu.cn/open-apis/bot/v2/hook/abc" || len(checked) != 1 {
		t.Fatalf("checked = %v, %v", checked, err)
	}
	masked := feishu.MaskConfig(map[string]string{"url": "https://open.feishu.cn/open-apis/bot/v2/hook/abc-def-0001", "secret": "0123456789abcdef"})
	if masked["url"] != "https://open.feishu.cn/••••" || masked["secret"] != "012••••def" {
		t.Fatalf("masked = %v", masked)
	}
	telegram := mustType(t, "telegram")
	if got := telegram.MaskConfig(map[string]string{"bot_token": "123456:secret", "chat_id": "-100"}); got["chat_id"] != "-100" || strings.Contains(got["bot_token"], "secret") {
		t.Fatalf("masked = %v", got)
	}
	if keys := feishu.SecretKeys(); len(keys) != 2 || keys[0] != "secret" || keys[1] != "url" {
		t.Errorf("secret keys = %v", keys)
	}
	for _, typ := range Types() {
		if typ.send == nil || typ.Name == "" || len(typ.Fields) == 0 {
			t.Errorf("%s is incomplete", typ.ID)
		}
	}
}

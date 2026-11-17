package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/itswl/quotapulse/internal/core"
)

// Field is one setting a channel type needs.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Help        string `json:"help,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	// Secret fields are write-only: the API never returns them.
	Secret   bool   `json:"secret,omitempty"`
	Optional bool   `json:"optional,omitempty"`
	Default  string `json:"default,omitempty"`
}

// Type describes a kind of channel.
type Type struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	NameZH string  `json:"name_zh,omitempty"`
	Fields []Field `json:"fields"`
	// Managed types are not filled in on a form: something else creates
	// their channels. A browser push subscription comes from the browser.
	Managed bool `json:"managed,omitempty"`

	send func(ctx context.Context, s *Sender, config map[string]string, m Message) error
}

var webhookURL = Field{Key: "url", Label: "Webhook URL", Secret: true}

// types is the registry, in the order the dashboard lists them.
var types = []Type{
	{ID: "feishu", Name: "Feishu / Lark", NameZH: "飞书", send: sendFeishu, Fields: []Field{
		webhookURL,
		{Key: "secret", Label: "Signing secret", Secret: true, Optional: true,
			Help: "Only if the bot has signature verification turned on."},
	}},
	{ID: "dingtalk", Name: "DingTalk", NameZH: "钉钉", send: sendDingTalk, Fields: []Field{
		webhookURL,
		{Key: "secret", Label: "Signing secret", Secret: true, Optional: true,
			Help: "Only if the bot uses the signature security setting."},
	}},
	{ID: "wecom", Name: "WeCom", NameZH: "企业微信", send: sendWeCom, Fields: []Field{webhookURL}},
	{ID: "slack", Name: "Slack", send: sendSlack, Fields: []Field{webhookURL}},
	{ID: "telegram", Name: "Telegram", send: sendTelegram, Fields: []Field{
		{Key: "bot_token", Label: "Bot token", Secret: true},
		{Key: "chat_id", Label: "Chat ID"},
	}},
	{ID: "ntfy", Name: "ntfy", send: sendNtfy, Fields: []Field{
		{Key: "server", Label: "Server", Optional: true, Default: "https://ntfy.sh"},
		{Key: "topic", Label: "Topic", Secret: true, Help: "On a public server the topic name is the password."},
		{Key: "token", Label: "Access token", Secret: true, Optional: true},
	}},
	{ID: "bark", Name: "Bark", send: sendBark, Fields: []Field{
		{Key: "server", Label: "Server", Optional: true, Default: "https://api.day.app"},
		{Key: "device_key", Label: "Device key", Secret: true},
	}},
	{ID: TypeWebPush, Name: "This browser", NameZH: "浏览器推送", send: sendWebPush, Managed: true, Fields: []Field{
		{Key: "endpoint", Label: "Endpoint", Secret: true},
		{Key: "p256dh", Label: "Key", Secret: true},
		{Key: "auth", Label: "Auth", Secret: true},
	}},
	{ID: "webhook", Name: "Webhook", send: sendWebhook, Fields: []Field{
		{Key: "url", Label: "URL", Secret: true},
		{Key: "secret", Label: "Signing secret", Secret: true, Optional: true,
			Help: "When set, each request carries X-QuotaPulse-Signature: sha256=<HMAC of the body>."},
	}},
}

// Types lists the channel types.
func Types() []Type { return append([]Type(nil), types...) }

// TypeOf looks up a channel type.
func TypeOf(id string) (Type, bool) {
	for _, t := range types {
		if t.ID == id {
			return t, true
		}
	}
	return Type{}, false
}

// CheckConfig trims a channel's settings, fills defaults, and reports what
// is missing or malformed.
func (t Type) CheckConfig(config map[string]string) (map[string]string, error) {
	out := map[string]string{}
	var missing []string
	for _, f := range t.Fields {
		value := strings.TrimSpace(config[f.Key])
		if value == "" {
			value = f.Default
		}
		if value == "" {
			if !f.Optional {
				missing = append(missing, f.Label)
			}
			continue
		}
		if f.Key == "url" || f.Key == "server" || f.Key == "endpoint" {
			if u, err := url.Parse(value); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return nil, fmt.Errorf("%s must be an http(s) address", f.Label)
			}
		}
		out[f.Key] = value
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// MaskConfig is what the API shows of a channel's settings.
func (t Type) MaskConfig(config map[string]string) map[string]string {
	out := map[string]string{}
	for _, f := range t.Fields {
		value, ok := config[f.Key]
		if !ok || value == "" {
			continue
		}
		if f.Secret {
			out[f.Key] = maskSecret(value)
		} else {
			out[f.Key] = value
		}
	}
	return out
}

// maskSecret shows where a webhook points without showing the part that
// authorizes it: the host for a URL, the ends for anything else.
func maskSecret(value string) string {
	if u, err := url.Parse(value); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host + "/••••"
	}
	runes := []rune(value)
	if len(runes) <= 8 {
		return "••••"
	}
	return string(runes[:3]) + "••••" + string(runes[len(runes)-3:])
}

// PermanentError means retrying the same message cannot help: the platform
// understood the request and refused it.
type PermanentError struct {
	Reason string
	// Gone means the destination itself no longer exists.
	Gone bool
}

func (e *PermanentError) Error() string { return e.Reason }

// IsPermanent reports whether an error is a PermanentError.
func IsPermanent(err error) bool {
	var permanent *PermanentError
	return errors.As(err, &permanent)
}

// Sender delivers messages.
type Sender struct {
	HTTP *http.Client
	// Now is the clock used for signatures; time.Now when nil.
	Now func() time.Time
	// TelegramAPI replaces https://api.telegram.org, for self-hosted bot
	// API servers and for tests.
	TelegramAPI string

	mu    sync.RWMutex
	vapid *VAPID
}

// SetVAPID installs the key pair that signs browser push messages.
func (s *Sender) SetVAPID(keys VAPID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vapid = &keys
}

// VAPID returns the installed key pair, if there is one.
func (s *Sender) VAPID() (VAPID, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.vapid == nil {
		return VAPID{}, false
	}
	return *s.vapid, true
}

// NewSender returns a sender with sensible timeouts.
func NewSender() *Sender {
	return &Sender{HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// Send delivers one message through one channel. The returned error never
// contains the channel's secrets.
func (s *Sender) Send(ctx context.Context, channelType string, config map[string]string, m Message) error {
	t, ok := TypeOf(channelType)
	if !ok {
		return &PermanentError{Reason: fmt.Sprintf("unknown channel type %q", channelType)}
	}
	checked, err := t.CheckConfig(config)
	if err != nil {
		return &PermanentError{Reason: err.Error()}
	}
	err = t.send(ctx, s, checked, m)
	if err == nil {
		return nil
	}
	text := err.Error()
	for _, f := range t.Fields {
		if value := checked[f.Key]; f.Secret && len(value) >= 6 {
			text = strings.ReplaceAll(text, value, "•••")
			if u, parseErr := url.Parse(value); parseErr == nil && u.Path != "" && len(u.Path) >= 6 {
				text = strings.ReplaceAll(text, u.Path, "/•••")
			}
		}
	}
	if IsPermanent(err) {
		return &PermanentError{Reason: text, Gone: IsGone(err)}
	}
	return errors.New(text)
}

func (s *Sender) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// post sends JSON and returns the response body. A status that will not get
// better by retrying comes back as a PermanentError.
func (s *Sender) post(ctx context.Context, target string, payload any, headers map[string]string) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, &PermanentError{Reason: "the message could not be encoded"}
	}
	return s.postRaw(ctx, target, raw, headers)
}

func (s *Sender) postRaw(ctx context.Context, target string, raw []byte, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return nil, &PermanentError{Reason: "the channel address is not valid"}
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	client := s.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			if urlErr.Timeout() {
				return nil, errors.New("the request timed out")
			}
			err = urlErr.Err // drop the URL: it is the secret
		}
		return nil, fmt.Errorf("could not connect: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return body, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return body, fmt.Errorf("HTTP %d: %s", resp.StatusCode, brief(body))
	default:
		return body, &PermanentError{Reason: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, brief(body))}
	}
}

func brief(body []byte) string {
	runes := []rune(strings.TrimSpace(string(body)))
	if len(runes) > 200 {
		return string(runes[:200]) + "…"
	}
	if len(runes) == 0 {
		return "(empty response)"
	}
	return string(runes)
}

// accent is a colour name most card-style platforms understand.
func accent(m Message) string {
	switch {
	case m.Resolved():
		return "green"
	case m.Severity == core.Critical:
		return "red"
	case m.Severity == core.Warning:
		return "orange"
	default:
		return "blue"
	}
}

// ---------- Feishu / Lark ----------

func sendFeishu(ctx context.Context, s *Sender, config map[string]string, m Message) error {
	elements := []map[string]any{}
	if text := m.Markdown(); text != "" {
		elements = append(elements, map[string]any{"tag": "markdown", "content": text})
	}
	if m.Link != "" {
		elements = append(elements, map[string]any{"tag": "action", "actions": []map[string]any{{
			"tag": "button", "type": "default", "url": m.Link,
			"text": map[string]any{"tag": "plain_text", "content": orDefault(m.LinkLabel, "Open")},
		}}})
	}
	if m.Footer != "" {
		elements = append(elements, map[string]any{"tag": "note", "elements": []map[string]any{
			{"tag": "plain_text", "content": m.Footer},
		}})
	}
	payload := map[string]any{
		"msg_type": "interactive",
		"card": map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": m.Title},
				"template": accent(m),
			},
			"elements": elements,
		},
	}
	if secret := config["secret"]; secret != "" {
		// Feishu's scheme: the HMAC key is "timestamp\nsecret" and the
		// message is empty.
		timestamp := strconv.FormatInt(s.now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(timestamp+"\n"+secret))
		payload["timestamp"] = timestamp
		payload["sign"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	}
	body, err := s.post(ctx, config["url"], payload, nil)
	if err != nil {
		return err
	}
	// Feishu answers 200 for a rejected message; the verdict is in the body.
	var verdict struct {
		Code          *int   `json:"code"`
		Msg           string `json:"msg"`
		StatusCode    *int   `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	}
	if json.Unmarshal(body, &verdict) != nil {
		return nil
	}
	switch {
	case verdict.Code != nil && *verdict.Code != 0:
		return &PermanentError{Reason: fmt.Sprintf("Feishu refused the message: %s (code %d)", verdict.Msg, *verdict.Code)}
	case verdict.StatusCode != nil && *verdict.StatusCode != 0:
		return &PermanentError{Reason: fmt.Sprintf("Feishu refused the message: %s (code %d)", verdict.StatusMessage, *verdict.StatusCode)}
	}
	return nil
}

// ---------- DingTalk ----------

func sendDingTalk(ctx context.Context, s *Sender, config map[string]string, m Message) error {
	target := config["url"]
	if secret := config["secret"]; secret != "" {
		timestamp := strconv.FormatInt(s.now().UnixMilli(), 10)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(timestamp + "\n" + secret))
		separator := "&"
		if !strings.Contains(target, "?") {
			separator = "?"
		}
		target += separator + "timestamp=" + timestamp + "&sign=" + url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	}
	// DingTalk's Markdown collapses single line breaks; two spaces before
	// the break keep each fact on its own line.
	text := "### " + m.Title + "\n\n" + strings.ReplaceAll(m.Markdown(), "\n", "  \n")
	if m.Link != "" {
		text += "\n\n[" + orDefault(m.LinkLabel, "Open") + "](" + m.Link + ")"
	}
	body, err := s.post(ctx, target, map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]any{"title": m.Title, "text": text},
	}, nil)
	if err != nil {
		return err
	}
	return errcodeVerdict("DingTalk", body)
}

// ---------- WeCom ----------

func sendWeCom(ctx context.Context, s *Sender, config map[string]string, m Message) error {
	text := "### " + m.Title + "\n" + m.Markdown()
	if m.Link != "" {
		text += "\n\n[" + orDefault(m.LinkLabel, "Open") + "](" + m.Link + ")"
	}
	body, err := s.post(ctx, config["url"], map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]any{"content": text},
	}, nil)
	if err != nil {
		return err
	}
	return errcodeVerdict("WeCom", body)
}

// errcodeVerdict reads the {"errcode": 0} convention DingTalk and WeCom share.
func errcodeVerdict(platform string, body []byte) error {
	var verdict struct {
		ErrCode *int   `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if json.Unmarshal(body, &verdict) != nil || verdict.ErrCode == nil || *verdict.ErrCode == 0 {
		return nil
	}
	return &PermanentError{Reason: fmt.Sprintf("%s refused the message: %s (code %d)", platform, verdict.ErrMsg, *verdict.ErrCode)}
}

// ---------- Slack ----------

func sendSlack(ctx context.Context, s *Sender, config map[string]string, m Message) error {
	// Slack's mrkdwn bolds with single asterisks.
	text := "*" + m.Title + "*\n" + strings.ReplaceAll(m.Markdown(), "**", "*")
	if m.Link != "" {
		text += "\n<" + m.Link + "|" + orDefault(m.LinkLabel, "Open") + ">"
	}
	_, err := s.post(ctx, config["url"], map[string]any{"text": text}, nil)
	return err
}

// ---------- Telegram ----------

func sendTelegram(ctx context.Context, s *Sender, config map[string]string, m Message) error {
	base := orDefault(s.TelegramAPI, "https://api.telegram.org")
	target := strings.TrimRight(base, "/") + "/bot" + config["bot_token"] + "/sendMessage"
	body, err := s.post(ctx, target, map[string]any{
		"chat_id":                  config["chat_id"],
		"text":                     m.HTML(),
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}, nil)
	var verdict struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if json.Unmarshal(body, &verdict) == nil && !verdict.OK && verdict.Description != "" {
		if err != nil && !IsPermanent(err) {
			return fmt.Errorf("Telegram: %s", verdict.Description)
		}
		return &PermanentError{Reason: "Telegram refused the message: " + verdict.Description}
	}
	return err
}

// ---------- ntfy ----------

func sendNtfy(ctx context.Context, s *Sender, config map[string]string, m Message) error {
	priority, tag := 3, "information_source"
	switch {
	case m.Resolved():
		tag = "white_check_mark"
	case m.Severity == core.Critical:
		priority, tag = 5, "rotating_light"
	case m.Severity == core.Warning:
		priority, tag = 4, "warning"
	}
	// Publishing as JSON to the server root keeps non-ASCII titles intact;
	// the header-based API would need each one specially encoded.
	payload := map[string]any{
		"topic": config["topic"], "title": m.Title, "message": orDefault(m.Plain(), m.Title),
		"priority": priority, "tags": []string{tag},
	}
	if m.Link != "" {
		payload["click"] = m.Link
	}
	headers := map[string]string{}
	if token := config["token"]; token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	_, err := s.post(ctx, strings.TrimRight(config["server"], "/")+"/", payload, headers)
	return err
}

// ---------- Bark ----------

func sendBark(ctx context.Context, s *Sender, config map[string]string, m Message) error {
	level := "active"
	if m.Severity == core.Critical && !m.Resolved() {
		level = "timeSensitive"
	}
	payload := map[string]any{
		"device_key": config["device_key"], "title": m.Title, "body": orDefault(m.Plain(), m.Title),
		"group": "QuotaPulse", "level": level,
	}
	if m.Link != "" {
		payload["url"] = m.Link
	}
	body, err := s.post(ctx, strings.TrimRight(config["server"], "/")+"/push", payload, nil)
	if err != nil {
		return err
	}
	var verdict struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &verdict) == nil && verdict.Code != nil && *verdict.Code != 200 {
		return &PermanentError{Reason: fmt.Sprintf("Bark refused the message: %s (code %d)", verdict.Message, *verdict.Code)}
	}
	return nil
}

// ---------- generic webhook ----------

// webhookPayload is a stable contract: fields are added, never renamed.
type webhookPayload struct {
	Version  int       `json:"version"`
	Event    string    `json:"event"`
	Severity string    `json:"severity"`
	Topic    string    `json:"topic"`
	Title    string    `json:"title"`
	Text     string    `json:"text"`
	Facts    []Fact    `json:"facts"`
	Link     string    `json:"link,omitempty"`
	At       time.Time `json:"at"`
	Subject  *Subject  `json:"subject,omitempty"`
}

func sendWebhook(ctx context.Context, s *Sender, config map[string]string, m Message) error {
	facts := m.Facts
	if facts == nil {
		facts = []Fact{}
	}
	raw, err := json.Marshal(webhookPayload{
		Version: 1, Event: m.Event, Severity: string(m.Severity), Topic: m.Topic,
		Title: m.Title, Text: m.Plain(), Facts: facts, Link: m.Link, At: m.At.UTC(), Subject: m.Subject,
	})
	if err != nil {
		return &PermanentError{Reason: "the message could not be encoded"}
	}
	headers := map[string]string{"User-Agent": "QuotaPulse"}
	if secret := config["secret"]; secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(raw)
		headers["X-QuotaPulse-Signature"] = "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}
	_, err = s.postRaw(ctx, config["url"], raw, headers)
	return err
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// SecretKeys lists a type's secret settings, sorted; used when merging an
// edit that left a secret untouched.
func (t Type) SecretKeys() []string {
	var keys []string
	for _, f := range t.Fields {
		if f.Secret {
			keys = append(keys, f.Key)
		}
	}
	sort.Strings(keys)
	return keys
}

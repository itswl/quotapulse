package httpapi

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/mailscan"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/monitor"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/push"
	"github.com/itswl/quotapulse/internal/state"
	"github.com/itswl/quotapulse/internal/store"
	"github.com/itswl/quotapulse/internal/subscription"
)

const testAPIKey = "test-key"

// Implementation note.
func newServer(t *testing.T, tweak func(*config.Settings)) (*Server, http.Handler) {
	t.Helper()

	settings := &config.Settings{
		WebAPIKey: testAPIKey, AppVersion: "1.0.0",
		RequestTimeout: 5, ResponseCacheTTL: 0,
	}
	if tweak != nil {
		tweak(settings)
	}
	st := store.Null()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	resolver := config.NewResolver(settings, st, log)

	s := &Server{
		Settings: settings, Resolver: resolver, Store: st, State: state.New(),
		Monitor: &monitor.Monitor{Settings: settings, Resolver: resolver, Store: st, Log: log},
		Subs:    &subscription.Checker{Store: st, Log: log},
		Scanner: &mailscan.Scanner{Store: st, Log: log},
		Log:     log,
	}
	return s, s.Handler()
}

func request(t *testing.T, handler http.Handler, method, path, body string, withKey bool) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if withKey {
		req.Header.Set("X-API-Key", testAPIKey)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func decode(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, recorder.Body.String())
	}
	return payload
}

// Implementation note.
func TestAuth(t *testing.T) {
	_, handler := newServer(t, nil)

	t.Run("没有密钥回 401", func(t *testing.T) {
		got := request(t, handler, "GET", "/api/features", "", false)
		if got.Code != http.StatusUnauthorized {
			t.Errorf("期望 401，实际 %d", got.Code)
		}
		if decode(t, got)["status"] != "error" {
			t.Error("错误响应要带 status=error")
		}
	})

	t.Run("密钥不对回 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/features", nil)
		req.Header.Set("X-API-Key", "错的")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("期望 401，实际 %d", recorder.Code)
		}
	})

	t.Run("Bearer 也认", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/features", nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Errorf("Bearer 形式应该通过，实际 %d", recorder.Code)
		}
	})

	t.Run("探针不需要密钥", func(t *testing.T) {
		for _, path := range []string{"/live", "/health"} {
			if got := request(t, handler, "GET", path, "", false); got.Code == http.StatusUnauthorized {
				t.Errorf("%s 不该要求密钥", path)
			}
		}
	})

	t.Run("没配密钥时一律 503", func(t *testing.T) {
		_, bare := newServer(t, func(s *config.Settings) { s.WebAPIKey = "" })
		got := request(t, bare, "GET", "/api/features", "", true)
		if got.Code != http.StatusServiceUnavailable {
			t.Errorf("期望 503，实际 %d", got.Code)
		}
	})

	t.Run("MCP 也复用 API 密钥", func(t *testing.T) {
		s, _ := newServer(t, func(settings *config.Settings) { settings.EnableMCP = true })
		s.MCP = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})
		handler := s.Handler()
		if got := request(t, handler, "POST", "/mcp", "{}", false); got.Code != http.StatusUnauthorized {
			t.Errorf("MCP 无密钥应回 401，实际 %d", got.Code)
		}
		if got := request(t, handler, "POST", "/mcp", "{}", true); got.Code != http.StatusOK {
			t.Errorf("MCP 带密钥应通过，实际 %d", got.Code)
		}
	})
}

func TestLiveAndHealth(t *testing.T) {
	s, handler := newServer(t, nil)

	got := request(t, handler, "GET", "/live", "", false)
	if got.Code != http.StatusOK {
		t.Fatalf("/live 应始终 200，实际 %d", got.Code)
	}
	if decode(t, got)["status"] != "alive" {
		t.Error("/live 应返回 alive")
	}

	// Implementation note.
	got = request(t, handler, "GET", "/health", "", false)
	if got.Code != http.StatusServiceUnavailable {
		t.Errorf("没数据时 /health 应为 503，实际 %d", got.Code)
	}
	body := decode(t, got)
	if body["has_data"] != false || body["status"] != "degraded" {
		t.Errorf("/health 字段不对: %v", body)
	}

	s.State.SetBalance([]model.CheckResult{{Project: "a", Provider: "p", Success: true}})
	got = request(t, handler, "GET", "/health", "", false)
	if got.Code != http.StatusOK {
		t.Errorf("有数据后应为 200，实际 %d：%s", got.Code, got.Body.String())
	}
}

func TestFeaturesReflectsToggles(t *testing.T) {
	_, handler := newServer(t, func(s *config.Settings) {
		s.EnableSubscriptions = true
		s.EnableHistoryAPI = true
	})
	body := decode(t, request(t, handler, "GET", "/api/features", "", true))
	features, _ := body["features"].(map[string]any)
	if features["subscriptions"] != true || features["history"] != true || features["dynamic_config"] != false {
		t.Errorf("能力开关没有如实反映: %v", features)
	}
	// No database behind this server, so the dashboard must not promise runway estimates.
	if features["database"] != false {
		t.Errorf("没有数据库时 database 应为 false: %v", features["database"])
	}
}

// A fresh install, or deleting the last project, leaves an empty list after a check.
// That is a real answer, not "not initialized", or the getting-started panel and a
// deleted card's removal would never show.
func TestCreditsWithNoProjectsAfterACheck(t *testing.T) {
	s, handler := newServer(t, nil)
	s.State.SetBalance(nil)

	got := request(t, handler, "GET", "/api/credits", "", true)
	if got.Code != http.StatusOK {
		t.Fatalf("检查过但没有项目时期望 200，实际 %d", got.Code)
	}
	if projects, ok := decode(t, got)["projects"].([]any); !ok || len(projects) != 0 {
		t.Errorf("projects 应是空数组，实际 %v", decode(t, got)["projects"])
	}
}

// Implementation note.
func TestCreditsBeforeFirstCheck(t *testing.T) {
	s, handler := newServer(t, nil)

	if got := request(t, handler, "GET", "/api/credits", "", true); got.Code != http.StatusServiceUnavailable {
		t.Errorf("期望 503，实际 %d", got.Code)
	}

	s.State.SetBalance([]model.CheckResult{{
		Project: "deepseek", Provider: "deepseek", Type: "balance", Success: true,
		Credits: model.Ptr(430.37), Threshold: model.Ptr(50.0),
	}})
	got := request(t, handler, "GET", "/api/credits", "", true)
	if got.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", got.Code)
	}

	body := decode(t, got)
	projects, _ := body["projects"].([]any)
	if len(projects) != 1 {
		t.Fatalf("应有 1 个项目，实际 %d 个", len(projects))
	}
	first, _ := projects[0].(map[string]any)
	for _, key := range []string{"project", "provider", "type", "success", "credits", "threshold", "need_alarm", "alarm_sent", "error", "cached"} {
		if _, ok := first[key]; !ok {
			t.Errorf("响应缺少字段 %q，前端依赖它", key)
		}
	}
	summary, _ := body["summary"].(map[string]any)
	if summary["total"] != float64(1) || summary["success"] != float64(1) {
		t.Errorf("汇总不对: %v", summary)
	}
}

// Implementation note.
func TestETag(t *testing.T) {
	s, handler := newServer(t, nil)
	s.State.SetBalance([]model.CheckResult{{Project: "a", Provider: "p", Success: true}})

	first := request(t, handler, "GET", "/api/credits", "", true)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("响应没有带 ETag")
	}

	req := httptest.NewRequest("GET", "/api/credits", nil)
	req.Header.Set("X-API-Key", testAPIKey)
	req.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, req)

	if second.Code != http.StatusNotModified {
		t.Errorf("ETag 命中应回 304，实际 %d", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Error("304 不该带响应体")
	}
}

// Implementation note.
func TestWritesNeedDynamicConfig(t *testing.T) {
	_, handler := newServer(t, nil)

	if got := request(t, handler, "GET", "/api/config/projects", "", true); got.Code != http.StatusOK {
		t.Errorf("读接口任何时候都该可用，实际 %d", got.Code)
	}

	writes := []struct{ method, path, body string }{
		{"POST", "/api/config/project", `{"name":"x","provider":"deepseek","api_key":"k"}`},
		{"POST", "/api/config/project/delete", `{"name":"x"}`},
		{"POST", "/api/config/threshold", `{"project_name":"x","new_threshold":1}`},
		{"POST", "/api/config/email", `{"name":"x","host":"h","username":"u","password":"p"}`},
		{"POST", "/api/config/email/delete", `{"name":"x"}`},
	}
	for _, w := range writes {
		got := request(t, handler, w.method, w.path, w.body, true)
		if got.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s 期望 503，实际 %d", w.method, w.path, got.Code)
		}
	}
}

// Implementation note.
func TestSubscriptionsNeedFeatureFlag(t *testing.T) {
	_, handler := newServer(t, nil)
	for _, path := range []string{"/api/config/subscriptions"} {
		if got := request(t, handler, "GET", path, "", true); got.Code != http.StatusServiceUnavailable {
			t.Errorf("%s 期望 503，实际 %d", path, got.Code)
		}
	}
	got := request(t, handler, "POST", "/api/subscription/add", `{"name":"x"}`, true)
	if got.Code != http.StatusServiceUnavailable {
		t.Errorf("新增订阅期望 503，实际 %d", got.Code)
	}
}

// Implementation note.
func TestHistoryRoutesAbsentWhenDisabled(t *testing.T) {
	_, off := newServer(t, nil)
	if got := request(t, off, "GET", "/api/history/balance", "", true); got.Code != http.StatusNotFound {
		t.Errorf("未启用时期望 404，实际 %d", got.Code)
	}

	_, on := newServer(t, func(s *config.Settings) { s.EnableHistoryAPI = true })
	if got := request(t, on, "GET", "/api/history/balance", "", true); got.Code != http.StatusOK {
		t.Errorf("启用后期望 200，实际 %d：%s", got.Code, got.Body.String())
	}
}

// Implementation note.
func TestHistoryParamValidation(t *testing.T) {
	_, handler := newServer(t, func(s *config.Settings) { s.EnableHistoryAPI = true })

	tests := []string{
		"/api/history/balance?days=0",
		"/api/history/balance?days=400",
		"/api/history/balance?limit=0",
		"/api/history/balance?days=abc",
		"/api/history/alerts?limit=99999",
	}
	for _, path := range tests {
		got := request(t, handler, "GET", path, "", true)
		if got.Code != http.StatusBadRequest {
			t.Errorf("%s 期望 400，实际 %d", path, got.Code)
		}
	}
}

func TestProviders(t *testing.T) {
	_, handler := newServer(t, nil)
	body := decode(t, request(t, handler, "GET", "/api/providers", "", true))

	providers, _ := body["providers"].([]any)
	if len(providers) < 8 {
		t.Fatalf("应至少有 8 个平台，实际 %d 个", len(providers))
	}
	first, _ := providers[0].(map[string]any)
	for _, key := range []string{"value", "label", "default_type"} {
		if _, ok := first[key]; !ok {
			t.Errorf("平台信息缺少字段 %q，页面下拉框依赖它", key)
		}
	}
}

// Implementation note.
func TestScanDaysValidation(t *testing.T) {
	_, handler := newServer(t, nil)
	for _, body := range []string{`{"days":0}`, `{"days":31}`, `{"days":-1}`} {
		got := request(t, handler, "POST", "/api/email/scan", body, true)
		if got.Code != http.StatusBadRequest {
			t.Errorf("days=%s 期望 400，实际 %d", body, got.Code)
		}
	}
}

// Implementation note.
func TestRefreshCooldown(t *testing.T) {
	_, handler := newServer(t, nil)

	if got := request(t, handler, "POST", "/api/refresh", "", true); got.Code != http.StatusOK {
		t.Fatalf("首次刷新应成功，实际 %d：%s", got.Code, got.Body.String())
	}
	second := request(t, handler, "POST", "/api/refresh", "", true)
	if second.Code != http.StatusTooManyRequests {
		t.Errorf("冷却期内应回 429，实际 %d", second.Code)
	}
	if !strings.Contains(decode(t, second)["message"].(string), "Refresh") {
		t.Error("429 response should describe refresh throttling")
	}
}

// Implementation note.
func TestBadJSONIsRejected(t *testing.T) {
	_, handler := newServer(t, func(s *config.Settings) { s.EnableDynamicConfig = true })
	got := request(t, handler, "POST", "/api/config/project", `{不是 JSON`, true)
	if got.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d", got.Code)
	}
}

// Implementation note.
func TestUnknownProviderRejected(t *testing.T) {
	_, handler := newServer(t, func(s *config.Settings) { s.EnableDynamicConfig = true })
	got := request(t, handler, "POST", "/api/config/project",
		`{"name":"x","provider":"不存在","api_key":"k"}`, true)
	if got.Code != http.StatusBadRequest {
		t.Errorf("期望 400，实际 %d：%s", got.Code, got.Body.String())
	}
}

// Implementation note.
// Implementation note.
// Implementation note.
func TestTrendPathIsDecoded(t *testing.T) {
	_, handler := newServer(t, func(s *config.Settings) { s.EnableHistoryAPI = true })

	// Implementation note.
	for _, path := range []string{
		"/api/history/trend/583276a5396571565636ca419969f306",
		"/api/history/trend/deepseek%3Adeepseek",
		"/api/history/trend/volc%3A%E7%81%AB%E5%B1%B1-%E4%B8%BB%E8%B4%A6%E5%8F%B7",
	} {
		got := request(t, handler, "GET", path, "", true)
		if got.Code != http.StatusNotFound {
			t.Errorf("%s 期望 404（路由命中但无数据），实际 %d：%s", path, got.Code, got.Body.String())
		}
	}
}

type stubNotifier struct{ err error }

func (f *stubNotifier) Send(context.Context, notify.Message) error { return f.err }

func TestNotifyTestEndpoint(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	build := func(n notify.Notifier) http.Handler {
		settings := &config.Settings{WebAPIKey: testAPIKey, AppVersion: "1.0.0"}
		s := &Server{
			Settings: settings, Resolver: config.NewResolver(settings, store.Null(), log),
			Store: store.Null(), State: state.New(), Log: log,
			Subs: &subscription.Checker{Notifier: n, Log: log},
		}
		return s.Handler()
	}
	post := func(h http.Handler) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/notify/test", nil)
		req.Header.Set("X-API-Key", testAPIKey)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := post(build(&stubNotifier{}))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Test notification sent") {
		t.Fatalf("测试通知应成功: %d %s", rec.Code, rec.Body.String())
	}

	rec = post(build(&stubNotifier{err: errors.New("Webhook returned HTTP 500: boom")}))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "HTTP 500") {
		t.Fatalf("webhook 失败应透传 502 与原因: %d %s", rec.Code, rec.Body.String())
	}

	rec = post(build(nil))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not configured") {
		t.Fatalf("未配置 webhook 应报 400 并说明: %d %s", rec.Code, rec.Body.String())
	}
}

func TestValidateWiringReportsMissingDependencies(t *testing.T) {
	s := &Server{}
	err := s.ValidateWiring()
	if err == nil {
		t.Fatal("零值 Server 必须报缺失依赖")
	}
	for _, name := range []string{"Settings", "Store", "Monitor", "Subs", "Scanner", "Push"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("错误应列出 %s: %v", name, err)
		}
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	wired := &Server{
		Settings: &config.Settings{WebAPIKey: testAPIKey},
		Resolver: config.NewResolver(&config.Settings{WebAPIKey: testAPIKey}, store.Null(), log),
		Store:    store.Null(), State: state.New(), Log: log,
		Monitor: &monitor.Monitor{}, Subs: &subscription.Checker{}, Scanner: &mailscan.Scanner{},
		Push: push.New(store.Null()),
	}
	if err := wired.ValidateWiring(); err != nil {
		t.Fatalf("全接线不应报错: %v", err)
	}
}

func TestPushEndpoints(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	settings := &config.Settings{WebAPIKey: testAPIKey, AppVersion: "1"}
	st := store.Null()
	s := &Server{
		Settings: settings, Resolver: config.NewResolver(settings, st, log), Store: st,
		State: state.New(), Log: log, Push: push.New(st),
	}
	handler := s.Handler()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-API-Key", testAPIKey)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec := call(http.MethodGet, "/api/push/config", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("push config 应返回 VAPID 公钥: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "publicKey") {
		t.Fatalf("push config 应带 publicKey 字段: %s", rec.Body.String())
	}

	// 本地假推送服务：订阅确认会真的投递一条 Web Push 到这里
	var pushServiceHits int32
	pushService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&pushServiceHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer pushService.Close()
	// webpush 加密会校验 p256dh 是合法的 P256 曲线点，测试用真实生成的客户端密钥
	clientKey, keyErr := ecdh.P256().GenerateKey(rand.Reader)
	if keyErr != nil {
		t.Fatalf("generate client key: %v", keyErr)
	}
	p256dh := base64.RawURLEncoding.EncodeToString(clientKey.PublicKey().Bytes())
	auth := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	subscribe, _ := json.Marshal(map[string]any{
		"endpoint": pushService.URL + "/push/1",
		"keys":     map[string]string{"p256dh": p256dh, "auth": auth},
	})
	if rec := call(http.MethodPost, "/api/push/subscribe", string(subscribe)); rec.Code != http.StatusOK {
		t.Fatalf("订阅应成功: %d %s", rec.Code, rec.Body.String())
	}
	if atomic.LoadInt32(&pushServiceHits) != 1 {
		t.Fatalf("订阅成功后应立即发一条确认推送, got %d", pushServiceHits)
	}
	if rec := call(http.MethodPost, "/api/push/subscribe", `{"endpoint":""}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 endpoint 应拒绝: %d", rec.Code)
	}

	// 用真实存储验证订阅确实被持久化
	realStore, storeErr := store.Open(context.Background(), store.Options{DatabaseURL: "sqlite://" + filepath.Join(t.TempDir(), "push.db")})
	if storeErr != nil {
		t.Fatalf("open store: %v", storeErr)
	}
	defer realStore.Close()
	subscribe2, _ := json.Marshal(map[string]any{
		"endpoint": pushService.URL + "/push/2",
		"keys":     map[string]string{"p256dh": p256dh, "auth": auth},
	})
	s2 := &Server{
		Settings: settings, Resolver: config.NewResolver(settings, realStore, log), Store: realStore,
		State: state.New(), Log: log, Push: push.New(realStore),
	}
	handler2 := s2.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/push/subscribe", strings.NewReader(string(subscribe2)))
	req.Header.Set("X-API-Key", testAPIKey)
	rec2 := httptest.NewRecorder()
	handler2.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("持久化订阅应成功: %d", rec2.Code)
	}
	subs, listErr := realStore.ListPushSubscriptions(context.Background())
	if listErr != nil || len(subs) != 1 || subs[0].Endpoint != pushService.URL+"/push/2" {
		t.Fatalf("订阅应落库: %v %v", subs, listErr)
	}
}

func TestSubscriptionSettingEndpoints(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	settings := &config.Settings{WebAPIKey: testAPIKey, AppVersion: "1", EnableDynamicConfig: true, EnableSubscriptions: true}
	st := store.Null()
	s := &Server{
		Settings: settings, Resolver: config.NewResolver(settings, st, log), Store: st,
		State: state.New(), Log: log,
		Subs: &subscription.Checker{Store: st, Log: log},
	}
	handler := s.Handler()
	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("X-API-Key", testAPIKey)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := post("/api/subscription/snooze", `{"name":"Netflix","days":14}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "snoozed until") {
		t.Fatalf("snooze 应成功: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("/api/subscription/snooze", `{"name":"Netflix","days":0}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("days=0 应拒绝: %d", rec.Code)
	}
	if rec := post("/api/subscription/timezone", `{"name":"Netflix","timezone":"Mars/Olympus"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("未知时区应拒绝: %d", rec.Code)
	}
	if rec := post("/api/subscription/timezone", `{"name":"Netflix","timezone":"Asia/Shanghai"}`); rec.Code != http.StatusOK {
		t.Fatalf("合法时区应接受: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("/api/subscription/webhook", `{"name":"Netflix","url":"notaurl"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 URL 应拒绝: %d", rec.Code)
	}
	if rec := post("/api/subscription/webhook", `{"name":"Netflix","url":"https://hooks.example.com/x"}`); rec.Code != http.StatusOK {
		t.Fatalf("合法 webhook 应接受: %d %s", rec.Code, rec.Body.String())
	}
}

func TestEmailSuppressionEndpoints(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	settings := &config.Settings{WebAPIKey: testAPIKey, AppVersion: "1", EnableHistoryAPI: true}
	st, storeErr := store.Open(context.Background(), store.Options{DatabaseURL: "sqlite://" + filepath.Join(t.TempDir(), "test.db")})
	if storeErr != nil {
		t.Fatalf("open test store: %v", storeErr)
	}
	defer st.Close()
	s := &Server{Settings: settings, Resolver: config.NewResolver(settings, st, log), Store: st, State: state.New(), Log: log}
	handler := s.Handler()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-API-Key", testAPIKey)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := call(http.MethodPost, "/api/email/suppression", `{"mailbox":"ops@x.com"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 sender 应拒绝: %d", rec.Code)
	}
	if rec := call(http.MethodPost, "/api/email/suppression", `{"mailbox":"ops@x.com","sender":"billing@loud.com"}`); rec.Code != http.StatusOK {
		t.Fatalf("添加抑制应成功: %d %s", rec.Code, rec.Body.String())
	}
	rec := call(http.MethodGet, "/api/email/suppressions", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "billing@loud.com") {
		t.Fatalf("抑制列表应包含刚加的记录: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPost, "/api/email/suppression/delete", `{"mailbox":"ops@x.com","sender":"billing@loud.com"}`); rec.Code != http.StatusOK {
		t.Fatalf("删除抑制应成功: %d", rec.Code)
	}
	rec = call(http.MethodGet, "/api/email/suppressions", "")
	if !strings.Contains(rec.Body.String(), `"count":0`) {
		t.Fatalf("删除后列表应为空: %s", rec.Body.String())
	}
}

// Without the History API the routes don't exist, and without a database a mute can't
// be kept, so neither may answer "success" for a sender that will keep notifying.
func TestEmailSuppressionNeedsHistoryAndStorage(t *testing.T) {
	_, withoutHistory := newServer(t, nil)
	// The SPA's catch-all GET route turns an unknown POST into 405 rather than 404.
	if rec := request(t, withoutHistory, "POST", "/api/email/suppression", `{"mailbox":"ops","sender":"a@b.c"}`, true); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("没开 History API 时应不存在该接口: %d", rec.Code)
	}

	_, noDatabase := newServer(t, func(s *config.Settings) { s.EnableHistoryAPI = true })
	if rec := request(t, noDatabase, "POST", "/api/email/suppression", `{"mailbox":"ops","sender":"a@b.c"}`, true); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("没有数据库时静音无法保存，应返回 503 而不是成功: %d %s", rec.Code, rec.Body.String())
	}
}

// The Email scanning view stays hidden until email is set up: a configured mailbox with
// a scan schedule, or ENABLE_EMAIL_SCAN=true, which shows it before the first mailbox
// exists so that mailbox can be added from the dashboard.
func TestEmailScanFeatureWithoutMailboxes(t *testing.T) {
	flag := func(tweak func(*config.Settings)) bool {
		_, handler := newServer(t, tweak)
		features, _ := decode(t, request(t, handler, "GET", "/api/features", "", true))["features"].(map[string]any)
		return features["email_scan"] == true
	}
	t.Setenv("EMAIL_SCAN_SCHEDULE", "10:00")
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	schedule := func(s *config.Settings) { s.EmailScanTimes = loaded.EmailScanTimes }
	if flag(schedule) {
		t.Error("没有邮箱时默认不应显示邮件页")
	}
	if flag(func(s *config.Settings) { schedule(s); s.EnableDynamicConfig = true }) {
		t.Error("只开动态配置不代表要用邮件扫描，默认不应显示邮件页")
	}
	if !flag(func(s *config.Settings) { s.EnableEmailScan = true; s.EnableDynamicConfig = true }) {
		t.Error("ENABLE_EMAIL_SCAN=true 时即使还没有邮箱也要显示邮件页，好在页面上添加第一个邮箱")
	}

	// A mailbox configured through the environment shows it without the flag, as before.
	t.Setenv("EMAIL_HOST", "imap.example.com")
	t.Setenv("EMAIL_USERNAME", "ops@example.com")
	t.Setenv("EMAIL_PASSWORD", "secret")
	if !flag(schedule) {
		t.Error("已配置邮箱时应显示邮件页")
	}
}

// Values the calendar can't interpret used to be stored as they came.
func TestSubscriptionPatchRangeChecks(t *testing.T) {
	patch := func(start model.Subscription, body string) []string {
		var req subscriptionRequest
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatalf("bad test body %s: %v", body, err)
		}
		return applySubscriptionPatch(&start, req)
	}
	monthly := model.Subscription{Name: "VPS", CycleType: model.CycleMonthly, RenewalDay: 15, AlertDaysBefore: 3}

	for body, wantProblem := range map[string]bool{
		`{"renewal_day": 99}`:                                 true,
		`{"renewal_day": 0}`:                                  true,
		`{"renewal_day": 28}`:                                 false,
		`{"cycle_type": "weekly"}`:                            true, // day 15 is no weekday
		`{"cycle_type": "weekly", "renewal_day": 5}`:          false,
		`{"cycle_type": "yearly", "renewal_day": 1350}`:       true,
		`{"cycle_type": "yearly", "renewal_day": "03-15"}`:    false,
		`{"cycle_type": "lunar_yearly", "renewal_day": 1231}`: true, // lunar months have at most 30 days
		`{"amount": -5}`:                                      true,
		`{"alert_days_before": 400}`:                          true,
		`{"alert_days_before": 0}`:                            false,
	} {
		if got := len(patch(monthly, body)) > 0; got != wantProblem {
			t.Errorf("%s: 期望有问题=%v，实际 %v", body, wantProblem, patch(monthly, body))
		}
	}
}

// MCP keys are read-only agent credentials: accepted on /mcp with their scopes, never on
// the REST API, and rate limited per key.
func TestMCPKeyAuthentication(t *testing.T) {
	agentKey := "qp_agent_key_0123456789"
	expiredAt := time.Now().Add(-time.Hour)
	build := func(tweak func(*config.Settings)) http.Handler {
		s, _ := newServer(t, func(settings *config.Settings) {
			settings.EnableMCP = true
			settings.MCPKeys = []config.MCPKey{
				{Name: "claude", Key: agentKey, Scopes: map[string]bool{config.ScopeBalance: true}},
				{Name: "old", Key: "qp_expired_key_987654321", Scopes: map[string]bool{config.ScopeBalance: true}, Expires: &expiredAt},
			}
			if tweak != nil {
				tweak(settings)
			}
		})
		s.MCP = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			caller, _ := config.MCPCallerFrom(r.Context())
			fmt.Fprintf(w, "%s balance=%v config=%v", caller.Name, caller.Allows(config.ScopeBalance), caller.Allows(config.ScopeConfig))
		})
		return s.Handler()
	}
	call := func(handler http.Handler, path, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Header.Set("X-API-Key", key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	handler := build(nil)
	if rec := call(handler, "/mcp", agentKey); rec.Code != http.StatusOK || rec.Body.String() != "claude balance=true config=false" {
		t.Fatalf("MCP key 应能访问 /mcp 并带上自己的 scope: %d %q", rec.Code, rec.Body.String())
	}
	if rec := call(handler, "/api/credits", agentKey); rec.Code != http.StatusUnauthorized {
		t.Errorf("MCP key 不能用于 REST 接口，否则只读凭证能调写接口: %d", rec.Code)
	}
	if rec := call(handler, "/mcp", testAPIKey); rec.Body.String() != "web balance=true config=true" {
		t.Errorf("WEB_API_KEY 在 /mcp 上应有全部 scope: %q", rec.Body.String())
	}
	if rec := call(handler, "/mcp", "qp_expired_key_987654321"); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "expired") {
		t.Errorf("过期的 key 应被拒绝并说明原因: %d %s", rec.Code, rec.Body.String())
	}

	strict := build(func(s *config.Settings) { s.MCPRequireScopedKey = true })
	if rec := call(strict, "/mcp", testAPIKey); rec.Code != http.StatusUnauthorized {
		t.Errorf("MCP_REQUIRE_SCOPED_KEY 时 /mcp 不应接受 WEB_API_KEY: %d", rec.Code)
	}

	limited := build(func(s *config.Settings) { s.MCPRateLimitPerMinute = 2 })
	for i := range 2 {
		if rec := call(limited, "/mcp", agentKey); rec.Code != http.StatusOK {
			t.Fatalf("第 %d 次请求不应被限速: %d", i+1, rec.Code)
		}
	}
	rec := call(limited, "/mcp", agentKey)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("超出限额应返回 429 并带 Retry-After: %d %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := call(limited, "/mcp", testAPIKey); rec.Code != http.StatusOK {
		t.Errorf("限速按 key 计算，另一个 key 不受影响: %d", rec.Code)
	}
}

package notify

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Sending logs are useless in tests; they bury failure details in the output.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

func TestNewWithoutURLReturnsNil(t *testing.T) {
	// deployments without a webhook must not error; callers skip sending
	n, err := New("", TypeFeishu, "credit-monitor", nil)
	if err != nil {
		t.Fatalf("未配置 URL 时不该报错: %v", err)
	}
	if n != nil {
		t.Fatalf("未配置 URL 时应返回 nil，实际 %#v", n)
	}
}

func TestNewRejectsUnknownType(t *testing.T) {
	_, err := New("https://example.com/hook/x", "slack", "credit-monitor", nil)
	if err == nil {
		t.Fatal("不支持的类型应当报错")
	}
	for _, typ := range SupportedTypes() {
		if !strings.Contains(err.Error(), typ) {
			t.Errorf("错误信息里应列出支持的类型 %q: %v", typ, err)
		}
	}
}

func TestNewNormalizesType(t *testing.T) {
	tests := []struct {
		given string
		want  string
	}{
		{"FeiShu", TypeFeishu},
		{" dingtalk ", TypeDingTalk},
		{"", TypeCustom}, // 没填类型按 custom 处理
	}

	for _, tt := range tests {
		n, err := New("https://example.com/hook/x", tt.given, "s", nil)
		if err != nil {
			t.Fatalf("New(%q) 返回错误: %v", tt.given, err)
		}
		if got := n.(*notifier).typ; got != tt.want {
			t.Errorf("New(%q) 的类型是 %q，期望 %q", tt.given, got, tt.want)
		}
	}
}

func TestSendRetriesServerErrors(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		wantHits int32 // 期望的请求次数
		wantErr  bool
	}{
		{"成功不重试", http.StatusOK, 1, false},
		{"5xx 重试到次数用尽", http.StatusBadGateway, 3, true},
		{"429 限流也重试", http.StatusTooManyRequests, 3, true},
		{"4xx 不重试", http.StatusBadRequest, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hits int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&hits, 1)
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			err := sendNoWait(t, srv, Custom("标题", []string{"**字段**: 值"}, KindRunway))
			if tt.wantErr && err == nil {
				t.Fatalf("HTTP %d 应当返回错误", tt.status)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("HTTP %d 不该返回错误: %v", tt.status, err)
			}
			if got := atomic.LoadInt32(&hits); got != tt.wantHits {
				t.Errorf("实际请求 %d 次，期望 %d 次", got, tt.wantHits)
			}
		})
	}
}

// Failure counts only after retries still fail; recovering mid-way counts as success.
func TestSendSucceedsAfterRetry(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := sendNoWait(t, srv, Custom("标题", []string{"x"}, KindRunway)); err != nil {
		t.Fatalf("第二次就成功了，不该返回错误: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("实际请求 %d 次，期望 2 次", got)
	}
}

// Network-layer failures must propagate, never be swallowed into "sent".
func TestSendReportsTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	client := srv.Client()
	url := srv.URL
	srv.Close() // 端口关掉，模拟机器人地址连不上

	n, err := New(url, TypeFeishu, "s", client)
	if err != nil {
		t.Fatalf("New 返回错误: %v", err)
	}
	n.(*notifier).backoff = []time.Duration{0}

	if err := n.Send(context.Background(), Custom("标题", []string{"x"}, KindRunway)); err == nil {
		t.Fatal("连不上时应当返回错误")
	}
}

// A cancelled ctx must return immediately, not sit in retry waits.
func TestSendStopsOnCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n, err := New(srv.URL, TypeFeishu, "s", srv.Client())
	if err != nil {
		t.Fatalf("New 返回错误: %v", err)
	}
	n.(*notifier).backoff = []time.Duration{time.Hour}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- n.Send(ctx, Custom("标题", []string{"x"}, KindRunway)) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ctx 已取消，应当返回错误")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ctx 取消后 Send 没有及时返回")
	}
}

func TestMaskURL(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"https://open.feishu.cn/open-apis/bot/v2/hook/abcdef123456",
			"https://open.feishu.cn/open-apis/bot/v2/hook/abcd***"},
		{"https://oapi.dingtalk.com/robot/send?access_token=deadbeefcafe",
			"https://oapi.dingtalk.com/robot/send?access_token=dead***"},
		// WeCom's webhook/ hits the hook/ marker first so the key= rule never runs;
		// the outcome is still "no key in logs", as intended
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc123456",
			"https://qyapi.weixin.qq.com/cgi-bin/webhook/send***"},
		{"https://example.com/a", "https://exam***om/a"},
		{"https://example.com/very/long/path/to/endpoint", "https://exam***oint"},
		{"https://x.cn/p", "***"}, // 太短的整体遮掉
		{"", ""},
	}

	for _, tt := range tests {
		if got := MaskURL(tt.raw); got != tt.want {
			t.Errorf("MaskURL(%q) = %q，期望 %q", tt.raw, got, tt.want)
		}
	}
}

// sendNoWait zeroes retry waits so tests never really sleep 2 seconds.
func sendNoWait(t *testing.T, srv *httptest.Server, msg Message) error {
	t.Helper()

	n, err := New(srv.URL, TypeFeishu, "credit-monitor", srv.Client())
	if err != nil {
		t.Fatalf("New 返回错误: %v", err)
	}
	n.(*notifier).backoff = []time.Duration{0, 0}
	return n.Send(context.Background(), msg)
}

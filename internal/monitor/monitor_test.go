package monitor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/store"
)

// Implementation note.
type fakeStore struct {
	store.Store
	mu       sync.Mutex
	balances []store.BalanceRecord
	alerts   []store.AlertRecord
	lastSent *time.Time
}

func newFakeStore() *fakeStore { return &fakeStore{Store: store.Null()} }

func (f *fakeStore) SaveBalance(_ context.Context, rec store.BalanceRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.balances = append(f.balances, rec)
	return nil
}

func (f *fakeStore) SaveAlert(_ context.Context, rec store.AlertRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = append(f.alerts, rec)
	return nil
}

func (f *fakeStore) LastSentAlert(context.Context, string, string, time.Duration) (*time.Time, error) {
	return f.lastSent, nil
}

// Implementation note.
type fakeNotifier struct {
	mu       sync.Mutex
	messages []notify.Message
	err      error
}

func (f *fakeNotifier) Send(_ context.Context, msg notify.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.messages = append(f.messages, msg)
	return nil
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}

// Implementation note.
func newTestMonitor(t *testing.T, projects []model.Project, balance string) (*Monitor, *fakeStore, *fakeNotifier) {
	t.Helper()

	// Implementation note.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"balance_infos":[{"currency":"CNY","total_balance":"` + balance + `"}]}`))
	}))
	t.Cleanup(upstream.Close)

	// Implementation note.
	provider.RegisterSpec(provider.Spec{
		Key: "testupstream", Name: "测试平台", DefaultType: model.TypeBalance,
		URL: upstream.URL,
		Extract: func(data map[string]any) (float64, error) {
			infos, _ := data["balance_infos"].([]any)
			if len(infos) == 0 {
				return 0, errors.New("无法从响应中解析 balance_infos 字段")
			}
			value, _ := provider.Num(provider.Object(infos[0])["total_balance"])
			return value, nil
		},
	})

	st := newFakeStore()
	notifier := &fakeNotifier{}
	settings := &config.Settings{ResponseCacheTTL: 0, RequestTimeout: 5}

	m := &Monitor{
		Settings: settings,
		Resolver: nil, // 这些用例直接调 CheckProject，不走清单解析
		Store:    st,
		Notifier: notifier,
		Client:   provider.NewClient(5 * time.Second),
	}
	return m, st, notifier
}

func testProject(threshold float64) model.Project {
	return model.Project{
		Name: "测试账户", Provider: "testupstream", APIKey: "k",
		Threshold: threshold, Type: model.TypeBalance, Enabled: true,
	}
}

func TestCheckProjectSuccess(t *testing.T) {
	m, st, notifier := newTestMonitor(t, nil, "430.37")
	result := m.CheckProject(context.Background(), testProject(50), false)

	if !result.Success || result.Credits == nil || *result.Credits != 430.37 {
		t.Fatalf("查询结果不对: %+v", result)
	}
	if result.NeedAlarm {
		t.Error("余额高于阈值不该告警")
	}
	if notifier.count() != 0 {
		t.Error("不该发通知")
	}
	if len(st.balances) != 1 || st.balances[0].Balance != 430.37 {
		t.Errorf("应记一条余额历史，实际 %+v", st.balances)
	}
}

func TestCheckProjectNeedsAlarm(t *testing.T) {
	m, st, notifier := newTestMonitor(t, nil, "8.5")
	result := m.CheckProject(context.Background(), testProject(50), false)

	if !result.NeedAlarm {
		t.Fatal("余额低于阈值应该告警")
	}
	if !result.AlarmSent {
		t.Error("应该发出了告警")
	}
	if notifier.count() != 1 {
		t.Fatalf("应发一条通知，实际 %d 条", notifier.count())
	}
	if kind := notifier.messages[0].Kind; kind != "balance" {
		t.Errorf("通知分类应是 balance，实际 %s", kind)
	}
	if len(st.alerts) != 1 || st.alerts[0].AlertType != "low_balance" {
		t.Errorf("应留一条告警记录，实际 %+v", st.alerts)
	}
}

// Implementation note.
func TestDryRunSendsNothing(t *testing.T) {
	m, st, notifier := newTestMonitor(t, nil, "8.5")
	result := m.CheckProject(context.Background(), testProject(50), true)

	if !result.NeedAlarm {
		t.Error("测试模式仍应判断出需要告警")
	}
	if result.AlarmSent || notifier.count() != 0 {
		t.Error("测试模式不该真的发通知")
	}
	if len(st.balances) != 1 {
		t.Error("测试模式仍应记录余额历史，否则跑道分析会缺数据")
	}
	if len(st.alerts) != 0 {
		t.Error("没发出去的告警不该留痕")
	}
}

// Implementation note.
func TestCooldownSkipsDuplicate(t *testing.T) {
	m, st, notifier := newTestMonitor(t, nil, "8.5")
	now := time.Now()
	st.lastSent = &now

	result := m.CheckProject(context.Background(), testProject(50), false)
	if !result.NeedAlarm {
		t.Error("仍应判断出需要告警")
	}
	if result.AlarmSent || notifier.count() != 0 {
		t.Error("冷却窗口内不该重复发送")
	}
}

// Implementation note.
func TestFailedSendIsRecordedButDoesNotBlockRetries(t *testing.T) {
	m, st, notifier := newTestMonitor(t, nil, "8.5")
	notifier.err = errors.New("webhook 超时")

	result := m.CheckProject(context.Background(), testProject(50), false)
	if result.AlarmSent {
		t.Error("发送失败不该标记为已告警")
	}
	if len(st.alerts) != 1 || st.alerts[0].Status != "failed" {
		t.Fatalf("失败必须留痕（status=failed），否则渠道静默失联没人看得见，实际 %+v", st.alerts)
	}
	if !strings.Contains(st.alerts[0].Message, "webhook 超时") {
		t.Errorf("失败记录应带上原因，实际 %q", st.alerts[0].Message)
	}
}

func TestUnknownProviderFails(t *testing.T) {
	m, _, _ := newTestMonitor(t, nil, "1")
	p := testProject(50)
	p.Provider = "不存在的平台"

	result := m.CheckProject(context.Background(), p, false)
	if result.Success {
		t.Fatal("未知平台应当失败")
	}
	if result.Error == nil || !strings.Contains(*result.Error, "Unknown provider") {
		t.Errorf("错误消息应说明是未知平台，实际 %v", result.Error)
	}
	if result.Credits != nil {
		t.Error("失败时余额应为空而不是 0")
	}
}

// Implementation note.
func TestResponseCache(t *testing.T) {
	var hits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"balance_infos":[{"currency":"CNY","total_balance":"100"}]}`))
	}))
	defer upstream.Close()

	provider.RegisterSpec(provider.Spec{
		Key: "cachetest", Name: "缓存测试", DefaultType: model.TypeBalance, URL: upstream.URL,
		Extract: func(data map[string]any) (float64, error) {
			infos, _ := data["balance_infos"].([]any)
			value, _ := provider.Num(provider.Object(infos[0])["total_balance"])
			return value, nil
		},
	})

	m := &Monitor{
		Settings: &config.Settings{ResponseCacheTTL: 300},
		Store:    newFakeStore(), Client: provider.NewClient(5 * time.Second),
	}
	p := model.Project{Name: "缓存账户", Provider: "cachetest", APIKey: "same-key", Threshold: 1, Type: model.TypeBalance}

	first := m.CheckProject(context.Background(), p, true)
	second := m.CheckProject(context.Background(), p, true)

	if hits != 1 {
		t.Errorf("第二次应该命中缓存，上游被打了 %d 次", hits)
	}
	if first.Cached {
		t.Error("第一次不该标记为缓存")
	}
	if !second.Cached {
		t.Error("第二次应标记为缓存，页面上要能看出来")
	}
}

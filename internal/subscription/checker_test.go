package subscription

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/store"
)

type fakeCheckerStore struct {
	store.Store
	lastSent *time.Time
	saved    []store.AlertRecord
}

func (f *fakeCheckerStore) LastSentAlert(context.Context, string, string, time.Duration) (*time.Time, error) {
	return f.lastSent, nil
}

func (f *fakeCheckerStore) SaveAlert(_ context.Context, rec store.AlertRecord) error {
	f.saved = append(f.saved, rec)
	return nil
}

type fakeCheckerNotifier struct {
	calls int
	err   error
}

func (f *fakeCheckerNotifier) Send(context.Context, notify.Message) error {
	f.calls++
	return f.err
}

func quietChecker(storeV store.Store, notifier notify.Notifier) *Checker {
	return &Checker{
		Store: storeV, Notifier: notifier, Cooldown: 24 * time.Hour,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func dispatchSub() model.Subscription {
	return model.Subscription{Name: "Copilot", CycleType: model.CycleMonthly, RenewalDay: 1, Amount: 10, AlertDaysBefore: 3}
}

func TestDispatchSendsAndRecords(t *testing.T) {
	st := &fakeCheckerStore{}
	notifier := &fakeCheckerNotifier{}
	c := quietChecker(st, notifier)

	state, next, lastErr := c.dispatch(context.Background(), dispatchSub(), 3)
	if state != AlertStateSent || lastErr != "" {
		t.Fatalf("首次应发送成功: state=%s err=%q", state, lastErr)
	}
	if next == nil {
		t.Fatal("发送成功后应给出 next_eligible_at")
	}
	got, parseErr := time.Parse(time.RFC3339, *next)
	if parseErr != nil {
		t.Fatalf("next_eligible_at 应是 RFC3339: %v", parseErr)
	}
	if want := time.Now().Add(24 * time.Hour); got.Sub(want) > 2*time.Second || want.Sub(got) > 2*time.Second {
		t.Errorf("next_eligible_at 应约为 now+冷却, got %v", *next)
	}
	if notifier.calls != 1 || len(st.saved) != 1 || st.saved[0].Status != "sent" {
		t.Fatalf("应发送并记录一条 sent: calls=%d saved=%+v", notifier.calls, st.saved)
	}
}

func TestDispatchSkipsInsideCooldown(t *testing.T) {
	last := time.Now().Add(-23 * time.Hour)
	st := &fakeCheckerStore{lastSent: &last}
	notifier := &fakeCheckerNotifier{}
	c := quietChecker(st, notifier)

	state, next, _ := c.dispatch(context.Background(), dispatchSub(), 3)
	if state != AlertStateCooldownSkipped {
		t.Fatalf("冷却期内应跳过: state=%s", state)
	}
	if notifier.calls != 0 || len(st.saved) != 0 {
		t.Fatalf("冷却跳过不应发送或落库: calls=%d saved=%d", notifier.calls, len(st.saved))
	}
	want := last.Add(24 * time.Hour)
	got, parseErr := time.Parse(time.RFC3339, *next)
	if parseErr != nil || got.Sub(want) > 2*time.Second || want.Sub(got) > 2*time.Second {
		t.Errorf("next_eligible_at 应基于上次发送时间, got %v (want %v)", *next, want)
	}
}

func TestDispatchUsesAbsoluteCooldownNotATrailingCount(t *testing.T) {
	// 24h 冷却减 2 秒：仍应跳过。
	st := &fakeCheckerStore{lastSent: ptrTime(time.Now().Add(-24*time.Hour + 2*time.Second))}
	c := quietChecker(st, &fakeCheckerNotifier{})
	if state, _, _ := c.dispatch(context.Background(), dispatchSub(), 3); state != AlertStateCooldownSkipped {
		t.Fatalf("差 2 秒满冷却也应跳过, got %s", state)
	}

	// 24h 冷却加 2 秒：必须重发，绝对时间语义不允许 1 秒的边界误判。
	st2 := &fakeCheckerStore{lastSent: ptrTime(time.Now().Add(-24*time.Hour - 2*time.Second))}
	c2 := quietChecker(st2, &fakeCheckerNotifier{})
	if state, _, _ := c2.dispatch(context.Background(), dispatchSub(), 3); state != AlertStateSent {
		t.Fatalf("过冷却 2 秒就应重发, got %s", state)
	}
}

func TestDispatchRecordsFailures(t *testing.T) {
	st := &fakeCheckerStore{}
	notifier := &fakeCheckerNotifier{err: errors.New("Webhook returned HTTP 500: boom")}
	c := quietChecker(st, notifier)

	state, next, lastErr := c.dispatch(context.Background(), dispatchSub(), 3)
	if state != AlertStateFailed || next != nil {
		t.Fatalf("发送失败应报 failed 且无 next_eligible_at: state=%s next=%v", state, next)
	}
	if !errors.Is(notifier.err, notifier.err) || lastErr == "" || !contains(lastErr, "HTTP 500") {
		t.Errorf("失败原因应原样暴露给 UI, got %q", lastErr)
	}
	if len(st.saved) != 1 || st.saved[0].Status != "failed" || !contains(st.saved[0].Message, "send failed") {
		t.Fatalf("失败必须落库且带原因: %+v", st.saved)
	}
}

func TestDispatchWithoutNotifierFails(t *testing.T) {
	c := quietChecker(&fakeCheckerStore{}, nil)
	state, _, lastErr := c.dispatch(context.Background(), dispatchSub(), 3)
	if state != AlertStateFailed || !contains(lastErr, "not configured") {
		t.Fatalf("未配置 webhook 应报 failed 并说明原因: state=%s err=%q", state, lastErr)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && strings.Contains(haystack, needle)
}

func ptrTime(t time.Time) *time.Time { return &t }

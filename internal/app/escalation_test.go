package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/scheduler"
)

// recordingNotifier captures escalations instead of delivering them; sendErr
// simulates a broken channel.
type recordingNotifier struct {
	messages []notify.Message
	sendErr  error
}

func (r *recordingNotifier) Send(_ context.Context, msg notify.Message) error {
	r.messages = append(r.messages, msg)
	return r.sendErr
}

func newEscalationApp(t *testing.T, threshold int, notifier notify.Notifier) *App {
	t.Helper()
	value := threshold
	return &App{
		Settings: &config.Settings{JobFailureAlertThreshold: &value},
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Notifier: notifier,
	}
}

func failedRun(name string) scheduler.Result {
	return scheduler.Result{Name: name, Success: false, Err: errors.New("provider unreachable")}
}

func TestJobFailureEscalation(t *testing.T) {
	recorder := new(recordingNotifier)
	app := newEscalationApp(t, 3, recorder)

	// Two failures stay below the threshold: no escalation, but the streak builds.
	app.maybeEscalateJobFailure(failedRun("dashboard_refresh"))
	app.maybeEscalateJobFailure(failedRun("dashboard_refresh"))
	if len(recorder.messages) != 0 {
		t.Fatalf("未到阈值不应发送，已发送 %d 条", len(recorder.messages))
	}

	// The third failure escalates exactly once...
	app.maybeEscalateJobFailure(failedRun("dashboard_refresh"))
	if len(recorder.messages) != 1 {
		t.Fatalf("第 3 次连续失败应发送 1 条，得到 %d", len(recorder.messages))
	}
	if title := recorder.messages[0].Title; title != "QuotaPulse job keeps failing" {
		t.Fatalf("标题 = %q", title)
	}

	// ...and further failures do not repeat it.
	app.maybeEscalateJobFailure(failedRun("dashboard_refresh"))
	if len(recorder.messages) != 1 {
		t.Fatalf("已升级后不应重复发送，得到 %d 条", len(recorder.messages))
	}

	// A success clears the streak...
	app.maybeEscalateJobFailure(scheduler.Result{Name: "dashboard_refresh", Success: true})
	// ...so the next streak needs the full threshold again, not one failure.
	app.maybeEscalateJobFailure(failedRun("dashboard_refresh"))
	if len(recorder.messages) != 1 {
		t.Fatalf("成功重置后单次失败不应立即升级，得到 %d 条", len(recorder.messages))
	}
	app.maybeEscalateJobFailure(failedRun("dashboard_refresh"))
	app.maybeEscalateJobFailure(failedRun("dashboard_refresh"))
	if len(recorder.messages) != 2 {
		t.Fatalf("新一轮连续失败应再次升级，得到 %d 条", len(recorder.messages))
	}
}

// Tasks count independently: one task's streak may not spend another's budget.
func TestJobFailureEscalationTracksTasksSeparately(t *testing.T) {
	recorder := new(recordingNotifier)
	app := newEscalationApp(t, 2, recorder)

	app.maybeEscalateJobFailure(failedRun("email_scan"))
	app.maybeEscalateJobFailure(failedRun("alert_check"))
	app.maybeEscalateJobFailure(failedRun("alert_check"))
	if len(recorder.messages) != 1 {
		t.Fatalf("只有 alert_check 达到阈值，应发送 1 条，得到 %d", len(recorder.messages))
	}
	if got := recorder.messages[0].Lines[0]; got != "Task: alert_check" {
		t.Fatalf("升级内容应指向达到阈值的任务，得到 %q", got)
	}
}

// A failed send must not mark the streak escalated: the broken channel has to
// hear about it again on the next failure, once it heals.
func TestJobFailureEscalationRetriesAfterFailedSend(t *testing.T) {
	broken := &recordingNotifier{sendErr: errors.New("webhook down")}
	app := newEscalationApp(t, 1, broken)

	app.maybeEscalateJobFailure(failedRun("weekly_report"))
	if len(broken.messages) != 1 {
		t.Fatalf("应尝试发送 1 次，得到 %d", len(broken.messages))
	}
	app.maybeEscalateJobFailure(failedRun("weekly_report"))
	if len(broken.messages) != 2 {
		t.Fatalf("发送失败不应标记已升级，下一次失败要再试，得到 %d", len(broken.messages))
	}
}

// Zero disables escalation entirely.
func TestJobFailureEscalationDisabled(t *testing.T) {
	recorder := new(recordingNotifier)
	app := newEscalationApp(t, 0, recorder)
	for range 10 {
		app.maybeEscalateJobFailure(failedRun("dashboard_refresh"))
	}
	if len(recorder.messages) != 0 {
		t.Fatalf("阈值为 0 应完全关闭升级，发送了 %d 条", len(recorder.messages))
	}
}

// The nil-notifier deployment (no WEBHOOK_URL) must not panic and must not
// mark anything escalated, so configuring a webhook later still reports.
func TestJobFailureEscalationWithoutNotifier(t *testing.T) {
	app := newEscalationApp(t, 1, nil)
	app.maybeEscalateJobFailure(failedRun("dashboard_refresh"))
	app.jobFailMu.Lock()
	escalated := app.jobEscalated["dashboard_refresh"]
	app.jobFailMu.Unlock()
	if escalated {
		t.Fatal("无通知渠道时不应标记已升级")
	}
}

// BuildTasks registers the retention sweep exactly when retention is on, so a
// deployment can opt out with HISTORY_RETENTION_DAYS=0.
func TestBuildTasksHistoryRetentionToggle(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	enabled := &config.Settings{}
	found := false
	for _, task := range (&App{Settings: enabled, Log: log}).BuildTasks() {
		if task.Name == "history_retention" {
			found = true
			if task.Interval != 24*time.Hour {
				t.Fatalf("清理周期 = %s，应为 24h", task.Interval)
			}
		}
	}
	if !found {
		t.Fatal("默认应注册 history_retention 任务")
	}

	off := 0
	disabled := &config.Settings{HistoryRetentionDays: &off}
	for _, task := range (&App{Settings: disabled, Log: log}).BuildTasks() {
		if task.Name == "history_retention" {
			t.Fatal("HISTORY_RETENTION_DAYS=0 时不应注册清理任务")
		}
	}
}

package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/timeutil"
)

func TestTaskEnabled(t *testing.T) {
	tests := []struct {
		task Task
		want bool
		why  string
	}{
		{Task{Interval: time.Minute}, true, "有间隔"},
		{Task{DailyTimes: []timeutil.ClockTime{{Hour: 9}}}, true, "有时刻"},
		{Task{}, false, "两个都没有就是关闭"},
		{Task{Interval: 0}, false, "间隔为 0 不算启用"},
	}
	for _, tt := range tests {
		if got := tt.task.Enabled(); got != tt.want {
			t.Errorf("%s: 期望 %v，实际 %v", tt.why, tt.want, got)
		}
	}
}

func TestScheduleText(t *testing.T) {
	interval := Task{Interval: 3600 * time.Second}
	if got := interval.ScheduleText(); got != "Every 3600 seconds" {
		t.Errorf("间隔任务描述: 实际 %q", got)
	}
	daily := Task{DailyTimes: []timeutil.ClockTime{{Hour: 9}, {Hour: 15}}}
	if got := daily.ScheduleText(); got != "Daily 09:00 / 15:00" {
		t.Errorf("每日任务描述: 实际 %q", got)
	}
	weekly := Task{DailyTimes: []timeutil.ClockTime{{Hour: 9}}, Weekdays: map[int]bool{1: true}}
	if got := weekly.ScheduleText(); got != "Weekly Mon 09:00" {
		t.Errorf("每周任务描述: 实际 %q", got)
	}
}

// TestRunAtStart: the dashboard refresh must run once at startup or the first minutes of the page stay empty.
func TestRunAtStart(t *testing.T) {
	now := time.Now()
	atStart := Task{Interval: time.Hour, RunAtStart: true}
	if got := atStart.initialNextRun(now); !got.Equal(now) {
		t.Errorf("RunAtStart 的首次时刻应就是现在，实际 %v", got)
	}
	later := Task{Interval: time.Hour}
	if got := later.initialNextRun(now); !got.Equal(now.Add(time.Hour)) {
		t.Errorf("非 RunAtStart 应等一个间隔，实际 %v", got)
	}
}

func TestRunPendingOnlyRunsDueTasks(t *testing.T) {
	var ranDue, ranFuture int
	due := &Task{Name: "due", Interval: time.Hour, RunAtStart: true,
		Run: func(context.Context) (any, error) { ranDue++; return nil, nil }}
	future := &Task{Name: "future", Interval: time.Hour,
		Run: func(context.Context) (any, error) { ranFuture++; return nil, nil }}
	disabled := &Task{Name: "disabled",
		Run: func(context.Context) (any, error) { t.Error("关掉的任务不该运行"); return nil, nil }}

	s := New([]*Task{due, future, disabled}, nil, nil)
	results := s.RunPending(context.Background(), time.Now())

	if len(results) != 1 || results[0].Name != "due" {
		t.Fatalf("应只跑到点的任务，实际 %+v", results)
	}
	if ranDue != 1 || ranFuture != 0 {
		t.Errorf("到点任务跑了 %d 次，未到点任务跑了 %d 次", ranDue, ranFuture)
	}
}

// TestFailureDoesNotStopOtherTasks: one failing task must not affect others.
func TestFailureDoesNotStopOtherTasks(t *testing.T) {
	var secondRan bool
	first := &Task{Name: "炸的", Interval: time.Hour, RunAtStart: true,
		Run: func(context.Context) (any, error) { return nil, errors.New("上游超时") }}
	second := &Task{Name: "好的", Interval: time.Hour, RunAtStart: true,
		Run: func(context.Context) (any, error) { secondRan = true; return "ok", nil }}

	var collected []Result
	s := New([]*Task{first, second}, func(r Result) { collected = append(collected, r) }, nil)
	s.RunPending(context.Background(), time.Now())

	if !secondRan {
		t.Error("第一个任务失败后第二个没跑")
	}
	if len(collected) != 2 {
		t.Fatalf("应回调两次，实际 %d 次", len(collected))
	}
	if collected[0].Success || collected[0].Err == nil {
		t.Error("失败的任务应标记为失败并带上错误")
	}
	if !collected[1].Success || collected[1].Detail != "ok" {
		t.Error("成功的任务应带上 detail")
	}
}

// TestNextRunAdvances: after a run the next time must advance or the loop spins hot.
func TestNextRunAdvances(t *testing.T) {
	task := &Task{Name: "t", Interval: time.Hour, RunAtStart: true,
		Run: func(context.Context) (any, error) { return nil, nil }}
	s := New([]*Task{task}, nil, nil)

	before := task.NextRun()
	s.RunTask(context.Background(), task)
	if !task.NextRun().After(before) {
		t.Errorf("跑完后下次时刻没有推进：%v → %v", before, task.NextRun())
	}
}

func TestSleepForIsBounded(t *testing.T) {
	now := time.Now()
	// with no enabled tasks, wait the max
	s := New([]*Task{{Name: "off"}}, nil, nil)
	if got := s.sleepFor(now); got != time.Minute {
		t.Errorf("没有启用任务时应等上限一分钟，实际 %v", got)
	}

	// overdue tasks must not make the wait negative
	overdue := &Task{Name: "overdue", Interval: time.Hour, RunAtStart: true}
	s = New([]*Task{overdue}, nil, nil)
	if got := s.sleepFor(now.Add(time.Hour)); got < 100*time.Millisecond {
		t.Errorf("等待时间不该小于下限，实际 %v", got)
	}
}

func TestLookup(t *testing.T) {
	task := &Task{Name: "alert_check", Interval: time.Hour}
	s := New([]*Task{task}, nil, nil)
	if s.Lookup("alert_check") != task {
		t.Error("按名字应该能找到任务")
	}
	if s.Lookup("不存在") != nil {
		t.Error("找不到时应返回 nil")
	}
}

// TestStartStop: the loop starts and stops without leaking goroutines.
func TestStartStop(t *testing.T) {
	done := make(chan struct{}, 1)
	task := &Task{Name: "t", Interval: time.Hour, RunAtStart: true,
		Run: func(context.Context) (any, error) {
			select {
			case done <- struct{}{}:
			default:
			}
			return nil, nil
		}}

	s := New([]*Task{task}, nil, nil)
	s.Start(context.Background())
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("启动后任务没有立刻执行")
	}
	s.Stop(2 * time.Second)
}

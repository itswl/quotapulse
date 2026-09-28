package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTaskTimeoutCancelsLongRun(t *testing.T) {
	task := &Task{
		Name: "slow",
		Run: func(ctx context.Context) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
		Timeout: 50 * time.Millisecond,
	}
	s := New([]*Task{task}, nil, nil)

	result := s.RunTask(context.Background(), task)
	if result.Success || result.Err == nil || !strings.Contains(result.Err.Error(), "timed out after 50ms") {
		t.Fatalf("超时任务应记录超时错误: %+v", result)
	}
}

func TestTaskWithoutTimeoutRunsNormally(t *testing.T) {
	task := &Task{Name: "ok", Run: func(context.Context) (any, error) { return "done", nil }}
	s := New([]*Task{task}, nil, nil)

	result := s.RunTask(context.Background(), task)
	if !result.Success || result.Detail != "done" {
		t.Fatalf("无超时的任务不应受影响: %+v", result)
	}
}

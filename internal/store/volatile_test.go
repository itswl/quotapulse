package store

import (
	"testing"
	"time"
)

func TestVolatileStoreRemembersSentAlerts(t *testing.T) {
	s := Volatile().(*volatileStore)
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	ctx := t.Context()

	if s.Enabled() {
		t.Error("没有数据库时 Enabled 应当是 false，否则会打开依赖数据库的功能")
	}
	if last, _ := s.LastSentAlert(ctx, "p1", "low_balance", 24*time.Hour); last != nil {
		t.Fatalf("还没发过告警: %v", last)
	}

	_ = s.SaveAlert(ctx, AlertRecord{AlertID: "p1", AlertType: "low_balance", Status: "failed"})
	if last, _ := s.LastSentAlert(ctx, "p1", "low_balance", 24*time.Hour); last != nil {
		t.Fatal("发送失败不能开始冷却，否则渠道坏了就再也不重试")
	}

	_ = s.SaveAlert(ctx, AlertRecord{AlertID: "p1", AlertType: "low_balance", Status: "sent"})
	if last, _ := s.LastSentAlert(ctx, "p1", "low_balance", 24*time.Hour); last == nil || !last.Equal(now) {
		t.Fatalf("发送成功后应能查到发送时间: %v", last)
	}
	if recent, _ := s.HasRecentAlert(ctx, "p1", "low_balance", 24*time.Hour); !recent {
		t.Error("HasRecentAlert 应与 LastSentAlert 一致")
	}
	if last, _ := s.LastSentAlert(ctx, "p1", "check_failed", 24*time.Hour); last != nil {
		t.Error("不同告警类型的冷却互不影响")
	}
	if last, _ := s.LastSentAlert(ctx, "p2", "low_balance", 24*time.Hour); last != nil {
		t.Error("不同项目的冷却互不影响")
	}

	now = now.Add(25 * time.Hour)
	if last, _ := s.LastSentAlert(ctx, "p1", "low_balance", 24*time.Hour); last != nil {
		t.Error("超出冷却窗口后不应再算作最近发送")
	}

	now = now.Add(volatileRetention)
	_ = s.SaveAlert(ctx, AlertRecord{AlertID: "p9", AlertType: "low_balance", Status: "sent"})
	if _, kept := s.sent[alertKey{"p1", "low_balance"}]; kept {
		t.Error("超过保留期的记录应被清理，内存不能无限增长")
	}
}

func TestVolatileStoreDeduplicatesSentEmails(t *testing.T) {
	s := Volatile().(*volatileStore)
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	ctx := t.Context()
	rec := EmailAlertRecord{Mailbox: "Ops", Sender: "billing@example.com", Subject: "Low balance", Date: "2026-09-30"}

	_ = s.SaveEmailAlert(ctx, rec)
	if dup, _ := s.HasRecentEmailAlert(ctx, rec.Mailbox, rec.Sender, rec.Subject, rec.Date, 7); dup {
		t.Fatal("没发出通知的邮件不算重复，否则下次扫描也不会再通知")
	}

	rec.AlertSent = true
	_ = s.SaveEmailAlert(ctx, rec)
	if dup, _ := s.HasRecentEmailAlert(ctx, rec.Mailbox, rec.Sender, rec.Subject, rec.Date, 7); !dup {
		t.Fatal("已通知过的同一封邮件应被去重")
	}
	if dup, _ := s.HasRecentEmailAlert(ctx, rec.Mailbox, rec.Sender, "Other subject", rec.Date, 7); dup {
		t.Error("不同主题不算重复")
	}

	now = now.Add(8 * 24 * time.Hour)
	if dup, _ := s.HasRecentEmailAlert(ctx, rec.Mailbox, rec.Sender, rec.Subject, rec.Date, 7); dup {
		t.Error("超出去重天数后不应再算重复")
	}
}

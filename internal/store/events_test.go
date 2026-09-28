package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMergeEventsInterleavesAndTrims(t *testing.T) {
	alerts := []AlertRow{
		{AlertType: "low_balance", ProjectName: "volc-1", Status: "sent", Message: "Low balance: 8 < 50", Timestamp: "2026-09-28T09:00:00Z"},
		{AlertType: "spend_spike", ProjectName: "deepseek", Status: "failed", Message: "Spike detected", Timestamp: "2026-09-28T12:00:00Z"},
	}
	emails := []EmailAlertRow{
		{Mailbox: "ops@example.com", Subject: "Invoice ready", Timestamp: "2026-09-28T10:00:00Z"},
	}

	events := MergeEvents(alerts, emails, 2)
	if len(events) != 2 {
		t.Fatalf("limit 应裁剪到 2, got %d", len(events))
	}
	if events[0].Type != "spend_spike" || events[0].Status != "failed" {
		t.Errorf("最新事件应是 spend_spike 并保留状态: %+v", events[0])
	}
	if events[1].Type != "email_alert" || events[1].Source != "ops@example.com" {
		t.Errorf("第二条应是 email_alert 并带邮箱来源: %+v", events[1])
	}
	if body, err := json.Marshal(events); err != nil || !json.Valid(body) {
		t.Errorf("events 应可序列化: %v", err)
	}
}

func TestSqliteBackupRoundtrip(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(context.Background(), Options{DatabaseURL: "sqlite://" + filepath.Join(dir, "main.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	if err := st.SaveAlert(context.Background(), AlertRecord{AlertID: "p1", AlertType: "low_balance", Status: "sent", Message: "Low balance: 1 < 5"}); err != nil {
		t.Fatalf("SaveAlert: %v", err)
	}

	backupDir := filepath.Join(dir, "backups")
	path1, err := st.Backup(context.Background(), backupDir, 2)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if info, err := os.Stat(path1); err != nil || info.Size() < 500 {
		t.Fatalf("备份文件应存在且有内容: %v %v", info, err)
	}

	time.Sleep(1100 * time.Millisecond) // 文件名精确到秒，错开才能验证清理
	if _, err := st.Backup(context.Background(), backupDir, 1); err != nil {
		t.Fatalf("second Backup: %v", err)
	}
	entries, _ := os.ReadDir(backupDir)
	if len(entries) != 1 {
		t.Fatalf("keep=1 应只保留最新备份, got %d", len(entries))
	}
}

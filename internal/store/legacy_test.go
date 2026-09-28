package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

// fixtureWindowDays：testdata/legacy.db 里的时间戳是固定的，而所有查询窗口都相对
// 「现在」计算。7 天、30 天这样的窗口迟早滑出快照日期，让测试在某一天开始失败
// （2026-09-28 就炸过一次）。这里统一用一个不会过期的窗口（约 100 年）：被测的是
// 读取与折算逻辑，窗口大小只是参数。
const fixtureWindowDays = 36500

// Implementation note.
//
// Implementation note.
// Implementation note.
func TestReadsLegacyDatabase(t *testing.T) {
	// Implementation note.
	source, err := os.ReadFile("testdata/legacy.db")
	if err != nil {
		t.Fatalf("读取旧版数据库失败: %v", err)
	}
	path := filepath.Join(t.TempDir(), "legacy.db")
	if err := os.WriteFile(path, source, 0o644); err != nil {
		t.Fatalf("复制数据库失败: %v", err)
	}

	ctx := context.Background()
	st, err := Open(ctx, Options{DatabaseURL: "sqlite:///" + path})
	if err != nil {
		t.Fatalf("打开旧版数据库失败: %v", err)
	}
	defer st.Close()

	t.Run("项目配置", func(t *testing.T) {
		projects, err := st.ListProjects(ctx)
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		if len(projects) != 2 {
			t.Fatalf("应有 2 个项目，实际 %d 个", len(projects))
		}
		volc := findByName(projects, func(p model.Project) string { return p.Name }, "火山-主账号")
		if volc == nil {
			t.Fatal("没读到「火山-主账号」，中文字段可能坏了")
		}
		if volc.Provider != "volc" || volc.APIKey != "AK:SK" || volc.Threshold != 7000 {
			t.Errorf("项目字段不对: %+v", *volc)
		}
		if volc.OwnerProject == nil || *volc.OwnerProject != "云服务" {
			t.Errorf("分组标签不对: %v", volc.OwnerProject)
		}
		if !volc.Enabled {
			t.Error("enabled 应为 true")
		}
		// Implementation note.
		deepseek := findByName(projects, func(p model.Project) string { return p.Name }, "deepseek")
		if deepseek == nil || deepseek.Enabled {
			t.Errorf("deepseek 应是停用状态，实际 %+v", deepseek)
		}
	})

	t.Run("订阅配置", func(t *testing.T) {
		subs, err := st.ListSubscriptions(ctx)
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		if len(subs) != 1 {
			t.Fatalf("应有 1 条订阅，实际 %d 条", len(subs))
		}
		s := subs[0]
		// Implementation note.
		if s.Name != "域名续费" || s.CycleType != "yearly" || s.RenewalDay != 315 {
			t.Errorf("订阅字段不对: %+v", s)
		}
		if s.Amount != 88 || s.AlertDaysBefore != 3 {
			t.Errorf("金额或提前天数不对: %+v", s)
		}
	})

	t.Run("邮箱配置", func(t *testing.T) {
		boxes, err := st.ListMailboxes(ctx)
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		if len(boxes) != 1 {
			t.Fatalf("应有 1 个邮箱，实际 %d 个", len(boxes))
		}
		m := boxes[0]
		if m.Name != "工作邮箱" || m.Host != "imap.example.com" || m.Port != 993 || !m.UseSSL {
			t.Errorf("邮箱字段不对: %+v", m)
		}
		if m.Password != "pw" {
			t.Errorf("密码应原样读出（旧库未加密），实际 %q", m.Password)
		}
	})

	t.Run("余额历史与跑道输入", func(t *testing.T) {
		series, err := st.BalanceSeries(ctx, fixtureWindowDays)
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		if len(series) != 5 {
			t.Fatalf("应有 5 条快照，实际 %d 条", len(series))
		}
		// Implementation note.
		for i := 1; i < len(series); i++ {
			if series[i].Timestamp < series[i-1].Timestamp {
				t.Fatal("余额快照没有按时间升序返回")
			}
		}
		if series[0].Balance != 9000 || series[4].Balance != 6900 {
			t.Errorf("余额数值不对: 首 %v 末 %v", series[0].Balance, series[4].Balance)
		}
		if series[0].ProjectName != "火山-主账号" || series[0].Provider != "volc" {
			t.Errorf("账户信息不对: %+v", series[0])
		}
		if series[0].Timestamp <= 0 {
			t.Error("时间戳没读出来，跑道分析会算不出跨度")
		}
	})

	t.Run("余额趋势", func(t *testing.T) {
		series, _ := st.BalanceSeries(ctx, fixtureWindowDays)
		if len(series) == 0 {
			t.Fatal("旧库里读不到任何快照，无法构造趋势输入")
		}
		trend, err := st.BalanceTrend(ctx, series[0].ProjectID, fixtureWindowDays)
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		if trend == nil {
			t.Fatal("有数据却返回了空趋势")
		}
		if trend.DataPoints != 5 || trend.MinBalance != 6900 || trend.MaxBalance != 9000 {
			t.Errorf("趋势统计不对: %+v", *trend)
		}
		if trend.Change == nil || *trend.Change != -2100 {
			t.Errorf("变化量应为 -2100，实际 %v", trend.Change)
		}
	})

	t.Run("没有数据的项目返回空趋势", func(t *testing.T) {
		trend, err := st.BalanceTrend(ctx, "不存在的项目", 30)
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		if trend != nil {
			t.Error("没有数据时应返回 nil，让接口回 404")
		}
	})

	t.Run("告警历史与冷却", func(t *testing.T) {
		alerts, err := st.RecentAlerts(ctx, AlertQuery{Days: fixtureWindowDays, Limit: 50})
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		if len(alerts) != 1 {
			t.Fatalf("应有 1 条告警，实际 %d 条", len(alerts))
		}
		if alerts[0].AlertType != "low_balance" || alerts[0].Status != "sent" {
			t.Errorf("告警字段不对: %+v", alerts[0])
		}

		// Implementation note.
		series, _ := st.BalanceSeries(ctx, fixtureWindowDays)
		if len(series) == 0 {
			t.Fatal("旧库里读不到任何快照，无法构造冷却输入")
		}
		cooling, err := st.HasRecentAlert(ctx, series[0].ProjectID, "low_balance", 365*24*time.Hour)
		if err != nil {
			t.Fatalf("查询冷却失败: %v", err)
		}
		if !cooling {
			t.Error("旧库里的告警记录没有让冷却生效")
		}
	})

	t.Run("邮件告警历史", func(t *testing.T) {
		rows, err := st.EmailAlerts(ctx, EmailAlertQuery{Days: fixtureWindowDays, Limit: 100})
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("应有 1 条，实际 %d 条", len(rows))
		}
		r := rows[0]
		if r.Mailbox != "工作邮箱" || r.Subject != "【阿里云】余额不足提醒" {
			t.Errorf("中文字段读坏了: %+v", r)
		}
		if r.ServiceName == nil || *r.ServiceName != "阿里云" {
			t.Errorf("服务名不对: %v", r.ServiceName)
		}
		if r.Amount == nil || *r.Amount != 12.5 {
			t.Errorf("金额不对: %v", r.Amount)
		}
		if !r.AlertSent {
			t.Error("alert_sent 应为 true")
		}

		// Implementation note.
		seen, err := st.HasRecentEmailAlert(ctx, "工作邮箱", "noreply@aliyun.com",
			"【阿里云】余额不足提醒", "Mon, 01 Sep 2026 10:00:00 +0800", fixtureWindowDays)
		if err != nil {
			t.Fatalf("查询去重失败: %v", err)
		}
		if !seen {
			t.Error("旧库里的邮件没有被认作已通知过")
		}
	})

	t.Run("能继续往旧库里写", func(t *testing.T) {
		err := st.UpsertProject(ctx, model.Project{
			Name: "新项目", Provider: "glm", APIKey: "id.secret",
			Threshold: 10, Type: model.TypeQuota, Enabled: true,
		})
		if err != nil {
			t.Fatalf("写入失败: %v", err)
		}
		projects, _ := st.ListProjects(ctx)
		if len(projects) != 3 {
			t.Errorf("写入后应有 3 个项目，实际 %d 个", len(projects))
		}
	})
}

func findByName[T any](items []T, name func(T) string, want string) *T {
	for i := range items {
		if name(items[i]) == want {
			return &items[i]
		}
	}
	return nil
}

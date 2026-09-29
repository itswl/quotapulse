package state

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

func result(name string, success bool, needAlarm bool) model.CheckResult {
	return model.CheckResult{Project: name, Provider: "deepseek", Success: success, NeedAlarm: needAlarm}
}

func TestBalanceSummary(t *testing.T) {
	m := New()
	m.SetBalance([]model.CheckResult{
		result("a", true, false), result("b", true, true), result("c", false, false),
	})

	got := m.Balance()
	want := model.BalanceSummary{Total: 3, Success: 2, Failed: 1, NeedAlarm: 1}
	if got.Summary != want {
		t.Errorf("汇总: 期望 %+v，实际 %+v", want, got.Summary)
	}
	if got.LastUpdate == nil {
		t.Error("更新后应该有 last_update")
	}
}

// Implementation note.
func TestMergeKeepsOrder(t *testing.T) {
	m := New()
	m.SetBalance([]model.CheckResult{result("a", true, false), result("b", true, false), result("c", true, false)})

	m.MergeBalance([]model.CheckResult{result("b", true, true)})

	projects := m.Balance().Projects
	if len(projects) != 3 {
		t.Fatalf("合并后应仍是 3 个项目，实际 %d", len(projects))
	}
	for i, name := range []string{"a", "b", "c"} {
		if projects[i].Project != name {
			t.Errorf("第 %d 个应是 %s，实际 %s", i, name, projects[i].Project)
		}
	}
	if !projects[1].NeedAlarm {
		t.Error("b 的新状态没有合并进去")
	}
}

// Implementation note.
func TestMergeAddsUnknownProject(t *testing.T) {
	m := New()
	m.SetBalance([]model.CheckResult{result("a", true, false)})
	m.MergeBalance([]model.CheckResult{result("新项目", true, false)})

	if projects := m.Balance().Projects; len(projects) != 2 || projects[1].Project != "新项目" {
		t.Errorf("新项目没有被加入，得到 %v", projects)
	}
}

func TestRemoveBalanceProject(t *testing.T) {
	m := New()
	m.SetBalance([]model.CheckResult{result("a", true, false), result("b", true, false)})

	m.RemoveBalanceProject("a")
	projects := m.Balance().Projects
	if len(projects) != 1 || projects[0].Project != "b" {
		t.Errorf("删除后应只剩 b，实际 %v", projects)
	}

	// Implementation note.
	before := m.Balance().LastUpdate
	m.RemoveBalanceProject("不存在")
	if m.Balance().LastUpdate != before {
		t.Error("删除不存在的项目不该刷新 last_update")
	}
}

// Implementation note.
func TestReturnedStateIsACopy(t *testing.T) {
	m := New()
	m.SetBalance([]model.CheckResult{result("a", true, false)})

	snapshot := m.Balance()
	snapshot.Projects[0].Project = "被改了"

	if m.Balance().Projects[0].Project != "a" {
		t.Error("外部修改副本影响到了内部状态")
	}
}

func TestJobHealth(t *testing.T) {
	m := New()
	next := time.Now().Add(time.Hour)
	m.RegisterJob("alert_check", "余额与订阅告警检查", "每天 09:00", true, next)
	m.RegisterJob("disabled_job", "关掉的任务", "已关闭", false, time.Time{})

	if !m.Jobs().Healthy {
		t.Error("还没跑过的任务不该算不健康")
	}

	m.RecordJobRun("alert_check", false, time.Now(), time.Second, errors.New("上游超时"), nil, next)
	if m.Jobs().Healthy {
		t.Error("任务失败后应该不健康")
	}
	if failed := m.FailedJobs(); len(failed) != 1 || failed[0] != "alert_check" {
		t.Errorf("失败任务清单应是 [alert_check]，实际 %v", failed)
	}

	m.RecordJobRun("alert_check", true, time.Now(), time.Second, nil, map[string]any{"projects": 2}, next)
	if !m.Jobs().Healthy {
		t.Error("重新成功后应恢复健康")
	}

	job := m.Jobs().Jobs[0]
	if job.Runs != 2 || job.Failures != 1 {
		t.Errorf("运行次数应为 2 次 1 失败，实际 %d 次 %d 失败", job.Runs, job.Failures)
	}
	if job.LastError != nil {
		t.Error("成功后应清掉上次错误")
	}
}

// Implementation note.
func TestDisabledJobNeverBlocksHealth(t *testing.T) {
	m := New()
	m.RegisterJob("weekly_report", "周报", "已关闭", false, time.Time{})
	m.RecordJobRun("weekly_report", false, time.Now(), time.Second, errors.New("炸了"), nil, time.Time{})

	if !m.Jobs().Healthy {
		t.Error("关掉的任务失败不该让整体不健康")
	}
	if len(m.FailedJobs()) != 0 {
		t.Error("关掉的任务不该出现在失败清单里")
	}
}

// Implementation note.
func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			m.SetBalance([]model.CheckResult{result("a", true, i%2 == 0)})
		}()
		go func() {
			defer wg.Done()
			_ = m.Balance()
			_ = m.Jobs()
		}()
	}
	wg.Wait()
}

func TestEmailSummary(t *testing.T) {
	m := New()
	failure := "连不上"
	m.SetEmailScan(model.ScanResult{
		Days: 3, DryRun: true,
		Mailboxes: []model.MailboxResult{
			{Name: "工作", TotalEmails: 10, Success: true},
			{Name: "备用", TotalEmails: 2, Error: &failure},
		},
		Alerts: []model.EmailAlert{{Mailbox: "工作", AlertSent: true}, {Mailbox: "工作"}},
	})

	got := m.EmailScan().Summary
	want := EmailSummary{TotalMailboxes: 2, FailedMailboxes: 1, TotalEmails: 12, TotalAlerts: 2, AlertsSent: 1}
	if got != want {
		t.Errorf("邮箱汇总: 期望 %+v，实际 %+v", want, got)
	}
}

// last_update is when balances were last read: an outage where every check fails must
// not stamp "updated just now", and removing a card is not a check.
func TestLastUpdateOnlyAdvancesOnSuccessfulReads(t *testing.T) {
	m := New()
	if m.BalanceChecked() {
		t.Fatal("启动后还没检查过")
	}

	m.SetBalance([]model.CheckResult{result("a", false, false), result("b", false, false)})
	if !m.BalanceChecked() {
		t.Error("检查完成后应标记为已检查，即使全部失败")
	}
	if m.Balance().LastUpdate != nil {
		t.Error("全部失败时不应产生 last_update")
	}

	m.SetBalance([]model.CheckResult{result("a", true, false), result("b", false, false)})
	first := m.Balance().LastUpdate
	if first == nil {
		t.Fatal("有成功的读取就应更新 last_update")
	}

	time.Sleep(2 * time.Millisecond)
	m.SetBalance([]model.CheckResult{result("a", false, false), result("b", false, false)})
	if got := m.Balance().LastUpdate; got == nil || *got != *first {
		t.Errorf("全部失败时应保留上次成功的时间 %v，实际 %v", *first, got)
	}
	if m.Balance().Summary.Failed != 2 {
		t.Error("失败结果本身仍要存下来，页面要显示错误")
	}

	m.MergeBalance([]model.CheckResult{result("b", false, false)})
	if got := m.Balance().LastUpdate; *got != *first {
		t.Error("单个项目刷新失败不应更新 last_update")
	}

	empty := New()
	empty.SetBalance(nil)
	if !empty.BalanceChecked() || empty.Balance().LastUpdate == nil {
		t.Error("没有项目时的检查也是一次成功的检查")
	}
}

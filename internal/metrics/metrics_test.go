package metrics

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/itswl/quotapulse/internal/model"
)

// Assertions always run against the scraped text rather than reading Vec
// internals: the contract is "what Prometheus scrapes", and
// WithLabelValues would helpfully recreate pruned series and hide the bug.
//
// prometheus/testutil is not used: it depends on kylelemons/godebug, absent
// from this repository's go.sum, and this task may not touch go.mod. The
// helpers below are equivalents of testutil's GatherAndCompare /
// ToFloat64 / CollectAndCount, all going through the official Gather +
// expfmt path.

// Fixed clock: metrics like last_check_timestamp need exact assertions.
var testNow = time.Date(2026, 9, 16, 10, 30, 0, 0, time.UTC)

func newTestCollector(t *testing.T) (*Collector, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	c := New(reg)
	c.now = func() time.Time { return testNow }
	return c, reg
}

func gather(t *testing.T, reg *prometheus.Registry) []*dto.MetricFamily {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather 失败: %v", err)
	}
	return mfs
}

// encode renders a metric family to text format; Gather output families and series are already sorted.
func encode(t *testing.T, mfs []*dto.MetricFamily) string {
	t.Helper()
	var buf bytes.Buffer
	enc := expfmt.NewEncoder(&buf, expfmt.NewFormat(expfmt.TypeTextPlain))
	for _, mf := range mfs {
		if err := enc.Encode(mf); err != nil {
			t.Fatalf("编码 %s 失败: %v", mf.GetName(), err)
		}
	}
	return buf.String()
}

// dump is the scrape text of the entire registry.
func dump(t *testing.T, reg *prometheus.Registry) string {
	t.Helper()
	return encode(t, gather(t, reg))
}

// compareText compares the full output of given metrics, equivalent to testutil.GatherAndCompare.
func compareText(t *testing.T, reg *prometheus.Registry, expected string, names ...string) {
	t.Helper()
	keep := map[string]bool{}
	for _, n := range names {
		keep[n] = true
	}
	var filtered []*dto.MetricFamily
	for _, mf := range gather(t, reg) {
		if keep[mf.GetName()] {
			filtered = append(filtered, mf)
		}
	}
	got := strings.TrimSpace(encode(t, filtered))
	if want := strings.TrimSpace(expected); got != want {
		t.Errorf("指标输出不符。\n实际:\n%s\n期望:\n%s", got, want)
	}
}

func mustContain(t *testing.T, text string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains(text, line) {
			t.Errorf("缺少指标行:\n%s\n实际输出:\n%s", line, text)
		}
	}
}

func mustNotContain(t *testing.T, text string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if strings.Contains(text, line) {
			t.Errorf("本应被清理掉的指标行还在:\n%s\n实际输出:\n%s", line, text)
		}
	}
}

// seriesCount is how many series a metric currently has; 0 when the metric does not exist.
func seriesCount(t *testing.T, reg *prometheus.Registry, name string) int {
	t.Helper()
	for _, mf := range gather(t, reg) {
		if mf.GetName() == name {
			return len(mf.GetMetric())
		}
	}
	return 0
}

// gaugeValue reads one Gauge series value; failing to read fails the case.
func gaugeValue(t *testing.T, reg *prometheus.Registry, name string, labels ...string) float64 {
	t.Helper()
	for _, mf := range gather(t, reg) {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			values := make([]string, 0, len(m.GetLabel()))
			for _, lp := range m.GetLabel() {
				values = append(values, lp.GetValue())
			}
			if strings.Join(values, "\x00") == strings.Join(labels, "\x00") {
				return m.GetGauge().GetValue()
			}
		}
	}
	t.Fatalf("没有找到序列 %s%v", name, labels)
	return 0
}

func okResult(project, provider, balanceType string, credits, threshold float64, needAlarm bool) model.CheckResult {
	return model.CheckResult{
		Project:   project,
		Provider:  provider,
		Type:      balanceType,
		Success:   true,
		Credits:   model.Ptr(credits),
		Threshold: model.Ptr(threshold),
		NeedAlarm: needAlarm,
	}
}

func failedResult(project, provider, balanceType string) model.CheckResult {
	return model.CheckResult{
		Project:  project,
		Provider: provider,
		Type:     balanceType,
		Success:  false,
		Error:    model.Ptr("connection timeout"),
	}
}

// exerciseAll walks every update path so each metric has at least one series.
func exerciseAll(c *Collector) {
	c.UpdateBalance([]model.CheckResult{{
		Project: "openai-main", Provider: "openai", Type: model.TypeBalance, Success: true,
		Credits: model.Ptr(42.5), Threshold: model.Ptr(50.0),
		Runway: &model.Runway{BurnPerDay: model.Ptr(3.0), RunwayDays: model.Ptr(14.0)},
	}})
	c.UpdateSubscriptions([]model.SubscriptionResult{
		{Name: "claude", CycleType: model.CycleMonthly, DaysUntilRenewal: 3, Amount: 20},
	})
	c.UpdateEmailScan(model.ScanResult{Mailboxes: []model.MailboxResult{
		{Name: "work", TotalEmails: 10, AlertCount: 2, Success: true},
	}})
	c.RecordJobRun("alert_check", true, testNow, 1500*time.Millisecond)
	c.RecordNotification("balance", true)
}

// The metric inventory is the contract, checked line by line against grafana/README.md: a missing one means a No Data panel, an extra one means a typo.
func TestMetricInventoryMatchesContract(t *testing.T) {
	c, reg := newTestCollector(t)
	exerciseAll(c)

	// value is the label names sorted by name; Gather output labels are sorted anyway.
	want := map[string]string{
		"quotapulse_balance":                    "project,provider,type",
		"quotapulse_threshold":                  "project,provider,type",
		"quotapulse_ratio":                      "project,provider,type",
		"quotapulse_status":                     "project,provider,type",
		"quotapulse_check_status":               "project,provider,type",
		"quotapulse_burn_rate_per_day":          "project,provider,type",
		"quotapulse_runway_days":                "project,provider,type",
		"quotapulse_subscription_days":          "cycle_type,name",
		"quotapulse_subscription_amount":        "cycle_type,name",
		"quotapulse_subscription_status":        "cycle_type,name",
		"quotapulse_email_mailbox_status":       "mailbox",
		"quotapulse_email_last_scan_emails":     "mailbox",
		"quotapulse_email_last_scan_alerts":     "mailbox",
		"quotapulse_email_scan_total":           "mailbox",
		"quotapulse_email_alerts_total":         "mailbox",
		"quotapulse_job_last_run_timestamp":     "task",
		"quotapulse_job_last_success_timestamp": "task",
		"quotapulse_job_last_status":            "task",
		"quotapulse_job_last_duration_seconds":  "task",
		"quotapulse_job_runs_total":             "status,task",
		"quotapulse_notifications_total":        "kind,status",
		"quotapulse_last_check_timestamp":       "check_type",
	}

	got := map[string]string{}
	for _, mf := range gather(t, reg) {
		if len(mf.GetMetric()) == 0 {
			t.Errorf("%s 没有任何序列", mf.GetName())
			continue
		}
		names := make([]string, 0, len(mf.GetMetric()[0].GetLabel()))
		for _, lp := range mf.GetMetric()[0].GetLabel() {
			names = append(names, lp.GetName())
		}
		sort.Strings(names)
		got[mf.GetName()] = strings.Join(names, ",")
	}

	for name, labels := range want {
		actual, ok := got[name]
		if !ok {
			t.Errorf("契约里的指标没有暴露出来: %s", name)
			continue
		}
		if actual != labels {
			t.Errorf("%s 的标签是 %q，契约要求 %q", name, actual, labels)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("多暴露了契约之外的指标: %s", name)
		}
	}
}

func TestBalanceMetricsText(t *testing.T) {
	c, reg := newTestCollector(t)
	c.UpdateBalance([]model.CheckResult{{
		Project: "openai-main", Provider: "openai", Type: model.TypeBalance, Success: true,
		Credits: model.Ptr(42.5), Threshold: model.Ptr(50.0), NeedAlarm: true,
		Runway: &model.Runway{BurnPerDay: model.Ptr(3.0), RunwayDays: model.Ptr(14.1666)},
	}})

	expected := `
# HELP quotapulse_balance Current balance or credits
# TYPE quotapulse_balance gauge
quotapulse_balance{project="openai-main",provider="openai",type="balance"} 42.5
# HELP quotapulse_burn_rate_per_day Average daily consumption over the burn-rate window
# TYPE quotapulse_burn_rate_per_day gauge
quotapulse_burn_rate_per_day{project="openai-main",provider="openai",type="balance"} 3
# HELP quotapulse_check_status Result of the last balance check (1=success, 0=failed; balance keeps its last good value)
# TYPE quotapulse_check_status gauge
quotapulse_check_status{project="openai-main",provider="openai",type="balance"} 1
# HELP quotapulse_ratio Balance to threshold ratio
# TYPE quotapulse_ratio gauge
quotapulse_ratio{project="openai-main",provider="openai",type="balance"} 0.85
# HELP quotapulse_runway_days Days of runway left at the current burn rate
# TYPE quotapulse_runway_days gauge
quotapulse_runway_days{project="openai-main",provider="openai",type="balance"} 14.1666
# HELP quotapulse_status Balance status (1=ok, 0=alert)
# TYPE quotapulse_status gauge
quotapulse_status{project="openai-main",provider="openai",type="balance"} 0
# HELP quotapulse_threshold Alert threshold
# TYPE quotapulse_threshold gauge
quotapulse_threshold{project="openai-main",provider="openai",type="balance"} 50
`
	compareText(t, reg, expected,
		"quotapulse_balance", "quotapulse_threshold", "quotapulse_ratio", "quotapulse_status",
		"quotapulse_check_status", "quotapulse_burn_rate_per_day", "quotapulse_runway_days")
}

// With a zero threshold the ratio must be 0, never +Inf from division by zero.
func TestRatioWithZeroThreshold(t *testing.T) {
	c, reg := newTestCollector(t)
	c.UpdateBalance([]model.CheckResult{okResult("p1", "glm", model.TypeQuota, 80, 0, false)})

	mustContain(t, dump(t, reg), `quotapulse_ratio{project="p1",provider="glm",type="quota"} 0`)
}

// Without enough history, no runway metrics: a 0 would trip runway_days < 3 alerts as "about to run out".
func TestRunwayMetricsAbsentWithoutHistory(t *testing.T) {
	c, reg := newTestCollector(t)
	c.UpdateBalance([]model.CheckResult{okResult("p1", "openai", model.TypeBalance, 100, 20, false)})

	mustNotContain(t, dump(t, reg), "quotapulse_burn_rate_per_day", "quotapulse_runway_days")
}

// A failed check only drops check_status to 0; balance series keep the last successful values.
func TestFailedCheckKeepsLastBalance(t *testing.T) {
	c, reg := newTestCollector(t)
	c.UpdateBalance([]model.CheckResult{{
		Project: "p1", Provider: "openai", Type: model.TypeBalance, Success: true,
		Credits: model.Ptr(100.0), Threshold: model.Ptr(20.0),
		Runway: &model.Runway{BurnPerDay: model.Ptr(5.0), RunwayDays: model.Ptr(20.0)},
	}})
	c.UpdateBalance([]model.CheckResult{failedResult("p1", "openai", model.TypeBalance)})

	mustContain(t, dump(t, reg),
		`quotapulse_balance{project="p1",provider="openai",type="balance"} 100`,
		`quotapulse_threshold{project="p1",provider="openai",type="balance"} 20`,
		`quotapulse_ratio{project="p1",provider="openai",type="balance"} 5`,
		`quotapulse_status{project="p1",provider="openai",type="balance"} 1`,
		`quotapulse_burn_rate_per_day{project="p1",provider="openai",type="balance"} 5`,
		`quotapulse_runway_days{project="p1",provider="openai",type="balance"} 20`,
		`quotapulse_check_status{project="p1",provider="openai",type="balance"} 0`,
	)

	// after recovery the balance refreshes and check_status returns to 1.
	c.UpdateBalance([]model.CheckResult{okResult("p1", "openai", model.TypeBalance, 88, 20, false)})
	mustContain(t, dump(t, reg),
		`quotapulse_balance{project="p1",provider="openai",type="balance"} 88`,
		`quotapulse_check_status{project="p1",provider="openai",type="balance"} 1`,
	)
}

// Without a type in the failure result, the balance series to keep must still be found by project+provider prefix.
func TestFailedCheckWithoutTypeStillKeepsBalance(t *testing.T) {
	c, reg := newTestCollector(t)
	c.UpdateBalance([]model.CheckResult{okResult("p1", "openai", model.TypeBalance, 100, 20, false)})
	c.UpdateBalance([]model.CheckResult{failedResult("p1", "openai", "")})

	text := dump(t, reg)
	mustContain(t, text,
		`quotapulse_balance{project="p1",provider="openai",type="balance"} 100`,
		`quotapulse_check_status{project="p1",provider="openai",type="unknown"} 0`,
	)
	// check_status follows this round: last round's type="balance" series must be gone.
	mustNotContain(t, text, `quotapulse_check_status{project="p1",provider="openai",type="balance"}`)
}

// After a project rename the old series must vanish or panel count() stays inflated forever.
func TestRenamedProjectDropsOldSeries(t *testing.T) {
	c, reg := newTestCollector(t)
	c.UpdateBalance([]model.CheckResult{{
		Project: "old-name", Provider: "openai", Type: model.TypeBalance, Success: true,
		Credits: model.Ptr(100.0), Threshold: model.Ptr(20.0),
		Runway: &model.Runway{BurnPerDay: model.Ptr(5.0), RunwayDays: model.Ptr(20.0)},
	}})
	c.UpdateBalance([]model.CheckResult{okResult("new-name", "openai", model.TypeBalance, 100, 20, false)})

	text := dump(t, reg)
	mustNotContain(t, text, `project="old-name"`)
	mustContain(t, text, `quotapulse_balance{project="new-name",provider="openai",type="balance"} 100`)

	for name, want := range map[string]int{
		"quotapulse_balance":           1,
		"quotapulse_threshold":         1,
		"quotapulse_ratio":             1,
		"quotapulse_status":            1,
		"quotapulse_check_status":      1,
		"quotapulse_burn_rate_per_day": 0, // 新项目没有历史，这两条压根没写过
		"quotapulse_runway_days":       0,
	} {
		if got := seriesCount(t, reg, name); got != want {
			t.Errorf("%s 剩下 %d 条序列，期望 %d", name, got, want)
		}
	}
}

// Deleting all projects passes an empty slice and clears every balance series.
func TestEmptyBalanceUpdateClearsSeries(t *testing.T) {
	c, reg := newTestCollector(t)
	c.UpdateBalance([]model.CheckResult{
		okResult("p1", "openai", model.TypeBalance, 100, 20, false),
		okResult("p2", "glm", model.TypeQuota, 80, 30, true),
	})
	c.UpdateBalance(nil)

	text := dump(t, reg)
	mustNotContain(t, text, "quotapulse_balance{", "quotapulse_check_status{", "quotapulse_status{")
	// last_check_timestamp is unaffected: this round did check.
	mustContain(t, text, `quotapulse_last_check_timestamp{check_type="balance"}`)
}

func TestSubscriptionStatus(t *testing.T) {
	c, reg := newTestCollector(t)
	c.UpdateSubscriptions([]model.SubscriptionResult{
		{Name: "claude", CycleType: model.CycleMonthly, DaysUntilRenewal: 3, Amount: 20, NeedAlert: true},
		{Name: "github", CycleType: model.CycleYearly, DaysUntilRenewal: 200, Amount: 100},
		// renewed wins over need_alert: paid this cycle means no further reminder
		{Name: "vps", CycleType: model.CycleMonthly, DaysUntilRenewal: 28, Amount: 5, NeedAlert: true, AlreadyRenewed: true},
	})

	expected := `
# HELP quotapulse_subscription_status Subscription status (1=normal, 0=needs_renewal, -1=renewed_in_cycle)
# TYPE quotapulse_subscription_status gauge
quotapulse_subscription_status{cycle_type="monthly",name="claude"} 0
quotapulse_subscription_status{cycle_type="monthly",name="vps"} -1
quotapulse_subscription_status{cycle_type="yearly",name="github"} 1
`
	compareText(t, reg, expected, "quotapulse_subscription_status")

	mustContain(t, dump(t, reg),
		`quotapulse_subscription_days{cycle_type="monthly",name="claude"} 3`,
		`quotapulse_subscription_amount{cycle_type="yearly",name="github"} 100`,
	)
}

func TestSubscriptionRenameAndDisableDropSeries(t *testing.T) {
	c, reg := newTestCollector(t)
	c.UpdateSubscriptions([]model.SubscriptionResult{
		{Name: "old-sub", CycleType: model.CycleMonthly, DaysUntilRenewal: 3, Amount: 20},
	})
	c.UpdateSubscriptions([]model.SubscriptionResult{
		{Name: "new-sub", CycleType: model.CycleMonthly, DaysUntilRenewal: 3, Amount: 20},
	})

	text := dump(t, reg)
	mustNotContain(t, text, `name="old-sub"`)
	mustContain(t, text, `quotapulse_subscription_days{cycle_type="monthly",name="new-sub"} 3`)

	// when the subscription feature is off the caller passes an empty slice and series clear
	c.UpdateSubscriptions(nil)
	mustNotContain(t, dump(t, reg), "quotapulse_subscription_days{", "quotapulse_subscription_status{")
}

func TestEmailScanGaugesAndCounters(t *testing.T) {
	c, reg := newTestCollector(t)
	c.UpdateEmailScan(model.ScanResult{Mailboxes: []model.MailboxResult{
		{Name: "work", TotalEmails: 10, AlertCount: 2, Success: true},
		{Name: "broken", Success: false, Error: model.Ptr("auth failed")},
	}})

	mustContain(t, dump(t, reg),
		`quotapulse_email_mailbox_status{mailbox="work"} 1`,
		`quotapulse_email_mailbox_status{mailbox="broken"} 0`,
		`quotapulse_email_last_scan_emails{mailbox="work"} 10`,
		`quotapulse_email_last_scan_alerts{mailbox="work"} 2`,
	)

	// second round: Gauges reflect this scan, Counters accumulate; the "broken" mailbox was deleted
	c.UpdateEmailScan(model.ScanResult{Mailboxes: []model.MailboxResult{
		{Name: "work", TotalEmails: 5, AlertCount: 1, Success: true},
	}})

	text := dump(t, reg)
	mustContain(t, text,
		`quotapulse_email_last_scan_emails{mailbox="work"} 5`,
		`quotapulse_email_last_scan_alerts{mailbox="work"} 1`,
		`quotapulse_email_scan_total{mailbox="work"} 15`,
		`quotapulse_email_alerts_total{mailbox="work"} 3`,
	)
	mustNotContain(t, text,
		`quotapulse_email_mailbox_status{mailbox="broken"}`,
		`quotapulse_email_last_scan_emails{mailbox="broken"}`,
	)
	// cumulative Counters are not deleted: deleting them would look like a false reset to increase().
	mustContain(t, text, `quotapulse_email_scan_total{mailbox="broken"} 0`)
}

func TestRecordJobRun(t *testing.T) {
	c, reg := newTestCollector(t)
	start := time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)

	c.RecordJobRun("alert_check", true, start, 1500*time.Millisecond)
	c.RecordJobRun("alert_check", false, start.Add(time.Hour), 2*time.Second)

	if got := gaugeValue(t, reg, "quotapulse_job_last_run_timestamp", "alert_check"); got != unixSeconds(start.Add(time.Hour)) {
		t.Errorf("last_run = %v，期望最后一次运行的时刻", got)
	}
	// failures do not refresh last_success: alert rules use time() - last_success to flag stale jobs.
	if got := gaugeValue(t, reg, "quotapulse_job_last_success_timestamp", "alert_check"); got != unixSeconds(start) {
		t.Errorf("last_success = %v，期望停在上次成功的时刻", got)
	}
	if got := gaugeValue(t, reg, "quotapulse_job_last_status", "alert_check"); got != 0 {
		t.Errorf("last_status = %v，期望 0", got)
	}
	if got := gaugeValue(t, reg, "quotapulse_job_last_duration_seconds", "alert_check"); got != 2 {
		t.Errorf("last_duration_seconds = %v，期望 2", got)
	}

	expected := `
# HELP quotapulse_job_runs_total Job runs by result
# TYPE quotapulse_job_runs_total counter
quotapulse_job_runs_total{status="failed",task="alert_check"} 1
quotapulse_job_runs_total{status="success",task="alert_check"} 1
`
	compareText(t, reg, expected, "quotapulse_job_runs_total")

	// the task label must never be job: Prometheus renames colliding labels to exported_job
	mustContain(t, dump(t, reg), `quotapulse_job_last_status{task="alert_check"}`)
	mustNotContain(t, dump(t, reg), `{job="alert_check"}`)
}

func TestRecordJobRunWithZeroStartTime(t *testing.T) {
	c, reg := newTestCollector(t)
	c.RecordJobRun("email_scan", true, time.Time{}, time.Second)

	if got := gaugeValue(t, reg, "quotapulse_job_last_run_timestamp", "email_scan"); got != unixSeconds(testNow) {
		t.Errorf("last_run = %v，期望退回当前时间 %v", got, unixSeconds(testNow))
	}
}

func TestNotificationsByKindAndStatus(t *testing.T) {
	c, reg := newTestCollector(t)
	c.RecordNotification("balance", true)
	c.RecordNotification("balance", true)
	c.RecordNotification("balance", false)
	c.RecordNotification("weekly_report", true)
	c.RecordNotification("mailbox_error", false)

	expected := `
# HELP quotapulse_notifications_total Webhook notifications by kind and result
# TYPE quotapulse_notifications_total counter
quotapulse_notifications_total{kind="balance",status="failed"} 1
quotapulse_notifications_total{kind="balance",status="success"} 2
quotapulse_notifications_total{kind="mailbox_error",status="failed"} 1
quotapulse_notifications_total{kind="weekly_report",status="success"} 1
`
	compareText(t, reg, expected, "quotapulse_notifications_total")
}

func TestLastCheckTimestamp(t *testing.T) {
	c, reg := newTestCollector(t)
	c.UpdateBalance(nil)
	c.UpdateSubscriptions(nil)
	c.UpdateEmailScan(model.ScanResult{})

	ts := unixSeconds(testNow)
	expected := fmt.Sprintf(`
# HELP quotapulse_last_check_timestamp Timestamp of last check
# TYPE quotapulse_last_check_timestamp gauge
quotapulse_last_check_timestamp{check_type="balance"} %v
quotapulse_last_check_timestamp{check_type="email"} %v
quotapulse_last_check_timestamp{check_type="subscription"} %v
`, ts, ts, ts)
	compareText(t, reg, expected, "quotapulse_last_check_timestamp")
}

// Missing label values fall back to unknown / monthly so panels never see unidentifiable series like project="".
func TestMissingLabelsFallBack(t *testing.T) {
	c, reg := newTestCollector(t)
	c.UpdateBalance([]model.CheckResult{{Success: true, Credits: model.Ptr(1.0)}})
	c.UpdateSubscriptions([]model.SubscriptionResult{{DaysUntilRenewal: 1}})
	c.UpdateEmailScan(model.ScanResult{Mailboxes: []model.MailboxResult{{Success: true}}})

	mustContain(t, dump(t, reg),
		`quotapulse_balance{project="unknown",provider="unknown",type="unknown"} 1`,
		`quotapulse_subscription_days{cycle_type="monthly",name="unknown"} 1`,
		`quotapulse_email_mailbox_status{mailbox="unknown"} 1`,
	)
}

// An unreadable balance records 0; pointer semantics only exist in API responses.
func TestNilBalanceCountsAsZero(t *testing.T) {
	c, reg := newTestCollector(t)
	c.UpdateBalance([]model.CheckResult{{Project: "p1", Provider: "openai", Type: model.TypeBalance, Success: true}})

	mustContain(t, dump(t, reg),
		`quotapulse_balance{project="p1",provider="openai",type="balance"} 0`,
		`quotapulse_threshold{project="p1",provider="openai",type="balance"} 0`,
		`quotapulse_ratio{project="p1",provider="openai",type="balance"} 0`,
	)
}

func TestHandlerServesMetrics(t *testing.T) {
	c, _ := newTestCollector(t)
	exerciseAll(c)

	rec := httptest.NewRecorder()
	c.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "quotapulse_balance{", "quotapulse_notifications_total{")
}

// The scheduler thread and HTTP handlers update concurrently; the series registry is a plain map — this case (with -race) watches it.
func TestConcurrentUpdates(t *testing.T) {
	c, reg := newTestCollector(t)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.UpdateBalance([]model.CheckResult{okResult(fmt.Sprintf("p%d", i), "openai", model.TypeBalance, float64(i), 10, false)})
			c.UpdateSubscriptions([]model.SubscriptionResult{{Name: fmt.Sprintf("s%d", i), CycleType: model.CycleMonthly}})
			c.UpdateEmailScan(model.ScanResult{Mailboxes: []model.MailboxResult{{Name: fmt.Sprintf("m%d", i), Success: true}}})
			c.RecordJobRun("alert_check", true, testNow, time.Second)
			c.RecordNotification("balance", true)
		}(i)
	}
	wg.Wait()

	// a new project has no history; these two were never written at all
	if got := seriesCount(t, reg, "quotapulse_balance"); got != 1 {
		t.Errorf("余额剩下 %d 条序列，期望 1", got)
	}
	mustContain(t, dump(t, reg), `quotapulse_job_runs_total{status="success",task="alert_check"} 8`)
}

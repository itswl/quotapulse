package subscription

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Implementation note.
// Implementation note.
type baselineCase struct {
	Cycle          string  `json:"cycle"`
	RenewalDay     int     `json:"renewal_day"`
	Today          string  `json:"today"`
	LastRenewed    *string `json:"last_renewed"`
	Days           int     `json:"days"`
	Next           string  `json:"next"`
	AlreadyRenewed bool    `json:"already_renewed"`
}

// Implementation note.
// Implementation note.
func TestMatchesBaseline(t *testing.T) {
	raw, err := os.ReadFile("testdata/baseline_cases.json")
	if err != nil {
		t.Fatalf("读取基准数据失败: %v", err)
	}
	var cases []baselineCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("解析基准数据失败: %v", err)
	}
	if len(cases) < 300 {
		t.Fatalf("基准数据只有 %d 条，太少，怀疑生成有问题", len(cases))
	}

	for _, c := range cases {
		today := mustDate(t, c.Today)
		var lastRenewed *time.Time
		if c.LastRenewed != nil {
			parsed := mustDate(t, *c.LastRenewed)
			lastRenewed = &parsed
		}

		days, next := NextRenewal(c.Cycle, c.RenewalDay, today, lastRenewed)
		if days != c.Days || next.Format("2006-01-02") != c.Next {
			t.Errorf("%s/%d 在 %s（上次续费 %v）: 期望 %d 天后的 %s，实际 %d 天后的 %s",
				c.Cycle, c.RenewalDay, c.Today, derefString(c.LastRenewed),
				c.Days, c.Next, days, next.Format("2006-01-02"))
			continue
		}

		already := false
		if lastRenewed != nil {
			start := cycleStart(c.Cycle, c.RenewalDay, startOfDay(today), next)
			already = !lastRenewed.Before(start)
		}
		if already != c.AlreadyRenewed {
			t.Errorf("%s/%d 在 %s（上次续费 %v）: 已续费判断期望 %v，实际 %v",
				c.Cycle, c.RenewalDay, c.Today, derefString(c.LastRenewed), c.AlreadyRenewed, already)
		}
	}
}

func TestSplitMMDD(t *testing.T) {
	tests := []struct {
		input       int
		month, day  int
		ok          bool
		explanation string
	}{
		{315, 3, 15, true, "3 月 15 日"},
		{1231, 12, 31, true, "12 月 31 日"},
		{229, 2, 29, true, "2 月 29 日"},
		{15, 0, 0, false, "小于 31 属于旧配置的只写了日"},
		{31, 0, 0, false, "边界：31 仍算旧配置"},
		{1350, 0, 0, false, "13 月不存在"},
		{300, 0, 0, false, "0 日不存在"},
	}
	for _, tt := range tests {
		month, day, ok := SplitMMDD(tt.input)
		if ok != tt.ok || (ok && (month != tt.month || day != tt.day)) {
			t.Errorf("SplitMMDD(%d) = %d,%d,%v；期望 %d,%d,%v（%s）",
				tt.input, month, day, ok, tt.month, tt.day, tt.ok, tt.explanation)
		}
	}
}

func TestCoerceRenewalDay(t *testing.T) {
	tests := []struct {
		input, cycle string
		want         int
		ok           bool
	}{
		{"03-15", "yearly", 315, true},
		{"3-15", "yearly", 315, true},
		{"3月15日", "yearly", 315, true},
		{"03/15", "yearly", 315, true},
		{"03-15", "monthly", 15, true},
		{"15", "monthly", 15, true},
		{" 7 ", "weekly", 7, true},
		{"每月十五", "monthly", 0, false},
		{"", "monthly", 0, false},
	}
	for _, tt := range tests {
		got, ok := CoerceRenewalDay(tt.input, tt.cycle)
		if got != tt.want || ok != tt.ok {
			t.Errorf("CoerceRenewalDay(%q, %q) = %d,%v；期望 %d,%v", tt.input, tt.cycle, got, ok, tt.want, tt.ok)
		}
	}
}

// Implementation note.
func TestMonthEndClamping(t *testing.T) {
	// Implementation note.
	_, next := NextRenewal("monthly", 31, mustDate(t, "2026-02-01"), nil)
	if got := next.Format("2006-01-02"); got != "2026-02-28" {
		t.Errorf("31 号的月付在 2 月应回退到 2026-02-28，实际 %s", got)
	}
	// Implementation note.
	last := mustDate(t, "2024-02-29")
	_, next = NextRenewal("yearly", 229, mustDate(t, "2026-01-01"), &last)
	if got := next.Format("2006-01-02"); got != "2026-02-28" {
		t.Errorf("2 月 29 日的年付在平年应回退到 2026-02-28，实际 %s", got)
	}
}

// Implementation note.
func TestRenewalDayIsNotOffByOne(t *testing.T) {
	today := time.Date(2026, 3, 15, 14, 30, 0, 0, time.Local) // 故意带上时分秒
	days, next := NextRenewal("monthly", 15, today, nil)
	if days != 0 || next.Format("2006-01-02") != "2026-03-15" {
		t.Errorf("续费当天应为 0 天（2026-03-15），实际 %d 天（%s）", days, next.Format("2006-01-02"))
	}
}

func TestLunarYearlyRenewal(t *testing.T) {
	day := 101 // Lunar New Year's day.
	tests := []struct {
		name  string
		today string
		days  int
		next  string
	}{
		{"before lunar new year", "2024-01-01", 40, "2024-02-10"},
		{"on lunar new year", "2024-02-10", 0, "2024-02-10"},
		{"after lunar new year", "2024-02-11", 353, "2025-01-29"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			days, next := NextRenewal("lunar_yearly", day, mustDate(t, tt.today), nil)
			if days != tt.days || next.Format("2006-01-02") != tt.next {
				t.Fatalf("农历年付在 %s: 期望 %d 天后的 %s，实际 %d 天后的 %s", tt.today,
					tt.days, tt.next, days, next.Format("2006-01-02"))
			}
		})
	}
}

func TestLunarRenewalDayInput(t *testing.T) {
	if got, ok := CoerceRenewalDay("五月初三", "lunar_yearly"); ok || got != 0 {
		t.Fatalf("不支持的中文大写日期不应被猜测: %d, %v", got, ok)
	}
	if got, ok := CoerceRenewalDay("05-03", "lunar_yearly"); !ok || got != 503 {
		t.Fatalf("农历月日解析错误: %d, %v", got, ok)
	}
}

func mustDate(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		t.Fatalf("测试数据里的日期解析失败 %q: %v", value, err)
	}
	return parsed
}

func derefString(p *string) string {
	if p == nil {
		return "无"
	}
	return *p
}

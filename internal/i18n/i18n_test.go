package i18n

import (
	"testing"
	"time"

	"quotapulse/internal/recur"
)

func TestNumber(t *testing.T) {
	cases := []struct {
		v        float64
		decimals int
		exact    bool
		want     string
	}{
		{547.67, 2, true, "547.67"},
		{1234567.891, 2, true, "1,234,567.89"},
		{1000, 2, true, "1,000.00"},
		{1000, 2, false, "1,000"},
		{42.4, 1, false, "42.4"},
		{99, 1, false, "99"},
		{0.05, 1, false, "0.1"},
		{-1234.5, 2, true, "-1,234.50"},
		{-0.001, 2, true, "0.00"}, // never "-0.00"
		{0, 2, false, "0"},
		{999.999, 2, true, "1,000.00"},
	}
	for _, c := range cases {
		if got := Number(c.v, c.decimals, c.exact); got != c.want {
			t.Errorf("Number(%v, %d, %v) = %q, want %q", c.v, c.decimals, c.exact, got, c.want)
		}
	}
}

func TestAmount(t *testing.T) {
	cases := map[string]string{
		Amount(547.67, "CNY"):   "547.67 CNY",
		Amount(8, "USD"):        "8.00 USD",
		Amount(99, "%"):         "99%",
		Amount(42.44, "%"):      "42.4%",
		Amount(923, "calls"):    "923 calls",
		Amount(263419, ""):      "263,419",
		Amount(12.5, "credits"): "12.5 credits",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestSpanAndDays(t *testing.T) {
	en, zh := Of("en"), Of("zh")
	spans := []struct {
		d      time.Duration
		en, zh string
	}{
		{20 * time.Second, "under a minute", "不到 1 分钟"},
		{45 * time.Minute, "45 min", "45 分钟"},
		{80 * time.Minute, "1 h 20 min", "1 小时 20 分钟"},
		{2 * time.Hour, "2 h", "2 小时"},
		{30 * time.Hour, "30 h", "30 小时"},
		{72 * time.Hour, "3 days", "3 天"},
		{-80 * time.Minute, "1 h 20 min", "1 小时 20 分钟"},
	}
	for _, c := range spans {
		if got := en.Span(c.d); got != c.en {
			t.Errorf("en.Span(%v) = %q, want %q", c.d, got, c.en)
		}
		if got := zh.Span(c.d); got != c.zh {
			t.Errorf("zh.Span(%v) = %q, want %q", c.d, got, c.zh)
		}
	}
	days := []struct {
		n      float64
		en, zh string
	}{
		{0.33, "0.3 days", "0.3 天"},
		{1, "1 day", "1 天"},
		{5.25, "5.2 days", "5.2 天"},
		{41.6, "42 days", "42 天"},
		{698.8, "over a year", "超过 1 年"},
		{900, "over 2 years", "超过 2 年"},
	}
	for _, c := range days {
		if got := en.Days(c.n); got != c.en {
			t.Errorf("en.Days(%v) = %q, want %q", c.n, got, c.en)
		}
		if got := zh.Days(c.n); got != c.zh {
			t.Errorf("zh.Days(%v) = %q, want %q", c.n, got, c.zh)
		}
	}
}

func TestUntil(t *testing.T) {
	zh, en := Of("zh"), Of("anything else")
	cases := []struct {
		n      int
		zh, en string
	}{
		{0, "今天", "today"}, {1, "明天", "tomorrow"}, {2, "后天", "in 2 days"},
		{38, "38 天后", "in 38 days"}, {-1, "昨天", "yesterday"}, {-3, "3 天前", "3 days ago"},
	}
	for _, c := range cases {
		if zh.Until(c.n) != c.zh || en.Until(c.n) != c.en {
			t.Errorf("Until(%d) = %q / %q", c.n, zh.Until(c.n), en.Until(c.n))
		}
	}
}

func TestDates(t *testing.T) {
	day := recur.NewDate(2026, 12, 24)
	if got := Of("zh").Date(day, 2026); got != "12月24日 周四" {
		t.Errorf("zh = %q", got)
	}
	if got := Of("en").Date(day, 2026); got != "Thu, Dec 24" {
		t.Errorf("en = %q", got)
	}
	next := recur.NewDate(2027, 2, 6)
	if got := Of("zh").Date(next, 2026); got != "2027年2月6日 周六" {
		t.Errorf("zh next year = %q", got)
	}
	if got := Of("en").Date(next, 2026); got != "Sat, Feb 6, 2027" {
		t.Errorf("en next year = %q", got)
	}

	shanghai, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, shanghai)
	if got := Of("zh").Clock(now.Add(4*time.Hour+20*time.Minute), now); got != "14:20" {
		t.Errorf("same day = %q", got)
	}
	if got := Of("zh").Clock(now.Add(5*24*time.Hour), now); got != "11月21日 10:00" {
		t.Errorf("another day = %q", got)
	}
	if got := Of("en").Clock(now.Add(5*24*time.Hour), now); got != "Nov 21 10:00" {
		t.Errorf("another day = %q", got)
	}
}

func TestOrdinal(t *testing.T) {
	en := Of("en")
	for n, want := range map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 30: "30th", 112: "112th"} {
		if got := en.Ordinal(n); got != want {
			t.Errorf("Ordinal(%d) = %q, want %q", n, got, want)
		}
	}
	if got := Of("zh").Ordinal(30); got != "第 30 个" {
		t.Errorf("zh = %q", got)
	}
}

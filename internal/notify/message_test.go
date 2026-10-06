package notify

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// Implementation note.
// Implementation note.

func TestMessageTemplates(t *testing.T) {
	owner := "核心业务"
	service := "OpenAI"
	amount := 20.0

	tests := []struct {
		name      string
		msg       Message
		wantTitle string
		wantKind  string
		wantLines []string
	}{
		{
			name:      "Balance alert",
			msg:       BalanceAlert("TestProject", &owner, "OpenRouter", 1234.5, 10000.0),
			wantTitle: "Balance alert",
			wantKind:  KindBalance,
			wantLines: []string{
				"API call: TestProject",
				"Owner project: 核心业务",
				"Provider: OpenRouter",
				"Current balance: 1,234.50",
				"Alert threshold: 10,000.00",
				"Status: ⚠️ balance insufficient",
			},
		},
		{
			name:      "Balance check failed",
			msg:       CheckFailedAlert("aliyun-ops", &owner, "Alibaba Cloud", "HTTP 401: AccessKey disabled"),
			wantTitle: "Balance check failed",
			wantKind:  KindCheckFailed,
			wantLines: []string{
				"API call: aliyun-ops",
				"Owner project: 核心业务",
				"Provider: Alibaba Cloud",
				"Error: HTTP 401: AccessKey disabled",
				"Status: ❌ balance unavailable; this account is not monitored until a check succeeds",
			},
		},
		{
			name:      "Balance alert_没有所属项目就不出这一行",
			msg:       BalanceAlert("TestProject", nil, "OpenRouter", 5.0, 10.0),
			wantTitle: "Balance alert",
			wantKind:  KindBalance,
			wantLines: []string{
				"API call: TestProject",
				"Provider: OpenRouter",
				"Current balance: 5.00",
				"Alert threshold: 10.00",
				"Status: ⚠️ balance insufficient",
			},
		},
		{
			name:      "订阅提醒_月付",
			msg:       SubscriptionAlert("Netflix", &owner, "monthly", 15, 3, 15.99),
			wantTitle: "Subscription reminder",
			wantKind:  KindSubscription,
			wantLines: []string{
				"Subscription: Netflix",
				"Owner project: 核心业务",
				"Renewal cycle: Monthly on day 15",
				"Time until renewal: In 3 days",
				"Renewal amount: 15.99",
			},
		},
		{
			name:      "订阅提醒_年付Today到期",
			msg:       SubscriptionAlert("ChatGPT", nil, "yearly", 315, 0, 20.0),
			wantTitle: "Subscription reminder",
			wantKind:  KindSubscription,
			wantLines: []string{
				"Subscription: ChatGPT",
				"Renewal cycle: Annually on 03-15",
				"Time until renewal: Today",
				"Renewal amount: 20.0",
			},
		},
		{
			name: "邮件命中",
			msg: EmailAlert("财务邮箱", "Your receipt from OpenAI", "billing@openai.com",
				"2026-09-16 10:00:00", []string{"invoice", "续费"}, &service, &amount),
			wantTitle: "📧 Email alert: Your receipt from OpenAI",
			wantKind:  KindEmail,
			wantLines: []string{
				"**Mailbox**: 财务邮箱",
				"**Sender**: billing@openai.com",
				"**Date**: 2026-09-16 10:00:00",
				"**Service**: OpenAI",
				"**Amount**: 20.0",
				"**Keywords**: invoice, 续费",
			},
		},
		{
			// Implementation note.
			name: "邮件命中_服务未识别且无金额",
			msg: EmailAlert("", "账单", "noreply@x.com", "2026-09-16",
				[]string{"账单"}, nil, nil),
			wantTitle: "📧 Email alert: 账单",
			wantKind:  KindEmail,
			wantLines: []string{
				"**Mailbox**: Unknown",
				"**Sender**: noreply@x.com",
				"**Date**: 2026-09-16",
				"**Service**: None",
				"**Keywords**: 账单",
			},
		},
		{
			name: "邮箱连接失败",
			msg: mailboxError("财务邮箱", "[Errno 61] Connection refused",
				time.Date(2026, 9, 16, 23, 45, 1, 0, time.Local)),
			wantTitle: "❌ Mailbox connection failed",
			wantKind:  KindMailboxError,
			wantLines: []string{
				"**Mailbox**: 财务邮箱",
				"**Error message**: [Errno 61] Connection refused",
				"**Time**: 2026-09-16 23:45:01",
			},
		},
		{
			name:      "富文本告警原样透传",
			msg:       Custom("balance周报", []string{"**统计区间**: 09-09 ~ 09-16"}, KindWeeklyReport),
			wantTitle: "balance周报",
			wantKind:  KindWeeklyReport,
			wantLines: []string{"**统计区间**: 09-09 ~ 09-16"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.msg.Title != tt.wantTitle {
				t.Errorf("Title = %q; 期望 %q", tt.msg.Title, tt.wantTitle)
			}
			if tt.msg.Kind != tt.wantKind {
				t.Errorf("Kind = %q; 期望 %q", tt.msg.Kind, tt.wantKind)
			}
			if !reflect.DeepEqual(tt.msg.Lines, tt.wantLines) {
				t.Errorf("正文不一致\n期望: %q\n实际: %q", tt.wantLines, tt.msg.Lines)
			}
		})
	}
}

// Implementation note.
func TestEmailAlertSkipsZeroAmount(t *testing.T) {
	zero := 0.0
	msg := EmailAlert("m", "s", "from", "date", []string{"k"}, nil, &zero)
	for _, line := range msg.Lines {
		if line == "**Amount**: 0.0" {
			t.Fatalf("金额为 0 不该出现在正文里: %q", msg.Lines)
		}
	}
}

// Implementation note.
func TestMailboxErrorUsesNowAndDropsHost(t *testing.T) {
	before := time.Now().Truncate(time.Second)
	msg := MailboxError("财务邮箱", "imap.exmail.qq.com", "[Errno 61] Connection refused")
	after := time.Now()

	if len(msg.Lines) != 3 {
		t.Fatalf("正文应当是三行; 实际 %q", msg.Lines)
	}
	for _, line := range msg.Lines {
		if strings.Contains(line, "imap.exmail.qq.com") {
			t.Errorf("host 不该出现在正文里: %q", line)
		}
	}

	at, err := time.ParseInLocation("2006-01-02 15:04:05",
		strings.TrimPrefix(msg.Lines[2], "**Time**: "), time.Local)
	if err != nil {
		t.Fatalf("时间行格式不对: %q", msg.Lines[2])
	}
	if at.Before(before) || at.After(after) {
		t.Errorf("时间 %v 不在 [%v, %v] 之间", at, before, after)
	}
}

func TestFormatSubscriptionCycle(t *testing.T) {
	tests := []struct {
		cycleType  string
		renewalDay int
		want       string
	}{
		{"weekly", 1, "Weekly Monday"},
		{"weekly", 7, "Weekly Sunday"},
		{"weekly", 9, "Weekly day 9"}, // 越界不猜; 原样说出来
		{"monthly", 15, "Monthly on day 15"},
		{"monthly", 31, "Monthly on day 31"},
		{"yearly", 315, "Annually on 03-15"},
		{"yearly", 1231, "Annually on 12-31"},
		{"yearly", 1, "Fixed annual date"},    // 旧配置只写了日
		{"yearly", 1350, "Fixed annual date"}, // 13 月不是合法 MMDD
		{"bogus", 15, "Unknown cycle"},        // The caller reports this configuration error.
		{"", 15, "Unknown cycle"},
	}

	for _, tt := range tests {
		if got := FormatSubscriptionCycle(tt.cycleType, tt.renewalDay); got != tt.want {
			t.Errorf("FormatSubscriptionCycle(%q, %d) = %q; 期望 %q",
				tt.cycleType, tt.renewalDay, got, tt.want)
		}
	}
}

func TestFormatDays(t *testing.T) {
	tests := []struct {
		days int
		want string
	}{
		{0, "Today"},
		{1, "Tomorrow"},
		{2, "In 2 days"},
		{30, "In 30 days"},
		{-1, "In -1 days"}, // 已经过期的订阅就这么写; 不做特殊措辞
	}

	for _, tt := range tests {
		if got := formatDays(tt.days); got != tt.want {
			t.Errorf("formatDays(%d) = %q; 期望 %q", tt.days, got, tt.want)
		}
	}
}

// Implementation note.
func TestFormatAmount(t *testing.T) {
	tests := []struct {
		value float64
		want  string
	}{
		{0, "0.00"},
		{5, "5.00"},
		{999.994, "999.99"},
		{1234.5, "1,234.50"},
		{10000, "10,000.00"},
		{1000000, "1,000,000.00"},
		{1234567.891, "1,234,567.89"},
		{2.675, "2.67"}, // 二进制里存的比 2.675 略小; 进不上去
		{-1234.5, "-1,234.50"},
		{1e-5, "0.00"},
	}

	for _, tt := range tests {
		if got := formatAmount(tt.value); got != tt.want {
			t.Errorf("formatAmount(%v) = %q; 期望 %q", tt.value, got, tt.want)
		}
	}
}

// Implementation note.
func TestFormatFloat(t *testing.T) {
	tests := []struct {
		value float64
		want  string
	}{
		{0, "0.0"},
		{20, "20.0"},
		{15.99, "15.99"},
		{1234.5, "1234.5"},
		{1234567, "1234567.0"}, // 换成 %g 这里就成了 1.234567e+06
		{0.30000000000000004, "0.30000000000000004"}, // 精度尾巴要原样留着; 别被四舍五入抹平
		{1e16, "1e+16"},
		{1e-5, "1e-05"},
		{-1234.5, "-1234.5"},
	}

	for _, tt := range tests {
		if got := formatFloat(tt.value); got != tt.want {
			t.Errorf("formatFloat(%v) = %q; 期望 %q", tt.value, got, tt.want)
		}
	}
}

func TestISOLocal(t *testing.T) {
	withMicros := time.Date(2026, 9, 16, 23, 50, 26, 26504000, time.Local)
	if got, want := isoLocal(withMicros), "2026-09-16T23:50:26.026504"; got != want {
		t.Errorf("isoLocal = %q; 期望 %q", got, want)
	}
	// Implementation note.
	whole := time.Date(2026, 9, 16, 23, 50, 26, 0, time.Local)
	if got, want := isoLocal(whole), "2026-09-16T23:50:26"; got != want {
		t.Errorf("isoLocal = %q; 期望 %q", got, want)
	}
}

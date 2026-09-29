package mailscan

import (
	"slices"
	"testing"
)

// The default list is product behavior: a missing word loses a class of alerts, an extra
// word adds a batch of false positives. A count mismatch means the list changed; think
// through which alerts that adds or removes before updating the number.
func TestDefaultAlertKeywordsIsUnchanged(t *testing.T) {
	if got, want := len(DefaultAlertKeywords), 50; got != want {
		t.Fatalf("默认关键词数量 = %d, 期望 %d", got, want)
	}
	for _, kw := range []string{"欠费", "余额不足", "停机", "overdue", "payment failed"} {
		if !slices.Contains(DefaultAlertKeywords, kw) {
			t.Errorf("默认关键词表里少了 %q", kw)
		}
	}
}

func TestMatchKeywords(t *testing.T) {
	m := newMatcher(DefaultAlertKeywords)

	tests := []struct {
		name    string
		subject string
		body    string
		want    []string
	}{
		{"中文关键词在主题里", "您的账户余额不足", "", []string{"余额不足"}},
		{"中文关键词在正文里", "通知", "您的服务已欠费，请及时充值", []string{"欠费"}},
		{"英文关键词", "Payment Overdue Notice", "", []string{"payment overdue"}},
		{"英文关键词在正文里", "", "Your account has a low balance", []string{"low balance"}},
		{"expired", "Your subscription has expired", "", []string{"expired"}},
		{"大小写不敏感", "PAYMENT DUE", "", []string{"payment due"}},
		{"多个关键词按词表顺序返回", "余额不足告警", "您的账户已欠费，请及时续费",
			[]string{"欠费", "余额不足", "请及时续费"}},
		{"续费提醒", "续费提醒", "", []string{"续费提醒"}},
		{"suspended", "", "Your account has been suspended", []string{"suspended"}},
		{"跨主题和正文查找", "服务通知", "余额预警：当前余额低于阈值", []string{"余额预警", "余额低于"}},
		// How providers phrase the same warnings in a sentence.
		{"余额低于阈值的句子", "Your Volcengine account balance is below 50 CNY", "", []string{"balance is below"}},
		{"余额偏低", "Your credit balance is low", "", []string{"balance is low"}},
		{"额度快用完", "Your OpenRouter credits are running low", "", []string{"credits are running low"}},
		{"余额不够付款", "", "Your account balance is insufficient to renew", []string{"balance is insufficient"}},
		{"带连字符的充值", "Please top-up your account", "", []string{"top-up"}},
		{"中文余额低于", "您的账户余额低于预警值", "", []string{"余额低于"}},
		// A plain monthly invoice needs no action, so it is left to the extra keywords.
		{"普通账单不告警", "Invoice #4471-2 for September is ready", "", nil},
		{"没有关键词", "周报通知", "本周工作总结", nil},
		{"空邮件", "", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.match(tt.subject, tt.body)
			if !slices.Equal(got, tt.want) {
				t.Errorf("match(%q, %q) = %v, 期望 %v", tt.subject, tt.body, got, tt.want)
			}
		})
	}
}

// Implementation note.
func TestMatchKeywordsTakesLongestPhrase(t *testing.T) {
	m := newMatcher(DefaultAlertKeywords)

	got := m.match("Payment Overdue Notice", "")
	if slices.Contains(got, "overdue") {
		t.Errorf("match = %v, 不该把 %q 再单独算一次", got, "overdue")
	}

	got = m.match("", "Your account suspended yesterday")
	if !slices.Equal(got, []string{"account suspended"}) {
		t.Errorf("match = %v, 期望只命中 %q", got, "account suspended")
	}
}

func TestMatchKeywordsCustomList(t *testing.T) {
	m := newMatcher([]string{"Renew", "配额不足"})
	if got, want := m.match("请尽快 renew", "配额不足"), []string{"Renew", "配额不足"}; !slices.Equal(got, want) {
		t.Errorf("match = %v, 期望 %v（保持词表里的原始大小写与顺序）", got, want)
	}
	if got := m.match("余额不足", "欠费"); got != nil {
		t.Errorf("match = %v, 自定义词表应当整体替换默认词表", got)
	}
}

// Implementation note.
func TestMatchKeywordsIgnoresEmptyWords(t *testing.T) {
	m := newMatcher([]string{"", "欠费"})
	if got, want := m.match("已欠费", ""), []string{"欠费"}; !slices.Equal(got, want) {
		t.Errorf("match = %v, 期望 %v", got, want)
	}
	if got := newMatcher([]string{"", ""}).match("欠费", "任何内容"); got != nil {
		t.Errorf("match = %v, 词表全空时不该有命中", got)
	}
}

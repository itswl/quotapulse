package mailscan

import (
	"regexp"
	"strings"
)

// DefaultAlertKeywords is the default alert keyword list. EMAIL_ALERT_KEYWORDS replaces
// it and EMAIL_EXTRA_ALERT_KEYWORDS appends to it.
//
// The order matters: matches are reported in this order, and where two keywords match at
// the same place in the text, the earlier one wins. The list targets emails that need
// action (low or insufficient balance, overdue or unpaid bills, expiry, renewal,
// suspension); informational mail such as every monthly invoice is left to the extras.
var DefaultAlertKeywords = []string{
	// Chinese
	"欠费", "余额不足", "余额预警", "余额告警",
	"即将到期", "已到期", "续费提醒", "续费通知",
	"账单逾期", "缴费通知", "请及时续费", "停机",
	"暂停服务", "服务即将暂停", "充值提醒",
	"余额低于", "额度不足", "即将耗尽",
	// English
	"overdue", "past due", "payment due", "payment overdue",
	"low balance", "insufficient balance", "balance alert",
	"expiring soon", "expired", "expiration notice",
	"renewal reminder", "renewal notice", "renew now",
	"payment reminder", "payment required", "bill overdue",
	"service suspension", "service suspended", "suspended",
	"recharge reminder", "top up", "account suspended",
	"unpaid invoice", "outstanding balance", "payment failed",
	// The same warnings as providers usually phrase them in a sentence.
	"balance is low", "balance is below", "balance is insufficient",
	"balance is running low", "credits are running low", "insufficient funds", "top-up",
}

// Implementation note.
//
// Implementation note.
// Implementation note.
// Implementation note.
type matcher struct {
	keywords []string
	re       *regexp.Regexp
}

func newMatcher(keywords []string) *matcher {
	parts := make([]string, 0, len(keywords))
	for _, kw := range keywords {
		if kw == "" {
			// Implementation note.
			continue
		}
		parts = append(parts, regexp.QuoteMeta(strings.ToLower(kw)))
	}
	if len(parts) == 0 {
		return &matcher{keywords: keywords}
	}
	return &matcher{
		keywords: keywords,
		re:       regexp.MustCompile("(?i)" + strings.Join(parts, "|")),
	}
}

// Implementation note.
func (m *matcher) match(subject, body string) []string {
	if m.re == nil {
		return nil
	}
	found := m.re.FindAllString(subject+"\n"+body, -1)
	if len(found) == 0 {
		return nil
	}

	hit := make(map[string]bool, len(found))
	for _, f := range found {
		hit[strings.ToLower(f)] = true
	}
	matched := make([]string, 0, len(hit))
	for _, kw := range m.keywords {
		if hit[strings.ToLower(kw)] {
			matched = append(matched, kw)
		}
	}
	return matched
}

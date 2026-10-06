package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Implementation note.
// Implementation note.
// Implementation note.

// Implementation note.
var pinnedNow = time.Date(2026, 9, 16, 23, 50, 26, 26504000, time.Local)

type payloadCase struct {
	name string
	msg  Message
	want map[string]string // webhook 类型 -> 期望报文
}

func payloadCases() []payloadCase {
	owner := "核心业务"
	rich := []string{
		"**账户**: TestProject",
		"**Owner project**: 核心业务",
		"**Provider**: OpenRouter",
		"**Current balance**: 1,234.50",
		"**日均消耗**: 120.00（最近 7 天）",
	}

	return []payloadCase{
		{
			name: "Balance alert_带Owner project与货币符号",
			msg:  balanceAlert("TestProject", &owner, "OpenRouter", "balance", 1234.5, 10000.0, "¥"),
			want: map[string]string{
				TypeFeishu:   `{"msg_type":"text","content":{"text":"[Balance alert]\n\nAPI call: TestProject\nOwner project: 核心业务\nProvider: OpenRouter\nCurrent balance: ¥1,234.50\nAlert threshold: ¥10,000.00\nStatus: ⚠️ balance insufficient\nSource: credit-monitor"}}`,
				TypeDingTalk: `{"msgtype":"markdown","markdown":{"title":"Balance alert","text":"## Balance alert\n\n- **API call**: TestProject\n- **Owner project**: 核心业务\n- **Provider**: OpenRouter\n- **Current balance**: ¥1,234.50\n- **Alert threshold**: ¥10,000.00\n- **Status**: ⚠️ balance insufficient"}}`,
				TypeWeCom:    `{"msgtype":"text","text":{"content":"[Balance alert]\nAPI call: TestProject\nOwner project: 核心业务\nProvider: OpenRouter\nCurrent balance: ¥1,234.50\nAlert threshold: ¥10,000.00\nStatus: ⚠️ balance insufficient"}}`,
				TypeCustom:   `{"Type":"AlarmNotification","RuleName":"TestProject balance alert","Level":"critical","Resources":[{"ProjectName":"TestProject","OwnerProject":"核心业务","Provider":"OpenRouter","BalanceType":"balance","CurrentValue":1234.5,"Threshold":10000.0,"Unit":"¥","Message":"Project [TestProject] has insufficient balance; current: ¥1,234.50, threshold: ¥10,000.00"}]}`,
			},
		},
		{
			name: "Balance alert_无Owner project且balance类型是credits",
			msg:  balanceAlert("P", nil, "OpenRouter", "credits", 5.0, 10.0, ""),
			want: map[string]string{
				TypeFeishu:   `{"msg_type":"text","content":{"text":"[Balance alert]\n\nAPI call: P\nProvider: OpenRouter\nCurrent credits: 5.00\nAlert threshold: 10.00\nStatus: ⚠️ credits insufficient\nSource: credit-monitor"}}`,
				TypeDingTalk: `{"msgtype":"markdown","markdown":{"title":"Balance alert","text":"## Balance alert\n\n- **API call**: P\n- **Provider**: OpenRouter\n- **Current credits**: 5.00\n- **Alert threshold**: 10.00\n- **Status**: ⚠️ credits insufficient"}}`,
				TypeWeCom:    `{"msgtype":"text","text":{"content":"[Balance alert]\nAPI call: P\nProvider: OpenRouter\nCurrent credits: 5.00\nAlert threshold: 10.00\nStatus: ⚠️ credits insufficient"}}`,
				TypeCustom:   `{"Type":"AlarmNotification","RuleName":"P balance alert","Level":"critical","Resources":[{"ProjectName":"P","OwnerProject":null,"Provider":"OpenRouter","BalanceType":"credits","CurrentValue":5.0,"Threshold":10.0,"Unit":"","Message":"Project [P] has insufficient credits; current: 5.00, threshold: 10.00"}]}`,
			},
		},
		{
			name: "Subscription提醒_月付还有三天",
			msg:  SubscriptionAlert("Netflix", &owner, "monthly", 15, 3, 15.99),
			want: map[string]string{
				TypeFeishu:   `{"msg_type":"text","content":{"text":"[Subscription reminder]\n\nSubscription: Netflix\nOwner project: 核心业务\nRenewal cycle: Monthly on day 15\nTime until renewal: In 3 days\nRenewal amount: 15.99\nSource: credit-monitor"}}`,
				TypeDingTalk: `{"msgtype":"markdown","markdown":{"title":"Subscription reminder","text":"## Subscription reminder\n\n- **Subscription**: Netflix\n- **Owner project**: 核心业务\n- **Renewal cycle**: Monthly on day 15\n- **Time until renewal**: In 3 days\n- **Renewal amount**: 15.99"}}`,
				TypeWeCom:    `{"msgtype":"text","text":{"content":"[Subscription reminder]\nSubscription: Netflix\nOwner project: 核心业务\nRenewal cycle: Monthly on day 15\nTime until renewal: In 3 days\nRenewal amount: 15.99"}}`,
				TypeCustom:   `{"Type":"SubscriptionReminder","RuleName":"Netflix subscription reminder","Level":"warning","Resources":[{"SubscriptionName":"Netflix","OwnerProject":"核心业务","RenewalDay":15,"CycleType":"monthly","DaysUntilRenewal":3,"Amount":15.99,"Message":"Subscription reminder for [Netflix]: due in 3 days (Monthly on day 15), amount: 15.99"}]}`,
			},
		},
		{
			name: "Subscription提醒_年付Today到期",
			msg:  SubscriptionAlert("ChatGPT", nil, "yearly", 315, 0, 20.0),
			want: map[string]string{
				TypeFeishu:   `{"msg_type":"text","content":{"text":"[Subscription reminder]\n\nSubscription: ChatGPT\nRenewal cycle: Annually on 03-15\nTime until renewal: Today\nRenewal amount: 20.0\nSource: credit-monitor"}}`,
				TypeDingTalk: `{"msgtype":"markdown","markdown":{"title":"Subscription reminder","text":"## Subscription reminder\n\n- **Subscription**: ChatGPT\n- **Renewal cycle**: Annually on 03-15\n- **Time until renewal**: Today\n- **Renewal amount**: 20.0"}}`,
				TypeWeCom:    `{"msgtype":"text","text":{"content":"[Subscription reminder]\nSubscription: ChatGPT\nRenewal cycle: Annually on 03-15\nTime until renewal: Today\nRenewal amount: 20.0"}}`,
				TypeCustom:   `{"Type":"SubscriptionReminder","RuleName":"ChatGPT subscription reminder","Level":"critical","Resources":[{"SubscriptionName":"ChatGPT","OwnerProject":null,"RenewalDay":315,"CycleType":"yearly","DaysUntilRenewal":0,"Amount":20.0,"Message":"Subscription reminder for [ChatGPT]: due today (Annually on 03-15), amount: 20.0"}]}`,
			},
		},
		{
			name: "富文本告警_跑道见底",
			msg:  Custom("balance跑道不足: TestProject", rich, KindRunway),
			want: map[string]string{
				TypeFeishu:   `{"msg_type":"interactive","card":{"header":{"title":{"tag":"plain_text","content":"balance跑道不足: TestProject"},"template":"orange"},"elements":[{"tag":"markdown","content":"**账户**: TestProject\n**Owner project**: 核心业务\n**Provider**: OpenRouter\n**Current balance**: 1,234.50\n**日均消耗**: 120.00（最近 7 天）"}]}}`,
				TypeDingTalk: `{"msgtype":"markdown","markdown":{"title":"balance跑道不足: TestProject","text":"### balance跑道不足: TestProject\n\n**账户**: TestProject\n**Owner project**: 核心业务\n**Provider**: OpenRouter\n**Current balance**: 1,234.50\n**日均消耗**: 120.00（最近 7 天）"}}`,
				TypeWeCom:    `{"msgtype":"markdown","markdown":{"content":"### balance跑道不足: TestProject\n\n**账户**: TestProject\n**Owner project**: 核心业务\n**Provider**: OpenRouter\n**Current balance**: 1,234.50\n**日均消耗**: 120.00（最近 7 天）"}}`,
				// Implementation note.
				TypeCustom: `{"title":"balance跑道不足: TestProject","content":"**账户**: TestProject\n**Owner project**: 核心业务\n**Provider**: OpenRouter\n**Current balance**: 1,234.50\n**日均消耗**: 120.00（最近 7 天）","source":"credit-monitor","timestamp":"2026-09-16T23:50:26.026504"}`,
			},
		},
	}
}

func TestSendPayloadMatchesContract(t *testing.T) {
	for _, tc := range payloadCases() {
		for _, typ := range SupportedTypes() {
			t.Run(tc.name+"/"+typ, func(t *testing.T) {
				got, _ := sendCaptured(t, typ, tc.msg)
				want := tc.want[typ]

				if typ == TypeCustom && tc.msg.envelope != nil {
					// Implementation note.
					// Implementation note.
					assertSameJSON(t, want, got)
					return
				}
				if got != want {
					t.Errorf("报文与约定不一致\n期望: %s\n实际: %s", want, got)
				}
			})
		}
	}
}

// Implementation note.
func TestSendEmailPayload(t *testing.T) {
	service := "OpenAI"
	amount := 20.0
	msg := EmailAlert("财务Mailbox", "Your receipt from OpenAI", "billing@openai.com",
		"2026-09-16 10:00:00", []string{"invoice", "续费"}, &service, &amount)

	got, _ := sendCaptured(t, TypeFeishu, msg)
	want := `{"msg_type":"interactive","card":{"header":{"title":{"tag":"plain_text","content":"📧 Email alert: Your receipt from OpenAI"},"template":"orange"},"elements":[{"tag":"markdown","content":"**Mailbox**: 财务Mailbox\n**Sender**: billing@openai.com\n**Date**: 2026-09-16 10:00:00\n**Service**: OpenAI\n**Amount**: 20.0\n**Keywords**: invoice, 续费"}]}}`
	if got != want {
		t.Errorf("Email alert报文不一致\n期望: %s\n实际: %s", want, got)
	}
}

// Implementation note.
func TestSendCustomFallsBackWithoutEnvelope(t *testing.T) {
	msg := Message{Title: "Balance alert", Lines: []string{"API call: P"}, Kind: KindBalance}

	got, _ := sendCaptured(t, TypeCustom, msg)
	want := `{"title":"Balance alert","content":"API call: P","source":"credit-monitor","timestamp":"2026-09-16T23:50:26.026504"}`
	if got != want {
		t.Errorf("退化报文不一致\n期望: %s\n实际: %s", want, got)
	}
}

func TestSendSetsJSONContentType(t *testing.T) {
	_, header := sendCaptured(t, TypeFeishu, Custom("标题", []string{"**字段**: 值"}, KindWeeklyReport))
	if got := header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q; 期望 application/json", got)
	}
}

// Implementation note.
func sendCaptured(t *testing.T, webhookType string, msg Message) (string, http.Header) {
	t.Helper()

	var body []byte
	var header http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		header = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n, err := New(srv.URL, webhookType, "credit-monitor", srv.Client())
	if err != nil {
		t.Fatalf("New(%q) 返回错误: %v", webhookType, err)
	}
	n.(*notifier).now = func() time.Time { return pinnedNow }

	if err := n.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	return string(body), header
}

// Implementation note.
// Implementation note.
func assertSameJSON(t *testing.T, want, got string) {
	t.Helper()

	wantTokens, gotTokens := jsonTokens(t, want), jsonTokens(t, got)
	if len(wantTokens) != len(gotTokens) {
		t.Fatalf("报文结构不一致\n期望: %s\n实际: %s", want, got)
	}
	for i := range wantTokens {
		if !sameToken(wantTokens[i], gotTokens[i]) {
			t.Fatalf("第 %d 个 token 不一致: 期望 %v; 实际 %v\n期望报文: %s\n实际报文: %s",
				i, wantTokens[i], gotTokens[i], want, got)
		}
	}
}

func jsonTokens(t *testing.T, raw string) []json.Token {
	t.Helper()

	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var out []json.Token
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("解析 JSON 失败: %v（%s）", err, raw)
		}
		out = append(out, tok)
	}
}

func sameToken(a, b json.Token) bool {
	an, aok := a.(json.Number)
	bn, bok := b.(json.Number)
	if aok && bok {
		af, aerr := an.Float64()
		bf, berr := bn.Float64()
		return aerr == nil && berr == nil && af == bf
	}
	return a == b
}

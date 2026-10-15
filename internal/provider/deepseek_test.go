package provider

import (
	"context"
	"testing"
)

func TestDeepSeekFetch(t *testing.T) {
	runSpecCases(t, deepseekSpec, []specCase{
		{
			name: "线上真实结构，金额是字符串",
			body: `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"430.37",
				"granted_balance":"0.00","topped_up_balance":"430.37"}]}`,
			want: 430.37,
		},
		{
			name: "多币种优先取人民币",
			body: `{"balance_infos":[{"currency":"USD","total_balance":"12.00"},
				{"currency":"CNY","total_balance":"88.00"}]}`,
			want: 88.0,
		},
		{
			name: "没有人民币账户退回第一条",
			body: `{"balance_infos":[{"currency":"USD","total_balance":"12.50"}]}`,
			want: 12.5,
		},
		{
			// arrears suspension is still a valid reading; the balance must reach threshold checks or the alert goes mute
			name: "欠费账户照样报 0",
			body: `{"is_available":false,"balance_infos":[{"currency":"CNY","total_balance":"0.00"}]}`,
			want: 0,
		},
		{
			name:   "Missing balance_infos",
			body:   `{"is_available":true}`,
			errMsg: "Could not parse balance_infos field",
		},
		{
			name:   "balance_infos 是空列表",
			body:   `{"is_available":true,"balance_infos":[]}`,
			errMsg: "Could not parse balance_infos field",
		},
		{
			name:   "Missing total_balance",
			body:   `{"balance_infos":[{"currency":"CNY","granted_balance":"0.00"}]}`,
			errMsg: "Could not parse total_balance field",
		},
		{
			name:   "密钥无效",
			status: 401,
			body:   `{"error":{"message":"Authentication Fails"}}`,
			errMsg: "HTTP 401",
		},
	})
}

func TestDeepSeekSendsBearerToken(t *testing.T) {
	srv, rec := serveJSON(t, 200, `{"balance_infos":[{"currency":"CNY","total_balance":"1.00"}]}`)
	if _, err := specProviderAt(deepseekSpec, srv.URL, "sk-deepseek").Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rec.header.Get("Authorization"); got != "Bearer sk-deepseek" {
		t.Fatalf("Authorization = %q", got)
	}
}

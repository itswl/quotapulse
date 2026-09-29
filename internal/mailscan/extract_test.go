package mailscan

import "testing"

// Implementation note.
// Implementation note.
func TestExtractServiceInfo(t *testing.T) {
	tests := []struct {
		name        string
		subject     string
		body        string
		wantService string
		wantAmount  *float64 // nil 表示应当提不出金额
		skipAmount  bool     // 该用例只断言服务名
	}{
		{name: "中文方括号", subject: "【阿里云】余额告警", wantService: "阿里云", skipAmount: true},
		{name: "英文方括号", subject: "[AWS] Balance Alert", wantService: "AWS", skipAmount: true},
		{name: "中文圆括号", subject: "（腾讯云）余额告警", wantService: "腾讯云", skipAmount: true},
		{name: "英文圆括号", subject: "(Azure) 续费通知", wantService: "Azure", skipAmount: true},
		{name: "提不出服务名", subject: "余额告警", wantService: unknownService, skipAmount: true},
		{name: "多个括号取第一个", subject: "【阿里云】【余额】告警", wantService: "阿里云", skipAmount: true},
		{name: "余额前缀带元", subject: "余额告警", body: "余额：100.50 元",
			wantService: unknownService, wantAmount: ptr(100.50)},
		{name: "金额前缀", subject: "余额告警", body: "当前金额: 200.00",
			wantService: unknownService, wantAmount: ptr(200.00)},
		{name: "CNY 前缀", subject: "余额告警", body: "当前余额 CNY 1000.00",
			wantService: unknownService, wantAmount: ptr(1000.00)},
		{name: "千位分隔符", subject: "余额告警", body: "余额：1,234.56 元",
			wantService: unknownService, wantAmount: ptr(1234.56)},
		{name: "提不出金额", subject: "余额告警", body: "请及时充值", wantService: unknownService},
		{name: "整数金额", subject: "告警", body: "余额：100 元",
			wantService: unknownService, wantAmount: ptr(100.0)},
		{name: "余额前缀优先", subject: "告警", body: "余额：88.88 元",
			wantService: unknownService, wantAmount: ptr(88.88)},
		{name: "服务名与金额同时提取", subject: "【阿里云】余额告警", body: "当前余额：50.00 元",
			wantService: "阿里云", wantAmount: ptr(50.00)},
		{name: "全角空格也算空白", subject: "告警", body: "余额：　 66.60 元",
			wantService: unknownService, wantAmount: ptr(66.60)},
		{name: "假数字退回下一条规则", subject: "告警", body: "余额：,,, 元", wantService: unknownService},
		{name: "服务名不跨行", subject: "【阿里云\n】余额告警", wantService: unknownService, skipAmount: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, amount := extractServiceInfo(tt.subject, tt.body)
			if service != tt.wantService {
				t.Errorf("服务名 = %q, 期望 %q", service, tt.wantService)
			}
			if tt.skipAmount {
				return
			}
			switch {
			case tt.wantAmount == nil && amount != nil:
				t.Errorf("金额 = %v, 期望提不出金额", *amount)
			case tt.wantAmount != nil && amount == nil:
				t.Errorf("金额 = nil, 期望 %v", *tt.wantAmount)
			case tt.wantAmount != nil && *amount != *tt.wantAmount:
				t.Errorf("金额 = %v, 期望 %v", *amount, *tt.wantAmount)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

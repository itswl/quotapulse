package provider

import (
	"context"
	"testing"
)

func TestGLMFetch(t *testing.T) {
	runSpecCases(t, glmSpec, []specCase{
		{
			// The longer window has less quota left, but the five-hour window is authoritative.
			name: "多窗口固定取五小时窗口",
			body: `{"code":200,"msg":"操作成功","success":true,"data":{"level":"pro","limits":[
				{"type":"TIME_LIMIT","unit":5,"number":1,"usage":1000,"currentValue":13,
				 "remaining":987,"percentage":1,"nextResetTime":1790409645998},
				{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":90}]}}`,
			want: 98.7,
		},
		{
			name: "新版 CREDIT_LIMIT 结构",
			body: `{"code":200,"msg":"Operation successful","success":true,"data":{"level":"lite","limits":[
				{"type":"CREDIT_LIMIT","unit":3,"number":5,"usage":2000,"currentValue":402,
				 "remaining":1597,"percentage":20},
				{"type":"CREDIT_LIMIT","unit":5,"number":1,"usage":10000,"currentValue":5208,
				 "remaining":4792,"percentage":48},
				{"type":"CREDIT_LIMIT","unit":6,"number":1,"usage":10000,"currentValue":5207,
				 "remaining":4792,"percentage":52}]}}`,
			want: 47.92,
		},
		{
			name: "只有百分比且已用满",
			body: `{"code":200,"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":100}]}}`,
			want: 0,
		},
		{
			// usage of 0 cannot divide; fall back to the API's used-percent
			name: "usage 为 0 时用百分比反推",
			body: `{"code":200,"success":true,"data":{"limits":[
				{"type":"CREDIT_LIMIT","usage":0,"remaining":0,"percentage":25}]}}`,
			want: 75,
		},
		{
			name: "异常数据钳制在 0-100",
			body: `{"code":200,"success":true,"data":{"limits":[
				{"type":"CREDIT_LIMIT","unit":3,"number":5,"usage":100,"remaining":150},
				{"type":"TOKENS_LIMIT","unit":5,"number":1,"percentage":130}]}}`,
			want: 0,
		},
		{
			name: "没有 code field视为成功",
			body: `{"success":true,"data":{"limits":[{"usage":4,"remaining":1}]}}`,
			want: 25,
		},
		{
			name: "算不尽时保留两位小数",
			body: `{"success":true,"data":{"limits":[{"usage":3,"remaining":2}]}}`,
			want: 66.67,
		},
		{
			name:   "业务失败",
			body:   `{"code":401,"msg":"令牌无效","success":false,"data":null}`,
			errMsg: "API returned an error: 令牌无效",
		},
		{
			name:   "success 为真但 code 不是 200",
			body:   `{"code":500,"msg":"服务异常","success":true}`,
			errMsg: "API returned an error: 服务异常",
		},
		{
			name:   "Missing data.limits",
			body:   `{"code":200,"success":true,"data":{"level":"pro"}}`,
			errMsg: "Could not parse data.limits field",
		},
		{
			name:   "limits 里没有能算的窗口",
			body:   `{"code":200,"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"number":5}]}}`,
			errMsg: "data.limits contains no five-hour quota window",
		},
		{
			name:   "limits 是空列表",
			body:   `{"code":200,"success":true,"data":{"limits":[]}}`,
			errMsg: "data.limits contains no five-hour quota window",
		},
		{
			name:   "没有五小时窗口时不猜测其他窗口",
			body:   `{"code":200,"success":true,"data":{"limits":[{"type":"CREDIT_LIMIT","unit":3,"number":5,"percentage":20},{"type":"CREDIT_LIMIT","unit":6,"number":1,"percentage":30}]}}`,
			errMsg: "data.limits contains no five-hour quota window",
		},
	})
}

func TestGLMSendsBearerToken(t *testing.T) {
	srv, rec := serveJSON(t, 200, `{"success":true,"data":{"limits":[{"percentage":0}]}}`)
	if _, err := specProviderAt(glmSpec, srv.URL, "glm-key").Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rec.header.Get("Authorization"); got != "Bearer glm-key" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestGLMRemainingPercentIgnoresStringNumbers(t *testing.T) {
	// quota fields must be real numbers; the string "50" does not count.
	// Loosening this to Num would admit strings and produce a plausible-looking
	// wrong balance
	if _, ok := glmRemainingPercent(map[string]any{"usage": "100", "remaining": "50"}); ok {
		t.Error("字符串的 usage/remaining 不该被当成数字")
	}
	if _, ok := glmRemainingPercent(map[string]any{"percentage": "30"}); ok {
		t.Error("字符串的 percentage 不该被当成数字")
	}
}

func TestRound2HalfToEven(t *testing.T) {
	// round2's contract: exact ties go to even; the rest follow the true binary value
	cases := []struct{ in, want float64 }{
		{0.125, 0.12}, // 正中间，进偶数
		{0.135, 0.14}, // 二进制里略大于 0.135
		{2.675, 2.67}, // 二进制里略小于 2.675
		{0.005, 0.01}, // 二进制里略大于 0.005
		{98.7, 98.7},  // 本来就够短
		{66.666666, 66.67},
		{33.333333, 33.33},
	}
	for _, tc := range cases {
		if got := round2(tc.in); got != tc.want {
			t.Errorf("round2(%v) = %v，expected %v", tc.in, got, tc.want)
		}
	}
}

package provider

import (
	"context"
	"testing"
)

func TestWxRankFetch(t *testing.T) {
	runSpecCases(t, wxrankSpec, []specCase{
		{
			name: "从 msg 文本里抠数字",
			body: `{"code":0,"msg":"剩余263419余额"}`,
			want: 263419,
		},
		{
			name: "msg 里有多段数字取第一段",
			body: `{"code":0,"msg":"剩余100次，共200次"}`,
			want: 100,
		},
		{
			name: "后备：data 直接是数字",
			body: `{"code":0,"msg":"查询成功","data":5000}`,
			want: 5000,
		},
		{
			name: "后备：data.score",
			body: `{"code":0,"msg":"查询成功","data":{"score":1234}}`,
			want: 1234,
		},
		{
			// fallback fields pick by truthiness: a score of 0 falls through to credits
			name: "后备：score 为 0 时看 credits",
			body: `{"code":0,"msg":"查询成功","data":{"score":0,"credits":66}}`,
			want: 66,
		},
		{
			name: "后备：顶层 score",
			body: `{"code":0,"msg":"查询成功","score":42}`,
			want: 42,
		},
		{
			name:   "business error码",
			body:   `{"code":-1,"msg":"密钥无效"}`,
			errMsg: "API returned an error: 密钥无效",
		},
		{
			name:   "Missing code 也算失败",
			body:   `{"msg":"Missing code"}`,
			errMsg: "API returned an error: Missing code",
		},
		{
			name:   "Missing code 且Missing msg",
			body:   `{}`,
			errMsg: "API returned an error: Unknown error",
		},
		{
			// a non-string msg still gets a proper message; a failed type assertion must not leak to users
			name:   "msg 不是字符串",
			body:   `{"code":0,"msg":12345}`,
			errMsg: "Could not parse balance",
		},
		{
			name:   "Could not parse balance",
			body:   `{"code":0,"msg":"无数据"}`,
			errMsg: "Could not parse balance: 无数据",
		},
	})
}

func TestWxRankPutsKeyInQuery(t *testing.T) {
	// this provider authenticates via query parameter, not Bearer
	srv, rec := serveJSON(t, 200, `{"code":0,"msg":"剩余1余额"}`)
	if _, err := specProviderAt(wxrankSpec, srv.URL, "wx-secret").Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rec.query.Get("key"); got != "wx-secret" {
		t.Fatalf("key = %q", got)
	}
	if got := rec.header.Get("Authorization"); got != "" {
		t.Fatalf("不该带 Authorization 头，却带了 %q", got)
	}
}

package config

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParseMCPKeys(t *testing.T) {
	keys, err := ParseMCPKeys(`
		claude:qp_7f3a91c2d4e5f6a7b8c9:balance,alerts;
		cursor:qp_0123456789abcdef01:all:2026-12-31
		ci:qp_fedcba98765432100f::2026-10-01T08:00:00+08:00`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("应解析出 3 个 key，实际 %d 个", len(keys))
	}
	if !keys[0].Scopes[ScopeBalance] || !keys[0].Scopes[ScopeAlerts] || keys[0].Scopes[ScopeSubscriptions] {
		t.Errorf("claude 的 scope 不对: %v", keys[0].Scopes)
	}
	if keys[0].Expires != nil {
		t.Error("没写过期时间就不该过期")
	}
	for _, scope := range AllScopes {
		if !keys[1].Scopes[scope] || !keys[2].Scopes[scope] {
			t.Errorf("all 和留空都应展开为全部 scope，缺 %s", scope)
		}
	}
	if !keys[1].Expired(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) || keys[1].Expired(time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC)) {
		t.Error("日期形式的过期时间应包含当天，次日零点起失效")
	}
	if want := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC); keys[2].Expires == nil || !keys[2].Expires.Equal(want) {
		t.Errorf("RFC 3339 过期时间解析错误: %v", keys[2].Expires)
	}
}

func TestParseMCPKeysRejectsBadEntries(t *testing.T) {
	secret := "qp_supersecretvalue123"
	for raw, want := range map[string]string{
		"justaname":                             "name:key",
		"a b:" + secret:                         "key name",
		"short:tooshort":                        "at least",
		"x:" + secret + ";y:" + secret:          "reuses",
		"x:" + secret + ";x:qp_anothervalue456": "used twice",
		"x:" + secret + ":payments":             "unknown scope",
		"x:" + secret + ":all:tomorrow":         "expiry",
	} {
		_, err := ParseMCPKeys(raw)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: 期望错误包含 %q，实际 %v", raw, want, err)
		}
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Errorf("%q: 错误信息不能带出 key 本身: %v", raw, err)
		}
	}
}

func TestMCPCallerContext(t *testing.T) {
	if _, ok := MCPCallerFrom(context.Background()); ok {
		t.Fatal("没有附加调用方时应返回 false")
	}
	ctx := WithMCPCaller(context.Background(), MCPCaller{Name: "claude", Scopes: map[string]bool{ScopeBalance: true}})
	caller, ok := MCPCallerFrom(ctx)
	if !ok || caller.Name != "claude" || !caller.Allows(ScopeBalance) || caller.Allows(ScopeConfig) {
		t.Fatalf("调用方没有如实取回: %+v", caller)
	}
	if full := FullMCPCaller("web"); !full.Allows(ScopeConfig) || !full.Allows(ScopeEmail) {
		t.Error("WEB_API_KEY 的调用方应拥有全部 scope")
	}
}

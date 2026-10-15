package provider

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The signature string must match Aliyun's documented algorithm byte for
// byte; a fixed key, timestamp and nonce pin the result here so regressions
// surface immediately — in production a broken signature only shows as a
// 403, and without these tests pinpointing the wrong step is painful.
const (
	aliyunTestID     = "LTAI5tTestAccessKey"
	aliyunTestSecret = "testAccessKeySecret"
	aliyunTestNonce  = "f81d4fae-7dec-11d0-a765-00a0c91e6bf6"
)

var aliyunTestTime = time.Date(2024, 5, 17, 8, 30, 45, 0, time.UTC)

func aliyunProviderAt(rawURL string) *aliyunProvider {
	return &aliyunProvider{
		accessKeyID: aliyunTestID, accessKeySecret: aliyunTestSecret,
		client:  NewClient(2 * time.Second),
		baseURL: rawURL,
		now:     func() time.Time { return aliyunTestTime },
		nonce:   func() string { return aliyunTestNonce },
	}
}

func TestAliyunSignature(t *testing.T) {
	params := aliyunProviderAt(aliyunBaseURL).buildParams(aliyunTestTime, aliyunTestNonce)

	want := map[string]string{
		"Action":           "QueryAccountBalance",
		"Version":          "2017-12-14",
		"AccessKeyId":      aliyunTestID,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":   aliyunTestNonce,
		"Format":           "JSON",
		"Timestamp":        "2024-05-17T08:30:45Z",
		"Signature":        "z8MVrPhYnXWKscZGXgj/jOnJNi4=",
	}
	if len(params) != len(want) {
		t.Fatalf("参数 = %v", params)
	}
	for key, value := range want {
		if params[key] != value {
			t.Errorf("%s = %q，expected %q", key, params[key], value)
		}
	}
}

func TestAliyunSignatureUsesUTC(t *testing.T) {
	// Timestamp must be UTC; a local timezone reads as excessive clock skew
	shanghai := time.FixedZone("CST", 8*3600)
	params := aliyunProviderAt(aliyunBaseURL).buildParams(aliyunTestTime.In(shanghai), aliyunTestNonce)
	if params["Timestamp"] != "2024-05-17T08:30:45Z" {
		t.Fatalf("Timestamp = %q", params["Timestamp"])
	}
	if params["Signature"] != "z8MVrPhYnXWKscZGXgj/jOnJNi4=" {
		t.Fatalf("Signature = %q", params["Signature"])
	}
}

func TestPercentEncode(t *testing.T) {
	// RFC 3986 encoding, exactly what the signature needs: space is %20 not +, * is encoded, ~ is not
	got := percentEncode("a b+c*d~e/f=g&h%i中")
	if want := "a%20b%2Bc%2Ad~e%2Ff%3Dg%26h%25i%E4%B8%AD"; got != want {
		t.Fatalf("percentEncode\n = %q\nexpected %q", got, want)
	}
}

func TestAliyunFetch(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   float64
		errMsg string
	}{
		{
			name: "标准结构，金额带千位分隔符",
			body: `{"Code":"Success","Message":"Successful!","RequestId":"x",
				"Data":{"AvailableAmount":"1,234.56","Currency":"CNY","AvailableCashAmount":"1,000.00"}}`,
			want: 1234.56,
		},
		{
			name: "数字status code也算成功",
			body: `{"Code":200,"Data":{"AvailableAmount":"88.00"}}`,
			want: 88,
		},
		{
			name: "旧结构：金额在顶层",
			body: `{"AvailableAmount":"50.00"}`,
			want: 50,
		},
		{
			name: "退回 AvailableCashAmount",
			body: `{"Code":"Success","Data":{"Currency":"CNY","AvailableCashAmount":"9.90"}}`,
			want: 9.9,
		},
		{
			name: "Data.AvailableAmount 优先于顶层field",
			body: `{"Data":{"AvailableAmount":"1.00"},"AvailableCashAmount":"999.00"}`,
			want: 1,
		},
		{
			// non-object Data still gets a proper message; a failed type assertion must not leak to users
			name:   "密钥无效（HTTP 400 带business error）",
			status: 400,
			body: `{"Code":"InvalidAccessKeyId.NotFound","Message":"Specified access key is not found.",
				"RequestId":"x","HostId":"business.aliyuncs.com"}`,
			errMsg: "API returned an error: Specified access key is not found. (Code: InvalidAccessKeyId.NotFound)",
		},
		{
			name:   "business error缺 Message",
			body:   `{"Code":"Forbidden"}`,
			errMsg: "API returned an error: Unknown error (Code: Forbidden)",
		},
		{
			name:   "Could not parse balance",
			body:   `{"Code":"Success","Data":{"Currency":"CNY"}}`,
			errMsg: "Could not parse balance field, response: ",
		},
		{
			// non-object Data still gets a proper message; a failed type assertion must not leak to users
			name:   "Data 不是对象",
			body:   `{"Code":"Success","Data":"oops"}`,
			errMsg: "Could not parse balance field, response: ",
		},
		{
			name:   "空response体",
			body:   "",
			errMsg: "API returned an empty response",
		},
		{
			name:   "不是 JSON",
			body:   `<html>502</html>`,
			errMsg: "Response is not valid JSON:<html>502</html>",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := serveJSON(t, tc.status, tc.body)
			got, err := aliyunProviderAt(srv.URL).Fetch(context.Background())
			if tc.errMsg != "" {
				if err == nil {
					t.Fatalf("expected失败，却拿到余额 %v", got)
				}
				if !strings.Contains(err.Error(), tc.errMsg) {
					t.Fatalf("error message = %q，expected包含 %q", err.Error(), tc.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("Fetch 失败: %v", err)
			}
			assertFloat(t, got, tc.want)
		})
	}
}

func TestAliyunRequestIsSignedOverWhatItSends(t *testing.T) {
	srv, rec := serveJSON(t, 200, `{"Code":"Success","Data":{"AvailableAmount":"1.00"}}`)
	provider := aliyunProviderAt(srv.URL)
	if _, err := provider.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}

	// recompute the signature from what the server actually received: what is
	// signed and what is sent must be the same — one missing or extra parameter
	// makes Aliyun compute a different signature
	sent := map[string]string{}
	for key := range rec.query {
		sent[key] = rec.query.Get(key)
	}
	signature := sent["Signature"]
	if signature == "" {
		t.Fatal("请求里没有 Signature")
	}
	delete(sent, "Signature")

	if want := aliyunRPCSign(provider.accessKeySecret, sent); want != signature {
		t.Fatalf("请求带的签名 = %q，按收到的参数重算 = %q", signature, want)
	}
	if sent["Action"] != aliyunAction || sent["Version"] != aliyunVersion {
		t.Errorf("参数 = %v", sent)
	}
}

func TestAliyunNonceIsRandomUUID(t *testing.T) {
	// a repeated SignatureNonce is rejected as a replay attack
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	first, second := aliyunNonce(), aliyunNonce()
	if !pattern.MatchString(first) {
		t.Fatalf("nonce = %q，不是 v4 UUID", first)
	}
	if first == second {
		t.Fatal("两次生成的 nonce 相同")
	}
}

func TestAliyunKeyFormat(t *testing.T) {
	if _, err := New("aliyun", "LTAI5tonly-id", nil); err == nil ||
		!strings.Contains(err.Error(), "Alibaba Cloud: invalid API key format; expected 'AccessKeyId:AccessKeySecret'") {
		t.Fatalf("error = %v", err)
	}
	if _, err := New("aliyun", "id:secret", nil); err != nil {
		t.Fatalf("合法密钥被拒: %v", err)
	}
}

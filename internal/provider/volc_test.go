package provider

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The signature must match Volcengine's documented HMAC-SHA256 algorithm; a
// fixed key and instant pin the result here, so a broken signature turns red
// at once — in production it only shows as a 403, and without these tests
// pinpointing it is painful.
const (
	volcTestAK = "AKLTtest-access-key"
	volcTestSK = "test-secret-key"
)

var volcTestTime = time.Date(2024, 5, 17, 8, 30, 45, 0, time.UTC)

func volcProviderAt(rawURL string) *volcProvider {
	return &volcProvider{
		ak: volcTestAK, sk: volcTestSK,
		client:  NewClient(2 * time.Second),
		baseURL: rawURL,
		now:     func() time.Time { return volcTestTime },
	}
}

func TestVolcSignature(t *testing.T) {
	got := volcProviderAt(volcBaseURL).buildHeaders(volcTestTime, "")
	want := map[string]string{
		"Host":             "open.volcengineapi.com",
		"X-Content-Sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"X-Date":           "20240517T083045Z",
		"Content-Type":     "application/json",
		"Authorization": "HMAC-SHA256 Credential=AKLTtest-access-key/20240517/cn-shanghai/billing/request, " +
			"SignedHeaders=content-type;host;x-content-sha256;x-date, " +
			"Signature=6a51ffbd7c59c8512f74418ce1413b3073e98935284f9df23627c8ae659c51eb",
	}
	if len(got) != len(want) {
		t.Fatalf("签名头 = %v", got)
	}
	for name, value := range want {
		if got[name] != value {
			t.Errorf("%s\n = %q\nexpected %q", name, got[name], value)
		}
	}
}

func TestVolcSignatureCoversBody(t *testing.T) {
	// only GET with an empty body today, but the digest must track body changes so a future POST signs correctly
	got := volcProviderAt(volcBaseURL).buildHeaders(volcTestTime, `{"Limit":1}`)
	if want := "55522f708dcfebccb7bd3e8d0001a53ecaf2beca9ca801f1e9161e24215faa99"; got["X-Content-Sha256"] != want {
		t.Errorf("X-Content-Sha256 = %q，expected %q", got["X-Content-Sha256"], want)
	}
	if want := "Signature=d9e714eb64900095dd9fd482ed4a29b0199fc08171ccfdd806943a2137a3c5b7"; !strings.HasSuffix(got["Authorization"], want) {
		t.Errorf("Authorization = %q，expected以 %q 结尾", got["Authorization"], want)
	}
}

func TestVolcSignatureUsesUTC(t *testing.T) {
	// same instant, different zones must sign identically: an X-Date with a local zone is judged expired
	shanghai := time.FixedZone("CST", 8*3600)
	got := volcProviderAt(volcBaseURL).buildHeaders(volcTestTime.In(shanghai), "")
	if got["X-Date"] != "20240517T083045Z" {
		t.Fatalf("X-Date = %q", got["X-Date"])
	}
}

func TestVolcNormQuery(t *testing.T) {
	// the query string used for signing and the one actually sent must be identical; keys are sorted
	got := volcNormQuery(map[string]string{"Version": volcVersion, "Action": volcAction})
	if want := "Action=QueryBalanceAcct&Version=2022-01-01"; got != want {
		t.Fatalf("查询串 = %q，expected %q", got, want)
	}
}

func TestVolcFetch(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   float64
		errMsg string
	}{
		{
			name: "金额是字符串",
			body: `{"ResponseMetadata":{"RequestId":"x","Action":"QueryBalanceAcct"},
				"Result":{"AccountID":1,"AvailableBalance":"1234.56","CashBalance":"1000.00"}}`,
			want: 1234.56,
		},
		{
			name: "金额是数字",
			body: `{"Result":{"AvailableBalance":88.5}}`,
			want: 88.5,
		},
		{
			// Volcengine puts business errors in ResponseMetadata.Error while HTTP stays 200
			name:   "business error",
			body:   `{"ResponseMetadata":{"Error":{"Code":"AuthFailure","Message":"invalid ak"}}}`,
			errMsg: `API returned an error: {"Code":"AuthFailure","Message":"invalid ak"}`,
		},
		{
			name:   "Missing AvailableBalance",
			body:   `{"ResponseMetadata":{"RequestId":"x"},"Result":{"CashBalance":"1.00"}}`,
			errMsg: "Could not parse AvailableBalance field",
		},
		{
			name:   "空response体",
			body:   "",
			errMsg: "API returned an empty response",
		},
		{
			name:   "空对象",
			body:   `{}`,
			errMsg: "API returned an empty response",
		},
		{
			name:   "不是 JSON",
			body:   `<html>502 Bad Gateway</html>`,
			errMsg: "Response is not valid JSON:<html>502 Bad Gateway</html>",
		},
		{
			name:   "HTTP 失败",
			status: 403,
			body:   `{"ResponseMetadata":{"Error":{"Code":"SignatureDoesNotMatch"}}}`,
			errMsg: "HTTP request failed, status code: 403",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := serveJSON(t, tc.status, tc.body)
			got, err := volcProviderAt(srv.URL).Fetch(context.Background())
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

func TestVolcRequestShape(t *testing.T) {
	srv, rec := serveJSON(t, 200, `{"Result":{"AvailableBalance":"1.00"}}`)
	if _, err := volcProviderAt(srv.URL).Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}

	if rec.query.Get("Action") != volcAction || rec.query.Get("Version") != volcVersion {
		t.Errorf("查询参数 = %v", rec.query)
	}
	// Host participates in the signature; the sent Host header must be the signed one, not the connection target
	if rec.host != volcHost {
		t.Errorf("Host = %q，expected %q", rec.host, volcHost)
	}
	if got := rec.header.Get("X-Date"); got != "20240517T083045Z" {
		t.Errorf("X-Date = %q", got)
	}
	if got := rec.header.Get("Content-Type"); got != volcContentType {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.header.Get("Authorization"); !strings.HasPrefix(got, "HMAC-SHA256 Credential="+volcTestAK+"/") {
		t.Errorf("Authorization = %q", got)
	}
}

func TestVolcKeyFormat(t *testing.T) {
	if _, err := New("volc", "AKLTxxx", nil); err == nil ||
		!strings.Contains(err.Error(), "Volcengine: invalid API key format; expected 'AK:SK'") {
		t.Fatalf("error = %v", err)
	}
	if _, err := New("volc", "AKLTxxx:sk", nil); err != nil {
		t.Fatalf("合法密钥被拒: %v", err)
	}
}

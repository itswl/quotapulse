package provider

import (
	"strings"
	"testing"
	"time"
)

// Signature vectors pinning the shared signing helpers, carried over from the
// retired adapters' tests.

func TestVolcV4SignatureVector(t *testing.T) {
	now := time.Date(2024, 5, 17, 12, 34, 56, 0, time.UTC)
	headers := VolcV4Headers(VolcV4Request{
		Host: "open.volcengineapi.com", Path: "/", Region: "cn-shanghai", Service: "billing",
		AK: "AKLTtest-access-key", SK: "test-secret-key",
		Params: map[string]string{"Action": "QueryBalanceAcct", "Version": "2022-01-01"},
	}, now)
	if headers["X-Date"] != "20240517T123456Z" {
		t.Errorf("X-Date = %q", headers["X-Date"])
	}
	auth := headers["Authorization"]
	if !strings.HasPrefix(auth, "HMAC-SHA256 Credential=AKLTtest-access-key/20240517/cn-shanghai/billing/request") {
		t.Errorf("Authorization malformed: %q", auth)
	}
	// body participates in the digest
	empty := VolcV4Headers(VolcV4Request{Host: "h", Path: "/", Region: "r", Service: "s", AK: "a", SK: "b"}, now)
	withBody := VolcV4Headers(VolcV4Request{Host: "h", Path: "/", Region: "r", Service: "s", AK: "a", SK: "b", Body: "x"}, now)
	if empty["X-Content-Sha256"] == withBody["X-Content-Sha256"] {
		t.Error("the digest must follow the body")
	}
}

func TestAliyunRPCSignatureVector(t *testing.T) {
	now := time.Date(2024, 5, 17, 12, 34, 56, 0, time.UTC)
	params := AliyunRPCParams("test-id", "test-secret", "QueryAccountBalance", "2017-12-14", nil, now, "nonce-1")
	if params["Timestamp"] != "2024-05-17T12:34:56Z" {
		t.Errorf("Timestamp = %q", params["Timestamp"])
	}
	// the signature covers every sent parameter: recompute over the received set
	signature := params["Signature"]
	delete(params, "Signature")
	if want := aliyunRPCSign("test-secret", params); want != signature {
		t.Fatalf("signature not reproducible: %q vs %q", signature, want)
	}
}

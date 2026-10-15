package provider

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Request signing presets, shared by the built-in adapters and the custom
// providers. The algorithms are fixed by the platforms; the parameters
// (keys, actions, regions) vary per definition.

// AliyunRPCParams assembles the fully signed query parameters for an
// Aliyun RPC-style GET: access key, timestamps and nonce plus the business
// parameters (Action, Version, anything extra the API mandates).
func AliyunRPCParams(id, secret, action, version string, extra map[string]string, now time.Time, nonce string) map[string]string {
	params := map[string]string{
		"Action":           action,
		"Version":          version,
		"AccessKeyId":      id,
		"SignatureMethod":  "HMAC-SHA1",
		"Timestamp":        now.UTC().Format("2006-01-02T15:04:05Z"),
		"SignatureVersion": "1.0",
		"SignatureNonce":   nonce,
		"Format":           "JSON",
	}
	for key, value := range extra {
		params[key] = value
	}
	params["Signature"] = aliyunRPCSign(secret, params)
	return params
}

// aliyunRPCSign computes the RPC signature: canonical query string ->
// string-to-sign -> HMAC-SHA1 -> base64.
func aliyunRPCSign(secret string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, percentEncode(key)+"="+percentEncode(params[key]))
	}
	canonicalQuery := strings.Join(pairs, "&")

	// the string-to-sign re-encodes the whole query string; "/" becomes %2F too
	stringToSign := "GET&" + percentEncode("/") + "&" + percentEncode(canonicalQuery)

	// the trailing & on the key is Aliyun's rule, not a typo
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// VolcV4Request is one Volcengine V4-signed GET: host and path come from the
// endpoint URL, region and service from the platform's documentation, and
// params carries the business parameters (Action, Version).
type VolcV4Request struct {
	Host    string
	Path    string
	Region  string
	Service string
	AK, SK  string
	Params  map[string]string
	Body    string
}

// VolcV4Headers builds the signed headers for the request. The canonical
// request joins method, path, normalized query, the fixed header set and the
// body digest (empty for GETs); the signing key derives through the date/
// region/service/request ladder.
func VolcV4Headers(req VolcV4Request, now time.Time) map[string]string {
	xDate := now.UTC().Format("20060102T150405Z")
	shortDate := xDate[:8]
	contentSHA256 := volcSHA256(req.Body)
	credentialScope := strings.Join([]string{shortDate, req.Region, req.Service, "request"}, "/")
	signedHeaders := strings.Join([]string{"content-type", "host", "x-content-sha256", "x-date"}, ";")

	canonicalRequest := strings.Join([]string{
		http.MethodGet,
		req.Path,
		volcNormQuery(req.Params),
		"content-type:application/json",
		"host:" + req.Host,
		"x-content-sha256:" + contentSHA256,
		"x-date:" + xDate,
		"",
		signedHeaders,
		contentSHA256,
	}, "\n")

	stringToSign := strings.Join([]string{
		"HMAC-SHA256", xDate, credentialScope, volcSHA256(canonicalRequest),
	}, "\n")

	// derive the signing key: starting from the SK, HMAC level by level through
	// date, region, service, request — leaking an intermediate key then only
	// compromises that one day and service
	signingKey := []byte(req.SK)
	for _, part := range []string{shortDate, req.Region, req.Service, "request"} {
		signingKey = volcHMAC(signingKey, part)
	}
	signature := hex.EncodeToString(volcHMAC(signingKey, stringToSign))

	return map[string]string{
		"Host":             req.Host,
		"X-Content-Sha256": contentSHA256,
		"X-Date":           xDate,
		"Content-Type":     "application/json",
		"Authorization": fmt.Sprintf(
			"HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
			req.AK, credentialScope, signedHeaders, signature,
		),
	}
}

// hostAndPathOf extracts the host and path a signing preset needs from the
// endpoint URL.
func hostAndPathOf(rawURL string) (string, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return "", "", fmt.Errorf("cannot derive host from %q", rawURL)
	}
	path := parsed.Path
	if path == "" {
		path = "/"
	}
	return parsed.Host, path, nil
}

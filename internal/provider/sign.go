package provider

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Two cloud platforms sign requests instead of taking a key. The algorithms
// are theirs and fixed; only the parameters vary by definition.

// percentEncode is RFC 3986 encoding as both platforms define it: everything
// but letters, digits and -_.~ is escaped, and a space is %20, never "+".
func percentEncode(s string) string {
	encoded := url.QueryEscape(s)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	return strings.ReplaceAll(encoded, "%7E", "~")
}

// canonicalQuery sorts parameters and encodes them. The signature covers this
// exact string, so the request must send it byte for byte.
func canonicalQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, percentEncode(key)+"="+percentEncode(params[key]))
	}
	return strings.Join(pairs, "&")
}

// aliyunRPCQuery returns the signed query for an Alibaba Cloud RPC-style GET.
// params are the business parameters (Action, Version, …).
func aliyunRPCQuery(id, secret string, params map[string]string, now time.Time, nonce string) map[string]string {
	signed := map[string]string{
		"AccessKeyId":      id,
		"Format":           "JSON",
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   nonce,
		"SignatureVersion": "1.0",
		"Timestamp":        now.UTC().Format("2006-01-02T15:04:05Z"),
	}
	for key, value := range params {
		signed[key] = value
	}
	signed["Signature"] = aliyunRPCSign(secret, "GET", signed)
	return signed
}

// aliyunRPCSign is HMAC-SHA1 over "METHOD&%2F&<encoded canonical query>".
func aliyunRPCSign(secret, method string, params map[string]string) string {
	stringToSign := method + "&" + percentEncode("/") + "&" + percentEncode(canonicalQuery(params))
	// The trailing & on the key is part of the scheme, not a typo.
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// volcV4 is one Volcengine request to sign.
type volcV4 struct {
	Method, Host, Path string
	Region, Service    string
	AK, SK             string
	Query              map[string]string
	Body               string
}

// headers returns the signed headers. The signing key is derived step by
// step through date, region and service, so a leaked intermediate key is
// only good for one day of one service.
func (r volcV4) headers(now time.Time) map[string]string {
	xDate := now.UTC().Format("20060102T150405Z")
	shortDate := xDate[:8]
	bodyHash := sha256Hex(r.Body)
	scope := strings.Join([]string{shortDate, r.Region, r.Service, "request"}, "/")
	signedHeaders := "content-type;host;x-content-sha256;x-date"

	canonicalRequest := strings.Join([]string{
		r.Method,
		r.Path,
		canonicalQuery(r.Query),
		"content-type:application/json",
		"host:" + r.Host,
		"x-content-sha256:" + bodyHash,
		"x-date:" + xDate,
		"",
		signedHeaders,
		bodyHash,
	}, "\n")
	stringToSign := strings.Join([]string{"HMAC-SHA256", xDate, scope, sha256Hex(canonicalRequest)}, "\n")

	key := []byte(r.SK)
	for _, part := range []string{shortDate, r.Region, r.Service, "request"} {
		key = hmacSHA256(key, part)
	}
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))

	return map[string]string{
		"Content-Type":     "application/json",
		"X-Content-Sha256": bodyHash,
		"X-Date":           xDate,
		"Authorization": fmt.Sprintf("HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
			r.AK, scope, signedHeaders, signature),
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, message string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return mac.Sum(nil)
}

// nonce is a random UUID. The platforms only need it unique.
func nonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

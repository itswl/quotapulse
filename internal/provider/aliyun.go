package provider

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Aliyun reports the account's available credit (CNY). Signing is the old RPC
// style: sort parameters into a canonical string and sign with HMAC-SHA1. The
// algorithm is the API's requirement, not our choice — only the newer APIs use
// V3/HMAC-SHA256.
const (
	aliyunEndpoint = "business.aliyuncs.com"
	aliyunAction   = "QueryAccountBalance"
	aliyunVersion  = "2017-12-14"
	aliyunBaseURL  = "https://" + aliyunEndpoint
)

type aliyunProvider struct {
	accessKeyID     string
	accessKeySecret string
	client          *Client
	baseURL         string
	// now and nonce are swappable so tests can pin signature results
	now   func() time.Time
	nonce func() string
}

func init() {
	RegisterWithCurrency("aliyun", "Alibaba Cloud", "balance", "CNY", func(apiKey string, client *Client) (Provider, error) {
		id, secret, err := SplitKeyPair(apiKey, "Alibaba Cloud", "AccessKeyId:AccessKeySecret")
		if err != nil {
			return nil, err
		}
		return &aliyunProvider{
			accessKeyID: id, accessKeySecret: secret, client: client,
			baseURL: aliyunBaseURL,
			now:     func() time.Time { return time.Now().UTC() },
			nonce:   aliyunNonce,
		}, nil
	})
}

func (p *aliyunProvider) Fetch(ctx context.Context) (float64, error) {
	data, err := p.request(ctx)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, errors.New("API returned an empty response")
	}
	return aliyunExtractAmount(data)
}

// aliyunExtractAmount parses the balance. Aliyun's API versions return different
// shapes: both the Code-wrapped standard structure and the legacy one with a
// bare balance field must parse.
func aliyunExtractAmount(data map[string]any) (float64, error) {
	if code, ok := data["Code"]; ok && code != nil && !aliyunCodeOK(code) {
		return 0, fmt.Errorf("API returned an error: %s (Code: %s)",
			messageOr(data, "Message", "Unknown error"), formatValue(code))
	}

	scope := Object(data["Data"])
	candidates := []any{
		scope["AvailableAmount"],
		data["AvailableAmount"],
		data["AvailableCashAmount"],
		scope["AvailableCashAmount"],
	}
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		// amounts may be strings with thousands separators ("1,234.56"); Num already handles commas
		if amount, ok := Num(candidate); ok {
			return amount, nil
		}
		break
	}
	return 0, fmt.Errorf("Could not parse balance field, response: %s", formatValue(data))
}

// aliyunCodeOK checks the business code: string "Success", number 200 and string "200" all count as success.
func aliyunCodeOK(code any) bool {
	if s, ok := code.(string); ok && s == "Success" {
		return true
	}
	if n, ok := jsonNum(code); ok && n == 200 {
		return true
	}
	return formatValue(code) == "200"
}

func (p *aliyunProvider) request(ctx context.Context) (map[string]any, error) {
	query := url.Values{}
	for key, value := range p.buildParams(p.now(), p.nonce()) {
		query.Set(key, value)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}

	// the status code is deliberately not checked: Aliyun returns business errors
	// as 4xx + JSON, and failing early on status would replace useful "invalid
	// AccessKey" details with a bare HTTP 400
	_, body, err := fetchBody(p.client, req)
	if err != nil {
		return nil, err
	}
	return parseJSONObject(body)
}

// buildParams assembles the fully signed request parameters.
func (p *aliyunProvider) buildParams(now time.Time, nonce string) map[string]string {
	params := map[string]string{
		"Action":           aliyunAction,
		"Version":          aliyunVersion,
		"AccessKeyId":      p.accessKeyID,
		"SignatureMethod":  "HMAC-SHA1",
		"Timestamp":        now.UTC().Format("2006-01-02T15:04:05Z"),
		"SignatureVersion": "1.0",
		"SignatureNonce":   nonce,
		"Format":           "JSON",
	}
	params["Signature"] = p.sign(params)
	return params
}

// sign computes the RPC signature: canonical query string -> string-to-sign -> HMAC-SHA1 -> base64.
func (p *aliyunProvider) sign(params map[string]string) string {
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
	mac := hmac.New(sha1.New, []byte(p.accessKeySecret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// aliyunNonce generates a v4 UUID as SignatureNonce. Aliyun only needs it unique
// (anti-replay); the standard library suffices — not worth a dependency.
func aliyunNonce() string {
	var b [16]byte
	// crypto/rand.Read never fails; it panics on its own
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

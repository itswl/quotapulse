package provider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Volcengine reports the account's available balance (CNY).
// Signing is Volcengine V4 (HMAC-SHA256), isomorphic to AWS SigV4:
// canonicalize the request, then sign with a key derived through the
// date/region/service/request ladder.
const (
	volcService     = "billing"
	volcAction      = "QueryBalanceAcct"
	volcVersion     = "2022-01-01"
	volcRegion      = "cn-shanghai"
	volcHost        = "open.volcengineapi.com"
	volcContentType = "application/json"
	volcPath        = "/"
	volcMethod      = http.MethodGet
	volcBaseURL     = "https://" + volcHost
)

// volcSignedHeaders lists the headers that participate in the signature, in
// signing order. The canonical request's header list and the Authorization's
// SignedHeaders must match — hence this single definition.
var volcSignedHeaders = []string{"content-type", "host", "x-content-sha256", "x-date"}

type volcProvider struct {
	ak, sk  string
	client  *Client
	baseURL string
	// now is pulled out separately so tests can pin time — signatures change with it and would be unassertable otherwise
	now func() time.Time
}

func init() {
	RegisterWithCurrency("volc", "Volcengine", "balance", "CNY", func(apiKey string, client *Client) (Provider, error) {
		ak, sk, err := SplitKeyPair(apiKey, "Volcengine", "AK:SK")
		if err != nil {
			return nil, err
		}
		return &volcProvider{
			ak: ak, sk: sk, client: client,
			baseURL: volcBaseURL,
			now:     func() time.Time { return time.Now().UTC() },
		}, nil
	})
}

func (p *volcProvider) Fetch(ctx context.Context) (float64, error) {
	data, err := p.request(ctx)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, errors.New("API returned an empty response")
	}

	// Volcengine puts business errors in ResponseMetadata.Error while the HTTP status stays 200
	if info := Dig(data, "ResponseMetadata", "Error"); truthy(info) {
		return 0, fmt.Errorf("API returned an error: %s", formatValue(info))
	}

	balance, ok := Num(Dig(data, "Result", "AvailableBalance"))
	if !ok {
		return 0, errors.New("Could not parse AvailableBalance field")
	}
	return balance, nil
}

func (p *volcProvider) request(ctx context.Context) (map[string]any, error) {
	rawURL := p.baseURL + volcPath + "?" + volcNormQuery(p.query())
	req, err := http.NewRequestWithContext(ctx, volcMethod, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for name, value := range p.buildHeaders(p.now(), "") {
		if name == "Host" {
			// host participates in the signature, so the Host header must match what was
			// signed; Go forbids setting Host via Header, so write req.Host
			req.Host = value
			continue
		}
		req.Header.Set(name, value)
	}

	status, body, err := fetchBody(p.client, req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed, status code: %d\nresponse body: %s", status, body)
	}
	return parseJSONObject(body)
}

func (p *volcProvider) query() map[string]string {
	return map[string]string{"Action": volcAction, "Version": volcVersion}
}

// buildHeaders builds the signed headers. body is the raw request body, empty for GET queries.
// The Host stays pinned to the API host even when tests point the base URL
// elsewhere: Host participates in the signature, so the sent Host header must
// be the one signed, not the connection target.
func (p *volcProvider) buildHeaders(now time.Time, body string) map[string]string {
	return VolcV4Headers(VolcV4Request{
		Host: volcHost, Path: volcPath, Region: volcRegion, Service: volcService,
		AK: p.ak, SK: p.sk, Params: p.query(), Body: body,
	}, now)
}

// volcNormQuery normalizes query parameters: sorted keys, RFC 3986 encoding — the signature and the real request share the same string.
func volcNormQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]string, 0, len(keys))
	for _, key := range keys {
		items = append(items, percentEncode(key)+"="+percentEncode(params[key]))
	}
	return strings.Join(items, "&")
}

func volcSHA256(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func volcHMAC(key []byte, content string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(content))
	return mac.Sum(nil)
}

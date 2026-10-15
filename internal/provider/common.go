package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Value-access semantics shared by the adapters: truthiness, field access with
// defaults, and "is it a number". They decide which branch boundary data (0,
// empty string, null, numeric strings) takes; one shared copy beats every
// adapter writing — and breaking — its own.

// truthy decides whether a JSON value counts as "true": null / false / 0 / "" /
// empty containers are false, everything else true. Platforms type success and
// code fields inconsistently — the same meaning may arrive as true, 1 or a
// non-empty string — so v == true would misjudge success: 1. One truthiness
// check covers them all.
func truthy(v any) bool {
	switch value := v.(type) {
	case nil:
		return false
	case bool:
		return value
	case float64:
		return value != 0
	case int:
		return value != 0
	case json.Number:
		f, err := value.Float64()
		return err == nil && f != 0
	case string:
		return value != ""
	case []any:
		return len(value) > 0
	case map[string]any:
		return len(value) > 0
	}
	return true
}

// jsonNum accepts only real JSON numbers, never strings.
//
// This strictness is deliberate — do not "helpfully" loosen it into Num:
// GLM quota math and wxrank's code checks depend on it. Treating "0" or
// "50" strings as numbers yields balances that are wrong yet look normal, so
// nobody notices. Num's lenient parsing exists for fields where the platform
// genuinely sends numbers as strings; the two are not interchangeable.
func jsonNum(v any) (float64, bool) {
	switch value := v.(type) {
	case float64:
		return value, true
	case int:
		return float64(value), true
	case json.Number:
		f, err := value.Float64()
		return f, err == nil
	}
	return 0, false
}

// orElse takes the first truthy value: if a is falsy take b, even when b is
// falsy too. wxrank's fallback fields depend on this: a score of 0 must fall
// through to credits instead of settling on 0.
func orElse(a, b any) any {
	if truthy(a) {
		return a
	}
	return b
}

// messageOr reads one field as the error message: if the key exists use its value,
// even empty or null; only a missing key falls back to the default message.
func messageOr(data map[string]any, key, fallback string) string {
	value, ok := data[key]
	if !ok {
		return fallback
	}
	return formatValue(value)
}

// formatValue renders a JSON value as text for error messages. Numbers print as
// decimal (float64(200) as "200", not "2e+02"); objects and arrays become JSON —
// error messages are for humans and JSON beats Go's %v.
func formatValue(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case int:
		return strconv.Itoa(value)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // error messages are for humans; don't turn & < > into &amp; escapes
	if err := enc.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// round2 keeps two decimals: format as decimal and parse back, ties go to even
// (banker's rounding). The boundary cases are pinned in glm_test.go; read them
// before changing this.
func round2(v float64) float64 {
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 2, 64), 64)
	if err != nil {
		return v
	}
	return rounded
}

// percentEncode is RFC 3986 percent-encoding: only letters, digits and -_.~ pass
// through. Volcengine and Aliyun signatures both require it; the differences from
// url.QueryEscape are space (%20, not +) and ~ (not encoded). A signature must
// match byte for byte, so implement it here rather than trusting the standard
// library's trade-offs.
func percentEncode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// fetchBody issues the request and reads the body. Volcengine and Aliyun treat
// status codes differently (Volcengine fails on non-200; Aliyun returns business
// errors as 4xx + JSON), so this only fetches and each adapter judges the status
// itself.
func fetchBody(client *Client, req *http.Request) (int, string, error) {
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, "", fmt.Errorf("failed to read response: %w", err)
	}
	return resp.StatusCode, string(body), nil
}

// parseJSONObject parses the response body. Empty bodies and null both return an
// empty map instead of erroring: "the API returned an empty response" is a
// business-layer judgment and each adapter words and handles it differently — let
// the caller report it.
func parseJSONObject(body string) (map[string]any, error) {
	if strings.TrimSpace(body) == "" {
		return nil, nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return nil, fmt.Errorf("Response is not valid JSON:%s", body)
	}
	return data, nil
}

package provider

import "testing"

// These helpers decide which branch boundary data (0, empty string, null,
// numeric strings) takes, so they are pinned individually rather than covered
// incidentally by platform cases.

func TestTruthy(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want bool
	}{
		{"null", nil, false},
		{"false", false, false},
		{"true", true, true},
		{"数字 0", float64(0), false},
		{"数字 1", float64(1), true},
		{"空串", "", false},
		{"非空串", "x", true},
		{"空数组", []any{}, false},
		{"非空数组", []any{1}, true},
		{"空对象", map[string]any{}, false},
		{"非空对象", map[string]any{"a": 1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truthy(tc.in); got != tc.want {
				t.Errorf("truthy(%v) = %v，expected %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestJSONNumRejectsStrings(t *testing.T) {
	// the difference from Num is exactly this: numeric strings are not numbers
	if _, ok := jsonNum("12"); ok {
		t.Error(`jsonNum("12") 不该通过`)
	}
	if _, ok := jsonNum(true); ok {
		t.Error("jsonNum(true) 不该通过")
	}
	if got, ok := jsonNum(float64(12.5)); !ok || got != 12.5 {
		t.Errorf("jsonNum(12.5) = %v, %v", got, ok)
	}
}

func TestOrElse(t *testing.T) {
	// take the first truthy value: a falsy a takes b, even when b is falsy too
	if got := orElse(nil, float64(5)); got != float64(5) {
		t.Errorf("orElse(nil, 5) = %v", got)
	}
	if got := orElse(float64(0), float64(0)); got != float64(0) {
		t.Errorf("orElse(0, 0) = %v", got)
	}
	if got := orElse(float64(3), float64(9)); got != float64(3) {
		t.Errorf("orElse(3, 9) = %v", got)
	}
}

func TestMessageOr(t *testing.T) {
	data := map[string]any{"msg": "出错了", "empty": "", "code": float64(401)}
	if got := messageOr(data, "msg", "Unknown error"); got != "出错了" {
		t.Errorf("msg = %q", got)
	}
	// an existing key with an empty value uses the empty string; only a missing key falls back to the default
	if got := messageOr(data, "empty", "Unknown error"); got != "" {
		t.Errorf("empty = %q", got)
	}
	if got := messageOr(data, "missing", "Unknown error"); got != "Unknown error" {
		t.Errorf("missing = %q", got)
	}
	// numbers must print as 401, not 4.01e+02
	if got := messageOr(data, "code", "Unknown error"); got != "401" {
		t.Errorf("code = %q", got)
	}
}

func TestFormatValue(t *testing.T) {
	if got := formatValue(map[string]any{"Code": "A&B", "N": float64(1)}); got != `{"Code":"A&B","N":1}` {
		t.Errorf("对象 = %q，& 不该被转义成 \\u0026", got)
	}
	if got := formatValue(float64(1234.5)); got != "1234.5" {
		t.Errorf("小数 = %q", got)
	}
	if got := formatValue("原样"); got != "原样" {
		t.Errorf("字符串 = %q", got)
	}
}

func TestParseJSONObject(t *testing.T) {
	// empty body and null both count as an empty response; the caller reports "API returned an empty response"
	for _, body := range []string{"", "   ", "null"} {
		data, err := parseJSONObject(body)
		if err != nil || len(data) != 0 {
			t.Errorf("parseJSONObject(%q) = %v, %v", body, data, err)
		}
	}

	if _, err := parseJSONObject("<html>"); err == nil ||
		err.Error() != "Response is not valid JSON:<html>" {
		t.Errorf("error = %v", err)
	}

	data, err := parseJSONObject(`{"a":1}`)
	if err != nil || data["a"] != float64(1) {
		t.Errorf("= %v, %v", data, err)
	}
}

func TestGLMRemainingPercentIgnoresStringNumbers(t *testing.T) {
	// quota fields must be real numbers; the string "50" does not count.
	// Loosening this to Num would admit strings and produce a plausible-looking
	// wrong balance
	if _, ok := glmRemainingPercent(map[string]any{"usage": "100", "remaining": "50"}); ok {
		t.Error("字符串的 usage/remaining 不该被当成数字")
	}
	if _, ok := glmRemainingPercent(map[string]any{"percentage": "30"}); ok {
		t.Error("字符串的 percentage 不该被当成数字")
	}
}

func TestRound2HalfToEven(t *testing.T) {
	// round2's contract: exact ties go to even; the rest follow the true binary value
	cases := []struct{ in, want float64 }{
		{0.125, 0.12}, // 正中间，进偶数
		{0.135, 0.14}, // 二进制里略大于 0.135
		{2.675, 2.67}, // 二进制里略小于 2.675
		{0.005, 0.01}, // 二进制里略大于 0.005
		{98.7, 98.7},  // 本来就够短
		{66.666666, 66.67},
		{33.333333, 33.33},
	}
	for _, tc := range cases {
		if got := round2(tc.in); got != tc.want {
			t.Errorf("round2(%v) = %v，expected %v", tc.in, got, tc.want)
		}
	}
}

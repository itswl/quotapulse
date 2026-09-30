package monitor

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	cases := []struct{ in, key, want string }{
		{"HTTP 401: invalid key sk-or-v1-abcdef123456", "", "HTTP 401: invalid key sk-***"},
		{"Authorization: Bearer abc.def.ghi-jkl", "", "Authorization: Bearer ***"},
		{`upstream said {"api_key":"live_9f8e7d6c5b4a"}`, "", `upstream said {"api_key":"***"}`},
		{"access_key_secret=Zx8Yw7Vu6Ts5 rejected", "", "access_key_secret=*** rejected"},
		// The project's own key, and each half of an AK:SK pair, whatever the upstream echoes.
		{"key LTAI5tKq9 with secret H7pQ2mNv8xY0 denied", "LTAI5tKq9:H7pQ2mNv8xY0", "key *** with secret *** denied"},
		{"HTTP 503: Service Unavailable", "LTAI5tKq9:H7pQ2mNv8xY0", "HTTP 503: Service Unavailable"},
	}
	for _, c := range cases {
		if got := redactSecrets(c.in, c.key); got != c.want {
			t.Errorf("redactSecrets(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	long := redactSecrets("Response is not valid JSON:" + strings.Repeat("<html>", 200))
	if n := len([]rune(long)); n > maxErrorLength+1 || !strings.HasSuffix(long, "…") {
		t.Errorf("原始响应体应被截断到 %d 字符以内，实际 %d", maxErrorLength, n)
	}
}

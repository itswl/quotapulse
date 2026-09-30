package monitor

import (
	"regexp"
	"strings"
)

// maxErrorLength caps a check error; some adapters quote a raw upstream body, which
// belongs in the logs, not on the dashboard, in alerts or over MCP.
const maxErrorLength = 300

var secretPatterns = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`), "Bearer ***"},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}`), "sk-***"},
	{regexp.MustCompile(`(?i)((?:api[_-]?key|access[_-]?key(?:[_-]?(?:id|secret))?|secret(?:[_-]?key)?|token|password)["']?\s*[:=]\s*["']?)[^\s"',;&}]{6,}`), "${1}***"},
}

// redactSecrets keeps credentials out of a check error, which is shown on the dashboard,
// sent in alerts and served over MCP: the project's own key and each part of a two-part
// key (AK:SK), anything shaped like a bearer token or API key, and overly long bodies.
func redactSecrets(message string, secrets ...string) string {
	for _, secret := range secrets {
		for _, part := range append([]string{secret}, strings.FieldsFunc(secret, func(r rune) bool { return r == ':' || r == '|' })...) {
			if part = strings.TrimSpace(part); len(part) >= 6 {
				message = strings.ReplaceAll(message, part, "***")
			}
		}
	}
	for _, p := range secretPatterns {
		message = p.re.ReplaceAllString(message, p.with)
	}
	if runes := []rune(message); len(runes) > maxErrorLength {
		message = string(runes[:maxErrorLength]) + "…"
	}
	return message
}

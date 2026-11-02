package httpapi

import (
	"io/fs"
	"net/http"
	"strings"
	"testing"

	quotapulseUI "github.com/itswl/quotapulse/ui"
)

// Every response carries the hardening headers, and the CSP has no
// 'unsafe-inline' for scripts — index.html loads its theme bootstrap from
// /static/theme.js for exactly that reason.
func TestSecurityHeaders(t *testing.T) {
	_, handler := newServer(t, nil)

	for _, path := range []string{"/", "/static/app.js", "/live"} {
		rec := request(t, handler, http.MethodGet, path, "", false)
		csp := rec.Header().Get("Content-Security-Policy")
		if csp == "" {
			t.Fatalf("%s: 缺少 Content-Security-Policy", path)
		}
		if !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
			t.Fatalf("%s: script-src 应严格为 'self': %s", path, csp)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("%s: X-Content-Type-Options = %q", path, got)
		}
		if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Fatalf("%s: X-Frame-Options = %q", path, got)
		}
		if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Fatalf("%s: Referrer-Policy = %q", path, got)
		}
	}
}

// The strict CSP breaks any inline <script>, so pin the shipped index.html
// against them: the theme bootstrap must stay in its own theme.js file.
func TestIndexHTMLHasNoInlineScript(t *testing.T) {
	assets, err := quotapulseUI.Assets()
	if err != nil {
		t.Skipf("frontend assets are not built: %v", err)
	}
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"<script>", "<script>"} {
		if strings.Contains(string(index), marker) {
			t.Fatal("index.html 不应再有内联 <script>，主题引导已外置为 theme.js")
		}
	}
	if !strings.Contains(string(index), "/static/theme.js?v=") {
		t.Fatal("index.html 应加载 /static/theme.js")
	}
}

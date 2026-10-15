package config

import (
	"os"
	"strings"
	"testing"
)

// TestMain clears environment variables that would affect auto-discovery.
//
// This once bit us: a real key from the dev machine's .env leaked into a
// test, and the case actually called the upstream platform. Tests must start
// from a blank slate and see only what they Setenv themselves.
func TestMain(m *testing.M) {
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if shouldClear(name) {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}

func shouldClear(name string) bool {
	for _, suffix := range []string{"_API_KEY", "_THRESHOLD", "_OWNER_PROJECT"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	for _, prefix := range []string{"EMAIL_", "WEBHOOK_", "BALANCE_", "MAX_", "WEB_", "ENABLE_", "DATABASE_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

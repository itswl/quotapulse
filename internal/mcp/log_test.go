package mcp

import (
	"log/slog"
	"strings"
	"testing"
)

// Every request is a session of its own here, and the protocol library
// announces each one. An agent asking a question must not cost three lines
// of log.
func TestTheProtocolLibraryIsQuietUnlessDebugging(t *testing.T) {
	var out strings.Builder
	normal := protocolLogger(slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelInfo})))
	normal.Info("server session connected")
	normal.Warn("something worth knowing")
	if got := out.String(); strings.Contains(got, "connected") || !strings.Contains(got, "worth knowing") {
		t.Errorf("at info: %q", got)
	}
	out.Reset()
	debugging := protocolLogger(slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})))
	debugging.Info("server session connected")
	if !strings.Contains(out.String(), "connected") {
		t.Errorf("at debug the library's own words are hidden: %q", out.String())
	}
}

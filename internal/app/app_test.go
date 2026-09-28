package app

import (
	"io"
	"log/slog"
	"testing"

	"github.com/itswl/quotapulse/internal/config"
)

// TestNewWiresEveryDependency guards against the v0.1.24 regression: a struct field
// that exists on App/Server but never gets assigned compiles fine, passes CI, and only
// fails at runtime when the guard rejects the startup.
func TestNewWiresEveryDependency(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	appInstance, err := New(&config.Settings{AppVersion: "test"}, log, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if appInstance.Push == nil {
		t.Fatal("App.Push 没有接线")
	}
	if appInstance.Server == nil {
		t.Fatal("App.Server 没有构造")
	}
	if appInstance.Server.Push != appInstance.Push {
		t.Fatal("Server.Push 必须与 App.Push 是同一个管理器实例")
	}
	if appInstance.Monitor == nil || appInstance.Monitor.Push == nil {
		t.Fatal("Monitor.Push 没有接线")
	}
	if appInstance.Subs == nil || appInstance.Subs.Push == nil {
		t.Fatal("Subs.Push 没有接线")
	}
	if err := appInstance.Server.ValidateWiring(); err != nil {
		t.Fatalf("ValidateWiring: %v", err)
	}
}

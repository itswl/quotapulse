package config

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/store"
)

// The production-shaped upgrade: the database already holds a user-defined
// custom provider and no marker. The first load must seed the eight default
// platforms without touching the user's row — otherwise the dynamic registry
// replaces the init-time defaults and every adapter-era project breaks.
func TestResolverSeedsDefaultsAlongsideUserProviders(t *testing.T) {
	st, err := store.Open(context.Background(), store.Options{
		DatabaseURL: "sqlite:///" + filepath.Join(t.TempDir(), "resolver_test.db"),
	})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// a user-defined platform from the v0.1.40 era
	if err := st.UpsertCustomProvider(context.Background(), model.CustomProvider{
		Name: "glm-custom", URL: "https://example.test/balance", AuthMode: "bearer",
		DefaultType: model.TypeQuota, BalancePath: "data.x",
	}); err != nil {
		t.Fatal(err)
	}

	settings := &Settings{EnableDynamicConfig: true}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	resolver := NewResolver(settings, st, log)
	ctx := context.Background()
	_ = resolver.Load(ctx)

	list, err := st.ListCustomProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, custom := range list {
		names[custom.Name] = true
	}
	if !names["glm-custom"] {
		t.Error("the user-defined provider must survive the seeding")
	}
	for _, want := range []string{"deepseek", "glm", "openrouter", "tikhub", "uniapi", "wxrank", "volc", "aliyun"} {
		if !names[want] {
			t.Errorf("default %s was not seeded; got %v", want, names)
		}
	}

	// the registry now resolves both eras: bare aliases and canonical keys
	if _, ok := provider.Lookup("deepseek"); !ok {
		t.Error("the bare alias must resolve after seeding")
	}
	if !provider.IsDynamic("custom:deepseek") {
		t.Error("the seeded definition must be registered")
	}

	// deleting a default sticks: the marker is set, so the next load seeds nothing
	if err := st.DeleteCustomProvider(ctx, "wxrank"); err != nil {
		t.Fatal(err)
	}
	_ = resolver.Load(ctx)
	if provider.IsDynamic("custom:wxrank") {
		t.Error("a deleted default must not come back once the marker is set")
	}
	if _, ok := provider.Lookup("custom:wxrank"); ok {
		t.Error("wxrank should be gone from the registry")
	}
}

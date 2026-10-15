package config

import (
	"context"
	"log/slog"
	"strings"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/store"
)

// Resolver rebuilds the business inventories on every call: database dynamic
// config plus environment auto-discovery.
//
// No caching. Config edits on the page must take effect immediately, and these
// tables hold at most a few dozen rows — one read is negligible.
type Resolver struct {
	settings *Settings
	store    store.Store
	log      *slog.Logger
}

// NewResolver creates an inventory resolver. store may be store.Null().
func NewResolver(settings *Settings, st store.Store, log *slog.Logger) *Resolver {
	if log == nil {
		log = slog.Default()
	}
	return &Resolver{settings: settings, store: st, log: log}
}

// Load returns the three business inventories with all fields filled in.
//
// A database read failure is not fatal: fall back to "environment variables
// only", keep serving, and log a warning.
func (r *Resolver) Load(ctx context.Context) model.Config {
	cfg := model.Config{}
	if r.settings.EnableDynamicConfig {
		cfg = r.fromDatabase(ctx)
	}

	cfg.Projects = append(cfg.Projects, DiscoverProjects(cfg.Projects)...)
	cfg.Mailboxes = append(cfg.Mailboxes, DiscoverMailboxes(cfg.Mailboxes)...)
	r.normalize(&cfg)
	r.registerCustomProviders(ctx)
	return cfg
}

// CustomProviders lists the page-defined platforms from the database.
func (r *Resolver) CustomProviders(ctx context.Context) []model.CustomProvider {
	if !r.settings.EnableDynamicConfig {
		return nil
	}
	list, err := r.store.ListCustomProviders(ctx)
	if err != nil {
		r.log.Warn("Failed to read custom provider configuration", "error", err)
		return nil
	}
	return list
}

// registerCustomProviders swaps the dynamic provider registry. Called on
// every Load so page edits apply to the next check round without a restart.
// defaultsSeededMarker keys the one-time seeding of the retired built-in
// platforms into the custom provider table.
const defaultsSeededMarker = "custom_defaults_seeded"

func (r *Resolver) registerCustomProviders(ctx context.Context) {
	// Without dynamic configuration the database holds no definitions; the
	// defaults registered at package init stay in place untouched.
	if !r.settings.EnableDynamicConfig {
		return
	}
	list := r.CustomProviders(ctx)
	// First start with dynamic configuration: persist the default platforms as
	// ordinary rows, once — only the missing ones, so definitions the user
	// already added under other names stay untouched. Later deletions stick:
	// the marker outlives them.
	if seeded, err := r.store.GetAppSetting(ctx, defaultsSeededMarker); err != nil || seeded != "1" {
		known := map[string]bool{}
		for _, custom := range list {
			known[custom.Name] = true
		}
		for _, custom := range provider.DefaultCustomProviders() {
			if known[custom.Name] {
				continue
			}
			if err := r.store.UpsertCustomProvider(ctx, custom); err != nil {
				r.log.Warn("Failed to seed a default custom provider", "name", custom.Name, "error", err)
			}
		}
		if err := r.store.SetAppSetting(ctx, defaultsSeededMarker, "1"); err != nil {
			r.log.Warn("Failed to record the defaults seeding; it will retry", "error", err)
		}
		list = r.CustomProviders(ctx)
	}
	specs := make([]provider.Spec, 0, len(list))
	for _, custom := range list {
		spec, err := provider.SpecFromCustomProvider(custom)
		if err != nil {
			r.log.Warn("Skipping invalid custom provider", "name", custom.Name, "error", err)
			continue
		}
		specs = append(specs, spec)
	}
	if err := provider.ReplaceDynamic(specs); err != nil {
		r.log.Error("Failed to register custom providers", "error", err)
	}
}

func (r *Resolver) fromDatabase(ctx context.Context) model.Config {
	var cfg model.Config
	var failed bool

	if projects, err := r.store.ListProjects(ctx); err != nil {
		failed = true
		r.log.Warn("Failed to read project configuration from the database; using environment variables only", "error", err)
	} else {
		cfg.Projects = projects
	}
	if subs, err := r.store.ListSubscriptions(ctx); err != nil {
		failed = true
		r.log.Warn("Failed to read subscription configuration from the database", "error", err)
	} else {
		cfg.Subscriptions = subs
	}
	if boxes, err := r.store.ListMailboxes(ctx); err != nil {
		failed = true
		r.log.Warn("Failed to read mailbox configuration from the database", "error", err)
	} else {
		cfg.Mailboxes = boxes
	}
	if failed && r.settings.StrictDatabaseErrors {
		r.log.Error("STRICT_DATABASE_ERRORS is on, and failing to read dynamic configuration leaves the configuration incomplete")
	}
	return cfg
}

// normalize fills omitted fields so downstream code only sees complete records.
func (r *Resolver) normalize(cfg *model.Config) {
	ordinals := make(map[string]int)
	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		model.NormalizeProject(p)
		ordinals[p.Provider]++
		if key, _ := ResolveAPIKey(*p, ordinals[p.Provider]); key != "" {
			p.APIKey = key
		}
	}
	for i := range cfg.Subscriptions {
		s := &cfg.Subscriptions[i]
		if s.CycleType == "" {
			s.CycleType = model.CycleMonthly
		}
		s.CycleType = strings.ToLower(strings.TrimSpace(s.CycleType))
		if s.AlertDaysBefore < 0 {
			s.AlertDaysBefore = 0
		}
	}
	for i := range cfg.Mailboxes {
		m := &cfg.Mailboxes[i]
		if m.Port <= 0 {
			m.Port = 993
		}
		if m.Name == "" {
			m.Name = m.Username
		}
	}
}

// KeySource returns a key's source description and candidate variable names; used by the self-check.
func KeySource(p model.Project, ordinal int) (source string, candidates []string) {
	source, _ = "", ""
	if p.APIKey == "" {
		return "", ProviderKeyEnvNames(p.Provider, ordinal)
	}
	for _, name := range ProviderKeyEnvNames(p.Provider, ordinal) {
		if value, ok := raw(name); ok && value == p.APIKey {
			return "environment variable " + name, nil
		}
	}
	return "the api_key field in the configuration", nil
}

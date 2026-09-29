package config

import (
	"context"
	"log/slog"
	"strings"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/store"
)

// Implementation note.
//
// Implementation note.
type Resolver struct {
	settings *Settings
	store    store.Store
	log      *slog.Logger
}

// Implementation note.
func NewResolver(settings *Settings, st store.Store, log *slog.Logger) *Resolver {
	if log == nil {
		log = slog.Default()
	}
	return &Resolver{settings: settings, store: st, log: log}
}

// Implementation note.
//
// Implementation note.
func (r *Resolver) Load(ctx context.Context) model.Config {
	cfg := model.Config{}
	if r.settings.EnableDynamicConfig {
		cfg = r.fromDatabase(ctx)
	}

	cfg.Projects = append(cfg.Projects, DiscoverProjects(cfg.Projects)...)
	cfg.Mailboxes = append(cfg.Mailboxes, DiscoverMailboxes(cfg.Mailboxes)...)
	r.normalize(&cfg)
	return cfg
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

// Implementation note.
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

// Implementation note.
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

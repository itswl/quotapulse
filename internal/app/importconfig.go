package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/subscription"
)

// Implementation note.
//
// Implementation note.
// Implementation note.
type legacyConfig struct {
	Projects      []map[string]any `json:"projects"`
	Subscriptions []map[string]any `json:"subscriptions"`
	Email         []map[string]any `json:"email"`
}

// Implementation note.
func (a *App) ImportLegacyConfig(ctx context.Context, path string, out io.Writer) (int, error) {
	if !a.Settings.EnableDatabase || !a.Settings.EnableDynamicConfig {
		return 0, fmt.Errorf("Importing needs ENABLE_DATABASE=true and ENABLE_DYNAMIC_CONFIG=true")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("Failed to read %s: %w", path, err)
	}
	var cfg legacyConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return 0, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}

	total := 0
	for _, item := range cfg.Projects {
		project := legacyProject(item)
		if project.Provider == "" {
			fmt.Fprintf(out, "Skipped a project without a provider: %v\n", item["name"])
			continue
		}
		model.NormalizeProject(&project)
		if err := a.Store.UpsertProject(ctx, project); err != nil {
			return total, fmt.Errorf("Failed to import project %s: %w", project.Name, err)
		}
		fmt.Fprintf(out, "Imported project: %s\n", project.Name)
		total++
	}

	for _, item := range cfg.Subscriptions {
		sub := legacySubscription(item)
		if sub.Name == "" {
			continue
		}
		if err := a.Store.UpsertSubscription(ctx, sub); err != nil {
			return total, fmt.Errorf("Failed to import subscription %s: %w", sub.Name, err)
		}
		fmt.Fprintf(out, "Imported subscription: %s\n", sub.Name)
		total++
	}

	for _, item := range cfg.Email {
		mailbox := legacyMailbox(item)
		if mailbox.Host == "" || mailbox.Username == "" {
			continue
		}
		if err := a.Store.UpsertMailbox(ctx, mailbox); err != nil {
			return total, fmt.Errorf("Failed to import mailbox %s: %w", mailbox.Name, err)
		}
		fmt.Fprintf(out, "Imported mailbox: %s\n", mailbox.Name)
		total++
	}

	fmt.Fprintf(out, "Migration complete: %d items. Once they show up on the dashboard, %s can be deleted\n", total, path)
	return total, nil
}

func legacyProject(item map[string]any) model.Project {
	project := model.Project{
		Name:      text(item, "name"),
		Provider:  strings.ToLower(strings.TrimSpace(text(item, "provider"))),
		APIKey:    expand(text(item, "api_key")),
		Threshold: number(item, "threshold"),
		Type:      text(item, "type"),
		Enabled:   boolean(item, "enabled", true),
	}
	project.OwnerProject = model.OwnerProjectOf(text(item, "owner_project"))
	return project
}

func legacySubscription(item map[string]any) model.Subscription {
	cycleType := strings.ToLower(strings.TrimSpace(text(item, "cycle_type")))
	if cycleType == "" {
		cycleType = model.CycleMonthly
	}
	sub := model.Subscription{
		Name:            text(item, "name"),
		CycleType:       cycleType,
		RenewalDay:      legacyRenewalDay(item["renewal_day"], cycleType),
		AlertDaysBefore: int(numberOr(item, "alert_days_before", 3)),
		Amount:          number(item, "amount"),
		Enabled:         boolean(item, "enabled", true),
	}
	sub.OwnerProject = model.OwnerProjectOf(text(item, "owner_project"))
	if renewed := text(item, "last_renewed_date"); renewed != "" {
		sub.LastRenewedDate = &renewed
	}
	return sub
}

// Implementation note.
func legacyRenewalDay(value any, cycleType string) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case string:
		if day, ok := subscription.CoerceRenewalDay(v, cycleType); ok {
			return day
		}
	}
	return 1
}

func legacyMailbox(item map[string]any) model.Mailbox {
	mailbox := model.Mailbox{
		Name:     text(item, "name"),
		Host:     text(item, "host"),
		Port:     int(numberOr(item, "port", 993)),
		Username: text(item, "username"),
		Password: expand(text(item, "password")),
		UseSSL:   boolean(item, "use_ssl", true),
		Enabled:  boolean(item, "enabled", true),
	}
	if mailbox.Name == "" {
		mailbox.Name = mailbox.Username
	}
	return mailbox
}

// Implementation note.
func expand(value string) string {
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		return os.Getenv(value[2 : len(value)-1])
	}
	return value
}

func text(item map[string]any, key string) string {
	switch v := item[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func number(item map[string]any, key string) float64 { return numberOr(item, key, 0) }

func numberOr(item map[string]any, key string, fallback float64) float64 {
	switch v := item[key].(type) {
	case float64:
		return v
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func boolean(item map[string]any, key string, fallback bool) bool {
	if v, ok := item[key].(bool); ok {
		return v
	}
	return fallback
}

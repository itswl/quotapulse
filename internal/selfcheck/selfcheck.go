// Package selfcheck provides the package implementation.
//
//	quotapulse -show-config
//
// Implementation note.
package selfcheck

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/itswl/quotapulse/internal/app"
	"github.com/itswl/quotapulse/internal/config"
	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/timeutil"
)

// Implementation note.
const (
	markOK   = "✓"
	markWarn = "!"
	markBad  = "✗"
)

// Implementation note.
func Run(ctx context.Context, instance *app.App, out io.Writer) int {
	settings := instance.Settings
	cfg := instance.Resolver.Load(ctx)

	var lines []string
	problems := 0

	lines = append(lines, "Configuration self-check", "  Database: "+databaseLabel(settings))
	lines = append(lines, "  Config sources: "+sources(cfg, settings))
	lines = append(lines, "  Optional features: "+features(settings))
	lines = append(lines, "  Scheduled jobs: "+schedules(settings))
	if !settings.EnableDatabase {
		lines = append(lines, "  "+markWarn+" Spend analysis and runway estimates need ENABLE_DATABASE=true to build up history; it is off")
	}
	if settings.EnableSubscriptions && !settings.EnableDynamicConfig {
		lines = append(lines, "  "+markWarn+" Subscription reminders are on, but subscriptions can only be stored in the database; also set ENABLE_DYNAMIC_CONFIG=true")
		problems++
	}

	projectLines, projectProblems := checkProjects(cfg.Projects)
	lines = append(lines, projectLines...)
	problems += projectProblems

	subLines, subProblems := checkSubscriptions(cfg.Subscriptions)
	lines = append(lines, subLines...)
	problems += subProblems

	mailLines, mailProblems := checkMailboxes(cfg.Mailboxes)
	lines = append(lines, mailLines...)
	problems += mailProblems

	channelLines, channelProblems := checkChannel(settings)
	lines = append(lines, channelLines...)
	problems += channelProblems

	if problems > 0 {
		lines = append(lines, "", fmt.Sprintf("Found %d problem(s) (the entries marked %s / %s above)", problems, markBad, markWarn))
	} else {
		lines = append(lines, "", "The configuration looks fine")
	}

	fmt.Fprintln(out, strings.Join(lines, "\n"))
	return problems
}

func databaseLabel(settings *config.Settings) string {
	if !settings.EnableDatabase {
		return "Disabled"
	}
	scheme, _, _ := strings.Cut(settings.DatabaseURL, "://")
	return scheme
}

// Implementation note.
func sources(cfg model.Config, settings *config.Settings) string {
	label := func(total, fromEnv int) string {
		if total == 0 {
			return "(empty)"
		}
		var parts []string
		if total-fromEnv > 0 {
			if settings.EnableDynamicConfig {
				parts = append(parts, "database")
			} else {
				parts = append(parts, "unknown source")
			}
		}
		if fromEnv > 0 {
			parts = append(parts, "environment variable")
		}
		return strings.Join(parts, " + ")
	}

	projectsFromEnv := 0
	for _, p := range cfg.Projects {
		if p.FromEnv {
			projectsFromEnv++
		}
	}
	mailboxesFromEnv := 0
	for _, m := range cfg.Mailboxes {
		if m.FromEnv {
			mailboxesFromEnv++
		}
	}
	return fmt.Sprintf("projects=%s  subscriptions=%s  mailboxes=%s",
		label(len(cfg.Projects), projectsFromEnv),
		label(len(cfg.Subscriptions), 0),
		label(len(cfg.Mailboxes), mailboxesFromEnv))
}

func features(settings *config.Settings) string {
	toggles := []struct {
		name string
		on   bool
	}{
		{"Database", settings.EnableDatabase},
		{"Dynamic configuration", settings.EnableDynamicConfig},
		{"History API", settings.EnableHistoryAPI},
		{"Subscription reminders", settings.EnableSubscriptions},
		{"Prometheus", settings.EnablePrometheus},
		{"Web alerts", settings.EnableWebAlarm},
	}
	parts := make([]string, 0, len(toggles))
	for _, t := range toggles {
		mark := markBad
		if t.on {
			mark = markOK
		}
		parts = append(parts, t.name+mark)
	}
	return strings.Join(parts, "  ")
}

func schedules(settings *config.Settings) string {
	return fmt.Sprintf("dashboard refresh every %d s  alert check %s  mailbox scan %s (last %s)  weekly report %s",
		settings.RefreshInterval(),
		timeutil.Describe(settings.AlertTimes, nil),
		timeutil.Describe(settings.EmailScanTimes, nil),
		model.Quantity(settings.EmailScanDays, "day", "days"),
		timeutil.Describe(settings.WeeklyReportTimes, settings.WeeklyReportWeekdays))
}

func checkProjects(projects []model.Project) ([]string, int) {
	lines := []string{"", fmt.Sprintf("Projects (%d)", len(projects))}
	if len(projects) == 0 {
		return append(lines, "  "+markBad+" No projects, so no balance checks will run"), 1
	}

	problems := 0
	ordinals := make(map[string]int)
	for _, p := range projects {
		if p.Provider == "" {
			lines = append(lines, "  "+markBad+" "+displayName(p.Name)+": the provider field is missing")
			problems++
			continue
		}
		if _, known := provider.Lookup(p.Provider); !known {
			lines = append(lines, fmt.Sprintf("  %s %s: Unknown provider %q; supported: %s",
				markBad, displayName(p.Name), p.Provider, strings.Join(provider.Keys(), " ")))
			problems++
			continue
		}

		ordinals[p.Provider]++
		source, candidates := config.KeySource(p, ordinals[p.Provider])
		if source == "" {
			hint := "?"
			if len(candidates) > 0 {
				hint = strings.Join(candidates, " or ")
			}
			lines = append(lines, fmt.Sprintf("  %s %s [%s]: Missing API key; set  %s",
				markBad, displayName(p.Name), p.Provider, hint))
			problems++
			continue
		}

		origin := ""
		if p.FromEnv {
			origin = " (auto-discovered from environment variables)"
		}
		detail := fmt.Sprintf("%s [%s/%s] threshold %g — key from %s%s",
			displayName(p.Name), p.Provider, p.Type, p.Threshold, source, origin)
		if p.Threshold == 0 {
			hint := "a threshold on the dashboard"
			if p.FromEnv {
				hint = strings.ToUpper(p.Provider) + "_THRESHOLD"
			}
			lines = append(lines, fmt.Sprintf("  %s %s (threshold 0 never alerts; set %s)", markWarn, detail, hint))
			problems++
			continue
		}
		lines = append(lines, "  "+markOK+" "+detail)
	}
	return lines, problems
}

func checkSubscriptions(subs []model.Subscription) ([]string, int) {
	lines := []string{"", fmt.Sprintf("Subscriptions (%d)", len(subs))}
	if len(subs) == 0 {
		return append(lines, "  — none configured"), 0
	}

	problems := 0
	for _, s := range subs {
		readable := notify.FormatSubscriptionCycle(s.CycleType, s.RenewalDay)
		if readable == "Unknown cycle" {
			lines = append(lines, fmt.Sprintf("  %s %s: cycle type %q is not supported (weekly/monthly/yearly/lunar_yearly)",
				markBad, displayName(s.Name), s.CycleType))
			problems++
			continue
		}
		lines = append(lines, fmt.Sprintf("  %s %s: %s, reminds %s ahead, amount %g",
			markOK, displayName(s.Name), readable, model.Quantity(s.AlertDaysBefore, "day", "days"), s.Amount))
	}
	return lines, problems
}

func checkMailboxes(mailboxes []model.Mailbox) ([]string, int) {
	lines := []string{"", fmt.Sprintf("Mailboxes (%d)", len(mailboxes))}
	if len(mailboxes) == 0 {
		return append(lines, "  — none configured"), 0
	}

	problems := 0
	for _, m := range mailboxes {
		var missing []string
		if m.Host == "" {
			missing = append(missing, "host")
		}
		if m.Username == "" {
			missing = append(missing, "username")
		}
		if m.Password == "" {
			missing = append(missing, "password")
		}
		if len(missing) > 0 {
			lines = append(lines, fmt.Sprintf("  %s %s: missing %s",
				markBad, displayName(m.Name), strings.Join(missing, ", ")))
			problems++
			continue
		}
		transport := "plaintext"
		if m.UseSSL {
			transport = "SSL"
		}
		lines = append(lines, fmt.Sprintf("  %s %s: %s:%d %s, account %s",
			markOK, displayName(m.Name), m.Host, m.Port, transport, m.Username))
	}
	return lines, problems
}

func checkChannel(settings *config.Settings) ([]string, int) {
	lines := []string{"", "Alerts and access"}
	problems := 0

	if settings.WebhookURL == "" {
		lines = append(lines, "  "+markBad+" WEBHOOK_URL is not set, so low balances cannot be alerted")
		problems++
	} else {
		webhookType := settings.WebhookType
		if webhookType == "" {
			webhookType = "custom"
		}
		if _, err := notify.New(settings.WebhookURL, webhookType, settings.WebhookSource, nil); err != nil {
			lines = append(lines, fmt.Sprintf("  %s Webhook [%s]: %s (options: %s)",
				markBad, webhookType, err, strings.Join(notify.SupportedTypes(), "/")))
			problems++
		} else {
			lines = append(lines, fmt.Sprintf("  %s Webhook [%s] → %s",
				markOK, webhookType, notify.MaskURL(settings.WebhookURL)))
		}
	}

	if settings.WebAPIKey == "" {
		lines = append(lines, "  "+markBad+" WEB_API_KEY is not set, so every /api/* request returns 503")
		problems++
	} else {
		lines = append(lines, fmt.Sprintf("  %s WEB_API_KEY is set (%s)", markOK, mask(settings.WebAPIKey)))
	}

	if settings.EnableDynamicConfig && settings.ConfigEncryptionKey == "" {
		lines = append(lines, "  "+markWarn+" Database dynamic configuration is on but CONFIG_ENCRYPTION_KEY is not set, so keys are stored in plain text")
	}
	return lines, problems
}

func displayName(name string) string {
	if name == "" {
		return "(unnamed)"
	}
	return name
}

func mask(value string) string {
	if len(value) <= 4 {
		return "***"
	}
	return value[:4] + "***"
}

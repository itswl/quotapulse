package runway

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/store"
)

// Implementation note.
//
// Implementation note.
type Alerter struct {
	Store    store.Store
	Notifier notify.Notifier
	Log      *slog.Logger

	// Implementation note.
	RunwayAlertDays float64
	// Implementation note.
	SpikeRatio float64
	// Implementation note.
	SpikeMinAmount float64
	// Implementation note.
	Cooldown time.Duration
}

// Implementation note.
type Sent struct {
	Runway int
	Spike  int
}

// Implementation note.
type notice struct {
	alertType string
	kind      string
	title     string
	lines     []string
	message   string
	value     *float64
	threshold *float64
}

// Implementation note.
func (a *Alerter) Check(ctx context.Context, results []model.CheckResult, dryRun bool) Sent {
	var sent Sent
	if a.RunwayAlertDays <= 0 && a.SpikeRatio <= 0 {
		return sent
	}

	for i := range results {
		result := &results[i]
		if !result.Success || !result.Runway.Alertable() {
			continue
		}
		projectID := model.ProjectID(result.Provider, result.Project)

		if a.runwayDue(result) {
			a.log().Warn("Short runway", "project", result.Project,
				"runway_days", *result.Runway.RunwayDays, "limit", a.RunwayAlertDays)
			if !dryRun && a.emit(ctx, projectID, result.Project, a.runwayNotice(result)) {
				sent.Runway++
			}
		}
		if a.spikeDue(result.Runway) {
			a.log().Warn("Spending spike", "project", result.Project, "ratio", *result.Runway.SpikeRatio)
			if !dryRun && a.emit(ctx, projectID, result.Project, a.spikeNotice(result)) {
				sent.Spike++
			}
		}
	}
	return sent
}

// Implementation note.
func (a *Alerter) runwayDue(result *model.CheckResult) bool {
	return a.RunwayAlertDays > 0 && result.Runway.RunwayDays != nil &&
		*result.Runway.RunwayDays <= a.RunwayAlertDays && !result.NeedAlarm
}

// Implementation note.
func (a *Alerter) spikeDue(r *model.Runway) bool {
	if a.SpikeRatio <= 0 || r.SpikeRatio == nil || *r.SpikeRatio < a.SpikeRatio {
		return false
	}
	today := 0.0
	if r.TodayConsumed != nil {
		today = *r.TodayConsumed
	}
	return today >= a.SpikeMinAmount
}

// Implementation note.
func (a *Alerter) emit(ctx context.Context, projectID, projectName string, n notice) bool {
	cooling, err := a.Store.HasRecentAlert(ctx, projectID, n.alertType, a.Cooldown)
	if err != nil {
		a.log().Warn("Failed to query alert cooldown; treating it as not cooling down", "error", err)
	}
	if cooling {
		return false
	}
	if a.Notifier == nil {
		a.log().Error("Webhook URL is not configured")
		return false
	}
	if err := a.Notifier.Send(ctx, notify.Message{Title: n.title, Lines: n.lines, Kind: n.kind}); err != nil {
		a.log().Error("Failed to send trend alert", "project", projectName, "kind", n.kind, "error", err)
		return false
	}
	if err := a.Store.SaveAlert(ctx, store.AlertRecord{
		AlertID: projectID, Name: projectName, AlertType: n.alertType,
		Message: n.message, Value: n.value, Threshold: n.threshold,
	}); err != nil {
		a.log().Warn("Failed to record alert", "project", projectName, "error", err)
	}
	return true
}

func (a *Alerter) runwayNotice(result *model.CheckResult) notice {
	r := result.Runway
	return notice{
		alertType: "low_runway",
		kind:      "runway",
		title:     "Short runway: " + result.Project,
		lines: append(head(result),
			"**Provider**: "+result.Provider,
			"**Current balance**: "+thousands(deref(r.CurrentBalance), 2),
			fmt.Sprintf("**Average daily spend**: %s (last %d days)", thousands(deref(r.BurnPerDay), 2), r.WindowDays),
			fmt.Sprintf("**Estimated depletion**: %s, remaining %.1f days (threshold %s days)",
				deref(r.DepletionDate), deref(r.RunwayDays), trimFloat(a.RunwayAlertDays)),
		),
		message:   fmt.Sprintf("Short runway: %.1f days, estimated depletion on %s", deref(r.RunwayDays), deref(r.DepletionDate)),
		value:     r.CurrentBalance,
		threshold: model.Ptr(a.RunwayAlertDays),
	}
}

func (a *Alerter) spikeNotice(result *model.CheckResult) notice {
	r := result.Runway
	lines := append(head(result),
		"**Today's spending**: "+thousands(deref(r.TodayConsumed), 2),
		fmt.Sprintf("**Baseline spending**: %s (median over the last %d days)", thousands(deref(r.BaselineSpend), 2), r.WindowDays),
		fmt.Sprintf("**Spike multiplier**: %.1fx", deref(r.SpikeRatio)),
		"**Current balance**: "+thousands(deref(r.CurrentBalance), 2),
	)
	if r.RunwayDays != nil {
		lines = append(lines, fmt.Sprintf("**At the current rate**: %.1f days remaining", *r.RunwayDays))
	}
	return notice{
		alertType: "spend_spike",
		kind:      "spend_spike",
		title:     "Spending spike: " + result.Project,
		lines:     lines,
		message: fmt.Sprintf("Spending spike: today %s is %.1fx the baseline",
			thousands(deref(r.TodayConsumed), 2), deref(r.SpikeRatio)),
		value:     r.TodayConsumed,
		threshold: r.BaselineSpend,
	}
}

func head(result *model.CheckResult) []string {
	lines := []string{"**Account**: " + result.Project}
	if result.OwnerProject != nil && *result.OwnerProject != "" {
		lines = append(lines, "**Owner project**: "+*result.OwnerProject)
	}
	return lines
}

func (a *Alerter) log() *slog.Logger {
	if a.Log != nil {
		return a.Log
	}
	return slog.Default()
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// Implementation note.
func thousands(value float64, decimals int) string {
	text := strconv.FormatFloat(value, 'f', decimals, 64)
	sign := ""
	if strings.HasPrefix(text, "-") {
		sign, text = "-", text[1:]
	}
	intPart, frac, _ := strings.Cut(text, ".")

	var grouped strings.Builder
	for i, digit := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	if frac == "" {
		return sign + grouped.String()
	}
	return sign + grouped.String() + "." + frac
}

// Implementation note.
func trimFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

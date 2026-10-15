package cost

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/notify"
	"github.com/itswl/quotapulse/internal/store"
)

// BudgetStore is the slice of persistence the budget alerts need; store.Store
// satisfies it, and so does any test fake.
type BudgetStore interface {
	BalanceSeries(ctx context.Context, days int) ([]model.BalancePoint, error)
	HasRecentAlert(ctx context.Context, projectID, alertType string, within time.Duration) (bool, error)
	SaveAlert(ctx context.Context, rec store.AlertRecord) error
}

// Alerter sends one budget-pace alert per project per cooldown window: the
// month-to-date spend, extrapolated to the month's end, has passed the
// project's budget. A budget of 0 (the default) never alerts.
type Alerter struct {
	Store    BudgetStore
	Notifier notify.Notifier
	Log      *slog.Logger
	// CooldownSeconds gates repeat alerts per project.
	CooldownSeconds int
	// Now is swappable for tests.
	Now func() time.Time
}

// Check walks the projects and alerts on budget overruns. It returns how many
// alerts were sent. dry-run judges without sending.
func (a *Alerter) Check(ctx context.Context, projects []model.Project, currencyOf func(model.Project) string, dryRun bool) int {
	var candidates []model.Project
	for _, p := range projects {
		if p.MonthlyBudget > 0 {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 || a.Store == nil {
		return 0
	}
	now := a.now()
	monthStart, daysInMonth := MonthWindow(now)
	daysElapsed := now.Day()

	series, err := a.Store.BalanceSeries(ctx, daysInMonth+1)
	if err != nil {
		a.log().Warn("Failed to read balance history; skipping budget checks", "error", err)
		return 0
	}
	byProject := map[string][]model.BalancePoint{}
	for _, point := range series {
		byProject[point.ProjectName] = append(byProject[point.ProjectName], point)
	}

	sent := 0
	for _, project := range candidates {
		spend := SpendInMonth(byProject[project.Name], monthStart, now)
		projected := Projected(spend, daysElapsed, daysInMonth)
		if projected <= project.MonthlyBudget {
			continue
		}
		id := project.ID()
		cooling, err := a.Store.HasRecentAlert(ctx, id, model.AlertBudget, time.Duration(a.CooldownSeconds)*time.Second)
		if err == nil && cooling {
			continue
		}
		lines := []string{
			fmt.Sprintf("Spent so far: %.2f %s", spend, currencyOf(project)),
			fmt.Sprintf("Projected by month end: %.2f %s", projected, currencyOf(project)),
			fmt.Sprintf("Budget: %.2f %s", project.MonthlyBudget, currencyOf(project)),
			fmt.Sprintf("Month is %d of %d days in", daysElapsed, daysInMonth),
		}
		if dryRun {
			sent++
			continue
		}
		msg := notify.Custom("Budget overrun: "+project.Name, lines, notify.KindBudget)
		sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err = a.Notifier.Send(sendCtx, msg)
		cancel()
		if err != nil {
			a.log().Error("Failed to send budget alert", "project", project.Name, "error", err)
			continue
		}
		if err := a.Store.SaveAlert(ctx, store.AlertRecord{
			AlertID: id, Name: project.Name, AlertType: model.AlertBudget, Status: "sent",
			Message: fmt.Sprintf("Budget overrun: projected %.2f over %.2f", projected, project.MonthlyBudget),
		}); err != nil {
			a.log().Warn("Failed to record budget alert", "project", project.Name, "error", err)
		}
		a.log().Warn("Budget alert sent", "project", project.Name, "projected", projected, "budget", project.MonthlyBudget)
		sent++
	}
	return sent
}

func (a *Alerter) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Alerter) log() *slog.Logger {
	if a.Log != nil {
		return a.Log
	}
	return slog.Default()
}

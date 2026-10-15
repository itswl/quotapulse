// Package cost turns balance history into a per-month spend picture: what
// each project consumed this calendar month, converted into one base
// currency, compared against per-project budgets and extrapolated to the
// month's end.
//
// Consumption follows runway's convention: between adjacent snapshots a drop
// is consumption and a rise is a top-up, so top-ups never count as negative
// spend. Days split in the process-local timezone — "this month" is what the
// user sees on the calendar, not UTC.
package cost

import (
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

// ProjectSpend is one project's month-to-date picture.
type ProjectSpend struct {
	Project    string  `json:"project"`
	Provider   string  `json:"provider"`
	Currency   string  `json:"currency"` // empty = quota percent, excluded from monetary totals
	Spend      float64 `json:"spend"`    // native currency
	SpendBase  float64 `json:"spend_base"`
	Budget     float64 `json:"budget,omitempty"` // native currency; 0 = none set
	Projected  float64 `json:"projected"`        // month-end extrapolation, native currency
	OverBudget bool    `json:"over_budget"`      // projection exceeds the budget
}

// Summary aggregates a whole month across projects.
type Summary struct {
	Month        string         `json:"month"` // YYYY-MM
	BaseCurrency string         `json:"base_currency"`
	DaysElapsed  int            `json:"days_elapsed"`
	DaysInMonth  int            `json:"days_in_month"`
	Projects     []ProjectSpend `json:"projects"`
	TotalBase    float64        `json:"total_base"`
	UnknownFX    []string       `json:"unknown_fx,omitempty"` // currencies seen without a rate
}

// MonthWindow returns the first instant of now's month and the month's length
// in days, in the local timezone.
func MonthWindow(now time.Time) (time.Time, int) {
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	return first, first.AddDate(0, 1, 0).Add(-time.Nanosecond).Day()
}

// SpendInMonth sums consumption between the snapshots that fall inside the
// month window. A balance falling from before the month still counts: the
// first in-month snapshot is preceded by the last out-of-month one, so usage
// across the boundary is not lost.
func SpendInMonth(points []model.BalancePoint, monthStart, now time.Time) float64 {
	var spend float64
	var prev *model.BalancePoint
	for _, p := range points {
		at := time.Unix(p.Timestamp, 0)
		inWindow := !at.Before(monthStart) && at.Before(now.Add(time.Second))
		if prev != nil && inWindow {
			if delta := prev.Balance - p.Balance; delta > 0 {
				spend += delta
			}
		}
		if !at.Before(monthStart) || prev == nil || at.Before(monthStart) {
			prev = &p
		}
	}
	return spend
}

// Projected linearly extrapolates month-to-date spend to the month's end.
// Day one projects nothing: one day of data says nothing about the trend.
func Projected(spend float64, daysElapsed, daysInMonth int) float64 {
	if daysElapsed <= 1 || daysElapsed >= daysInMonth {
		return spend
	}
	return spend / float64(daysElapsed) * float64(daysInMonth)
}

// Build computes the summary. currencyOf maps a project to its native
// currency (empty for quota types); fx holds "1 unit of X = N base".
func Build(points []model.BalancePoint, projects []model.Project, currencyOf func(model.Project) string, fx map[string]float64, base string, now time.Time) Summary {
	monthStart, daysInMonth := MonthWindow(now)
	daysElapsed := now.Day()

	byProject := map[string][]model.BalancePoint{}
	for _, p := range points {
		byProject[p.ProjectName] = append(byProject[p.ProjectName], p)
	}

	summary := Summary{
		Month:        monthStart.Format("2006-01"),
		BaseCurrency: base,
		DaysElapsed:  daysElapsed,
		DaysInMonth:  daysInMonth,
		Projects:     []ProjectSpend{},
	}
	unknown := map[string]bool{}
	for _, project := range projects {
		currency := currencyOf(project)
		spend := SpendInMonth(byProject[project.Name], monthStart, now)
		entry := ProjectSpend{
			Project:   project.Name,
			Provider:  project.Provider,
			Currency:  currency,
			Spend:     round2(spend),
			Projected: round2(Projected(spend, daysElapsed, daysInMonth)),
		}
		if project.MonthlyBudget > 0 {
			entry.Budget = project.MonthlyBudget
			entry.OverBudget = entry.Projected > project.MonthlyBudget
		}
		if currency != "" {
			if rate, ok := fx[currency]; ok {
				entry.SpendBase = round2(spend * rate)
				summary.TotalBase += entry.SpendBase
			} else {
				unknown[currency] = true
			}
		}
		summary.Projects = append(summary.Projects, entry)
	}
	summary.TotalBase = round2(summary.TotalBase)
	for currency := range unknown {
		summary.UnknownFX = append(summary.UnknownFX, currency)
	}
	sortStrings(summary.UnknownFX)
	return summary
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

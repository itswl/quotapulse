package cost

import (
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

func at(day, hour int) int64 {
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	return base.AddDate(0, 0, day-1).Add(time.Duration(hour) * time.Hour).Unix()
}

// A drop is consumption, a rise is a top-up: only drops count.
func TestSpendInMonthCountsOnlyDrops(t *testing.T) {
	monthStart := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.Local)
	points := []model.BalancePoint{
		{Balance: 500, Timestamp: at(1, 0)},  // month opens at 500
		{Balance: 450, Timestamp: at(2, 0)},  // -50
		{Balance: 600, Timestamp: at(3, 0)},  // +150 top-up: ignored
		{Balance: 520, Timestamp: at(10, 0)}, // -80
		{Balance: 500, Timestamp: at(14, 0)}, // -20
	}
	if got := SpendInMonth(points, monthStart, now); got != 150 {
		t.Errorf("SpendInMonth = %v, want 150", got)
	}
}

// Spending that happened between the last pre-month snapshot and the first
// in-month snapshot still belongs to the month.
func TestSpendAcrossMonthBoundary(t *testing.T) {
	monthStart := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.Local)
	points := []model.BalancePoint{
		{Balance: 400, Timestamp: time.Date(2026, 9, 30, 20, 0, 0, 0, time.Local).Unix()},
		{Balance: 380, Timestamp: at(1, 6)}, // -20 across midnight
		{Balance: 360, Timestamp: at(2, 6)}, // -20
	}
	if got := SpendInMonth(points, monthStart, now); got != 40 {
		t.Errorf("SpendInMonth = %v, want 40", got)
	}
}

// One day of data must not be extrapolated; the last day is already the end.
func TestProjectionNeedsATrend(t *testing.T) {
	if got := Projected(100, 1, 31); got != 100 {
		t.Errorf("day one projects nothing, got %v", got)
	}
	if got := Projected(100, 31, 31); got != 100 {
		t.Errorf("the last day is final, got %v", got)
	}
	if got := Projected(100, 10, 31); got != 310 {
		t.Errorf("10 days at 100 projects 310, got %v", got)
	}
}

func TestBuildConvertsAndFlags(t *testing.T) {
	local := time.Local
	monthStart := time.Date(2026, 10, 1, 0, 0, 0, 0, local)
	_ = monthStart
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, local)
	points := []model.BalancePoint{
		{ProjectName: "deepseek", Balance: 1000, Timestamp: monthStart.Unix()},
		{ProjectName: "deepseek", Balance: 800, Timestamp: monthStart.AddDate(0, 0, 15).Unix()}, // -200 CNY
		{ProjectName: "router", Balance: 50, Timestamp: monthStart.Unix()},
		{ProjectName: "router", Balance: 30, Timestamp: monthStart.AddDate(0, 0, 15).Unix()}, // -20 USD
		{ProjectName: "glm", Balance: 100, Timestamp: monthStart.Unix()},
		{ProjectName: "glm", Balance: 40, Timestamp: monthStart.AddDate(0, 0, 15).Unix()}, // quota, no currency
	}
	projects := []model.Project{
		{Name: "deepseek", Provider: "deepseek", MonthlyBudget: 300},
		{Name: "router", Provider: "openrouter", MonthlyBudget: 30}, // 20 USD spent, ~41.3 projected > 30 USD budget
		{Name: "glm", Provider: "glm"},
	}
	summary := Build(points, projects, func(p model.Project) string {
		switch p.Provider {
		case "deepseek":
			return "CNY"
		case "openrouter":
			return "USD"
		}
		return ""
	}, map[string]float64{"CNY": 1, "USD": 7}, "CNY", now)

	if summary.Month != "2026-10" || summary.DaysInMonth != 31 || summary.DaysElapsed != 16 {
		t.Fatalf("window wrong: %+v", summary)
	}
	if summary.TotalBase != 340 { // 200 CNY + 20*7 CNY
		t.Errorf("TotalBase = %v, want 340", summary.TotalBase)
	}
	byName := map[string]ProjectSpend{}
	for _, p := range summary.Projects {
		byName[p.Project] = p
	}
	if got := byName["router"]; !got.OverBudget {
		t.Errorf("router should be over budget: %+v", got)
	}
	if got := byName["glm"]; got.Currency != "" || got.SpendBase != 0 {
		t.Errorf("quota projects carry no currency: %+v", got)
	}
	if got := byName["deepseek"]; got.Projected != 387.5 || !got.OverBudget {
		t.Errorf("deepseek projection wrong: %+v", got)
	}
}

package mcpserver

// Spending for spend_summary. Like views.go, this file must not import internal
// packages.
//
// The rules mirror runway.Compute, so spend_summary agrees with the runway estimates: a
// decrease between consecutive snapshots is spending, booked on the later snapshot's day
// in the server timezone; an increase is a top-up; today's spending is a spike against
// the median of at least three earlier days.

import (
	"math"
	"sort"
	"time"
)

const minBaselineDays = 3

// spendSeries is one project's snapshots, oldest first.
type spendSeries struct {
	projectID, projectName, provider, balanceType string
	samples                                       []sample
}

func spendView(series spendSeries, now time.Time, loc *time.Location) SpendProjectView {
	samples := series.samples
	view := SpendProjectView{
		ProjectID: series.projectID, ProjectName: series.projectName, Provider: series.provider,
		BalanceType: series.balanceType, Unit: unitOf(series.balanceType), Samples: len(samples), Daily: []DailySpendView{},
	}
	if len(samples) == 0 {
		return view
	}
	first, last := samples[0], samples[len(samples)-1]
	view.Balance = round(last.balance)
	span := last.at.Sub(first.at)
	view.SpanDays = round(span.Hours() / 24)

	byDay := map[string]*DailySpendView{}
	day := func(at time.Time) *DailySpendView {
		key := at.In(loc).Format("2006-01-02")
		if byDay[key] == nil {
			byDay[key] = &DailySpendView{Date: key}
		}
		return byDay[key]
	}
	day(first.at)
	for i := 1; i < len(samples); i++ {
		d := day(samples[i].at)
		switch delta := samples[i-1].balance - samples[i].balance; {
		case delta > 0:
			view.Consumed += delta
			d.Consumed += delta
		case delta < 0:
			view.ToppedUp -= delta
			d.ToppedUp -= delta
			view.TopUps++
		}
	}
	// Every day from the first snapshot to the last, quiet days included.
	for at := startOfDay(first.at, loc); !at.After(last.at); at = at.AddDate(0, 0, 1) {
		d := day(at)
		view.Daily = append(view.Daily, DailySpendView{Date: d.Date, Consumed: round(d.Consumed), ToppedUp: round(d.ToppedUp)})
	}
	view.Consumed, view.ToppedUp = round(view.Consumed), round(view.ToppedUp)
	if len(samples) >= 2 {
		perDay := round(view.Consumed / math.Max(span.Hours()/24, 1.0/24))
		view.PerDay = &perDay
	}

	today := now.In(loc).Format("2006-01-02")
	if n := len(view.Daily); n > 0 && view.Daily[n-1].Date == today {
		spent := view.Daily[n-1].Consumed
		view.Today = &spent
		if earlier := view.Daily[:n-1]; len(earlier) >= minBaselineDays {
			values := make([]float64, len(earlier))
			for i, d := range earlier {
				values[i] = d.Consumed
			}
			if baseline := median(values); baseline > 0 {
				ratio := math.Round(spent/baseline*100) / 100
				view.SpikeRatio = &ratio
			}
		}
	}
	return view
}

func startOfDay(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// spendTotals adds the projects up per provider and balance type, the widest grouping
// whose amounts share a unit.
func spendTotals(projects []SpendProjectView) []SpendProviderView {
	var totals []SpendProviderView
	index := map[[2]string]int{}
	for _, p := range projects {
		key := [2]string{p.Provider, p.BalanceType}
		i, ok := index[key]
		if !ok {
			i = len(totals)
			index[key] = i
			totals = append(totals, SpendProviderView{Provider: p.Provider, BalanceType: p.BalanceType, Unit: p.Unit})
		}
		totals[i].Projects++
		totals[i].Consumed = round(totals[i].Consumed + p.Consumed)
		totals[i].ToppedUp = round(totals[i].ToppedUp + p.ToppedUp)
	}
	sort.SliceStable(totals, func(i, j int) bool { return totals[i].Consumed > totals[j].Consumed })
	if totals == nil {
		return []SpendProviderView{}
	}
	return totals
}

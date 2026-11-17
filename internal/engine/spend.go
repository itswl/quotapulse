package engine

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/forecast"
	"github.com/itswl/quotapulse/internal/recur"
)

// Spend is a calendar month of money: what the meters burned, and which
// renewals fall in it.
//
// Only amounts in a currency are added up, and only once converted to the
// base currency with a rate the user supplied. Credits, calls and percent
// are listed in their own unit and never summed: adding 189 "percent" to
// 16 yuan was a real bug in an earlier version of this view.
type Spend struct {
	Month     string `json:"month"`
	Base      string `json:"base"`
	IsCurrent bool   `json:"is_current"`

	Total     float64 `json:"total"`
	Projected float64 `json:"projected"`
	Fixed     float64 `json:"fixed"`

	DaysElapsed float64 `json:"days_elapsed"`
	DaysInMonth int     `json:"days_in_month"`

	Rows        []SpendRow     `json:"rows"`
	Renewals    []SpendRenewal `json:"renewals"`
	Daily       []SpendDay     `json:"daily"`
	Unconverted []string       `json:"unconverted"`
	History     []SpendMonth   `json:"history"`
}

// SpendRow is one meter's month.
type SpendRow struct {
	MeterID   string   `json:"meter_id"`
	Display   string   `json:"display"`
	Unit      string   `json:"unit"`
	Spend     float64  `json:"spend"`
	Projected float64  `json:"projected"`
	Budget    *float64 `json:"budget"`
	// Base and ProjectedBase are the same amounts in the base currency; nil
	// when the unit is not a currency or has no rate.
	Base          *float64 `json:"base"`
	ProjectedBase *float64 `json:"projected_base"`
}

// SpendRenewal is one dated payment.
type SpendRenewal struct {
	ReminderID string     `json:"reminder_id"`
	Title      string     `json:"title"`
	Date       recur.Date `json:"date"`
	Amount     float64    `json:"amount"`
	Currency   string     `json:"currency"`
	Base       *float64   `json:"base"`
}

// SpendDay is one day's metered total in the base currency.
type SpendDay struct {
	Date   recur.Date `json:"date"`
	Amount float64    `json:"amount"`
}

// SpendMonth is a past month's total.
type SpendMonth struct {
	Month string  `json:"month"`
	Total float64 `json:"total"`
	Fixed float64 `json:"fixed"`
}

// Spend builds the spend view for a month ("2026-11"), or the current month
// when month is empty.
func (e *Engine) Spend(ctx context.Context, month, lang string) (Spend, error) {
	v, err := e.view(ctx, lang)
	if err != nil {
		return Spend{}, err
	}
	meters, err := e.Meters(ctx, lang)
	if err != nil {
		return Spend{}, err
	}
	out, err := v.spendMonth(ctx, month, meters, true)
	if err != nil {
		return out, err
	}
	// The five months before, for a sense of scale.
	start, _ := time.ParseInLocation("2006-01", out.Month, v.loc)
	for back := 5; back >= 1; back-- {
		past, err := v.spendMonth(ctx, start.AddDate(0, -back, 0).Format("2006-01"), meters, false)
		if err != nil {
			return out, err
		}
		out.History = append(out.History, SpendMonth{Month: past.Month, Total: past.Total, Fixed: past.Fixed})
	}
	out.History = append(out.History, SpendMonth{Month: out.Month, Total: out.Total, Fixed: out.Fixed})
	return out, nil
}

// spend is the current month, cheaply: it reuses the forecasts the meters
// already carry.
func (v viewer) spend(ctx context.Context, month string, meters []MeterView) (Spend, error) {
	return v.spendMonth(ctx, month, meters, false)
}

func (v viewer) spendMonth(ctx context.Context, month string, meters []MeterView, detailed bool) (Spend, error) {
	local := v.now.In(v.loc)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, v.loc)
	if month != "" {
		parsed, err := time.ParseInLocation("2006-01", month, v.loc)
		if err != nil {
			return Spend{}, invalid("month must look like 2026-11")
		}
		start = parsed
	}
	end := start.AddDate(0, 1, 0)
	current := !v.now.Before(start) && v.now.Before(end)
	until := end
	if current {
		until = v.now
	}
	out := Spend{
		Month: start.Format("2006-01"), Base: v.s.BaseCurrency, IsCurrent: current,
		DaysInMonth: int(end.Sub(start).Hours()/24 + 0.5),
		Rows:        []SpendRow{}, Renewals: []SpendRenewal{}, Daily: []SpendDay{}, Unconverted: []string{}, History: []SpendMonth{},
	}
	out.DaysElapsed = float64(out.DaysInMonth)
	if current {
		out.DaysElapsed = until.Sub(start).Hours() / 24
	}
	unconverted := map[string]bool{}
	perDay := map[recur.Date]float64{}

	if !start.After(v.now) {
		for _, m := range meters {
			if m.Kind == core.Quota || m.Hidden || m.Value == nil {
				continue
			}
			row := SpendRow{MeterID: m.ID, Display: m.Display, Unit: m.Unit, Budget: m.Rules.Budget}
			var days []forecast.Day
			if current && m.Month != nil && !detailed {
				row.Spend, row.Projected = m.Month.Spend, m.Month.Projected
			} else {
				readings, err := v.e.store.ReadingsBetween(ctx, m.ID, start, until)
				if err != nil {
					return out, err
				}
				flows := forecast.Flows(readings, m.Kind)
				row.Spend = forecast.SpendBetween(flows, start, until)
				row.Projected = row.Spend
				if current && m.Month != nil {
					row.Projected = m.Month.Projected
				}
				if detailed {
					days = forecast.Daily(flows, start, until.Add(-time.Second), v.loc)
				}
			}
			if row.Spend == 0 && row.Projected == 0 && row.Budget == nil {
				continue
			}
			if core.IsCurrency(m.Unit) {
				if rate, ok := v.s.Rate(m.Unit); ok {
					base, projected := row.Spend*rate, row.Projected*rate
					row.Base, row.ProjectedBase = &base, &projected
					out.Total += base
					out.Projected += projected
					for _, day := range days {
						perDay[day.Date] += day.Spend * rate
					}
				} else if row.Spend > 0 {
					unconverted[m.Unit] = true
				}
			}
			out.Rows = append(out.Rows, row)
		}
	}
	sort.SliceStable(out.Rows, func(i, j int) bool { return deref(out.Rows[i].Base) > deref(out.Rows[j].Base) })

	reminders, err := v.e.store.Reminders(ctx)
	if err != nil {
		return out, err
	}
	first, last := dateOf(start), dateOf(end.Add(-time.Second))
	for _, r := range reminders {
		if !r.Enabled || r.Amount == nil || *r.Amount <= 0 {
			continue
		}
		currency := r.Currency
		if currency == "" {
			currency = v.s.BaseCurrency
		}
		for date, ok := r.Rule.Next(first); ok && !date.After(last); date, ok = r.Rule.After(date) {
			renewal := SpendRenewal{ReminderID: r.ID, Title: r.Title, Date: date, Amount: *r.Amount, Currency: currency}
			if rate, ok := v.s.Rate(currency); ok {
				base := *r.Amount * rate
				renewal.Base = &base
				out.Fixed += base
			} else {
				unconverted[currency] = true
			}
			out.Renewals = append(out.Renewals, renewal)
		}
	}
	sort.SliceStable(out.Renewals, func(i, j int) bool { return out.Renewals[i].Date.Before(out.Renewals[j].Date) })

	if detailed && !start.After(v.now) {
		for day := first; !day.After(dateOf(until.Add(-time.Second).In(v.loc))); day = day.AddDays(1) {
			out.Daily = append(out.Daily, SpendDay{Date: day, Amount: perDay[day]})
		}
	}
	for unit := range unconverted {
		out.Unconverted = append(out.Unconverted, unit)
	}
	sort.Strings(out.Unconverted)
	out.tidy()
	return out, nil
}

// tidy rounds the sums for a reader, once everything has been added up.
func (s *Spend) tidy() {
	amount := func(v *float64) *float64 {
		if v == nil {
			return nil
		}
		return core.Ptr(forecast.Round(*v))
	}
	s.Total, s.Projected, s.Fixed = forecast.Round(s.Total), forecast.Round(s.Projected), forecast.Round(s.Fixed)
	s.DaysElapsed = math.Round(s.DaysElapsed*100) / 100
	for i := range s.Rows {
		row := &s.Rows[i]
		row.Spend, row.Projected = forecast.Round(row.Spend), forecast.Round(row.Projected)
		row.Base, row.ProjectedBase = amount(row.Base), amount(row.ProjectedBase)
	}
	for i := range s.Renewals {
		s.Renewals[i].Base = amount(s.Renewals[i].Base)
	}
	for i := range s.Daily {
		s.Daily[i].Amount = forecast.Round(s.Daily[i].Amount)
	}
}

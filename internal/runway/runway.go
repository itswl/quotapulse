// Package runway provides the package implementation.
//
// Implementation note.
// Implementation note.
// Implementation note.
//
// Implementation note.
package runway

import (
	"math"
	"sort"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

// Implementation note.
const (
	MinPoints      = 4
	MinSpanHours   = 6.0
	MinBaselineDay = 3 // operation
)

type point struct {
	at      time.Time
	balance float64
}

// Implementation note.
//
// Implementation note.
func Compute(records []model.BalancePoint, windowDays int, now time.Time) model.Runway {
	if now.IsZero() {
		now = time.Now()
	}
	points := validPoints(records)

	result := model.Runway{
		WindowDays: windowDays,
		DataPoints: len(points),
		Confidence: model.ConfidenceNone,
		Daily:      []model.DailySpend{},
	}
	if n := len(records); n > 0 {
		last := records[n-1]
		result.ProjectID = last.ProjectID
		result.ProjectName = last.ProjectName
		result.Provider = last.Provider
		result.BalanceType = last.BalanceType
	}
	if len(points) > 0 {
		result.CurrentBalance = model.Ptr(points[len(points)-1].balance)
	}
	// Quota percentages reset with the provider's rolling window. Treating
	// their fluctuations as spending produces a meaningless daily burn rate
	// and runway, so quota projects use threshold alerts only.
	if result.BalanceType == model.TypeQuota {
		return result
	}
	if len(points) < 2 {
		return result
	}

	span := points[len(points)-1].at.Sub(points[0].at)
	result.SpanHours = round(span.Hours(), 2)

	consumed, toppedUp, perDay := splitFlows(points)
	result.Consumed, result.ToppedUp = consumed, toppedUp
	result.Confidence = confidence(len(points), result.SpanHours)
	if result.Confidence == model.ConfidenceNone {
		return result
	}

	// Implementation note.
	burn := round(result.Consumed/math.Max(result.SpanHours/24, 1.0/24), 4)
	result.BurnPerDay = &burn
	if burn > 0 && result.CurrentBalance != nil {
		days := round(math.Max(*result.CurrentBalance, 0)/burn, 2)
		result.RunwayDays = &days
		depletion := now.Add(time.Duration(days * float64(24*time.Hour))).Format("2006-01-02")
		result.DepletionDate = &depletion
	}

	result.Daily = fillDaily(perDay, points[0].at, points[len(points)-1].at)
	result.TodayConsumed, result.BaselineSpend, result.SpikeRatio = spike(result.Daily, now.Format("2006-01-02"))
	if result.BurnPerDay != nil {
		projection := *result.BurnPerDay * 30
		result.MonthlyProjection = &projection
	}
	return result
}

// Implementation note.
// Implementation note.
func validPoints(records []model.BalancePoint) []point {
	points := make([]point, 0, len(records))
	for _, r := range records {
		if r.Timestamp == 0 {
			continue
		}
		points = append(points, point{at: time.Unix(r.Timestamp, 0).Local(), balance: r.Balance})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].at.Before(points[j].at) })
	return points
}

// Implementation note.
func splitFlows(points []point) (consumed, toppedUp float64, perDay map[string]float64) {
	perDay = make(map[string]float64)
	for i := 1; i < len(points); i++ {
		delta := points[i-1].balance - points[i].balance
		switch {
		case delta > 0:
			consumed += delta
			day := points[i].at.Format("2006-01-02")
			perDay[day] += delta
		case delta < 0:
			toppedUp += -delta
		}
	}
	return round(consumed, 4), round(toppedUp, 4), perDay
}

// Implementation note.
func fillDaily(perDay map[string]float64, start, end time.Time) []model.DailySpend {
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())

	var daily []model.DailySpend
	for day := startDay; !day.After(endDay); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		daily = append(daily, model.DailySpend{Date: key, Consumed: round(perDay[key], 4)})
	}
	return daily
}

// Implementation note.
func spike(daily []model.DailySpend, today string) (todayConsumed, baseline, ratio *float64) {
	if len(daily) == 0 || daily[len(daily)-1].Date != today {
		return nil, nil, nil
	}
	consumed := daily[len(daily)-1].Consumed
	todayConsumed = &consumed

	history := daily[:len(daily)-1]
	if len(history) < MinBaselineDay {
		return todayConsumed, nil, nil
	}
	values := make([]float64, len(history))
	for i, d := range history {
		values[i] = d.Consumed
	}
	mid := round(median(values), 4)
	baseline = &mid
	if mid > 0 {
		r := round(consumed/mid, 2)
		ratio = &r
	}
	return todayConsumed, baseline, ratio
}

// Implementation note.
func confidence(points int, spanHours float64) string {
	switch {
	case points < MinPoints || spanHours < MinSpanHours:
		return model.ConfidenceNone
	case spanHours < 24:
		return model.ConfidenceLow
	case spanHours < 72:
		return model.ConfidenceMedium
	default:
		return model.ConfidenceHigh
	}
}

// Implementation note.
func ComputeAll(series []model.BalancePoint, windowDays int, now time.Time) map[string]model.Runway {
	if len(series) == 0 {
		return nil
	}
	byProject := make(map[string][]model.BalancePoint)
	for _, r := range series {
		byProject[r.ProjectID] = append(byProject[r.ProjectID], r)
	}
	out := make(map[string]model.Runway, len(byProject))
	for id, records := range byProject {
		if containsQuota(records) {
			continue
		}
		out[id] = Compute(records, windowDays, now)
	}
	return out
}

func containsQuota(records []model.BalancePoint) bool {
	for _, record := range records {
		if record.BalanceType == model.TypeQuota {
			return true
		}
	}
	return false
}

// Implementation note.
func Attach(results []model.CheckResult, runways map[string]model.Runway) {
	if len(runways) == 0 {
		return
	}
	for i := range results {
		if !results[i].Success {
			continue
		}
		if r, ok := runways[model.ProjectID(results[i].Provider, results[i].Project)]; ok {
			results[i].Runway = &r
		}
	}
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

// Implementation note.
//
// Implementation note.
// Implementation note.
// Implementation note.
func round(value float64, decimals int) float64 {
	shift := math.Pow(10, float64(decimals))
	return math.RoundToEven(value*shift) / shift
}

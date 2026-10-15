// Package runway reconstructs the burn rate from balance snapshots and
// computes "how many days remain".
//
// Balance history is a series of snapshots: between adjacent points a drop
// is consumption and a rise is a top-up. That yields the daily average burn,
// the runway (days left at the current rate), and whether today's spend
// suddenly ballooned. Both are earlier and sharper than static thresholds:
// the same 430 CNY means very different things at 5/day versus 200/day.
//
// Without a database there is no history, every result is empty, and callers
// naturally fall back to plain threshold alerts.
package runway

import (
	"math"
	"sort"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

// Below these data sizes no conclusions: the computed rate is meaningless.
const (
	MinPoints      = 4
	MinSpanHours   = 6.0
	MinBaselineDay = 3 // minimum complete history days needed as the spike baseline
)

type point struct {
	at      time.Time
	balance float64
}

// Compute derives the spend profile from one account's balance snapshots.
//
// records must be time-ascending; account info comes from the last record.
// A zero now means "use the current time".
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

	// floor the divisor at one hour so a very short window cannot blow the rate up
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

// validPoints drops records whose time or balance fails to parse and sorts by
// time ascending. The database stores UTC; convert to local because "how much
// did we spend today" splits days in the user's timezone.
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

// splitFlows turns the balance series into income and spend: drops are consumption, rises are top-ups; spend also groups by local date.
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

// fillDaily pads days without spend with 0 so the median reflects a true "ordinary day".
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

// spike computes today's spend versus the daily norm. When the last day is not today or the baseline is thin, the last two fields are nil.
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

// confidence declines to conclude on too little data or too short a span; spans under a day never feed alerts.
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

// ComputeAll groups a batch of snapshots by account and computes one profile each.
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

// Attach pins profiles onto balance check results for the dashboard, metrics and alerts to share.
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

// round keeps n decimals with banker's rounding (ties to even): 2.125 becomes
// 2.12, not 2.13.
//
// Do not swap in plain half-up rounding for convenience: stored history was
// computed with this convention, and a different one makes the same data
// produce different numbers — the dashboard curves would step on the day of
// the switch. The 0.01 rarely matters, but when runway days sit exactly on
// an alert threshold it decides whether the alert fires.
func round(value float64, decimals int) float64 {
	shift := math.Pow(10, float64(decimals))
	return math.RoundToEven(value*shift) / shift
}

package mcpserver

// Trend aggregation for balance_trend. Like views.go, this file must not import internal
// packages.

import (
	"math"
	"time"
)

// Trend intervals: every snapshot, or one point per hour, day or week.
var trendIntervals = []string{"raw", "hour", "day", "week"}

// sample is one balance snapshot.
type sample struct {
	at      time.Time
	balance float64
	below   bool
}

// bucketStart is the start of the interval that holds t, in loc; weeks start on Monday.
func bucketStart(t time.Time, interval string, loc *time.Location) time.Time {
	t = t.In(loc)
	y, m, d := t.Date()
	switch interval {
	case "hour":
		return time.Date(y, m, d, t.Hour(), 0, 0, 0, loc)
	case "week":
		day := time.Date(y, m, d, 0, 0, 0, 0, loc)
		return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	default:
		return time.Date(y, m, d, 0, 0, 0, 0, loc)
	}
}

// bucket folds chronological samples into one point per interval, carrying the
// interval's last balance and its range. "raw" keeps every sample as its own point.
func bucket(samples []sample, interval string, loc *time.Location) []TrendPointView {
	points := make([]TrendPointView, 0, len(samples))
	if interval == "raw" {
		for _, s := range samples {
			b := round(s.balance)
			points = append(points, TrendPointView{
				Timestamp: s.at.UTC().Format(time.RFC3339Nano), Balance: b, Min: b, Max: b, Samples: 1, BelowThreshold: s.below,
			})
		}
		return points
	}
	var start time.Time
	for _, s := range samples {
		b := round(s.balance)
		if at := bucketStart(s.at, interval, loc); len(points) == 0 || !at.Equal(start) {
			start = at
			points = append(points, TrendPointView{Timestamp: at.Format(time.RFC3339), Min: b, Max: b})
		}
		p := &points[len(points)-1]
		p.Balance, p.BelowThreshold = b, s.below
		p.Min, p.Max = min(p.Min, b), max(p.Max, b)
		p.Samples++
	}
	return points
}

// summarize describes every sample of the window: its range, net change, and how much
// was spent and topped up between consecutive snapshots.
func summarize(samples []sample) TrendSummary {
	s := TrendSummary{Samples: len(samples)}
	if len(samples) == 0 {
		return s
	}
	first, last := samples[0], samples[len(samples)-1]
	s.First, s.Last, s.Min, s.Max = first.balance, last.balance, first.balance, first.balance
	s.FirstAt, s.LastAt = first.at.UTC().Format(time.RFC3339Nano), last.at.UTC().Format(time.RFC3339Nano)
	sum := 0.0
	for i, p := range samples {
		s.Min, s.Max = min(s.Min, p.balance), max(s.Max, p.balance)
		sum += p.balance
		if i == 0 {
			continue
		}
		switch delta := p.balance - samples[i-1].balance; {
		case delta < 0:
			s.Consumed -= delta
		case delta > 0:
			s.ToppedUp += delta
			s.TopUps++
		}
	}
	s.Average = sum / float64(len(samples))
	s.Change = last.balance - first.balance
	for _, v := range []*float64{&s.First, &s.Last, &s.Min, &s.Max, &s.Average, &s.Change, &s.Consumed, &s.ToppedUp} {
		*v = round(*v)
	}
	return s
}

// round drops float noise such as 0.30000000000000004 from sums and differences.
func round(value float64) float64 { return math.Round(value*1e6) / 1e6 }

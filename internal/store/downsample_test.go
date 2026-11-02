package store

import (
	"testing"
	"time"
)

func trendSeries(n int, value func(i int) float64) []TrendPoint {
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	out := make([]TrendPoint, n)
	for i := range n {
		out[i] = TrendPoint{
			Timestamp: base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339Nano),
			Balance:   value(i),
		}
	}
	return out
}

// The output stays within the cap, keeps the exact endpoints, and preserves
// one-point extremes that uniform sampling would average away.
func TestDownsampleTrend(t *testing.T) {
	// At or below the cap: untouched.
	small := trendSeries(trendPointCap, func(i int) float64 { return float64(i) })
	if got := downsampleTrend(small); len(got) != trendPointCap {
		t.Fatalf("cap 及以下不应改动，得到 %d 点", len(got))
	}

	// Above the cap: bounded, ordered, endpoints exact, extremes kept.
	const n = 5000
	series := trendSeries(n, func(i int) float64 {
		switch i {
		case 1234:
			return 1 // one-point crash
		case 4321:
			return 9999 // one-point spike
		default:
			return 100 + float64(i%7)
		}
	})
	got := downsampleTrend(series)
	if len(got) > trendPointCap+2 {
		t.Fatalf("降采样后 %d 点，超过上限 %d", len(got), trendPointCap+2)
	}
	if got[0].Timestamp != series[0].Timestamp || got[len(got)-1].Timestamp != series[n-1].Timestamp {
		t.Fatal("首尾点必须原样保留")
	}
	var minSeen, maxSeen float64 = got[0].Balance, got[0].Balance
	for i := 1; i < len(got); i++ {
		if got[i].Timestamp <= got[i-1].Timestamp {
			t.Fatalf("时间戳必须严格递增：第 %d 点 %q", i, got[i].Timestamp)
		}
		minSeen = min(minSeen, got[i].Balance)
		maxSeen = max(maxSeen, got[i].Balance)
	}
	if minSeen != 1 || maxSeen != 9999 {
		t.Fatalf("单点极值必须保留：min=%v max=%v，想要 1 和 9999", minSeen, maxSeen)
	}

	// Constant series collapse to the endpoints plus one point per bucket pair.
	flat := downsampleTrend(trendSeries(n, func(int) float64 { return 42 }))
	for _, p := range flat {
		if p.Balance != 42 {
			t.Fatalf("平坦序列的值不该变化: %v", p.Balance)
		}
	}
}

// Through the store: DataPoints reports the stored count, History the capped
// series, and the summary (min/max/avg) covers every row, not just survivors.
func TestBalanceTrendDownsamples(t *testing.T) {
	eachBackend(t, "trend_downsample", func(t *testing.T, f *fixture) {
		s := f.open(Options{})
		ctx := t.Context()

		const n = 1500
		now := time.Now().UTC()
		var sum float64
		for i := range n {
			balance := 100.0 + float64(i%9)
			sum += balance
			at := now.Add(time.Duration(i-n) * time.Hour)
			if _, err := s.db.ExecContext(ctx, rebind(s.engine,
				"INSERT INTO balance_history (project_id, project_name, provider, balance, balance_type, need_alarm, timestamp) VALUES ('p1','one','deepseek',?, 'balance', ?, ?)"),
				balance, false, at); err != nil {
				t.Fatalf("seed %d: %v", i, err)
			}
		}

		trend, err := s.BalanceTrend(ctx, "p1", 365)
		if err != nil {
			t.Fatal(err)
		}
		if trend.DataPoints != n {
			t.Fatalf("DataPoints = %d，应为存储的 %d", trend.DataPoints, n)
		}
		if len(trend.History) > trendPointCap+2 {
			t.Fatalf("History %d 点，超过上限", len(trend.History))
		}
		// The summary covers every stored row, not just the survivors.
		wantAvg := sum / n
		if diff := trend.AvgBalance - wantAvg; diff > 1e-6 || diff < -1e-6 {
			t.Fatalf("AvgBalance = %v，应为全量均值 %v", trend.AvgBalance, wantAvg)
		}
		if trend.MinBalance != 100 || trend.MaxBalance != 108 {
			t.Fatalf("min/max = %v/%v，应为 100/108", trend.MinBalance, trend.MaxBalance)
		}
	})
}

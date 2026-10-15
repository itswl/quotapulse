package runway

import (
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

// Hour offsets in the baseline data are relative to this instant; change it and all of testdata/baseline_cases.json desyncs.
var baselineNow = time.Date(2026, 9, 16, 20, 0, 0, 0, time.Local)

type baselineCase struct {
	Name   string      `json:"name"`
	Points [][]float64 `json:"points"` // [小时偏移, 余额]
	Result struct {
		DataPoints     int      `json:"data_points"`
		SpanHours      float64  `json:"span_hours"`
		Consumed       float64  `json:"consumed"`
		ToppedUp       float64  `json:"topped_up"`
		BurnPerDay     *float64 `json:"burn_per_day"`
		RunwayDays     *float64 `json:"runway_days"`
		DepletionDate  *string  `json:"depletion_date"`
		Confidence     string   `json:"confidence"`
		CurrentBalance *float64 `json:"current_balance"`
		TodayConsumed  *float64 `json:"today_consumed"`
		BaselineSpend  *float64 `json:"baseline_consumed"`
		SpikeRatio     *float64 `json:"spike_ratio"`
	} `json:"result"`
	Daily []model.DailySpend `json:"daily"`
}

// TestMatchesBaseline pins this package's behavior contract: 12 realistically
// shaped balance series compared field by field. Spend, top-ups, confidence,
// runway and spike ratio all live here — any algorithm regression blows up in
// this test.
func TestMatchesBaseline(t *testing.T) {
	raw, err := os.ReadFile("testdata/baseline_cases.json")
	if err != nil {
		t.Fatalf("读取基准数据失败: %v", err)
	}
	var cases []baselineCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("解析基准数据失败: %v", err)
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			got := Compute(buildPoints(c.Points), 7, baselineNow)

			assertEqual(t, "data_points", float64(got.DataPoints), float64(c.Result.DataPoints))
			assertEqual(t, "span_hours", got.SpanHours, c.Result.SpanHours)
			assertEqual(t, "consumed", got.Consumed, c.Result.Consumed)
			assertEqual(t, "topped_up", got.ToppedUp, c.Result.ToppedUp)
			if got.Confidence != c.Result.Confidence {
				t.Errorf("confidence: 期望 %q，实际 %q", c.Result.Confidence, got.Confidence)
			}
			assertPtr(t, "burn_per_day", got.BurnPerDay, c.Result.BurnPerDay)
			assertPtr(t, "runway_days", got.RunwayDays, c.Result.RunwayDays)
			assertPtr(t, "current_balance", got.CurrentBalance, c.Result.CurrentBalance)
			assertPtr(t, "today_consumed", got.TodayConsumed, c.Result.TodayConsumed)
			assertPtr(t, "baseline_consumed", got.BaselineSpend, c.Result.BaselineSpend)
			assertPtr(t, "spike_ratio", got.SpikeRatio, c.Result.SpikeRatio)

			if (got.DepletionDate == nil) != (c.Result.DepletionDate == nil) {
				t.Errorf("depletion_date: 期望 %v，实际 %v", strOrNil(c.Result.DepletionDate), strOrNil(got.DepletionDate))
			} else if got.DepletionDate != nil && *got.DepletionDate != *c.Result.DepletionDate {
				t.Errorf("depletion_date: 期望 %s，实际 %s", *c.Result.DepletionDate, *got.DepletionDate)
			}

			if len(got.Daily) != len(c.Daily) {
				t.Fatalf("daily 长度: 期望 %d，实际 %d", len(c.Daily), len(got.Daily))
			}
			for i := range c.Daily {
				if got.Daily[i].Date != c.Daily[i].Date {
					t.Errorf("daily[%d].date: 期望 %s，实际 %s", i, c.Daily[i].Date, got.Daily[i].Date)
				}
				assertEqual(t, "daily["+c.Daily[i].Date+"].consumed", got.Daily[i].Consumed, c.Daily[i].Consumed)
			}
		})
	}
}

func buildPoints(raw [][]float64) []model.BalancePoint {
	points := make([]model.BalancePoint, 0, len(raw))
	for _, pair := range raw {
		at := baselineNow.Add(time.Duration(pair[0] * float64(time.Hour)))
		points = append(points, model.BalancePoint{
			ProjectID: "pid", ProjectName: "demo", Provider: "deepseek",
			BalanceType: "balance", Balance: pair[1], Timestamp: at.Unix(),
		})
	}
	return points
}

// TestConfidenceBoundaries: the three confidence boundaries that gate whether an estimate may alert.
func TestConfidenceBoundaries(t *testing.T) {
	tests := []struct {
		points    int
		spanHours float64
		want      string
		why       string
	}{
		{3, 100, model.ConfidenceNone, "点数不足 4 个"},
		{4, 5.9, model.ConfidenceNone, "跨度不足 6 小时"},
		{4, 6, model.ConfidenceLow, "刚够 6 小时"},
		{4, 23.9, model.ConfidenceLow, "不满一天"},
		{4, 24, model.ConfidenceMedium, "满一天"},
		{4, 71.9, model.ConfidenceMedium, "不满三天"},
		{4, 72, model.ConfidenceHigh, "满三天"},
	}
	for _, tt := range tests {
		if got := confidence(tt.points, tt.spanHours); got != tt.want {
			t.Errorf("%d 点 %.1f 小时（%s）: 期望 %s，实际 %s", tt.points, tt.spanHours, tt.why, tt.want, got)
		}
	}
}

// TestToppedUpNotCountedAsConsumption: a top-up jumps the balance up and must never count as negative consumption.
func TestToppedUpNotCountedAsConsumption(t *testing.T) {
	points := buildPoints([][]float64{{-48, 100}, {-36, 60}, {-24, 500}, {-12, 460}, {0, 420}})
	got := Compute(points, 7, baselineNow)

	if got.Consumed != 120 {
		t.Errorf("消耗应为 40+40+40=120，实际 %v", got.Consumed)
	}
	if got.ToppedUp != 440 {
		t.Errorf("充值应为 500-60=440，实际 %v", got.ToppedUp)
	}
}

// TestZeroBurnHasNoRunway: with zero consumption there must be no runway days.
func TestZeroBurnHasNoRunway(t *testing.T) {
	points := buildPoints([][]float64{{-72, 200}, {-48, 200}, {-24, 200}, {0, 200}})
	got := Compute(points, 7, baselineNow)

	if got.BurnPerDay == nil || *got.BurnPerDay != 0 {
		t.Errorf("日均消耗应为 0，实际 %v", got.BurnPerDay)
	}
	if got.RunwayDays != nil {
		t.Errorf("没有消耗就不该有跑道，实际 %v", *got.RunwayDays)
	}
}

func TestQuotaDoesNotProduceRunway(t *testing.T) {
	points := buildPoints([][]float64{{-72, 100}, {-48, 90}, {-24, 80}, {0, 70}})
	for i := range points {
		points[i].BalanceType = model.TypeQuota
	}

	got := Compute(points, 7, baselineNow)
	if got.CurrentBalance == nil || *got.CurrentBalance != 70 {
		t.Fatalf("quota should retain its current value, got %v", got.CurrentBalance)
	}
	if got.Confidence != model.ConfidenceNone || got.BurnPerDay != nil || got.RunwayDays != nil {
		t.Fatalf("quota should not have a runway estimate: %+v", got)
	}
}

func TestComputeAllSkipsQuotaProjects(t *testing.T) {
	points := buildPoints([][]float64{{-72, 100}, {-48, 90}, {-24, 80}, {0, 70}})
	for i := range points {
		points[i].BalanceType = model.TypeQuota
	}

	if got := ComputeAll(points, 7, baselineNow); len(got) != 0 {
		t.Fatalf("quota projects should not enter runway results: %+v", got)
	}
}

func assertEqual(t *testing.T, field string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s: 期望 %v，实际 %v", field, want, got)
	}
}

func assertPtr(t *testing.T, field string, got, want *float64) {
	t.Helper()
	if (got == nil) != (want == nil) {
		t.Errorf("%s: 期望 %v，实际 %v", field, numOrNil(want), numOrNil(got))
		return
	}
	if got != nil && math.Abs(*got-*want) > 1e-6 {
		t.Errorf("%s: 期望 %v，实际 %v", field, *want, *got)
	}
}

func numOrNil(p *float64) any {
	if p == nil {
		return "nil"
	}
	return *p
}

func strOrNil(p *string) any {
	if p == nil {
		return "nil"
	}
	return *p
}

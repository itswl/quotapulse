package forecast

import (
	"math"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/core"
)

var (
	shanghai = mustLocation("Asia/Shanghai")
	t0       = time.Date(2026, 11, 16, 10, 0, 0, 0, shanghai)
)

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// series builds hourly readings ending at t0. spend(hoursAgo) is what was
// spent in the hour that ended that many hours ago.
func series(hours int, final float64, spend func(hoursAgo int) float64) []core.Reading {
	readings := make([]core.Reading, hours+1)
	value := final
	for ago := 0; ago <= hours; ago++ {
		readings[hours-ago] = core.Reading{At: t0.Add(-time.Duration(ago) * time.Hour), Value: value}
		value += spend(ago)
	}
	return readings
}

func near(got, want, tolerance float64) bool { return math.Abs(got-want) <= tolerance }

func TestFlowsByKind(t *testing.T) {
	at := func(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }
	readings := []core.Reading{
		{At: at(0), Value: 100}, {At: at(1), Value: 90}, {At: at(2), Value: 90},
		{At: at(3), Value: 150}, {At: at(4), Value: 140},
	}

	balance := Flows(readings, core.Balance)
	if len(balance) != 3 || balance[0].Spend != 10 || balance[1].TopUp != 60 || balance[2].Spend != 10 {
		t.Fatalf("balance flows = %+v", balance)
	}

	// The same numbers as an odometer: rises are spend, and a drop means it
	// started over — the new reading is what was spent since.
	counter := Flows(readings, core.Counter)
	if len(counter) != 3 || counter[0].Spend != 90 || counter[1].Spend != 60 || counter[2].Spend != 140 {
		t.Fatalf("counter flows = %+v", counter)
	}

	// A quota reset is a rise, never spend.
	quota := Flows([]core.Reading{{At: at(0), Value: 40}, {At: at(1), Value: 12}, {At: at(2), Value: 100}}, core.Quota)
	if len(quota) != 2 || quota[0].Spend != 28 || quota[1].Spend != 0 || quota[1].TopUp != 88 {
		t.Fatalf("quota flows = %+v", quota)
	}

	if got := Flows(readings[:1], core.Balance); got != nil {
		t.Errorf("one reading has no flows, got %+v", got)
	}
}

func TestSpendBetweenSplitsByTime(t *testing.T) {
	flows := []Flow{{From: t0, To: t0.Add(4 * time.Hour), Spend: 8}}
	cases := []struct {
		from, to int
		want     float64
	}{
		{0, 4, 8}, {-2, 6, 8}, {0, 1, 2}, {1, 3, 4}, {3, 9, 2}, {4, 5, 0}, {-3, 0, 0}, {2, 2, 0},
	}
	for _, c := range cases {
		got := SpendBetween(flows, t0.Add(time.Duration(c.from)*time.Hour), t0.Add(time.Duration(c.to)*time.Hour))
		if !near(got, c.want, 1e-9) {
			t.Errorf("SpendBetween(%d,%d) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestDailyUsesLocalDays(t *testing.T) {
	// 22:00 to 02:00 Shanghai time straddles midnight: half the spend belongs
	// to each day. In UTC both halves would land on the same day.
	evening := time.Date(2026, 11, 15, 22, 0, 0, 0, shanghai)
	flows := []Flow{
		{From: evening, To: evening.Add(4 * time.Hour), Spend: 8},
		{From: evening.Add(4 * time.Hour), To: evening.Add(5 * time.Hour), TopUp: 100},
	}
	days := Daily(flows, evening.Add(-48*time.Hour), evening.Add(6*time.Hour), shanghai)
	if len(days) != 4 {
		t.Fatalf("want 4 days including the quiet ones, got %+v", days)
	}
	if days[0].Spend != 0 || days[1].Spend != 0 {
		t.Errorf("quiet days must be zero: %+v", days[:2])
	}
	if days[2].Date.String() != "2026-11-15" || !near(days[2].Spend, 4, 1e-9) {
		t.Errorf("the 15th = %+v", days[2])
	}
	if days[3].Date.String() != "2026-11-16" || !near(days[3].Spend, 4, 1e-9) || days[3].TopUp != 100 {
		t.Errorf("the 16th = %+v", days[3])
	}
}

func TestSteadySpend(t *testing.T) {
	// 24 a day for 20 days, 480 left.
	got := ForBalance(series(20*24, 480, func(int) float64 { return 1 }), core.Balance)
	if got.Confidence != ConfidenceHigh || !got.Alertable() {
		t.Fatalf("confidence = %s", got.Confidence)
	}
	if !near(got.Burn, 24, 0.01) || !near(*got.Burn7, 24, 0.01) || !near(got.Peak, 24, 0.01) {
		t.Errorf("burn = %v, burn7 = %v, peak = %v", got.Burn, *got.Burn7, got.Peak)
	}
	if !near(*got.RunwayDays, 20, 0.01) {
		t.Errorf("runway = %v", *got.RunwayDays)
	}
	if want := t0.Add(20 * 24 * time.Hour); got.DepletesAt.Sub(want).Abs() > time.Hour {
		t.Errorf("depletes at %v, want about %v", got.DepletesAt, want)
	}
	if got.Burn30 != nil {
		t.Errorf("20 days of history is not a 30-day average: %v", *got.Burn30)
	}
	if len(got.Bursts) != 2 {
		t.Fatalf("bursts = %+v", got.Bursts)
	}
	for _, b := range got.Bursts {
		if !near(*b.Ratio, 1, 0.01) {
			t.Errorf("steady spend is no burst: %+v", b)
		}
	}
}

func TestRecentDaysCountForMore(t *testing.T) {
	// Idle for three weeks, then 10 a day for the last week.
	rising := ForBalance(series(28*24, 1000, func(ago int) float64 {
		if ago < 7*24 {
			return 10.0 / 24
		}
		return 0
	}), core.Balance)
	// A plain 28-day average says 2.5; the weighted one must sit well above
	// it, near what is actually being spent now.
	if rising.Burn < 5 || rising.Burn > 10 {
		t.Errorf("weighted burn = %v, want between 5 and 10", rising.Burn)
	}

	// The mirror image: a burst three weeks ago, nothing since.
	faded := ForBalance(series(28*24, 1000, func(ago int) float64 {
		if ago >= 21*24 {
			return 10.0 / 24
		}
		return 0
	}), core.Balance)
	if faded.Burn > 1 {
		t.Errorf("an old burst should have faded, burn = %v", faded.Burn)
	}
	if faded.Burn <= 0 || faded.RunwayDays == nil {
		t.Errorf("but not vanished: %+v", faded)
	}
}

func TestIdleBalanceHasNoRunway(t *testing.T) {
	got := ForBalance(series(10*24, 500, func(int) float64 { return 0 }), core.Balance)
	if got.Burn != 0 || got.RunwayDays != nil || got.DepletesAt != nil || len(got.Bursts) != 0 {
		t.Errorf("nothing spent means nothing to forecast: %+v", got)
	}
	if got.Confidence != ConfidenceHigh {
		t.Errorf("confidence is about history, not spend: %s", got.Confidence)
	}
}

func TestThinHistory(t *testing.T) {
	if got := ForBalance(nil, core.Balance); got.Confidence != ConfidenceNone || got.Alertable() {
		t.Errorf("no readings: %+v", got)
	}
	three := ForBalance(series(3, 100, func(int) float64 { return 1 }), core.Balance)
	if three.Confidence != ConfidenceNone || three.RunwayDays != nil || three.Burn != 0 {
		t.Errorf("three hours is not a forecast: %+v", three)
	}
	twelve := ForBalance(series(12, 100, func(int) float64 { return 1 }), core.Balance)
	if twelve.Confidence != ConfidenceLow || twelve.Alertable() || twelve.RunwayDays == nil {
		t.Errorf("twelve hours shows an estimate but must not alert: %+v", twelve)
	}
	if !near(twelve.Burn, 24, 0.01) {
		t.Errorf("burn from twelve hours = %v", twelve.Burn)
	}
	twoDays := ForBalance(series(48, 100, func(int) float64 { return 1 }), core.Balance)
	if twoDays.Confidence != ConfidenceMedium || !twoDays.Alertable() {
		t.Errorf("two days: %+v", twoDays)
	}
	if len(twoDays.Bursts) != 0 {
		t.Errorf("two days is no baseline for a burst: %+v", twoDays.Bursts)
	}
}

// A real false alarm from the old spike rule: 2.53 spent in a day against a
// usual 0.1, on a balance of 547. "25 times the baseline" — and still two
// hundred days of runway. The numbers must make that obvious.
func TestSmallBurstOnLargeBalance(t *testing.T) {
	got := ForBalance(series(14*24, 547.67, func(ago int) float64 {
		if ago < 24 {
			return 2.53 / 24
		}
		return 0.1 / 24
	}), core.Balance)
	var daily Burst
	for _, b := range got.Bursts {
		if b.WindowHours == 24 {
			daily = b
		}
	}
	if daily.Ratio == nil || *daily.Ratio < 20 {
		t.Fatalf("the ratio really is large: %+v", daily)
	}
	if daily.RunwayDays < 200 {
		t.Errorf("at the burst rate there are still %v days", daily.RunwayDays)
	}
}

// A leaked key: 50 an hour for the last three hours.
func TestLeakShowsAsShortBurstRunway(t *testing.T) {
	got := ForBalance(series(14*24, 397, func(ago int) float64 {
		if ago < 3 {
			return 50
		}
		return 0.1 / 24
	}), core.Balance)
	var short Burst
	for _, b := range got.Bursts {
		if b.WindowHours == 3 {
			short = b
		}
	}
	if !near(short.RatePerDay, 1200, 1) || short.RunwayDays > 0.5 {
		t.Fatalf("burst = %+v", short)
	}
	if short.Ratio == nil || *short.Ratio < 1000 {
		t.Errorf("ratio = %v", short.Ratio)
	}
}

func TestBurstWithoutBaseline(t *testing.T) {
	got := ForBalance(series(10*24, 100, func(ago int) float64 {
		if ago < 3 {
			return 5
		}
		return 0
	}), core.Balance)
	if len(got.Bursts) == 0 || got.Bursts[0].Ratio != nil {
		t.Fatalf("no prior spend means no ratio: %+v", got.Bursts)
	}
}

func TestTopUpIsNotNegativeSpend(t *testing.T) {
	readings := series(10*24, 900, func(ago int) float64 { return 1 })
	// 800 added five days ago
	for i := range readings {
		if readings[i].At.Before(t0.Add(-5 * 24 * time.Hour)) {
			readings[i].Value -= 800
		}
	}
	got := ForBalance(readings, core.Balance)
	if !near(got.Burn, 24, 0.5) {
		t.Errorf("burn = %v, a top-up must not lower it", got.Burn)
	}
	if got.LastTopUp == nil || !near(got.LastTopUp.Amount, 799, 1.5) {
		t.Errorf("last top-up = %+v", got.LastTopUp)
	}
}

func TestLookbackIsBounded(t *testing.T) {
	// Heavy spend more than 30 days ago must not show at all.
	got := ForBalance(series(60*24, 100, func(ago int) float64 {
		if ago > 35*24 {
			return 100
		}
		return 0
	}), core.Balance)
	if got.Burn != 0 || got.CoverageDays != 30 {
		t.Errorf("burn = %v, coverage = %v", got.Burn, got.CoverageDays)
	}
}

func TestCounterForecast(t *testing.T) {
	readings := make([]core.Reading, 0, 10*24)
	total := 0.0
	for h := 10 * 24; h >= 0; h-- {
		readings = append(readings, core.Reading{At: t0.Add(-time.Duration(h) * time.Hour), Value: total})
		total += 0.5
	}
	got := ForBalance(readings, core.Counter)
	if !near(got.Burn, 12, 0.01) {
		t.Errorf("counter burn = %v", got.Burn)
	}
	if got.RunwayDays != nil || got.LastTopUp != nil || len(got.Bursts) != 0 {
		t.Errorf("a counter does not run out: %+v", got)
	}
}

func TestQuota(t *testing.T) {
	window := int64(5 * 3600)
	reset := t0.Add(3 * time.Hour) // two of five hours gone

	// 30% used in 40% of the window: on course for 75%, fine.
	calm := ForQuota(70, core.Ptr(100.0), &reset, &window, t0)
	if calm.RemainingPct != 70 || calm.UsedPct != 30 || *calm.ElapsedPct != 40 || *calm.ProjectedPct != 75 {
		t.Fatalf("calm = %+v", calm)
	}
	if calm.ExhaustsAt != nil {
		t.Errorf("75%% projected does not run out: %v", calm.ExhaustsAt)
	}

	// 80% used in 40% of the window: runs out half an hour from now.
	hot := ForQuota(20, core.Ptr(100.0), &reset, &window, t0)
	if *hot.ProjectedPct != 200 || hot.ExhaustsAt == nil {
		t.Fatalf("hot = %+v", hot)
	}
	if want := t0.Add(30 * time.Minute); hot.ExhaustsAt.Sub(want).Abs() > time.Second {
		t.Errorf("exhausts at %v, want %v", hot.ExhaustsAt, want)
	}

	// Absolute units: 923 of 1000 calls left.
	calls := ForQuota(923, core.Ptr(1000.0), nil, nil, t0)
	if calls.RemainingPct != 92.3 || calls.UsedPct != 7.7 || calls.ElapsedPct != nil {
		t.Errorf("calls = %+v", calls)
	}

	// A few minutes into a window the pace is noise.
	early := ForQuota(95, core.Ptr(100.0), core.Ptr(t0.Add(295*time.Minute)), &window, t0)
	if early.ProjectedPct != nil || early.ExhaustsAt != nil {
		t.Errorf("too early to project: %+v", early)
	}

	empty := ForQuota(0, core.Ptr(100.0), &reset, &window, t0)
	if empty.RemainingPct != 0 || empty.ExhaustsAt == nil || !empty.ExhaustsAt.Equal(t0) {
		t.Errorf("empty = %+v", empty)
	}

	// No limit given: the value is already a percentage.
	bare := ForQuota(42.4, nil, nil, nil, t0)
	if bare.RemainingPct != 42.4 {
		t.Errorf("bare = %+v", bare)
	}

	// A reset time in the past is stale data, not a window.
	past := t0.Add(-time.Minute)
	if got := ForQuota(50, nil, &past, &window, t0); got.ElapsedPct != nil {
		t.Errorf("stale reset: %+v", got)
	}
}

func TestMonth(t *testing.T) {
	// One unit an hour since the start of November. t0 is 10:00 on the 16th.
	flows := Flows(series(40*24, 0, func(int) float64 { return 1 }), core.Balance)

	got := ForMonth(flows, core.Ptr(24.0), t0, shanghai)
	elapsed := 15 + 10.0/24
	if got.Month != "2026-11" || got.DaysInMonth != 30 || !near(got.DaysElapsed, elapsed, 0.01) {
		t.Fatalf("month = %+v", got)
	}
	if !near(got.Spend, elapsed*24, 0.01) {
		t.Errorf("month to date = %v, want %v — October must not leak in", got.Spend, elapsed*24)
	}
	if !near(got.Projected, 30*24, 0.01) {
		t.Errorf("projected = %v", got.Projected)
	}

	// No burn estimate: extend the month so far in a straight line.
	if linear := ForMonth(flows, nil, t0, shanghai); !near(linear.Projected, 30*24, 0.01) {
		t.Errorf("linear projection = %v", linear.Projected)
	}

	// Spending has stopped: the projection is just what was spent.
	if stopped := ForMonth(flows, core.Ptr(0.0), t0, shanghai); !near(stopped.Projected, stopped.Spend, 1e-9) {
		t.Errorf("stopped = %+v", stopped)
	}

	// On the first morning a straight line would multiply noise by thirty.
	first := time.Date(2026, 11, 1, 6, 0, 0, 0, shanghai)
	if early := ForMonth(flows, nil, first, shanghai); !near(early.Projected, early.Spend, 1e-9) {
		t.Errorf("first morning = %+v", early)
	}
}

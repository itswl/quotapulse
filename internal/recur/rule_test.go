package recur

import (
	"encoding/json"
	"testing"
)

func date(s string) Date {
	d, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func ptr(d Date) *Date { return &d }

func TestNext(t *testing.T) {
	cases := []struct {
		name string
		rule Rule
		from string
		want string
	}{
		{"weekly today counts", Rule{Freq: Weekly, Weekday: 1}, "2026-11-16", "2026-11-16"},
		{"weekly later this week", Rule{Freq: Weekly, Weekday: 5}, "2026-11-16", "2026-11-20"},
		{"weekly wraps to next week", Rule{Freq: Weekly, Weekday: 7}, "2026-11-16", "2026-11-22"},
		{"weekly sunday from sunday", Rule{Freq: Weekly, Weekday: 7}, "2026-11-22", "2026-11-22"},

		{"monthly today counts", Rule{Freq: Monthly, Day: 16}, "2026-11-16", "2026-11-16"},
		{"monthly passed rolls over", Rule{Freq: Monthly, Day: 15}, "2026-11-16", "2026-12-15"},
		{"monthly rolls over the year", Rule{Freq: Monthly, Day: 5}, "2026-12-16", "2027-01-05"},
		{"monthly 31st clamps to feb 28", Rule{Freq: Monthly, Day: 31}, "2027-02-01", "2027-02-28"},
		{"monthly 31st clamps to feb 29 in a leap year", Rule{Freq: Monthly, Day: 31}, "2028-02-01", "2028-02-29"},
		{"monthly 31st clamped day is not skipped", Rule{Freq: Monthly, Day: 31}, "2027-02-28", "2027-02-28"},
		{"monthly 31st after the clamped day", Rule{Freq: Monthly, Day: 31}, "2027-03-01", "2027-03-31"},
		{"monthly 31st in a 30-day month", Rule{Freq: Monthly, Day: 31}, "2026-11-30", "2026-11-30"},

		{"yearly later this year", Rule{Freq: Yearly, Month: 12, Day: 25}, "2026-11-16", "2026-12-25"},
		{"yearly passed rolls over", Rule{Freq: Yearly, Month: 1, Day: 1}, "2026-11-16", "2027-01-01"},
		{"yearly feb 29 in a common year", Rule{Freq: Yearly, Month: 2, Day: 29}, "2027-01-01", "2027-02-28"},
		{"yearly feb 29 in a leap year", Rule{Freq: Yearly, Month: 2, Day: 29}, "2028-01-01", "2028-02-29"},

		{"once in the future", Rule{Freq: Once, Date: ptr(date("2031-05-01"))}, "2026-11-16", "2031-05-01"},
		{"once today", Rule{Freq: Once, Date: ptr(date("2026-11-16"))}, "2026-11-16", "2026-11-16"},

		// every N, counted from the anchor
		{"every 2 weeks before the anchor", Rule{Freq: Weekly, Interval: 2, Date: ptr(date("2026-11-20"))}, "2026-11-01", "2026-11-20"},
		{"every 2 weeks on a beat", Rule{Freq: Weekly, Interval: 2, Date: ptr(date("2026-11-20"))}, "2026-12-04", "2026-12-04"},
		{"every 2 weeks between beats", Rule{Freq: Weekly, Interval: 2, Date: ptr(date("2026-11-20"))}, "2026-11-21", "2026-12-04"},
		{"quarterly", Rule{Freq: Monthly, Interval: 3, Day: 15, Date: ptr(date("2026-01-15"))}, "2026-11-16", "2027-01-15"},
		{"quarterly on the day", Rule{Freq: Monthly, Interval: 3, Day: 15, Date: ptr(date("2026-01-15"))}, "2026-10-15", "2026-10-15"},
		{"quarterly from the 31st clamps", Rule{Freq: Monthly, Interval: 3, Day: 31, Date: ptr(date("2026-01-31"))}, "2026-02-01", "2026-04-30"},
		{"every 2 years", Rule{Freq: Yearly, Interval: 2, Month: 3, Day: 10, Date: ptr(date("2025-03-10"))}, "2026-01-01", "2027-03-10"},
		{"every 2 years on a beat year, passed", Rule{Freq: Yearly, Interval: 2, Month: 3, Day: 10, Date: ptr(date("2025-03-10"))}, "2027-03-11", "2029-03-10"},
		{"every 2 years before the anchor", Rule{Freq: Yearly, Interval: 2, Month: 3, Day: 10, Date: ptr(date("2025-03-10"))}, "2024-01-01", "2025-03-10"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := c.rule.Next(date(c.from))
			if !ok || got.String() != c.want {
				t.Fatalf("Next(%s) = %s, %v; want %s", c.from, got, ok, c.want)
			}
		})
	}
}

func TestOnceInThePastHasNoNext(t *testing.T) {
	rule := Rule{Freq: Once, Date: ptr(date("2026-11-15"))}
	if got, ok := rule.Next(date("2026-11-16")); ok {
		t.Fatalf("a passed one-off came back as %s", got)
	}
	if got, ok := rule.Prev(date("2026-11-16")); !ok || got.String() != "2026-11-15" {
		t.Fatalf("Prev = %s, %v", got, ok)
	}
}

// Spring Festival, Dragon Boat and Mid-Autumn dates are public record; they
// pin the conversion end to end.
func TestLunarYearlyFestivals(t *testing.T) {
	cases := []struct {
		name       string
		month, day int
		from, want string
	}{
		{"spring festival 2025", 1, 1, "2024-06-01", "2025-01-29"},
		{"spring festival 2026", 1, 1, "2025-06-01", "2026-02-17"},
		{"spring festival 2027", 1, 1, "2026-11-16", "2027-02-06"},
		{"spring festival 2028", 1, 1, "2027-06-01", "2028-01-26"},
		{"spring festival on the day", 1, 1, "2027-02-06", "2027-02-06"},
		{"spring festival the day after", 1, 1, "2027-02-07", "2028-01-26"},
		{"dragon boat 2026", 5, 5, "2026-01-01", "2026-06-19"},
		{"dragon boat 2027", 5, 5, "2026-06-20", "2027-06-09"},
		{"mid-autumn 2026", 8, 15, "2026-01-01", "2026-09-25"},
		{"mid-autumn 2027", 8, 15, "2026-09-26", "2027-09-15"},
		// Early in a Gregorian year the lunar year has not turned yet: on
		// 2027-01-10 it is still the 12th month of lunar 2026.
		{"twelfth month before the new year", 12, 24, "2027-01-10", "2027-01-31"},
		{"eleventh month, same gregorian year", 11, 16, "2026-11-16", "2026-12-24"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rule := Rule{Freq: Yearly, Calendar: LunarCal, Month: c.month, Day: c.day}
			got, ok := rule.Next(date(c.from))
			if !ok || got.String() != c.want {
				t.Fatalf("Next(%s) = %s, %v; want %s", c.from, got, ok, c.want)
			}
		})
	}
}

// 腊月三十 must be New Year's Eve every year, whether 腊月 has 29 or 30 days.
// Skipping the short years would silently lose the reminder.
func TestLunarThirtiethIsNewYearsEve(t *testing.T) {
	newYears := []string{"2024-02-10", "2025-01-29", "2026-02-17", "2027-02-06", "2028-01-26"}
	eve := Rule{Freq: Yearly, Calendar: LunarCal, Month: 12, Day: 30}
	for _, s := range newYears {
		newYear := date(s)
		got, ok := eve.Next(newYear.AddDays(-200))
		if want := newYear.AddDays(-1); !ok || got != want {
			t.Errorf("eve before %s = %s, %v; want %s", s, got, ok, want)
		}
	}
}

func TestLunarMonthly(t *testing.T) {
	first := Rule{Freq: Monthly, Calendar: LunarCal, Day: 1}
	// 2027-02-06 is the first day of the first lunar month.
	if got, _ := first.Next(date("2027-02-01")); got.String() != "2027-02-06" {
		t.Errorf("next 初一 = %s", got)
	}
	if got, _ := first.Next(date("2027-02-06")); got.String() != "2027-02-06" {
		t.Errorf("初一 on the day = %s", got)
	}
	fifteenth := Rule{Freq: Monthly, Calendar: LunarCal, Day: 15}
	// Mid-Autumn is the 15th of the 8th month.
	if got, _ := fifteenth.Next(date("2026-09-20")); got.String() != "2026-09-25" {
		t.Errorf("next 十五 = %s", got)
	}
	// Whatever the month lengths, consecutive occurrences are 29 or 30 days apart.
	current, _ := fifteenth.Next(date("2026-01-01"))
	for range 30 {
		next, ok := fifteenth.After(current)
		if gap := next.Sub(current); !ok || gap < 29 || gap > 30 {
			t.Fatalf("十五 after %s is %s (gap %d)", current, next, gap)
		}
		current = next
	}
}

func TestPrevAgreesWithNext(t *testing.T) {
	rules := []Rule{
		{Freq: Weekly, Weekday: 3},
		{Freq: Monthly, Day: 31},
		{Freq: Yearly, Month: 2, Day: 29},
		{Freq: Yearly, Calendar: LunarCal, Month: 12, Day: 30},
		{Freq: Monthly, Calendar: LunarCal, Day: 15},
		{Freq: Monthly, Interval: 3, Day: 15, Date: ptr(date("2026-01-15"))},
		{Freq: Yearly, Interval: 2, Month: 3, Day: 10, Date: ptr(date("2021-03-10"))},
	}
	for _, rule := range rules {
		day := date("2026-11-16")
		for range 12 {
			next, ok := rule.Next(day)
			if !ok {
				t.Fatalf("%+v: no next from %s", rule, day)
			}
			prev, ok := rule.Prev(next)
			if !ok {
				t.Fatalf("%+v: no prev before %s", rule, next)
			}
			if back, _ := rule.After(prev); back != next {
				t.Fatalf("%+v: After(Prev(%s)) = %s", rule, next, back)
			}
			day = next.AddDays(1)
		}
	}
}

func TestPrevBeforeTheAnchorIsEmpty(t *testing.T) {
	rule := Rule{Freq: Monthly, Interval: 3, Day: 15, Date: ptr(date("2026-01-15"))}
	if got, ok := rule.Prev(date("2026-01-15")); ok {
		t.Fatalf("occurrence before the anchor: %s", got)
	}
}

func TestOrdinal(t *testing.T) {
	solar := Rule{Freq: Yearly, Month: 7, Day: 16, Since: 1990}
	if got := solar.Ordinal(date("2027-07-16")); got != 37 {
		t.Errorf("solar ordinal = %d", got)
	}
	// 2027-01-31 is still lunar year 2026: the lunar age counts lunar years.
	lunar := Rule{Freq: Yearly, Calendar: LunarCal, Month: 12, Day: 24, Since: 1990}
	if got := lunar.Ordinal(date("2027-01-31")); got != 36 {
		t.Errorf("lunar ordinal = %d", got)
	}
	if got := (Rule{Freq: Yearly, Month: 1, Day: 1}).Ordinal(date("2027-01-01")); got != 0 {
		t.Errorf("ordinal without since = %d", got)
	}
}

func TestValidate(t *testing.T) {
	bad := []Rule{
		{},
		{Freq: "daily"},
		{Freq: Weekly, Weekday: 8},
		{Freq: Weekly},
		{Freq: Monthly, Day: 32},
		{Freq: Monthly, Calendar: LunarCal, Day: 31},
		{Freq: Yearly, Month: 13, Day: 1},
		{Freq: Yearly, Month: 2},
		{Freq: Once},
		{Freq: Once, Calendar: LunarCal, Date: ptr(date("2026-01-01"))},
		{Freq: Weekly, Calendar: LunarCal, Weekday: 1},
		{Freq: Monthly, Day: 1, Interval: 3},
		{Freq: Yearly, Calendar: LunarCal, Month: 1, Day: 1, Interval: 2, Date: ptr(date("2026-01-01"))},
		{Freq: Yearly, Calendar: "julian", Month: 1, Day: 1},
		{Freq: Monthly, Day: 1, Interval: 1000, Date: ptr(date("2026-01-01"))},
	}
	for _, rule := range bad {
		if err := rule.Validate(); err == nil {
			t.Errorf("%+v passed validation", rule)
		}
		if _, ok := rule.Next(date("2026-11-16")); ok {
			t.Errorf("%+v produced an occurrence", rule)
		}
	}
	good := []Rule{
		{Freq: Weekly, Weekday: 1},
		{Freq: Weekly, Interval: 2, Date: ptr(date("2026-11-20"))},
		{Freq: Monthly, Day: 31},
		{Freq: Monthly, Calendar: LunarCal, Day: 30},
		{Freq: Yearly, Calendar: Solar, Month: 2, Day: 29},
		{Freq: Yearly, Calendar: LunarCal, Month: 12, Day: 30},
		{Freq: Once, Date: ptr(date("2031-05-01"))},
	}
	for _, rule := range good {
		if err := rule.Validate(); err != nil {
			t.Errorf("%+v: %v", rule, err)
		}
	}
}

func TestLunarOf(t *testing.T) {
	got := LunarOf(date("2026-12-24"))
	if got.Year != 2026 || got.Month != 11 || got.Day != 16 || got.Leap || got.Label != "冬月十六" {
		t.Errorf("LunarOf(2026-12-24) = %+v", got)
	}
	// 2025 has a leap sixth month, from 2025-07-25 to 2025-08-22.
	leap := LunarOf(date("2025-08-01"))
	if !leap.Leap || leap.Month != 6 || leap.Label != "闰六月初八" {
		t.Errorf("LunarOf(2025-08-01) = %+v", leap)
	}
}

func TestDate(t *testing.T) {
	d := date("2026-11-16")
	if d.Weekday() != 1 {
		t.Errorf("2026-11-16 is a Monday, got %d", d.Weekday())
	}
	if date("2026-11-22").Weekday() != 7 {
		t.Error("Sunday must be 7")
	}
	if got := date("2027-03-01").Sub(date("2027-02-01")); got != 28 {
		t.Errorf("Sub = %d", got)
	}
	if got := d.AddDays(46); got.String() != "2027-01-01" {
		t.Errorf("AddDays = %s", got)
	}
	raw, _ := json.Marshal(struct {
		Day  Date  `json:"day"`
		None *Date `json:"none"`
	}{Day: d})
	if string(raw) != `{"day":"2026-11-16","none":null}` {
		t.Errorf("json = %s", raw)
	}
	var back struct{ Day Date }
	if err := json.Unmarshal([]byte(`{"day":"2026-02-30"}`), &back); err == nil {
		t.Error("an impossible date parsed")
	}
	if err := json.Unmarshal([]byte(`{"day":""}`), &back); err != nil || !back.Day.IsZero() {
		t.Errorf("empty date: %v %v", back.Day, err)
	}
}

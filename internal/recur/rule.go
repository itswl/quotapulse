package recur

import (
	"errors"
	"fmt"
)

// Frequencies a Rule can repeat at.
const (
	Once    = "once"
	Weekly  = "weekly"
	Monthly = "monthly"
	Yearly  = "yearly"
)

// Calendars a Rule can be expressed in.
const (
	Solar     = "solar"
	LunarCal  = "lunar"
	maxPeriod = 100 // upper bound for Interval; anything larger is a typo
)

// Rule says which days a reminder falls on.
//
// With Interval 1 a rule is a pattern ("the 15th of every month"), and needs no
// start date. With a larger interval it needs an anchor to count from, which
// is Date.
type Rule struct {
	Freq     string `json:"freq"`
	Calendar string `json:"calendar,omitempty"` // solar when empty

	// Date is the day itself for a one-off, and the anchor when Interval > 1.
	Date *Date `json:"date,omitempty"`

	Weekday int `json:"weekday,omitempty"` // weekly: 1 Monday … 7 Sunday
	Month   int `json:"month,omitempty"`   // yearly: 1-12
	Day     int `json:"day,omitempty"`     // monthly and yearly: 1-31, or 1-30 on the lunar calendar

	Interval int `json:"interval,omitempty"` // every N weeks, months or years; 1 when zero

	// Since is the year it all started — a birth year, the year a contract was
	// signed. It only feeds Ordinal ("the 30th").
	Since int `json:"since,omitempty"`
}

func (r Rule) lunar() bool { return r.Calendar == LunarCal }

func (r Rule) every() int {
	if r.Interval < 1 {
		return 1
	}
	return r.Interval
}

// Validate reports the first thing wrong with the rule.
func (r Rule) Validate() error {
	switch r.Calendar {
	case "", Solar, LunarCal:
	default:
		return fmt.Errorf("calendar must be solar or lunar, not %q", r.Calendar)
	}
	if r.Interval < 0 || r.Interval > maxPeriod {
		return fmt.Errorf("interval must be between 1 and %d", maxPeriod)
	}
	if r.every() > 1 {
		if r.lunar() {
			return errors.New("a lunar rule repeats every month or every year; interval is not supported")
		}
		if r.Date == nil || r.Date.IsZero() {
			return errors.New("an interval above 1 needs a start date to count from")
		}
	}
	maxDay := 31
	if r.lunar() {
		maxDay = 30
	}
	switch r.Freq {
	case Once:
		if r.Date == nil || r.Date.IsZero() {
			return errors.New("a one-off needs a date")
		}
		if r.lunar() {
			return errors.New("a one-off is a plain date; convert the lunar date first")
		}
	case Weekly:
		if r.lunar() {
			return errors.New("the lunar calendar has no weeks")
		}
		if r.Weekday < 1 || r.Weekday > 7 {
			if r.every() > 1 && r.Weekday == 0 {
				break // the anchor's weekday stands in
			}
			return errors.New("weekday must be 1 (Monday) to 7 (Sunday)")
		}
	case Monthly:
		if r.Day < 1 || r.Day > maxDay {
			return fmt.Errorf("day must be between 1 and %d", maxDay)
		}
	case Yearly:
		if r.Month < 1 || r.Month > 12 {
			return errors.New("month must be between 1 and 12")
		}
		if r.Day < 1 || r.Day > maxDay {
			return fmt.Errorf("day must be between 1 and %d", maxDay)
		}
	default:
		return fmt.Errorf("freq must be once, weekly, monthly or yearly, not %q", r.Freq)
	}
	return nil
}

// Next is the first occurrence on or after from. It is false only for a
// one-off whose day has passed, or for a rule that fails Validate.
func (r Rule) Next(from Date) (Date, bool) {
	if r.Validate() != nil {
		return Date{}, false
	}
	switch r.Freq {
	case Once:
		if r.Date.Before(from) {
			return Date{}, false
		}
		return *r.Date, true
	case Weekly:
		return r.nextWeekly(from), true
	case Monthly:
		if r.lunar() {
			return nextLunarMonthly(r.Day, from), true
		}
		return r.nextMonthly(from), true
	default:
		if r.lunar() {
			return nextLunarYearly(r.Month, r.Day, from)
		}
		return r.nextYearly(from), true
	}
}

// Prev is the last occurrence strictly before the given day.
func (r Rule) Prev(before Date) (Date, bool) {
	if r.Validate() != nil {
		return Date{}, false
	}
	if r.Freq == Once {
		if r.Date.Before(before) {
			return *r.Date, true
		}
		return Date{}, false
	}
	// Step back further than one full period, then walk forward. Simple, and
	// it cannot disagree with Next about where the occurrences are.
	current, ok := r.Next(before.AddDays(-(r.periodDays() + 1)))
	if !ok || !current.Before(before) {
		return Date{}, false
	}
	for {
		next, ok := r.Next(current.AddDays(1))
		if !ok || !next.Before(before) {
			return current, true
		}
		current = next
	}
}

// After is the occurrence that follows the given one.
func (r Rule) After(occurrence Date) (Date, bool) {
	return r.Next(occurrence.AddDays(1))
}

// periodDays is an upper bound on the gap between two occurrences.
func (r Rule) periodDays() int {
	switch r.Freq {
	case Weekly:
		return 7 * r.every()
	case Monthly:
		return 31 * r.every()
	default:
		return 385 * r.every() // a lunar leap year is 384 days
	}
}

// Ordinal is how many years lie between Since and the occurrence: the age at
// a birthday, the year of a contract. Zero when the rule has no Since or is
// not yearly.
func (r Rule) Ordinal(occurrence Date) int {
	if r.Since <= 0 || r.Freq != Yearly {
		return 0
	}
	year := occurrence.Y
	if r.lunar() {
		year = LunarOf(occurrence).Year
	}
	if year <= r.Since {
		return 0
	}
	return year - r.Since
}

func (r Rule) nextWeekly(from Date) Date {
	every := r.every()
	if every == 1 {
		return from.AddDays((r.Weekday - from.Weekday() + 7) % 7)
	}
	weekday := r.Weekday
	if weekday == 0 {
		weekday = r.Date.Weekday()
	}
	first := r.Date.AddDays((weekday - r.Date.Weekday() + 7) % 7)
	if !first.Before(from) {
		return first
	}
	period := 7 * every
	steps := (from.Sub(first) + period - 1) / period
	return first.AddDays(steps * period)
}

func (r Rule) nextMonthly(from Date) Date {
	every := r.every()
	if every == 1 {
		if candidate := clamp(from.Y, from.M, r.Day); !candidate.Before(from) {
			return candidate
		}
		y, m := addMonths(from.Y, from.M, 1)
		return clamp(y, m, r.Day)
	}
	start := r.Date.Y*12 + r.Date.M - 1
	at := func(step int) Date {
		index := start + step*every
		return clamp(index/12, index%12+1, r.Day)
	}
	step := 0
	if gap := from.Y*12 + from.M - 1 - start; gap > 0 {
		step = (gap + every - 1) / every
	}
	if candidate := at(step); !candidate.Before(from) {
		return candidate
	}
	return at(step + 1)
}

func (r Rule) nextYearly(from Date) Date {
	every := r.every()
	if every == 1 {
		if candidate := clamp(from.Y, r.Month, r.Day); !candidate.Before(from) {
			return candidate
		}
		return clamp(from.Y+1, r.Month, r.Day)
	}
	step := 0
	if gap := from.Y - r.Date.Y; gap > 0 {
		step = (gap + every - 1) / every
	}
	if candidate := clamp(r.Date.Y+step*every, r.Month, r.Day); !candidate.Before(from) {
		return candidate
	}
	return clamp(r.Date.Y+(step+1)*every, r.Month, r.Day)
}

// nextLunarYearly finds the next Gregorian day for a lunar month and day. A
// lunar year starts in late January or February, so the lunar year before
// the one containing from can still hold the answer's predecessor — start
// there and take the first candidate that is not in the past.
func nextLunarYearly(month, day int, from Date) (Date, bool) {
	lunarYear := LunarOf(from).Year
	for year := lunarYear - 1; year <= lunarYear+2; year++ {
		if candidate, ok := lunarToSolar(year, month, day); ok && !candidate.Before(from) {
			return candidate, true
		}
	}
	return Date{}, false
}

// nextLunarMonthly finds the next day-of-lunar-month, leap months included:
// 初一 and 十五 come round in a leap month like in any other.
func nextLunarMonthly(day int, from Date) Date {
	today := LunarOf(from)
	length := lunarMonthLength(today)
	if target := min(day, length); today.Day <= target {
		return from.AddDays(target - today.Day)
	}
	first := from.AddDays(length - today.Day + 1) // the 1st of the next lunar month
	return first.AddDays(min(day, lunarMonthLength(LunarOf(first))) - 1)
}

func addMonths(y, m, n int) (int, int) {
	index := y*12 + m - 1 + n
	return index / 12, index%12 + 1
}

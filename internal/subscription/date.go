// Package subscription provides the package implementation.
//
// Implementation note.
// Implementation note.
package subscription

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/6tail/lunar-go/calendar"
	"github.com/itswl/quotapulse/internal/model"
)

// Implementation note.
//
// Implementation note.
func SplitMMDD(renewalDay int) (month, day int, ok bool) {
	if renewalDay <= 31 {
		return 0, 0, false
	}
	month, day = renewalDay/100, renewalDay%100
	if month >= 1 && month <= 12 && day >= 1 && day <= 31 {
		return month, day, true
	}
	return 0, 0, false
}

var mmddPattern = regexp.MustCompile(`^(\d{1,2})\s*[-/月]\s*(\d{1,2})\s*日?$`)

// Implementation note.
//
// Implementation note.
// Implementation note.
func CoerceRenewalDay(value string, cycleType string) (int, bool) {
	text := strings.TrimSpace(value)
	if matched := mmddPattern.FindStringSubmatch(text); matched != nil {
		month, _ := strconv.Atoi(matched[1])
		day, _ := strconv.Atoi(matched[2])
		if cycleType == model.CycleYearly || cycleType == model.CycleLunarYearly {
			return month*100 + day, true
		}
		return day, true
	}
	if n, err := strconv.Atoi(text); err == nil {
		return n, true
	}
	return 0, false
}

// Implementation note.
// Implementation note.
func safeMonthDate(year int, month time.Month, day int, loc *time.Location) time.Time {
	maxDay := daysInMonth(year, month)
	if day > maxDay {
		day = maxDay
	}
	if day < 1 {
		day = 1
	}
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

func daysInMonth(year int, month time.Month) int {
	// Implementation note.
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// Implementation note.
func shiftMonth(base time.Time, months int, day int) time.Time {
	total := int(base.Month()) - 1 + months
	year := base.Year() + floorDiv(total, 12)
	month := time.Month(floorMod(total, 12) + 1)
	return safeMonthDate(year, month, day, base.Location())
}

// Implementation note.
func safeReplaceYear(base time.Time, year int) time.Time {
	return safeMonthDate(year, base.Month(), base.Day(), base.Location())
}

// Implementation note.
func floorDiv(a, b int) int {
	quotient := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		quotient--
	}
	return quotient
}

func floorMod(a, b int) int { return a - floorDiv(a, b)*b }

// Implementation note.
// Implementation note.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// Renewal is where one subscription stands on a given day.
type Renewal struct {
	// Next is the renewal that needs attention next. Once the upcoming renewal is
	// marked as paid, that is the one after it.
	Next time.Time
	// Days counts calendar days from today to Next.
	Days int
	// AlreadyRenewed means a renewal mark pays for the upcoming renewal, or for the one
	// that just passed while the next reminder window has not opened yet.
	AlreadyRenewed bool
}

// Evaluate places a subscription on its renewal calendar for today. A renewal mark pays
// for exactly one renewal (see schedule.covering): reminders for that renewal stop, and
// the one after it is reminded about as usual once its window opens. alertDaysBefore is
// the length of that window.
func Evaluate(cycleType string, renewalDay int, today time.Time, lastRenewed *time.Time, alertDaysBefore int) Renewal {
	today = startOfDay(today)
	cal := newSchedule(cycleType, renewalDay, lastRenewed)
	upcoming := cal.onOrAfter(today)
	result := Renewal{Next: upcoming}
	if lastRenewed != nil {
		covered := cal.covering(*lastRenewed, alertDaysBefore)
		switch {
		case !covered.Before(upcoming):
			// Paid ahead: the upcoming renewal is taken care of.
			result.AlreadyRenewed = true
			result.Next = cal.after(covered)
		case covered.Equal(cal.before(upcoming)):
			// Paid for the renewal that just passed; say so until the next reminder
			// window opens.
			result.AlreadyRenewed = daysBetween(today, upcoming) > alertDaysBefore
		}
	}
	result.Days = daysBetween(today, result.Next)
	return result
}

// NextRenewal returns the first scheduled renewal on or after today, before any renewal
// mark is applied. lastRenewed only matters for yearly items without a month and day,
// which renew on the anniversary of their last renewal.
func NextRenewal(cycleType string, renewalDay int, today time.Time, lastRenewed *time.Time) (days int, next time.Time) {
	today = startOfDay(today)
	next = newSchedule(cycleType, renewalDay, lastRenewed).onOrAfter(today)
	return daysBetween(today, next), next
}

// schedule is one subscription's renewal calendar. A renewal mark never moves it, with
// one exception: yearly items without a month and day renew on the anniversary of their
// last renewal, so the mark is their anchor.
type schedule struct {
	cycleType  string
	renewalDay int
	anchor     *time.Time
}

func newSchedule(cycleType string, renewalDay int, lastRenewed *time.Time) schedule {
	cal := schedule{cycleType: cycleType, renewalDay: renewalDay}
	if cycleType == model.CycleYearly && lastRenewed != nil {
		if _, _, ok := SplitMMDD(renewalDay); !ok {
			anchor := startOfDay(*lastRenewed)
			cal.anchor = &anchor
		}
	}
	return cal
}

// onOrAfter returns the first renewal on or after day.
func (s schedule) onOrAfter(day time.Time) time.Time {
	day = startOfDay(day)
	switch s.cycleType {
	case model.CycleWeekly:
		ahead := s.renewalDay - isoWeekday(day)
		if ahead < 0 {
			ahead += 7
		}
		return day.AddDate(0, 0, ahead)
	case model.CycleYearly:
		if month, dom, ok := SplitMMDD(s.renewalDay); ok {
			candidate := safeMonthDate(day.Year(), time.Month(month), dom, day.Location())
			if candidate.Before(day) {
				candidate = safeMonthDate(day.Year()+1, time.Month(month), dom, day.Location())
			}
			return candidate
		}
		if s.anchor != nil {
			candidate := safeReplaceYear(*s.anchor, day.Year())
			if candidate.Before(day) {
				candidate = safeReplaceYear(*s.anchor, day.Year()+1)
			}
			return candidate
		}
		// No month and day and no renewal to anchor on: the date is unknown, so it stays
		// a year out and never enters a reminder window.
		return safeReplaceYear(day, day.Year()+1)
	case model.CycleLunarYearly:
		return nextLunarYearlyDate(s.renewalDay, day)
	default: // monthly; a day past the end of a month falls back to its last day
		months := 0
		if day.Day() > s.renewalDay {
			months = 1
		}
		return shiftMonth(day, months, s.renewalDay)
	}
}

// before returns the renewal one cycle before next.
func (s schedule) before(next time.Time) time.Time {
	switch s.cycleType {
	case model.CycleWeekly:
		return next.AddDate(0, 0, -7)
	case model.CycleYearly:
		if month, dom, ok := SplitMMDD(s.renewalDay); ok {
			return safeMonthDate(next.Year()-1, time.Month(month), dom, next.Location())
		}
		if s.anchor != nil {
			return safeReplaceYear(*s.anchor, next.Year()-1)
		}
		return safeReplaceYear(next, next.Year()-1)
	case model.CycleLunarYearly:
		return previousLunarYearlyDate(s.renewalDay, next)
	default:
		return shiftMonth(next, -1, s.renewalDay)
	}
}

// after returns the renewal one cycle after occurrence.
func (s schedule) after(occurrence time.Time) time.Time {
	return s.onOrAfter(startOfDay(occurrence).AddDate(0, 0, 1))
}

// covering returns the renewal that a mark made on day pays for. A mark inside a
// renewal's reminder window, or on the renewal day itself, pays for that renewal.
// Otherwise the nearer renewal wins: the next one when paid early, the previous one when
// paid late.
func (s schedule) covering(day time.Time, alertDaysBefore int) time.Time {
	day = startOfDay(day)
	next := s.onOrAfter(day)
	ahead := daysBetween(day, next)
	if ahead <= alertDaysBefore {
		return next
	}
	if previous := s.before(next); daysBetween(previous, day) < ahead {
		return previous
	}
	return next
}

// daysBetween counts calendar days, so a daylight-saving change can't shave one off.
func daysBetween(from, to time.Time) int {
	a := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	b := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a).Hours() / 24)
}

// nextLunarYearlyDate returns the next Gregorian date for a recurring lunar
// MMDD. Lunar years do not begin on January 1, so use the lunar year that
// contains today instead of the Gregorian year.
func nextLunarYearlyDate(renewalDay int, today time.Time) time.Time {
	lunarToday := calendar.NewLunarFromDate(today)
	if candidate, ok := lunarDate(lunarToday.GetYear(), renewalDay, today.Location()); ok && !candidate.Before(today) {
		return candidate
	}
	if candidate, ok := lunarDate(lunarToday.GetYear()+1, renewalDay, today.Location()); ok {
		return candidate
	}
	return safeReplaceYear(today, today.Year()+1)
}

func lunarDate(lunarYear int, renewalDay int, loc *time.Location) (time.Time, bool) {
	month, day, ok := SplitMMDD(renewalDay)
	if !ok || month > 12 {
		return time.Time{}, false
	}
	lunarMonth := calendar.NewLunarYear(lunarYear).GetMonth(month)
	if lunarMonth == nil || day > lunarMonth.GetDayCount() {
		return time.Time{}, false
	}
	solar := calendar.NewLunarFromYmd(lunarYear, month, day).GetSolar()
	return time.Date(solar.GetYear(), time.Month(solar.GetMonth()), solar.GetDay(), 0, 0, 0, 0, loc), true
}

func previousLunarYearlyDate(renewalDay int, next time.Time) time.Time {
	lunarYear := calendar.NewLunarFromDate(next).GetYear()
	if previous, ok := lunarDate(lunarYear-1, renewalDay, next.Location()); ok {
		return previous
	}
	return next.AddDate(-1, 0, 0)
}

func isoWeekday(t time.Time) int {
	if w := int(t.Weekday()); w == 0 {
		return 7
	} else {
		return w
	}
}

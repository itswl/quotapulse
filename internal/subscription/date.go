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

// Implementation note.
//
// Implementation note.
// Implementation note.
func NextRenewal(cycleType string, renewalDay int, today time.Time, lastRenewed *time.Time) (days int, next time.Time) {
	today = startOfDay(today)

	switch cycleType {
	case "weekly":
		ahead := renewalDay - isoWeekday(today)
		if ahead < 0 { // operation,operation
			ahead += 7
		}
		next = today.AddDate(0, 0, ahead)
	case model.CycleYearly:
		next = nextYearlyDate(renewalDay, today, lastRenewed)
	case model.CycleLunarYearly:
		next = nextLunarYearlyDate(renewalDay, today)
	default: // monthly:operation,operation
		months := 0
		if today.Day() > renewalDay {
			months = 1
		}
		next = shiftMonth(today, months, renewalDay)
	}
	return int(next.Sub(today).Hours() / 24), next
}

// Implementation note.
func nextYearlyDate(renewalDay int, today time.Time, lastRenewed *time.Time) time.Time {
	if lastRenewed != nil {
		// Implementation note.
		candidate := safeReplaceYear(*lastRenewed, lastRenewed.Year()+1)
		for range 20 {
			if candidate.After(today) {
				return candidate
			}
			candidate = safeReplaceYear(candidate, candidate.Year()+1)
		}
	}

	month, day, ok := SplitMMDD(renewalDay)
	if !ok {
		// Implementation note.
		return safeReplaceYear(today, today.Year()+1)
	}
	// Implementation note.
	candidate := safeMonthDate(today.Year(), time.Month(month), day, today.Location())
	if !candidate.Before(today) {
		return candidate
	}
	return safeMonthDate(today.Year()+1, time.Month(month), day, today.Location())
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

// Implementation note.
func cycleStart(cycleType string, renewalDay int, today, next time.Time) time.Time {
	switch cycleType {
	case "weekly":
		return next.AddDate(0, 0, -7)
	case model.CycleYearly:
		return safeReplaceYear(next, next.Year()-1)
	case model.CycleLunarYearly:
		return previousLunarYearlyDate(renewalDay, next)
	default: // monthly:operation
		months := 0
		if today.Day() < renewalDay {
			months = -1
		}
		return shiftMonth(today, months, renewalDay)
	}
}

func isoWeekday(t time.Time) int {
	if w := int(t.Weekday()); w == 0 {
		return 7
	} else {
		return w
	}
}

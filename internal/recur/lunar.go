package recur

import (
	"github.com/6tail/lunar-go/calendar"
)

// The Chinese lunisolar calendar. Conversion tables come from lunar-go; this
// file only adds the conventions a reminder needs on top of raw conversion:
//
//   - A day past the month's end falls back to its last day. 腊月三十 in a
//     year whose 腊月 has 29 days is still New Year's Eve, not "no such day".
//   - A rule names a regular month. Someone born in a leap month keeps the
//     same-numbered regular month, which is how such birthdays are observed.

// lunarMonthDays is the length of a regular lunar month, or 0 when the year
// has no such month.
func lunarMonthDays(lunarYear, month int) int {
	m := calendar.NewLunarYear(lunarYear).GetMonth(month)
	if m == nil {
		return 0
	}
	return m.GetDayCount()
}

// lunarToSolar converts a lunar month and day of a lunar year to a Gregorian
// date, clamping the day to the month's length.
func lunarToSolar(lunarYear, month, day int) (Date, bool) {
	days := lunarMonthDays(lunarYear, month)
	if days == 0 || day < 1 {
		return Date{}, false
	}
	if day > days {
		day = days
	}
	solar := calendar.NewLunarFromYmd(lunarYear, month, day).GetSolar()
	return Date{solar.GetYear(), solar.GetMonth(), solar.GetDay()}, true
}

// Lunar is a Gregorian date seen through the lunar calendar.
type Lunar struct {
	Year  int  `json:"year"`
	Month int  `json:"month"` // 1-12
	Day   int  `json:"day"`
	Leap  bool `json:"leap"` // a leap month repeats the number of the month before it
	// Label is the traditional spelling, e.g. 冬月十六 or 闰六月初三.
	Label string `json:"label"`
}

// LunarOf converts a Gregorian date.
func LunarOf(d Date) Lunar {
	l := calendar.NewSolarFromYmd(d.Y, d.M, d.D).GetLunar()
	month := l.GetMonth()
	leap := month < 0
	if leap {
		month = -month
	}
	return Lunar{
		Year:  l.GetYear(),
		Month: month,
		Day:   l.GetDay(),
		Leap:  leap,
		Label: l.GetMonthInChinese() + "月" + l.GetDayInChinese(),
	}
}

// lunarMonthLength is the length of the lunar month that contains l, leap
// months included.
func lunarMonthLength(l Lunar) int {
	month := l.Month
	if l.Leap {
		month = -month
	}
	if m := calendar.NewLunarYear(l.Year).GetMonth(month); m != nil {
		return m.GetDayCount()
	}
	return 30
}

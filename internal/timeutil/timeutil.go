// Package timeutil parses schedule-time expressions for jobs and computes
// the next trigger time.
//
// Pure functions with no dependencies inside the project (config and
// scheduler both use it — no cycles allowed). Weekdays always use ISO
// numbers: 1=Monday … 7=Sunday.
package timeutil

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ClockTime is a time of day.
type ClockTime struct {
	Hour   int
	Minute int
}

func (c ClockTime) String() string { return fmt.Sprintf("%02d:%02d", c.Hour, c.Minute) }

func (c ClockTime) before(other ClockTime) bool {
	return c.Hour < other.Hour || (c.Hour == other.Hour && c.Minute < other.Minute)
}

// These values mean "disable the task".
var offValues = map[string]bool{
	"off": true, "none": true, "disabled": true, "false": true, "0": true, "-": true,
}

var timePattern = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)

// Weekday spellings; values are ISO numbers.
var weekdayNames = map[string]int{
	"mon": 1, "monday": 1, "周一": 1, "一": 1,
	"tue": 2, "tues": 2, "tuesday": 2, "周二": 2, "二": 2,
	"wed": 3, "wednesday": 3, "周三": 3, "三": 3,
	"thu": 4, "thur": 4, "thurs": 4, "thursday": 4, "周四": 4, "四": 4,
	"fri": 5, "friday": 5, "周五": 5, "五": 5,
	"sat": 6, "saturday": 6, "周六": 6, "六": 6,
	"sun": 7, "sunday": 7, "周日": 7, "周天": 7, "日": 7, "天": 7,
}

// ParseDailyTimes parses "09:00,15:30" into a deduplicated, sorted time list;
// off / none mean disabled. Malformed input errors, so a bad config fails at
// startup.
func ParseDailyTimes(text string) ([]ClockTime, error) {
	raw := strings.TrimSpace(text)
	if raw == "" || offValues[strings.ToLower(raw)] {
		return nil, nil
	}

	seen := make(map[ClockTime]bool)
	var result []ClockTime
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		matched := timePattern.FindStringSubmatch(part)
		if matched == nil {
			return nil, fmt.Errorf("invalid time format %q; use HH:MM and separate multiple times with commas", part)
		}
		hour, _ := strconv.Atoi(matched[1])
		minute, _ := strconv.Atoi(matched[2])
		if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
			return nil, fmt.Errorf("time is out of range: %q", part)
		}
		c := ClockTime{Hour: hour, Minute: minute}
		if !seen[c] {
			seen[c] = true
			result = append(result, c)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].before(result[j]) })
	return result, nil
}

// ParseWeekdays parses "Mon,Thu" / "周一" into {1,4}; empty means no weekday restriction.
func ParseWeekdays(text string) (map[int]bool, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	result := make(map[int]bool)
	for _, part := range strings.Split(strings.ReplaceAll(text, "、", ","), ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		if n, err := strconv.Atoi(part); err == nil {
			if n < 1 || n > 7 {
				return nil, fmt.Errorf("invalid weekday %q; use Mon or an ISO weekday number", part)
			}
			result[n] = true
			continue
		}
		day, ok := weekdayNames[part]
		if !ok {
			return nil, fmt.Errorf("invalid weekday %q; use Mon or an ISO weekday number", part)
		}
		result[day] = true
	}
	return result, nil
}

// ParseWeeklySchedule parses "Mon 09:00" into ({1}, [09:00]); omitting the
// weekday means daily, off means disabled. Weekdays and times are separated by a
// space, each may list several comma-separated values, e.g. "Mon,Thu
// 09:00,18:00".
func ParseWeeklySchedule(text string) (map[int]bool, []ClockTime, error) {
	raw := strings.TrimSpace(text)
	if raw == "" || offValues[strings.ToLower(raw)] {
		return nil, nil, nil
	}
	parts := strings.SplitN(raw, " ", 2)
	if len(parts) == 1 {
		times, err := ParseDailyTimes(parts[0])
		return nil, times, err
	}
	weekdays, err := ParseWeekdays(parts[0])
	if err != nil {
		return nil, nil, err
	}
	times, err := ParseDailyTimes(strings.TrimSpace(parts[1]))
	return weekdays, times, err
}

var weekdayLabels = []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

// Describe renders a schedule in plain words for the self-check output and /api/jobs.
func Describe(times []ClockTime, weekdays map[int]bool) string {
	if len(times) == 0 {
		return "Disabled"
	}
	clocks := make([]string, len(times))
	for i, t := range times {
		clocks[i] = t.String()
	}
	clock := strings.Join(clocks, " / ")
	if len(weekdays) == 0 {
		return "Daily " + clock
	}
	days := make([]int, 0, len(weekdays))
	for d := range weekdays {
		days = append(days, d)
	}
	sort.Ints(days)
	labels := make([]string, len(days))
	for i, d := range days {
		labels[i] = weekdayLabels[d-1]
	}
	return "Weekly " + strings.Join(labels, ", ") + " " + clock
}

// ISOWeekday returns 1=Monday … 7=Sunday.
func ISOWeekday(t time.Time) int {
	if w := int(t.Weekday()); w == 0 {
		return 7
	} else {
		return w
	}
}

// NextOccurrence returns the first fire time strictly after now. With a non-empty
// weekdays it fires only on those days. The result shares now's timezone.
func NextOccurrence(now time.Time, times []ClockTime, weekdays map[int]bool) (time.Time, bool) {
	if len(times) == 0 {
		return time.Time{}, false
	}
	ordered := append([]ClockTime(nil), times...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].before(ordered[j]) })

	for offset := 0; offset < 8; offset++ { // Search at most one week; a matching day must exist.
		day := now.AddDate(0, 0, offset)
		if len(weekdays) > 0 && !weekdays[ISOWeekday(day)] {
			continue
		}
		for _, c := range ordered {
			candidate := time.Date(day.Year(), day.Month(), day.Day(), c.Hour, c.Minute, 0, 0, now.Location())
			if candidate.After(now) {
				return candidate, true
			}
		}
	}
	return time.Time{}, false
}

// UTCISO renders a time as a Z-suffixed ISO string; a nil time returns nil.
func UTCISO(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format("2006-01-02T15:04:05.999999Z")
	return &s
}

// NowISO is the current time's UTC ISO string.
func NowISO() string { return time.Now().UTC().Format("2006-01-02T15:04:05.999999Z") }

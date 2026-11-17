package core

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Settings are the few things a person decides once. They live in the
// database and are edited from the dashboard; nothing here needs a restart.
type Settings struct {
	// Timezone decides what "today" is: day boundaries for spend, the
	// reminder time, quiet hours. An IANA name.
	Timezone string `json:"timezone"`
	// Language is the language notifications are written in: zh or en.
	Language string `json:"language"`
	// BaseCurrency is what monetary totals are converted to.
	BaseCurrency string `json:"base_currency"`
	// FX holds manual rates: one unit of the key is worth this much base.
	FX map[string]float64 `json:"fx"`

	// SampleSeconds is how often accounts are sampled unless they say otherwise.
	SampleSeconds int `json:"sample_seconds"`
	// RunwayDays is the default lead time for "running out" warnings.
	RunwayDays float64 `json:"runway_days"`
	// Anomaly turns burst detection on by default.
	Anomaly bool `json:"anomaly"`

	// ReminderTime is the local time of day date reminders go out, HH:MM.
	ReminderTime string `json:"reminder_time"`
	// Quiet holds back anything that is not critical during the night.
	Quiet QuietHours `json:"quiet"`
	// Digest is the periodic summary.
	Digest Digest `json:"digest"`
	// RenotifyHours repeats an unacknowledged critical incident; 0 never repeats.
	RenotifyHours int `json:"renotify_hours"`
	// NotifyResolved sends a message when something recovers.
	NotifyResolved bool `json:"notify_resolved"`

	// RetentionDays is how long readings and closed incidents are kept.
	RetentionDays int `json:"retention_days"`
	// UpcomingDays is how far ahead the overview looks for dates.
	UpcomingDays int `json:"upcoming_days"`
	// PublicURL is where the dashboard is reachable; notifications link to it.
	PublicURL string `json:"public_url"`
}

// QuietHours is a daily window in local time. It may cross midnight.
type QuietHours struct {
	Enabled bool   `json:"enabled"`
	Start   string `json:"start"`
	End     string `json:"end"`
}

// Digest schedules the summary: a weekday (1 Monday … 7 Sunday), or 0 for
// every day, at a local time.
type Digest struct {
	Enabled bool   `json:"enabled"`
	Weekday int    `json:"weekday"`
	Time    string `json:"time"`
}

// DefaultSettings is what a fresh install starts with.
func DefaultSettings() Settings {
	return Settings{
		Timezone:       "UTC",
		Language:       "en",
		BaseCurrency:   "USD",
		FX:             map[string]float64{},
		SampleSeconds:  1800,
		RunwayDays:     7,
		Anomaly:        true,
		ReminderTime:   "09:00",
		Quiet:          QuietHours{Enabled: true, Start: "23:00", End: "08:00"},
		Digest:         Digest{Enabled: true, Weekday: 1, Time: "09:00"},
		RenotifyHours:  24,
		NotifyResolved: true,
		RetentionDays:  730,
		UpcomingDays:   60,
	}
}

// Limits on what Settings will accept.
const (
	MinSampleSeconds = 60
	MaxSampleSeconds = 24 * 3600
)

// Validate reports everything wrong with the settings at once, so a form can
// show all of it.
func (s Settings) Validate() error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if _, err := time.LoadLocation(s.Timezone); err != nil || s.Timezone == "" {
		add("timezone: %q is not a known IANA time zone", s.Timezone)
	}
	if s.Language != "zh" && s.Language != "en" {
		add("language: must be zh or en")
	}
	if !IsCurrency(s.BaseCurrency) {
		add("base_currency: must be a three-letter code such as USD")
	}
	for code, rate := range s.FX {
		if !IsCurrency(code) {
			add("fx: %q is not a three-letter currency code", code)
		}
		if rate <= 0 {
			add("fx: the rate for %s must be positive", code)
		}
	}
	if s.SampleSeconds < MinSampleSeconds || s.SampleSeconds > MaxSampleSeconds {
		add("sample_seconds: must be between %d and %d", MinSampleSeconds, MaxSampleSeconds)
	}
	if s.RunwayDays < 0 || s.RunwayDays > 365 {
		add("runway_days: must be between 0 and 365")
	}
	if _, err := ParseClock(s.ReminderTime); err != nil {
		add("reminder_time: %v", err)
	}
	if _, err := ParseClock(s.Quiet.Start); err != nil && s.Quiet.Enabled {
		add("quiet.start: %v", err)
	}
	if _, err := ParseClock(s.Quiet.End); err != nil && s.Quiet.Enabled {
		add("quiet.end: %v", err)
	}
	if s.Digest.Weekday < 0 || s.Digest.Weekday > 7 {
		add("digest.weekday: must be 0 (every day) or 1 (Monday) to 7 (Sunday)")
	}
	if _, err := ParseClock(s.Digest.Time); err != nil && s.Digest.Enabled {
		add("digest.time: %v", err)
	}
	if s.RenotifyHours < 0 || s.RenotifyHours > 24*30 {
		add("renotify_hours: must be between 0 and 720")
	}
	if s.RetentionDays < 30 || s.RetentionDays > 3650 {
		add("retention_days: must be between 30 and 3650")
	}
	if s.UpcomingDays < 1 || s.UpcomingDays > 366 {
		add("upcoming_days: must be between 1 and 366")
	}
	if s.PublicURL != "" && !strings.HasPrefix(s.PublicURL, "http://") && !strings.HasPrefix(s.PublicURL, "https://") {
		add("public_url: must start with http:// or https://")
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

// Location is the configured time zone, falling back to UTC.
func (s Settings) Location() *time.Location {
	if loc, err := time.LoadLocation(s.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

// Rate is how much base currency one unit is worth, and whether that is known.
func (s Settings) Rate(unit string) (float64, bool) {
	if unit == s.BaseCurrency {
		return 1, true
	}
	rate, ok := s.FX[unit]
	return rate, ok && rate > 0
}

// Clock is a time of day in minutes after midnight.
type Clock int

// ParseClock reads HH:MM.
func ParseClock(s string) (Clock, error) {
	hours, minutes, ok := strings.Cut(strings.TrimSpace(s), ":")
	h, errH := strconv.Atoi(hours)
	m, errM := strconv.Atoi(minutes)
	if !ok || errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 || len(minutes) != 2 {
		return 0, fmt.Errorf("%q is not a time of day, want HH:MM", s)
	}
	return Clock(h*60 + m), nil
}

// On is this time of day on the day of t, in t's location.
func (c Clock) On(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), int(c)/60, int(c)%60, 0, 0, t.Location())
}

// QuietUntil returns when the quiet window containing now ends, or the zero
// time when now is outside it. now must already be in the user's location.
func (q QuietHours) QuietUntil(now time.Time) time.Time {
	if !q.Enabled {
		return time.Time{}
	}
	start, errStart := ParseClock(q.Start)
	end, errEnd := ParseClock(q.End)
	if errStart != nil || errEnd != nil || start == end {
		return time.Time{}
	}
	current := Clock(now.Hour()*60 + now.Minute())
	if start < end {
		if current >= start && current < end {
			return end.On(now)
		}
		return time.Time{}
	}
	// the window crosses midnight
	if current >= start {
		return end.On(now.AddDate(0, 0, 1))
	}
	if current < end {
		return end.On(now)
	}
	return time.Time{}
}

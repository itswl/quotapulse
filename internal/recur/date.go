// Package recur answers one question: when does a date come around next?
//
// Everything in QuotaPulse that "comes due" — a renewal, a birthday, an
// expiry — is a Rule here. The package is pure: no clock, no storage, no time
// zones. Callers decide what "today" is and pass it in as a Date.
package recur

import (
	"encoding/json"
	"fmt"
	"time"
)

// Date is a calendar day. It deliberately carries no time and no zone: a
// birthday is the same day wherever the server happens to run.
type Date struct {
	Y, M, D int
}

// NewDate builds a date, normalizing overflow the way time.Date does.
func NewDate(y, m, d int) Date {
	return DateOf(time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC))
}

// DateOf is the calendar day of t in t's own location.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{y, int(m), d}
}

// ParseDate reads YYYY-MM-DD.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return Date{}, fmt.Errorf("invalid date %q, want YYYY-MM-DD", s)
	}
	return DateOf(t), nil
}

func (d Date) IsZero() bool { return d == Date{} }

func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Y, d.M, d.D)
}

// utc is the date at midnight UTC — the neutral form used for arithmetic.
func (d Date) utc() time.Time {
	return time.Date(d.Y, time.Month(d.M), d.D, 0, 0, 0, 0, time.UTC)
}

// In is midnight of this date in loc.
func (d Date) In(loc *time.Location) time.Time {
	return time.Date(d.Y, time.Month(d.M), d.D, 0, 0, 0, 0, loc)
}

func (d Date) AddDays(n int) Date { return DateOf(d.utc().AddDate(0, 0, n)) }

// Sub is the number of days from o to d; negative when d is earlier.
func (d Date) Sub(o Date) int {
	return int(d.utc().Sub(o.utc()).Hours() / 24)
}

func (d Date) Before(o Date) bool { return d.Compare(o) < 0 }
func (d Date) After(o Date) bool  { return d.Compare(o) > 0 }

func (d Date) Compare(o Date) int {
	switch {
	case d.Y != o.Y:
		return sign(d.Y - o.Y)
	case d.M != o.M:
		return sign(d.M - o.M)
	default:
		return sign(d.D - o.D)
	}
}

// Weekday is ISO: 1 is Monday, 7 is Sunday.
func (d Date) Weekday() int {
	if w := int(d.utc().Weekday()); w != 0 {
		return w
	}
	return 7
}

func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d *Date) UnmarshalJSON(raw []byte) error {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	if s == "" {
		*d = Date{}
		return nil
	}
	parsed, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// daysIn is the length of a Gregorian month.
func daysIn(y, m int) int {
	return time.Date(y, time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// clamp builds y-m-d, pulling a day past the month's end back to its last
// day: a rule on the 31st lands on Feb 28, not Mar 3.
func clamp(y, m, d int) Date {
	if last := daysIn(y, m); d > last {
		d = last
	}
	if d < 1 {
		d = 1
	}
	return Date{y, m, d}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

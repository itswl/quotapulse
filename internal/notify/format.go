package notify

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Number formatting is part of the external contract: people in the group eyeball
// digit counts to judge "is this number right". A lost thousands separator, or
// 20.0 rendered as 20, reads as a value change.

// formatAmount: thousands separator + two fixed decimals, like "1,234.50". Balances and thresholds in alerts all go through it.
func formatAmount(v float64) string {
	if s, ok := nonFinite(v); ok {
		return s
	}

	s := strconv.FormatFloat(v, 'f', 2, 64)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	whole, frac, _ := strings.Cut(s, ".")
	return sign + group(whole) + "." + frac
}

// group inserts a comma every three digits.
func group(digits string) string {
	if len(digits) <= 3 {
		return digits
	}
	var b strings.Builder
	head := len(digits) % 3
	if head > 0 {
		b.WriteString(digits[:head])
	}
	for i := head; i < len(digits); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(digits[i : i+3])
	}
	return b.String()
}

// formatFloat uses the shortest round-trip form, padding integers with ".0":
// a subscription amount of 20 joins text as "20.0".
//
// %g would be easier but switches to scientific notation above one million,
// and nobody wants to read "1.234567e+06" as an amount in the group.
// Scientific notation starts only at 1e16 here.
func formatFloat(v float64) string {
	if s, ok := nonFinite(v); ok {
		return s
	}

	sci := strconv.FormatFloat(v, 'e', -1, 64) // like "1.599e+01"
	_, expText, _ := strings.Cut(sci, "e")
	exp, err := strconv.Atoi(expText)
	if err == nil && (exp < -4 || exp >= 16) {
		return sci // outside this range nobody reads decimals; give scientific notation straight away
	}

	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// nonFinite picks out NaN / Inf and writes "nan" / "inf" / "-inf". Without
// this gate, the decimal-splitting logic above would emit half-formed output
// like "NaN.".
func nonFinite(v float64) (string, bool) {
	switch {
	case math.IsNaN(v):
		return "nan", true
	case math.IsInf(v, 1):
		return "inf", true
	case math.IsInf(v, -1):
		return "-inf", true
	}
	return "", false
}

// isoLocal writes local time in ISO 8601: no timezone, and the fractional part
// is dropped entirely when microseconds are zero. The custom webhook payload's
// timestamp uses exactly this format.
func isoLocal(t time.Time) string {
	if t.Nanosecond()/1000 == 0 {
		return t.Format("2006-01-02T15:04:05")
	}
	return t.Format("2006-01-02T15:04:05.000000")
}

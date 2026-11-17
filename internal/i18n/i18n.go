// Package i18n writes the things QuotaPulse says to people — notification
// text, mostly — in English and Chinese.
//
// The dashboard translates itself in the browser. What is here are the
// sentences that leave the server, where there is no browser to ask: a
// message in a chat group has to arrive already in the right language.
package i18n

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/itswl/quotapulse/internal/core"
	"github.com/itswl/quotapulse/internal/recur"
)

// Languages.
const (
	EN = "en"
	ZH = "zh"
)

// Lang picks between an English and a Chinese string.
type Lang string

// Of normalizes a language setting.
func Of(language string) Lang {
	if language == ZH {
		return ZH
	}
	return EN
}

// Pick returns the string for this language.
func (l Lang) Pick(en, zh string) string {
	if l == ZH {
		return zh
	}
	return en
}

// Sep joins a title's parts: "Low balance · DeepSeek".
const Sep = " · "

// Number formats a number with thousands separators and at most the given
// number of decimals, dropping trailing zeros when exact is false.
func Number(v float64, decimals int, exact bool) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "—"
	}
	text := strconv.FormatFloat(v, 'f', decimals, 64)
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	whole, frac, _ := strings.Cut(text, ".")
	if !exact {
		frac = strings.TrimRight(frac, "0")
	}
	var grouped strings.Builder
	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	out := grouped.String()
	if frac != "" {
		out += "." + frac
	}
	if negative && strings.Trim(out, "0.,") != "" {
		out = "-" + out
	}
	return out
}

// Amount formats a value in a meter's unit: money with two decimals, a
// percentage with at most one, anything else with at most two.
func Amount(v float64, unit string) string {
	switch {
	case unit == "%":
		return Number(v, 1, false) + "%"
	case core.IsCurrency(unit):
		return Number(v, 2, true) + " " + unit
	case unit == "":
		return Number(v, 2, false)
	default:
		return Number(v, 2, false) + " " + unit
	}
}

// Span says how long a duration is, in the two largest units that matter:
// "3 days", "1 h 20 min", "45 min".
func (l Lang) Span(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	minutes := int(math.Round(d.Minutes()))
	switch {
	case minutes < 1:
		return l.Pick("under a minute", "不到 1 分钟")
	case minutes < 60:
		return l.Pick(fmt.Sprintf("%d min", minutes), fmt.Sprintf("%d 分钟", minutes))
	case minutes < 48*60:
		hours, rest := minutes/60, minutes%60
		if rest == 0 || hours >= 12 {
			return l.Pick(fmt.Sprintf("%d h", hours), fmt.Sprintf("%d 小时", hours))
		}
		return l.Pick(fmt.Sprintf("%d h %d min", hours, rest), fmt.Sprintf("%d 小时 %d 分钟", hours, rest))
	default:
		return l.Days(d.Hours() / 24)
	}
}

// Days says a number of days: one decimal under ten, whole days above, and
// "over a year" where precision would be pretence.
func (l Lang) Days(days float64) string {
	switch {
	case days >= 730:
		return l.Pick("over 2 years", "超过 2 年")
	case days >= 365:
		return l.Pick("over a year", "超过 1 年")
	case days >= 10:
		n := int(math.Round(days))
		return l.Pick(fmt.Sprintf("%d days", n), fmt.Sprintf("%d 天", n))
	default:
		text := Number(days, 1, false)
		if text == "1" {
			return l.Pick("1 day", "1 天")
		}
		return l.Pick(text+" days", text+" 天")
	}
}

// Until says when a date is, relative to today.
func (l Lang) Until(days int) string {
	switch {
	case days == 0:
		return l.Pick("today", "今天")
	case days == 1:
		return l.Pick("tomorrow", "明天")
	case days == 2:
		return l.Pick("in 2 days", "后天")
	case days > 0:
		return l.Pick(fmt.Sprintf("in %d days", days), fmt.Sprintf("%d 天后", days))
	case days == -1:
		return l.Pick("yesterday", "昨天")
	default:
		return l.Pick(fmt.Sprintf("%d days ago", -days), fmt.Sprintf("%d 天前", -days))
	}
}

var (
	weekdaysEN = []string{"", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	weekdaysZH = []string{"", "周一", "周二", "周三", "周四", "周五", "周六", "周日"}
	monthsEN   = []string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
)

// Date writes a calendar day with its weekday: "Thu, Dec 24" or "12月24日 周四".
// The year is added when it is not the reference year.
func (l Lang) Date(d recur.Date, referenceYear int) string {
	if l == ZH {
		text := fmt.Sprintf("%d月%d日 %s", d.M, d.D, weekdaysZH[d.Weekday()])
		if d.Y != referenceYear {
			text = fmt.Sprintf("%d年", d.Y) + text
		}
		return text
	}
	text := fmt.Sprintf("%s, %s %d", weekdaysEN[d.Weekday()], monthsEN[d.M], d.D)
	if d.Y != referenceYear {
		text += fmt.Sprintf(", %d", d.Y)
	}
	return text
}

// ShortDate writes a day without its weekday: "Dec 24" or "12月24日".
func (l Lang) ShortDate(d recur.Date) string {
	if l == ZH {
		return fmt.Sprintf("%d月%d日", d.M, d.D)
	}
	return fmt.Sprintf("%s %d", monthsEN[d.M], d.D)
}

// Clock writes a moment as a time of day, adding the date when it is not
// today: "14:20", "Nov 21 14:20".
func (l Lang) Clock(t, now time.Time) string {
	if recur.DateOf(t) == recur.DateOf(now) {
		return t.Format("15:04")
	}
	return l.ShortDate(recur.DateOf(t)) + " " + t.Format("15:04")
}

// Ordinal writes "30th" or "第 30 个".
func (l Lang) Ordinal(n int) string {
	if l == ZH {
		return fmt.Sprintf("第 %d 个", n)
	}
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

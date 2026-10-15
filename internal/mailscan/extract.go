package mailscan

import (
	"regexp"
	"strconv"
	"strings"
)

// unknownService is the placeholder used when no service name was recognized.
const unknownService = "Unknown service"

// The service name usually sits in brackets in the subject: 【阿里云】/ [AWS] /
// （腾讯云）/ (Azure). Try the four bracket styles in this order and stop at the
// first hit.
var servicePatterns = []*regexp.Regexp{
	regexp.MustCompile(`【(.+?)】`),
	regexp.MustCompile(`\[(.+?)\]`),
	regexp.MustCompile(`（(.+?)）`),
	regexp.MustCompile(`\((.+?)\)`),
}

// space is a wider whitespace class than \s: Go's \s only matches ASCII
// whitespace, while billing mails converted from HTML are full of non-breaking
// spaces (U+00A0) and ideographic spaces (U+3000). Without counting them,
// "余额： 12.50 元" fails to match as a whole and the amount is lost.
const space = `[\s\p{Z}]`

// Amount extraction rules in priority order: prefixed "balance/amount" matches
// are most trusted; the bare "number + 元" fallback misfires easily and comes
// last.
var amountPatterns = []*regexp.Regexp{
	regexp.MustCompile(`余额[：:]` + space + `*([0-9,]+\.?[0-9]*)` + space + `*元`),
	regexp.MustCompile(`金额[：:]` + space + `*([0-9,]+\.?[0-9]*)`),
	regexp.MustCompile(`([0-9,]+\.?[0-9]*)` + space + `*元`),
	regexp.MustCompile(`CNY` + space + `*([0-9,]+\.?[0-9]*)`),
}

// extractServiceInfo takes the service name from the subject brackets and the
// amount from subject plus body. Missing values are unknownService and nil — the
// amount is a pointer because "unrecognized" and "zero balance" differ.
func extractServiceInfo(subject, body string) (string, *float64) {
	service := unknownService
	for _, re := range servicePatterns {
		if m := re.FindStringSubmatch(subject); m != nil {
			service = m[1]
			break
		}
	}

	full := subject + "\n" + body
	for _, re := range amountPatterns {
		m := re.FindStringSubmatch(full)
		if m == nil {
			continue
		}
		amount, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
		if err != nil {
			// fake numbers like ",,," do not count for this rule; try the next one
			continue
		}
		return service, &amount
	}
	return service, nil
}

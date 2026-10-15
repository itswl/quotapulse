package provider

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// GLM Coding Plan exposes several quota windows. The dashboard value is the
// remaining percentage of the rolling five-hour window; longer windows reset
// on a different schedule and must not be used as the account balance.
//
// Window identity: a rolling five-hour window always resets within five
// hours, so nextResetTime is the reliable marker. The unit/number fields are
// an opaque enum — an earlier revision read unit=5 as "5 hours" and kept
// picking the monthly tool quota (search / web-reader / zread), which sits at
// 100% almost always and hides real exhaustion. TIME_LIMIT entries are tool
// time quotas and never the balance; TOKENS_LIMIT / CREDIT_LIMIT are.
var glmSpec = Spec{
	Key:         "glm",
	Name:        "GLM",
	DefaultType: "quota",
	URL:         "https://open.bigmodel.cn/api/monitor/usage/quota/limit",
	Headers:     map[string]string{"Accept": "application/json"},
	Check: func(data map[string]any) error {
		if !truthy(data["success"]) || !glmCodeOK(data["code"]) {
			return fmt.Errorf("API returned an error: %s", messageOr(data, "msg", "Unknown error"))
		}
		return nil
	},
	Extract: func(data map[string]any) (float64, error) {
		limits, ok := Dig(data, "data", "limits").([]any)
		if !ok {
			return 0, errors.New("Could not parse data.limits field")
		}

		now := time.Now()
		var rolling, balance, legacy []float64
		for _, item := range limits {
			limit := Object(item)
			if limit == nil {
				continue
			}
			percent, ok := glmRemainingPercent(limit)
			if !ok {
				continue
			}
			switch {
			case glmBalanceWindow(limit) && glmResetsWithin(limit, now, rollingWindowSlack):
				rolling = append(rolling, percent)
			case glmBalanceWindow(limit):
				balance = append(balance, percent)
			case !glmHasWindowMetadata(limit):
				legacy = append(legacy, percent)
			}
		}
		// A window that resets within hours is the rolling one; take the
		// lowest so the tightest limit surfaces. Without a readable reset
		// time, the lowest balance-type window is still the safest answer.
		if len(rolling) > 0 {
			return round2(minFloat(rolling)), nil
		}
		if len(balance) > 0 {
			return round2(minFloat(balance)), nil
		}
		// Preserve compatibility with older responses that returned one
		// unlabelled limit, but never guess when several labelled windows exist.
		if len(legacy) == 1 && len(limits) == 1 {
			return round2(legacy[0]), nil
		}
		return 0, errors.New("data.limits contains no five-hour quota window")
	},
}

// rollingWindowSlack is how far out a reset may sit and still count as the
// rolling window. Five hours of window plus an hour of clock skew.
const rollingWindowSlack = 6 * time.Hour

// glmBalanceWindow reports whether the entry is a token/credit quota — the
// plan's balance — rather than a tool time quota.
func glmBalanceWindow(limit map[string]any) bool {
	switch limit["type"] {
	case "TOKENS_LIMIT", "CREDIT_LIMIT":
		return true
	}
	return false
}

// glmResetsWithin reports whether the window's reset timestamp falls inside
// the next d. nextResetTime may arrive in milliseconds or seconds.
func glmResetsWithin(limit map[string]any, now time.Time, d time.Duration) bool {
	value, ok := jsonNum(limit["nextResetTime"])
	if !ok || value <= 0 {
		return false
	}
	seconds := value / 1000
	if value < 1e12 { // already seconds
		seconds = value
	}
	reset := time.Unix(int64(seconds), 0).UTC()
	return reset.After(now) && reset.Sub(now) <= d
}

func glmHasWindowMetadata(limit map[string]any) bool {
	_, hasUnit := jsonNum(limit["unit"])
	_, hasNumber := jsonNum(limit["number"])
	return hasUnit || hasNumber
}

func minFloat(values []float64) float64 {
	minimum := math.Inf(1)
	for _, value := range values {
		if value < minimum {
			minimum = value
		}
	}
	return minimum
}

func glmCodeOK(code any) bool {
	if code == nil {
		return true
	}
	value, ok := jsonNum(code)
	return ok && value == 200
}

// glmRemainingPercent computes one window's remaining ratio. With totals and
// remaining it is exact; otherwise infer from the API's used-percent. Abnormal
// data clamps to 0-100 so no negative or over-full balances appear.
func glmRemainingPercent(limit map[string]any) (float64, bool) {
	usage, hasUsage := jsonNum(limit["usage"])
	remaining, hasRemaining := jsonNum(limit["remaining"])
	if hasUsage && usage > 0 && hasRemaining {
		return clampPercent(remaining / usage * 100), true
	}
	if percentage, ok := jsonNum(limit["percentage"]); ok {
		return clampPercent(100 - percentage), true
	}
	return 0, false
}

func clampPercent(v float64) float64 {
	return math.Max(0, math.Min(100, v))
}

func init() { RegisterSpec(glmSpec) }

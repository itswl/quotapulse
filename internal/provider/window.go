package provider

import (
	"math"
	"time"
)

// Quota-window helpers shared by the min_window extract mode — born in the
// GLM adapter, now part of the custom engine.

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

// glmHasWindowMetadata reports whether the entry carries unit/number labels.
func glmHasWindowMetadata(limit map[string]any) bool {
	_, hasUnit := jsonNum(limit["unit"])
	_, hasNumber := jsonNum(limit["number"])
	return hasUnit || hasNumber
}

func clampPercent(v float64) float64 {
	return math.Max(0, math.Min(100, v))
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

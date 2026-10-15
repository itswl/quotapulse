package provider

import (
	"errors"
	"fmt"
	"math"
)

// GLM Coding Plan exposes several quota windows. The dashboard value is the
// remaining percentage of the rolling five-hour window; longer windows reset
// on a different schedule and must not be used as the account balance.
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

		var fiveHour []float64
		var legacy []float64
		for _, item := range limits {
			limit := Object(item)
			if limit == nil {
				continue
			}
			percent, ok := glmRemainingPercent(limit)
			if !ok {
				continue
			}
			if glmIsFiveHourWindow(limit) {
				fiveHour = append(fiveHour, percent)
			} else if !glmHasWindowMetadata(limit) {
				legacy = append(legacy, percent)
			}
		}
		if len(fiveHour) > 0 {
			return round2(minFloat(fiveHour)), nil
		}
		// Preserve compatibility with older responses that returned one
		// unlabelled limit, but never guess when several labelled windows exist.
		if len(legacy) == 1 && len(limits) == 1 {
			return round2(legacy[0]), nil
		}
		return 0, errors.New("data.limits contains no five-hour quota window")
	},
}

func glmIsFiveHourWindow(limit map[string]any) bool {
	unit, hasUnit := jsonNum(limit["unit"])
	number, hasNumber := jsonNum(limit["number"])
	return hasUnit && hasNumber && unit == 5 && number == 1
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

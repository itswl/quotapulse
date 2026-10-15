package provider

import "errors"

// OpenRouter only reports cumulative top-ups and spend; net the two ourselves.
var openRouterSpec = Spec{
	Key:         "openrouter",
	Name:        "OpenRouter",
	DefaultType: "credits",
	URL:         "https://openrouter.ai/api/v1/credits",
	Extract: func(data map[string]any) (float64, error) {
		// both response shapes have been seen: fields under data, or flat at the top level
		scope := data
		if nested, ok := data["data"]; ok {
			scope = Object(nested)
		}
		total, ok := Num(scope["total_credits"])
		if !ok {
			return 0, errors.New("Could not parse total_credits field")
		}
		used, ok := Num(scope["total_usage"])
		if !ok {
			return 0, errors.New("Could not parse total_usage field")
		}
		return total - used, nil
	},
}

func init() { RegisterSpec(openRouterSpec) }

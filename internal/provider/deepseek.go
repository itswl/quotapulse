package provider

import "errors"

// A DeepSeek account may hold multi-currency sub-accounts; the balance is the CNY account total.
var deepseekSpec = Spec{
	Key:         "deepseek",
	Name:        "DeepSeek",
	DefaultType: "balance",
	URL:         "https://api.deepseek.com/user/balance",
	Headers:     map[string]string{"Accept": "application/json"},
	Extract: func(data map[string]any) (float64, error) {
		infos, ok := data["balance_infos"].([]any)
		if !ok || len(infos) == 0 {
			return 0, errors.New("Could not parse balance_infos field")
		}

		// prefer the CNY account, fall back to the first one without it
		chosen := Object(infos[0])
		for _, item := range infos {
			if obj := Object(item); obj != nil && Str(obj["currency"]) == "CNY" {
				chosen = obj
				break
			}
		}

		// in production this field is a string amount ("430.37")
		total, ok := Num(chosen["total_balance"])
		if !ok {
			return 0, errors.New("Could not parse total_balance field")
		}
		return total, nil
	},
}

func init() { RegisterSpec(deepseekSpec) }

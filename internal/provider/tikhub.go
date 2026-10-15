package provider

import "errors"

// TikHub reports the account balance in USD.
var tikhubSpec = Spec{
	Key:         "tikhub",
	Name:        "TikHub",
	DefaultType: "balance",
	URL:         "https://api.tikhub.dev/api/v1/tikhub/user/get_user_info",
	Currency:    "USD",
	Headers:     map[string]string{"accept": "application/json"},
	Extract: func(data map[string]any) (float64, error) {
		// the API changed shape: balance may sit under user_data, data or the top level; try in this order
		scope := data
		if v, ok := data["user_data"]; ok {
			scope = Object(v)
		} else if v, ok := data["data"]; ok {
			scope = Object(v)
		}
		balance, ok := Num(scope["balance"])
		if !ok {
			return 0, errors.New("Could not parse balance field")
		}
		return balance, nil
	},
}

func init() { RegisterSpec(tikhubSpec) }

package provider

import "errors"

// UniAPI's balance is a USD prepayment; unit=usd is the API's required explicit unit.
var uniapiSpec = Spec{
	Key:         "uniapi",
	Name:        "UniAPI",
	DefaultType: "credits",
	URL:         "https://api.uniapi.io/v1/billing/usage",
	Currency:    "USD",
	Params:      map[string]string{"unit": "usd"},
	Check: func(data map[string]any) error {
		if !truthy(data["success"]) {
			return errors.New("API returned success=false")
		}
		return nil
	},
	Extract: func(data map[string]any) (float64, error) {
		balance, ok := Num(Dig(data, "data", "balance"))
		if !ok {
			return 0, errors.New("Could not parse balance field")
		}
		return balance, nil
	},
}

func init() { RegisterSpec(uniapiSpec) }

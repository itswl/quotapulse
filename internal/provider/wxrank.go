package provider

import (
	"fmt"
	"regexp"
	"strconv"
)

// wxrank has no dedicated balance API; remaining credits sit inside the query API's msg text, e.g. "剩余263419余额".
var wxrankDigits = regexp.MustCompile(`\d+`)

var wxrankSpec = Spec{
	Key:         "wxrank",
	Name:        "WxRank",
	DefaultType: "credits",
	URL:         "https://data.wxrank.com/weixin/score",
	Auth:        AuthQuery,
	AuthParam:   "key",
	Check: func(data map[string]any) error {
		code, ok := jsonNum(data["code"])
		if !ok || code != 0 {
			return fmt.Errorf("API returned an error: %s", messageOr(data, "msg", "Unknown error"))
		}
		return nil
	},
	Extract: func(data map[string]any) (float64, error) {
		// grab the first number in msg
		msg := Str(data["msg"])
		if matched := wxrankDigits.FindString(msg); matched != "" {
			if value, err := strconv.ParseFloat(matched, 64); err == nil {
				return value, nil
			}
		}

		// fallback fields when msg has no numbers, tried in order: data is a number > data.score/credits > top-level score/credits
		switch raw := data["data"].(type) {
		case float64:
			return raw, nil
		case map[string]any:
			if value, ok := Num(orElse(raw["score"], raw["credits"])); ok {
				return value, nil
			}
		}
		if value, ok := Num(orElse(data["score"], data["credits"])); ok {
			return value, nil
		}
		return 0, fmt.Errorf("Could not parse balance: %s", msg)
	},
}

func init() { RegisterSpec(wxrankSpec) }

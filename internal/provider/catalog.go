package provider

import "sort"

// The built-in catalog. Each entry is an ordinary Definition — nothing here
// can do what a definition saved from the dashboard cannot.
//
// Checked says how far each entry was verified; catalog_test.go replays a
// response for every one of them.

var apiKey = Field{Key: "api_key", Label: "API Key", Secret: true}

var catalog = []Definition{
	{
		ID: "deepseek", Name: "DeepSeek",
		Homepage: "https://platform.deepseek.com",
		Fields:   []Field{apiKey},
		Request: Request{
			URL:  "https://api.deepseek.com/user/balance",
			Auth: Auth{Type: AuthBearer},
		},
		// One balance per currency the account holds.
		Extract: `
.balance_infos[]? | {
  key: ("balance-" + (.currency | ascii_downcase)),
  name: "Balance", name_zh: "余额",
  kind: "balance", unit: .currency,
  value: (.total_balance | num)
}`,
		Checked: CheckedLive,
	},
	{
		ID: "glm-coding", Name: "Zhipu GLM Coding Plan", NameZH: "智谱 GLM Coding Plan",
		Homepage: "https://bigmodel.cn",
		Notes:    "The token window and the monthly tool quota are separate meters, each with its own reset time.",
		NotesZH:  "Token 窗口和月度工具配额是两个独立的额度，各有自己的重置时间。",
		Fields:   []Field{apiKey},
		Request: Request{
			URL:  "https://open.bigmodel.cn/api/monitor/usage/quota/limit",
			Auth: Auth{Type: AuthBearer},
		},
		// unit is the window's time unit: 3 is hours and 5 is months, as seen
		// on the live API. Other values are passed through without a window
		// length, so no pace is computed from a guess.
		Extract: `
if .success != true then error(.msg // "the request was rejected") else
.data.limits[]? |
  (.unit // 0) as $unit | (.number // 1) as $n |
  (if $unit == 3 then "\($n)h" elif $unit == 5 then "\($n)mo" else "u\($unit)n\($n)" end) as $span |
  (if $unit == 3 then $n * 3600 elif $unit == 5 then $n * 2592000 else null end) as $window |
  if .type == "TOKENS_LIMIT" then {
    key: "tokens-\($span)",
    name: (if $unit == 3 then "\($n)-hour window" elif $unit == 5 then "Monthly tokens" else "Token quota" end),
    name_zh: (if $unit == 3 then "\($n) 小时窗口" elif $unit == 5 then "月度 Token" else "Token 配额" end),
    kind: "quota", unit: "%",
    value: (100 - (.percentage // 0)), limit: 100,
    resets_at: .nextResetTime, window: $window
  } elif .type == "TIME_LIMIT" then {
    key: "tools-\($span)",
    name: "MCP tools", name_zh: "MCP 工具",
    kind: "quota", unit: "calls",
    value: (.remaining // ((.usage // 0) - (.currentValue // 0))), limit: .usage,
    resets_at: .nextResetTime, window: $window
  } else empty end
end`,
		Checked: CheckedLive,
	},
	{
		ID: "openrouter", Name: "OpenRouter",
		Homepage: "https://openrouter.ai",
		Fields:   []Field{apiKey},
		Request: Request{
			URL:  "https://openrouter.ai/api/v1/credits",
			Auth: Auth{Type: AuthBearer},
		},
		// The API reports lifetime purchases and lifetime usage; what is left
		// is the difference.
		Extract: `
(.data // .) | {
  key: "credits", name: "Credits", name_zh: "额度",
  kind: "balance", unit: "USD",
  value: ((.total_credits | num) - (.total_usage | num))
}`,
		Checked: CheckedRecorded,
	},
	{
		ID: "siliconflow", Name: "SiliconFlow", NameZH: "硅基流动",
		Homepage: "https://siliconflow.cn",
		Fields:   []Field{apiKey},
		Request: Request{
			URL:  "https://api.siliconflow.cn/v1/user/info",
			Auth: Auth{Type: AuthBearer},
		},
		Extract: `
if (.status // true) != true then error(.message // "the request was rejected") else
.data | {
  key: "balance", name: "Balance", name_zh: "余额",
  kind: "balance", unit: "CNY",
  value: (.totalBalance | num)
} end`,
		Checked: CheckedDocs,
	},
	{
		ID: "moonshot", Name: "Moonshot AI (Kimi)", NameZH: "月之暗面 Kimi",
		Homepage: "https://platform.moonshot.cn",
		Fields:   []Field{apiKey},
		Request: Request{
			URL:  "https://api.moonshot.cn/v1/users/me/balance",
			Auth: Auth{Type: AuthBearer},
		},
		Extract: `
if (.status // true) != true then error(.error.message // .message // "the request was rejected") else
.data | {
  key: "balance", name: "Balance", name_zh: "余额",
  kind: "balance", unit: "CNY",
  value: (.available_balance | num)
} end`,
		Checked: CheckedDocs,
	},
	{
		ID: "volcengine", Name: "Volcengine", NameZH: "火山引擎",
		Homepage: "https://www.volcengine.com",
		Fields: []Field{
			{Key: "access_key_id", Label: "Access Key ID"},
			{Key: "secret_access_key", Label: "Secret Access Key", Secret: true},
		},
		Request: Request{
			URL:   "https://open.volcengineapi.com/",
			Query: map[string]string{"Action": "QueryBalanceAcct", "Version": "2022-01-01"},
			Auth:  Auth{Type: AuthVolcV4, Region: "cn-shanghai", Service: "billing"},
		},
		Extract: `
if .ResponseMetadata.Error then
  error(.ResponseMetadata.Error.Message // .ResponseMetadata.Error.Code // "the request was rejected")
else {
  key: "balance", name: "Balance", name_zh: "余额",
  kind: "balance", unit: "CNY",
  value: (.Result.AvailableBalance | num)
} end`,
		Checked: CheckedRecorded,
	},
	{
		ID: "aliyun", Name: "Alibaba Cloud", NameZH: "阿里云",
		Homepage: "https://www.aliyun.com",
		Fields: []Field{
			{Key: "access_key_id", Label: "AccessKey ID"},
			{Key: "access_key_secret", Label: "AccessKey Secret", Secret: true},
		},
		Request: Request{
			URL:   "https://business.aliyuncs.com/",
			Query: map[string]string{"Action": "QueryAccountBalance", "Version": "2017-12-14"},
			Auth:  Auth{Type: AuthAliyunRPC},
		},
		Extract: `
if (.Success // true) != true then error(.Message // "the request was rejected") else {
  key: "balance", name: "Balance", name_zh: "余额",
  kind: "balance", unit: (.Data.Currency // "CNY"),
  value: ((.Data.AvailableAmount // .AvailableAmount) | num)
} end`,
		Checked: CheckedRecorded,
	},
	{
		ID: "tikhub", Name: "TikHub",
		Homepage: "https://tikhub.io",
		Fields:   []Field{apiKey},
		Request: Request{
			URL:  "https://api.tikhub.dev/api/v1/tikhub/user/get_user_info",
			Auth: Auth{Type: AuthBearer},
		},
		Extract: `{
  key: "balance", name: "Balance", name_zh: "余额",
  kind: "balance", unit: "USD",
  value: ((.user_data.balance // .data.balance // .balance) | num)
}`,
		Checked: CheckedRecorded,
	},
	{
		ID: "uniapi", Name: "UniAPI",
		Homepage: "https://uniapi.io",
		Fields:   []Field{apiKey},
		Request: Request{
			URL:   "https://api.uniapi.io/v1/billing/usage",
			Query: map[string]string{"unit": "usd"},
			Auth:  Auth{Type: AuthBearer},
		},
		Extract: `
if (.success // true) != true then error(.message // .msg // "the request was rejected") else {
  key: "credits", name: "Credits", name_zh: "额度",
  kind: "balance", unit: "USD",
  value: (.data.balance | num)
} end`,
		Checked: CheckedRecorded,
	},
}

func init() {
	for i := range catalog {
		catalog[i].Builtin = true
	}
	sort.SliceStable(catalog, func(i, j int) bool { return catalog[i].ID < catalog[j].ID })
}

// Catalog returns the built-in definitions, sorted by ID.
func Catalog() []Definition {
	return append([]Definition(nil), catalog...)
}

// Builtin looks up a built-in definition.
func Builtin(id string) (Definition, bool) {
	for _, def := range catalog {
		if def.ID == id {
			return def, true
		}
	}
	return Definition{}, false
}

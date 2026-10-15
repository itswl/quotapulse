package provider

import (
	"github.com/itswl/quotapulse/internal/model"
)

// DefaultCustomProviders carries the platforms the built-in adapters used to
// be: the same endpoints, paths and semantics, expressed as custom provider
// definitions. They register at init so even a database-less core build has
// a working catalog, and seed into the database on the first start with
// dynamic configuration enabled — after which they are ordinary rows the
// page can edit or delete.
//
// The proof these definitions match the retired adapters is
// TestCustomCoversEveryBuiltinShape, which replays each adapter's fixtures
// through the custom engine.
func DefaultCustomProviders() []model.CustomProvider {
	return []model.CustomProvider{
		{
			Name: "deepseek", DisplayName: "DeepSeek",
			URL: "https://api.deepseek.com/user/balance", AuthMode: CustomAuthBearer,
			DefaultType: model.TypeBalance, Currency: "CNY",
			BalancePath: "balance_infos.{currency:CNY}.total_balance",
		},
		{
			Name: "glm", DisplayName: "GLM",
			URL: "https://open.bigmodel.cn/api/monitor/usage/quota/limit", AuthMode: CustomAuthBearer,
			DefaultType: model.TypeQuota,
			BalancePath: "data.limits", ExtractMode: "min_window",
			SuccessPath: "success", ErrorPath: "msg",
		},
		{
			Name: "openrouter", DisplayName: "OpenRouter",
			URL: "https://openrouter.ai/api/v1/credits", AuthMode: CustomAuthBearer,
			DefaultType: model.TypeCredits, Currency: "USD",
			BalancePath:  "data.total_credits | total_credits",
			SubtractPath: "data.total_usage | total_usage",
		},
		{
			Name: "tikhub", DisplayName: "TikHub",
			URL: "https://api.tikhub.dev/api/v1/tikhub/user/get_user_info", AuthMode: CustomAuthBearer,
			DefaultType: model.TypeBalance, Currency: "USD",
			BalancePath: "user_data.balance | data.balance | balance",
		},
		{
			Name: "uniapi", DisplayName: "UniAPI",
			URL: "https://api.uniapi.io/v1/billing/usage", AuthMode: CustomAuthBearer,
			DefaultType: model.TypeCredits, Currency: "USD",
			BalancePath: "data.balance",
			SuccessPath: "success",
			ExtraParams: "unit=usd",
		},
		{
			Name: "wxrank", DisplayName: "WxRank",
			URL: "https://data.wxrank.com/weixin/score", AuthMode: CustomAuthQuery, AuthParam: "key",
			DefaultType: model.TypeCredits,
			BalancePath: "msg | data.score | data.credits | score | credits",
			SuccessPath: "code", SuccessValues: "0", ErrorPath: "msg",
		},
		{
			Name: "volc", DisplayName: "Volcengine",
			URL: "https://open.volcengineapi.com/", AuthMode: CustomAuthVolcV4,
			DefaultType: model.TypeBalance, Currency: "CNY",
			BalancePath: "Result.AvailableBalance",
			ErrorPath:   "ResponseMetadata.Error",
			SignRegion:  "cn-shanghai",
			SignService: "billing",
			ExtraParams: "Action=QueryBalanceAcct,Version=2022-01-01",
		},
		{
			Name: "aliyun", DisplayName: "Alibaba Cloud",
			URL: "https://business.aliyuncs.com/", AuthMode: CustomAuthAliyunRPC,
			DefaultType: model.TypeBalance, Currency: "CNY",
			BalancePath: "Data.AvailableAmount | AvailableAmount",
			ExtraParams: "Action=QueryAccountBalance,Version=2017-12-14",
		},
	}
}

// EnvPrefixOf maps a registry key to its environment variable prefix: the
// custom: namespace strips away, so DEEPSEEK_API_KEY keeps discovering a
// project on custom:deepseek exactly as it did on the built-in adapter.
func EnvPrefixOf(key string) string {
	return trimCustomPrefix(key)
}

func defaultSpecs() []Spec {
	specs := make([]Spec, 0, 8)
	for _, custom := range DefaultCustomProviders() {
		if spec, err := SpecFromCustomProvider(custom); err == nil {
			specs = append(specs, spec)
		}
	}
	return specs
}

func init() {
	// The built-in adapters are gone; their definitions live here. Seed the
	// dynamic registry so a fresh process — with or without a database —
	// starts with a complete catalog.
	_ = ReplaceDynamic(defaultSpecs())
}

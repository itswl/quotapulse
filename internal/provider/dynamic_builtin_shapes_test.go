package provider

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/itswl/quotapulse/internal/model"
)

func customSpec(t *testing.T, p model.CustomProvider) Spec {
	t.Helper()
	spec, err := SpecFromCustomProvider(p)
	if err != nil {
		t.Fatalf("SpecFromCustomProvider(%s): %v", p.Name, err)
	}
	return spec
}

func fetchSpec(t *testing.T, spec Spec, apiKey string) (float64, error) {
	t.Helper()
	return NewFromSpec(spec, apiKey, NewClient(DefaultTimeout)).Fetch(context.Background())
}

// ---------- path features ----------

// Fallback paths and the array filter, expressed exactly as the built-in
// adapters use them.
func TestCustomPathFallbackAndFilter(t *testing.T) {
	// tikhub shape: balance drifts between user_data, data and the top level
	spec := customSpec(t, model.CustomProvider{
		Name: "tikhub-like", URL: "https://placeholder.test", AuthMode: CustomAuthNone, DefaultType: model.TypeBalance,
		BalancePath: "user_data.balance | data.balance | balance",
	})
	for _, body := range []string{
		`{"user_data":{"balance":11.5}}`,
		`{"data":{"balance":22.5}}`,
		`{"balance":33.5}`,
	} {
		srv, _ := serveJSON(t, 200, body)
		spec.URL = srv.URL
		got, err := fetchSpec(t, spec, "")
		if err != nil {
			t.Fatal(err)
		}
		if got == 0 {
			t.Errorf("no fallback matched for %s", body)
		}
	}

	// deepseek shape: prefer the CNY account among multi-currency sub-accounts
	deepseek := customSpec(t, model.CustomProvider{
		Name: "deepseek-like", URL: "https://placeholder.test", AuthMode: CustomAuthBearer, DefaultType: model.TypeBalance, Currency: "CNY",
		BalancePath: "balance_infos.{currency:CNY}.total_balance",
	})
	srv, _ := serveJSON(t, 200, `{"balance_infos":[
		{"currency":"USD","total_balance":"8.00"},
		{"currency":"CNY","total_balance":"430.37"},
		{"currency":"EUR","total_balance":"9.00"}]}`)
	deepseek.URL = srv.URL
	got, err := fetchSpec(t, deepseek, "sk-x")
	if err != nil || got != 430.37 {
		t.Errorf("CNY filter: got %v (%v), want 430.37", got, err)
	}

	// no match falls back to the first element, like the built-in adapter
	srv2, _ := serveJSON(t, 200, `{"balance_infos":[{"currency":"USD","total_balance":"8.00"}]}`)
	deepseek.URL = srv2.URL
	got, err = fetchSpec(t, deepseek, "sk-x")
	if err != nil || got != 8 {
		t.Errorf("fallback to first: got %v (%v), want 8", got, err)
	}
}

// wxrank shape: the balance hides inside a Chinese sentence.
func TestCustomNumberFromText(t *testing.T) {
	spec := customSpec(t, model.CustomProvider{
		Name: "wxrank-like", URL: "https://placeholder.test", AuthMode: CustomAuthQuery, AuthParam: "key", DefaultType: model.TypeCredits,
		BalancePath: "data.msg",
	})
	srv, _ := serveJSON(t, 200, `{"data":{"msg":"尊敬的用户，剩余263419余额"}}`)
	spec.URL = srv.URL
	got, err := fetchSpec(t, spec, "k")
	if err != nil || got != 263419 {
		t.Errorf("text number: got %v (%v), want 263419", got, err)
	}
}

// openrouter shape: net balance is cumulative top-ups minus cumulative spend.
func TestCustomSubtractPath(t *testing.T) {
	spec := customSpec(t, model.CustomProvider{
		Name: "openrouter-like", URL: "https://placeholder.test", AuthMode: CustomAuthBearer, DefaultType: model.TypeCredits, Currency: "USD",
		BalancePath: "data.total_credits", SubtractPath: "data.total_usage",
	})
	srv, _ := serveJSON(t, 200, `{"data":{"total_credits":100,"total_usage":37.5}}`)
	spec.URL = srv.URL
	got, err := fetchSpec(t, spec, "k")
	if err != nil || got != 62.5 {
		t.Errorf("subtract: got %v (%v), want 62.5", got, err)
	}
}

// uniapi shape: unit=usd rides in the URL, success is a truthy field.
func TestCustomExtraParamsAndSuccess(t *testing.T) {
	srv, rec := serveJSON(t, 200, `{"success":true,"data":{"balance":12.34}}`)
	spec := customSpec(t, model.CustomProvider{
		Name: "uniapi-like", URL: srv.URL + "/usage?unit=usd", AuthMode: CustomAuthNone,
		DefaultType: model.TypeCredits, Currency: "USD",
		BalancePath: "data.balance", SuccessPath: "success",
	})
	got, err := fetchSpec(t, spec, "")
	if err != nil || got != 12.34 {
		t.Fatalf("extra params: got %v (%v)", got, err)
	}
	if q := rec.query.Get("unit"); q != "usd" {
		t.Errorf("unit param lost: %q", q)
	}
}

// ---------- min_window mode (GLM shape) ----------

// The live payload captured on 2026-10-15: the tool quota sits at 100% while
// the rolling window is half spent.
func TestCustomMinWindow(t *testing.T) {
	spec := customSpec(t, model.CustomProvider{
		Name: "glm-like", URL: "https://placeholder.test", AuthMode: CustomAuthBearer, DefaultType: model.TypeQuota,
		ExtractMode: "min_window",
		BalancePath: "data.limits", // informational; the mode does the walking
	})
	body := fmt.Sprintf(`{"code":200,"success":true,"data":{"limits":[
		{"type":"TIME_LIMIT","unit":5,"number":1,"usage":1000,"currentValue":0,"remaining":1000,"percentage":0,"nextResetTime":%d},
		{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":47,"nextResetTime":%d}]}}`,
		time.Now().Add(200*time.Hour).UnixMilli(),
		time.Now().Add(50*time.Minute).UnixMilli())
	srv, _ := serveJSON(t, 200, body)
	spec.URL = srv.URL
	got, err := fetchSpec(t, spec, "k")
	if err != nil || got != 53 {
		t.Errorf("min_window: got %v (%v), want 53", got, err)
	}

	// no reset timestamps at all: the lowest TOKENS/CREDIT window stands in
	srv2, _ := serveJSON(t, 200, `{"code":200,"success":true,"data":{"limits":[
		{"type":"TIME_LIMIT","usage":1000,"remaining":500},
		{"type":"TOKENS_LIMIT","percentage":20}]}}`)
	spec.URL = srv2.URL
	got, err = fetchSpec(t, spec, "k")
	if err != nil || got != 80 {
		t.Errorf("min_window fallback: got %v (%v), want 80", got, err)
	}
}

// ---------- signing presets ----------

// The Aliyun RPC preset reproduces the built-in adapter's request: signature
// recomputable from what the server actually received.
func TestCustomAliyunRPCPreset(t *testing.T) {
	srv, rec := serveJSON(t, 200, `{"Data":{"AvailableAmount":"100.12"}}`)
	spec := customSpec(t, model.CustomProvider{
		Name: "aliyun-like", URL: srv.URL + "/", AuthMode: CustomAuthAliyunRPC,
		DefaultType: model.TypeBalance, Currency: "CNY",
		BalancePath: "Data.AvailableAmount | AvailableAmount",
	})
	got, err := fetchSpec(t, spec, "test-id:test-secret")
	if err != nil || got != 100.12 {
		t.Fatalf("aliyun preset: got %v (%v), want 100.12", got, err)
	}
	sent := map[string]string{}
	for key, values := range rec.query {
		if len(values) > 0 {
			sent[key] = values[0]
		}
	}
	signature := sent["Signature"]
	if signature == "" {
		t.Fatal("request carried no Signature")
	}
	delete(sent, "Signature")
	if want := aliyunRPCSign("test-secret", sent); want != signature {
		t.Fatalf("signature mismatch: sent %q, recomputed %q", signature, want)
	}
	if sent["Action"] != "QueryAccountBalance" || sent["Version"] != "2017-12-14" {
		t.Errorf("default action/version missing: %v", sent)
	}
}

// The Volcengine V4 preset signs with host/path derived from the URL and the
// region/service of the definition.
func TestCustomVolcV4Preset(t *testing.T) {
	srv, rec := serveJSON(t, 200, `{"Result":{"AvailableBalance":88.5}}`)
	spec := customSpec(t, model.CustomProvider{
		Name: "volc-like", URL: srv.URL + "/",
		AuthMode: CustomAuthVolcV4, DefaultType: model.TypeBalance, Currency: "CNY",
		BalancePath: "Result.AvailableBalance",
		SignRegion:  "cn-shanghai", SignService: "billing",
		ExtraParams: "Action=QueryBalanceAcct,Version=2022-01-01",
	})
	got, err := fetchSpec(t, spec, "AKLTtest:test-sk")
	if err != nil || got != 88.5 {
		t.Fatalf("volc preset: got %v (%v), want 88.5", got, err)
	}
	if action := rec.query.Get("Action"); action != "QueryBalanceAcct" {
		t.Errorf("volc action: %q", action)
	}
	if host := rec.host; host == "" {
		t.Error("volc request carried no Host header")
	}
}

// ---------- all eight, one table ----------

// The proof the custom engine can express every built-in: one spec per
// platform shape, verified against trimmed versions of the adapters' own
// fixtures.
func TestCustomCoversEveryBuiltinShape(t *testing.T) {
	cases := []struct {
		name   string
		def    model.CustomProvider
		body   string
		apiKey string
		want   float64
	}{
		{
			name: "deepseek", body: `{"balance_infos":[{"currency":"CNY","total_balance":"110.00"}]}`,
			def: model.CustomProvider{Name: "ds", AuthMode: CustomAuthBearer, DefaultType: model.TypeBalance,
				BalancePath: "balance_infos.{currency:CNY}.total_balance"}, want: 110,
		},
		{
			name: "openrouter", body: `{"data":{"total_credits":10,"total_usage":2.5}}`,
			def: model.CustomProvider{Name: "or", AuthMode: CustomAuthBearer, DefaultType: model.TypeCredits,
				BalancePath: "data.total_credits", SubtractPath: "data.total_usage"}, want: 7.5,
		},
		{
			name: "tikhub", body: `{"user_data":{"balance":41.3}}`,
			def: model.CustomProvider{Name: "th", AuthMode: CustomAuthBearer, DefaultType: model.TypeBalance,
				BalancePath: "user_data.balance | data.balance | balance"}, want: 41.3,
		},
		{
			name: "uniapi", body: `{"success":true,"data":{"balance":12.34}}`,
			def: model.CustomProvider{Name: "ua", AuthMode: CustomAuthNone, DefaultType: model.TypeCredits,
				BalancePath: "data.balance", SuccessPath: "success"}, want: 12.34,
		},
		{
			name: "wxrank", body: `{"data":{"msg":"剩余263419余额"}}`,
			def: model.CustomProvider{Name: "wr", AuthMode: CustomAuthQuery, AuthParam: "key", DefaultType: model.TypeCredits,
				BalancePath: "data.msg"}, want: 263419,
		},
		{
			name: "glm", body: fmt.Sprintf(`{"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":47,"nextResetTime":%d}]}}`,
				time.Now().Add(time.Hour).UnixMilli()),
			def: model.CustomProvider{Name: "gl", AuthMode: CustomAuthBearer, DefaultType: model.TypeQuota,
				ExtractMode: "min_window", BalancePath: "data.limits"}, want: 53,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := serveJSON(t, 200, tc.body)
			tc.def.URL = srv.URL
			spec := customSpec(t, tc.def)
			got, err := fetchSpec(t, spec, tc.apiKey)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got != tc.want {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			}
		})
	}
	// volc and aliyun need signed transports; the preset tests above cover them
}

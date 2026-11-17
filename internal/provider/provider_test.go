package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"quotapulse/internal/core"
)

var fixedNow = time.Date(2026, 11, 16, 2, 0, 0, 0, time.UTC)

// recorded is what the test server saw of the last request.
type recorded struct {
	method string
	path   string
	query  url.Values
	header http.Header
	body   string
	raw    string
}

// serve answers every request with the same status and body.
func serve(t *testing.T, status int, body string) (*httptest.Server, *recorded) {
	t.Helper()
	seen := &recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		*seen = recorded{method: r.Method, path: r.URL.Path, query: r.URL.Query(), header: r.Header.Clone(), body: string(payload), raw: r.URL.RawQuery}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

func testClient() *Client {
	c := NewClient()
	c.Now = func() time.Time { return fixedNow }
	return c
}

// against points a definition at a test server, keeping its path.
func against(def Definition, srv *httptest.Server) Definition {
	u, _ := url.Parse(def.Request.URL)
	def.Request.URL = srv.URL + u.Path
	return def
}

func mustBuiltin(t *testing.T, id string) Definition {
	t.Helper()
	def, ok := Builtin(id)
	if !ok {
		t.Fatalf("no built-in definition %q", id)
	}
	return def
}

func fetch(t *testing.T, def Definition, credential map[string]string) ([]Sample, error) {
	t.Helper()
	result, err := testClient().Fetch(context.Background(), def, credential)
	return result.Samples, err
}

func TestCatalogIsValid(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range Catalog() {
		if err := def.Validate(); err != nil {
			t.Errorf("%s: %v", def.ID, err)
		}
		if !def.Builtin || def.Checked == "" {
			t.Errorf("%s: built-ins must say how they were checked", def.ID)
		}
		if seen[def.ID] {
			t.Errorf("%s: declared twice", def.ID)
		}
		seen[def.ID] = true
	}
}

func TestDeepSeek(t *testing.T) {
	// The live shape: balances are strings, one entry per currency.
	srv, seen := serve(t, 200, `{"is_available":true,"balance_infos":[
		{"currency":"CNY","total_balance":"547.67","granted_balance":"0.00","topped_up_balance":"547.67"},
		{"currency":"USD","total_balance":"8.00","granted_balance":"0.00","topped_up_balance":"8.00"}]}`)
	samples, err := fetch(t, against(mustBuiltin(t, "deepseek"), srv), map[string]string{"api_key": "sk-test-0001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 {
		t.Fatalf("want a meter per currency, got %+v", samples)
	}
	cny := samples[0]
	if cny.Key != "balance-cny" || cny.Unit != "CNY" || cny.Value != 547.67 || cny.Kind != core.Balance || cny.NameZH != "余额" {
		t.Errorf("CNY meter = %+v", cny)
	}
	if samples[1].Key != "balance-usd" || samples[1].Value != 8 {
		t.Errorf("USD meter = %+v", samples[1])
	}
	if got := seen.header.Get("Authorization"); got != "Bearer sk-test-0001" {
		t.Errorf("Authorization = %q", got)
	}
	if seen.path != "/user/balance" || seen.method != "GET" {
		t.Errorf("request = %s %s", seen.method, seen.path)
	}
}

func TestDeepSeekWithoutBalancesIsAnError(t *testing.T) {
	srv, _ := serve(t, 200, `{"is_available":false,"balance_infos":[]}`)
	_, err := fetch(t, against(mustBuiltin(t, "deepseek"), srv), map[string]string{"api_key": "sk-test-0001"})
	if err == nil || !strings.Contains(err.Error(), "no meters") {
		t.Fatalf("err = %v", err)
	}
}

func TestGLMCodingPlan(t *testing.T) {
	// The live shape: a monthly tool quota in calls and a five-hour token
	// window in percent, each with its own reset time in milliseconds.
	srv, _ := serve(t, 200, `{"code":200,"msg":"操作成功","success":true,"data":{"level":"pro","limits":[
		{"type":"TIME_LIMIT","unit":5,"number":1,"usage":1000,"currentValue":77,"remaining":923,"percentage":7,
		 "nextResetTime":1795680045998,"usageDetails":[{"modelCode":"search-prime","usage":41}]},
		{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":1,"nextResetTime":1794798055318}]}}`)
	samples, err := fetch(t, against(mustBuiltin(t, "glm-coding"), srv), map[string]string{"api_key": "id.secret-0001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 {
		t.Fatalf("samples = %+v", samples)
	}
	tools, tokens := samples[0], samples[1]
	if tools.Key != "tools-1mo" || tools.Kind != core.Quota || tools.Unit != "calls" || tools.Value != 923 || *tools.Limit != 1000 {
		t.Errorf("tools = %+v", tools)
	}
	if *tools.Window != 2592000 || tools.ResetsAt.Unix() != 1795680045 {
		t.Errorf("tools window = %v, resets %v", *tools.Window, tools.ResetsAt)
	}
	if tokens.Key != "tokens-5h" || tokens.Unit != "%" || tokens.Value != 99 || *tokens.Limit != 100 || tokens.NameZH != "5 小时窗口" {
		t.Errorf("tokens = %+v", tokens)
	}
	if *tokens.Window != 5*3600 || tokens.ResetsAt.Unix() != 1794798055 {
		t.Errorf("tokens window = %v, resets %v", *tokens.Window, tokens.ResetsAt)
	}
}

func TestGLMUnknownUnitGetsNoWindow(t *testing.T) {
	srv, _ := serve(t, 200, `{"success":true,"data":{"limits":[
		{"type":"TOKENS_LIMIT","unit":6,"number":1,"percentage":40,"nextResetTime":1795680045998},
		{"type":"SOMETHING_NEW","unit":3,"number":5,"percentage":40}]}}`)
	samples, err := fetch(t, against(mustBuiltin(t, "glm-coding"), srv), map[string]string{"api_key": "id.secret-0001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 || samples[0].Key != "tokens-u6n1" || samples[0].Window != nil || samples[0].Value != 60 {
		t.Fatalf("samples = %+v", samples)
	}
}

func TestGLMBusinessError(t *testing.T) {
	srv, _ := serve(t, 200, `{"code":1001,"msg":"令牌已过期","success":false}`)
	_, err := fetch(t, against(mustBuiltin(t, "glm-coding"), srv), map[string]string{"api_key": "id.secret-0001"})
	if err == nil || err.Error() != "令牌已过期" {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenRouter(t *testing.T) {
	srv, _ := serve(t, 200, `{"data":{"total_credits":100,"total_usage":37.5}}`)
	samples, err := fetch(t, against(mustBuiltin(t, "openrouter"), srv), map[string]string{"api_key": "sk-or-0001"})
	if err != nil || len(samples) != 1 || samples[0].Value != 62.5 || samples[0].Unit != "USD" {
		t.Fatalf("samples = %+v, err = %v", samples, err)
	}
}

func TestSiliconFlowAndMoonshot(t *testing.T) {
	srv, _ := serve(t, 200, `{"code":20000,"message":"OK","status":true,"data":{"balance":"0.88","chargeBalance":"88.00","totalBalance":"88.88"}}`)
	samples, err := fetch(t, against(mustBuiltin(t, "siliconflow"), srv), map[string]string{"api_key": "sk-sf-0001"})
	if err != nil || samples[0].Value != 88.88 || samples[0].Unit != "CNY" {
		t.Fatalf("siliconflow = %+v, %v", samples, err)
	}

	srv, _ = serve(t, 200, `{"code":0,"data":{"available_balance":49.58,"voucher_balance":46.58,"cash_balance":3.0},"scode":"0x0","status":true}`)
	samples, err = fetch(t, against(mustBuiltin(t, "moonshot"), srv), map[string]string{"api_key": "sk-ms-0001"})
	if err != nil || samples[0].Value != 49.58 {
		t.Fatalf("moonshot = %+v, %v", samples, err)
	}
}

func TestTikHubFallbackPaths(t *testing.T) {
	// The balance has moved between these three places over time.
	for _, body := range []string{
		`{"user_data":{"balance":11.5}}`,
		`{"data":{"balance":"22.5"}}`,
		`{"balance":33.5}`,
	} {
		srv, _ := serve(t, 200, body)
		samples, err := fetch(t, against(mustBuiltin(t, "tikhub"), srv), map[string]string{"api_key": "tk-0001"})
		if err != nil || samples[0].Value == 0 {
			t.Errorf("%s: %+v, %v", body, samples, err)
		}
	}
}

func TestUniAPISendsItsFixedQuery(t *testing.T) {
	srv, seen := serve(t, 200, `{"success":true,"data":{"balance":12.34}}`)
	samples, err := fetch(t, against(mustBuiltin(t, "uniapi"), srv), map[string]string{"api_key": "uk-0001"})
	if err != nil || samples[0].Value != 12.34 {
		t.Fatalf("samples = %+v, err = %v", samples, err)
	}
	if seen.query.Get("unit") != "usd" {
		t.Errorf("query = %v", seen.query)
	}
}

func TestWxRankNumberInsideASentence(t *testing.T) {
	srv, seen := serve(t, 200, `{"code":0,"msg":"尊敬的用户，剩余263,419余额"}`)
	samples, err := fetch(t, against(mustBuiltin(t, "wxrank"), srv), map[string]string{"api_key": "wx-key-0001"})
	if err != nil || samples[0].Value != 263419 || samples[0].Unit != "credits" {
		t.Fatalf("samples = %+v, err = %v", samples, err)
	}
	if seen.query.Get("key") != "wx-key-0001" {
		t.Errorf("the key goes in the query: %v", seen.query)
	}

	srv, _ = serve(t, 200, `{"code":1002,"msg":"key 无效"}`)
	if _, err := fetch(t, against(mustBuiltin(t, "wxrank"), srv), map[string]string{"api_key": "wx-key-0001"}); err == nil || err.Error() != "key 无效" {
		t.Fatalf("err = %v", err)
	}
}

func TestAliyunSignsTheQuery(t *testing.T) {
	srv, seen := serve(t, 200, `{"Code":"200","Message":"Successful!","Success":true,"Data":{"AvailableAmount":"1,234.56","Currency":"CNY"}}`)
	samples, err := fetch(t, against(mustBuiltin(t, "aliyun"), srv), map[string]string{
		"access_key_id": "test-id", "access_key_secret": "test-secret-0001",
	})
	if err != nil || samples[0].Value != 1234.56 || samples[0].Unit != "CNY" {
		t.Fatalf("samples = %+v, err = %v", samples, err)
	}
	sent := map[string]string{}
	for key, values := range seen.query {
		sent[key] = values[0]
	}
	signature := sent["Signature"]
	delete(sent, "Signature")
	if signature == "" || aliyunRPCSign("test-secret-0001", "GET", sent) != signature {
		t.Fatalf("the signature does not cover what was sent: %v", sent)
	}
	if sent["Action"] != "QueryAccountBalance" || sent["Version"] != "2017-12-14" || sent["AccessKeyId"] != "test-id" {
		t.Errorf("sent = %v", sent)
	}
	if sent["Timestamp"] != "2026-11-16T02:00:00Z" {
		t.Errorf("Timestamp = %q", sent["Timestamp"])
	}
}

// The worked example from Alibaba Cloud's signature documentation.
func TestAliyunDocumentedVector(t *testing.T) {
	params := map[string]string{
		"AccessKeyId":      "testid",
		"Action":           "DescribeRegions",
		"Format":           "XML",
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   "3ee8c1b8-83d3-44af-a94f-4e0ad82fd6cf",
		"SignatureVersion": "1.0",
		"Timestamp":        "2016-02-23T12:46:24Z",
		"Version":          "2014-05-26",
	}
	if got := aliyunRPCSign("testsecret", "GET", params); got != "OLeaidS1JvxuMvnyHOwuJ+uX5qY=" {
		t.Fatalf("signature = %q", got)
	}
}

func TestVolcengineSignsTheRequest(t *testing.T) {
	srv, seen := serve(t, 200, `{"ResponseMetadata":{"RequestId":"x","Action":"QueryBalanceAcct"},"Result":{"AvailableBalance":"88.50"}}`)
	def := against(mustBuiltin(t, "volcengine"), srv)
	samples, err := fetch(t, def, map[string]string{"access_key_id": "AKLTtest", "secret_access_key": "test-sk-0001"})
	if err != nil || samples[0].Value != 88.5 {
		t.Fatalf("samples = %+v, err = %v", samples, err)
	}
	if seen.raw != "Action=QueryBalanceAcct&Version=2022-01-01" {
		t.Errorf("query = %q", seen.raw)
	}
	// Recompute from what the server received.
	host := strings.TrimPrefix(srv.URL, "http://")
	want := volcV4{
		Method: "GET", Host: host, Path: "/", Region: "cn-shanghai", Service: "billing",
		AK: "AKLTtest", SK: "test-sk-0001",
		Query: map[string]string{"Action": "QueryBalanceAcct", "Version": "2022-01-01"},
	}.headers(fixedNow)
	for _, name := range []string{"Authorization", "X-Date", "X-Content-Sha256"} {
		if seen.header.Get(name) != want[name] {
			t.Errorf("%s = %q, want %q", name, seen.header.Get(name), want[name])
		}
	}
	if !strings.HasPrefix(want["Authorization"], "HMAC-SHA256 Credential=AKLTtest/20261116/cn-shanghai/billing/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=") {
		t.Errorf("Authorization = %q", want["Authorization"])
	}

	srv, _ = serve(t, 200, `{"ResponseMetadata":{"Error":{"Code":"InvalidAccessKey","Message":"the access key is invalid"}}}`)
	if _, err := fetch(t, against(mustBuiltin(t, "volcengine"), srv), map[string]string{"access_key_id": "AKLTtest", "secret_access_key": "test-sk-0001"}); err == nil || err.Error() != "the access key is invalid" {
		t.Fatalf("err = %v", err)
	}
}

func TestPercentEncode(t *testing.T) {
	cases := map[string]string{
		"a b":     "a%20b",
		"a+b":     "a%2Bb",
		"a*b":     "a%2Ab",
		"a~b-_.c": "a~b-_.c",
		"/":       "%2F",
		"中":       "%E4%B8%AD",
		"a=b&c":   "a%3Db%26c",
		"12:46":   "12%3A46",
	}
	for in, want := range cases {
		if got := percentEncode(in); got != want {
			t.Errorf("percentEncode(%q) = %q, want %q", in, got, want)
		}
	}
}

func custom(extract string) Definition {
	return Definition{
		ID: "custom-test", Name: "Custom",
		Fields:  []Field{{Key: "api_key", Label: "API Key", Secret: true}, {Key: "team", Label: "Team", Optional: true}},
		Request: Request{URL: "https://placeholder.test/v1", Auth: Auth{Type: AuthBearer}},
		Extract: extract,
	}
}

func TestHTTPErrorsCarryThePlatformMessage(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{401, `{"error":{"message":"Incorrect API key provided","type":"auth"}}`, "HTTP 401: Incorrect API key provided"},
		{403, `{"code":"invalid-argument","error":"Incorrect API key provided."}`, "HTTP 403: Incorrect API key provided."},
		{400, `{"Code":"InvalidAccessKeyId.NotFound","Message":"Specified access key is not found."}`, "HTTP 400: Specified access key is not found."},
		{404, `{"unrelated":true}`, "HTTP 404 Not Found"},
		{401, `<html>nope</html>`, "HTTP 401 Unauthorized"},
	}
	for _, c := range cases {
		srv, _ := serve(t, c.status, c.body)
		def := custom(`{value: .balance}`)
		def.Request.URL = srv.URL
		if _, err := fetch(t, def, map[string]string{"api_key": "sk-test-0001"}); err == nil || err.Error() != c.want {
			t.Errorf("status %d: err = %v, want %q", c.status, err, c.want)
		}
	}
}

func TestSecretsNeverReachAnError(t *testing.T) {
	// A platform that echoes the key it rejected.
	srv, _ := serve(t, 401, `{"error":{"message":"invalid key sk-test-secret-0001 for team blue"}}`)
	def := custom(`{value: .balance}`)
	def.Request.URL = srv.URL
	_, err := fetch(t, def, map[string]string{"api_key": "sk-test-secret-0001", "team": "blue"})
	if err == nil || strings.Contains(err.Error(), "sk-test-secret-0001") {
		t.Fatalf("the key leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid key •••") {
		t.Errorf("err = %v", err)
	}

	// A connection error must not repeat a URL that carries the key.
	def = custom(`{value: .balance}`)
	def.Request.URL = "http://127.0.0.1:1/x"
	def.Request.Auth = Auth{Type: AuthQuery, Name: "key"}
	client := testClient()
	client.HTTP.Timeout = 2 * time.Second
	_, err = client.Fetch(context.Background(), def, map[string]string{"api_key": "sk-test-secret-0001"})
	if err == nil || strings.Contains(err.Error(), "sk-test-secret-0001") || strings.Contains(err.Error(), "key=") {
		t.Fatalf("err = %v", err)
	}
}

func TestRetriesTransientFailures(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"balance":5}`))
	}))
	defer srv.Close()
	def := custom(`{value: .balance}`)
	def.Request.URL = srv.URL
	samples, err := fetch(t, def, map[string]string{"api_key": "sk-test-0001"})
	if err != nil || samples[0].Value != 5 || calls.Load() != 3 {
		t.Fatalf("samples = %+v, err = %v, calls = %d", samples, err, calls.Load())
	}

	// A wrong key is not transient: one call, no retry.
	calls.Store(0)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv2.Close()
	def.Request.URL = srv2.URL
	if _, err := fetch(t, def, map[string]string{"api_key": "sk-test-0001"}); err == nil || calls.Load() != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls.Load())
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		_, _ = w.Write([]byte(`{"balance":1}`))
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer srv.Close()
	def := custom(`{value: .balance}`)
	def.Request.URL = srv.URL
	def.Request.Auth = Auth{Type: AuthHeader, Name: "X-API-Key"}
	if _, err := fetch(t, def, map[string]string{"api_key": "sk-test-0001"}); err == nil {
		t.Fatal("a redirect must fail the fetch")
	}
	if elsewhere.Load() != 0 {
		t.Fatal("the credential followed a redirect to another host")
	}
}

func TestTemplatesAndAuthModes(t *testing.T) {
	srv, seen := serve(t, 200, `{"balance":7}`)
	def := Definition{
		ID: "templated", Name: "Templated",
		Fields: []Field{
			{Key: "base_url", Label: "Base URL"},
			{Key: "team", Label: "Team"},
			{Key: "token", Label: "Token", Secret: true},
			{Key: "region", Label: "Region", Optional: true, Default: "eu"},
		},
		Request: Request{
			Method:  "POST",
			URL:     "{{base_url}}/teams/{{team}}/balance?fixed=1",
			Headers: map[string]string{"X-Region": "{{ region }}"},
			Query:   map[string]string{"since": "{{month_start_date}}", "at": "{{now}}"},
			Body:    `{"team":"{{team}}","day":"{{today}}"}`,
			Auth:    Auth{Type: AuthHeader, Name: "X-Token", Value: "t={{token}}"},
		},
		Extract: `{value: .balance, unit: $fields.team}`,
	}
	if err := def.Validate(); err != nil {
		t.Fatal(err)
	}
	client := testClient()
	client.Location = time.FixedZone("CST", 8*3600)
	result, err := client.Fetch(context.Background(), def, map[string]string{
		"base_url": srv.URL + "/", "team": "blue", "token": "tok-secret-0001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen.method != "POST" || seen.path != "/teams/blue/balance" {
		t.Errorf("request = %s %s", seen.method, seen.path)
	}
	if seen.query.Get("fixed") != "1" || seen.query.Get("since") != "2026-11-01" || seen.query.Get("at") != "1794794400" {
		t.Errorf("query = %v", seen.query)
	}
	if seen.header.Get("X-Token") != "t=tok-secret-0001" || seen.header.Get("X-Region") != "eu" {
		t.Errorf("headers = %v", seen.header)
	}
	if seen.body != `{"team":"blue","day":"2026-11-16"}` || seen.header.Get("Content-Type") != "application/json" {
		t.Errorf("body = %s", seen.body)
	}
	// $fields shows plain fields and never the secret ones.
	if result.Samples[0].Unit != "blue" {
		t.Errorf("$fields.team = %q", result.Samples[0].Unit)
	}
	leak := def
	leak.Extract = `{value: .balance, unit: ($fields.token // "hidden")}`
	result, err = client.Fetch(context.Background(), leak, map[string]string{"base_url": srv.URL, "team": "blue", "token": "tok-secret-0001"})
	if err != nil || result.Samples[0].Unit != "hidden" {
		t.Fatalf("a secret field was visible to the extract program: %+v, %v", result.Samples, err)
	}

	basic := custom(`{value: .balance}`)
	basic.Request.URL = srv.URL
	basic.Request.Auth = Auth{Type: AuthBasic, Value: "{{team}}:{{api_key}}"}
	if _, err := fetch(t, basic, map[string]string{"api_key": "pw-0001", "team": "blue"}); err != nil {
		t.Fatal(err)
	}
	if seen.header.Get("Authorization") != "Basic Ymx1ZTpwdy0wMDAx" {
		t.Errorf("basic auth = %q", seen.header.Get("Authorization"))
	}
}

func TestMissingCredentialFields(t *testing.T) {
	def := mustBuiltin(t, "volcengine")
	if _, err := fetch(t, def, map[string]string{"access_key_id": "AK"}); err == nil || err.Error() != "missing Secret Access Key" {
		t.Fatalf("err = %v", err)
	}
	if _, err := fetch(t, def, map[string]string{"access_key_id": " ", "secret_access_key": ""}); err == nil || err.Error() != "missing Access Key ID, Secret Access Key" {
		t.Fatalf("err = %v", err)
	}
}

func TestExtractOutputs(t *testing.T) {
	ctx := context.Background()
	run := func(program, body string) ([]Sample, error) {
		var parsed any
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			t.Fatal(err)
		}
		return Extract(ctx, program, parsed, map[string]string{"team": "blue"}, fixedNow)
	}

	// defaults: one meter, key "main", kind balance
	got, err := run(`{value: .n}`, `{"n":"12.5"}`)
	if err != nil || len(got) != 1 || got[0].Key != "main" || got[0].Kind != core.Balance || got[0].Value != 12.5 {
		t.Fatalf("defaults: %+v, %v", got, err)
	}

	// an array of objects is as good as a stream of them
	got, err = run(`[.items[] | {key: .id, value: .v, kind: "counter"}]`, `{"items":[{"id":"a","v":1},{"id":"b","v":2}]}`)
	if err != nil || len(got) != 2 || got[1].Key != "b" || got[1].Kind != core.Counter {
		t.Fatalf("array: %+v, %v", got, err)
	}

	// reset times: seconds, milliseconds, RFC 3339, and zero for none
	got, err = run(`
		{key:"s", value:1, resets_at: 1794798055},
		{key:"ms", value:1, resets_at: 1794798055318},
		{key:"iso", value:1, resets_at: "2026-11-16T03:00:55Z"},
		{key:"none", value:1, resets_at: 0}`, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ResetsAt.Unix() != 1794798055 || got[1].ResetsAt.Unix() != 1794798055 || got[2].ResetsAt.Unix() != 1794798055 || got[3].ResetsAt != nil {
		t.Errorf("reset times: %v %v %v %v", got[0].ResetsAt, got[1].ResetsAt, got[2].ResetsAt, got[3].ResetsAt)
	}

	// $now is the fetch time
	got, err = run(`{value: $now}`, `{}`)
	if err != nil || got[0].Value != float64(fixedNow.Unix()) {
		t.Fatalf("$now: %+v, %v", got, err)
	}

	bad := map[string]string{
		`{value: .missing}`:                        "value must be a number, got null",
		`{value: "abc"}`:                           "value must be a number",
		`{value: 1, kind: "gauge"}`:                "kind must be balance, quota or counter",
		`{key: "Bad Key", value: 1}`:               "meter key",
		`{key:"a", value: 1}, {key:"a", value: 2}`: "two meters with the key",
		`.n`:                                    "must output objects",
		`empty`:                                 "no meters",
		`error("the platform said no")`:         "the platform said no",
		`{value: (1/0)}`:                        "",
		`{value: 1, limit: "x"}`:                "limit must be a number",
		`{value: 1, window: -5}`:                "window must be a number of seconds",
		`{value: 1, resets_at: "next tuesday"}`: "resets_at",
		`{value: ("no digits here" | num)}`:     "expected a number",
		`{value: (.n | num)}`:                   "",
		`{value: ([1] | num)}`:                  "expected a number, got array",
		`def f: f; {value: f}`:                  "",
		`{value: ($ENV.PATH // "x" | length)}`:  "",
	}
	for program, want := range bad {
		got, err := run(program, `{"n": 3}`)
		switch program {
		case `{value: (.n | num)}`:
			if err != nil || got[0].Value != 3 {
				t.Errorf("%s: %+v, %v", program, got, err)
			}
			continue
		case `{value: ($ENV.PATH // "x" | length)}`:
			// The environment is not visible: $ENV.PATH is null, so the
			// fallback string "x" is measured.
			if err != nil || got[0].Value != 1 {
				t.Errorf("the environment leaked into jq: %+v, %v", got, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: expected an error, got %+v", program, got)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %q, want it to mention %q", program, err, want)
		}
	}
}

func TestValuesAreTidied(t *testing.T) {
	got, err := Extract(context.Background(), `{value: (150 - 131.6), limit: (0.1 + 0.2)}`, map[string]any{}, nil, fixedNow)
	if err != nil || got[0].Value != 18.4 || *got[0].Limit != 0.3 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestExtractStopsARunawayProgram(t *testing.T) {
	started := time.Now()
	_, err := Extract(context.Background(), `def f: f; {value: f}`, map[string]any{}, nil, fixedNow)
	if err == nil {
		t.Fatal("an endless program returned")
	}
	if elapsed := time.Since(started); elapsed > extractTimeout+3*time.Second {
		t.Fatalf("took %v to stop", elapsed)
	}
}

func TestValidate(t *testing.T) {
	good := custom(`{value: .balance}`)
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	mutate := func(change func(*Definition)) Definition {
		def := custom(`{value: .balance}`)
		change(&def)
		return def
	}
	bad := map[string]Definition{
		"id":             mutate(func(d *Definition) { d.ID = "Has Spaces" }),
		"name":           mutate(func(d *Definition) { d.Name = " " }),
		"request.url":    mutate(func(d *Definition) { d.Request.URL = "ftp://example.test" }),
		"request.method": mutate(func(d *Definition) { d.Request.Method = "DELETE" }),
		"auth.type":      mutate(func(d *Definition) { d.Request.Auth.Type = "oauth" }),
		"auth.name":      mutate(func(d *Definition) { d.Request.Auth = Auth{Type: AuthHeader} }),
		"not a declared": mutate(func(d *Definition) { d.Request.URL = "https://x.test/{{nope}}" }),
		"declared twice": mutate(func(d *Definition) { d.Fields = append(d.Fields, d.Fields[0]) }),
		"built-in":       mutate(func(d *Definition) { d.Fields = append(d.Fields, Field{Key: "now"}) }),
		"valid key":      mutate(func(d *Definition) { d.Fields = append(d.Fields, Field{Key: "Bad-Key"}) }),
		"extract":        mutate(func(d *Definition) { d.Extract = "{value: " }),
		"extract: req":   mutate(func(d *Definition) { d.Extract = "" }),
		"api_key":        mutate(func(d *Definition) { d.Fields = []Field{{Key: "token"}} }),
		"aliyun-rpc":     mutate(func(d *Definition) { d.Request.Auth = Auth{Type: AuthAliyunRPC} }),
		"region":         mutate(func(d *Definition) { d.Request.Auth = Auth{Type: AuthVolcV4} }),
	}
	for want, def := range bad {
		err := def.Validate()
		if err == nil {
			t.Errorf("%s: passed validation", want)
			continue
		}
		if !strings.Contains(err.Error(), strings.Split(want, ":")[0]) {
			t.Errorf("%s: err = %v", want, err)
		}
	}
}

func TestMaskCredential(t *testing.T) {
	def := mustBuiltin(t, "volcengine")
	masked := def.MaskCredential(map[string]string{"access_key_id": "AKLTplain", "secret_access_key": "abcdefghijklmnop"})
	if masked["access_key_id"] != "AKLTplain" {
		t.Errorf("a plain field was masked: %q", masked["access_key_id"])
	}
	if masked["secret_access_key"] != "abc••••mnop" {
		t.Errorf("masked secret = %q", masked["secret_access_key"])
	}
	if Mask("short") != "••••" {
		t.Errorf("a short secret must not show its ends: %q", Mask("short"))
	}
}

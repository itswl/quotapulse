package provider

import (
	"context"
	"testing"
)

func TestUniAPIFetch(t *testing.T) {
	runSpecCases(t, uniapiSpec, []specCase{
		{
			name: "线上真实结构",
			body: `{"success":true,"data":{"balance":10446.05,"used":48280.24,"cache_used":0}}`,
			want: 10446.05,
		},
		{
			name:   "业务失败",
			body:   `{"success":false,"data":{}}`,
			errMsg: "API returned success=false",
		},
		{
			name:   "没有 success field也算失败",
			body:   `{"data":{"balance":1}}`,
			errMsg: "API returned success=false",
		},
		{
			name:   "Missing balance",
			body:   `{"success":true,"data":{"used":100}}`,
			errMsg: "Could not parse balance field",
		},
		{
			name:   "data 是 null",
			body:   `{"success":true,"data":null}`,
			errMsg: "Could not parse balance field",
		},
	})
}

func TestUniAPISendsUnitParam(t *testing.T) {
	// unit=usd is required by the API; dropping it fetches a number in another unit
	srv, rec := serveJSON(t, 200, `{"success":true,"data":{"balance":1}}`)
	if _, err := specProviderAt(uniapiSpec, srv.URL, "test-key").Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rec.query.Get("unit"); got != "usd" {
		t.Fatalf("unit = %q", got)
	}
	if got := rec.header.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("Authorization = %q", got)
	}
}

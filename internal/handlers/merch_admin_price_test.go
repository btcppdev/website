package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMerchAdminDecimalPrices(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		signed bool
		want   int64
		bad    bool
	}{
		{"65.00", false, 6500, false}, {"0.29", false, 29, false}, {"-2.50", true, -250, false},
		{"-2.50", false, 0, true}, {"1.234", false, 0, true}, {"NaN", false, 0, true}, {"1e3", false, 0, true}, {"999999999999", false, 0, true},
	} {
		got, err := merchAdminCents(tc.raw, tc.signed)
		if (err != nil) != tc.bad || got != tc.want {
			t.Fatalf("%q: %d %v", tc.raw, got, err)
		}
	}
	r := httptest.NewRequest("POST", "/admin/merch", strings.NewReader("base_price=65.00&base_price_cents=1&price_adjustment=-2.50"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if err := normalizeMerchAdminPrices(r); err != nil {
		t.Fatal(err)
	}
	if r.FormValue("base_price_cents") != "6500" || r.FormValue("price_delta_cents") != "-250" {
		t.Fatal("decimal inputs did not control stored prices")
	}
}

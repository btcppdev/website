package handlers

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMerchInches(t *testing.T) {
	tests := map[int]string{
		0:   "0",
		254: "10",
		250: "9.84",
		120: "4.72",
	}
	for mm, want := range tests {
		if got := merchInches(mm); got != want {
			t.Errorf("merchInches(%d) = %q, want %q", mm, got, want)
		}
	}
}

func TestMerchVariantInputFromFormConvertsInchesToMillimeters(t *testing.T) {
	form := url.Values{
		"length_inches": {"9.84"},
		"width_inches":  {"7.87"},
		"height_inches": {"4.72"},
	}
	r := httptest.NewRequest("POST", "/admin/merch/product/variants", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	got := merchVariantInputFromForm(r, "product")
	if got.LengthMM != 250 || got.WidthMM != 200 || got.HeightMM != 120 {
		t.Fatalf("dimensions = %dx%dx%dmm, want 250x200x120mm", got.LengthMM, got.WidthMM, got.HeightMM)
	}
}

func TestMerchVariantInputFromFormAcceptsLegacyMillimeters(t *testing.T) {
	form := url.Values{"length_mm": {"250"}}
	r := httptest.NewRequest("POST", "/admin/merch/product/variants", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if got := merchVariantInputFromForm(r, "product").LengthMM; got != 250 {
		t.Fatalf("length = %dmm, want 250mm", got)
	}
}

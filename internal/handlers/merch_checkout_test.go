package handlers

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestShopCheckoutDetailsFromRequestPreservesAndNormalizesValues(t *testing.T) {
	form := url.Values{
		"name":                       {"  Satoshi Nakamoto  "},
		"email":                      {"  SATOSHI@EXAMPLE.COM "},
		"fulfillment":                {"pickup"},
		"address1":                   {"  1 Genesis Block "},
		"postal_code":                {" 12345 "},
		"country":                    {" us "},
		"payment_method":             {"card"},
		"shipping_rate_id":           {" service-123 "},
		"shipping_rate_amount_cents": {" 725 "},
	}
	r := httptest.NewRequest("POST", "/shop/checkout", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	details := shopCheckoutDetailsFromRequest(r, "fallback@example.com")
	if details.Name != "Satoshi Nakamoto" || details.Email != "satoshi@example.com" {
		t.Fatalf("contact details = %#v", details)
	}
	if details.Fulfillment != "pickup" || details.PaymentMethod != "card" {
		t.Fatalf("checkout choices = %#v", details)
	}
	if details.ShippingRateID != "service-123" {
		t.Fatalf("shipping rate = %q", details.ShippingRateID)
	}
	if details.ShippingRateAmountCents != 725 {
		t.Fatalf("shipping rate amount = %d", details.ShippingRateAmountCents)
	}
	if details.Address1 != "1 Genesis Block" || details.PostalCode != "12345" || details.Country != "US" {
		t.Fatalf("shipping details = %#v", details)
	}
}

func TestShopCheckoutDetailsFromRequestUsesSafeDefaults(t *testing.T) {
	r := httptest.NewRequest("POST", "/shop/checkout", nil)
	details := shopCheckoutDetailsFromRequest(r, " Buyer@Example.com ")

	if details.Email != "" || details.Fulfillment != "ship" || details.Country != "US" || details.PaymentMethod != "btc" {
		t.Fatalf("defaults = %#v", details)
	}
}

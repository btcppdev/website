package handlers

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/types"
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
		"pickup_conf_id":             {" conference-123 "},
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
	if details.PickupConferenceID != "conference-123" {
		t.Fatalf("pickup conference = %q", details.PickupConferenceID)
	}
	if details.Address1 != "1 Genesis Block" || details.PostalCode != "12345" || details.Country != "US" {
		t.Fatalf("shipping details = %#v", details)
	}
}

func TestShopEventPickupClosesSevenDaysBeforeEvent(t *testing.T) {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.July, 22, 9, 0, 0, 0, loc)
	conf := &types.Conf{
		Ref:               "conference-123",
		PublicationStatus: "published",
		Timezone:          "America/Toronto",
		StartDate:         start,
	}
	cutoff := time.Date(2026, time.July, 15, 9, 0, 0, 0, loc)
	if !shopEventPickupOpenAt(conf, cutoff.Add(-time.Second)) {
		t.Fatal("pickup should remain open immediately before the cutoff")
	}
	if shopEventPickupOpenAt(conf, cutoff) {
		t.Fatal("pickup should close exactly seven days before the event")
	}
	if shopEventPickupOpenAt(conf, cutoff.Add(time.Second)) {
		t.Fatal("pickup should remain closed after the cutoff")
	}
}

func TestValidateShopPickupSelectionRejectsStaleConference(t *testing.T) {
	conf := &types.Conf{Ref: "current-conference"}
	if err := validateShopPickupSelection(conf, "old-conference"); err == nil {
		t.Fatal("expected stale pickup conference to be rejected")
	}
	if err := validateShopPickupSelection(conf, conf.Ref); err != nil {
		t.Fatalf("current pickup conference rejected: %v", err)
	}
}

func TestShopCheckoutDetailsFromRequestUsesSafeDefaults(t *testing.T) {
	r := httptest.NewRequest("POST", "/shop/checkout", nil)
	details := shopCheckoutDetailsFromRequest(r, " Buyer@Example.com ")

	if details.Email != "" || details.Fulfillment != "ship" || details.Country != "US" || details.PaymentMethod != "btc" {
		t.Fatalf("defaults = %#v", details)
	}
}

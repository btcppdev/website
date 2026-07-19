package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"btcpp-web/external/easyship"
	"btcpp-web/internal/types"
)

func TestParseShopRequestFormMultipart(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := map[string]string{
		"fulfillment": "ship",
		"address1":    "1600 Pennsylvania Avenue NW",
		"city":        "Washington",
		"postal_code": "20500",
		"country":     "US",
	}
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	r := httptest.NewRequest("POST", "/shop/shipping-rates", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())

	if err := parseShopRequestForm(r); err != nil {
		t.Fatalf("parse multipart form: %v", err)
	}
	for name, want := range fields {
		if got := r.FormValue(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestShopShippingQuoteUsesOriginallyDisplayedRateSet(t *testing.T) {
	raw := json.RawMessage(`{"courier_service_id":"displayed-service","total_charge":7.25}`)
	rateSet := &shopShippingRateSet{
		Destination: easyship.Address{Country: "US", Region: "TX", PostalCode: "78701"},
		Rates: []easyship.Rate{{
			ProviderQuoteID: "displayed-service", CourierName: "USPS",
			ServiceName: "Ground", AmountCents: 725, Currency: "USD", Raw: raw,
		}},
	}

	quote, err := shopShippingQuoteFromRateSet(rateSet, "displayed-service", 725)
	if err != nil {
		t.Fatalf("selected displayed rate: %v", err)
	}
	if quote.ProviderQuoteID != "displayed-service" || quote.AmountCents != 725 || quote.DestinationPostalCode != "78701" {
		t.Fatalf("quote = %#v", quote)
	}
}

func TestShopShippingQuoteRejectsClientPriceChange(t *testing.T) {
	rateSet := &shopShippingRateSet{Rates: []easyship.Rate{{ProviderQuoteID: "service-1", AmountCents: 725}}}
	if _, err := shopShippingQuoteFromRateSet(rateSet, "service-1", 1); err == nil {
		t.Fatal("expected changed client amount to be rejected")
	}
}

func TestShopShippingQuoteTreatsMissingDisplayedRateAsExpired(t *testing.T) {
	rateSet := &shopShippingRateSet{Rates: []easyship.Rate{{ProviderQuoteID: "new-service", AmountCents: 725}}}
	_, err := shopShippingQuoteFromRateSet(rateSet, "previously-displayed-service", 725)
	if !errors.Is(err, errShopShippingRatesExpired) {
		t.Fatalf("error = %v, want shipping rates expired", err)
	}
}

func TestShopShippingCartKeyChangesWithParcelData(t *testing.T) {
	item := &shopCartItem{
		Product: &types.MerchProduct{},
		Variant: &types.MerchVariant{ID: "variant-1", WeightGrams: 100, LengthMM: 200, WidthMM: 150, HeightMM: 80},
		Qty:     1, UnitPriceCents: 2500,
	}
	before := shopShippingCartKey([]*shopCartItem{item})
	item.Variant.WeightGrams = 200
	after := shopShippingCartKey([]*shopCartItem{item})
	if before == after {
		t.Fatal("shipping cart key did not change with parcel weight")
	}
}

func TestValidateShopVariantParcelRejectsMissingDimensions(t *testing.T) {
	product := &types.MerchProduct{Name: "Libre Relay Hat"}
	variant := &types.MerchVariant{WeightGrams: 227, LengthMM: 0, WidthMM: 0, HeightMM: 0}
	if err := validateShopVariantParcel(product, variant); err == nil || !strings.Contains(err.Error(), "Libre Relay Hat") {
		t.Fatalf("error = %v, want product-specific parcel error", err)
	}
	variant.LengthMM, variant.WidthMM, variant.HeightMM = 127, 178, 102
	if err := validateShopVariantParcel(product, variant); err != nil {
		t.Fatalf("valid parcel rejected: %v", err)
	}
}

func TestParseShopRequestFormURLEncoded(t *testing.T) {
	form := url.Values{"fulfillment": {"ship"}, "country": {"US"}}
	r := httptest.NewRequest("POST", "/shop/shipping-rates", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if err := parseShopRequestForm(r); err != nil {
		t.Fatalf("parse URL-encoded form: %v", err)
	}
	if got := r.FormValue("fulfillment"); got != "ship" {
		t.Fatalf("fulfillment = %q, want ship", got)
	}
}

func TestCanManageEasyshipShipment(t *testing.T) {
	quote := &types.ShippingRateQuote{Provider: types.ShippingProviderEasyship}
	tests := []struct {
		name  string
		order *types.ShopOrder
		quote *types.ShippingRateQuote
		want  bool
	}{
		{name: "paid shippable order", order: &types.ShopOrder{Status: types.ShopOrderStatusPaid, UnfulfilledShippingQuantity: 2}, quote: quote, want: true},
		{name: "partially refunded shippable order", order: &types.ShopOrder{Status: types.ShopOrderStatusPartiallyRefunded, UnfulfilledShippingQuantity: 1}, quote: quote, want: true},
		{name: "pending order", order: &types.ShopOrder{Status: types.ShopOrderStatusPending, UnfulfilledShippingQuantity: 1}, quote: quote},
		{name: "no quote", order: &types.ShopOrder{Status: types.ShopOrderStatusPaid, UnfulfilledShippingQuantity: 1}},
		{name: "nothing left to ship", order: &types.ShopOrder{Status: types.ShopOrderStatusPaid}, quote: quote},
		{name: "no order", quote: quote},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canManageEasyshipShipment(tt.order, tt.quote); got != tt.want {
				t.Fatalf("canManageEasyshipShipment() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestCanPrepareEasyshipShipmentOrderWithoutExistingRate(t *testing.T) {
	order := &types.ShopOrder{
		Status:                      types.ShopOrderStatusPaid,
		ShippingAddress:             &types.ShopAddress{Line1: "1 Main St"},
		UnfulfilledShippingQuantity: 1,
	}
	if !canPrepareEasyshipShipmentOrder(order) {
		t.Fatal("paid mailed order should be able to request a replacement Easyship rate")
	}
	order.ShippingAddress = nil
	if canPrepareEasyshipShipmentOrder(order) {
		t.Fatal("order without a shipping address should not offer Easyship preparation")
	}
}

func TestUpdateEasyshipDestinationPhoneRequiresPhone(t *testing.T) {
	order := &types.ShopOrder{ShippingAddress: &types.ShopAddress{}}
	if err := updateEasyshipDestinationPhone(nil, order, ""); err == nil || !strings.Contains(err.Error(), "phone number") {
		t.Fatalf("missing phone error = %v", err)
	}
	order.ShippingAddress.Phone = "+1 512 555 0100"
	if err := updateEasyshipDestinationPhone(nil, order, ""); err != nil {
		t.Fatalf("existing phone rejected: %v", err)
	}
}

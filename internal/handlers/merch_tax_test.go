package handlers

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"btcpp-web/internal/types"
)

func TestShopStripeTaxParamsUsesDestinationAndLineTotals(t *testing.T) {
	cart := []*shopCartItem{{
		Product: &types.MerchProduct{StripeTaxCode: "txcd_custom"},
		Variant: &types.MerchVariant{ID: "variant-1"},
		Qty:     2, LineTotalCents: 5000,
	}}
	address := &types.ShopAddress{
		Line1: "101 Main St", City: "Austin", Region: "TX",
		PostalCode: "78701", Country: "us",
	}

	params, err := shopStripeTaxParams(cart, address, 900)
	if err != nil {
		t.Fatalf("shopStripeTaxParams: %s", err)
	}
	if got := *params.CustomerDetails.Address.Country; got != "US" {
		t.Fatalf("country = %q, want US", got)
	}
	if len(params.LineItems) != 1 {
		t.Fatalf("line item count = %d, want 1", len(params.LineItems))
	}
	line := params.LineItems[0]
	if got := *line.Amount; got != 5000 {
		t.Fatalf("line amount = %d, want 5000", got)
	}
	if got := *line.Quantity; got != 2 {
		t.Fatalf("quantity = %d, want 2", got)
	}
	if got := *line.TaxCode; got != "txcd_custom" {
		t.Fatalf("tax code = %q, want txcd_custom", got)
	}
	if params.ShippingCost == nil || *params.ShippingCost.Amount != 900 {
		t.Fatalf("shipping amount was not included")
	}
}

func TestShopTaxAddressUsesSameDestinationForEveryPaymentMethod(t *testing.T) {
	for _, paymentMethod := range []string{"btc", "card"} {
		t.Run(paymentMethod, func(t *testing.T) {
			form := url.Values{
				"name": {"Ada"}, "address1": {"101 Main St"}, "city": {"Austin"},
				"region": {"TX"}, "postal_code": {"78701"}, "country": {"us"},
				"payment_method": {paymentMethod},
			}
			r := httptest.NewRequest("POST", "/shop/tax-quote", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			address, err := shopTaxAddress(nil, r, types.ShopFulfillmentShip, nil)
			if err != nil {
				t.Fatal(err)
			}
			if address.Country != "US" || address.PostalCode != "78701" {
				t.Fatalf("address = %#v", address)
			}
		})
	}
}

func TestShopTaxAddressUsesConferencePickupLocation(t *testing.T) {
	conf := &types.Conf{
		Venue: "Conference Hall", PickupAddressLine1: "500 Venue Way",
		PickupAddressCity: "Nashville", PickupAddressRegion: "TN",
		PickupAddressPostalCode: "37201", PickupAddressCountry: "us",
	}
	address, err := shopTaxAddress(nil, httptest.NewRequest("POST", "/shop/tax-quote", nil), types.ShopFulfillmentEventPickup, conf)
	if err != nil {
		t.Fatal(err)
	}
	if address.Name != "Conference Hall" || address.Country != "US" || address.PostalCode != "37201" {
		t.Fatalf("address = %#v", address)
	}
}

func TestShopStripeCheckoutUsesPrecalculatedShippingAndTax(t *testing.T) {
	order := &types.ShopOrder{
		ID: "order-id", PublicID: "public-id", BuyerEmail: "buyer@example.com",
		BuyerName: "Buyer", Currency: "USD", ShippingAmountCents: 725,
		SalesTaxAmountCents: 483, TotalCents: 6208,
		ShippingAddress: &types.ShopAddress{
			Name: "Buyer", Line1: "101 Main St", City: "Austin", Region: "TX",
			PostalCode: "78701", Country: "US",
		},
	}
	cart := []*shopCartItem{{
		Product: &types.MerchProduct{Name: "Hat"}, Variant: &types.MerchVariant{SKU: "HAT"},
		Qty: 1, UnitPriceCents: 5000, LineTotalCents: 5000,
	}}
	params := shopStripeCheckoutParams(order, cart, "https://example.test")
	if params.AutomaticTax == nil || params.AutomaticTax.Enabled == nil || *params.AutomaticTax.Enabled {
		t.Fatal("Stripe automatic tax should be disabled after the app calculates tax")
	}
	if params.ShippingAddressCollection != nil || len(params.ShippingOptions) != 0 {
		t.Fatal("Stripe should not collect an address or select shipping again")
	}
	var total int64
	for _, item := range params.LineItems {
		total += *item.PriceData.UnitAmount * *item.Quantity
	}
	if total != int64(order.TotalCents) {
		t.Fatalf("Stripe line item total = %d, want %d", total, order.TotalCents)
	}
	if params.PaymentIntentData == nil || params.PaymentIntentData.Shipping == nil {
		t.Fatal("captured shipping address was not forwarded to the payment intent")
	}
}

func TestShopStripeTaxParamsRejectsMissingAddress(t *testing.T) {
	if _, err := shopStripeTaxParams(nil, nil, 0); err == nil {
		t.Fatal("expected missing address error")
	}
}

func TestFillCartTotalsDoesNotGuessTax(t *testing.T) {
	page := &shopPage{Cart: []*shopCartItem{{LineTotalCents: 5000}}}
	fillCartTotals(page)
	if page.TaxCents != 0 {
		t.Fatalf("tax = %d, want destination-calculated zero placeholder", page.TaxCents)
	}
	if page.TotalCents != 7480 {
		t.Fatalf("total = %d, want subtotal plus shipping 7480", page.TotalCents)
	}
}

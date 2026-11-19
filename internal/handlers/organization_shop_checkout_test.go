package handlers

import (
	"btcpp-web/external/easyship"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"encoding/json"
	"fmt"
	"github.com/alexedwards/scs/v2"
	"github.com/stripe/stripe-go/v86"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrganizationDiscountAndTaxOrigin(t *testing.T) {
	a := &shopCartItem{Product: &types.MerchProduct{FulfillmentOrganizationID: "a"}, Variant: &types.MerchVariant{ID: "a"}, OriginalUnitPriceCents: 2501, UnitPriceCents: 2501, Qty: 2, LineTotalCents: 5002}
	b := &shopCartItem{Product: &types.MerchProduct{FulfillmentOrganizationID: "b"}, Variant: &types.MerchVariant{ID: "b"}, OriginalUnitPriceCents: 1000, UnitPriceCents: 1000, Qty: 1, LineTotalCents: 1000}
	if e := applyShopDiscount([]*shopCartItem{a, b}, &types.OrganizationShopDiscount{OrganizationID: "a", PercentOff: 10}); e != nil {
		t.Fatal(e)
	}
	if a.UnitPriceCents != 2251 || a.DiscountAmountCents != 500 || b.LineTotalCents != 1000 {
		t.Fatal("incorrect scoped discount or rounding")
	}
	address := &types.ShopAddress{Line1: "1 Buyer St", City: "Austin", Region: "TX", Country: "US", PostalCode: "78701"}
	origin := &types.EasyshipSettings{Line1: "2 Origin St", City: "Berlin", CountryAlpha2: "DE", PostalCode: "10115"}
	params, e := shopStripeTaxParams([]*shopCartItem{a}, address, 200, "USD", origin)
	if e != nil {
		t.Fatal(e)
	}
	if *params.LineItems[0].Amount != 4502 || *params.ShipFromDetails.Address.Country != "DE" || *params.CustomerDetails.Address.Country != "US" || *params.ShippingCost.Amount != 200 {
		t.Fatal("tax basis or origin mismatch")
	}
	key := shopShippingCartKey([]*shopCartItem{a, b})
	a.OriginRevision++
	if shopShippingCartKey([]*shopCartItem{a, b}) == key {
		t.Fatal("changed warehouse reused old quote")
	}
	key = shopShippingCartKey([]*shopCartItem{a, b})
	a.Product.CountryOfOrigin = "VN"
	if shopShippingCartKey([]*shopCartItem{a, b}) == key {
		t.Fatal("changed manufacturing country reused old quote")
	}
}
func TestMixedOriginShippingCombinations(t *testing.T) {
	groups := []shopOriginGroup{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}
	bundles := []shopShippingBundle{{}}
	bundles = combineShopShippingRates(bundles, groups[0], []easyship.Rate{{ProviderQuoteID: "fast", AmountCents: 500}, {ProviderQuoteID: "slow", AmountCents: 200}})
	bundles = combineShopShippingRates(bundles, groups[1], []easyship.Rate{{ProviderQuoteID: "b", AmountCents: 300}})
	if len(bundles) != 2 || len(bundles[0].Rates) != 2 || bundles[0].Rates[0].Rate.AmountCents+bundles[0].Rates[1].Rate.AmountCents != 500 {
		t.Fatalf("missing or incorrectly priced shipment: %+v", bundles)
	}
}

func TestMixedOriginTaxQuotesUseEachWarehouseAndShippingShare(t *testing.T) {
	var requests int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseForm(); e != nil {
			t.Error(e)
		}
		requests++
		country := r.Form.Get("ship_from_details[address][country]")
		shipping := r.Form.Get("shipping_cost[amount]")
		amount := r.Form.Get("line_items[0][amount]")
		tax := 100
		if country == "JP" {
			tax = 200
			if shipping != "400" || amount != "3000" {
				t.Errorf("Japan quote %s %s", shipping, amount)
			}
		} else if country == "DE" {
			if shipping != "200" || amount != "2000" {
				t.Errorf("Germany quote %s %s", shipping, amount)
			}
		} else {
			t.Errorf("missing origin %q", country)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"taxcalc_%s","object":"tax.calculation","tax_amount_exclusive":%d,"expires_at":2000000000}`, country, tax)
	}))
	defer provider.Close()
	previous := stripe.GetBackend(stripe.APIBackend)
	defer stripe.SetBackend(stripe.APIBackend, previous)
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(provider.URL), HTTPClient: provider.Client()}))
	origin := func(country string) *types.EasyshipSettings {
		return &types.EasyshipSettings{ContactName: "Test", Line1: "1 Origin St", City: "City", Region: "Region", PostalCode: "12345", CountryAlpha2: country}
	}
	cart := []*shopCartItem{
		{Product: &types.MerchProduct{FulfillmentOrganizationID: "a"}, Variant: &types.MerchVariant{ID: "a"}, Qty: 1, UnitPriceCents: 2000, LineTotalCents: 2000, ShipFrom: origin("DE")},
		{Product: &types.MerchProduct{FulfillmentOrganizationID: "b"}, Variant: &types.MerchVariant{ID: "b"}, Qty: 1, UnitPriceCents: 3000, LineTotalCents: 3000, ShipFrom: origin("JP")},
	}
	bundle, _ := json.Marshal(shopShippingBundle{Rates: []shopOriginRate{{OrganizationID: "a", Rate: easyship.Rate{AmountCents: 200}}, {OrganizationID: "b", Rate: easyship.Rate{AmountCents: 400}}}})
	app := &config.AppContext{Session: scs.New()}
	req := httptest.NewRequest("POST", "/shop/tax-quote", strings.NewReader("shipping_rate_id=combined&shipping_rate_amount_cents=600"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()
	app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := saveShopShippingRateSet(app, r, cart, easyship.Address{}, []easyship.Rate{{ProviderQuoteID: "combined", AmountCents: 600, Raw: bundle}}); e != nil {
			t.Fatal(e)
		}
		quotes, total, e := shopCheckoutTaxQuotes(app, r, cart, &types.ShopAddress{Line1: "2 Buyer St", City: "Austin", Country: "US"}, 600, types.ShopFulfillmentShip)
		if e != nil || total != 300 || len(quotes) != 2 || quotes[0].OriginKey != "a" || quotes[1].OriginKey != "b" {
			t.Fatalf("split tax quotes %+v %d %v", quotes, total, e)
		}
	})).ServeHTTP(httptest.NewRecorder(), req)
	if requests != 2 {
		t.Fatalf("tax requests %d", requests)
	}
}

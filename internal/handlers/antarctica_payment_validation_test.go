package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"btcpp-web/internal/types"
	"context"
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func TestExpeditionStripeSessionValidation(t *testing.T) {
	expiry := time.Now().Add(time.Hour).Truncate(time.Second)
	order := &getters.ServiceOrderView{ID: "order", Quote: getters.ServiceQuote{TotalCents: 1670000, ExpiresAt: expiry}}
	valid := func() *stripe.CheckoutSession {
		return &stripe.CheckoutSession{ID: "cs_test_fixture", URL: "https://checkout.stripe.com/c/pay/test", Mode: "payment", Currency: "usd", AmountTotal: 1670000, Status: "open", ExpiresAt: expiry.Unix(), Metadata: map[string]string{"checkout-kind": "service", "service-attempt-id": "attempt", "service-order-id": "order"}}
	}
	if err := validateExpeditionStripeSession(valid(), order, "attempt", false); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*stripe.CheckoutSession){
		"wrong amount":    func(s *stripe.CheckoutSession) { s.AmountTotal-- },
		"foreign order":   func(s *stripe.CheckoutSession) { s.Metadata["service-order-id"] = "other" },
		"foreign attempt": func(s *stripe.CheckoutSession) { s.Metadata["service-attempt-id"] = "other" },
		"foreign host":    func(s *stripe.CheckoutSession) { s.URL = "https://checkout.stripe.com.attacker.test/pay" },
		"http":            func(s *stripe.CheckoutSession) { s.URL = "http://checkout.stripe.com/pay" },
		"live mismatch":   func(s *stripe.CheckoutSession) { s.Livemode = true },
		"expired":         func(s *stripe.CheckoutSession) { s.Status = "expired" },
		"changed expiry":  func(s *stripe.CheckoutSession) { s.ExpiresAt++ },
	} {
		t.Run(name, func(t *testing.T) {
			s := valid()
			mutate(s)
			if validateExpeditionStripeSession(s, order, "attempt", false) == nil {
				t.Fatal("untrusted session accepted")
			}
		})
	}
}
func TestExpeditionTaxValidation(t *testing.T) {
	app := &config.AppContext{Env: &types.EnvConfig{}}
	charge := expedition.Charge{TotalCents: 1670000, Provider: expedition.Stripe, ExpiresAt: time.Now().Add(time.Hour)}
	address := &types.ShopAddress{Line1: "123 Test St", City: "Austin", Country: "US"}
	valid := func() *stripe.TaxCalculation {
		return &stripe.TaxCalculation{ID: "taxcalc_fixture", Currency: "usd", AmountTotal: 1670000, ExpiresAt: time.Now().Add(time.Hour).Unix(), LineItems: &stripe.TaxCalculationLineItemList{Data: []*stripe.TaxCalculationLineItem{{Amount: 1670000, Quantity: 1, Reference: expedition.ProductCode, TaxBehavior: "inclusive", TaxCode: types.StripeTaxCodeNontaxable}}}}
	}
	for name, mutate := range map[string]func(*stripe.TaxCalculation){
		"valid":                    func(*stripe.TaxCalculation) {},
		"wrong total":              func(c *stripe.TaxCalculation) { c.AmountTotal-- },
		"unexpected included tax":  func(c *stripe.TaxCalculation) { c.TaxAmountInclusive = 100; c.LineItems.Data[0].AmountTax = 100 },
		"exclusive tax":            func(c *stripe.TaxCalculation) { c.TaxAmountExclusive = 100 },
		"wrong product":            func(c *stripe.TaxCalculation) { c.LineItems.Data[0].Reference = "hat" },
		"wrong tax code":           func(c *stripe.TaxCalculation) { c.LineItems.Data[0].TaxCode = "txcd_other" },
		"insufficient card window": func(c *stripe.TaxCalculation) { c.ExpiresAt = time.Now().Add(20 * time.Minute).Unix() },
	} {
		t.Run(name, func(t *testing.T) {
			c := valid()
			mutate(c)
			_, err := calculateExpeditionTax(context.Background(), app, charge, address, func(p *stripe.TaxCalculationParams) (*stripe.TaxCalculation, error) {
				if *p.LineItems[0].Amount != charge.TotalCents || *p.LineItems[0].TaxBehavior != "inclusive" || *p.LineItems[0].TaxCode != ticketStripeTaxCode(nil) {
					t.Fatal("untrusted tax request")
				}
				return c, nil
			})
			if (err == nil) != (name == "valid") {
				t.Fatal(name, err)
			}
		})
	}
}

func TestExpeditionBitcoinOnlyLaunch(t *testing.T) {
	app := &config.AppContext{Env: &types.EnvConfig{StripeKey: "sk_test_fixture"}}
	if !antarcticaPaymentProviderAllowed(app, expedition.Bitcoin) {
		t.Fatal("Bitcoin must remain available")
	}
	for _, provider := range []string{expedition.Stripe, "", "unknown"} {
		if antarcticaPaymentProviderAllowed(app, provider) {
			t.Fatalf("unexpected launch provider: %s", provider)
		}
	}
	order := &getters.ServiceOrderView{Status: "pending", Currency: "USD", TotalCents: 100, Quote: getters.ServiceQuote{ProductCode: expedition.ProductCode, Provider: expedition.Stripe, TotalCents: 100, ExpiresAt: time.Now().Add(time.Hour)}}
	if serviceOrderCanPay(order, app, time.Now()) {
		t.Fatal("existing Stripe order payable")
	}
	if _, err := expeditionStripeCheckout(context.Background(), app, "buyer", order); err == nil {
		t.Fatal("direct Stripe handoff allowed")
	}
	app.Env.AntarcticaStripeCheckoutEnabled = true
	if !antarcticaPaymentProviderAllowed(app, expedition.Stripe) {
		t.Fatal("explicit opt-in unavailable")
	}
}

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

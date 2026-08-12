package handlers

import (
	"context"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	stripe "github.com/stripe/stripe-go/v86"
)

func TestBuildTicketCheckoutAddOnOfferConvertsAndSignsStripePrice(t *testing.T) {
	resetTicketFXQuoteCache(t)
	originalCreate := createStripeTicketFXQuote
	t.Cleanup(func() { createStripeTicketFXQuote = originalCreate })
	createCalls := 0
	createStripeTicketFXQuote = func(_ context.Context, key string, from []string, to string) (*stripe.FxQuote, error) {
		createCalls++
		if key != "sk_test_fx" || len(from) != 1 || from[0] != "eur" || to != "usd" {
			t.Fatalf("unexpected Stripe FX request: key=%q from=%v to=%q", key, from, to)
		}
		return &stripe.FxQuote{
			ID:            "fxq_test",
			LockStatus:    stripe.FxQuoteLockStatusActive,
			LockExpiresAt: time.Now().Add(time.Hour).Unix(),
			Rates: map[string]*stripe.FxQuoteRates{
				"eur": {ExchangeRate: 1.1},
			},
		}, nil
	}

	ctx := ticketFXTestContext()
	conf := &types.Conf{Ref: "conf-berlin"}
	tix := &types.ConfTicket{Currency: "EUR", Symbol: "€"}
	variant := &types.MerchVariant{ID: "variant-hat", PriceDeltaCents: 200}
	product := &types.MerchProduct{
		Name: "Hat", Currency: "USD", BasePriceCents: 3600,
		Variants: []*types.MerchVariant{variant},
	}

	offer, err := buildTicketCheckoutAddOnOffer(context.Background(), ctx, conf, tix, []*types.MerchProduct{product})
	if err != nil {
		t.Fatal(err)
	}
	if len(offer.Products) != 1 || offer.Products[0].UnitPriceCents != 3455 {
		t.Fatalf("converted offer = %#v, want one €34.55 item", offer.Products)
	}
	payload, err := verifyTicketAddOnQuote(ctx, conf, "EUR", offer.Quote, offer.QuoteHMAC)
	if err != nil {
		t.Fatal(err)
	}
	if payload.QuoteID != "fxq_test" || payload.VariantPrices[variant.ID] != 3455 || payload.Rates["usd"] != 1/1.1 {
		t.Fatalf("signed payload = %#v", payload)
	}
	if _, err := buildTicketCheckoutAddOnOffer(context.Background(), ctx, conf, tix, []*types.MerchProduct{product}); err != nil {
		t.Fatal(err)
	}
	if createCalls != 1 {
		t.Fatalf("Stripe FX quote calls = %d, want 1 cached call", createCalls)
	}
}

func TestBuildTicketCheckoutAddOnOfferSkipsStripeForMatchingCurrency(t *testing.T) {
	resetTicketFXQuoteCache(t)
	originalCreate := createStripeTicketFXQuote
	t.Cleanup(func() { createStripeTicketFXQuote = originalCreate })
	createStripeTicketFXQuote = func(context.Context, string, []string, string) (*stripe.FxQuote, error) {
		t.Fatal("Stripe FX API should not be called for matching currencies")
		return nil, nil
	}

	ctx := ticketFXTestContext()
	conf := &types.Conf{Ref: "conf-us"}
	tix := &types.ConfTicket{Currency: "USD", Symbol: "$"}
	variant := &types.MerchVariant{ID: "variant-shirt"}
	product := &types.MerchProduct{
		Name: "Shirt", Currency: "USD", BasePriceCents: 2500,
		Variants: []*types.MerchVariant{variant},
	}
	offer, err := buildTicketCheckoutAddOnOffer(context.Background(), ctx, conf, tix, []*types.MerchProduct{product})
	if err != nil {
		t.Fatal(err)
	}
	if offer.Products[0].UnitPriceCents != 2500 {
		t.Fatalf("same-currency price = %d, want 2500", offer.Products[0].UnitPriceCents)
	}
}

func TestVerifyTicketAddOnQuoteRejectsTamperingAndExpiry(t *testing.T) {
	ctx := ticketFXTestContext()
	conf := &types.Conf{Ref: "conf-berlin"}
	payload := ticketAddOnQuotePayload{
		Version: 1, ConferenceID: conf.Ref, TargetCurrency: "eur",
		ExpiresAt: time.Now().Add(time.Hour).Unix(), VariantPrices: map[string]uint{"variant": 1000},
	}
	encoded, signature, err := signTicketAddOnQuote(ctx, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyTicketAddOnQuote(ctx, conf, "EUR", encoded+"x", signature); err == nil {
		t.Fatal("tampered quote was accepted")
	}
	payload.ExpiresAt = time.Now().Add(-time.Second).Unix()
	encoded, signature, err = signTicketAddOnQuote(ctx, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyTicketAddOnQuote(ctx, conf, "EUR", encoded, signature); err == nil {
		t.Fatal("expired quote was accepted")
	}
}

func TestStripeCheckoutShippingAddressSupportsCurrentAndLegacyPayloads(t *testing.T) {
	current := &stripeCheckoutEvent{CheckoutSession: stripe.CheckoutSession{
		CollectedInformation: &stripe.CheckoutSessionCollectedInformation{
			Phone: "+49 30 555", ShippingDetails: &stripe.CheckoutSessionCollectedInformationShippingDetails{
				Name: "Ada", Address: &stripe.Address{Line1: "Current 1", City: "Berlin", Country: "DE"},
			},
		},
	}}
	if got := stripeCheckoutShippingAddress(current); got == nil || got.Line1 != "Current 1" || got.Phone != "+49 30 555" {
		t.Fatalf("current shipping address = %#v", got)
	}
	legacy := &stripeCheckoutEvent{ShippingDetails: &stripe.ShippingDetails{
		Name: "Grace", Phone: "+1 555", Address: &stripe.Address{Line1: "Legacy 2", City: "Austin", Country: "US"},
	}}
	if got := stripeCheckoutShippingAddress(legacy); got == nil || got.Line1 != "Legacy 2" || got.Phone != "+1 555" {
		t.Fatalf("legacy shipping address = %#v", got)
	}
}

func ticketFXTestContext() *config.AppContext {
	return &config.AppContext{Env: &types.EnvConfig{
		StripeKey: "sk_test_fx",
		HMACKey:   [32]byte{1, 2, 3, 4},
	}}
}

func resetTicketFXQuoteCache(t *testing.T) {
	t.Helper()
	ticketFXQuoteCache.Lock()
	ticketFXQuoteCache.quotes = make(map[string]*stripe.FxQuote)
	ticketFXQuoteCache.Unlock()
}

package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	stripe "github.com/stripe/stripe-go/v86"
)

const ticketAddOnFXQuoteTimeout = 10 * time.Second

type ticketCheckoutAddOnProduct struct {
	Product        *types.MerchProduct
	Variant        *types.MerchVariant
	UnitPriceCents uint
}

type ticketAddOnQuotePayload struct {
	Version        int                `json:"version"`
	ConferenceID   string             `json:"conference_id"`
	QuoteID        string             `json:"quote_id,omitempty"`
	TargetCurrency string             `json:"target_currency"`
	ExpiresAt      int64              `json:"expires_at"`
	Rates          map[string]float64 `json:"rates"`
	VariantPrices  map[string]uint    `json:"variant_prices"`
}

type ticketAddOnOffer struct {
	Products  []*ticketCheckoutAddOnProduct
	Quote     string
	QuoteHMAC string
}

var createStripeTicketFXQuote = func(requestContext context.Context, apiKey string, fromCurrencies []string, toCurrency string) (*stripe.FxQuote, error) {
	client := stripe.NewClient(apiKey)
	return client.V1FxQuotes.Create(requestContext, &stripe.FxQuoteCreateParams{
		FromCurrencies: stripe.StringSlice(fromCurrencies),
		LockDuration:   stripe.String(stripe.FxQuoteLockDurationDay),
		ToCurrency:     stripe.String(toCurrency),
		Usage: &stripe.FxQuoteCreateUsageParams{
			Type: stripe.String(stripe.FxQuoteUsageTypePayment),
		},
	})
}

var ticketFXQuoteCache = struct {
	sync.Mutex
	quotes map[string]*stripe.FxQuote
}{quotes: make(map[string]*stripe.FxQuote)}

func cachedStripeTicketFXQuote(requestContext context.Context, apiKey string, fromCurrencies []string, toCurrency string) (*stripe.FxQuote, error) {
	cacheKey := strings.Join(fromCurrencies, ",") + "->" + toCurrency
	ticketFXQuoteCache.Lock()
	defer ticketFXQuoteCache.Unlock()
	now := time.Now().UTC().Unix()
	if quote := ticketFXQuoteCache.quotes[cacheKey]; quote != nil && quote.LockStatus == stripe.FxQuoteLockStatusActive && quote.LockExpiresAt > now+300 {
		return quote, nil
	}
	quote, err := createStripeTicketFXQuote(requestContext, apiKey, fromCurrencies, toCurrency)
	if err != nil {
		return nil, err
	}
	ticketFXQuoteCache.quotes[cacheKey] = quote
	return quote, nil
}

func ticketCheckoutAddOnOffer(requestContext context.Context, ctx *config.AppContext, conf *types.Conf, tix *types.ConfTicket) (*ticketAddOnOffer, error) {
	products := ticketCheckoutAddOnProducts(ctx, conf)
	return buildTicketCheckoutAddOnOffer(requestContext, ctx, conf, tix, products)
}

func buildTicketCheckoutAddOnOffer(requestContext context.Context, ctx *config.AppContext, conf *types.Conf, tix *types.ConfTicket, products []*types.MerchProduct) (*ticketAddOnOffer, error) {
	if len(products) == 0 {
		return &ticketAddOnOffer{}, nil
	}
	if ctx == nil || ctx.Env == nil || conf == nil || tix == nil {
		return nil, fmt.Errorf("checkout configuration is incomplete")
	}
	targetCurrency := normalizedTicketCurrency(tix.Currency)
	if targetCurrency == "" {
		return nil, fmt.Errorf("ticket currency is required for add-on pricing")
	}

	productCurrency := ""
	for _, product := range products {
		source := normalizedTicketCurrency(firstNonEmpty(product.Currency, "USD"))
		if source == "" {
			return nil, fmt.Errorf("merchandise currency is required for %s", product.Name)
		}
		if productCurrency == "" {
			productCurrency = source
		} else if productCurrency != source {
			return nil, fmt.Errorf("ticket add-ons must use one merchandise currency")
		}
	}

	now := time.Now().UTC()
	payload := ticketAddOnQuotePayload{
		Version:        1,
		ConferenceID:   conf.Ref,
		TargetCurrency: targetCurrency,
		ExpiresAt:      now.Add(24 * time.Hour).Unix(),
		Rates:          map[string]float64{productCurrency: 1},
		VariantPrices:  make(map[string]uint, len(products)),
	}
	if productCurrency != targetCurrency {
		if strings.TrimSpace(ctx.Env.StripeKey) == "" {
			return nil, fmt.Errorf("Stripe is not configured for add-on currency conversion")
		}
		quoteContext, cancel := context.WithTimeout(requestContext, ticketAddOnFXQuoteTimeout)
		defer cancel()
		// Stripe's to_currency must be the account's settlement currency. Merch
		// prices are stored in that currency (currently USD), so request the
		// ticket/presentment currency as the from_currency and invert Stripe's
		// settlement rate to price the USD merchandise in the ticket currency.
		quote, err := cachedStripeTicketFXQuote(quoteContext, ctx.Env.StripeKey, []string{targetCurrency}, productCurrency)
		if err != nil {
			return nil, fmt.Errorf("create Stripe FX quote: %w", err)
		}
		if quote == nil || strings.TrimSpace(quote.ID) == "" || quote.LockStatus != stripe.FxQuoteLockStatusActive || quote.LockExpiresAt <= now.Unix() {
			return nil, fmt.Errorf("Stripe returned an inactive FX quote")
		}
		payload.QuoteID = quote.ID
		payload.ExpiresAt = quote.LockExpiresAt
		settlementRate := quote.Rates[targetCurrency]
		if settlementRate == nil || settlementRate.ExchangeRate <= 0 || math.IsNaN(settlementRate.ExchangeRate) || math.IsInf(settlementRate.ExchangeRate, 0) {
			return nil, fmt.Errorf("Stripe FX quote omitted the %s rate", strings.ToUpper(targetCurrency))
		}
		payload.Rates[productCurrency] = 1 / settlementRate.ExchangeRate
	}

	offer := &ticketAddOnOffer{}
	for _, product := range products {
		if product == nil || len(product.Variants) == 0 {
			continue
		}
		variant := product.Variants[0]
		source := normalizedTicketCurrency(firstNonEmpty(product.Currency, "USD"))
		converted, err := convertTicketAddOnCents(merchVariantPrice(product, variant), payload.Rates[source])
		if err != nil {
			return nil, fmt.Errorf("convert %s add-on price: %w", product.Name, err)
		}
		payload.VariantPrices[variant.ID] = converted
		offer.Products = append(offer.Products, &ticketCheckoutAddOnProduct{
			Product:        product,
			Variant:        variant,
			UnitPriceCents: converted,
		})
	}
	encoded, signature, err := signTicketAddOnQuote(ctx, payload)
	if err != nil {
		return nil, err
	}
	offer.Quote = encoded
	offer.QuoteHMAC = signature
	return offer, nil
}

func populateTicketCheckoutAddOns(requestContext context.Context, ctx *config.AppContext, page *TixFormPage) error {
	if page == nil {
		return fmt.Errorf("ticket checkout page is required")
	}
	offer, err := ticketCheckoutAddOnOffer(requestContext, ctx, page.Conf, page.Tix)
	if err != nil {
		page.AddOnUnavailable = "Add-ons are temporarily unavailable. You can still continue with just your ticket."
		return err
	}
	page.AddOnProducts = offer.Products
	page.AddOnQuote = offer.Quote
	page.AddOnQuoteHMAC = offer.QuoteHMAC
	return nil
}

func normalizedTicketCurrency(currency string) string {
	currency = strings.ToLower(strings.TrimSpace(currency))
	if len(currency) != 3 {
		return ""
	}
	return currency
}

func convertTicketAddOnCents(sourceCents uint, rate float64) (uint, error) {
	converted := math.Round(float64(sourceCents) * rate)
	if converted < 0 || converted > float64(^uint(0)) || math.IsNaN(converted) || math.IsInf(converted, 0) {
		return 0, fmt.Errorf("converted amount is out of range")
	}
	return uint(converted), nil
}

func signTicketAddOnQuote(ctx *config.AppContext, payload ticketAddOnQuotePayload) (string, string, error) {
	if ctx == nil || ctx.Env == nil {
		return "", "", fmt.Errorf("checkout signing is not configured")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("encode add-on quote: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	return encoded, ticketAddOnQuoteSignature(ctx, encoded), nil
}

func ticketAddOnQuoteSignature(ctx *config.AppContext, encoded string) string {
	mac := hmac.New(sha256.New, ctx.Env.HMACKey[:])
	mac.Write([]byte("ticket-add-on-fx-quote\x00"))
	mac.Write([]byte(encoded))
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyTicketAddOnQuote(ctx *config.AppContext, conf *types.Conf, targetCurrency, encoded, signature string) (*ticketAddOnQuotePayload, error) {
	if ctx == nil || ctx.Env == nil || conf == nil || encoded == "" || signature == "" {
		return nil, fmt.Errorf("add-on pricing quote is missing")
	}
	expected := ticketAddOnQuoteSignature(ctx, encoded)
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return nil, fmt.Errorf("add-on pricing quote signature is invalid")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode add-on pricing quote: %w", err)
	}
	var payload ticketAddOnQuotePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse add-on pricing quote: %w", err)
	}
	if payload.Version != 1 || payload.ConferenceID != conf.Ref || payload.TargetCurrency != normalizedTicketCurrency(targetCurrency) {
		return nil, fmt.Errorf("add-on pricing quote does not match this checkout")
	}
	if payload.ExpiresAt <= time.Now().UTC().Unix() {
		return nil, fmt.Errorf("add-on pricing quote has expired")
	}
	return &payload, nil
}

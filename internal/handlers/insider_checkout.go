package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"btcpp-web/internal/servicestripe"

	"github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/checkout/session"
)

const (
	insiderAnnualMembershipCents  = servicebilling.InsiderAnnualCents
	insiderMonthlyMembershipCents = servicebilling.InsiderMonthlyCents
	insiderArticleUnlockCents     = int64(210)
	insiderQuoteTTL               = 45 * time.Minute
	insiderQuoteMaxFuture         = 50 * time.Minute
)

type insiderCheckoutRequest struct {
	BillingMode               string          `json:"billing_mode,omitempty"`
	RequestID                 string          `json:"request_id"`
	ProductCode               string          `json:"product_code"`
	ResourceID                string          `json:"resource_id,omitempty"`
	ContributionPublicationID string          `json:"contribution_publication_id,omitempty"`
	PersonID                  string          `json:"person_id"`
	AmountCents               int64           `json:"amount_cents"`
	Currency                  string          `json:"currency"`
	PaymentProvider           string          `json:"payment_provider"`
	QuoteExpiresAt            time.Time       `json:"quote_expires_at"`
	SuccessURL                string          `json:"success_url"`
	CancelURL                 string          `json:"cancel_url"`
	PublicCredit              json.RawMessage `json:"public_credit,omitempty"`
}

type insiderFulfillmentSnapshot struct {
	ContributionPublicationID string          `json:"contribution_publication_id,omitempty"`
	PublicCredit              json.RawMessage `json:"public_credit,omitempty"`
}

type insiderSettlementEvent struct {
	AccessAction              string          `json:"access_action,omitempty"`
	ReversesEventID           string          `json:"reverses_event_id,omitempty"`
	AdjustmentReference       string          `json:"adjustment_reference,omitempty"`
	AdjustmentAmountCents     int64           `json:"adjustment_amount_cents,omitempty"`
	AdjustmentReason          string          `json:"adjustment_reason,omitempty"`
	EventID                   string          `json:"event_id"`
	EventKind                 string          `json:"event_kind"`
	ProductCode               string          `json:"product_code"`
	ResourceID                string          `json:"resource_id,omitempty"`
	ContributionPublicationID string          `json:"contribution_publication_id,omitempty"`
	SourceReference           string          `json:"source_reference"`
	OrderPublicID             string          `json:"order_public_id"`
	PersonID                  string          `json:"person_id"`
	BuyerEmail                string          `json:"buyer_email"`
	Description               string          `json:"description"`
	Provider                  string          `json:"provider"`
	ProviderReference         string          `json:"provider_reference"`
	MerchantName              string          `json:"merchant_name"`
	AmountCents               int64           `json:"amount_cents"`
	TaxCents                  int64           `json:"tax_cents"`
	SettledSats               int64           `json:"settled_sats,omitempty"`
	Currency                  string          `json:"currency"`
	PublicCredit              json.RawMessage `json:"public_credit,omitempty"`
	OccurredAt                time.Time       `json:"occurred_at"`
	QuotedAt                  time.Time       `json:"quoted_at"`
	AccessStartsAt            *time.Time      `json:"access_starts_at,omitempty"`
	AccessEndsAt              *time.Time      `json:"access_ends_at,omitempty"`
}

type insiderCheckoutResponse struct {
	CheckoutURL string `json:"checkout_url"`
	OrderID     string `json:"order_id"`
	CheckoutID  string `json:"checkout_id,omitempty"`
}

// CreateInsiderCheckout is the narrow product adapter between Insider and
// shared commerce. The authenticated Insider service chooses the product; this
// handler revalidates all fixed prices and shapes before freezing a quote.
func CreateInsiderCheckout(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	if !validInsiderServiceAuthorization(r.Header.Get("Authorization"), app.Env.InsiderServiceToken) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="btcpp-insider"`)
		writeInsiderServiceError(w, http.StatusUnauthorized, "A valid Insider service token is required.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request insiderCheckoutRequest
	if err := decoder.Decode(&request); err != nil || ensureInsiderJSONEnd(decoder) != nil {
		writeInsiderServiceError(w, http.StatusBadRequest, "The checkout request is invalid.")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) != request.RequestID {
		writeInsiderServiceError(w, http.StatusBadRequest, "The checkout idempotency key is invalid.")
		return
	}
	if request.BillingMode == "recurring" {
		serveInsiderRecurringCheckout(w, r, app, request, servicestripe.CheckoutClient{Create: session.New, Retrieve: session.Get, Live: strings.HasPrefix(app.Env.StripeKey, "sk_live_")})
		return
	}
	if request.BillingMode != "" && request.BillingMode != "fixed_term" {
		writeInsiderServiceError(w, http.StatusUnprocessableEntity, "The billing mode is invalid.")
		return
	}
	if strings.TrimSpace(app.Env.InsiderFulfillmentEndpoint) == "" || strings.TrimSpace(app.Env.InsiderFulfillmentToken) == "" || strings.TrimSpace(app.Env.InsiderMerchantName) == "" {
		writeInsiderServiceError(w, http.StatusServiceUnavailable, "Insider fulfillment is not configured.")
		return
	}
	quote, err := resolveInsiderServiceQuote(request, time.Now().UTC())
	if err != nil || !allowedInsiderReturnURL(request.SuccessURL, app.Env.InsiderAllowedReturnOrigins) || !allowedInsiderReturnURL(request.CancelURL, app.Env.InsiderAllowedReturnOrigins) {
		writeInsiderServiceError(w, http.StatusUnprocessableEntity, "The checkout request could not be verified.")
		return
	}
	purchase, err := getters.CreateServicePurchase(app, request.PersonID, request.RequestID, quote)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if !errors.Is(err, getters.ErrServiceQuote) && !errors.Is(err, getters.ErrServicePurchaseConflict) && !errors.Is(err, getters.ErrServiceBuyer) {
			status = http.StatusInternalServerError
			app.Err.Printf("create Insider purchase: %s", err)
		}
		writeInsiderServiceError(w, status, "The purchase could not be created.")
		return
	}
	attempt, err := getters.BeginServicePayment(app, request.PersonID, purchase.OrderID)
	if err != nil {
		app.Err.Printf("begin Insider payment for order %s: %s", purchase.OrderID, err)
		writeInsiderServiceError(w, http.StatusInternalServerError, "The payment could not be started.")
		return
	}
	checkoutURL, reference, err := startInsiderProviderCheckout(app, request, quote, purchase, attempt)
	if err != nil {
		app.Err.Printf("start Insider %s checkout for order %s: %s", quote.Provider, purchase.OrderID, err)
		writeInsiderServiceError(w, http.StatusBadGateway, "The payment provider is temporarily unavailable.")
		return
	}
	if attempt.Reference == "" {
		if err := getters.BindServicePayment(app, attempt.ID, quote.Provider, reference); err != nil {
			app.Err.Printf("bind Insider payment for order %s: %s", purchase.OrderID, err)
			writeInsiderServiceError(w, http.StatusInternalServerError, "The payment could not be saved.")
			return
		}
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(insiderCheckoutResponse{CheckoutURL: checkoutURL, OrderID: purchase.PublicID})
}

func resolveInsiderServiceQuote(request insiderCheckoutRequest, now time.Time) (getters.ServiceQuote, error) {
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.PersonID = strings.TrimSpace(request.PersonID)
	request.ProductCode = strings.TrimSpace(request.ProductCode)
	request.ResourceID = strings.TrimSpace(request.ResourceID)
	request.ContributionPublicationID = strings.TrimSpace(request.ContributionPublicationID)
	request.PaymentProvider = strings.ToLower(strings.TrimSpace(request.PaymentProvider))
	if len(request.RequestID) < 16 || len(request.RequestID) > 128 || request.PersonID == "" ||
		(request.PaymentProvider != "stripe" && request.PaymentProvider != "opennode") || !strings.EqualFold(strings.TrimSpace(request.Currency), "USD") ||
		!request.QuoteExpiresAt.After(now) || request.QuoteExpiresAt.After(now.Add(insiderQuoteMaxFuture)) {
		return getters.ServiceQuote{}, getters.ErrServiceQuote
	}
	if len(request.PublicCredit) > 0 {
		var credit struct {
			PersonID string `json:"person_id"`
			Name     string `json:"name"`
		}
		if request.ContributionPublicationID == "" || json.Unmarshal(request.PublicCredit, &credit) != nil ||
			strings.TrimSpace(credit.PersonID) != request.PersonID || strings.TrimSpace(credit.Name) == "" {
			return getters.ServiceQuote{}, getters.ErrServiceQuote
		}
	}
	fulfillment, err := json.Marshal(insiderFulfillmentSnapshot{
		ContributionPublicationID: request.ContributionPublicationID,
		PublicCredit:              request.PublicCredit,
	})
	if err != nil {
		return getters.ServiceQuote{}, getters.ErrServiceQuote
	}
	quote := getters.ServiceQuote{
		ProductCode: request.ProductCode, ResourceID: request.ResourceID, Provider: request.PaymentProvider,
		Quantity: 1, UnitCents: request.AmountCents, TotalCents: request.AmountCents, TaxCents: 0,
		ExpiresAt: request.QuoteExpiresAt.UTC(), Fulfillment: fulfillment,
	}
	switch request.ProductCode {
	case servicebilling.InsiderAnnualProduct, servicebilling.InsiderMonthlyProduct:
		start := request.QuoteExpiresAt.UTC().Add(-insiderQuoteTTL)
		plan, end, err := servicebilling.ProductTerm(request.ProductCode, request.PaymentProvider, 1, start)
		if err != nil || request.AmountCents != plan.UnitCents || request.ResourceID != "" || (request.ProductCode == servicebilling.InsiderMonthlyProduct && request.ContributionPublicationID != "") {
			return getters.ServiceQuote{}, getters.ErrServiceQuote
		}
		quote.Name, quote.PeriodStart, quote.PeriodEnd = plan.Name, &start, &end
	case "insider.article":
		if request.AmountCents != insiderArticleUnlockCents || request.ResourceID == "" ||
			(request.ContributionPublicationID != "" && request.ResourceID != request.ContributionPublicationID) {
			return getters.ServiceQuote{}, getters.ErrServiceQuote
		}
		quote.Name = "Bitcoin++ Insider article unlock"
	case "insider.open-access":
		// The remaining campaign balance is dynamic and is resolved by the
		// authenticated Insider service immediately before this request.
		if request.AmountCents < 1 || request.AmountCents > 100_000_000 || request.ResourceID == "" || request.ResourceID != request.ContributionPublicationID {
			return getters.ServiceQuote{}, getters.ErrServiceQuote
		}
		quote.Name = "Open this Bitcoin++ Insider article for everyone"
	default:
		return getters.ServiceQuote{}, getters.ErrServiceQuote
	}
	return quote, nil
}

func startInsiderProviderCheckout(app *config.AppContext, request insiderCheckoutRequest, quote getters.ServiceQuote, purchase *getters.ServicePurchase, attempt *getters.ServicePaymentAttempt) (string, string, error) {
	if attempt.Reference != "" {
		switch quote.Provider {
		case "stripe":
			checkout, err := session.Get(attempt.Reference, nil)
			if err != nil {
				return "", "", err
			}
			return checkout.URL, checkout.ID, nil
		case "opennode":
			charge, err := GetCharge(app, attempt.Reference)
			if err != nil {
				return "", "", err
			}
			return charge.HostedCheckoutURL, charge.ID, nil
		}
	}
	if !attempt.CanCreateCharge {
		return "", "", getters.ErrServicePayment
	}
	if quote.Provider == "opennode" {
		payment, err := getters.InitOpenNodeServiceCheckout(app, attempt, purchase, quote, request.SuccessURL)
		if err != nil {
			return "", "", err
		}
		return payment.HostedCheckoutURL, payment.ID, nil
	}
	params := &stripe.CheckoutSessionParams{
		CustomerEmail: stripe.String(purchase.BuyerEmail),
		LineItems: []*stripe.CheckoutSessionLineItemParams{{
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{Name: stripe.String(quote.Name)},
				UnitAmount:  stripe.Int64(quote.TotalCents), Currency: stripe.String("usd"),
			},
			Quantity: stripe.Int64(1),
		}},
		Metadata: map[string]string{
			"checkout-kind": "service", "service-attempt-id": attempt.ID, "service-order-id": purchase.OrderID,
		},
		Mode: stripe.String(string(stripe.CheckoutSessionModePayment)), SuccessURL: stripe.String(request.SuccessURL), CancelURL: stripe.String(request.CancelURL),
		AutomaticTax: &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(false)}, ExpiresAt: stripe.Int64(quote.ExpiresAt.Unix()),
	}
	params.SetIdempotencyKey(attempt.ID)
	checkout, err := session.New(params)
	if err != nil {
		return "", "", err
	}
	return checkout.URL, checkout.ID, nil
}

func allowedInsiderReturnURL(raw, configuredOrigins string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.Fragment != "" {
		return false
	}
	origin := strings.ToLower(parsed.Scheme + "://" + parsed.Host)
	for _, allowed := range strings.Split(configuredOrigins, ",") {
		if origin == strings.ToLower(strings.TrimRight(strings.TrimSpace(allowed), "/")) {
			return true
		}
	}
	return false
}

func ensureInsiderJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

var insiderFulfillmentHTTPClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func settleInsiderServicePayment(ctx context.Context, app *config.AppContext, payment getters.VerifiedServicePayment) error {
	purchase, err := getters.GetServicePaymentContext(app, payment.AttemptID)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(purchase.Quote.ProductCode, "insider.") {
		return getters.ErrServicePayment
	}
	// The shared ledger correctly rejects a provider event ID replayed with a
	// different timestamp. If this attempt was already settled, reuse its
	// canonical payment time before recording the adapter retry so the outgoing
	// Insider fulfillment event remains byte-for-byte stable as well.
	if settled, settledErr := getters.GetFulfillableServicePaymentContext(app, payment.AttemptID); settledErr == nil {
		payment.PaidAt = settled.PaidAt
	} else if !errors.Is(settledErr, getters.ErrServicePayment) {
		return settledErr
	}
	if err := getters.RecordServicePayment(app, payment); err != nil {
		return err
	}
	_, err = processServiceFulfillment(ctx, app, payment.AttemptID, time.Now())
	return err
}

func buildInsiderSettlement(app *config.AppContext, payment getters.VerifiedServicePayment) (insiderSettlementEvent, error) {
	purchase, err := getters.GetFulfillableServicePaymentContext(app, payment.AttemptID)
	if err != nil {
		return insiderSettlementEvent{}, err
	}
	var snapshot insiderFulfillmentSnapshot
	if err := json.Unmarshal(purchase.Quote.Fulfillment, &snapshot); err != nil {
		return insiderSettlementEvent{}, fmt.Errorf("decode Insider fulfillment: %w", err)
	}
	event := insiderSettlementEvent{
		EventID: payment.Provider + ":" + payment.EventID, EventKind: "settled",
		ProductCode: purchase.Quote.ProductCode, ResourceID: purchase.Quote.ResourceID,
		ContributionPublicationID: snapshot.ContributionPublicationID,
		SourceReference:           "service-purchase:" + purchase.PurchaseID,
		OrderPublicID:             purchase.PublicID,
		PersonID:                  purchase.PersonID,
		BuyerEmail:                purchase.BuyerEmail,
		Description:               purchase.Quote.Name,
		Provider:                  payment.Provider,
		ProviderReference:         payment.Reference,
		MerchantName:              strings.TrimSpace(app.Env.InsiderMerchantName),
		AmountCents:               purchase.Quote.TotalCents,
		TaxCents:                  purchase.Quote.TaxCents,
		SettledSats:               payment.SettledSats,
		Currency:                  "USD",
		PublicCredit:              snapshot.PublicCredit, OccurredAt: purchase.PaidAt.UTC(),
		QuotedAt:       purchase.Quote.ExpiresAt.Add(-insiderQuoteTTL).UTC(),
		AccessStartsAt: purchase.Quote.PeriodStart, AccessEndsAt: purchase.Quote.PeriodEnd,
	}
	// Membership time belongs to the reader from verified settlement, not from
	// the earlier moment when the short-lived checkout quote was rendered.
	if !purchase.Quote.RecurringInvoice && (purchase.Quote.ProductCode == "insider.membership.annual" || purchase.Quote.ProductCode == "insider.membership.monthly") {
		startsAt := purchase.PaidAt.UTC()
		_, endsAt, err := servicebilling.ProductTerm(purchase.Quote.ProductCode, payment.Provider, 1, startsAt)
		if err != nil {
			return insiderSettlementEvent{}, err
		}
		event.AccessStartsAt, event.AccessEndsAt = &startsAt, &endsAt
	}
	if purchase.Quote.RecurringInvoice {
		event.QuotedAt = purchase.PaidAt.UTC()
	}
	return event, nil
}

func postInsiderSettlement(ctx context.Context, app *config.AppContext, event insiderSettlementEvent) error {
	endpoint := strings.TrimSpace(app.Env.InsiderFulfillmentEndpoint)
	token := strings.TrimSpace(app.Env.InsiderFulfillmentToken)
	if endpoint == "" || token == "" {
		return errors.New("Insider fulfillment endpoint is not configured")
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", event.EventID)
	response, err := insiderFulfillmentHTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	// The refund queue may proceed only after durable settlement acceptance.
	// In particular, 202 means pending work rather than an acknowledgment.
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("Insider fulfillment returned %s", response.Status)
	}
	return nil
}

package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"btcpp-web/internal/servicestripe"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The caller has already authenticated the Insider server and matched its
// idempotency header. Recurring membership uses the shared subscription ledger.
func serveInsiderRecurringCheckout(w http.ResponseWriter, r *http.Request, app *config.AppContext, request insiderCheckoutRequest, client servicestripe.CheckoutClient) {
	if !serviceFulfillmentReady(app) || strings.TrimSpace(app.Env.InsiderStripeTaxCode) == "" {
		writeInsiderServiceError(w, 503, "Recurring Insider billing is not configured.")
		return
	}
	if _, err := lifecycleEndpoint(app); err != nil {
		writeInsiderServiceError(w, 503, "Recurring Insider lifecycle delivery is not configured.")
		return
	}
	terms, err := insiderRecurringTerms(request, app.Env.InsiderAllowedReturnOrigins, app.Env.InsiderStripeTaxCode, time.Now().UTC())
	if err != nil {
		writeInsiderServiceError(w, 422, "The recurring checkout request could not be verified.")
		return
	}
	reserved, err := getters.ReserveServiceStripeCheckout(app, request.PersonID, request.RequestID, terms)
	if err != nil {
		status := 503
		if errors.Is(err, getters.ErrServiceStripeCheckoutConflict) {
			status = 409
		}
		if errors.Is(err, getters.ErrServiceBuyer) {
			status = 422
		}
		writeInsiderServiceError(w, status, "The recurring checkout could not be reserved.")
		return
	}
	checkoutURL, err := getters.OpenServiceStripeCheckout(r.Context(), app, request.PersonID, reserved.Intent.ID, terms.ProductCode, client)
	if err != nil {
		writeInsiderServiceError(w, 502, "The saved checkout needs retry or reconciliation.")
		return
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(insiderCheckoutResponse{CheckoutURL: checkoutURL, CheckoutID: reserved.Intent.ID})
}

func insiderRecurringTerms(request insiderCheckoutRequest, origins, taxCode string, now time.Time) (servicestripe.CheckoutIntent, error) {
	bad := servicestripe.ErrCheckout
	if request.BillingMode != "recurring" || request.PaymentProvider != "stripe" || request.ResourceID != "" || request.ContributionPublicationID != "" || len(request.PublicCredit) != 0 {
		return servicestripe.CheckoutIntent{}, bad
	}
	if request.ProductCode != "insider.membership.monthly" && request.ProductCode != "insider.membership.annual" {
		return servicestripe.CheckoutIntent{}, bad
	}
	plan, _, err := servicebilling.ProductTerm(request.ProductCode, "stripe", 1, now)
	if err != nil || request.AmountCents != plan.UnitCents || request.Currency != "USD" || len(request.RequestID) < 16 || len(request.RequestID) > 128 || request.PersonID == "" || !request.QuoteExpiresAt.After(now) || request.QuoteExpiresAt.After(now.Add(insiderQuoteMaxFuture)) || taxCode == "" {
		return servicestripe.CheckoutIntent{}, bad
	}
	if !allowedInsiderReturnURL(request.SuccessURL, origins) || !allowedInsiderReturnURL(request.CancelURL, origins) {
		return servicestripe.CheckoutIntent{}, bad
	}
	success, err := url.Parse(request.SuccessURL)
	if err != nil {
		return servicestripe.CheckoutIntent{}, bad
	}
	cancel, err := url.Parse(request.CancelURL)
	if err != nil || success.Scheme != cancel.Scheme || success.Host != cancel.Host {
		return servicestripe.CheckoutIntent{}, bad
	}
	return servicestripe.CheckoutIntent{ProductCode: plan.Code, Quantity: 1, TaxCode: taxCode, ReturnOrigin: success.Scheme + "://" + success.Host, SuccessURL: request.SuccessURL, CancelURL: request.CancelURL}, nil
}

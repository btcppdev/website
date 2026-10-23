package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Sales are controlled by global admin. Passage uses the conference-ticket tax
// classification; shared billing retains the classification with each order.
func antarcticaCheckoutReady(app *config.AppContext) bool {
	return antarcticaPaymentsReady(app) && antarcticaSalesEnabled(app)
}

func antarcticaPaymentsReady(app *config.AppContext) bool {
	return app != nil && app.DB != nil && app.Env != nil && strings.TrimSpace(app.Env.OpenNode.Key) != ""
}

// Card checkout uses the existing shared Stripe configuration.
func antarcticaPaymentProviderAllowed(app *config.AppContext, provider string) bool {
	return provider == expedition.Bitcoin || (provider == expedition.Stripe && app != nil && app.Env != nil && strings.TrimSpace(app.Env.StripeKey) != "")
}

// A verified email without a person record needs profile setup, not another login.
// Keep checkout as the destination through profile validation and creation.
func antarcticaCheckoutAccountURL(identity *auth.Identity) string {
	const next = "/expeditions/antarctica/checkout"
	if identity != nil && identity.PersonID == "" && strings.TrimSpace(identity.LoginEmail) != "" {
		return dashboardProfileURLWithFlash("", "", next, "Create your profile to continue your expedition booking.")
	}
	return "/login?next=" + url.QueryEscape(next)
}

type antarcticaCheckoutRequest struct {
	CSRF          string                      `json:"csrf"`
	Key           string                      `json:"key"`
	Spots         int                         `json:"spots"`
	Selections    []expedition.CabinSelection `json:"selections"`
	Plan          string                      `json:"plan"`
	Provider      string                      `json:"provider"`
	TermsVersion  string                      `json:"terms_version"`
	Accepted      bool                        `json:"accepted"`
	ExpectedTotal int64                       `json:"expected_total"`
}

func AntarcticaCheckout(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !antarcticaCheckoutReady(app) {
		http.Error(w, "Checkout is temporarily unavailable. Please try again or contact inbox@btcpp.dev.", http.StatusServiceUnavailable)
		return
	}
	identity, err := auth.Resolve(r, app)
	if err != nil {
		http.Error(w, "Unable to verify your sign-in.", 503)
		return
	}
	if identity == nil || identity.PersonID == "" {
		if r.Method == "GET" {
			if email, sessionErr := validatedSessionEmail(r, app, true); sessionErr == nil && email != "" {
				identity = &auth.Identity{LoginEmail: email}
			}
			http.Redirect(w, r, antarcticaCheckoutAccountURL(identity), http.StatusSeeOther)
		} else {
			http.Error(w, "Sign in again before booking.", 401)
		}
		return
	}
	policy, err := getters.CurrentServiceLegalPolicy(app, expedition.ProductCode)
	if err != nil || policy == nil {
		http.Error(w, "Unable to load booking terms. Please try again or contact inbox@btcpp.dev.", 503)
		return
	}
	beta := false
	if err = app.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM person_emails e JOIN people p ON p.id=e.person_id WHERE e.person_id::text=$1 AND lower(e.email::text)='nifty@btcpp.dev' AND e.verified_at IS NOT NULL AND NOT p.is_deleted_account)`, identity.PersonID).Scan(&beta); err != nil {
		http.Error(w, "Unable to verify beta access", 503)
		return
	}

	if r.Method == "GET" {
		page := newAntarcticaPage(nil, false)
		page.CheckoutEnabled = true
		page.StripeEnabled = antarcticaPaymentProviderAllowed(app, expedition.Stripe)
		page.CSRF, err = ensureAuthMethodsCSRF(app, r)
		if err != nil {
			http.Error(w, "Unable to prepare checkout.", 503)
			return
		}
		sessionKey := "antarctica-checkout-key:" + identity.PersonID
		page.RequestKey = app.Session.GetString(r.Context(), sessionKey)
		if page.RequestKey == "" {
			page.RequestKey = uuid.NewString()
			app.Session.Put(r.Context(), sessionKey, page.RequestKey)
		}
		// Resume an existing order after a reload instead of making another hold.
		var existing string
		lookupErr := app.DB.QueryRow(r.Context(), `SELECT o.public_id FROM expedition_reservations e JOIN service_purchase_requests p ON p.request_key='expedition:'||e.id::text||':1' AND p.person_id::text=e.buyer_id JOIN shop_orders o ON o.id=p.order_id WHERE e.buyer_id=$1 AND e.request_key=$2`, identity.PersonID, page.RequestKey).Scan(&existing)
		if lookupErr == nil {
			http.Redirect(w, r, "/services/orders/"+url.PathEscape(existing), 303)
			return
		}
		if !errors.Is(lookupErr, pgx.ErrNoRows) {
			http.Error(w, "Unable to check existing booking.", 503)
			return
		}
		var expired bool
		lookupErr = app.DB.QueryRow(r.Context(), `SELECT hold_expires_at <= now() AND state IN ('held','expired') FROM expedition_reservations WHERE buyer_id=$1 AND request_key=$2`, identity.PersonID, page.RequestKey).Scan(&expired)
		if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
			http.Error(w, "Unable to check reservation.", 503)
			return
		}
		if expired {
			page.RequestKey = uuid.NewString()
			app.Session.Put(r.Context(), sessionKey, page.RequestKey)
		}
		if beta {
			page.BetaTrial = true
			if page.RemainingBerths > 1 {
				page.RemainingBerths = 1
			}
			for i := range page.Berths {
				page.Berths[i].PriceCents = 1
			}
		}
		renderAntarcticaCheckout(w, app, page)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var request antarcticaCheckoutRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&request); err != nil || ensureInsiderJSONEnd(decoder) != nil {
		http.Error(w, "Invalid checkout request.", 400)
		return
	}
	if !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), request.CSRF) {
		http.Error(w, "Reload checkout before trying again.", 403)
		return
	}
	if !request.Accepted || request.TermsVersion != policy.Version {
		http.Error(w, "Read and accept the current booking terms before continuing.", 422)
		return
	}
	if _, err = uuid.Parse(request.Key); err != nil {
		http.Error(w, "Invalid booking key.", 400)
		return
	}
	if !antarcticaPaymentProviderAllowed(app, request.Provider) {
		http.Error(w, "Expedition checkout currently accepts Bitcoin only.", http.StatusUnprocessableEntity)
		return
	}
	if request.Provider == expedition.Bitcoin && app.Env.OpenNode.Key == "" {
		http.Error(w, "Bitcoin payments are temporarily unavailable.", 503)
		return
	}
	schedule, err := expedition.QuoteParty(request.Spots, request.Selections, request.Plan, request.Provider, time.Now())
	if beta && err == nil {
		if request.Spots != 1 || len(request.Selections) != 1 || request.Plan != expedition.Full || request.Provider != expedition.Bitcoin {
			http.Error(w, "Beta checkout allows one berth, paid in full with Bitcoin.", 422)
			return
		}
		schedule.TotalCents = 1
	}
	if err != nil || schedule.TotalCents != request.ExpectedTotal {
		http.Error(w, "Your fare or cabin selection has changed. Reload checkout and review it.", 422)
		return
	}
	var reservation expedition.Reservation
	if beta {
		reservation, err = (&expedition.Store{DB: app.DB}).ReserveBetaParty(r.Context(), identity.PersonID, request.Key, request.Selections)
	} else {
		reservation, err = (&expedition.Store{DB: app.DB}).ReserveParty(r.Context(), identity.PersonID, request.Key, request.Plan, request.Provider, request.Spots, request.Selections)
	}
	if err != nil {
		http.Error(w, "These berths could not be reserved. Check availability and your selection before trying again.", 409)
		return
	}
	// A retry must return the original order, including after payment or expiry.
	existing, err := expeditionExistingOrder(r, app, identity.PersonID, reservation.ID)
	if err == nil {
		writeExpeditionOrder(w, existing)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Unable to check booking status. Retry this checkout.", 503)
		return
	}
	charge, err := (&expedition.Store{DB: app.DB}).Charge(r.Context(), identity.PersonID, reservation.ID, 1)
	if err != nil {
		http.Error(w, "This reservation has expired or needs review. Check your booking before starting again.", 409)
		return
	}
	tax := getters.ExpeditionConferenceTaxQuote(charge.ExpiresAt)
	purchase, err := getters.CreateExpeditionPurchase(app, identity.PersonID, reservation.ID, 1, tax, request.TermsVersion)
	if err != nil {
		// Concurrent retries return the first stored order. Never replace its
		// financial or legal snapshot.
		if existing, lookupErr := expeditionExistingOrder(r, app, identity.PersonID, reservation.ID); lookupErr == nil {
			writeExpeditionOrder(w, existing)
			return
		}
		app.Err.Printf("expedition checkout order: %v", err)
		http.Error(w, "Unable to prepare your order. Retry this checkout.", 503)
		return
	}
	writeExpeditionOrder(w, purchase.PublicID)
}

func expeditionExistingOrder(r *http.Request, app *config.AppContext, buyer, reservation string) (string, error) {
	var publicID string
	err := app.DB.QueryRow(r.Context(), `SELECT o.public_id FROM service_purchase_requests p JOIN shop_orders o ON o.id=p.order_id WHERE p.person_id=$1 AND p.request_key=$2`, buyer, fmt.Sprintf("expedition:%s:1", reservation)).Scan(&publicID)
	return publicID, err
}
func writeExpeditionOrder(w http.ResponseWriter, publicID string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": "/services/orders/" + url.PathEscape(publicID)})
}

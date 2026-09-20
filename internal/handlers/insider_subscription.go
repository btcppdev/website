package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"btcpp-web/internal/servicestripe"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
)

type insiderSubscriptionRequest struct {
	PersonID    string `json:"person_id"`
	CheckoutID  string `json:"checkout_id"`
	ProductCode string `json:"product_code"`
	RequestID   string `json:"request_id,omitempty"`
	Revision    int64  `json:"revision,omitempty"`
	PeriodEnd   int64  `json:"period_end,omitempty"`
}

// Insider authenticates the reader and submits that reader's account ID using
// its private service token. These endpoints are not browser bearer endpoints.
func ManageInsiderSubscription(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	serveInsiderSubscription(w, r, app, false, nil)
}
func CancelInsiderSubscription(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	serveInsiderSubscription(w, r, app, true, nil)
}

func serveInsiderSubscription(w http.ResponseWriter, r *http.Request, app *config.AppContext, cancel bool, injected *servicestripe.SubscriptionClient) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeInsiderServiceError(w, 405, "POST is required.")
		return
	}
	if app == nil || app.Env == nil || !validInsiderServiceAuthorization(r.Header.Get("Authorization"), app.Env.InsiderServiceToken) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="btcpp-insider"`)
		writeInsiderServiceError(w, 401, "A valid Insider service token is required.")
		return
	}
	var request insiderSubscriptionRequest
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || ensureInsiderJSONEnd(decoder) != nil {
		writeInsiderServiceError(w, 400, "The subscription request is invalid.")
		return
	}
	_, personErr := uuid.Parse(request.PersonID)
	discover := !cancel && request.CheckoutID == "" && request.ProductCode == ""
	if personErr != nil {
		writeInsiderServiceError(w, 422, "The subscription request could not be verified.")
		return
	}
	if !discover {
		plan, err := servicebilling.LookupProductPlan(request.ProductCode)
		_, checkoutErr := uuid.Parse(request.CheckoutID)
		if err != nil || plan.Family != "insider" || checkoutErr != nil {
			writeInsiderServiceError(w, 422, "The subscription request could not be verified.")
			return
		}
	}
	if cancel {
		_, err := uuid.Parse(request.RequestID)
		if err != nil || r.Header.Get("Idempotency-Key") != request.RequestID || request.Revision < 1 || request.PeriodEnd <= 0 {
			writeInsiderServiceError(w, 400, "A reviewed subscription and matching request key are required.")
			return
		}
	} else if request.RequestID != "" || request.Revision != 0 || request.PeriodEnd != 0 {
		writeInsiderServiceError(w, 400, "Status requests must not include cancellation instructions.")
		return
	}
	if discover {
		var err error
		request.CheckoutID, request.ProductCode, err = getters.CurrentInsiderStripeCheckout(r.Context(), app, request.PersonID)
		if err != nil {
			writeInsiderSubscriptionError(w, err)
			return
		}
	}
	var client servicestripe.SubscriptionClient
	if injected != nil {
		client = *injected
	} else {
		key := app.Env.StripeKey
		live := strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "rk_live_")
		if !live && !strings.HasPrefix(key, "sk_test_") && !strings.HasPrefix(key, "rk_test_") {
			writeInsiderServiceError(w, 503, "Subscription management is unavailable.")
			return
		}
		provider := alertsstripe.NewClient(live)
		client = servicestripe.SubscriptionClient{Subscription: provider.Subscription, UpdateSubscription: provider.UpdateSubscription, Live: live}
	}
	if cancel {
		saved, err := getters.ReserveServiceStripeCancellation(r.Context(), app, request.PersonID, request.CheckoutID, request.ProductCode, request.RequestID, request.Revision, request.PeriodEnd)
		if err != nil {
			writeInsiderSubscriptionError(w, err)
			return
		}
		state, err := getters.ProcessServiceStripeCancellation(r.Context(), app, request.PersonID, saved.Intent.ID, request.ProductCode, client)
		status := http.StatusOK
		if err != nil {
			state = saved.State
			status = http.StatusAccepted
		}
		if state == "pending" {
			status = http.StatusAccepted
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(struct {
			RequestID string `json:"request_id"`
			State     string `json:"state"`
		}{saved.Intent.ID, state})
		return
	}
	if err := getters.RefreshServiceStripeSubscription(r.Context(), app, request.PersonID, request.CheckoutID, request.ProductCode, client); err != nil {
		writeInsiderSubscriptionError(w, err)
		return
	}
	status, err := getters.ServiceStripeSubscriptionStatus(r.Context(), app, request.PersonID, request.CheckoutID, request.ProductCode)
	if err != nil {
		writeInsiderSubscriptionError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(status)
}

func writeInsiderSubscriptionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeInsiderServiceError(w, 404, "The subscription was not found.")
	case errors.Is(err, servicestripe.ErrSubscription):
		writeInsiderServiceError(w, 409, "The subscription changed. Refresh it before trying again.")
	default:
		writeInsiderServiceError(w, 503, "Subscription management is temporarily unavailable.")
	}
}

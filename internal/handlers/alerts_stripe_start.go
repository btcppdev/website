package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type alertsStripeStartPage struct {
	Policy                    *getters.ServiceLegalPolicy
	Account, CSRF, Key, Total string
	Slots                     int64
	Enabled                   bool
}

func alertsStripeSelection(values url.Values) (string, int64, error) {
	account := values.Get("account")
	if _, err := uuid.Parse(account); err != nil || len(values["account"]) != 1 {
		return "", 0, errors.New("invalid account")
	}
	slots, err := strconv.ParseInt(values.Get("slots"), 10, 64)
	if err != nil || len(values["slots"]) != 1 || slots < 1 || slots > 2147483647/servicebilling.AlertsAnnualSlotCents {
		return "", 0, errors.New("invalid repository count")
	}
	return account, slots, nil
}

// GET only reviews a proposed count. POST binds it to the signed-in account,
// reserves an immutable purchase intent, then opens that same Stripe checkout.
func AlertsStripeStart(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	r = r.WithContext(ctx)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", 405)
		return
	}
	values := r.URL.Query()
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid request", 400)
			return
		}
		values = r.PostForm
	}
	account, slots, err := alertsStripeSelection(values)
	if err != nil {
		http.Error(w, "Return to Alerts to select your repositories", 400)
		return
	}
	identity, err := auth.Resolve(r, app)
	if err != nil {
		http.Error(w, "Unable to verify your account", 503)
		return
	}
	if identity == nil || identity.PersonID == "" {
		if r.Method == http.MethodPost {
			http.Error(w, "Sign in before paying", 401)
			return
		}
		next := "/services/alerts/subscribe?" + url.Values{"account": {account}, "slots": {strconv.FormatInt(slots, 10)}}.Encode()
		http.Redirect(w, r, "/login?next="+url.QueryEscape(next), 303)
		return
	}
	if identity.PersonID != account {
		http.Error(w, "Sign in with the same Bitcoin++ account you use for Alerts before checking out.", 403)
		return
	}
	key := ""
	if app.Env != nil {
		key = app.Env.StripeKey
	}
	enabled := strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "sk_test_") || strings.HasPrefix(key, "rk_live_") || strings.HasPrefix(key, "rk_test_")
	if r.Method == http.MethodPost && (len(values["csrf"]) != 1 || !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), values.Get("csrf"))) {
		http.Error(w, "Reload checkout and try again", 403)
		return
	}
	if enabled {
		var checkout string
		var currentSlots int64
		err = app.DB.QueryRow(r.Context(), `SELECT c.id::text,(s.binding->>'PaidSlots')::bigint FROM alerts_stripe_checkouts c JOIN alerts_stripe_current_subscriptions s ON s.checkout_id=c.id WHERE c.person_id=$1 AND c.closed_at IS NULL ORDER BY c.created_at DESC LIMIT 1`, account).Scan(&checkout, &currentSlots)
		if err == nil {
			if slots <= currentSlots {
				http.Redirect(w, r, "/services/alerts/manage", 303)
				return
			}
			client := alertsstripe.NewClient(strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "rk_live_"))
			quote, e := getters.PrepareAlertsStripeIncrease(r.Context(), app, account, checkout, slots, client)
			if errors.Is(e, alertsstripe.ErrSubscription) || errors.Is(e, alertsstripe.ErrInvoice) {
				http.Error(w, "Your current plan cannot be upgraded yet. Check billing before trying again.", 409)
				return
			}
			if e != nil {
				http.Error(w, "Unable to prepare your upgrade. Please try again shortly.", 503)
				return
			}
			http.Redirect(w, r, "/services/alerts/upgrade/"+quote.ID, 303)
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Unable to load billing", 503)
			return
		}
	}
	policy, err := getters.CurrentServiceLegalPolicy(app, servicebilling.AlertsProduct)
	if err != nil {
		http.Error(w, "Unable to load checkout policy", 503)
		return
	}
	if r.Method == http.MethodPost {
		if len(values["csrf"]) != 1 || !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), values.Get("csrf")) {
			http.Error(w, "Reload checkout and try again", 403)
			return
		}
		if !enabled {
			http.Error(w, "Card checkout is not configured here yet", 503)
			return
		}
		if len(values["key"]) != 1 {
			http.Error(w, "Invalid checkout request", 400)
			return
		}
		if _, err = uuid.Parse(values.Get("key")); err != nil {
			http.Error(w, "Invalid checkout request", 400)
			return
		}
		acceptedVersion := ""
		if policy != nil {
			if len(values["accept_terms"]) != 1 || values.Get("accept_terms") != "yes" || len(values["legal_version"]) != 1 || values.Get("legal_version") != policy.Version {
				http.Error(w, "Review and accept the current Alerts Terms before paying", 409)
				return
			}
			acceptedVersion = policy.Version
		}
		stored, err := getters.ReserveAlertsStripeCheckoutWithPolicy(app, account, values.Get("key"), slots, app.Env.GetURI(), acceptedVersion)
		if errors.Is(err, getters.ErrServiceLegalReview) {
			http.Error(w, "Checkout terms have changed. Reload and review before paying.", 409)
			return
		}
		if errors.Is(err, getters.ErrAlertsStripeCheckoutConflict) {
			http.Error(w, "An existing Alerts purchase needs attention. Return to My alerts before starting another subscription.", 409)
			return
		}
		if errors.Is(err, getters.ErrServiceBuyer) {
			http.Error(w, "Verify an email address on your Bitcoin++ account before paying.", 409)
			return
		}
		if err != nil {
			http.Error(w, "Unable to prepare your checkout", 503)
			return
		}
		live := strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "rk_live_")
		destination, err := getters.OpenAlertsStripeCheckout(r.Context(), app, account, stored.Intent.ID, alertsstripe.NewClient(live))
		if errors.Is(err, getters.ErrAlertsStripeCheckoutConflict) {
			http.Error(w, "Your Alerts access has changed. Return to My alerts to review your plan before paying.", http.StatusConflict)
			return
		}
		if err != nil {
			// The durable return page is safe even after an ambiguous provider
			// response. Never create another intent to recover a timeout.
			http.Redirect(w, r, "/services/alerts/checkout/"+stored.Intent.ID, 303)
			return
		}
		http.Redirect(w, r, destination, 303)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare checkout", 500)
		return
	}
	total := slots * servicebilling.AlertsAnnualSlotCents
	page := alertsStripeStartPage{Policy: policy, Account: account, Slots: slots, CSRF: csrf, Key: uuid.NewString(), Total: fmt.Sprintf("$%d.%02d", total/100, total%100), Enabled: enabled}
	var html bytes.Buffer
	if err = app.TemplateCache.ExecuteTemplate(&html, "alerts_stripe_start.tmpl", page); err != nil {
		http.Error(w, "Unable to show checkout", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(html.Bytes())
}

package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type alertsStripeUpgradePage struct {
	Policy                                         *getters.ServiceLegalPolicy
	ID, CSRF, Charge, Tax, Annual, Period, Message string
	Previous, Desired                              int64
	CanConfirm, CanPay                             bool
}

func AlertsStripeUpgrade(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	r = r.WithContext(ctx)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != "GET" && r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", 405)
		return
	}
	identity, err := auth.Resolve(r, app)
	if err != nil {
		http.Error(w, "Unable to verify your account", 503)
		return
	}
	if identity == nil || identity.PersonID == "" {
		if r.Method == "POST" {
			http.Error(w, "Sign in before changing billing", 401)
		} else {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), 303)
		}
		return
	}
	id := mux.Vars(r)["quote"]
	quote, err := getters.AlertsStripeIncreaseQuoteForPerson(r.Context(), app, identity.PersonID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Unable to load upgrade", 503)
		return
	}
	key := ""
	if app.Env != nil {
		key = app.Env.StripeKey
	}
	if !strings.HasPrefix(key, "sk_test_") && !strings.HasPrefix(key, "sk_live_") && !strings.HasPrefix(key, "rk_test_") && !strings.HasPrefix(key, "rk_live_") {
		http.Error(w, "Card billing is temporarily unavailable", 503)
		return
	}
	client := alertsstripe.NewClient(strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "rk_live_"))
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if r.ParseForm() != nil || len(r.PostForm["csrf"]) != 1 || len(r.PostForm["action"]) != 1 {
			http.Error(w, "Invalid request", 400)
			return
		}
		if !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), r.PostForm.Get("csrf")) {
			http.Error(w, "Reload the upgrade and try again", 403)
			return
		}
		switch r.PostForm.Get("action") {
		case "confirm":
			acceptedVersion := ""
			if len(r.PostForm["accept_terms"]) == 1 && r.PostForm.Get("accept_terms") == "yes" && len(r.PostForm["legal_version"]) == 1 {
				acceptedVersion = r.PostForm.Get("legal_version")
			}
			state, e := getters.ReserveAlertsStripeIncreaseWithPolicy(r.Context(), app, identity.PersonID, id, acceptedVersion)
			if errors.Is(e, getters.ErrServiceLegalReview) {
				http.Error(w, "Reload the upgrade and accept the current terms before confirming.", 409)
				return
			}
			if errors.Is(e, alertsstripe.ErrSubscription) {
				http.Error(w, "This quote expired or billing changed. Request a new upgrade quote.", 409)
				return
			}
			if e != nil {
				http.Error(w, "Unable to save your confirmation", 503)
				return
			}
			if state == "pending" {
				// Consent survives transport failures and is resumed by the worker.
				_ = getters.ProcessAlertsStripeIncrease(r.Context(), app, identity.PersonID, id, client)
			}
			http.Redirect(w, r, "/services/alerts/upgrade/"+url.PathEscape(id), 303)
			return
		case "pay":
			link, e := getters.AlertsStripeIncreasePaymentLink(r.Context(), app, identity.PersonID, id, client)
			if e != nil {
				http.Error(w, "Unable to open this invoice. Reload the upgrade to check its status.", 409)
				return
			}
			http.Redirect(w, r, link, 303)
			return
		default:
			http.Error(w, "Invalid action", 400)
			return
		}
	}
	var state string
	err = app.DB.QueryRow(r.Context(), `SELECT state FROM alerts_stripe_increases WHERE quote_id=$1`, id).Scan(&state)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Unable to load upgrade status", 503)
		return
	}
	if state == "pending" {
		_, _ = getters.RecoverAlertsStripeIncrease(r.Context(), app, identity.PersonID, id, client)
		if err = app.DB.QueryRow(r.Context(), `SELECT state FROM alerts_stripe_increases WHERE quote_id=$1`, id).Scan(&state); err != nil {
			http.Error(w, "Unable to load upgrade status", 503)
			return
		}
	}
	q := quote.Preview
	money := func(cents int64) string { return fmt.Sprintf("$%d.%02d", cents/100, cents%100) }
	page := alertsStripeUpgradePage{ID: id, Previous: q.PreviousSlots, Desired: q.PaidSlots, Charge: money(q.TotalCents), Tax: money(q.TaxCents), Annual: money(q.PaidSlots * servicebilling.AlertsAnnualSlotCents), Period: time.Unix(q.PeriodEnd, 0).UTC().Format("January 2, 2006"), Message: "Review the prorated charge before confirming your upgrade."}
	switch state {
	case "":
		page.CanConfirm = time.Now().Before(q.ExpiresAt)
		if !page.CanConfirm {
			page.Message = "This quote has expired. Request a new quote before upgrading."
		}
	case "pending":
		page.Message = "Your upgrade request is saved. Extra projects activate after payment is verified."
		if _, e := getters.AlertsStripeIncreasePaymentLink(r.Context(), app, identity.PersonID, id, client); e == nil {
			page.CanPay = true
		}
	case "applied":
		page.Message = "Payment verified. Your additional project allowance is active."
	case "expired":
		page.Message = "This upgrade has expired. It did not add any paid project allowance."
	default:
		page.Message = "This upgrade needs review. Contact support before submitting another request."
	}
	if page.CanConfirm {
		page.Policy, err = getters.CurrentServiceLegalPolicy(app, servicebilling.AlertsProduct)
		if err != nil {
			http.Error(w, "Unable to load service terms", 503)
			return
		}
	}
	page.CSRF, err = ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare upgrade", 500)
		return
	}
	var html bytes.Buffer
	if err = app.TemplateCache.ExecuteTemplate(&html, "alerts_stripe_upgrade.tmpl", page); err != nil {
		http.Error(w, "Unable to display upgrade", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(html.Bytes())
}

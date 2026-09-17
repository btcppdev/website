package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
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

type alertsStripeDecreasePage struct {
	CSRF, Monthly, Period, Message string
	Previous, Desired              int64
	CanConfirm                     bool
}

func AlertsStripeDecrease(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
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
	quote, err := getters.AlertsStripeDecreaseQuoteForPerson(ctx, app, identity.PersonID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Unable to load subscription change", 503)
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
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if r.ParseForm() != nil || len(r.PostForm["csrf"]) != 1 || len(r.PostForm["action"]) != 1 {
			http.Error(w, "Invalid request", 400)
			return
		}
		if !secureTokenEqual(app.Session.GetString(ctx, authMethodsCSRFKey), r.PostForm.Get("csrf")) {
			http.Error(w, "Reload the review and try again", 403)
			return
		}
		if r.PostForm.Get("action") != "confirm" {
			http.Error(w, "Invalid action", 400)
			return
		}
		state, e := getters.ReserveAlertsStripeDecrease(ctx, app, identity.PersonID, id)
		if errors.Is(e, alertsstripe.ErrSubscription) {
			http.Error(w, "This review expired or billing changed. Request a new review.", 409)
			return
		}
		if e != nil {
			http.Error(w, "Unable to save your confirmation", 503)
			return
		}
		if state == "pending" {
			client := alertsstripe.NewClient(strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "rk_live_"))
			_ = getters.ProcessAlertsStripeDecrease(ctx, app, identity.PersonID, id, client)
		}
		http.Redirect(w, r, "/services/alerts/decrease/"+url.PathEscape(id), 303)
		return
	}
	// GET displays persisted status only. Provider changes require confirmation
	// or the worker resuming existing consent, never a visit to this page.
	var state string
	err = app.DB.QueryRow(ctx, `SELECT state FROM alerts_stripe_decreases WHERE quote_id=$1`, id).Scan(&state)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Unable to load subscription status", 503)
		return
	}
	q := quote.Preview
	page := alertsStripeDecreasePage{Previous: q.PreviousSlots, Desired: q.PaidSlots, Monthly: fmt.Sprintf("$%d.%02d", q.MonthlyCents/100, q.MonthlyCents%100), Period: time.Unix(q.EffectiveAt, 0).UTC().Format("January 2, 2006 at 15:04 UTC"), Message: "Your current allowance stays available until renewal. There is no charge or refund today."}
	switch state {
	case "":
		page.CanConfirm = time.Now().Before(q.ExpiresAt)
		if !page.CanConfirm {
			page.Message = "This review has expired. Return to billing to request a new review."
		}
	case "pending":
		page.Message = "Your request is saved. We are waiting for Stripe to confirm the change. You do not need to submit it again."
	case "scheduled":
		page.Message = "Your decrease is scheduled. Your current allowance stays available until the effective date below."
	case "applied":
		page.Message = "Your lower subscription quantity has taken effect. Paid access depends on your renewal payment."
	case "expired":
		page.Message = "This request expired before scheduling. Return to billing to request a new review."
	case "cancelled":
		page.Message = "This decrease was cancelled."
	default:
		page.Message = "This change needs review. Contact support before submitting another request."
	}
	page.CSRF, err = ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare confirmation", 500)
		return
	}
	var html bytes.Buffer
	if err = app.TemplateCache.ExecuteTemplate(&html, "alerts_stripe_decrease.tmpl", page); err != nil {
		http.Error(w, "Unable to display review", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(html.Bytes())
}

package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type alertsStripeManagePage struct {
	Checkout, CSRF, Key, Heading, Message, Period string
	Revision, PeriodEnd                           int64
	CanCancel                                     bool
	CanDecrease                                   bool
	PaidSlots, MaxDecrease                        int64
	DecreasePath                                  string
}

// GET reads verified provider state. Only an owned, CSRF-protected POST can
// persist consent and schedule cancellation. Redirect parameters prove nothing.
func AlertsStripeManage(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
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
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), 303)
		}
		return
	}
	if expected := r.URL.Query().Get("account"); expected != "" && expected != identity.PersonID {
		http.Error(w, "Sign in with the Bitcoin++ account you use for Alerts", 403)
		return
	}
	page := alertsStripeManagePage{Heading: "Manage Alerts billing", Message: "No card subscription is linked to this account. Bitcoin prepayments do not renew automatically."}
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid request", 400)
			return
		}
		fields := []string{"csrf", "checkout"}
		if len(r.PostForm["action"]) > 1 || (r.PostForm.Get("action") != "" && r.PostForm.Get("action") != "decrease" && r.PostForm.Get("action") != "cancel") {
			http.Error(w, "Invalid action", 400)
			return
		}
		if r.PostForm.Get("action") == "decrease" {
			fields = append(fields, "slots")
		} else {
			fields = append(fields, "key", "revision", "periodEnd")
		}
		for _, field := range fields {
			if len(r.PostForm[field]) != 1 {
				http.Error(w, "Reload billing and try again", 400)
				return
			}
		}
		if !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), r.PostForm.Get("csrf")) {
			http.Error(w, "Reload billing and try again", 403)
			return
		}
		page.Checkout = r.PostForm.Get("checkout")
		if _, err = uuid.Parse(page.Checkout); err != nil {
			http.NotFound(w, r)
			return
		}
	} else {
		err = app.DB.QueryRow(r.Context(), `SELECT c.id::text FROM alerts_stripe_checkouts c JOIN alerts_stripe_subscriptions s ON s.checkout_id=c.id WHERE c.person_id=$1 ORDER BY c.created_at DESC,c.id DESC LIMIT 1`, identity.PersonID).Scan(&page.Checkout)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Unable to load billing", 503)
			return
		}
	}
	if page.Checkout != "" {
		if _, err = getters.AlertsStripeCheckoutForPerson(app, identity.PersonID, page.Checkout); errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			http.Error(w, "Unable to load billing", 503)
			return
		}
		key := ""
		if app.Env != nil {
			key = app.Env.StripeKey
		}
		enabled := strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "sk_test_") || strings.HasPrefix(key, "rk_live_") || strings.HasPrefix(key, "rk_test_")
		if !enabled {
			http.Error(w, "Card billing is temporarily unavailable", 503)
			return
		}
		client := alertsstripe.NewClient(strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "rk_live_"))
		if r.Method == "POST" {
			if r.PostForm.Get("action") == "decrease" {
				slots, e := strconv.ParseInt(r.PostForm.Get("slots"), 10, 64)
				if e != nil || slots < 1 {
					http.Error(w, "Choose at least one paid project, or cancel future renewals to return to the free plan.", 400)
					return
				}
				quote, e := getters.PrepareAlertsStripeDecrease(r.Context(), app, identity.PersonID, page.Checkout, slots, client)
				if errors.Is(e, alertsstripe.ErrSubscription) {
					http.Error(w, "This quantity is unavailable or billing changed. Reload billing and try again.", 409)
					return
				}
				if e != nil {
					http.Error(w, "Unable to prepare your subscription change. Please try again shortly.", 503)
					return
				}
				http.Redirect(w, r, "/services/alerts/decrease/"+url.PathEscape(quote.ID), 303)
				return
			}
			rev, e1 := strconv.ParseInt(r.PostForm.Get("revision"), 10, 64)
			end, e2 := strconv.ParseInt(r.PostForm.Get("periodEnd"), 10, 64)
			if e1 != nil || e2 != nil || rev < 1 || end <= 0 {
				http.Error(w, "Invalid billing review", 400)
				return
			}
			request, e := getters.ReserveAlertsStripeCancellation(r.Context(), app, identity.PersonID, page.Checkout, r.PostForm.Get("key"), rev, end)
			if errors.Is(e, alertsstripe.ErrSubscription) {
				http.Error(w, "Your billing period changed. Reload billing before confirming.", 409)
				return
			}
			if e != nil {
				http.Error(w, "Unable to save cancellation. Please try again.", 503)
				return
			}
			// A saved request survives provider errors; the page shows its durable status.
			_, _ = getters.ProcessAlertsStripeCancellation(r.Context(), app, identity.PersonID, request.Intent.ID, client)
			http.Redirect(w, r, "/services/alerts/manage", 303)
			return
		}
		if err = getters.RefreshAlertsStripeSubscription(r.Context(), app, identity.PersonID, page.Checkout, client); err != nil {
			http.Error(w, "Unable to verify billing with Stripe. Please try again shortly.", 503)
			return
		}
		var raw []byte
		if err = app.DB.QueryRow(r.Context(), `SELECT revision,snapshot FROM alerts_stripe_subscription_state WHERE checkout_id=$1`, page.Checkout).Scan(&page.Revision, &raw); err != nil {
			http.Error(w, "Unable to read billing", 503)
			return
		}
		var state alertsstripe.SubscriptionState
		if json.Unmarshal(raw, &state) != nil {
			http.Error(w, "Unable to read billing", 503)
			return
		}
		page.PeriodEnd = state.PeriodEnd
		page.Period = time.Unix(state.PeriodEnd, 0).UTC().Format("January 2, 2006 at 15:04 UTC")
		page.Message = "Your card plan renews monthly. Canceling stops future renewals; it does not refund or remove access you have already paid for."
		page.CanCancel = !state.CancelAtPeriodEnd && state.CancelAt == 0 && state.PeriodEnd > time.Now().Unix() && (state.ProviderStatus == "active" || state.ProviderStatus == "trialing" || state.ProviderStatus == "past_due" || state.ProviderStatus == "unpaid" || state.ProviderStatus == "paused")
		if state.CancelAtPeriodEnd || state.CancelAt > 0 {
			page.Message = "Your subscription is scheduled to stop renewing. Access you have already paid for remains available."
		} else if state.ProviderStatus == "canceled" || state.ProviderStatus == "incomplete_expired" {
			page.Message = "This card subscription has ended."
		}
		var outcome string
		err = app.DB.QueryRow(r.Context(), `SELECT state FROM alerts_stripe_cancellations WHERE checkout_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, page.Checkout).Scan(&outcome)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Unable to read cancellation status", 503)
			return
		}
		if outcome == "pending" {
			page.CanCancel = false
			page.Message = "Your cancellation request is saved. We are checking Stripe for confirmation. You do not need to submit it again."
		}
		if outcome == "review" {
			page.CanCancel = false
			page.Message = "Your cancellation request needs review because the billing period changed or confirmation took too long. Contact support before submitting another request."
		}
		var bindingRaw []byte
		if err = app.DB.QueryRow(r.Context(), `SELECT binding FROM alerts_stripe_current_subscriptions WHERE checkout_id=$1`, page.Checkout).Scan(&bindingRaw); err != nil {
			http.Error(w, "Unable to read subscription quantity", 503)
			return
		}
		var binding alertsstripe.Binding
		if json.Unmarshal(bindingRaw, &binding) != nil {
			http.Error(w, "Unable to read subscription quantity", 503)
			return
		}
		page.PaidSlots = binding.PaidSlots
		page.MaxDecrease = binding.PaidSlots - 1
		page.CanDecrease = page.CanCancel && state.State == "active" && binding.PaidSlots > 1 && outcome != "pending" && outcome != "review"
		var decreaseID, decreaseState string
		err = app.DB.QueryRow(r.Context(), `SELECT quote_id::text,state FROM alerts_stripe_decreases WHERE checkout_id=$1 AND state IN ('pending','scheduled','review')`, page.Checkout).Scan(&decreaseID, &decreaseState)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Unable to read subscription change", 503)
			return
		}
		if decreaseID != "" {
			page.CanDecrease = false
			page.CanCancel = false
			page.DecreasePath = "/services/alerts/decrease/" + decreaseID
			switch decreaseState {
			case "scheduled":
				page.Message = "A lower project quantity is scheduled for renewal. View the saved change for its price and effective date."
			case "pending":
				page.Message = "Your subscription change is saved and awaiting Stripe confirmation."
			default:
				page.Message = "Your subscription change needs review. Contact support before submitting another change."
			}
		}
		if page.CanCancel || page.CanDecrease {
			page.Key = uuid.NewString()
			page.CSRF, err = ensureAuthMethodsCSRF(app, r)
			if err != nil {
				http.Error(w, "Unable to prepare billing", 500)
				return
			}
		}
	}
	var html bytes.Buffer
	if err = app.TemplateCache.ExecuteTemplate(&html, "alerts_stripe_manage.tmpl", page); err != nil {
		http.Error(w, "Unable to display billing", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(html.Bytes())
}

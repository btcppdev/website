package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
)

type alertsStripeReturnPage struct {
	ID, Heading, Message, Total, CSRF string
	Slots                             int64
	CanPay                            bool
	CanCheck                          bool
}

func alertsStripeReturnPresentation(stored *getters.AlertsStripeCheckout, bound, enabled bool, now time.Time) alertsStripeReturnPage {
	total := stored.Intent.PaidSlots * servicebilling.AlertsAnnualSlotCents
	page := alertsStripeReturnPage{ID: stored.Intent.ID, Slots: stored.Intent.PaidSlots, Total: fmt.Sprintf("$%d.%02d", total/100, total%100), Heading: "Awaiting payment confirmation", Message: "Payment has not yet been verified. If you just paid, refresh this page shortly to check again."}
	page.CanCheck = enabled && stored.Reference != "" && !stored.Closed
	if stored.Closed {
		page.Heading, page.Message = "Checkout closed", "This checkout is closed. Return to My alerts to review your subscriptions."
	} else if bound {
		page.Message = "Your checkout is recorded. We are waiting for the verified invoice before activating paid access."
	} else if !stored.Intent.CreatedAt.Add(2 * time.Hour).After(now) {
		page.Heading, page.Message = "Checkout needs review", "This checkout can no longer be resumed here. If you paid, confirmation may still arrive. Contact support with the checkout reference below before starting another purchase."
		if page.CanCheck {
			page.Message = "This checkout can no longer be resumed here. Use Check with Stripe to verify whether payment completed or the checkout expired."
		}
	} else {
		page.CanPay = enabled && (stored.Reference != "" || !now.After(stored.Intent.CreatedAt.Add(90*time.Minute)))
		page.Heading, page.Message = "Continue checkout", "Continue with card to open your saved Stripe checkout. If you already paid, use Check with Stripe to verify its status."
		if stored.Reference == "" {
			page.Heading, page.Message = "Checkout could not open", "We could not open Stripe checkout. Your selection is saved. Try Continue with card again; it resumes the same checkout."
			if !page.CanPay {
				page.Heading, page.Message = "Checkout needs review", "We could not confirm whether Stripe created your checkout. Contact support with the checkout reference below before starting another purchase."
			}
		}

	}
	return page
}

// The redirect query string is intentionally ignored: only persisted, verified
// invoice evidence may lead to the account-owned receipt and paid-access status.
func AlertsStripeReturn(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
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
		if r.Method == http.MethodPost {
			http.Error(w, "Sign in before paying", 401)
			return
		}
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), 303)
		return
	}
	stored, err := getters.AlertsStripeCheckoutForPerson(app, identity.PersonID, mux.Vars(r)["checkout"])
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Unable to load checkout", 503)
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err = r.ParseForm(); err != nil {
			http.Error(w, "Invalid request", 400)
			return
		}
		if len(r.PostForm["csrf"]) != 1 || !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), r.PostForm.Get("csrf")) {
			http.Error(w, "Reload checkout and try again", 403)
			return
		}
	}
	var receipt string
	err = app.DB.QueryRow(r.Context(), `SELECT o.public_id FROM alerts_stripe_invoices i JOIN shop_orders o ON o.id=i.order_id JOIN alerts_stripe_checkouts c ON c.id=i.checkout_id WHERE c.id=$1 AND c.person_id=$2 ORDER BY i.ends_at DESC,i.invoice_id LIMIT 1`, stored.Intent.ID, identity.PersonID).Scan(&receipt)
	if err == nil {
		http.Redirect(w, r, "/services/orders/"+url.PathEscape(receipt), 303)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Unable to check payment status", 503)
		return
	}
	var bound bool
	if err = app.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM alerts_stripe_subscriptions WHERE checkout_id=$1)`, stored.Intent.ID).Scan(&bound); err != nil {
		http.Error(w, "Unable to check payment status", 503)
		return
	}
	key := ""
	if app.Env != nil {
		key = app.Env.StripeKey
	}
	enabled := strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "sk_test_") || strings.HasPrefix(key, "rk_live_") || strings.HasPrefix(key, "rk_test_")
	page := alertsStripeReturnPresentation(stored, bound, enabled, time.Now())
	if r.Method == http.MethodPost {
		if len(r.PostForm["action"]) > 1 {
			http.Error(w, "Invalid action", 400)
			return
		}
		action := r.PostForm.Get("action")
		if action == "check" {
			if !page.CanCheck {
				http.Error(w, "This checkout cannot be checked yet", 409)
				return
			}
			live := strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "rk_live_")
			err := getters.RecoverAlertsStripeCheckout(r.Context(), app, identity.PersonID, stored.Intent.ID, alertsstripe.NewClient(live))
			if err != nil && !errors.Is(err, alertsstripe.ErrReconcile) {
				http.Error(w, "Unable to verify payment status. Your checkout is saved; please try again shortly.", 503)
				return
			}
			http.Redirect(w, r, r.URL.Path, 303)
			return
		}
		if action != "" && action != "pay" {
			http.Error(w, "Invalid action", 400)
			return
		}
		if !page.CanPay {
			http.Error(w, "This checkout cannot be resumed. Reload to check its status.", 409)
			return
		}
		live := strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "rk_live_")
		destination, err := getters.OpenAlertsStripeCheckout(r.Context(), app, identity.PersonID, stored.Intent.ID, alertsstripe.NewClient(live))
		if errors.Is(err, getters.ErrAlertsStripeCheckoutConflict) {
			http.Error(w, "Your Alerts access has changed. Return to My alerts to review your plan before paying.", http.StatusConflict)
			return
		}
		if errors.Is(err, alertsstripe.ErrReconcile) {
			http.Redirect(w, r, r.URL.Path, 303)
			return
		}
		if err != nil {
			http.Error(w, "Unable to resume payment. Reload this checkout to check its status before trying again.", 503)
			return
		}
		http.Redirect(w, r, destination, 303)
		return
	}
	if page.CanPay || page.CanCheck {
		page.CSRF, err = ensureAuthMethodsCSRF(app, r)
		if err != nil {
			http.Error(w, "Unable to prepare checkout", 500)
			return
		}
	}
	var html bytes.Buffer
	if err = app.TemplateCache.ExecuteTemplate(&html, "alerts_stripe_return.tmpl", page); err != nil {
		http.Error(w, "Unable to show checkout", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(html.Bytes())
}

package handlers

import (
	"bytes"
	"context"
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
	"github.com/google/uuid"
)

type alertsPaymentPage struct {
	Heading, Message, CSRF, ID, Checkout, Action, Button, PayURL, OrderURL string
	CanWithdraw, CanRestartBitcoin                                         bool
}

func renderAlertsPayment(w http.ResponseWriter, app *config.AppContext, page alertsPaymentPage) {
	var html bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&html, "alerts_payment_method.tmpl", page); err != nil {
		http.Error(w, "Unable to display payment settings", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(html.Bytes())
}
func AlertsPaymentMethod(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	r = r.WithContext(ctx)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	identity, err := auth.Resolve(r, app)
	if err != nil {
		http.Error(w, "Unable to verify your account", 503)
		return
	}
	if identity == nil || identity.PersonID == "" {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), 303)
		return
	}
	if app.Env == nil || app.Env.StripeKey == "" {
		http.Error(w, "Billing is temporarily unavailable", 503)
		return
	}
	client := alertsstripe.NewClient(strings.HasPrefix(app.Env.StripeKey, "sk_live_") || strings.HasPrefix(app.Env.StripeKey, "rk_live_"))
	csrf, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare settings", 500)
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if r.ParseForm() != nil || len(r.PostForm["csrf"]) != 1 || !secureTokenEqual(csrf, r.PostForm.Get("csrf")) {
			http.Error(w, "Reload settings and try again", 403)
			return
		}
		id := r.PostForm.Get("change")
		if _, err = uuid.Parse(id); err != nil || len(r.PostForm["change"]) != 1 {
			http.Error(w, "Invalid change", 400)
			return
		}
		action := r.PostForm.Get("action")
		if len(r.PostForm["action"]) != 1 {
			http.Error(w, "Invalid action", 400)
			return
		}
		switch action {
		case "card", "apply_card":
			checkout := ""
			if action == "card" {
				checkout = r.PostForm.Get("checkout")
				if _, err = uuid.Parse(checkout); err != nil {
					http.Error(w, "Invalid subscription", 400)
					return
				}
			}
			next, e := getters.AlertsCardUpdate(r.Context(), app, identity.PersonID, checkout, id, action == "apply_card", client)
			if e != nil {
				http.Error(w, "Unable to update your card. No subscription quantity was changed. Return to billing and try again.", 503)
				return
			}
			if next == "" {
				next = "/services/alerts/payment-method?change=" + url.QueryEscape(id)
			}
			http.Redirect(w, r, next, 303)
			return
		case "bitcoin":
			revision, e1 := strconv.ParseInt(r.PostForm.Get("revision"), 10, 64)
			end, e2 := strconv.ParseInt(r.PostForm.Get("periodEnd"), 10, 64)
			if e1 != nil || e2 != nil {
				http.Error(w, "Reload billing before confirming", 400)
				return
			}
			next, e := getters.StartAlertsBitcoinSwitch(r.Context(), app, identity.PersonID, r.PostForm.Get("checkout"), id, revision, end, client)
			if e != nil {
				http.Error(w, "Billing changed or another change is pending. Reload billing before switching.", 409)
				return
			}
			http.Redirect(w, r, "/services/alerts/payment-method?switch="+url.QueryEscape(next), 303)
			return
		case "refresh_bitcoin":
			if e := getters.RefreshAlertsBitcoinSwitchOrder(r.Context(), app, identity.PersonID, id); e != nil {
				http.Error(w, "This payment needs review before starting another checkout.", 409)
				return
			}
			http.Redirect(w, r, "/services/alerts/payment-method?switch="+url.QueryEscape(id), 303)
			return
		case "withdraw":

			if e := getters.WithdrawAlertsBitcoinSwitch(r.Context(), app, identity.PersonID, id, client); e != nil {
				http.Error(w, "Unable to resume card billing. A Bitcoin order may already need attention; return to payment settings before trying again.", 409)
				return
			}
			http.Redirect(w, r, "/services/alerts/manage", 303)
			return
		default:
			http.Error(w, "Invalid action", 400)
			return
		}
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", 405)
		return
	}
	if id := r.URL.Query().Get("switch"); id != "" {
		sw, e := getters.AlertsBitcoinSwitchForPerson(r.Context(), app, identity.PersonID, id)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		if sw.CancellationState == "pending" {
			_, _ = getters.ProcessAlertsStripeCancellation(r.Context(), app, identity.PersonID, id, client)
			sw, e = getters.AlertsBitcoinSwitchForPerson(r.Context(), app, identity.PersonID, id)
			if e != nil {
				http.Error(w, "Unable to read switch", 503)
				return
			}
		}
		page := alertsPaymentPage{Heading: "Switch to annual Bitcoin payments", CSRF: csrf, ID: id, Message: "We are confirming that Stripe will stop card renewals. Bitcoin checkout is available only after confirmation."}
		if sw.State == "withdrawn" {
			page.Message = "Your switch was withdrawn. Your card plan continues."
		} else if sw.State == "resuming" {
			page.Message = "Your request to keep card billing is saved. Retry below to finish confirming it."
			page.CanWithdraw = true
		} else if sw.CancellationState == "confirmed" {
			end := time.Unix(sw.PeriodEnd, 0).UTC()
			page.Message = fmt.Sprintf("Your card will not renew. Your current paid access ends on %s. A 12-month Bitcoin prepayment for %d paid projects costs $%d.00, including applicable tax. If unpaid when access ends, your three earliest-followed projects remain active; no failed-renewal grace period applies.", end.Format("January 2, 2006"), sw.Quantity, sw.Quantity*24)
			page.CanWithdraw = sw.PurchaseID == "" && end.After(time.Now())
			page.PayURL = "/services/alerts/prepay?" + url.Values{"account": {identity.PersonID}, "slots": {strconv.FormatInt(sw.Quantity, 10)}, "months": {"12"}, "switch": {id}}.Encode()
			if sw.PurchaseID != "" {
				var canAbandon bool
				e = app.DB.QueryRow(r.Context(), `SELECT o.status IN ('pending','cancelled') AND NOT EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=r.id) FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id WHERE r.id=$1 AND r.person_id=$2`, sw.PurchaseID, identity.PersonID).Scan(&canAbandon)
				if e != nil {
					http.Error(w, "Unable to read payment status", 503)
					return
				}
				page.CanWithdraw = canAbandon && end.After(time.Now())
				page.CanRestartBitcoin = canAbandon
				var public string
				e = app.DB.QueryRow(r.Context(), `SELECT o.public_id FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id WHERE r.id=$1 AND r.person_id=$2`, sw.PurchaseID, identity.PersonID).Scan(&public)
				if e != nil {
					http.Error(w, "Unable to read Bitcoin order", 503)
					return
				}
				page.PayURL = ""
				page.OrderURL = "/services/orders/" + url.PathEscape(public)
			}
		} else if sw.CancellationState == "review" {
			page.Message = "Your cancellation needs review. Contact support before paying with Bitcoin."
		}
		renderAlertsPayment(w, app, page)
		return
	}
	id := r.URL.Query().Get("change")
	if _, err = uuid.Parse(id); err != nil {
		http.NotFound(w, r)
		return
	}
	var status string
	err = app.DB.QueryRow(r.Context(), `SELECT u.state FROM alerts_card_updates u JOIN alerts_stripe_checkouts c ON c.id=u.checkout_id WHERE u.id=$1 AND c.person_id=$2`, id, identity.PersonID).Scan(&status)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	page := alertsPaymentPage{Heading: "Save your new card", Message: "Use the card you just saved with Stripe for future Alerts renewals and upgrades. This does not change your allowance, renewal date, or cancellation settings.", CSRF: csrf, ID: id, Action: "apply_card", Button: "Use this card for Alerts"}
	if status == "applied" {
		page.Heading = "Card updated"
		page.Message = "Your new card is saved for Alerts renewals and upgrades."
		page.Button = ""
	}
	renderAlertsPayment(w, app, page)
}

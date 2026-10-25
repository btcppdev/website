package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
)

type serviceOrderPage struct {
	NewBitcoinURL                                              string
	CanPay                                                     bool
	CSRF                                                       string
	Order                                                      *getters.ServiceOrderView
	Heading, Message, Total, Tax, Refunded, NetPaid, ReturnURL string
}

func ServiceOrder(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	id, err := auth.Resolve(r, app)
	if err != nil {
		http.Error(w, "Unable to verify your account", http.StatusServiceUnavailable)
		return
	}
	if id == nil || id.PersonID == "" {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	publicID := strings.TrimSpace(mux.Vars(r)["order"])
	if publicID == "" || len(publicID) > 128 {
		http.NotFound(w, r)
		return
	}
	order, err := getters.ServiceOrderForPerson(r.Context(), app, id.PersonID, publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Unable to load this order", http.StatusServiceUnavailable)
		return
	}
	page := serviceOrderPresentation(order)
	if order.Quote.ProductCode == "alerts.repository-slots" && order.Status == "refunded" && order.RefundedCents == order.TotalCents && order.TotalCents > 0 && order.Quote.Quantity > 0 {
		page.NewBitcoinURL = "/services/alerts/prepay?" + url.Values{"account": {id.PersonID}, "slots": {strconv.FormatInt(order.Quote.Quantity, 10)}, "months": {"12"}}.Encode()
	}
	page.CanPay = serviceOrderCanPay(order, app, time.Now())
	if page.CanPay {
		page.CSRF, err = ensureAuthMethodsCSRF(app, r)
		if err != nil {
			http.Error(w, "Unable to prepare checkout", 500)
			return
		}
	}
	var html bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&html, "service_order.tmpl", page); err != nil {
		app.Err.Printf("service order render: %v", err)
		http.Error(w, "Unable to show this order", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(html.Bytes())
}

func serviceOrderCanPay(order *getters.ServiceOrderView, app *config.AppContext, now time.Time) bool {
	available := app != nil && app.Env != nil && ((order.Quote.Provider == "opennode" && strings.TrimSpace(app.Env.OpenNode.Key) != "") || (order.Quote.Provider == "stripe" && order.Quote.ProductCode == expedition.ProductCode && app.Env.StripeKey != ""))
	if order.Quote.ProductCode == expedition.ProductCode {
		available = available && antarcticaCheckoutReady(app) && antarcticaPaymentProviderAllowed(app, order.Quote.Provider)
	}
	return available && order.Status == "pending" && !order.PaymentVerified && order.Quote.ExpiresAt.After(now) && order.Currency == "USD" && order.TotalCents == order.Quote.TotalCents && order.TaxCents == order.Quote.TaxCents
}

func ServiceOrderPay(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", 405)
		return
	}
	id, err := auth.Resolve(r, app)
	if err != nil || id == nil || id.PersonID == "" {
		http.Error(w, "Sign in before paying", 401)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	if len(r.PostForm["csrf"]) != 1 || !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), r.PostForm.Get("csrf")) {
		http.Error(w, "Reload the order page and try again", 403)
		return
	}
	order, err := getters.ServiceOrderForPerson(r.Context(), app, id.PersonID, mux.Vars(r)["order"])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !serviceOrderCanPay(order, app, time.Now()) {
		http.Error(w, "This order is not available for payment. Return to the order page to check its status.", 409)
		return
	}
	var destination string
	if order.Quote.Provider == "stripe" {
		destination, err = expeditionStripeCheckout(r.Context(), app, id.PersonID, order)
	} else {
		destination, err = getters.OpenNodeServiceCheckout(r.Context(), app, id.PersonID, order.ID)
	}
	if err != nil {
		app.Err.Printf("service order payment handoff: %v", err)
		http.Error(w, "We could not open checkout. Return to the order page to check payment status before trying again.", 503)
		return
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func serviceOrderPresentation(order *getters.ServiceOrderView) serviceOrderPage {
	page := serviceOrderPage{Order: order, Heading: "Awaiting payment", Message: "Your payment has not yet been verified. If you just paid, confirmation may take a little time. Refresh this page to check again.", Total: fmt.Sprintf("$%d.%02d", order.TotalCents/100, order.TotalCents%100), Tax: fmt.Sprintf("$%d.%02d", order.TaxCents/100, order.TaxCents%100)}
	page.Refunded = fmt.Sprintf("$%d.%02d", order.RefundedCents/100, order.RefundedCents%100)
	net := order.TotalCents - order.RefundedCents
	page.NetPaid = fmt.Sprintf("$%d.%02d", net/100, net%100)
	if order.Quote.ProductCode == "alerts.repository-slots" {
		page.ReturnURL = "https://alerts.btcpp.dev/alerts"
	}
	switch {
	case order.Status == "refunded" || order.Status == "partially_refunded":
		page.Heading = "Refund recorded"
		page.Message = "A refund has been recorded for this order. Check your product account for your current access status."
	case order.Status == "cancelled" && !order.PaymentVerified:
		page.Heading = "Order canceled"
		page.Message = "This order is canceled."
	case order.PaymentVerified && order.Status != "paid":
		page.Heading = "Payment received — review needed"
		page.Message = "Your payment is recorded, but this order needs review before access can be activated. Please contact support with your order number."
	case order.PaymentVerified && order.AccessState == "active":
		page.Heading = "Payment confirmed"
		page.Message = "Your purchased access is active."
	case order.PaymentVerified && order.AccessState == "scheduled":
		page.Heading = "Payment confirmed"
		page.Message = "Your purchased access will begin on the start date below."
	case order.PaymentVerified && order.AccessState == "expired":
		page.Heading = "Access period ended"
		page.Message = "Your payment is recorded. The purchased access period has ended."
	case order.PaymentVerified && order.AccessState == "inactive":
		page.Heading = "Access inactive"
		page.Message = "Your payment is recorded, but access from this purchase is no longer active."
	case order.PaymentVerified:
		page.Heading = "Payment confirmed — access pending"
		page.Message = "Your payment is recorded. Access is still being activated. Please refresh shortly; contact support if this persists."
	}
	if order.Quote.ProductCode == expedition.ProductCode && order.PaymentVerified && order.Status != "paid" {
		page.Heading = "Payment recorded — booking needs review"
		page.Message = "Check your expedition booking and the payment or refund details below. Contact support before making another payment."
	}
	if order.Quote.ProductCode == expedition.ProductCode && order.PaymentVerified && order.Status == "paid" {
		page.Heading = "Payment received — berth confirmation pending"
		page.Message = "Your payment is recorded. Your expedition reservation is awaiting confirmation."
		if order.ExpeditionState == "confirmed" {
			page.Heading = "Expedition payment confirmed"
			page.Message = "This payment has been applied to your expedition reservation. Any remaining installments are still due on their scheduled dates."
		}
		if order.ExpeditionState == "review" {
			page.Heading = "Payment received — reservation needs review"
			page.Message = "Your payment is recorded, but we need to review your berth allocation. Please contact support with your order number."
		}
	}
	return page
}

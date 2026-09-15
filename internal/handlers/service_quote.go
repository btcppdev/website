package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
)

type serviceQuotePage struct {
	Stored                    *getters.ServiceCheckoutQuote
	CSRF, Total, Tax, Message string
	Expired                   bool
}

func ServiceQuoteReview(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	id, err := auth.Resolve(r, app)
	if err != nil {
		http.Error(w, "Unable to verify your account", 503)
		return
	}
	if id == nil || id.PersonID == "" {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), 303)
		return
	}
	stored, err := getters.ServiceCheckoutQuoteForPerson(app, id.PersonID, mux.Vars(r)["quote"])
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Unable to load this quote", 503)
		return
	}
	page := serviceQuotePresentation(stored, time.Now())
	if !page.Expired {
		page.CSRF, err = ensureAuthMethodsCSRF(app, r)
		if err != nil {
			http.Error(w, "Unable to prepare confirmation", 500)
			return
		}
	}
	var html bytes.Buffer
	if err = app.TemplateCache.ExecuteTemplate(&html, "service_quote.tmpl", page); err != nil {
		http.Error(w, "Unable to show this quote", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html.Bytes())
}

func ServiceQuoteConfirm(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", 405)
		return
	}
	id, err := auth.Resolve(r, app)
	if err != nil {
		http.Error(w, "Unable to verify your account", 503)
		return
	}
	if id == nil || id.PersonID == "" {
		http.Error(w, "Sign in before confirming", 401)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err = r.ParseForm(); err != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	if len(r.PostForm["csrf"]) != 1 || !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), r.PostForm.Get("csrf")) {
		http.Error(w, "Reload the quote page and try again", 403)
		return
	}
	// No browser-supplied quantities, prices, or tax evidence are read here.
	purchase, err := getters.ConfirmServiceCheckoutQuote(app, id.PersonID, mux.Vars(r)["quote"])
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, getters.ErrAlertsBillingChanged) || errors.Is(err, getters.ErrServiceQuote) {
		http.Error(w, "This quote expired or your billing changed. Request a new quote before continuing.", 409)
		return
	}
	if err != nil {
		app.Err.Printf("service quote confirmation: %v", err)
		http.Error(w, "Unable to confirm this quote. You can safely retry; no payment has been started.", 503)
		return
	}
	http.Redirect(w, r, "/services/orders/"+url.PathEscape(purchase.PublicID), 303)
}

func serviceQuotePresentation(stored *getters.ServiceCheckoutQuote, now time.Time) serviceQuotePage {
	q := stored.Quote
	page := serviceQuotePage{Stored: stored, Total: fmt.Sprintf("$%d.%02d", q.TotalCents/100, q.TotalCents%100), Tax: fmt.Sprintf("$%d.%02d", q.TaxCents/100, q.TaxCents%100), Expired: !q.ExpiresAt.After(now)}
	page.Message = "Review your additional projects and access period. Confirming creates an order; payment is a separate step."
	if q.BaseOrderID != "" {
		page.Message = "This prorated increase adds projects through your existing paid-through date. Your current projects are not charged again."
	}
	if page.Expired {
		page.Message = "This quote has expired. Request a new quote to continue."
	}
	return page
}

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
	"btcpp-web/internal/servicebilling"
	"btcpp-web/internal/types"
)

type alertsBitcoinStartPage struct {
	Account, CSRF, Total string
	Slots                int64
	Months               int
	Enabled              bool
}

func alertsBitcoinSelection(values url.Values) (string, int64, int, error) {
	account, slots, err := alertsStripeSelection(values)
	if err != nil {
		return "", 0, 0, err
	}
	months, err := strconv.Atoi(values.Get("months"))
	if err != nil || len(values["months"]) != 1 {
		return "", 0, 0, servicebilling.ErrAlertsPrice
	}
	if _, err = servicebilling.AlertsTerm("opennode", slots, months, time.Now()); err != nil {
		return "", 0, 0, err
	}
	return account, slots, months, nil
}

func alertsBitcoinAddress(values url.Values) (*types.ShopAddress, error) {
	for _, field := range []string{"line1", "line2", "city", "region", "postal", "country"} {
		if len(values[field]) != 1 || len(values.Get(field)) > 200 || strings.ContainsAny(values.Get(field), "\r\n\x00") {
			return nil, errors.New("invalid billing address")
		}
	}
	a := &types.ShopAddress{Line1: strings.TrimSpace(values.Get("line1")), Line2: strings.TrimSpace(values.Get("line2")), City: strings.TrimSpace(values.Get("city")), Region: strings.TrimSpace(values.Get("region")), PostalCode: strings.TrimSpace(values.Get("postal")), Country: strings.ToUpper(strings.TrimSpace(values.Get("country")))}
	if a.Line1 == "" || a.City == "" || len(a.Country) != 2 || a.Country[0] < 'A' || a.Country[0] > 'Z' || a.Country[1] < 'A' || a.Country[1] > 'Z' {
		return nil, errors.New("billing address is required")
	}
	return a, nil
}

// Only server-generated pricing and tax evidence reach the stored quote. Order
// confirmation and OpenNode charge creation remain separate, owned POST actions.
func AlertsBitcoinStart(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
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
	account, slots, months, err := alertsBitcoinSelection(values)
	if err != nil {
		http.Error(w, "Return to Alerts to select your repositories and prepaid period", 400)
		return
	}
	identity, err := auth.Resolve(r, app)
	if err != nil {
		http.Error(w, "Unable to verify your account", 503)
		return
	}
	if identity == nil || identity.PersonID == "" {
		if r.Method == http.MethodPost {
			http.Error(w, "Sign in before preparing checkout", 401)
			return
		}
		next := "/services/alerts/prepay?" + url.Values{"account": {account}, "slots": {strconv.FormatInt(slots, 10)}, "months": {strconv.Itoa(months)}}.Encode()
		http.Redirect(w, r, "/login?next="+url.QueryEscape(next), 303)
		return
	}
	if identity.PersonID != account {
		http.Error(w, "Sign in with the same Bitcoin++ account you use for Alerts before checking out.", 403)
		return
	}
	enabled := app.Env != nil && strings.TrimSpace(app.Env.OpenNode.Key) != "" && strings.TrimSpace(app.Env.StripeKey) != ""
	if r.Method == http.MethodPost {
		if len(values["csrf"]) != 1 || !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), values.Get("csrf")) {
			http.Error(w, "Reload checkout and try again", 403)
			return
		}
		if !enabled {
			http.Error(w, "Bitcoin checkout is not configured here yet", 503)
			return
		}
		address, err := alertsBitcoinAddress(values)
		if err != nil {
			http.Error(w, "Enter a complete billing address with a two-letter country code", 422)
			return
		}
		quote, tax, err := PrepareAlertsCheckout(r.Context(), app, account, "opennode", slots, months, address)
		if errors.Is(err, ErrAlertsPendingPurchase) || errors.Is(err, ErrAlertsPlanReview) || errors.Is(err, ErrAlertsRenewalChange) || errors.Is(err, ErrAlertsZeroCharge) {
			http.Error(w, "Your existing Alerts plan needs review before this purchase. Return to My alerts to check your billing.", 409)
			return
		}
		if err != nil {
			http.Error(w, "Unable to prepare a tax-inclusive quote. Check your billing address and try again. No payment has been started.", 503)
			return
		}
		stored, err := getters.SaveAlertsCheckoutQuote(app, account, *quote, *tax)
		if err != nil {
			http.Error(w, "Unable to save your quote. No payment has been started.", 503)
			return
		}
		http.Redirect(w, r, "/services/quotes/"+url.PathEscape(stored.ID), 303)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare checkout", 500)
		return
	}
	total := slots * servicebilling.AlertsMonthlySlotCents * int64(months)
	page := alertsBitcoinStartPage{Account: account, CSRF: csrf, Slots: slots, Months: months, Total: fmt.Sprintf("$%d.%02d", total/100, total%100), Enabled: enabled}
	var html bytes.Buffer
	if err = app.TemplateCache.ExecuteTemplate(&html, "alerts_bitcoin_start.tmpl", page); err != nil {
		http.Error(w, "Unable to show checkout", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html.Bytes())
}

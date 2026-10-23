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
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"btcpp-web/internal/types"
)

type alertsBitcoinStartPage struct {
	Account, CSRF, Total                 string
	SwitchID, RenewOrderID, Starts, Ends string
	Slots                                int64
	Months                               int
	Enabled                              bool
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
		nextValues := url.Values{"account": {account}, "slots": {strconv.FormatInt(slots, 10)}, "months": {strconv.Itoa(months)}}
		for _, name := range []string{"switch", "renew"} {
			if len(values[name]) == 1 {
				nextValues.Set(name, values.Get(name))
			}
		}
		next := "/services/alerts/prepay?" + nextValues.Encode()

		http.Redirect(w, r, "/login?next="+url.QueryEscape(next), 303)
		return
	}
	if identity.PersonID != account {
		http.Error(w, "Sign in with the same Bitcoin++ account you use for Alerts before checking out.", 403)
		return
	}
	enabled := app.Env != nil && strings.TrimSpace(app.Env.OpenNode.Key) != "" && strings.TrimSpace(app.Env.StripeKey) != ""
	switchID := values.Get("switch")
	renewOrder := values.Get("renew")
	var switchStart time.Time
	if switchID != "" {
		if len(values["switch"]) != 1 || months != 12 {
			http.Error(w, "Invalid Bitcoin switch", 400)
			return
		}
		sw, e := getters.AlertsBitcoinSwitchForPerson(r.Context(), app, account, switchID)
		if e != nil || sw.State != "scheduled" || sw.CancellationState != "confirmed" || sw.Quantity != slots || sw.PurchaseID != "" {
			http.Error(w, "Return to payment settings to review this switch", 409)
			return
		}
		if !enabled {
			http.Error(w, "Bitcoin checkout is unavailable", 503)
			return
		}
		client := alertsstripe.NewClient(strings.HasPrefix(app.Env.StripeKey, "sk_live_") || strings.HasPrefix(app.Env.StripeKey, "rk_live_"))
		if e = getters.RefreshAlertsStripeSubscription(r.Context(), app, account, sw.CheckoutID, client); e != nil {
			http.Error(w, "Unable to verify card cancellation", 503)
			return
		}
		switchStart = time.Unix(sw.PeriodEnd, 0).UTC()
		if switchStart.Before(time.Now()) {
			switchStart = time.Now().UTC().Truncate(time.Second)
		}
	}

	if renewOrder != "" {
		if switchID != "" || len(values["renew"]) != 1 || months != 12 {
			http.Error(w, "Invalid renewal", 400)
			return
		}
		quantity, end, e := getters.AlertsPrepaidRenewal(r.Context(), app, account, renewOrder)
		if e != nil || quantity != slots {
			http.Error(w, "Your prepaid plan changed. Return to billing to review it.", 409)
			return
		}
		switchStart = end
		if switchStart.Before(time.Now()) {
			switchStart = time.Now().UTC().Truncate(time.Second)
		}
	}
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
		var quote *getters.ServiceQuote
		var tax *getters.TaxQuoteInput
		if switchID != "" || renewOrder != "" {
			quote, tax, err = AlertsTermTaxQuote(r.Context(), "opennode", slots, 12, switchStart, address)
			if err == nil {
				quote.AccountRevision, err = getters.AlertsBillingRevision(app, account)
				quote.AlertsSwitchID = switchID
				quote.AlertsRenewOrderID = renewOrder
				quote.BillingMonths = 12
			}
		} else {
			quote, tax, err = PrepareAlertsCheckout(r.Context(), app, account, "opennode", slots, months, address)
		}

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
	if switchID != "" || renewOrder != "" {
		page.SwitchID = switchID
		page.RenewOrderID = renewOrder
		page.Starts = switchStart.Format("January 2, 2006")
		term, _ := servicebilling.AlertsTerm("opennode", slots, 12, switchStart)
		page.Ends = term.End.Format("January 2, 2006")
	}
	var html bytes.Buffer

	if err = app.TemplateCache.ExecuteTemplate(&html, "alerts_bitcoin_start.tmpl", page); err != nil {
		http.Error(w, "Unable to show checkout", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html.Bytes())
}

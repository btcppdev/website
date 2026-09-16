package handlers

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"encoding/json"
	"errors"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/url"
	"strings"
)

// AlertsStripeRenew resolves only account-owned, previously verified renewals.
// Stripe's hosted invoice handles payment; the normal verifier grants access.
func AlertsStripeRenew(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", 405)
		return
	}
	identity, err := auth.Resolve(r, app)
	if err != nil {
		http.Error(w, "Unable to verify your account", 503)
		return
	}
	if identity == nil || identity.PersonID == "" {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), 303)
		return
	}
	id := mux.Vars(r)["invoice"]
	var raw []byte
	var state string
	err = app.DB.QueryRow(r.Context(), `SELECT s.binding,r.state FROM alerts_stripe_renewals r JOIN alerts_stripe_checkouts c ON c.id=r.checkout_id JOIN alerts_stripe_current_subscriptions s ON s.checkout_id=c.id WHERE r.invoice_id=$1 AND c.person_id=$2`, id, identity.PersonID).Scan(&raw, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Unable to load renewal", 503)
		return
	}
	var b alertsstripe.Binding
	if json.Unmarshal(raw, &b) != nil || b.PersonID != identity.PersonID {
		http.Error(w, "Unable to verify renewal", 503)
		return
	}
	if state != "failed" {
		http.Redirect(w, r, "/services/alerts/checkout/"+url.PathEscape(b.IntentID), 303)
		return
	}
	key := ""
	if app.Env != nil {
		key = app.Env.StripeKey
	}
	if !strings.HasPrefix(key, "sk_live_") && !strings.HasPrefix(key, "sk_test_") && !strings.HasPrefix(key, "rk_live_") && !strings.HasPrefix(key, "rk_test_") {
		http.Error(w, "Payment temporarily unavailable", 503)
		return
	}
	live := strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "rk_live_")
	destination, err := alertsstripe.NewClient(live).RenewalLink(r.Context(), id, b)
	if err != nil {
		http.Error(w, "Unable to open renewal. Please try again shortly.", 503)
		return
	}
	http.Redirect(w, r, destination, 303)
}

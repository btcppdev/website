package handlers

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

var installmentPageTemplate = template.Must(template.New("installment").Parse(`<!doctype html><html lang="en"><head><meta name="viewport" content="width=device-width,initial-scale=1"><title>Your expedition installment</title><link rel="stylesheet" href="/static/css/antarctica.css"><link rel="stylesheet" href="/static/css/antarctica-checkout.css"></head><body class="antarctica expedition-checkout"><header><a class="wordmark" href="/">bitcoin<span>++</span></a><a href="/dashboard/orders">Your dashboard</a></header><main class="checkout-shell"><p class="eyebrow">Cold Storage Edition</p><h1>Payment {{.Number}} of {{.Count}}.</h1><p><strong>{{.Amount}} USD · due {{.Due}}</strong></p><p>Pay with {{.ProviderName}}. The full due date is available in Texas time (America/Chicago). This payment applies to your existing booking.</p><form method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="key" value="{{.Key}}"><input type="hidden" name="version" value="{{.Version}}"><p>Need a company invoice? You can add billing details after payment.</p><details><summary>Terms for this payment · {{.Version}}</summary><div class="policy-text">{{.Terms}}</div><h2>Privacy</h2><div class="policy-text">{{.Privacy}}</div></details><label class="checkout-check"><input type="checkbox" name="accepted" value="yes" required><span>I agree to these payment terms and acknowledge the privacy policy.</span></label><button type="submit" class="button">Continue to {{.ProviderName}} payment →</button></form><p><a href="{{.BookingURL}}">Back to your booking</a> · <a href="mailto:inbox@btcpp.dev">Get help</a></p></main></body></html>`))

func ExpeditionInstallment(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	id, err := auth.Resolve(r, app)
	if err != nil {
		http.Error(w, "Unable to verify account", 503)
		return
	}
	if id == nil || id.PersonID == "" {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), 303)
		return
	}
	number, err := strconv.Atoi(mux.Vars(r)["number"])
	if err != nil || number < 2 || number > 5 {
		http.NotFound(w, r)
		return
	}
	booking, err := getters.ExpeditionBookingForPerson(r.Context(), app, id.PersonID, mux.Vars(r)["booking"])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if app == nil || app.Env == nil || !antarcticaPaymentProviderAllowed(app, booking.Provider) || (booking.Provider == expedition.Bitcoin && app.Env.OpenNode.Key == "") {
		http.Error(w, "Installment checkout is unavailable. Contact inbox@btcpp.dev.", 503)
		return
	}
	if booking.State != "confirmed" {
		http.Error(w, "This booking is not available for installment payment.", 409)
		return
	}
	if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare payment", 503)
		return
	}
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		if r.ParseForm() != nil || len(r.PostForm["csrf"]) != 1 || !secureTokenEqual(csrf, r.PostForm.Get("csrf")) {
			http.Error(w, "Reload before trying again", 403)
			return
		}
	}
	// Refresh unresolved provider evidence before deciding whether a new order is safe.
	if err = getters.ReconcileExpeditionPayments(r.Context(), app, booking.ID); err != nil {
		if existing, e := getters.ExpeditionInstallmentOrder(r.Context(), app, id.PersonID, booking.ID, number); e == nil && existing != "" {
			http.Redirect(w, r, "/services/orders/"+url.PathEscape(existing), 303)
			return
		}
		http.Error(w, "A previous payment needs reconciliation. Contact inbox@btcpp.dev before paying again.", 409)
		return
	}
	existing, err := getters.ExpeditionInstallmentOrder(r.Context(), app, id.PersonID, booking.ID, number)
	if err != nil {
		http.Error(w, "Unable to check existing payment", 503)
		return
	}
	if existing != "" {
		http.Redirect(w, r, "/services/orders/"+url.PathEscape(existing), 303)
		return
	}
	charge, err := (&expedition.Store{DB: app.DB}).Charge(r.Context(), id.PersonID, booking.ID, number)
	if err != nil {
		http.Error(w, "This installment is not payable. Check your booking schedule.", 409)
		return
	}
	policy, err := getters.CurrentServiceLegalPolicy(app, expedition.ProductCode)
	if err != nil || policy == nil {
		http.Error(w, "Payment terms unavailable", 503)
		return
	}
	if r.Method == "GET" {
		providerName := "Bitcoin"
		if booking.Provider == expedition.Stripe {
			providerName = "Stripe"
		}
		d := struct {
			Number, Count                                                             int
			Amount, Due, CSRF, Key, Version, Terms, Privacy, BookingURL, ProviderName string
		}{number, len(booking.Payments), antarcticaMoney(charge.TotalCents), booking.Payments[number-1].DueAt.UTC().Format("January 2, 2006"), csrf, uuid.NewString(), policy.Version, policy.TermsText, policy.PrivacyText, "/expeditions/bookings/" + url.PathEscape(booking.ID), providerName}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err = installmentPageTemplate.Execute(w, d); err != nil {
			app.Err.Printf("installment page: %v", err)
		}
		return
	}
	if r.PostForm.Get("accepted") != "yes" || r.PostForm.Get("version") != policy.Version {
		http.Error(w, "Review and accept the current terms", 422)
		return
	}
	key, err := uuid.Parse(r.PostForm.Get("key"))
	if err != nil {
		http.Error(w, "Invalid payment key", 400)
		return
	}
	// Stripe Checkout sessions must expire within 24 hours. Freeze a one-hour
	// window in the new order; retries reuse that order and its expiry.
	if booking.Provider == expedition.Stripe {
		window := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
		if window.Before(charge.ExpiresAt) {
			charge.ExpiresAt = window
		}
	}
	tax := getters.ExpeditionConferenceTaxQuote(charge.ExpiresAt)
	purchase, err := getters.CreateExpeditionInstallmentPurchase(app, id.PersonID, booking.ID, number, charge.Key+":"+key.String(), tax, policy.Version)
	if err != nil {
		if existing, e := getters.ExpeditionInstallmentOrder(r.Context(), app, id.PersonID, booking.ID, number); e == nil && existing != "" {
			http.Redirect(w, r, "/services/orders/"+url.PathEscape(existing), 303)
			return
		}
		http.Error(w, "Unable to prepare payment. Refresh your booking before retrying.", 409)
		return
	}
	http.Redirect(w, r, "/services/orders/"+url.PathEscape(purchase.PublicID), 303)
}

var expeditionAuditTemplate = template.Must(template.New("audit").Parse(`<!doctype html><html><head><title>Expedition booking audit</title></head><body><h1>Expedition booking {{.ID}}</h1><p>State: {{.State}}</p>{{range .Events}}<article><h2>{{.Kind}} · {{.At}}</h2><pre style="white-space:pre-wrap">{{.Snapshot}}</pre><p>{{.Delivery}}</p></article>{{end}}</body></html>`))

func ExpeditionBookingAudit(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if requireGlobalAdmin(w, r, app) == nil {
		return
	}
	id := mux.Vars(r)["booking"]
	if _, err := uuid.Parse(id); err != nil {
		http.NotFound(w, r)
		return
	}
	var state string
	if err := app.DB.QueryRow(r.Context(), `SELECT state FROM expedition_reservations WHERE id=$1`, id).Scan(&state); err != nil {
		http.NotFound(w, r)
		return
	}
	rows, err := app.DB.Query(r.Context(), `SELECT e.kind,e.occurred_at,e.snapshot,COALESCE((SELECT string_agg(m.audience||': '||m.state||' ('||m.attempts::text||' attempts)',', ') FROM expedition_lifecycle_mail m WHERE m.event_id=e.id),'') FROM expedition_lifecycle_events e WHERE e.reservation_id=$1 ORDER BY e.occurred_at`, id)
	if err != nil {
		http.Error(w, "Unable to load audit", 503)
		return
	}
	defer rows.Close()
	type event struct{ Kind, At, Snapshot, Delivery string }
	var events []event
	for rows.Next() {
		var e event
		var at time.Time
		var raw []byte
		if err = rows.Scan(&e.Kind, &at, &raw, &e.Delivery); err != nil {
			http.Error(w, "Unable to load audit", 503)
			return
		}
		var pretty bytes.Buffer
		json.Indent(&pretty, raw, "", "  ")
		e.At = at.Format(time.RFC3339)
		e.Snapshot = pretty.String()
		events = append(events, e)
	}
	if rows.Err() != nil {
		http.Error(w, "Unable to load audit", 503)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err = expeditionAuditTemplate.Execute(w, struct {
		ID, State string
		Events    []event
	}{id, state, events}); err != nil {
		app.Err.Printf("expedition audit: %v", err)
	}
}

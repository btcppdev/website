package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/skip2/go-qrcode"
)

func registerCheckInUpgradeRoutes(r *mux.Router, app *config.AppContext) {
	r.HandleFunc("/check-in/{ticket}/proof", func(w http.ResponseWriter, r *http.Request) { CheckInProof(w, r, app) }).Methods("POST")
	r.HandleFunc("/check-in/{ticket}/upgrade", func(w http.ResponseWriter, r *http.Request) { CheckInUpgradeStart(w, r, app) }).Methods("POST")
	r.HandleFunc("/check-in/{ticket}/upgrade/{upgrade}", func(w http.ResponseWriter, r *http.Request) { CheckInUpgradeStatus(w, r, app) }).Methods("GET")
	r.HandleFunc("/check-in/{ticket}/upgrade/{upgrade}/qr", func(w http.ResponseWriter, r *http.Request) { CheckInUpgradeQR(w, r, app) }).Methods("GET")
	r.HandleFunc("/callback/opennode-ticket-upgrade/{upgrade}", func(w http.ResponseWriter, r *http.Request) { CheckInUpgradeCallback(w, r, app) }).Methods("POST")
}

func populateCheckInEligibility(r *http.Request, app *config.AppContext, p *CheckInPage) error {
	var err error
	p.Eligibility, err = getters.CheckInEligibility(app, p.TicketRef)
	if err != nil {
		return err
	}
	if p.Eligibility.NeedsProof() {
		p.CheckInComplete = false
		p.CSRF, err = ensureAuthMethodsCSRF(app, r)
		if err != nil {
			return err
		}
		full, paid, currency, e := getters.TicketUpgradePrice(app, p.TicketRef)
		if e != nil {
			p.UpgradeError = e.Error()
		} else {
			amount := full - paid
			if amount < 0 {
				amount = 0
			}
			p.UpgradePrice = fmt.Sprintf("%s %.2f", currency, float64(amount)/100)
		}
	}
	return nil
}

func checkInUpgradeAuthorized(w http.ResponseWriter, r *http.Request, app *config.AppContext) bool {
	w.Header().Set("Cache-Control", "private, no-store")
	if !posAuthorized(r, app) {
		http.Error(w, "Check-in passcode required", 401)
		return false
	}
	if r.Method == "POST" && !posPost(w, r, app) {
		return false
	}
	return true
}
func CheckInProof(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	if !checkInUpgradeAuthorized(w, r, app) {
		return
	}
	revision, err := strconv.ParseInt(r.PostForm.Get("revision"), 10, 64)
	ref := mux.Vars(r)["ticket"]
	if err == nil {
		err = getters.ConfirmCheckInProof(app, ref, posOperator(r, app), revision)
	}
	if err != nil {
		http.Error(w, "Ticket changed; reload and check the proof again.", 409)
		return
	}
	http.Redirect(w, r, "/check-in/"+url.PathEscape(ref), 303)
}
func CheckInUpgradeStart(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	if !checkInUpgradeAuthorized(w, r, app) {
		return
	}
	if app.Env.OpenNode.Key == "" {
		http.Error(w, "Bitcoin payments are not configured", 503)
		return
	}
	revision, err := strconv.ParseInt(r.PostForm.Get("revision"), 10, 64)
	if err != nil {
		http.Error(w, "Reload check-in", 400)
		return
	}
	u, created, err := getters.BeginCheckInUpgrade(app, mux.Vars(r)["ticket"], posOperator(r, app), revision)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	if created {
		if u.AmountCents == 0 {
			err = getters.ApplyCheckInUpgradePayment(app, u.ID, nil)
		} else {
			err = getters.CreateCheckInUpgradeCharge(app, u)
		}
		if err != nil {
			app.Err.Printf("ticket upgrade %s creation needs reconciliation: %s", u.ID, err)
		}
	}
	u, err = getters.GetCheckInUpgrade(app, u.ID, u.TicketRef)
	if err != nil {
		http.Error(w, "Unable to load upgrade", 500)
		return
	}
	writeCheckInUpgrade(w, u)
}
func writeCheckInUpgrade(w http.ResponseWriter, u *types.CheckInUpgrade) {
	status := u.Status
	message := "Waiting for payment"
	switch status {
	case "creating":
		message = "Preparing payment. If this takes a while, ask an admin to check the charge before trying again."
	case "processing":
		message = "Payment detected. Waiting for confirmation."
	case "underpaid":
		message = "Payment is short. Ask an admin to review it."
	case "paid":
		message = "Paid · full ticket ready"
	case "review":
		message = "Payment needs admin review. Do not charge again."
	case "expired", "failed", "refunded":
		message = "This payment is no longer available. Close and try again."
	}
	qr := u.Status == "pending" && u.Invoice != "" && u.ExpiresAt != nil && time.Now().Before(*u.ExpiresAt)
	if status == "pending" && !qr {
		message = "QR expired. Checking for payment; do not pay this invoice."
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": u.ID, "status": status, "message": message, "amount": fmt.Sprintf("%s %.2f", u.Currency, float64(u.AmountCents)/100), "sats": u.TotalSats, "qr_available": qr, "expires_at": u.ExpiresAt})
}
func readCheckInUpgrade(w http.ResponseWriter, r *http.Request, app *config.AppContext) *types.CheckInUpgrade {
	if !checkInUpgradeAuthorized(w, r, app) {
		return nil
	}
	id := mux.Vars(r)["upgrade"]
	if _, err := uuid.Parse(id); err != nil {
		http.NotFound(w, r)
		return nil
	}
	u, err := getters.GetCheckInUpgrade(app, id, mux.Vars(r)["ticket"])
	if err != nil {
		http.NotFound(w, r)
		return nil
	}
	var current bool
	err = app.DB.QueryRow(app.DatabaseContext(), `SELECT attendee_access_key=$2::uuid AND (proof_revision=$3 OR full_ticket_upgraded_at IS NOT NULL) AND NOT revoked FROM registrations WHERE ref_id=$1`, u.TicketRef, u.OwnerKey, u.Revision).Scan(&current)
	if err != nil || !current {
		http.Error(w, "Ticket changed. Close and reload check-in.", 409)
		return nil
	}
	return u
}
func reconcileCheckInUpgrade(app *config.AppContext, u *types.CheckInUpgrade) error {
	if u.ChargeID == "" || u.Status == "paid" || u.Status == "review" {
		return nil
	}
	p, err := getters.LookupCheckInUpgradeCharge(app, u.ChargeID)
	if err != nil {
		return err
	}
	return getters.ApplyCheckInUpgradePayment(app, u.ID, p)
}
func CheckInUpgradeStatus(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	u := readCheckInUpgrade(w, r, app)
	if u == nil {
		return
	}
	if err := reconcileCheckInUpgrade(app, u); err != nil {
		http.Error(w, "Unable to confirm payment yet. Do not charge again.", 503)
		return
	}
	u, err := getters.GetCheckInUpgrade(app, u.ID, u.TicketRef)
	if err != nil {
		http.Error(w, "Unable to load upgrade", 500)
		return
	}
	writeCheckInUpgrade(w, u)
}
func CheckInUpgradeQR(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	u := readCheckInUpgrade(w, r, app)
	if u == nil {
		return
	}
	if u.Status != "pending" || u.ExpiresAt == nil || !time.Now().Before(*u.ExpiresAt) {
		http.Error(w, "Invoice unavailable", 409)
		return
	}
	value := "lightning:" + strings.ToUpper(u.Invoice)
	if r.URL.Query().Get("network") == "onchain" {
		value = fmt.Sprintf("bitcoin:%s?amount=%d.%08d", u.ChainAddress, u.TotalSats/100000000, u.TotalSats%100000000)
	} else if u.Invoice == "" {
		http.Error(w, "Invoice unavailable", 409)
		return
	}
	png, err := qrcode.Encode(value, qrcode.Medium, 400)
	if err != nil {
		http.Error(w, "QR unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}
func CheckInUpgradeCallback(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	limitRequestBody(w, r, maxWebhookBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad callback", 400)
		return
	}
	id, chargeID := mux.Vars(r)["upgrade"], r.PostForm.Get("id")
	if _, err := uuid.Parse(id); err != nil || app.Env.OpenNode.Key == "" || chargeID == "" || !validHash(app.Env.OpenNode.Key, chargeID, r.PostForm.Get("hashed_order")) {
		http.Error(w, "Invalid signature", 403)
		return
	}
	p, err := getters.LookupCheckInUpgradeCharge(app, chargeID)
	if err != nil {
		http.Error(w, "Unable to verify payment", 503)
		return
	}
	if err = getters.ApplyCheckInUpgradePayment(app, id, p); err != nil {
		app.Err.Printf("ticket upgrade callback %s: %s", id, err)
		http.Error(w, "Unable to record payment", 503)
		return
	}
	w.WriteHeader(204)
}
func reconcileCheckInUpgrades(app *config.AppContext) {
	rows, err := app.DB.Query(app.DatabaseContext(), `SELECT id::text,ticket_ref FROM ticket_checkin_upgrades WHERE status IN ('pending','processing','underpaid') AND charge_id IS NOT NULL ORDER BY checked_at NULLS FIRST,created_at LIMIT 20`)
	if err != nil {
		app.Err.Printf("ticket upgrade reconciliation: %s", err)
		return
	}
	var ids [][2]string
	for rows.Next() {
		var id [2]string
		if rows.Scan(&id[0], &id[1]) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		u, err := getters.GetCheckInUpgrade(app, id[0], id[1])
		if err == nil {
			err = reconcileCheckInUpgrade(app, u)
		}
		if err != nil {
			app.Err.Printf("ticket upgrade reconciliation %s: %s", id[0], err)
		}
	}
}

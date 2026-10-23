package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"github.com/jackc/pgx/v5"
)

const antarcticaSalesFeature = "antarctica_checkout"

func antarcticaSalesState(app *config.AppContext) (bool, error) {
	if app == nil || app.DB == nil {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var enabled bool
	err := app.DB.QueryRow(ctx, `SELECT enabled FROM site_features WHERE name=$1`, antarcticaSalesFeature).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return enabled, err
}

func antarcticaSalesEnabled(app *config.AppContext) bool {
	enabled, err := antarcticaSalesState(app)
	return err == nil && enabled
}

func antarcticaLaunchBlockers(app *config.AppContext) []string {
	var blockers []string
	for _, key := range []string{"SERVICE_FULFILLMENT_ENABLED", "SERVICE_PAYMENT_RECONCILIATION_ENABLED", "SERVICE_RECEIPTS_ENABLED", "ANTARCTICA_INSTALLMENT_LIFECYCLE_ENABLED"} {
		if os.Getenv(key) != "true" {
			blockers = append(blockers, "Enable the production worker setting "+key+".")
		}
	}
	if !serviceReceiptMailReady(app) {
		blockers = append(blockers, "Configure transactional email delivery for confirmations and reminders.")
	}
	if app.Env == nil || app.Env.StripeKey == "" {
		blockers = append(blockers, "Configure the shared Stripe credential for tax calculation.")
	}
	if app.Env == nil || app.Env.OpenNode.Key == "" {
		blockers = append(blockers, "Configure the shared OpenNode payment credential.")
	}
	policy, err := getters.CurrentServiceLegalPolicy(app, expedition.ProductCode)
	if err != nil || policy == nil {
		blockers = append(blockers, "Publish the expedition booking terms and privacy policy below.")
	} else {
		var registered bool
		err = app.DB.QueryRow(app.DatabaseContext(), `SELECT EXISTS(SELECT 1 FROM expedition_lifecycle_policies WHERE version=$1 AND time_zone='America/Chicago')`, policy.Version).Scan(&registered)
		if err != nil || !registered {
			blockers = append(blockers, "Publish this policy for Texas-time installment reminders and abandonment.")
		}
	}
	var categories int
	err = app.DB.QueryRow(app.DatabaseContext(), `SELECT count(*) FROM expedition_inventory WHERE sales_open AND booking_closes_at>now() AND capacity>0`).Scan(&categories)
	if err != nil || categories != 3 {
		blockers = append(blockers, "Configure all three berth categories with an open sales window.")
	}
	return blockers
}

type antarcticaAdminPage struct {
	CSRF, Flash, Version, Effective, Terms, Privacy string
	Enabled                                         bool
	Blockers                                        []string
	Current                                         *getters.ServiceLegalPolicy
}

func AdminAntarctica(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	id := requireGlobalAdmin(w, r, app)
	if id == nil {
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 600<<10)
		if r.ParseForm() != nil || !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), r.PostFormValue("csrf")) {
			http.Error(w, "Invalid form token", http.StatusForbidden)
			return
		}
		var err error
		message := ""
		switch r.PostFormValue("action") {
		case "open", "pause":
			enabled := r.PostFormValue("action") == "open"
			if enabled {
				if blockers := antarcticaLaunchBlockers(app); len(blockers) > 0 {
					http.Redirect(w, r, "/admin/antarctica?blocked=1&flash="+url.QueryEscape("Checkout remains paused. Complete the items listed below before opening sales."), http.StatusSeeOther)
					return
				}
			}
			_, err = app.DB.Exec(r.Context(), `INSERT INTO site_features(name,enabled,updated_by) VALUES($1,$2,$3) ON CONFLICT(name) DO UPDATE SET enabled=EXCLUDED.enabled,updated_by=EXCLUDED.updated_by,updated_at=now()`, antarcticaSalesFeature, enabled, id.PersonID)
			message = "Antarctica checkout paused. Existing bookings and installment payments are unchanged."
			if enabled {
				message = "Antarctica checkout is open."
			}
		case "publish":
			effective, parseErr := time.Parse(time.RFC3339, r.PostFormValue("effective"))
			if parseErr != nil || r.PostFormValue("reviewed") != "yes" {
				http.Error(w, "Review the terms and privacy policy and enter a valid effective date (UTC).", 400)
				return
			}
			policy := getters.ServiceLegalPolicy{ProductCode: expedition.ProductCode, Version: strings.TrimSpace(r.PostFormValue("version")), TermsText: strings.TrimSpace(r.PostFormValue("terms")), PrivacyText: strings.TrimSpace(r.PostFormValue("privacy")), EffectiveAt: effective}
			err = getters.PublishExpeditionPolicy(app, policy)
			message = "Booking policy published for checkout and Texas-time installment reminders/abandonment."
		case "inventory":
			// Initialize only missing rows. Never overwrite existing capacity or live bookings.
			_, err = app.DB.Exec(r.Context(), `INSERT INTO expedition_inventory(berth,capacity,sales_open,booking_closes_at,hold_seconds) VALUES ('shared',18,true,'2027-01-01T00:00:00Z',3600),('triple',3,true,'2027-01-01T00:00:00Z',3600),('quad',4,true,'2027-01-01T00:00:00Z',3600) ON CONFLICT(berth) DO NOTHING`)
			message = "Missing inventory categories initialized. Existing inventory settings were preserved."
		default:
			http.Error(w, "Unknown action", 400)
			return
		}
		if err != nil {
			app.Err.Printf("Antarctica admin action %s by %s: %v", r.PostFormValue("action"), id.PersonID, err)
			http.Error(w, "Unable to save. Policy versions are immutable; use a new version for changed text, with an effective date no later than now and no earlier than the current policy.", http.StatusConflict)
			return
		}
		app.Infos.Printf("Antarctica admin action %s by %s", r.PostFormValue("action"), id.PersonID)
		http.Redirect(w, r, "/admin/antarctica?flash="+url.QueryEscape(message), http.StatusSeeOther)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare form", 500)
		return
	}
	policy, err := getters.CurrentServiceLegalPolicy(app, expedition.ProductCode)
	if err != nil {
		http.Error(w, "Unable to load policy", 503)
		return
	}
	enabled, err := antarcticaSalesState(app)
	if err != nil {
		http.Error(w, "Unable to load checkout status", 503)
		return
	}
	now := time.Now().UTC().Truncate(time.Second)
	page := antarcticaAdminPage{CSRF: csrf, Flash: r.URL.Query().Get("flash"), Enabled: enabled, Blockers: antarcticaLaunchBlockers(app), Current: policy, Version: "antarctica-" + now.Format("20060102-150405"), Effective: now.Format(time.RFC3339), Terms: expedition.BookingTerms, Privacy: expedition.BookingPrivacy}
	if policy != nil {
		page.Terms, page.Privacy = policy.TermsText, policy.PrivacyText
	}
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "admin/antarctica.tmpl", page); err != nil {
		app.Err.Printf("Antarctica admin template: %v", err)
		http.Error(w, "Unable to render settings", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(out.Bytes())
}

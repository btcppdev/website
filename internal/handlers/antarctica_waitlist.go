package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/expedition"
	"github.com/jackc/pgx/v5"
)

type antarcticaWaitlistPage struct {
	Preview                     bool
	CSRF, Email                 string
	Joined, Notified, Available bool
}

func AntarcticaWaitlist(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if app.DB == nil {
		http.Error(w, "The waitlist will be available when reservations open.", 503)
		return
	}
	identity, err := auth.Resolve(r, app)
	if err != nil {
		http.Error(w, "Unable to verify your sign-in.", 503)
		return
	}
	if identity == nil || identity.PersonID == "" {
		http.Redirect(w, r, "/login?next="+url.QueryEscape("/expeditions/antarctica/waitlist"), 303)
		return
	}
	var page antarcticaWaitlistPage
	var contact string
	err = app.DB.QueryRow(r.Context(), `SELECT pe.contact_id::text,c.email::text FROM person_emails pe JOIN email_contacts c ON c.id=pe.contact_id WHERE pe.person_id=$1 AND pe.verified_at IS NOT NULL AND c.erased_at IS NULL ORDER BY pe.is_primary DESC,pe.email LIMIT 1`, identity.PersonID).Scan(&contact, &page.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Add and verify an email in your bitcoin++ account before joining the waitlist.", 403)
		return
	}
	if err != nil {
		http.Error(w, "Unable to load your email.", 503)
		return
	}
	page.CSRF, err = ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare the waitlist.", 503)
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil || len(r.PostForm["csrf"]) != 1 || !secureTokenEqual(page.CSRF, r.PostForm.Get("csrf")) {
			http.Error(w, "Reload the page and try again.", 403)
			return
		}
		switch r.PostForm.Get("action") {
		case "join":
			err = joinAntarcticaWaitlist(r.Context(), app, identity.PersonID, contact)
			if errors.Is(err, expedition.ErrConflict) {
				http.Error(w, "Spots are available now. Return to the expedition checkout to reserve.", 409)
				return
			}
		case "leave":
			_, err = app.DB.Exec(r.Context(), `UPDATE expedition_waitlist SET subscribed=false WHERE person_id=$1`, identity.PersonID)
		default:
			http.Error(w, "Invalid waitlist action.", 400)
			return
		}
		if err != nil {
			app.Err.Printf("expedition waitlist: %v", err)
			http.Error(w, "Unable to update the waitlist. Please try again.", 503)
			return
		}
		http.Redirect(w, r, "/expeditions/antarctica/waitlist", 303)
		return
	}
	a, err := (&expedition.Store{DB: app.DB}).Availability(r.Context())
	if err != nil {
		http.Error(w, "Unable to check availability.", 503)
		return
	}
	page.Available = a.Remaining > 0
	err = app.DB.QueryRow(r.Context(), `SELECT w.subscribed,w.notified_at IS NOT NULL,c.email::text FROM expedition_waitlist w JOIN email_contacts c ON c.id=w.contact_id WHERE w.person_id=$1`, identity.PersonID).Scan(&page.Joined, &page.Notified, &page.Email)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Unable to load waitlist status.", 503)
		return
	}
	var body bytes.Buffer
	if err = app.TemplateCache.ExecuteTemplate(&body, "conf/antarctica-waitlist.tmpl", page); err != nil {
		app.Err.Printf("waitlist template: %v", err)
		http.Error(w, "Unable to load waitlist.", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(body.Bytes())
}

func joinAntarcticaWaitlist(ctx context.Context, app *config.AppContext, person, contact string) error {
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize with reservations/staff allocation so a signup is for a genuinely
	// sold-out expedition, including spots temporarily held at checkout.
	var staff, total, used int
	if err = tx.QueryRow(ctx, `SELECT staff_berths FROM expedition_settings WHERE singleton FOR UPDATE`).Scan(&staff); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(capacity),0) FROM expedition_cabins WHERE enabled`).Scan(&total); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(quantity),0) FROM expedition_reservations WHERE (confirmed_at IS NOT NULL AND released_at IS NULL) OR (state='held' AND hold_expires_at>now())`).Scan(&used); err != nil {
		return err
	}
	if total-staff-used > 0 {
		return expedition.ErrConflict
	}
	tag, err := tx.Exec(ctx, `INSERT INTO expedition_waitlist(person_id,contact_id) SELECT $1,pe.contact_id FROM person_emails pe JOIN email_contacts c ON c.id=pe.contact_id WHERE pe.person_id=$1 AND pe.contact_id=$2 AND pe.verified_at IS NOT NULL AND c.erased_at IS NULL
 ON CONFLICT(person_id) DO UPDATE SET contact_id=CASE WHEN expedition_waitlist.subscribed THEN expedition_waitlist.contact_id ELSE excluded.contact_id END,subscribed=true,
 notification_key=CASE WHEN expedition_waitlist.subscribed THEN expedition_waitlist.notification_key ELSE gen_random_uuid() END,
 notified_at=CASE WHEN expedition_waitlist.subscribed THEN expedition_waitlist.notified_at ELSE NULL END,created_at=CASE WHEN expedition_waitlist.subscribed THEN expedition_waitlist.created_at ELSE now() END`, person, contact)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("verified waitlist email required")
	}
	return tx.Commit(ctx)
}

// Delivery is opt-in at deployment. It uses the existing mailer's durable job
// key, so retries after an ambiguous response cannot produce duplicate messages.
func StartAntarcticaWaitlist(app *config.AppContext) {
	if os.Getenv("ANTARCTICA_WAITLIST_NOTIFICATIONS_ENABLED") != "true" {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, err := processAntarcticaWaitlist(ctx, app, func(m *emails.Mail) error { return emails.ComposeAndSendMail(app, m) })
			cancel()
			if err != nil {
				app.Err.Printf("expedition waitlist notification needs retry: %v", err)
			}
		}
	}()
}

func processAntarcticaWaitlist(ctx context.Context, app *config.AppContext, send func(*emails.Mail) error) (bool, error) {
	if !antarcticaCheckoutReady(app) || !serviceReceiptMailReady(app) {
		return false, nil
	}
	policy, err := getters.CurrentServiceLegalPolicy(app, expedition.ProductCode)
	if err != nil || policy == nil {
		return false, err
	}
	a, err := (&expedition.Store{DB: app.DB}).Availability(ctx)
	if err != nil || a.Remaining == 0 {
		return false, err
	}
	// Do not announce closed sales or a category with no remaining capacity.
	var open bool
	err = app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM expedition_inventory i WHERE i.sales_open AND i.booking_closes_at>now() AND i.capacity>(SELECT COALESCE(sum(a.quantity),0) FROM expedition_allocations a JOIN expedition_reservations r ON r.id=a.reservation_id WHERE a.berth=i.berth AND ((r.confirmed_at IS NOT NULL AND r.released_at IS NULL) OR (r.state='held' AND r.hold_expires_at>now()))))`).Scan(&open)
	if err != nil || !open {
		return false, err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var person, recipient, key string
	err = tx.QueryRow(ctx, `SELECT w.person_id::text,c.email::text,w.notification_key::text FROM expedition_waitlist w JOIN email_contacts c ON c.id=w.contact_id WHERE w.subscribed AND w.notified_at IS NULL AND c.erased_at IS NULL AND EXISTS(SELECT 1 FROM person_emails pe WHERE pe.person_id=w.person_id AND pe.contact_id=w.contact_id AND pe.verified_at IS NOT NULL) ORDER BY w.created_at FOR UPDATE OF w SKIP LOCKED LIMIT 1`).Scan(&person, &recipient, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	base := strings.TrimRight(app.Env.GetURI(), "/")
	checkout := base + "/expeditions/antarctica/checkout"
	manage := base + "/expeditions/antarctica/waitlist"
	text := fmt.Sprintf("A spot has opened for bitcoin++ Antarctica, January 24–February 5, 2028.\n\nCheck current availability: %s\n\nThis is an availability alert, not a reservation or a guaranteed place. Spots may be booked before you return. This is the one-time alert you requested; you have not been added to the newsletter.\n\nManage your waitlist request: %s\n", checkout, manage)
	body := `<h1>A spot has opened for Antarctica</h1><p>January 24–February 5, 2028.</p><p><a href="` + html.EscapeString(checkout) + `">Check current availability</a></p><p>This is an availability alert, not a reservation or a guaranteed place. Spots may be booked before you return.</p><p>This is the one-time alert you requested. You have not been added to the newsletter.</p><p><a href="` + html.EscapeString(manage) + `">Manage your waitlist request</a></p>`
	if err = send(&emails.Mail{JobKey: "antarctica-waitlist:" + key, Email: recipient, Title: "A spot has opened for bitcoin++ Antarctica", SendAt: time.Now(), HTMLBody: []byte(body), TextBody: []byte(text)}); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `UPDATE expedition_waitlist SET notified_at=now() WHERE person_id=$1`, person); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

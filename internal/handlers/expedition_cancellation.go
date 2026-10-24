package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
)

type expeditionAdminBooking struct {
	ID, Email, State string
	Quantity         int
}
type expeditionCancellationPage struct {
	Bookings                                               []expeditionAdminBooking
	Booking                                                *getters.ExpeditionCancellation
	CSRF, Query, Flash, Paid, Deposit, Refunded, RefundDue string
	CanCancel                                              bool
}

func AdminExpeditionBookings(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	actor := requireGlobalAdmin(w, r, app)
	if actor == nil {
		return
	}
	page := expeditionCancellationPage{}
	id := mux.Vars(r)["booking"]
	if id == "" {
		page.Query = strings.TrimSpace(r.URL.Query().Get("email"))
		if len(page.Query) > 254 {
			http.Error(w, "Email is too long", 400)
			return
		}
		rows, err := app.DB.Query(r.Context(), `SELECT r.id::text,coalesce((SELECT e.email::text FROM person_emails e WHERE e.person_id::text=r.buyer_id ORDER BY e.is_primary DESC,e.email LIMIT 1),r.buyer_id),r.state,r.quantity FROM expedition_reservations r WHERE r.confirmed_at IS NOT NULL AND ($1='' OR EXISTS(SELECT 1 FROM person_emails e WHERE e.person_id::text=r.buyer_id AND lower(e.email::text)=lower($1))) ORDER BY r.created_at DESC LIMIT 100`, page.Query)
		if err != nil {
			http.Error(w, "Unable to load bookings", 503)
			return
		}
		for rows.Next() {
			var b expeditionAdminBooking
			if err = rows.Scan(&b.ID, &b.Email, &b.State, &b.Quantity); err != nil {
				rows.Close()
				http.Error(w, "Unable to load bookings", 503)
				return
			}
			page.Bookings = append(page.Bookings, b)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			http.Error(w, "Unable to load bookings", 503)
			return
		}
	} else {
		if _, err := uuid.Parse(id); err != nil {
			http.NotFound(w, r)
			return
		}
		csrf, err := ensureAuthMethodsCSRF(app, r)
		if err != nil {
			http.Error(w, "Unable to prepare cancellation", 503)
			return
		}
		page.CSRF = csrf
		if r.Method == "POST" {
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			if r.ParseForm() != nil || len(r.PostForm["csrf"]) != 1 || !secureTokenEqual(csrf, r.PostForm.Get("csrf")) {
				http.Error(w, "Reload before cancelling", 403)
				return
			}
			if r.PostForm.Get("confirmed") != "yes" {
				http.Error(w, "Confirm cancellation and inventory release", 422)
				return
			}
			err = getters.CancelExpeditionBooking(r.Context(), app, id, actor.PersonID, r.PostForm.Get("snapshot"), r.PostForm.Get("reason"))
			if errors.Is(err, getters.ErrExpeditionCancellation) {
				http.Error(w, err.Error(), 409)
				return
			}
			if err != nil {
				app.Err.Printf("expedition cancellation %s: %v", id, err)
				http.Error(w, "Unable to cancel booking", 503)
				return
			}
			http.Redirect(w, r, "/admin/antarctica/bookings/"+url.PathEscape(id), 303)
			return
		}
		page.Booking, err = getters.ExpeditionCancellationPreview(r.Context(), app, id)
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			app.Err.Printf("expedition cancellation preview: %v", err)
			http.Error(w, "Unable to load booking", 503)
			return
		}
		c := page.Booking
		page.CanCancel = c.State == "confirmed" && !c.Blocked
		page.Paid = antarcticaMoney(c.Paid)
		page.Deposit = antarcticaMoney(c.Deposit)
		page.Refunded = antarcticaMoney(c.Refunded)
		page.RefundDue = antarcticaMoney(c.RefundDue)
	}
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "admin/antarctica-bookings.tmpl", page); err != nil {
		app.Err.Printf("expedition bookings page: %v", err)
		http.Error(w, "Unable to display bookings", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(out.Bytes())
}

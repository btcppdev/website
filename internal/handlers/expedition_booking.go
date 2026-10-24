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
	"btcpp-web/internal/expedition"
	"btcpp-web/internal/servicebilling"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
)

type expeditionCabinView struct {
	Name, Preference string
	Quantity         int
}
type expeditionPaymentView struct {
	PayURL                         string
	Number                         int
	Date, Amount, Status, OrderURL string
	Legal                          *servicebilling.LegalAcceptance
}
type expeditionBookingPage struct {
	Terminal                                                   bool
	TerminalMessage                                            string
	Preview                                                    bool
	ID, State, Total, Paid, Remaining, NextDue, Plan, Provider string
	Quantity                                                   int
	Cabins                                                     []expeditionCabinView
	Payments                                                   []expeditionPaymentView
	Review                                                     bool
}

func expeditionBookingPresentation(b *getters.ExpeditionBooking, now time.Time) expeditionBookingPage {
	p := expeditionBookingPage{ID: b.ID, Quantity: b.Quantity, Total: antarcticaMoney(b.Schedule.TotalCents), Plan: fmt.Sprintf("%d installments", len(b.Payments)), Provider: "Card via Stripe"}
	if b.Plan == expedition.Full {
		p.Plan = "Full payment at signup"
	}
	if b.Provider == expedition.Bitcoin {
		p.Provider = "Bitcoin"
	}
	switch b.State {
	case "confirmed":
		p.State = "Reservation confirmed"
	case "abandoned":
		p.State = "Reservation abandoned — payments forfeited"
		p.Terminal = true
		p.TerminalMessage = "This reservation was abandoned after an installment remained unpaid for 30 days. The berths have been released and prior payments retained under the accepted booking terms. This is distinct from a guest-requested cancellation. Contact inbox@btcpp.dev with questions."
	case "cancelled":
		p.State = "Reservation cancelled"
		p.Terminal = true
		p.TerminalMessage = "This reservation has been cancelled. See the agreed cancellation settlement and contact inbox@btcpp.dev with questions."
	case "held":
		p.State = "Berths held — awaiting payment"
		if !b.HoldExpiresAt.After(now) {
			p.State = "Hold expired"
		}
	case "expired":
		p.State = "Hold expired"
	default:
		p.State = "Reservation needs review"
		p.Review = true
	}
	names := map[string]string{"shared": "Two-person cabin", "triple": "Three-person cabin", "quad": "Four-person cabin"}
	selections := b.Schedule.Selections
	if len(selections) == 0 && b.Schedule.Berth != "" {
		selections = []expedition.CabinSelection{{Berth: b.Schedule.Berth, Quantity: b.Quantity, Preference: "coed"}}
	}
	for _, a := range selections {
		preference := "Co-ed sharing"
		if (a.Berth == "shared" && a.Quantity == 2) || (a.Berth == "triple" && a.Quantity == 3) || (a.Berth == "quad" && a.Quantity == 4) {
			preference = "Filled by your group"
		} else if a.Berth == "shared" {
			preference = map[string]string{"male": "M roommate requested", "female": "F roommate requested", "coed": "Co-ed roommate requested"}[a.Preference]
		}
		p.Cabins = append(p.Cabins, expeditionCabinView{names[a.Berth], preference, a.Quantity})
	}
	var paid int64
	for _, i := range b.Payments {
		v := expeditionPaymentView{Number: i.Number, Date: i.DueAt.UTC().Format("January 2, 2006"), Amount: antarcticaMoney(i.Cents), Status: "Scheduled", Legal: i.Legal}
		if i.Number == 1 {
			v.Date = "At booking"
		}
		if !i.DueAt.After(now) {
			v.Status = "Due"
		}
		if i.OrderPublicID != "" {
			v.OrderURL = "/services/orders/" + url.PathEscape(i.OrderPublicID)
		}
		if i.Applied {
			paid += i.Cents
			v.Status = "Payment applied"
		} else if i.OrderStatus == "paid" {
			v.Status = "Payment received — confirmation pending"
		}
		if i.RefundedCents > 0 || i.OrderStatus == "refunded" || i.OrderStatus == "partially_refunded" {
			v.Status = "Refund recorded — booking needs review"
			p.Review = true
		}
		if !i.Applied && p.NextDue == "" {
			if b.State == "confirmed" && !p.Review && i.Number > 1 && !now.Before(i.DueAt.AddDate(0, 0, -14)) {
				v.PayURL = "/expeditions/bookings/" + url.PathEscape(b.ID) + "/installments/" + fmt.Sprint(i.Number)
			}
			p.NextDue = v.Date + " · " + v.Amount
		}
		p.Payments = append(p.Payments, v)
	}
	p.Paid = antarcticaMoney(paid)
	p.Remaining = antarcticaMoney(b.Schedule.TotalCents - paid)
	if p.Terminal {
		p.NextDue = "No further payments accepted"
	}
	if p.NextDue == "" {
		p.NextDue = "No remaining scheduled payments"
	}
	return p
}

func ExpeditionBooking(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	identity, err := auth.Resolve(r, app)
	if err != nil {
		http.Error(w, "Unable to verify your account", 503)
		return
	}
	if identity == nil || identity.PersonID == "" {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	b, err := getters.ExpeditionBookingForPerson(r.Context(), app, identity.PersonID, mux.Vars(r)["booking"])
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		app.Err.Printf("expedition booking: %v", err)
		http.Error(w, "Unable to load this booking", 503)
		return
	}
	page := expeditionBookingPresentation(b, time.Now())
	if b.State == "cancelled" {
		cancellation, err := getters.ExpeditionCancellationPreview(r.Context(), app, b.ID)
		if err != nil {
			app.Err.Printf("booking cancellation summary: %v", err)
			http.Error(w, "Unable to load cancellation details", 503)
			return
		}
		if cancellation.CancelledAt != nil {
			page.TerminalMessage = "This booking has been cancelled and its berths released. No further installments are due. Refund still due: " + antarcticaMoney(cancellation.RefundDue) + " USD. Cancellation does not send a refund; completed refunds appear on your payment receipts. Contact inbox@btcpp.dev for help."
		}
	}
	renderExpeditionBooking(w, app, page)
}
func renderExpeditionBooking(w http.ResponseWriter, app *config.AppContext, p expeditionBookingPage) {
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "conf/antarctica-booking.tmpl", p); err != nil {
		app.Err.Printf("expedition booking render: %v", err)
		http.Error(w, "Unable to show this booking", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(out.Bytes())
}

// Demonstration only: never persisted or exposed as a real receipt.
func sampleExpeditionBooking() expeditionBookingPage {
	booked := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	quote, _ := expedition.QuoteParty(3, []expedition.CabinSelection{{Berth: "shared", Quantity: 2, Preference: "coed"}, {Berth: "shared", Quantity: 1, Preference: "female"}}, expedition.Installments, expedition.Stripe, booked)
	b := &getters.ExpeditionBooking{ID: "SAMPLE — not a booking", State: "confirmed", Quantity: 3, Plan: expedition.Installments, Provider: expedition.Stripe, Schedule: expedition.Schedule{Selections: quote.Selections, TotalCents: quote.TotalCents}}
	for _, i := range quote.Installments {
		b.Payments = append(b.Payments, getters.ExpeditionBookingPayment{Number: i.Number, DueAt: i.DueAt, Cents: i.Cents, Applied: i.Number == 1})
	}
	b.Payments[0].Legal = &servicebilling.LegalAcceptance{Version: "SAMPLE — not accepted terms", AcceptedAt: booked, TermsText: "Illustrative saved terms only. A real booking shows the exact approved terms accepted for that purchase, including its cancellation policy and payment schedule.", PrivacyText: "Illustrative privacy snapshot only. No customer data was collected for this sample."}
	p := expeditionBookingPresentation(b, booked)
	p.Preview = true
	return p
}

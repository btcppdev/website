package emails

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/expedition"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func antarcticaMailFixture(number int) (*getters.ServiceOrderView, *getters.ExpeditionBooking) {
	raw, _ := json.Marshal(expedition.Fulfillment{Version: 1, ReservationID: "booking-fixture", Installment: number})
	o := &getters.ServiceOrderView{PublicID: "order-fixture", Status: "paid", Currency: "USD", PaymentVerified: true, TotalCents: 334000, Quote: getters.ServiceQuote{ProductCode: expedition.ProductCode, ResourceID: "booking-fixture", Quantity: 1, Fulfillment: raw}}
	b := &getters.ExpeditionBooking{ID: "booking-fixture", State: "confirmed", Quantity: 1, Plan: expedition.Installments, Payments: []getters.ExpeditionBookingPayment{{Number: number, Applied: true, OrderPublicID: o.PublicID, OrderStatus: "paid", Cents: o.TotalCents}}}
	return o, b
}
func TestAntarcticaBookingMail(t *testing.T) {
	o, b := antarcticaMailFixture(1)
	m, err := AntarcticaBookingMail(o, b, "passenger@example.test", "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	if m.JobKey != "service-receipt:order-fixture" || m.Sub != "" || m.Missive != "" || m.ReplyTo != "inbox@btcpp.dev" {
		t.Fatal("transactional identity changed")
	}
	for _, body := range []string{string(m.HTMLBody), string(m.TextBody)} {
		for _, want := range []string{"January 23, 2028", "January 24–February 5, 2028", "February 6, 2028", "February 5", "100,000", "packing list", "additional fee", "inbox@btcpp.dev", "remaining installments", "https://btcpp.dev/expeditions/bookings/booking-fixture", "https://btcpp.dev/dashboard/orders"} {
			if !strings.Contains(body, want) {
				t.Errorf("missing %s", want)
			}
		}
		if strings.Contains(body, "paid in full") || strings.Contains(body, "Ortelius") {
			t.Fatal("incorrect confirmation content")
		}
	}
	if !strings.Contains(string(m.HTMLBody), "https://btcpp.dev/static/img/antarctica/leading.png") {
		t.Fatal("missing absolute image")
	}
	b.Plan = expedition.Full
	full, err := AntarcticaBookingMail(o, b, "passenger@example.test", "https://btcpp.dev")
	if err != nil || !strings.Contains(string(full.TextBody), "paid in full") {
		t.Fatal("full payment", err)
	}
	o, b = antarcticaMailFixture(2)
	later, err := AntarcticaBookingMail(o, b, "passenger@example.test", "https://btcpp.dev")
	if err != nil || strings.Contains(string(later.TextBody), "You’re going to Antarctica") || strings.Contains(string(later.HTMLBody), "leading.png") {
		t.Fatal("welcome repeated on installment", err)
	}
}
func TestAntarcticaBookingMailRejectsUnconfirmed(t *testing.T) {
	for _, change := range []func(*getters.ServiceOrderView, *getters.ExpeditionBooking){
		func(o *getters.ServiceOrderView, b *getters.ExpeditionBooking) { o.PaymentVerified = false },
		func(o *getters.ServiceOrderView, b *getters.ExpeditionBooking) { b.State = "held" },
		func(o *getters.ServiceOrderView, b *getters.ExpeditionBooking) { b.State = "review" },
		func(o *getters.ServiceOrderView, b *getters.ExpeditionBooking) { b.Payments[0].Applied = false },
		func(o *getters.ServiceOrderView, b *getters.ExpeditionBooking) { b.Payments[0].RefundedCents = 1 },
		func(o *getters.ServiceOrderView, b *getters.ExpeditionBooking) { b.Payments[0].OrderPublicID = "other" },
		func(o *getters.ServiceOrderView, b *getters.ExpeditionBooking) { b.ID = "other" },
	} {
		o, b := antarcticaMailFixture(1)
		change(o, b)
		if _, err := AntarcticaBookingMail(o, b, "passenger@example.test", "https://btcpp.dev"); err == nil {
			t.Fatal("unsafe confirmation accepted")
		}
	}
}

// Explicit local artifact preview; never sends mail.
func TestAntarcticaBookingEmailPreview(t *testing.T) {
	path := os.Getenv("ANTARCTICA_EMAIL_PREVIEW_PATH")
	if path == "" {
		t.Skip("local email preview")
	}
	o, b := antarcticaMailFixture(1)
	m, err := AntarcticaBookingMail(o, b, "preview@example.test", "http://127.0.0.1:8110")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, m.HTMLBody, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAntarcticaWelcomeCalendar(t *testing.T) {
	for _, plan := range []string{expedition.Full, expedition.Installments} {
		o, b := antarcticaMailFixture(1)
		b.Plan = plan
		m, err := AntarcticaBookingMail(o, b, "passenger@example.test", "https://btcpp.dev")
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Files) != 1 {
			t.Fatal("welcome must include one calendar attachment")
		}
		f := m.Files[0]
		if f.Name != "bitcoin-plus-plus-antarctica-2028.ics" || f.ContentType != "text/calendar; charset=utf-8; method=PUBLISH" {
			t.Fatal("incorrect attachment metadata")
		}
		got := strings.ReplaceAll(string(f.payload()), "\r\n ", "")
		for _, want := range []string{"UID:antarctica-2028@btcpp.dev", "METHOD:PUBLISH", "DTSTART;VALUE=DATE:20280124", "DTEND;VALUE=DATE:20280206", "January 23", "February 6", "inbox@btcpp.dev"} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q", want)
			}
		}
		if strings.Contains(got, "passenger@example.test") || strings.Contains(got, b.ID) {
			t.Fatal("calendar should be safe to share with fellow passengers")
		}
		o, b = antarcticaMailFixture(2)
		later, err := AntarcticaBookingMail(o, b, "passenger@example.test", "https://btcpp.dev")
		if err != nil || len(later.Files) != 0 {
			t.Fatal("calendar repeated on installment", err)
		}
	}
}

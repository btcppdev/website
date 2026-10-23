package handlers

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/expedition"
)

func TestExpeditionBookingPresentation(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	b := &getters.ExpeditionBooking{ID: "example", State: "confirmed", Quantity: 1, Plan: expedition.Installments, Schedule: expedition.Schedule{TotalCents: 10000, Selections: []expedition.CabinSelection{{Berth: "shared", Quantity: 1, Preference: "female"}}}, Payments: []getters.ExpeditionBookingPayment{{Number: 1, Cents: 2000, Applied: true}, {Number: 2, Cents: 8000, DueAt: now.AddDate(0, 1, 0)}}}
	p := expeditionBookingPresentation(b, now)
	if p.Paid != "$20" || p.Remaining != "$80" || p.NextDue != "November 23, 2026 · $80" || p.Cabins[0].Preference != "F roommate requested" {
		t.Fatal(p)
	}
	b.Payments[0].OrderStatus = "refunded"
	if !expeditionBookingPresentation(b, now).Review {
		t.Fatal("refund not flagged for review")
	}
	b.State = "held"
	b.HoldExpiresAt = now.Add(-time.Second)
	if expeditionBookingPresentation(b, now).State != "Hold expired" {
		t.Fatal("expired hold presented as active")
	}
}
func TestExpeditionBookingAndTermsTemplates(t *testing.T) {
	app := antarcticaTestTemplates(t)
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "conf/antarctica-booking.tmpl", sampleExpeditionBooking()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Sample booking", "Two-person cabin", "$50,100", "$10,020", "$40,080", "Traveler names have not been assigned"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal("missing", want)
		}
	}
	out.Reset()
	page := newAntarcticaPage(nil, true)
	if err := app.TemplateCache.ExecuteTemplate(&out, "conf/antarctica-checkout.tmpl", page); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "draft_terms_reviewed") || strings.Contains(out.String(), `name="terms_version"`) || !strings.Contains(out.String(), "Booking information updated October 24, 2026") {
		t.Fatal("draft masquerades as approved terms")
	}
	page.Policy = &getters.ServiceLegalPolicy{Version: "reviewed-v1", TermsText: "Approved <text>", EffectiveAt: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}
	out.Reset()
	if err := app.TemplateCache.ExecuteTemplate(&out, "conf/antarctica-checkout.tmpl", page); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name="terms_version" value="reviewed-v1"`, `name="terms_accepted" value="yes" required`, "Effective October 23, 2026"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal("missing", want)
		}
	}
	if strings.Contains(out.String(), `value="yes" required checked`) {
		t.Fatal("terms preselected")
	}
	out.Reset()
	if err := app.TemplateCache.ExecuteTemplate(&out, "conf/antarctica-terms.tmpl", page); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Approved &lt;text&gt;") {
		t.Fatal("unsafe terms")
	}
}

func TestExpeditionPurchaseSummary(t *testing.T) {
	app := antarcticaTestTemplates(t)
	sample := sampleExpeditionBooking()
	sample.ID = "sample-booking"
	var out bytes.Buffer
	data := accountPurchasesPage{Expeditions: []expeditionBookingPage{sample}}
	if err := app.TemplateCache.ExecuteTemplate(&out, "dashboard_purchases.tmpl", data); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, want := range []string{"Your expedition", "3 passengers", "$10,020 USD", "$40,080 USD", "Manage booking →", `href="/expeditions/bookings/sample-booking"`} {
		if !strings.Contains(body, want) {
			t.Fatal("missing summary detail", want)
		}
	}
	if strings.Index(body, "Your expedition") > strings.Index(body, `aria-label="Purchase history"`) {
		t.Fatal("summary below history")
	}
	out.Reset()
	if err := app.TemplateCache.ExecuteTemplate(&out, "dashboard_purchases.tmpl", accountPurchasesPage{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Manage booking →") {
		t.Fatal("empty account shows booking")
	}
	sample.Terminal = true
	sample.TerminalMessage = "Berths released and payments retained."
	sample.NextDue = "January 1, 2027"
	data.Expeditions = []expeditionBookingPage{sample}
	out.Reset()
	if err := app.TemplateCache.ExecuteTemplate(&out, "dashboard_purchases.tmpl", data); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Next payment:") || !strings.Contains(out.String(), "Unpaid passage at closure") {
		t.Fatal("closed booking requests payment")
	}
}

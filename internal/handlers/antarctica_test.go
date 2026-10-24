package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/emaildirectory"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/expedition"
	"bytes"
	"github.com/alexedwards/scs/v2"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestAntarcticaFares(t *testing.T) {
	page := newAntarcticaPageAt(nil, true, time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))
	want := [][2]string{
		{"$16,700", "$3,340"},
		{"$15,800", "$3,160"},
		{"$14,900", "$2,980"},
	}
	wantFull := [][2]string{{"$15,865", "$835"}, {"$15,010", "$790"}, {"$14,155", "$745"}}
	for i, berth := range page.Berths {
		if full := [2]string{berth.PayInFull, berth.PayInFullSavings}; full != wantFull[i] {
			t.Errorf("%s pay in full: got %v, want %v", berth.ID, full, wantFull[i])
		}
		got := [2]string{berth.Price, berth.Deposit}
		if got != want[i] {
			t.Errorf("%s: got %v, want %v", berth.ID, got, want[i])
		}
	}
}

func antarcticaTestTemplates(t *testing.T) *config.AppContext {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	app := &config.AppContext{Env: &types.EnvConfig{}, Err: log.New(os.Stderr, "", 0)}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestAntarcticaTemplate(t *testing.T) {
	app := antarcticaTestTemplates(t)
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "conf/antarctica.tmpl", newAntarcticaPageAt(nil, true, time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"Early access", "Reservations are now open", "$16,700", "$15,800", "$14,900", "https://btcpp.dev/static/img/antarctica/social-card.png", "summary_large_image", "January 1, 2027", "April 1, 2027", "July 1, 2027", "October 1, 2027", "Pay in full with bitcoin", "Standard passage fare"} {
		if !strings.Contains(out.String(), required) {
			t.Errorf("missing %q", required)
		}
	}
	for _, removed := range []string{"bitcoin-explorer", "Stay in bitcoin", "December 1, 2026", "data-held", "Settle before sailing", "Reservations opening soon", "Booking information preview", "Review draft terms", "Reservations remain closed"} {
		if strings.Contains(out.String(), removed) {
			t.Errorf("removed payment plan still present: %q", removed)
		}
	}
}

// Explicit opt-in, loopback-only design preview. No database or payment provider.
func TestAntarcticaCheckout(t *testing.T) {
	app := antarcticaTestTemplates(t)
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "conf/antarctica-checkout.tmpl", newAntarcticaPage(nil, true)); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"Number of spots to reserve", "cabin-allocations", "Roommate", "Payment unavailable in this demonstration"} {
		if !strings.Contains(strings.ToLower(out.String()), strings.ToLower(required)) {
			t.Errorf("missing checkout content %q", required)
		}
	}
	if strings.Contains(out.String(), `newsletter-opt-in`) {
		t.Fatal("newsletter still present")
	}
}

// Explicit opt-in, loopback-only design preview. No database or payment provider.
func TestAntarcticaDemo(t *testing.T) {
	if os.Getenv("ANTARCTICA_DEMO") != "1" {
		t.Skip("local expedition design preview")
	}
	app := antarcticaTestTemplates(t)
	app.Session = scs.New()
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/antarctica/bookings/sample", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Layout demonstration only", 405)
			return
		}
		page := expeditionCancellationPage{Booking: &getters.ExpeditionCancellation{ID: "SAMPLE", State: "confirmed", Provider: "stripe", Quantity: 1, Eligible: true, Terms: expedition.BookingTerms, PolicyVersion: "Sample terms — not a booking", Orders: []getters.ExpeditionCancellationOrder{{PublicID: "SAMPLE", Provider: "stripe", Reference: "Sample payment"}}}, CanCancel: true, Paid: "$0.50", Deposit: "$0.10", RefundDue: "$0.40", Refunded: "$0"}
		if err := app.TemplateCache.ExecuteTemplate(w, "admin/antarctica-bookings.tmpl", page); err != nil {
			t.Log(err)
		}
	})
	mux.HandleFunc("/admin/antarctica", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Local layout demonstration only", http.StatusMethodNotAllowed)
			return
		}
		page := antarcticaAdminPage{Version: "antarctica-20261024-120000", Effective: "2026-10-24T12:00:00Z", Terms: expedition.BookingTerms, Privacy: expedition.BookingPrivacy, Flash: "Layout demonstration — no settings are changed.", Blockers: []string{"Enable the production worker setting SERVICE_FULFILLMENT_ENABLED.", "Publish the expedition booking terms and privacy policy below."}}
		if err := app.TemplateCache.ExecuteTemplate(w, "admin/antarctica.tmpl", page); err != nil {
			t.Log(err)
		}
	})
	mux.HandleFunc("/expeditions/antarctica/billing-preview", func(w http.ResponseWriter, r *http.Request) {
		booking := sampleExpeditionBooking()
		booking.ID = "sample-booking"
		booking.Provider = "Bitcoin"
		var out bytes.Buffer
		if err := app.TemplateCache.ExecuteTemplate(&out, "dashboard_purchases.tmpl", accountPurchasesPage{Expeditions: []expeditionBookingPage{booking}}); err != nil {
			http.Error(w, "Unable to render preview", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(bytes.Replace(out.Bytes(), []byte(`<main class="profile-edit-shell person-emails-page">`), []byte(`<main class="profile-edit-shell person-emails-page"><p><strong>Sample account · Illustrative data only · No booking created</strong></p>`), 1))
	})
	mux.HandleFunc("/expeditions/bookings/sample-booking", func(w http.ResponseWriter, r *http.Request) {
		booking := sampleExpeditionBooking()
		booking.Provider = "Bitcoin"
		renderExpeditionBooking(w, app, booking)
	})
	mux.HandleFunc("/expeditions/antarctica/installment-preview", func(w http.ResponseWriter, r *http.Request) {
		due := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
		deadline, _ := expedition.InstallmentDeadline(due, "America/Chicago")
		offset, err := strconv.Atoi(r.URL.Query().Get("day"))
		if err != nil {
			offset = -14
		}
		job := &getters.ExpeditionLifecycleMailJob{EventID: "sample-event", Audience: "customer", Recipient: "guest@example.test", Kind: "reminder", Snapshot: getters.ExpeditionLifecycleSnapshot{BookingID: "sample-booking", Recipient: "guest@example.test", Name: "Sample traveler", Policy: "preview-policy", Number: 2, Quantity: 1, Offset: offset, Due: due, Deadline: deadline, ForfeitAt: deadline.AddDate(0, 0, 30), Zone: "America/Chicago", Cents: 334000, RetainedCents: 334000, Payments: []byte(`[{"order":"sample-order","total_cents":334000,"provider":"opennode"}]`)}}
		if offset == 30 {
			job.Kind = "abandoned"
			job.Snapshot.OccurredAt = job.Snapshot.ForfeitAt
		}
		if r.URL.Query().Get("staff") == "1" {
			job.Kind = "abandoned"
			job.Audience = "staff"
			job.Recipient = "inbox@btcpp.dev"
			job.Snapshot.OccurredAt = job.Snapshot.ForfeitAt
		}
		message, err := emails.AntarcticaInstallmentMail(job, "http://127.0.0.1:8110")
		if err != nil {
			http.Error(w, "Unable to render preview", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<div style="padding:16px;text-align:center;background:white;font:16px/2 Arial">Sample emails — nothing is sent.<br><a href="?day=-14">Two weeks before</a> · <a href="?day=-4">Four days before</a> · <a href="?day=0">Due today</a> · <a href="?day=2">Two days overdue</a> · <a href="?day=28">28 days overdue</a> · <a href="?day=30">Forfeited</a> · <a href="?day=30&staff=1">Staff notice</a></div>`))
		w.Write(message.HTMLBody)
	})
	mux.HandleFunc("/expeditions/antarctica/confirmation-preview", func(w http.ResponseWriter, r *http.Request) {
		order := &getters.ServiceOrderView{PublicID: "sample-order", Status: "paid", Currency: "USD", PaymentVerified: true, TotalCents: 334000, Quote: getters.ServiceQuote{ProductCode: expedition.ProductCode, ResourceID: "sample-booking", Quantity: 1, Fulfillment: []byte(`{"version":1,"reservation_id":"sample-booking","installment":1}`)}}
		booking := &getters.ExpeditionBooking{ID: "sample-booking", State: "confirmed", Quantity: 1, Plan: expedition.Installments, Payments: []getters.ExpeditionBookingPayment{{Number: 1, Applied: true, OrderPublicID: "sample-order", OrderStatus: "paid", Cents: 334000}}}
		message, err := emails.AntarcticaBookingMail(order, booking, "preview@example.test", "http://127.0.0.1:8110")
		if err != nil {
			http.Error(w, "Unable to render preview", 500)
			return
		}
		if r.URL.Query().Get("calendar") == "1" {
			file := message.Files[0]
			w.Header().Set("Content-Type", file.ContentType)
			w.Header().Set("Content-Disposition", `attachment; filename="`+file.Name+`"`)
			w.Write(file.Bytes)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<div style="padding:12px;text-align:center;background:#063747;color:white">Sample email preview — no booking or email has been created.</div>`))
		w.Write(message.HTMLBody)
	})
	mux.HandleFunc("/expeditions/antarctica/waitlist", func(w http.ResponseWriter, r *http.Request) { antarcticaDemoWaitlist(w, r, app) })
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/expeditions/antarctica/terms", func(w http.ResponseWriter, r *http.Request) { AntarcticaTerms(w, r, app) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("booking") == "1" {
			renderExpeditionBooking(w, app, sampleExpeditionBooking())
			return
		}
		if r.URL.Query().Get("checkout") == "1" {
			page := newAntarcticaPage(nil, true)
			if r.URL.Query().Get("soldout") == "1" {
				page.RemainingBerths = 0
			}
			renderAntarcticaCheckout(w, app, page)
			return
		}
		renderAntarcticaPage(w, app, newAntarcticaPage(nil, true))
	})
	t.Log("Antarctica preview: http://127.0.0.1:8110")
	t.Fatal(http.ListenAndServe("127.0.0.1:8110", app.Session.LoadAndSave(mux)))
}

// This route exists only in the opt-in, database-free design demo. Session state
// lets the preview exercise the full interaction without claiming a real signup.
func antarcticaDemoWaitlist(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "no-store")
	csrf, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare preview.", 500)
		return
	}
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil || !secureTokenEqual(csrf, r.PostForm.Get("csrf")) {
			http.Error(w, "Reload the preview and try again.", 403)
			return
		}
		switch r.PostForm.Get("action") {
		case "join":
			email, err := emaildirectory.NormalizeEmail(r.PostForm.Get("email"))
			if err != nil {
				http.Error(w, "Enter a valid email address.", 400)
				return
			}
			app.Session.Put(r.Context(), "antarctica-demo-waitlist-email", email)
			app.Session.Put(r.Context(), "antarctica-demo-waitlist-joined", true)
		case "leave":
			app.Session.Put(r.Context(), "antarctica-demo-waitlist-joined", false)
		default:
			http.Error(w, "Invalid preview action.", 400)
			return
		}
		http.Redirect(w, r, "/expeditions/antarctica/waitlist", 303)
		return
	}
	page := antarcticaWaitlistPage{Preview: true, CSRF: csrf, Email: app.Session.GetString(r.Context(), "antarctica-demo-waitlist-email"), Joined: app.Session.GetBool(r.Context(), "antarctica-demo-waitlist-joined")}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := app.TemplateCache.ExecuteTemplate(w, "conf/antarctica-waitlist.tmpl", page); err != nil {
		app.Err.Printf("waitlist preview: %v", err)
	}
}

func TestAntarcticaPublishedCancellationTerms(t *testing.T) {
	app := antarcticaTestTemplates(t)
	w := httptest.NewRecorder()
	AntarcticaTerms(w, httptest.NewRequest("GET", "/expeditions/antarctica/terms", nil), app)
	for _, required := range []string{"before 12:00 a.m. on October 1, 2027", "America/Chicago", "initial 20% deposit", "at or after that cutoff", "day each payment was received", "exchange rate when the refund is sent"} {
		if !strings.Contains(w.Body.String(), required) {
			t.Errorf("missing cancellation term %q", required)
		}
	}
}

func TestAntarcticaDirectLanding(t *testing.T) {
	app := antarcticaTestTemplates(t)
	w := httptest.NewRecorder()
	AntarcticaLanding(w, httptest.NewRequest("GET", "/antarctica", nil), app)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `href="/expeditions/antarctica/checkout"`) || !strings.Contains(w.Body.String(), `property="og:image"`) {
		t.Fatal("direct landing missing checkout or sharing metadata")
	}
}

func TestAntarcticaLateBookingDisplay(t *testing.T) {
	for _, tc := range []struct {
		month time.Month
		count int
		due   string
	}{
		{1, 4, "$4,175"}, {4, 3, "$5,566.66"}, {7, 2, "$8,350"}, {10, 1, "$16,700"},
	} {
		page := newAntarcticaPageAt(nil, true, time.Date(2027, tc.month, 2, 12, 0, 0, 0, time.UTC))
		if page.PaymentCount != tc.count || page.Berths[0].Deposit != tc.due || page.InstallmentsAvailable != (tc.count > 1) {
			t.Fatalf("month %d: %+v", tc.month, page)
		}
	}
}

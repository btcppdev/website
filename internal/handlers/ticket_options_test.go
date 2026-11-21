package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/stripe/stripe-go/v86"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/eventplan"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func TestEvergreenCheckoutOffersAndDiscountPolicy(t *testing.T) {
	app := wizardTestApp(t)
	tag := fmt.Sprintf("checkout-options-%d", time.Now().UnixNano())
	id, err := getters.CreatePlannedEvent(app, eventplan.Basics{Tag: tag, Name: "Cryptography edition", Edition: "global", Timezone: "UTC"}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.DB.Exec(context.Background(), `DELETE FROM conferences WHERE id=$1`, id) })
	conf, _ := getters.GetConfByTag(app, tag)
	restrictions := "Proof of Eurocrypt ticket will be requested at the door. <Bring ID>"
	settings := eventplan.TicketSettings{Tickets: []eventplan.Ticket{
		{Tier: "Standard", Price: 200, Capacity: 100, Currency: "EUR", SalesEnd: "2099-02-01T10:00", TaxCode: "txcd_20030000", CardSurchargeBPS: 1000},
		{Tier: "Student", Price: 50, Capacity: 2, Currency: "EUR", TaxCode: "txcd_20030000", CardSurchargeBPS: 1000, Evergreen: true, TicketKind: "student"},
	}, Offers: []eventplan.CheckoutOffer{{Label: "Eurocrypt ticket holder", Restrictions: restrictions, Percent: 15, Enabled: true}}}
	raw, _ := json.Marshal(settings)
	if _, err = getters.SaveEventPlanning(app, conf, 0, "", "tickets", raw); err != nil {
		t.Fatal(err)
	}
	conf, _ = getters.GetConfByTag(app, tag)
	var standard, student *types.ConfTicket
	for _, ticket := range conf.Tickets {
		if ticket.Evergreen {
			student = ticket
		} else {
			standard = ticket
		}
	}
	offers, _ := getters.ListCheckoutOffers(app, conf.Ref, true)
	offer := offers[0]
	request := func(ticket *types.ConfTicket, path, method string, values url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/tix/"+ticket.ID+path, strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = mux.SetURLVars(req, map[string]string{"tix": ticket.ID})
		w := httptest.NewRecorder()
		app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if path == "/apply-discount" {
				HandleDiscount(w, r, app)
			} else {
				HandleCheckout(w, r, app)
			}
		})).ServeHTTP(w, req)
		return w
	}
	page := request(standard, "/checkout", "GET", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "<strong>Eurocrypt ticket holder</strong>") || !strings.Contains(page.Body.String(), `type="checkbox" name="OfferCode"`) {
		t.Fatalf("standard page: %d %s", page.Code, page.Body.String())
	}
	if !strings.Contains(page.Body.String(), "<small>"+html.EscapeString(restrictions)+"</small>") {
		t.Fatal("saved offer restrictions missing or not escaped")
	}
	studentPage := request(student, "/checkout", "GET", nil)
	if studentPage.Code != 200 || !strings.Contains(studentPage.Body.String(), `name="ticket_`+student.ID+`" value="1"`) || !strings.Contains(studentPage.Body.String(), `name="OfferCode"`) {
		t.Fatalf("student discount UI: %d", studentPage.Code)
	}
	values := url.Values{"OfferCode": {offer.Code}, "Count": {"1"}, "DiscountPrice": {"200"}}
	discounted := request(standard, "/apply-discount", "POST", values)
	if discounted.Code != 200 || !strings.Contains(discounted.Body.String(), `name="DiscountPrice" value="170"`) || !strings.Contains(discounted.Body.String(), offer.Label) {
		t.Fatalf("checkbox quote: %d %s", discounted.Code, discounted.Body.String())
	}
	values.Set("Email", "test@example.test")
	values.Set("DiscountPrice", "42")
	values.Set("HMAC", calcTixHMAC(app, conf, 50, 42, offer.Code))
	forged := request(student, "/checkout", "POST", values)
	if forged.Code != http.StatusUnprocessableEntity {
		t.Fatalf("student accepted forged offer: %d %s", forged.Code, forged.Body.String())
	}
	values.Set("Discount", "ANOTHER")
	combined := request(standard, "/checkout", "POST", values)
	if combined.Code != http.StatusUnprocessableEntity {
		t.Fatal("stacked discounts accepted")
	}
	values.Del("Discount")
	values.Set("OfferCode", "not-an-offer")
	invalid := request(standard, "/checkout", "POST", values)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatal("unknown offer accepted")
	}
	values.Set("OfferCode", offer.Code)
	values.Set("DiscountPrice", "170")
	values.Set("HMAC", calcTixHMAC(app, conf, 200, 170, offer.Code))
	if _, err = app.DB.Exec(context.Background(), `UPDATE discounts SET amount=20,discount_expr='%20' WHERE id=$1`, offer.ID); err != nil {
		t.Fatal(err)
	}
	stale := request(standard, "/checkout", "POST", values)
	if stale.Code != 200 || !strings.Contains(stale.Body.String(), `name="DiscountPrice" value="160"`) {
		t.Fatalf("stale offer not refreshed: %d %s", stale.Code, stale.Body.String())
	}
	var held int
	if err = app.DB.QueryRow(context.Background(), `SELECT count(*) FROM ticket_reservations WHERE conference_id=$1`, conf.Ref).Scan(&held); err != nil || held != 0 {
		t.Fatal("invalid checkout reserved inventory", held, err)
	}
	// Exercise both hosted payment requests against a local provider, including
	// metadata, idempotency and provider-confirmed expiration.
	var mu sync.Mutex
	metadata := map[string]map[string]string{}
	var expired, rejected atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if rejected.Load() && r.Method == "POST" {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"message":"Rejected test checkout","type":"invalid_request_error"}}`)
			return
		}
		if r.Method == "POST" && r.URL.Path == "/charges" {
			var body types.OpenNodeRequest
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
			if body.Metadata.ReservationID == "" || body.OrderID != body.Metadata.ReservationID || body.Metadata.TicketID != standard.ID || body.Metadata.DiscountRef != offer.ID || body.Amount != 160 {
				t.Error("OpenNode payment metadata/amount", body)
			}
			mu.Lock()
			metadata["on_test"] = map[string]string{"ticket-reservation-id": body.Metadata.ReservationID}
			mu.Unlock()
			fmt.Fprint(w, `{"data":{"id":"on_test","hosted_checkout_url":"https://payments.example.test/bitcoin"}}`)
			return
		}
		if r.Method == "POST" {
			if e := r.ParseForm(); e != nil {
				t.Error(e)
			}
			id := r.Form.Get("metadata[ticket-reservation-id]")
			if id == "" || r.Header.Get("Idempotency-Key") != id || r.Form.Get("metadata[tix-id]") != standard.ID || r.Form.Get("metadata[discount-ref]") != offer.ID || r.Form.Get("line_items[0][price_data][unit_amount]") != "17600" {
				t.Error("Stripe payment metadata/amount", r.Form)
			}
			mu.Lock()
			metadata["cs_test"] = map[string]string{"ticket-reservation-id": id}
			mu.Unlock()
			fmt.Fprint(w, `{"id":"cs_test","url":"https://payments.example.test/card"}`)
			return
		}
		status := "paid"
		if expired.Load() {
			status = "expired"
		}
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(r.URL.Path, "cs_test") {
			paymentStatus := "paid"
			if expired.Load() {
				paymentStatus = "unpaid"
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "cs_test", "status": status, "payment_status": paymentStatus, "metadata": metadata["cs_test"]})
		} else {
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": "on_test", "status": status, "metadata": metadata["on_test"]}})
		}
	}))
	defer provider.Close()
	original, key := stripe.GetBackend(stripe.APIBackend), stripe.Key
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(provider.URL), HTTPClient: provider.Client()}))
	stripe.Key = "sk_test_local"
	defer func() { stripe.SetBackend(stripe.APIBackend, original); stripe.Key = key }()
	app.Env.OpenNode.Endpoint = provider.URL
	app.Env.OpenNode.Key = "local-test"
	values.Set("DiscountPrice", "160")
	values.Set("HMAC", calcTixHMAC(app, conf, 200, 160, offer.Code))
	for _, method := range []string{"btc", "card"} {
		values.Set("PaymentMethod", method)
		paid := request(standard, "/checkout", "POST", values)
		if paid.Code != 303 {
			t.Fatalf("%s provider redirect: %d %s", method, paid.Code, paid.Body.String())
		}
	}
	ageHolds := func() {
		t.Helper()
		if _, e := app.DB.Exec(context.Background(), `UPDATE ticket_reservations SET updated_at=now()-interval '36 minutes' WHERE conference_id=$1`, conf.Ref); e != nil {
			t.Fatal(e)
		}
	}
	ageHolds()
	reconcileTicketReservations(app)
	inv, e := getters.TicketInventory(app, conf.Ref)
	if e != nil || inv.Held != 2 {
		t.Fatal("paid but unfulfilled checkouts were released", inv, e)
	}
	expired.Store(true)
	ageHolds()
	reconcileTicketReservations(app)
	inv, e = getters.TicketInventory(app, conf.Ref)
	if e != nil || inv.Held != 0 {
		t.Fatal("expired checkouts retained inventory", inv, e)
	}
	rejected.Store(true)
	for _, method := range []string{"btc", "card"} {
		values.Set("PaymentMethod", method)
		failed := request(standard, "/checkout", "POST", values)
		if failed.Code != 500 {
			t.Fatalf("%s rejection: %d", method, failed.Code)
		}
	}
	inv, e = getters.TicketInventory(app, conf.Ref)
	if e != nil || inv.Held != 0 {
		t.Fatal("rejected payment retained inventory", inv, e)
	}
	// A legacy URL must not revive a paused independent student allocation.
	if _, e = app.DB.Exec(context.Background(), `UPDATE conference_tickets SET disabled=true WHERE id=$1`, student.ID); e != nil {
		t.Fatal(e)
	}
	_, mapped, _, _, e := determineTixPrice(app, standard.ID+"+student")
	if e != nil || mapped.ID != student.ID {
		t.Fatal("legacy student URL bypassed its allocation", e)
	}

}

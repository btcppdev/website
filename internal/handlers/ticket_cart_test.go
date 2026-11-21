package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/eventplan"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
	"github.com/stripe/stripe-go/v86"
)

func TestTicketCartQuantityValidation(t *testing.T) {
	for _, values := range []url.Values{
		{}, {"ticket_a": {"-1"}}, {"ticket_a": {"1.5"}}, {"ticket_a": {"9"}}, {"ticket_a": {"1", "2"}}, {"ticket_a": {"5"}, "ticket_b": {"4"}},
	} {
		if _, err := ticketCartQuantities(values); err == nil {
			t.Errorf("accepted %v", values)
		}
	}
	got, err := ticketCartQuantities(url.Values{"ticket_a": {"2"}, "ticket_b": {"0"}, "ticket_c": {"3"}})
	if err != nil || len(got) != 2 || got["a"] != 2 || got["c"] != 3 {
		t.Fatal(got, err)
	}
}

func TestTicketCartCheckoutAndFulfillment(t *testing.T) {
	app := wizardTestApp(t)
	id, err := getters.CreatePlannedEvent(app, eventplan.Basics{Tag: fmt.Sprintf("cart-%d", time.Now().UnixNano()), Name: "Cart test", Edition: "global", Timezone: "UTC"}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.DB.Exec(context.Background(), `DELETE FROM conferences WHERE id=$1`, id) })
	conf, _ := getters.GetConfByRef(app, id)
	raw, _ := json.Marshal(eventplan.TicketSettings{Tickets: []eventplan.Ticket{
		{Tier: "Standard", Price: 200, Capacity: 100, Currency: "EUR", TaxCode: "txcd_20030000", SalesEnd: "2099-01-01T10:00", CardSurchargeBPS: 1000},
		{Tier: "Student", Price: 50, Capacity: 3, Currency: "EUR", TaxCode: "txcd_20030000", Evergreen: true, TicketKind: "student", CardSurchargeBPS: 1000},
		{Tier: "Supporter", Price: 500, Capacity: 10, Currency: "EUR", TaxCode: "txcd_20030000", Evergreen: true, TicketKind: "supporter", AllowDiscounts: true, CardSurchargeBPS: 1000},
	}, Offers: []eventplan.CheckoutOffer{{Label: "Eurocrypt ticket holder", Percent: 15, Enabled: true}}})
	if _, err = getters.SaveEventPlanning(app, conf, 0, "", "tickets", raw); err != nil {
		t.Fatal(err)
	}
	conf, _ = getters.GetConfByRef(app, id)
	tickets := map[string]*types.ConfTicket{}
	for _, ticket := range conf.Tickets {
		tickets[ticket.Tier] = ticket
	}
	offers, _ := getters.ListCheckoutOffers(app, id, true)
	selected := tickets["Student"]
	var loginCookie *http.Cookie
	request := func(path, method string, values url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/tix/"+selected.ID+path, strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if loginCookie != nil {
			req.AddCookie(loginCookie)
		}
		req = mux.SetURLVars(req, map[string]string{"tix": selected.ID})
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
	hidden := func(body, name string) string {
		t.Helper()
		m := regexp.MustCompile(`name="` + name + `" value="([^"]*)"`).FindStringSubmatch(body)
		if len(m) != 2 {
			t.Fatalf("missing %s: %s", name, body)
		}
		return html.UnescapeString(m[1])
	}
	page := request("/checkout", "GET", nil)
	if page.Code != 200 {
		t.Fatalf("render %d: %s", page.Code, page.Body.String())
	}
	if !strings.Contains(page.Body.String(), `type="email" id="Email"`) {
		t.Fatal("guest email field missing")
	}
	for name, ticket := range tickets {
		want := "0"
		if name == "Student" {
			want = "1"
		}
		if got := hidden(page.Body.String(), "ticket_"+ticket.ID); got != want {
			t.Fatalf("%s prefill %s", name, got)
		}
	}
	if !strings.Contains(page.Body.String(), `name="Subscribe" checked`) || !strings.Contains(page.Body.String(), `</strong> — $0</span>`) {
		t.Fatal("free checked newsletter missing")
	}
	if strings.Index(page.Body.String(), "checkout-cart-tabs--ticket") > strings.Index(page.Body.String(), `id="Discount"`) {
		t.Fatal("ticket selection appears after discount")
	}
	values := url.Values{"TicketCart": {"1"}, "Email": {"buyer@example.test"}, "PaymentMethod": {"btc"}, "OfferCode": {offers[0].Code}, "ticket_" + tickets["Standard"].ID: {"2"}, "ticket_" + selected.ID: {"1"}, "ticket_" + tickets["Supporter"].ID: {"1"}}
	quote := request("/apply-discount", "POST", values)
	if quote.Code != 200 {
		t.Fatal(quote.Code, quote.Body.String())
	}
	values.Set("CartQuote", hidden(quote.Body.String(), "CartQuote"))
	values.Set("CartHMAC", hidden(quote.Body.String(), "CartHMAC"))
	var form types.TixForm
	form.PaymentMethod = "btc"
	form.OfferCode = offers[0].Code
	lines, err := validateTicketCart(app, conf, selected, selected.ID, values, &form)
	if err != nil {
		t.Fatal(err)
	}
	var total uint
	for _, line := range lines {
		total += line.UnitCents * line.Quantity
		if line.Kind == "student" && (line.UnitCents != 5000 || line.DiscountRef != "") {
			t.Fatal("student discounted", line)
		}
	}
	if total != 81500 || form.Count != 4 {
		t.Fatal("mixed quote", total, form.Count)
	}
	for name, mutate := range map[string]func(url.Values){
		"forged signature": func(v url.Values) { v.Set("CartHMAC", "bad") },
		"unknown option":   func(v url.Values) { v.Set("ticket_fake", "1") },
		"over cap":         func(v url.Values) { v.Set("ticket_"+selected.ID, "4") },
		"too many":         func(v url.Values) { v.Set("ticket_"+tickets["Standard"].ID, "8") },
		"changed offer":    func(v url.Values) { v.Set("OfferCode", "fake") },
		"stacked code":     func(v url.Values) { v.Set("Discount", "OTHER") },
	} {
		t.Run(name, func(t *testing.T) {
			v := url.Values{}
			for k, vs := range values {
				v[k] = append([]string{}, vs...)
			}
			mutate(v)
			w := request("/checkout", "POST", v)
			if w.Code != 422 {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
		})
	}
	var held int
	if err = app.DB.QueryRow(context.Background(), `SELECT count(*) FROM ticket_reservations WHERE conference_id=$1`, id).Scan(&held); err != nil || held != 0 {
		t.Fatal("invalid carts held inventory", held, err)
	}
	buyerEmail := "cart-buyer-" + id + "@example.test"
	buyer, err := getters.CreateSpeaker(app, getters.SpeakerInput{Name: "Ticket buyer", Email: buyerEmail})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.DB.Exec(context.Background(), `DELETE FROM conferences WHERE id=$1`, id)
		app.DB.Exec(context.Background(), `DELETE FROM people WHERE id=$1`, buyer)
	})
	login := httptest.NewRecorder()
	app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := auth.LoginPerson(app, r, buyer, auth.MethodPasskey); err != nil {
			t.Fatal(err)
		}
	})).ServeHTTP(login, httptest.NewRequest("GET", "/test-login", nil))
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == app.Session.Cookie.Name {
			loginCookie = cookie
		}
	}
	if loginCookie == nil {
		t.Fatal("login did not create a session")
	}
	signedIn := request("/checkout", "GET", nil)
	body := signedIn.Body.String()
	if signedIn.Code != 200 || strings.Contains(body, `type="email"`) || !strings.Contains(body, "Tickets will be sent to") || !strings.Contains(body, "<strong>"+buyerEmail+"</strong>") || hidden(body, "Email") != buyerEmail {
		t.Fatalf("signed-in delivery UI: %d %s", signedIn.Code, body)
	}
	if signedIn.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("personalized checkout can be cached")
	}
	var reservationID, paymentID string
	var paidCents int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/charges" {
			var body types.OpenNodeRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Metadata.Email != buyerEmail || body.CustomerEmail != buyerEmail {
				t.Error("OpenNode did not use account delivery email")
			}
			reservationID = body.Metadata.ReservationID
			paymentID = "on_cart"
			paidCents = int64(body.Amount*100 + .5)
			if body.Amount != 815 || body.Metadata.Quantity != 4 || body.Metadata.TicketTotalCents != 81500 || body.OrderID != reservationID {
				t.Errorf("OpenNode cart: %+v", body)
			}
			fmt.Fprint(w, `{"data":{"id":"on_cart","hosted_checkout_url":"https://payments.example.test/bitcoin"}}`)
		} else {
			r.ParseForm()
			if r.Form.Get("customer_email") != buyerEmail {
				t.Error("Stripe did not use account delivery email")
			}
			reservationID = r.Form.Get("metadata[ticket-reservation-id]")
			paymentID = "cs_cart"
			paidCents = 0
			if r.Header.Get("Idempotency-Key") != reservationID {
				t.Error("missing payment idempotency")
			}
			for i := 0; i < 3; i++ {
				prefix := fmt.Sprintf("line_items[%d]", i)
				price, _ := strconv.ParseInt(r.Form.Get(prefix+"[price_data][unit_amount]"), 10, 64)
				qty, _ := strconv.ParseInt(r.Form.Get(prefix+"[quantity]"), 10, 64)
				paidCents += price * qty
			}
			if paidCents != 89700 {
				t.Errorf("Stripe subtotal %d", paidCents)
			}
			fmt.Fprint(w, `{"id":"cs_cart","url":"https://payments.example.test/card"}`)
		}
	}))
	defer provider.Close()
	original, key := stripe.GetBackend(stripe.APIBackend), stripe.Key
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(provider.URL), HTTPClient: provider.Client()}))
	stripe.Key = "sk_test_local"
	defer func() { stripe.SetBackend(stripe.APIBackend, original); stripe.Key = key }()
	app.Env.OpenNode.Endpoint = provider.URL
	app.Env.OpenNode.Key = "local-test"
	for _, method := range []string{"btc", "card"} {
		values.Set("PaymentMethod", method)
		// A missing or modified hidden email must not change delivery.
		values.Del("Email")
		if method == "card" {
			values.Set("Email", "different@example.test")
		}
		result := request("/checkout", "POST", values)
		if result.Code != 303 {
			t.Fatalf("%s checkout %d: %s", method, result.Code, result.Body.String())
		}
		inv, err := getters.TicketInventory(app, id)
		if err != nil || inv.Held != 4 {
			t.Fatal("mixed reservation", inv, err)
		}
		src := "opennode"
		if method == "card" {
			src = "stripe"
		}
		entry := &types.Entry{ID: paymentID, ConfRef: id, ReservationID: reservationID, Email: buyerEmail, Currency: "EUR", Created: time.Now(), Total: paidCents - 1, Items: make([]types.Item, 4)}
		if err = getters.AddTickets(app, entry, src); err == nil {
			t.Fatal("underpaid mixed order fulfilled")
		}
		entry.Total = paidCents
		entry.Currency = "USD"
		if err = getters.AddTickets(app, entry, src); err == nil {
			t.Fatal("wrong currency fulfilled")
		}
		entry.Currency = "EUR"
		if err = getters.AddTickets(app, entry, src); err != nil {
			t.Fatal(err)
		}
		if !entry.Cart || len(entry.Items) != 4 {
			t.Fatal("mixed fulfillment lost", entry)
		}
		kinds := map[string]int{}
		for _, item := range entry.Items {
			kinds[item.Type]++
			if item.TicketID == selected.ID && item.DiscountRef != "" {
				t.Fatal("student discount credited")
			}
		}
		if kinds["general"] != 2 || kinds["student"] != 1 || kinds["supporter"] != 1 {
			t.Fatal(kinds)
		}
		if err = getters.AddTickets(app, entry, src); err != nil || !entry.AlreadyFulfilled {
			t.Fatal("replay", err)
		}
	}
	inv, err := getters.TicketInventory(app, id)
	if err != nil || inv.Sold != 8 || inv.Held != 0 || inv.ByTicket[selected.ID] != 2 {
		t.Fatal("settled inventory", inv, err)
	}
	// Only one cart may take the final student seat. Losing carts must not
	// leave behind a reservation for the standard ticket in the same order.
	counts := url.Values{"ticket_" + tickets["Standard"].ID: {"1"}, "ticket_" + selected.ID: {"1"}, "CartQuote": {values.Get("CartQuote")}, "CartHMAC": {values.Get("CartHMAC")}}
	cartForm := &types.TixForm{OfferCode: offers[0].Code, PaymentMethod: "btc"}
	counts.Set("OfferCode", offers[0].Code)
	lastLines, err := validateTicketCart(app, conf, selected, selected.ID, counts, cartForm)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan string, 8)
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := getters.ReserveTicketCart(app, conf, lastLines, "last@example.test", "opennode")
			if err == nil {
				results <- id
			}
		}()
	}
	wg.Wait()
	close(results)
	var ids []string
	for id := range results {
		ids = append(ids, id)
	}
	if len(ids) != 1 {
		t.Fatalf("last seat reserved by %d carts", len(ids))
	}
	inv, err = getters.TicketInventory(app, conf.Ref)
	if err != nil || inv.Held != 2 || inv.ByTicket[selected.ID] != 3 {
		t.Fatal("partial or duplicate reservation", inv, err)
	}
	if err = getters.ReleaseTicketReservation(app, ids[0], "opennode", ""); err != nil {
		t.Fatal(err)
	}
	inv, err = getters.TicketInventory(app, conf.Ref)
	if err != nil || inv.Held != 0 || inv.ByTicket[selected.ID] != 2 {
		t.Fatal("cart cancellation did not release all types", inv, err)
	}
	// Repricing invalidates a signed quote even if an old amount was submitted.
	if _, err = app.DB.Exec(context.Background(), `UPDATE conference_tickets SET base_price=250 WHERE id=$1`, tickets["Standard"].ID); err != nil {
		t.Fatal(err)
	}
	updated, _ := getters.GetConfByRef(app, id)
	if _, err = validateTicketCart(app, updated, selected, selected.ID, counts, cartForm); err == nil {
		t.Fatal("stale cart price accepted")
	}
	legacyID, err := getters.ReserveTickets(app, updated, selected, 1, "legacy@example.test", "stripe")
	if err != nil {
		t.Fatal(err)
	}
	legacy := &types.Entry{ID: "cs_legacy", ConfRef: id, TicketID: selected.ID, ReservationID: legacyID, Email: "legacy@example.test", Currency: "EUR", Created: time.Now(), Total: 5500, Items: []types.Item{{Total: 5500, Type: "student", Desc: "Student"}}}
	if err = getters.AddTickets(app, legacy, "stripe"); err != nil {
		t.Fatal("legacy reservation fulfillment", err)
	}
	inv, err = getters.TicketInventory(app, id)
	if err != nil || inv.Sold != 9 || inv.Held != 0 || inv.ByTicket[selected.ID] != 3 {
		t.Fatal("legacy ticket attribution", inv, err)
	}

}

func TestTicketCartMerchReceiptTotals(t *testing.T) {
	conf := &types.Conf{Ref: "event", Desc: "Cryptography"}
	ticket := &types.ConfTicket{ID: "standard", CardSurchargeBPS: 1000}
	form := &types.TixForm{Count: 4, CartLines: []types.TicketCartLine{
		{TicketID: "standard", Title: "Standard", Kind: "general", Quantity: 2, UnitCents: 18700},
		{TicketID: "student", Title: "Student", Kind: "student", Quantity: 1, UnitCents: 5500},
		{TicketID: "supporter", Title: "Supporter", Kind: "supporter", Quantity: 1, UnitCents: 46800},
	}}
	items, total := ticketShopOrderLines(conf, ticket, form, "general", "card")
	if total != 89700 || len(items) != 3 || items[0].LineTotalCents != 37400 || items[1].SKUSnapshot != "student" || items[1].UnitPriceCents != 5500 || items[2].VariantLabelSnapshot != "supporter" {
		t.Fatal("mixed merch receipt lost ticket lines", total, items)
	}
	legacy, total := ticketShopOrderLines(conf, ticket, &types.TixForm{Count: 2, DiscountPrice: 170}, "general", "card")
	if total != 37400 || len(legacy) != 1 {
		t.Fatal("legacy merch receipt changed", total, legacy)
	}
}

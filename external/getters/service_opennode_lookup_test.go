package getters

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOpenNodeOriginalAmount(t *testing.T) {
	for _, amount := range []string{"12.34", "12.3400"} {
		if !(OpenNodeServiceCharge{Currency: "USD", SourceFiatValue: json.Number(amount)}).matchesUSD(1234) {
			t.Fatal(amount)
		}
	}
	for _, amount := range []string{"", "12.339999999999999999", "12.340000000000000001", "12.35", "-12.34", "1234/100", "1.234e1", "NaN"} {
		if (OpenNodeServiceCharge{Currency: "USD", SourceFiatValue: json.Number(amount)}).matchesUSD(1234) {
			t.Fatal("accepted", amount)
		}
	}
	if (OpenNodeServiceCharge{Currency: "EUR", SourceFiatValue: "12.34"}).matchesUSD(1234) {
		t.Fatal("currency mismatch")
	}
}

func TestOpenNodeServiceLookupPostgres(t *testing.T) {
	for _, product := range []string{"alerts.repository-slots", "insider.membership.annual"} {
		for _, closed := range []bool{false, true} {
			t.Run(product+"/"+map[bool]string{false: "active", true: "sales-stopped"}[closed], func(t *testing.T) {
				testOpenNodeServiceLookup(t, product, closed)
			})
		}
	}
}

func testOpenNodeServiceLookup(t *testing.T, product string, closed bool) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Local charge lookup test') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "lookup-"+person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	starts := time.Now().UTC().Truncate(time.Microsecond)
	ends := starts.AddDate(1, 0, 0)
	quote := ServiceQuote{ProductCode: product, ResourceID: "lookup-test", Name: "Service item", Provider: "opennode", Quantity: 1, UnitCents: 1234, TotalCents: 1234, PeriodStart: &starts, PeriodEnd: &ends, ExpiresAt: time.Now().Add(time.Hour)}
	purchase, err := CreateServicePurchase(app, person, "lookup-purchase-test", quote)
	if err != nil {
		t.Fatal(err)
	}
	view, err := ServiceOrderForPerson(ctx, app, person, purchase.PublicID)
	if err != nil || view.PaymentVerified || view.Status != "pending" {
		t.Fatal("pending order view", err)
	}
	if _, err := ServiceOrderForPerson(ctx, app, uuid.NewString(), purchase.PublicID); err == nil {
		t.Fatal("cross-account order disclosure")
	}
	attempt, err := BeginServicePayment(app, person, purchase.OrderID)
	if err != nil || !attempt.CanCreateCharge {
		t.Fatal("creation claim", err)
	}
	chargeID := uuid.NewString()
	status, currency, amount, orderID, responseID := "paid", "USD", json.Number("12.34"), attempt.ID, chargeID
	redirect := false
	price := int64(25000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v2/charge/"+chargeID || r.Header.Get("Authorization") != "local-lookup-key" {
			t.Error("incorrect lookup request")
		}
		if redirect {
			w.Header().Set("Location", "/must-not-follow")
			w.WriteHeader(302)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": responseID, "order_id": orderID, "status": status, "currency": currency, "source_fiat_value": amount, "fiat_value": 999, "price": price, "fee": 250, "missing_amt": 0, "overpaid_by": 50}})
	}))
	defer server.Close()
	app.Env.OpenNode.Endpoint, app.Env.OpenNode.Key = server.URL+"/v1", "local-lookup-key"
	for _, bad := range []string{"amount", "currency", "order", "id", "redirect"} {
		switch bad {
		case "amount":
			amount = "12.339999999999999999"
		case "currency":
			currency = "EUR"
		case "order":
			orderID = uuid.NewString()
		case "id":
			responseID = uuid.NewString()
		case "redirect":
			redirect = true
		}
		if _, err := lookupOpenNodeServiceCharge(ctx, app, chargeID, server.Client()); err == nil {
			t.Fatal("accepted", bad)
		}
		var bound string
		if err := app.DB.QueryRow(ctx, `SELECT coalesce(provider_reference,'') FROM service_payment_attempts WHERE id=$1`, attempt.ID).Scan(&bound); err != nil || bound != "" {
			t.Fatal("invalid lookup bound reference", err)
		}
		amount, currency, orderID, responseID, redirect = "12.34", "USD", attempt.ID, chargeID, false
	}
	for _, current := range []string{"unpaid", "processing", "paid", "paid"} {
		status = current
		charge, err := lookupOpenNodeServiceCharge(ctx, app, chargeID, server.Client())
		if err != nil || charge.Status != current || charge.AttemptID != attempt.ID {
			t.Fatal("lookup", err)
		}
	}
	var orderStatus string
	if err := app.DB.QueryRow(ctx, `SELECT status FROM shop_orders WHERE id=$1`, purchase.OrderID).Scan(&orderStatus); err != nil || orderStatus != "pending" {
		t.Fatal("lookup granted settlement", err)
	}
	var bound string
	if err := app.DB.QueryRow(ctx, `SELECT provider_reference FROM service_payment_attempts WHERE id=$1`, attempt.ID).Scan(&bound); err != nil || bound != chargeID {
		t.Fatal("uncertain creation not reconciled", err)
	}
	if closed {
		if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := app.DB.Exec(ctx, `TRUNCATE service_closure_plans,service_sales_closures`); err != nil {
				t.Error(err)
			}
		})
		service := "alerts"
		if product == "insider.membership.annual" {
			service = "insider"
		}
		if err := StopServiceSales(ctx, app, person, service, "Permanent closure fixture"); err != nil {
			t.Fatal(err)
		}
		if link, err := openNodeServiceCheckout(ctx, app, person, purchase.OrderID, server.Client()); !errors.Is(err, ErrServiceSalesClosed) || link != "" {
			t.Fatal("closed service exposed checkout", link, err)
		}
	}
	// Expiry does not erase an issued on-chain address. A later authenticated
	// paid observation must still enter accounting after the sales stop.
	for _, pending := range []string{"unpaid", "processing", "underpaid", "expired"} {
		status = pending
		if paid, err := reconcileOpenNodeServiceCharge(ctx, app, chargeID, server.Client()); err != nil || paid {
			t.Fatal("non-final status settled", pending, err)
		}
	}
	status = "paid"
	price = 0
	if paid, err := reconcileOpenNodeServiceCharge(ctx, app, chargeID, server.Client()); err == nil || paid {
		t.Fatal("missing Bitcoin price accepted", err)
	}
	var rejectedEvents int
	if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_payment_events WHERE attempt_id=$1`, attempt.ID).Scan(&rejectedEvents); err != nil || rejectedEvents != 0 {
		t.Fatal("invalid Bitcoin evidence persisted settlement", rejectedEvents, err)
	}
	price = 25000
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if paid, err := reconcileOpenNodeServiceCharge(ctx, app, chargeID, server.Client()); err != nil || !paid {
				t.Error("settlement replay", err)
			}
		}()
	}
	wg.Wait()
	var evidence OpenNodeBitcoinEvidence
	var evidenceBytes []byte
	if err := app.DB.QueryRow(ctx, `SELECT opennode_bitcoin FROM service_payment_events WHERE attempt_id=$1`, attempt.ID).Scan(&evidenceBytes); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(evidenceBytes, &evidence); err != nil || evidence.PriceSatoshis != 25000 || evidence.FeeSatoshis == nil || *evidence.FeeSatoshis != 250 || evidence.OverpaidSatoshis == nil || *evidence.OverpaidSatoshis != 50 {
		t.Fatal("original Bitcoin evidence lost", evidence, err)
	}
	price++
	if paid, err := reconcileOpenNodeServiceCharge(ctx, app, chargeID, server.Client()); err == nil || paid {
		t.Fatal("changed Bitcoin evidence accepted on replay", err)
	}
	price--
	if _, err := app.DB.Exec(ctx, `UPDATE service_payment_events SET opennode_bitcoin=NULL WHERE attempt_id=$1`, attempt.ID); err == nil {
		t.Fatal("Bitcoin settlement evidence was mutable")
	}
	var events, grants int
	var source string
	var paidAt time.Time
	if err := app.DB.QueryRow(ctx, `SELECT count(*),min(time_source),min(paid_at) FROM service_payment_events WHERE attempt_id=$1`, attempt.ID).Scan(&events, &source, &paidAt); err != nil || events != 1 || source != "first_verified_observation" || paidAt.IsZero() {
		t.Fatal("settlement evidence", events, source, err)
	}
	if err := app.DB.QueryRow(ctx, `SELECT status FROM shop_orders WHERE id=$1`, purchase.OrderID).Scan(&orderStatus); err != nil || orderStatus != "paid" {
		t.Fatal("order not settled", err)
	}
	wantAccess := "active"
	if product == "alerts.repository-slots" {
		if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_entitlements e JOIN service_purchase_requests r ON r.id=e.purchase_id WHERE r.order_id=$1`, purchase.OrderID).Scan(&grants); err != nil || grants != 1 {
			t.Fatal("duplicate or missing grant", grants, err)
		}
		entitlements, err := ActiveServiceEntitlements(app, person, starts.Add(time.Minute))
		if err != nil || len(entitlements) != 1 {
			t.Fatal("missing paid allowance", err)
		}
		grant := entitlements[0]
		if grant.ProductCode != "alerts.repository-slots" || grant.Quantity != 1 || grant.StartsAt == nil || grant.EndsAt == nil || !grant.StartsAt.Equal(starts) || !grant.EndsAt.Equal(ends) {
			t.Fatal("purchased access changed", grant)
		}
		expired, err := ActiveServiceEntitlements(app, person, ends)
		if err != nil || len(expired) != 0 {
			t.Fatal("access extended beyond purchased term", err)
		}
	} else {
		wantAccess = "pending"
		if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_entitlements e JOIN service_purchase_requests r ON r.id=e.purchase_id WHERE r.order_id=$1`, purchase.OrderID).Scan(&grants); err != nil || grants != 0 {
			t.Fatal("Insider payment created local Alerts grant", grants, err)
		}
		var jobs int
		if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_fulfillment_jobs WHERE attempt_id=$1 AND state='pending'`, attempt.ID).Scan(&jobs); err != nil || jobs != 1 {
			t.Fatal("missing or duplicate Insider fulfillment", jobs, err)
		}
		var payment VerifiedServicePayment
		var raw []byte
		if err := app.DB.QueryRow(ctx, `SELECT payment FROM service_fulfillment_jobs WHERE attempt_id=$1`, attempt.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &payment); err != nil || payment.AttemptID != attempt.ID || payment.Reference != chargeID || payment.TotalCents != quote.TotalCents || payment.OpenNodeBitcoin == nil || payment.OpenNodeBitcoin.PriceSatoshis != 25000 {
			t.Fatal("Insider handoff lost original payment", payment, err)
		}
	}

	view, err = ServiceOrderForPerson(ctx, app, person, purchase.PublicID)
	if err != nil || !view.PaymentVerified || view.Status != "paid" || view.AccessState != wantAccess || view.TotalCents != quote.TotalCents {
		t.Fatal("paid order view", view, err)
	}
	if view.Quote.ProductCode != product || view.Quote.PeriodStart == nil || view.Quote.PeriodEnd == nil || !view.Quote.PeriodStart.Equal(starts) || !view.Quote.PeriodEnd.Equal(ends) {
		t.Fatal("original purchased term changed", view.Quote)
	}
}

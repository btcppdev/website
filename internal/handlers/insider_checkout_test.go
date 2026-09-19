package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestResolveInsiderServiceQuoteRejectsBrowserPriceChanges(t *testing.T) {
	now := time.Date(2026, time.September, 14, 12, 0, 0, 0, time.UTC)
	request := insiderCheckoutRequest{
		RequestID: "checkout-request-0001", ProductCode: "insider.article",
		ResourceID: "article-1", ContributionPublicationID: "article-1", PersonID: "person-1",
		AmountCents: 210, Currency: "USD", PaymentProvider: "stripe", QuoteExpiresAt: now.Add(insiderQuoteTTL),
	}
	quote, err := resolveInsiderServiceQuote(request, now)
	if err != nil || quote.TotalCents != 210 || quote.Provider != "stripe" {
		t.Fatalf("valid quote = %#v, %v", quote, err)
	}
	request.AmountCents = 1
	if _, err := resolveInsiderServiceQuote(request, now); err == nil {
		t.Fatal("tampered article amount was accepted")
	}
	request.AmountCents = 210
	request.ResourceID = "another-article"
	if _, err := resolveInsiderServiceQuote(request, now); err == nil {
		t.Fatal("mismatched article was accepted")
	}
	request.ContributionPublicationID = ""
	if quote, err := resolveInsiderServiceQuote(request, now); err != nil || quote.ResourceID != "another-article" {
		t.Fatalf("standalone article quote = %#v, %v", quote, err)
	}
}

func TestResolveInsiderAnnualQuoteFreezesTermAndFulfillment(t *testing.T) {
	now := time.Date(2026, time.September, 14, 12, 0, 0, 0, time.UTC)
	expires := now.Add(insiderQuoteTTL)
	request := insiderCheckoutRequest{
		RequestID: "checkout-request-0002", ProductCode: "insider.membership.annual",
		ContributionPublicationID: "article-1", PersonID: "person-1", AmountCents: 21_000,
		Currency: "usd", PaymentProvider: "opennode", QuoteExpiresAt: expires,
		PublicCredit: json.RawMessage(`{"person_id":"person-1","name":"Alice"}`),
	}
	quote, err := resolveInsiderServiceQuote(request, now)
	if err != nil || quote.PeriodStart == nil || quote.PeriodEnd == nil || quote.Provider != "opennode" {
		t.Fatalf("annual quote = %#v, %v", quote, err)
	}
	if !quote.PeriodStart.Equal(now) || !quote.PeriodEnd.Equal(now.AddDate(1, 0, 0)) {
		t.Fatalf("annual term = %v — %v", quote.PeriodStart, quote.PeriodEnd)
	}
	var fulfillment insiderFulfillmentSnapshot
	if err := json.Unmarshal(quote.Fulfillment, &fulfillment); err != nil || fulfillment.ContributionPublicationID != "article-1" {
		t.Fatalf("fulfillment = %#v, %v", fulfillment, err)
	}
}

func TestAllowedInsiderReturnURLUsesExactConfiguredOrigin(t *testing.T) {
	allowed := "https://insider.btcpp.dev, http://localhost:3001"
	for _, candidate := range []string{"https://insider.btcpp.dev/p/story?paid=1", "http://localhost:3001/account"} {
		if !allowedInsiderReturnURL(candidate, allowed) {
			t.Fatalf("expected %q to be allowed", candidate)
		}
	}
	for _, candidate := range []string{"https://insider.btcpp.dev.attacker.test/p/story", "https://beta.btcpp.dev/p/story", "javascript:alert(1)", "https://user@insider.btcpp.dev/p/story"} {
		if allowedInsiderReturnURL(candidate, allowed) {
			t.Fatalf("expected %q to be rejected", candidate)
		}
	}
}

func TestInsiderSettlementRequiresDurableAcknowledgment(t *testing.T) {
	for _, status := range []int{200, 204, 202, 201, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer receiver.Close()
			app := &config.AppContext{Env: &types.EnvConfig{InsiderFulfillmentEndpoint: receiver.URL, InsiderFulfillmentToken: "test-token"}}
			err := postInsiderSettlement(context.Background(), app, insiderSettlementEvent{EventID: "test"})
			if (err == nil) != (status == 200 || status == 204) {
				t.Fatalf("status %d: %v", status, err)
			}
		})
	}
}

func TestSettleInsiderServicePaymentPostgres(t *testing.T) {
	t.Run("fixed-term", func(t *testing.T) { testSettleInsiderServicePaymentPostgres(t, false) })
	t.Run("recurring-period", func(t *testing.T) { testSettleInsiderServicePaymentPostgres(t, true) })
}

func testSettleInsiderServicePaymentPostgres(t *testing.T, recurring bool) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" || os.Getenv("DATABASE_URL") == "" {
		t.Skip("BTCPP_POSTGRES_SMOKE=1 and DATABASE_URL are required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	const fulfillmentToken = "insider-fulfillment-test-token"
	var received []string
	fulfillment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+fulfillmentToken {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		var event insiderSettlementEvent
		if json.Unmarshal(body, &event) != nil || r.Header.Get("Idempotency-Key") != event.EventID {
			http.Error(w, "invalid event", http.StatusBadRequest)
			return
		}
		received = append(received, string(body))
		if len(received) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer fulfillment.Close()
	app := &config.AppContext{
		DB: pool,
		Env: &types.EnvConfig{
			InsiderFulfillmentEndpoint: fulfillment.URL,
			InsiderFulfillmentToken:    fulfillmentToken,
			InsiderMerchantName:        "Bitcoin++ Test Merchant",
		},
		Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0),
	}

	var personID string
	if err := pool.QueryRow(ctx, `INSERT INTO people(name) VALUES('Insider settlement test') RETURNING id::text`).Scan(&personID); err != nil {
		t.Fatal(err)
	}
	email := "insider-settlement-" + personID + "@example.test"
	if _, err := pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary) VALUES($1,$2,true)`, personID, email); err != nil {
		t.Fatal(err)
	}
	quotedAt := time.Now().UTC().Truncate(time.Microsecond)
	quotedEnd := quotedAt.AddDate(1, 0, 0)
	quote := getters.ServiceQuote{
		RecurringInvoice: recurring,
		ProductCode:      "insider.membership.annual", Name: "Bitcoin++ Insider annual membership", Provider: "stripe",
		Quantity: 1, UnitCents: insiderAnnualMembershipCents, TotalCents: insiderAnnualMembershipCents,
		PeriodStart: &quotedAt, PeriodEnd: &quotedEnd, ExpiresAt: quotedAt.Add(insiderQuoteTTL), Fulfillment: json.RawMessage(`{}`),
	}
	purchase, err := getters.CreateServicePurchase(app, personID, "insider-settlement-"+strconv.FormatInt(time.Now().UnixNano(), 10), quote)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := getters.BeginServicePayment(app, personID, purchase.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	reference := "cs_test_" + strings.ReplaceAll(attempt.ID, "-", "")
	if err := getters.BindServicePayment(app, attempt.ID, "stripe", reference); err != nil {
		t.Fatal(err)
	}
	paidAt := quotedAt.Add(2 * time.Minute)
	payment := getters.VerifiedServicePayment{
		AttemptID: attempt.ID, Provider: "stripe", Reference: reference, EventID: "evt_" + strings.ReplaceAll(attempt.ID, "-", ""),
		Currency: "USD", TotalCents: insiderAnnualMembershipCents, PaidAt: paidAt,
	}
	if err := settleInsiderServicePayment(ctx, app, payment); err == nil {
		t.Fatal("expected unavailable product receiver")
	}
	// A later worker recovers without another provider callback. Configuration
	// changes must not alter the frozen receipt/fulfillment payload on retry.
	claimedAt := time.Now().Add(2 * time.Minute)
	abandoned, err := getters.ClaimServiceFulfillment(ctx, app, attempt.ID, claimedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := getters.ClaimServiceFulfillment(ctx, app, attempt.ID, claimedAt.Add(30*time.Second)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("active fulfillment lease reused", err)
	}
	app.Env.InsiderMerchantName = "Changed configuration"
	if worked, err := processServiceFulfillment(ctx, app, attempt.ID, claimedAt.Add(2*time.Minute)); err != nil || !worked {
		t.Fatal("durable recovery", worked, err)
	}

	if applied, err := getters.FinishServiceFulfillment(ctx, app, abandoned, "retry", time.Now()); err != nil || applied {
		t.Fatal("stale worker changed recovered fulfillment", applied, err)
	}
	if _, err := getters.FulfillServicePurchase(app, purchase.OrderID); !errors.Is(err, getters.ErrServiceFulfillment) {
		t.Fatalf("Insider purchase reached central access grants: %v", err)
	}
	if _, err := getters.ClaimServiceReceipt(ctx, app, purchase.OrderID, time.Now().Add(time.Hour)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("Insider purchase reached Alerts receipt sender: %v", err)
	}
	// Provider retries can observe the same settlement later. The persisted
	// time keeps the outgoing idempotent event byte-for-byte stable.
	payment.PaidAt = paidAt.Add(time.Minute)
	if err := settleInsiderServicePayment(ctx, app, payment); err != nil {
		t.Fatal(err)
	}
	if len(received) != 2 || received[0] != received[1] {
		t.Fatalf("settlement retry was not stable: %d events", len(received))
	}
	var event insiderSettlementEvent
	if err := json.Unmarshal([]byte(received[0]), &event); err != nil {
		t.Fatal(err)
	}
	expectedStart, expectedEnd := paidAt, paidAt.AddDate(1, 0, 0)
	if recurring {
		expectedStart, expectedEnd = quotedAt, quotedEnd
	}
	if !event.OccurredAt.Equal(paidAt) || event.AccessStartsAt == nil || !event.AccessStartsAt.Equal(expectedStart) || event.AccessEndsAt == nil || !event.AccessEndsAt.Equal(expectedEnd) {
		t.Fatalf("settlement dates = %+v", event)
	}
	var status string
	var fulfilled, entitlements int
	if err := pool.QueryRow(ctx, `SELECT o.status,i.fulfilled_quantity,
		(SELECT count(*) FROM service_entitlements e JOIN service_purchase_requests r ON r.id=e.purchase_id WHERE r.order_id=o.id)
		FROM shop_orders o JOIN shop_order_items i ON i.order_id=o.id WHERE o.id=$1`, purchase.OrderID).Scan(&status, &fulfilled, &entitlements); err != nil {
		t.Fatal(err)
	}
	if status != "paid" || fulfilled != 1 || entitlements != 0 {
		t.Fatalf("shared fulfillment status=%s fulfilled=%d local_entitlements=%d", status, fulfilled, entitlements)
	}
	testInsiderRefundDelivery(t, app, attempt.ID, event)
}

package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log"
	"os"
	"testing"
	"time"
)

func TestInsiderGiftQuoteFreezesQuantityAndBuyerAccess(t *testing.T) {
	now := time.Now().UTC()
	base := insiderCheckoutRequest{RequestID: "gift-checkout-request-1", ProductCode: "insider.article.gifts", ResourceID: "article-1", ContributionPublicationID: "article-1", PersonID: "buyer",
		GiftLinkID: "135c1f50-656a-4fd2-8c8a-a4b8e90760da", GiftQuantity: 5, GiftIncludeBuyer: true, AmountCents: 1260, Currency: "USD", PaymentProvider: "stripe", QuoteExpiresAt: now.Add(insiderQuoteTTL)}
	quote, err := resolveInsiderServiceQuote(base, now)
	if err != nil || quote.TotalCents != 1260 || quote.Quantity != 1 || quote.Name != "bitcoin++ Insider article access + 5 gift reads" {
		t.Fatal("gift quote", quote, err)
	}
	var snapshot insiderFulfillmentSnapshot
	if err := json.Unmarshal(quote.Fulfillment, &snapshot); err != nil || snapshot.GiftLinkID != base.GiftLinkID || snapshot.GiftQuantity != 5 || !snapshot.GiftIncludeBuyer || snapshot.ContributionPublicationID != "article-1" {
		t.Fatal("gift fulfillment snapshot", snapshot, err)
	}
	for _, change := range []struct {
		name  string
		apply func(*insiderCheckoutRequest)
	}{
		{"price", func(r *insiderCheckoutRequest) { r.AmountCents = 1 }},
		{"zero", func(r *insiderCheckoutRequest) { r.GiftQuantity = 0 }},
		{"negative", func(r *insiderCheckoutRequest) { r.GiftQuantity = -1 }},
		{"too many", func(r *insiderCheckoutRequest) { r.GiftQuantity = 1001 }},
		{"link", func(r *insiderCheckoutRequest) { r.GiftLinkID = "invalid" }},
		{"wrong contribution", func(r *insiderCheckoutRequest) { r.ContributionPublicationID = "another-article" }},
		{"other product", func(r *insiderCheckoutRequest) { r.ProductCode = "insider.article"; r.AmountCents = 210 }},
		{"public credit", func(r *insiderCheckoutRequest) {
			r.PublicCredit = json.RawMessage(`{"person_id":"buyer","name":"Buyer"}`)
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			r := base
			change.apply(&r)
			if _, err := resolveInsiderServiceQuote(r, now); err == nil {
				t.Fatal("invalid gift quote accepted")
			}
		})
	}
	base.GiftIncludeBuyer = false
	base.AmountCents = 1050
	base.PaymentProvider = "opennode"
	quote, err = resolveInsiderServiceQuote(base, now)
	if err != nil || quote.TotalCents != 1050 || quote.Name != "bitcoin++ Insider · 5 gift reads" {
		t.Fatal("gift-only Bitcoin purchase", quote, err)
	}
}

func TestInsiderGiftRefundPreservesGiftIdentity(t *testing.T) {
	original := insiderSettlementEvent{EventKind: "settled", EventID: "stripe:gift-paid", ProductCode: "insider.article.gifts", ResourceID: "article-1", ContributionPublicationID: "article-1",
		GiftLinkID: "135c1f50-656a-4fd2-8c8a-a4b8e90760da", GiftQuantity: 5, GiftIncludeBuyer: true, PersonID: "buyer", Provider: "stripe", ProviderReference: "cs_gifts", AmountCents: 1260, Currency: "USD"}
	raw, _ := json.Marshal(original)
	job := getters.ServiceRefundJob{AccessAction: "retain", Refund: getters.VerifiedServiceRefund{Provider: "stripe", PaymentReference: "cs_gifts", RefundReference: "re_gifts", AmountCents: 1260, Currency: "USD", RefundedAt: time.Now()}}
	refund, err := buildInsiderRefund(raw, "buyer", &job)
	if err != nil || refund.GiftLinkID != original.GiftLinkID || refund.GiftQuantity != 5 || !refund.GiftIncludeBuyer || refund.ContributionPublicationID != "article-1" || refund.ReversesEventID != original.EventID || refund.AccessAction != "retain" {
		t.Fatal("gift refund", refund, err)
	}
}

func TestInsiderGiftSettlementPostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" || os.Getenv("DATABASE_URL") == "" {
		t.Skip("local billing database is required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app := &config.AppContext{DB: pool, Env: &types.EnvConfig{InsiderMerchantName: "btcplusplus LLC"}, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0)}
	var buyer string
	if err := pool.QueryRow(ctx, `INSERT INTO people(name) VALUES('Gift reader') RETURNING id::text`).Scan(&buyer); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, buyer, "gift-"+buyer+"@example.test"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	request := insiderCheckoutRequest{RequestID: uuid.NewString(), ProductCode: "insider.article.gifts", ResourceID: uuid.NewString(), PersonID: buyer, GiftLinkID: uuid.NewString(), GiftQuantity: 10, GiftIncludeBuyer: true, AmountCents: 2310, Currency: "USD", PaymentProvider: "stripe", QuoteExpiresAt: now.Add(insiderQuoteTTL)}
	request.ContributionPublicationID = request.ResourceID
	quote, err := resolveInsiderServiceQuote(request, now)
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := getters.CreateServicePurchase(app, buyer, request.RequestID, quote)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := getters.CreateServicePurchase(app, buyer, request.RequestID, quote); err != nil || replay.OrderID != purchase.OrderID {
		t.Fatal("checkout replay", replay, err)
	}
	changed := quote
	changed.Fulfillment = json.RawMessage(`{"gift_quantity":25}`)
	if _, err := getters.CreateServicePurchase(app, buyer, request.RequestID, changed); err == nil {
		t.Fatal("gift quantity changed after quote")
	}
	attempt, err := getters.BeginServicePayment(app, buyer, purchase.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	reference := "cs_gift_" + uuid.NewString()
	if err := getters.BindServicePayment(app, attempt.ID, "stripe", reference); err != nil {
		t.Fatal(err)
	}
	payment := getters.VerifiedServicePayment{AttemptID: attempt.ID, Provider: "stripe", Reference: reference, EventID: "evt_gift_" + uuid.NewString(), Currency: "USD", TotalCents: 2310, PaidAt: now}
	if err := getters.RecordServicePayment(app, payment); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		event, err := buildInsiderSettlement(app, payment)
		if err != nil || event.GiftLinkID != request.GiftLinkID || event.GiftQuantity != 10 || !event.GiftIncludeBuyer || event.ResourceID != request.ResourceID || event.ContributionPublicationID != request.ResourceID || event.AmountCents != 2310 || event.PersonID != buyer {
			t.Fatal("gift settlement", event, err)
		}
	}
}

package getters

import (
	"context"
	"errors"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

func refundTaxFixture(t *testing.T, product string, amount int64, original bool) (*config.AppContext, VerifiedServiceRefund, func()) {
	t.Helper()
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Refund tax fixture') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "refund-tax-"+person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(time.Second)
	end := start.AddDate(1, 0, 0)
	expires := start.Add(time.Hour)
	quote := ServiceQuote{ProductCode: product, Name: "Tax refund", Provider: "opennode", Quantity: 1, UnitCents: 1000, TotalCents: 1000, TaxCents: 100, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: expires}
	purchase, err := CreateTaxedServicePurchase(app, person, uuid.NewString(), quote, TaxQuoteInput{Provider: "stripe", ProviderQuoteID: "taxcalc_" + person, SalesTaxAmountCents: 100, DestinationCountry: "US", RawResponse: `{}`, ExpiresAt: &expires})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := BeginServicePayment(app, person, purchase.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	ref := uuid.NewString()
	if err = BindServicePayment(app, attempt.ID, "opennode", ref); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	if err = RecordServicePayment(app, VerifiedServicePayment{AttemptID: attempt.ID, Provider: "opennode", Reference: ref, EventID: "paid:" + ref, Currency: "USD", TotalCents: 1000, PaidAt: start, OpenNodeBitcoin: &OpenNodeBitcoinEvidence{PriceSatoshis: 1000000, OverpaidSatoshis: &zero}}); err != nil {
		t.Fatal(err)
	}
	finalize := func() {
		t.Helper()
		err := finalizeServiceTax(ctx, app, purchase.OrderID, func(params *stripe.TaxTransactionCreateFromCalculationParams) (*stripe.TaxTransaction, error) {
			return &stripe.TaxTransaction{ID: "tax_original_" + person, Reference: *params.Reference, Currency: "usd", Type: "transaction", LineItems: &stripe.TaxTransactionLineItemList{Data: []*stripe.TaxTransactionLineItem{{Amount: 1000, AmountTax: 100, Quantity: 1, Reference: product, TaxBehavior: "inclusive"}}}}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if original {
		finalize()
	}
	sats := amount * 1000
	if sats == 0 {
		sats = 1
	}
	refund := VerifiedServiceRefund{AttemptID: attempt.ID, Provider: "opennode", PaymentReference: ref, RefundReference: uuid.NewString(), AmountCents: amount, AmountSatoshis: &sats, Currency: "USD", RefundedAt: start.Add(time.Second), Reason: "permanent_closure"}
	if err = RecordServiceRefund(ctx, app, refund); err != nil {
		t.Fatal(err)
	}
	// Leave no intentionally pending test job ahead of another shuffled test.
	t.Cleanup(func() {
		app.DB.Exec(context.Background(), `UPDATE service_refund_tax_jobs j SET state='review',review_reason='fixture_complete' FROM service_payment_refunds r WHERE r.provider=j.provider AND r.refund_reference=j.refund_reference AND r.attempt_id=$1 AND j.state='pending'`, attempt.ID)
	})
	return app, refund, finalize
}

func claimRefundTaxFixture(t *testing.T, app *config.AppContext, ref string, now time.Time) *serviceRefundTaxJob {
	t.Helper()
	if _, err := app.DB.Exec(context.Background(), `UPDATE service_refund_tax_jobs SET next_attempt_at='0001-01-01' WHERE refund_reference=$1`, ref); err != nil {
		t.Fatal(err)
	}
	job, err := claimServiceRefundTax(context.Background(), app, now)
	if err != nil || job == nil || job.Reference != ref {
		t.Fatalf("claim: %+v %v", job, err)
	}
	return job
}

func refundTaxResponse(params *stripe.TaxTransactionCreateReversalParams) *stripe.TaxTransaction {
	return &stripe.TaxTransaction{ID: "tax_reversal_" + (*params.Reference)[19:], Reference: *params.Reference, Currency: "usd", Type: "reversal", Reversal: &stripe.TaxTransactionReversal{OriginalTransaction: *params.OriginalTransaction}, LineItems: &stripe.TaxTransactionLineItemList{Data: []*stripe.TaxTransactionLineItem{{Amount: *params.FlatAmount, AmountTax: *params.FlatAmount / 10, TaxBehavior: "inclusive"}}}}
}

func TestServiceRefundTaxRecordingPostgres(t *testing.T) {
	for _, product := range []string{"alerts.repository-slots", "insider.membership.annual"} {
		for _, amount := range []int64{0, 400, 1000} {
			name := map[int64]string{0: "zero", 400: "partial", 1000: "full"}[amount]
			t.Run(product+"/"+name, func(t *testing.T) {
				ctx := context.Background()
				app, refund, _ := refundTaxFixture(t, product, amount, true)
				now := time.Now().UTC().Add(time.Second)
				job := claimRefundTaxFixture(t, app, refund.RefundReference, now)
				calls := 0
				create := func(params *stripe.TaxTransactionCreateReversalParams) (*stripe.TaxTransaction, error) {
					calls++
					if *params.Mode != "partial" || *params.FlatAmount != -amount || *params.IdempotencyKey != refundTaxReference(refund.Provider, refund.RefundReference) || params.Context == nil {
						t.Error("tax request changed refund basis")
					}
					return refundTaxResponse(params), nil
				}
				if err := processServiceRefundTax(ctx, app, job, now, create); err != nil {
					t.Fatal(err)
				}
				var state string
				var gross, tax int64
				var reversal *string
				if err := app.DB.QueryRow(ctx, `SELECT j.state,r.amount_cents,r.tax_cents,r.reversal_transaction FROM service_refund_tax_jobs j JOIN service_refund_tax_records r USING(provider,refund_reference) WHERE j.refund_reference=$1`, refund.RefundReference).Scan(&state, &gross, &tax, &reversal); err != nil || state != "accepted" || gross != amount || tax != amount/10 {
					t.Fatalf("tax record: %s %d %d %v", state, gross, tax, err)
				}
				wantCalls := 1
				if amount == 0 {
					wantCalls = 0
					if reversal != nil {
						t.Fatal("zero amount sent to provider")
					}
				}
				if calls != wantCalls {
					t.Fatalf("provider calls %d", calls)
				}
				if err := recordServiceRefundTax(ctx, app, job, nil, now); !errors.Is(err, ErrServiceRefundTax) {
					t.Fatalf("stale lease reused: %v", err)
				}
				if _, err := app.DB.Exec(ctx, `UPDATE service_refund_tax_records SET tax_cents=0 WHERE refund_reference=$1`, refund.RefundReference); err == nil {
					t.Fatal("tax record mutable")
				}
				if _, err := app.DB.Exec(ctx, `UPDATE service_refund_tax_jobs SET first_attempt_at=first_attempt_at+interval '1 day' WHERE refund_reference=$1`, refund.RefundReference); err == nil {
					t.Fatal("submission clock reset")
				}
			})
		}
	}
}

func TestServiceRefundTaxLostAcknowledgmentPostgres(t *testing.T) {
	ctx := context.Background()
	app, refund, _ := refundTaxFixture(t, "alerts.repository-slots", 400, true)
	now := time.Now().UTC().Add(time.Second)
	job := claimRefundTaxFixture(t, app, refund.RefundReference, now)
	var accepted *stripe.TaxTransaction
	var key string
	if err := processServiceRefundTax(ctx, app, job, now, func(p *stripe.TaxTransactionCreateReversalParams) (*stripe.TaxTransaction, error) {
		accepted = refundTaxResponse(p)
		key = *p.IdempotencyKey
		return nil, errors.New("lost acknowledgment")
	}); !errors.Is(err, ErrServiceRefundTax) {
		t.Fatal(err)
	}
	var first time.Time
	var count int
	if err := app.DB.QueryRow(ctx, `SELECT first_attempt_at,(SELECT count(*) FROM service_refund_tax_records WHERE refund_reference=$1) FROM service_refund_tax_jobs WHERE refund_reference=$1`, refund.RefundReference).Scan(&first, &count); err != nil || count != 0 {
		t.Fatal("ambiguous call recorded", count, err)
	}
	retry := claimRefundTaxFixture(t, app, refund.RefundReference, now.Add(16*time.Minute))
	if retry.Token == job.Token {
		t.Fatal("lease token reused")
	}
	if err := recordServiceRefundTax(ctx, app, job, accepted, now); !errors.Is(err, ErrServiceRefundTax) {
		t.Fatal("old worker committed", err)
	}
	if err := processServiceRefundTax(ctx, app, retry, now.Add(16*time.Minute), func(p *stripe.TaxTransactionCreateReversalParams) (*stripe.TaxTransaction, error) {
		if *p.IdempotencyKey != key || *p.Reference != accepted.Reference || *p.OriginalTransaction != accepted.Reversal.OriginalTransaction || *p.FlatAmount != -400 {
			t.Error("retry changed provider request")
		}
		return accepted, nil
	}); err != nil {
		t.Fatal(err)
	}
	var replayFirst time.Time
	if err := app.DB.QueryRow(ctx, `SELECT first_attempt_at FROM service_refund_tax_jobs WHERE refund_reference=$1`, refund.RefundReference).Scan(&replayFirst); err != nil || !first.Equal(replayFirst) {
		t.Fatal("first submission moved", err)
	}
}

func TestServiceRefundTaxAmbiguityAndMismatchPostgres(t *testing.T) {
	for _, mode := range []string{"expired", "wrong amount", "wrong original", "wrong tax", "wrong reference"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			app, refund, _ := refundTaxFixture(t, "insider.membership.annual", 400, true)
			now := time.Now().UTC().Add(time.Second)
			job := claimRefundTaxFixture(t, app, refund.RefundReference, now)
			if mode == "expired" {
				if err := processServiceRefundTax(ctx, app, job, now, func(*stripe.TaxTransactionCreateReversalParams) (*stripe.TaxTransaction, error) {
					return nil, errors.New("outage")
				}); err == nil {
					t.Fatal("outage accepted")
				}
				expired := claimRefundTaxFixture(t, app, refund.RefundReference, now.Add(24*time.Hour))
				if !expired.Skip {
					t.Fatal("expired idempotency window resent")
				}
			} else {
				if err := processServiceRefundTax(ctx, app, job, now, func(p *stripe.TaxTransactionCreateReversalParams) (*stripe.TaxTransaction, error) {
					r := refundTaxResponse(p)
					switch mode {
					case "wrong amount":
						r.LineItems.Data[0].Amount++
					case "wrong original":
						r.Reversal.OriginalTransaction = "tax_wrong"
					case "wrong tax":
						r.LineItems.Data[0].AmountTax = -101
					case "wrong reference":
						r.Reference = "wrong"
					}
					return r, nil
				}); !errors.Is(err, ErrServiceRefundTax) {
					t.Fatal("mismatch accepted", err)
				}
			}
			var state string
			var count int
			if err := app.DB.QueryRow(ctx, `SELECT state,(SELECT count(*) FROM service_refund_tax_records WHERE refund_reference=$1) FROM service_refund_tax_jobs WHERE refund_reference=$1`, refund.RefundReference).Scan(&state, &count); err != nil || state != "review" || count != 0 {
				t.Fatalf("unsafe completion: %s %d %v", state, count, err)
			}
		})
	}
}

func TestServiceRefundTaxWaitsForOriginalPostgres(t *testing.T) {
	ctx := context.Background()
	app, refund, finalize := refundTaxFixture(t, "insider.membership.annual", 400, false)
	// The actual selector must not claim a refund before its original tax entry exists.
	var eligible bool
	if err := app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tax_transactions t JOIN service_purchase_requests p ON p.order_id=t.order_id JOIN service_payment_attempts a ON a.purchase_id=p.id WHERE a.id=$1 AND t.provider='stripe' AND t.status='recorded')`, refund.AttemptID).Scan(&eligible); err != nil || eligible {
		t.Fatal("original unexpectedly recorded", err)
	}
	now := time.Now().UTC().Add(time.Second)
	job, err := claimServiceRefundTax(ctx, app, now)
	if err != nil {
		t.Fatal(err)
	}
	if job != nil && job.Reference == refund.RefundReference {
		t.Fatal("claimed before original tax recording")
	}
	finalize()
	job = claimRefundTaxFixture(t, app, refund.RefundReference, now)
	if err := processServiceRefundTax(ctx, app, job, now, func(p *stripe.TaxTransactionCreateReversalParams) (*stripe.TaxTransaction, error) {
		return refundTaxResponse(p), nil
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := ListServiceRefunds(ctx, app, refund.AttemptID)
	if err != nil || len(rows) != 1 {
		t.Fatal("inspection", rows, err)
	}
	if rows[0].TaxState != "accepted" || rows[0].TaxReversalReference == "" || rows[0].TaxReviewReason != "" {
		t.Fatalf("missing tax evidence: %+v", rows[0])
	}
}

func TestServiceRefundTaxOrdersAdjustmentsPostgres(t *testing.T) {
	ctx := context.Background()
	app, first, _ := refundTaxFixture(t, "alerts.repository-slots", 400, true)
	second := first
	second.RefundReference = uuid.NewString()
	second.AmountCents = 600
	sats := int64(600000)
	second.AmountSatoshis = &sats
	if err := RecordServiceRefund(ctx, app, second); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(time.Second)
	// Even with the second job due earlier, the first must be recorded first.
	if _, err := app.DB.Exec(ctx, `UPDATE service_refund_tax_jobs SET next_attempt_at=CASE WHEN refund_reference=$1 THEN '0001-01-01'::timestamptz ELSE '0002-01-01'::timestamptz END WHERE refund_reference IN ($1,$2)`, second.RefundReference, first.RefundReference); err != nil {
		t.Fatal(err)
	}
	job, err := claimServiceRefundTax(ctx, app, now)
	if err != nil || job == nil || job.Reference != first.RefundReference {
		t.Fatalf("out-of-order claim: %+v %v", job, err)
	}
	create := func(p *stripe.TaxTransactionCreateReversalParams) (*stripe.TaxTransaction, error) {
		return refundTaxResponse(p), nil
	}
	if err := processServiceRefundTax(ctx, app, job, now, create); err != nil {
		t.Fatal(err)
	}
	job = claimRefundTaxFixture(t, app, second.RefundReference, now)
	if err := processServiceRefundTax(ctx, app, job, now, create); err != nil {
		t.Fatal(err)
	}
	var gross, tax int64
	if err := app.DB.QueryRow(ctx, `SELECT sum(amount_cents),sum(tax_cents) FROM service_refund_tax_records WHERE original_transaction=$1`, job.Original).Scan(&gross, &tax); err != nil || gross != 1000 || tax != 100 {
		t.Fatal("cumulative reversal", gross, tax, err)
	}
}

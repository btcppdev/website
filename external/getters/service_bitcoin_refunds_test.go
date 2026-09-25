package getters

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
)

func bitcoinRefundFixture(t *testing.T, evidence *OpenNodeBitcoinEvidence) (*config.AppContext, VerifiedServiceRefund, time.Time) {
	return bitcoinRefundProductFixture(t, evidence, "alerts.repository-slots")
}

func bitcoinRefundProductFixture(t *testing.T, evidence *OpenNodeBitcoinEvidence, product string) (*config.AppContext, VerifiedServiceRefund, time.Time) {
	t.Helper()
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Bitcoin closure test') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "closure-"+person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(time.Second)
	end := start.AddDate(1, 0, 0)
	quote := ServiceQuote{ProductCode: product, ResourceID: "closure-test", Name: "Prepaid service", Provider: "opennode", Quantity: 1, UnitCents: 1000, TotalCents: 1000, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: start.Add(time.Hour)}
	purchase, err := CreateServicePurchase(app, person, uuid.NewString(), quote)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := BeginServicePayment(app, person, purchase.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	reference := uuid.NewString()
	if err := BindServicePayment(app, attempt.ID, "opennode", reference); err != nil {
		t.Fatal(err)
	}
	if err := RecordServicePayment(app, VerifiedServicePayment{AttemptID: attempt.ID, Provider: "opennode", Reference: reference, EventID: "paid:" + reference, Currency: "USD", TotalCents: 1000, PaidAt: start, OpenNodeBitcoin: evidence}); err != nil {
		t.Fatal(err)
	}
	return app, VerifiedServiceRefund{AttemptID: attempt.ID, Provider: "opennode", PaymentReference: reference, RefundReference: uuid.NewString(), AmountCents: 100, Currency: "USD", RefundedAt: start.Add(time.Second), Reason: "permanent_closure"}, start.Add(end.Sub(start) / 2)
}

func TestBitcoinRefundLedgerPostgres(t *testing.T) {
	zero := int64(0)
	app, refund, _ := bitcoinRefundFixture(t, &OpenNodeBitcoinEvidence{PriceSatoshis: 1001, OverpaidSatoshis: &zero})
	ctx := context.Background()
	first := int64(200)
	refund.AmountSatoshis = &first
	for i := 0; i < 2; i++ {
		if err := RecordServiceRefund(ctx, app, refund); err != nil {
			t.Fatal(err)
		}
	}
	changed := refund
	different := int64(201)
	changed.AmountSatoshis = &different
	if err := RecordServiceRefund(ctx, app, changed); !errors.Is(err, ErrServiceRefund) {
		t.Fatal("changed replay accepted", err)
	}
	// These refunds fit the USD balance, but only one fits the Bitcoin balance.
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			next := refund
			amount := int64(500)
			next.AmountSatoshis = &amount
			next.RefundReference = uuid.NewString()
			results <- RecordServiceRefund(ctx, app, next)
		}()
	}
	wg.Wait()
	close(results)
	success, rejected := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrServiceRefund) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatal("concurrent Bitcoin over-refund", success, rejected)
	}
	items, err := ListServiceRefunds(ctx, app, refund.AttemptID)
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	var sum int64
	for _, item := range items {
		if item.AmountSatoshis == nil {
			t.Fatal("inspection lost Bitcoin amount")
		}
		sum += *item.AmountSatoshis
	}
	if sum != 700 {
		t.Fatal("duplicate or missing Bitcoin refund", sum)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE service_payment_refunds SET amount_satoshis=1 WHERE attempt_id=$1`, refund.AttemptID); err == nil {
		t.Fatal("Bitcoin refund evidence mutable")
	}
}

func TestBitcoinClosurePlanPostgres(t *testing.T) {
	for _, product := range []string{"alerts.repository-slots", "insider.membership.annual"} {
		t.Run(product, func(t *testing.T) {
			zero := int64(0)
			app, refund, closure := bitcoinRefundProductFixture(t, &OpenNodeBitcoinEvidence{PriceSatoshis: 1001, OverpaidSatoshis: &zero}, product)
			ctx := context.Background()
			plan, err := PlanBitcoinClosureRefund(ctx, app, refund.AttemptID, closure)
			if err != nil || plan.RemainingRefundSatoshis == nil || *plan.RemainingRefundSatoshis != 500 || len(plan.ReviewReasons) != 0 {
				t.Fatal("floor estimate", plan, err)
			}
			if plan.ProductCode != product {
				t.Fatal("preview changed product identity", plan.ProductCode)
			}
			if records, err := ListServiceRefunds(ctx, app, refund.AttemptID); err != nil || len(records) != 0 {
				t.Fatal("preview recorded a refund", records, err)
			}
			amount := int64(200)
			refund.AmountSatoshis = &amount
			if err := RecordServiceRefund(ctx, app, refund); err != nil {
				t.Fatal(err)
			}
			plan, err = PlanBitcoinClosureRefund(ctx, app, refund.AttemptID, closure)
			if err != nil || plan.PreviouslyRefundedSatoshis == nil || *plan.PreviouslyRefundedSatoshis != 200 || plan.RemainingRefundSatoshis == nil || *plan.RemainingRefundSatoshis != 300 {
				t.Fatal("prior closure refund not deducted", plan, err)
			}
			plan, err = PlanBitcoinClosureRefund(ctx, app, refund.AttemptID, closure.AddDate(2, 0, 0))
			if err != nil || plan.RemainingRefundSatoshis == nil || *plan.RemainingRefundSatoshis != 0 {
				t.Fatal("expired refund", plan, err)
			}
		})
	}
}

func TestBitcoinClosureReviewBlocksPostgres(t *testing.T) {
	for _, mode := range []string{"missing payment", "unknown overpayment", "overpaid", "unknown prior amount", "goodwill refund"} {
		t.Run(mode, func(t *testing.T) {
			zero := int64(0)
			evidence := &OpenNodeBitcoinEvidence{PriceSatoshis: 1001, OverpaidSatoshis: &zero}
			switch mode {
			case "missing payment":
				evidence = nil
			case "unknown overpayment":
				evidence.OverpaidSatoshis = nil
			case "overpaid":
				overpaid := int64(1)
				evidence.OverpaidSatoshis = &overpaid
			}
			app, refund, closure := bitcoinRefundFixture(t, evidence)
			ctx := context.Background()
			if mode == "unknown prior amount" || mode == "goodwill refund" {
				if mode == "goodwill refund" {
					amount := int64(200)
					refund.AmountSatoshis = &amount
					refund.Reason = "manual_goodwill"
				}
				if err := RecordServiceRefund(ctx, app, refund); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := PlanBitcoinClosureRefund(ctx, app, refund.AttemptID, closure)
			if err != nil || plan.RemainingRefundSatoshis != nil || len(plan.ReviewReasons) == 0 {
				t.Fatal("unsafe refund estimate", plan, err)
			}
			if mode == "missing payment" || mode == "unknown prior amount" {
				amount := int64(1)
				refund.AmountSatoshis = &amount
				refund.RefundReference = uuid.NewString()
				if err := RecordServiceRefund(ctx, app, refund); !errors.Is(err, ErrServiceRefund) {
					t.Fatal("unknown Bitcoin balance allowed refund", err)
				}
			}
		})
	}
}

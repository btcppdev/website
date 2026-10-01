package getters

import (
	"context"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

func TestServiceTaxRecoveryAfterRefundPostgres(t *testing.T) {
	for _, product := range []string{"alerts.repository-slots", "insider.membership.annual"} {
		for _, refundCents := range []int64{0, 100, 1000} {
			name := map[int64]string{0: "paid", 100: "partial", 1000: "full"}[refundCents]
			t.Run(product+"/"+name, func(t *testing.T) {
				app := databaseSmokeContext(t)
				ctx := context.Background()
				var person string
				if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Refund tax recovery') RETURNING id::text`).Scan(&person); err != nil {
					t.Fatal(err)
				}
				if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, "tax-recovery-"+person+"@example.test"); err != nil {
					t.Fatal(err)
				}
				start := time.Now().UTC().Truncate(time.Second)
				end := start.AddDate(1, 0, 0)
				expires := start.Add(time.Hour)
				quote := ServiceQuote{ProductCode: product, Name: "Tax recovery", Provider: "opennode", Quantity: 1, UnitCents: 1000, TotalCents: 1000, TaxCents: 100, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: expires}
				tax := TaxQuoteInput{Provider: "stripe", ProviderQuoteID: "taxcalc_" + person, SalesTaxAmountCents: 100, DestinationCountry: "US", DestinationRegion: "TX", RawResponse: `{"amount_total":1000,"tax_amount_inclusive":100}`, ExpiresAt: &expires}
				purchase, err := CreateTaxedServicePurchase(app, person, uuid.NewString(), quote, tax)
				if err != nil {
					t.Fatal(err)
				}
				attempt, err := BeginServicePayment(app, person, purchase.OrderID)
				if err != nil {
					t.Fatal(err)
				}
				reference := uuid.NewString()
				if err = BindServicePayment(app, attempt.ID, "opennode", reference); err != nil {
					t.Fatal(err)
				}
				zero := int64(0)
				if err = RecordServicePayment(app, VerifiedServicePayment{AttemptID: attempt.ID, Provider: "opennode", Reference: reference, EventID: "paid:" + reference, Currency: "USD", TotalCents: 1000, PaidAt: start, OpenNodeBitcoin: &OpenNodeBitcoinEvidence{PriceSatoshis: 1000000, OverpaidSatoshis: &zero}}); err != nil {
					t.Fatal(err)
				}
				if refundCents > 0 {
					sats := refundCents * 1000
					if err = RecordServiceRefund(ctx, app, VerifiedServiceRefund{AttemptID: attempt.ID, Provider: "opennode", PaymentReference: reference, RefundReference: "refund:" + reference, AmountCents: refundCents, AmountSatoshis: &sats, Currency: "USD", RefundedAt: start.Add(time.Second), Reason: "permanent_closure"}); err != nil {
						t.Fatal(err)
					}
				}
				findTaxReview := func() bool {
					t.Helper()
					after := ""
					for {
						page, err := ReviewServicePayments(app, after, time.Now())
						if err != nil {
							t.Fatal(err)
						}
						for _, item := range page.Items {
							if item.AttemptID == attempt.ID && item.Reason == "tax_transaction_missing" {
								return true
							}
						}
						if page.Next == "" {
							return false
						}
						after = page.Next
					}
				}
				if !findTaxReview() {
					t.Fatal("missing original tax transaction disappeared after refund")
				}
				calls := 0
				finalize := func(ctx context.Context, app *config.AppContext, order string) error {
					return finalizeServiceTax(ctx, app, order, func(params *stripe.TaxTransactionCreateFromCalculationParams) (*stripe.TaxTransaction, error) {
						calls++
						if *params.Calculation != tax.ProviderQuoteID || *params.PostedAt != start.Unix() || *params.IdempotencyKey != "service-tax-transaction:"+purchase.OrderID {
							t.Error("original tax basis changed")
						}
						return &stripe.TaxTransaction{ID: "tax_recovery_" + person, Reference: "service-order-" + purchase.OrderID, Currency: "usd", Type: "transaction", LineItems: &stripe.TaxTransactionLineItemList{Data: []*stripe.TaxTransactionLineItem{{Amount: 1000, AmountTax: 100, Quantity: 1, Reference: product, TaxBehavior: "inclusive"}}}}, nil
					})
				}
				// The real due-selection query must include a refunded order and stop
				// selecting it once its original tax transaction has been recorded.
				if _, err = app.DB.Exec(ctx, `UPDATE service_payment_attempts SET next_reconcile_at='0001-01-01' WHERE id=$1`, attempt.ID); err != nil {
					t.Fatal(err)
				}
				selected := 0
				recover := func(ctx context.Context, app *config.AppContext, ref string) (bool, error) {
					if ref != reference {
						return false, nil
					}
					selected++
					return finishVerifiedServiceSettlement(ctx, app, purchase.OrderID, finalize)
				}
				if _, err = reconcileDueOpenNodeServices(ctx, app, recover); err != nil {
					t.Fatal(err)
				}
				if selected != 1 || calls != 1 {
					t.Fatalf("refund tax recovery not run: selected=%d calls=%d", selected, calls)
				}
				if paid, err := finishVerifiedServiceSettlement(ctx, app, purchase.OrderID, finalize); err != nil || !paid || calls != 1 {
					t.Fatalf("tax replay: paid=%v calls=%d err=%v", paid, calls, err)
				}
				if findTaxReview() {
					t.Fatal("completed tax transaction still missing")
				}
				var grants, taxCount int
				var status string
				if err = app.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM service_entitlements WHERE purchase_id=$1),(SELECT count(*) FROM tax_transactions WHERE order_id=$2 AND sales_tax_amount_cents=100),status FROM shop_orders WHERE id=$2`, purchase.PurchaseID, purchase.OrderID).Scan(&grants, &taxCount, &status); err != nil {
					t.Fatal(err)
				}
				wantGrants := 0
				wantStatus := "paid"
				if refundCents == 0 && product == "alerts.repository-slots" {
					wantGrants = 1
				}
				if refundCents == 100 {
					wantStatus = "partially_refunded"
				}
				if refundCents == 1000 {
					wantStatus = "refunded"
				}
				if grants != wantGrants || taxCount != 1 || status != wantStatus {
					t.Fatalf("tax recovery altered access/payment: grants=%d tax=%d status=%s", grants, taxCount, status)
				}
				if _, err = app.DB.Exec(ctx, `UPDATE service_payment_attempts SET next_reconcile_at='0001-01-01' WHERE id=$1`, attempt.ID); err != nil {
					t.Fatal(err)
				}
				if _, err = reconcileDueOpenNodeServices(ctx, app, recover); err != nil || selected != 1 {
					t.Fatalf("completed refund polled again: %d %v", selected, err)
				}
			})
		}
	}
}

package getters

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v86"
)

func TestServiceRefundTaxReviewPostgres(t *testing.T) {
	for _, product := range []string{"alerts.repository-slots", "insider.membership.annual"} {
		for _, mode := range []string{"recover", "wrong amount", "unauthorized", "revoked during retrieval", "atomic rollback"} {
			t.Run(product+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				app, refund, _ := refundTaxFixture(t, product, 400, true)
				now := time.Now().UTC().Add(time.Second)
				job := claimRefundTaxFixture(t, app, refund.RefundReference, now)
				var response *stripe.TaxTransaction
				err := processServiceRefundTax(ctx, app, job, now, func(p *stripe.TaxTransactionCreateReversalParams) (*stripe.TaxTransaction, error) {
					response = refundTaxResponse(p)
					return nil, errors.New("lost response")
				})
				if !errors.Is(err, ErrServiceRefundTax) {
					t.Fatal(err)
				}
				expired := claimRefundTaxFixture(t, app, refund.RefundReference, now.Add(24*time.Hour))
				if !expired.Skip {
					t.Fatal("not held for review")
				}
				var actor string
				if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Tax reviewer') RETURNING id::text`).Scan(&actor); err != nil {
					t.Fatal(err)
				}
				if mode != "unauthorized" {
					if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, actor); err != nil {
						t.Fatal(err)
					}
				}
				calls := 0
				retrieve := func(id string) (*stripe.TaxTransaction, error) {
					calls++
					if id != response.ID {
						t.Error("different provider ID")
					}
					if mode == "wrong amount" {
						response.LineItems.Data[0].Amount++
					}
					if mode == "revoked during retrieval" {
						if _, err := app.DB.Exec(ctx, `DELETE FROM people_roles WHERE person_id=$1`, actor); err != nil {
							t.Fatal(err)
						}
					}
					return response, nil
				}
				if mode == "atomic rollback" {
					// An insert-only trigger fails after the accounting record is staged.
					if _, err := app.DB.Exec(ctx, `CREATE FUNCTION fail_tax_review_fixture() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture'; END $$; CREATE TRIGGER fail_tax_review_fixture BEFORE INSERT ON service_refund_tax_reviews FOR EACH ROW EXECUTE FUNCTION fail_tax_review_fixture()`); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						app.DB.Exec(context.Background(), `DROP TRIGGER IF EXISTS fail_tax_review_fixture ON service_refund_tax_reviews; DROP FUNCTION IF EXISTS fail_tax_review_fixture()`)
					})
				}
				err = reconcileServiceRefundTax(ctx, app, actor, refund.RefundReference, response.ID, "Confirmed in provider records", retrieve)
				if mode == "recover" {
					if err != nil {
						t.Fatal(err)
					}
					if err := reconcileServiceRefundTax(ctx, app, actor, refund.RefundReference, response.ID, "Confirmed in provider records", retrieve); err != nil {
						t.Fatal("replay", err)
					}
					if err := reconcileServiceRefundTax(ctx, app, actor, refund.RefundReference, response.ID, "Changed reason", retrieve); !errors.Is(err, ErrServiceRefundTax) {
						t.Fatal("changed replay", err)
					}
					if _, err := app.DB.Exec(ctx, `UPDATE service_refund_tax_reviews SET reason='changed' WHERE refund_reference=$1`, refund.RefundReference); err == nil {
						t.Fatal("audit mutable")
					}
				} else if err == nil {
					t.Fatal("invalid review accepted")
				}
				if mode == "unauthorized" && calls != 0 {
					t.Fatal("provider called before authorization")
				}
				var state string
				var records, audits int
				if err := app.DB.QueryRow(ctx, `SELECT state,(SELECT count(*) FROM service_refund_tax_records WHERE refund_reference=$1),(SELECT count(*) FROM service_refund_tax_reviews WHERE refund_reference=$1) FROM service_refund_tax_jobs WHERE refund_reference=$1`, refund.RefundReference).Scan(&state, &records, &audits); err != nil {
					t.Fatal(err)
				}
				if mode == "recover" {
					if state != "accepted" || records != 1 || audits != 1 {
						t.Fatal(state, records, audits)
					}
				} else if state != "review" || records != 0 || audits != 0 {
					t.Fatal("partial write", state, records, audits)
				}
			})
		}
	}
}

package getters

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestBitcoinClosureReviewPostgres(t *testing.T) {
	for _, product := range []string{"alerts.repository-slots", "insider.membership.annual"} {
		t.Run(product, func(t *testing.T) {
			zero := int64(0)
			app, refund, closure := bitcoinRefundProductFixture(t, &OpenNodeBitcoinEvidence{PriceSatoshis: 1001, OverpaidSatoshis: &zero}, product)
			ctx := context.Background()
			var actor string
			if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Closure review admin') RETURNING id::text`).Scan(&actor); err != nil {
				t.Fatal(err)
			}
			reviewID := uuid.NewString()
			if _, err := ReviewBitcoinClosureRefund(ctx, app, actor, reviewID, refund.AttemptID, closure, 500, "Reviewed closure"); !errors.Is(err, ErrServiceRefundAdmin) {
				t.Fatalf("non-admin review: %v", err)
			}
			if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, actor); err != nil {
				t.Fatal(err)
			}
			if _, err := ReviewBitcoinClosureRefund(ctx, app, actor, reviewID, refund.AttemptID, closure, 501, "Reviewed closure"); !errors.Is(err, ErrServiceRefund) {
				t.Fatalf("unreviewed amount accepted: %v", err)
			}
			first, err := ReviewBitcoinClosureRefund(ctx, app, actor, reviewID, refund.AttemptID, closure, 500, "Reviewed closure")
			if err != nil || first.Basis.ProductCode != product || *first.Basis.RemainingRefundSatoshis != 500 {
				t.Fatalf("review: %+v %v", first, err)
			}
			inspected, err := InspectBitcoinClosureReview(ctx, app, reviewID)
			if err != nil || inspected.ActorID != actor || inspected.Basis.AttemptID != refund.AttemptID || *inspected.Basis.RemainingRefundSatoshis != 500 || !inspected.CreatedAt.Equal(first.CreatedAt) {
				t.Fatalf("inspect review: %+v %v", inspected, err)
			}
			replay, err := ReviewBitcoinClosureRefund(ctx, app, actor, reviewID, refund.AttemptID, closure, 500, "Reviewed closure")
			if err != nil || !replay.CreatedAt.Equal(first.CreatedAt) {
				t.Fatalf("review replay: %+v %v", replay, err)
			}
			if _, err := ReviewBitcoinClosureRefund(ctx, app, actor, reviewID, refund.AttemptID, closure, 500, "Changed reason"); !errors.Is(err, ErrServiceRefund) {
				t.Fatalf("changed review replay: %v", err)
			}
			for _, query := range []string{`UPDATE service_bitcoin_closure_reviews SET amount_satoshis=501 WHERE id=$1`, `DELETE FROM service_bitcoin_closure_reviews WHERE id=$1`} {
				if _, err := app.DB.Exec(ctx, query, reviewID); err == nil {
					t.Fatal("review audit changed")
				}
			}
			var count int
			if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_payment_refunds WHERE attempt_id=$1`, refund.AttemptID).Scan(&count); err != nil || count != 0 {
				t.Fatal("review issued a refund")
			}
			paid := int64(200)
			refund.AmountSatoshis = &paid
			refund.AmountCents = 199
			if err := RecordServiceRefund(ctx, app, refund); err != nil {
				t.Fatal(err)
			}
			if _, err := ReviewBitcoinClosureRefund(ctx, app, actor, uuid.NewString(), refund.AttemptID, closure, 500, "Stale amount"); !errors.Is(err, ErrServiceRefund) {
				t.Fatalf("review ignored intervening refund: %v", err)
			}
			updated, err := ReviewBitcoinClosureRefund(ctx, app, actor, uuid.NewString(), refund.AttemptID, closure, 300, "Reviewed remaining obligation")
			if err != nil || *updated.Basis.PreviouslyRefundedSatoshis != 200 {
				t.Fatalf("updated review: %+v %v", updated, err)
			}
		})
	}
}

package getters

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"btcpp-web/internal/bitcoinpayout"
	"github.com/google/uuid"
)

func TestBitcoinPayoutBlockReconciliationPostgres(t *testing.T) {
	for _, product := range []string{"alerts.repository-slots", "insider.membership.annual"} {
		t.Run(product, func(t *testing.T) {
			ctx := context.Background()
			var calls atomic.Int64
			node := closurePayoutNode(t, nil, &calls)
			app, actor, req, _ := closurePayoutFixture(t, product, node)
			original, err := RecordBitcoinClosurePayout(ctx, app, node, actor, req)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := app.DB.Exec(ctx, `INSERT INTO service_bitcoin_payout_checks(review_id,next_check_at) VALUES($1,'0001-01-01')`, req.ReviewID); err != nil {
				t.Fatal(err)
			}
			old, err := claimBitcoinPayoutCheck(ctx, app, time.Now().UTC().Add(time.Second))
			if err != nil || old == nil || old.Review != req.ReviewID {
				t.Fatal(old, err)
			}
			id, block := uuid.NewString(), strings.Repeat("e", 64)
			if err := ReconcileBitcoinPayoutBlock(ctx, app, node, actor, id, req.ReviewID, block, "Remined transaction verified"); err != nil {
				t.Fatal(err)
			}
			before := calls.Load()
			if err := ReconcileBitcoinPayoutBlock(ctx, app, node, actor, id, req.ReviewID, block, "Remined transaction verified"); err != nil || calls.Load() != before {
				t.Fatal("replay changed evidence", err)
			}
			if err := ReconcileBitcoinPayoutBlock(ctx, app, node, actor, id, req.ReviewID, block, "Changed reason"); !errors.Is(err, ErrServiceRefund) {
				t.Fatal("changed replay", err)
			}
			if err := finishBitcoinPayoutCheck(ctx, app, old, nil, bitcoinpayout.ErrUnverified, time.Now().UTC()); !errors.Is(err, bitcoinpayout.ErrUnverified) {
				t.Fatal("old worker overwrote reconciliation", err)
			}
			check, err := InspectBitcoinPayoutCheck(ctx, app, req.ReviewID)
			if err != nil || check.State != "confirmed" || check.BlockHash != block || check.ReconciliationID != id {
				t.Fatal(check, err)
			}
			if _, err := app.DB.Exec(ctx, `UPDATE service_bitcoin_payout_checks SET next_check_at='0001-01-01' WHERE review_id=$1`, req.ReviewID); err != nil {
				t.Fatal(err)
			}
			current, err := claimBitcoinPayoutCheck(ctx, app, time.Now().UTC().Add(time.Second))
			if err != nil || current == nil || current.Original.BlockHash != block || current.Original.TxID != req.TxID {
				t.Fatal("worker did not follow new block", current, err)
			}
			if err := finishBitcoinPayoutCheck(ctx, app, current, nil, bitcoinpayout.ErrUnverified, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			payout, err := InspectBitcoinClosurePayout(ctx, app, req.ReviewID)
			if err != nil || payout.Evidence.BlockHash != original.Evidence.BlockHash || !payout.RecordedAt.Equal(original.RecordedAt) {
				t.Fatal("original proof rewritten", err)
			}
			var count int
			if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_payment_refunds WHERE attempt_id=$1`, original.Refund.AttemptID).Scan(&count); err != nil || count != 1 {
				t.Fatal("new refund", count, err)
			}
			if _, err := app.DB.Exec(ctx, `UPDATE service_bitcoin_payout_reconciliations SET reason='changed' WHERE id=$1`, id); err == nil {
				t.Fatal("mutable audit")
			}
			next := uuid.NewString()
			revoked := closurePayoutNode(t, func() { app.DB.Exec(ctx, `DELETE FROM people_roles WHERE person_id=$1`, actor) }, &calls)
			if err := ReconcileBitcoinPayoutBlock(ctx, app, revoked, actor, next, req.ReviewID, strings.Repeat("f", 64), "Another block"); !errors.Is(err, ErrServiceRefundAdmin) {
				t.Fatal("revoked admin wrote", err)
			}
			if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_bitcoin_payout_reconciliations WHERE review_id=$1`, req.ReviewID).Scan(&count); err != nil || count != 1 {
				t.Fatal("extra audit after rejection", count, err)
			}
		})
	}
}

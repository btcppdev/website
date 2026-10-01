package getters

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"btcpp-web/internal/bitcoinpayout"
)

func TestBitcoinPayoutFollowupPostgres(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int64
	node := closurePayoutNode(t, nil, &calls)
	app, actor, req, _ := closurePayoutFixture(t, "insider.membership.annual", node)
	original, err := RecordBitcoinClosurePayout(ctx, app, node, actor, req)
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := ListBusinessMetricCounts(app)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, metric := range metrics {
		if metric.Metric == "bitcoin_payout_checks" {
			if metric.State != "pending" && metric.State != "confirmed" && metric.State != "unverified" {
				t.Fatal("unbounded state", metric)
			}
			if metric.State == "pending" && metric.Count < 1 {
				t.Fatal("unmonitored payout missing")
			}
			if metric.Conference != "" {
				t.Fatal("private metric label")
			}
			found[metric.State] = true
		}
		if metric.Metric == "bitcoin_payout_oldest_due_timestamp_seconds" {
			if metric.Count <= 0 {
				t.Fatal("missing due time")
			}
			found["due"] = true
		}
	}
	if len(found) != 4 {
		t.Fatal("missing payout metrics", found)
	}
	t.Cleanup(func() {
		app.DB.Exec(ctx, `UPDATE service_bitcoin_payout_checks SET next_check_at=now()+interval '10 years' WHERE review_id=$1`, req.ReviewID)
	})
	if _, err := app.DB.Exec(ctx, `INSERT INTO service_bitcoin_payout_checks(review_id,next_check_at) VALUES($1,'0001-01-01')`, req.ReviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := RecheckDueBitcoinPayouts(ctx, app, node); err != nil {
		t.Fatal(err)
	}
	check, err := InspectBitcoinPayoutCheck(ctx, app, req.ReviewID)
	if err != nil || check.State != "confirmed" || check.CheckedAt == nil || check.NextCheckAt.Sub(*check.CheckedAt) != 6*time.Hour {
		t.Fatal("follow-up not scheduled", check, err)
	}
	now := time.Now().UTC().Add(time.Second)
	if _, err := app.DB.Exec(ctx, `UPDATE service_bitcoin_payout_checks SET next_check_at='0001-01-01' WHERE review_id=$1`, req.ReviewID); err != nil {
		t.Fatal(err)
	}
	first, err := claimBitcoinPayoutCheck(ctx, app, now)
	if err != nil || first == nil || first.Review != req.ReviewID {
		t.Fatal("claim", first, err)
	}
	// A replacement worker owns the row after the original lease expires.
	second, err := claimBitcoinPayoutCheck(ctx, app, now.Add(3*time.Minute))
	if err != nil || second == nil || second.Review != req.ReviewID {
		t.Fatal("replacement claim", second, err)
	}
	if err := finishBitcoinPayoutCheck(ctx, app, first, nil, errors.New("outage"), now); !errors.Is(err, bitcoinpayout.ErrUnverified) {
		t.Fatal("stale worker wrote", err)
	}
	if err := finishBitcoinPayoutCheck(ctx, app, second, nil, errors.New("outage"), now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	check, err = InspectBitcoinPayoutCheck(ctx, app, req.ReviewID)
	if err != nil || check.State != "unverified" || check.NextCheckAt.Sub(*check.CheckedAt) != 15*time.Minute {
		t.Fatal("failure lost", check, err)
	}
	payout, err := InspectBitcoinClosurePayout(ctx, app, req.ReviewID)
	if err != nil || !payout.RecordedAt.Equal(original.RecordedAt) || !payout.Evidence.VerifiedAt.Equal(original.Evidence.VerifiedAt) || payout.Refund.RefundReference != original.Refund.RefundReference {
		t.Fatal("original proof changed", err)
	}
	// A later valid observation clears the warning without rewriting the refund.
	if _, err := app.DB.Exec(ctx, `UPDATE service_bitcoin_payout_checks SET next_check_at='0001-01-01' WHERE review_id=$1`, req.ReviewID); err != nil {
		t.Fatal(err)
	}
	job, err := claimBitcoinPayoutCheck(ctx, app, now.Add(20*time.Minute))
	if err != nil || job == nil || job.Review != req.ReviewID {
		t.Fatal(job, err)
	}
	evidence, err := node.Verify(ctx, job.Original.Request)
	if err != nil {
		t.Fatal(err)
	}
	if err := finishBitcoinPayoutCheck(ctx, app, job, evidence, nil, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	check, err = InspectBitcoinPayoutCheck(ctx, app, req.ReviewID)
	if err != nil || check.State != "confirmed" {
		t.Fatal(check, err)
	}
	var count int
	if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_payment_refunds WHERE attempt_id=$1`, original.Refund.AttemptID).Scan(&count); err != nil || count != 1 {
		t.Fatal("follow-up created refund", count, err)
	}
}

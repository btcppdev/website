package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"context"
	"errors"
	"fmt"
	"github.com/stripe/stripe-go/v86"
	"time"
)

// ImportServiceStripeRefund uses the authenticated provider's payment links,
// then verifies the refund against a settled local payment before recording it.
func ImportServiceStripeRefund(ctx context.Context, app *config.AppContext, id string, c servicestripe.RefundSyncClient) error {
	p := &stripe.RefundParams{}
	p.Context = ctx
	r, err := c.Verify.Refund(id, p)
	if err != nil {
		return err
	}
	if r == nil || r.ID != id {
		return servicestripe.ErrRefund
	}
	switch r.Status {
	case "pending", "requires_action", "failed", "canceled":
		return nil
	case "succeeded":
	default:
		return servicestripe.ErrRefund
	}
	if r.Charge == nil {
		return servicestripe.ErrRefund
	}
	cp := &stripe.ChargeParams{}
	cp.Context = ctx
	ch, err := c.Verify.Charge(r.Charge.ID, cp)
	if err != nil {
		return err
	}
	if ch == nil || ch.ID != r.Charge.ID || ch.Livemode != c.Verify.Live {
		return servicestripe.ErrRefund
	}
	// Legacy direct charges without a PaymentIntent cannot be our service payments.
	if ch.PaymentIntent == nil {
		return nil
	}
	refs, err := c.References(ctx, ch.PaymentIntent.ID)
	if err != nil {
		return err
	}
	rows, err := app.DB.Query(ctx, `SELECT id::text,settled_at IS NOT NULL FROM service_payment_attempts WHERE provider='stripe' AND provider_reference=ANY($1::text[])`, refs)
	if err != nil {
		return err
	}
	var ids []string
	unsettled := false
	for rows.Next() {
		var id string
		var settled bool
		if err = rows.Scan(&id, &settled); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
		unsettled = unsettled || !settled
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	} // Merch/tickets use their own accounting.
	if len(ids) != 1 || unsettled {
		return ErrServiceRefund
	}
	return ReconcileServiceStripeRefund(ctx, app, ids[0], id, c.Verify)
}

func SyncServiceStripePaymentRefunds(ctx context.Context, app *config.AppContext, attempt, reference string, c servicestripe.RefundSyncClient) error {
	ids, err := c.PaymentRefundIDs(ctx, reference)
	if err != nil {
		return err
	}
	for _, id := range ids {
		p := &stripe.RefundParams{}
		p.Context = ctx
		r, err := c.Verify.Refund(id, p)
		if err != nil {
			return err
		}
		if r == nil || r.ID != id {
			return servicestripe.ErrRefund
		}
		switch r.Status {
		case "pending", "requires_action", "failed", "canceled":
			continue
		case "succeeded":
		default:
			return servicestripe.ErrRefund
		}
		if err = ReconcileServiceStripeRefund(ctx, app, attempt, id, c.Verify); err != nil {
			return err
		}
	}
	return nil
}

// ReconcileDueServiceStripeRefunds checks every settled service payment,
// including those predating webhook integration. Claims expire after ten minutes.
func ReconcileDueServiceStripeRefunds(ctx context.Context, app *config.AppContext, c servicestripe.RefundSyncClient) (int, error) {
	_, err := app.DB.Exec(ctx, `INSERT INTO service_stripe_refund_checks(attempt_id)
 SELECT a.id FROM service_payment_attempts a WHERE a.provider='stripe' AND a.settled_at IS NOT NULL
 ON CONFLICT DO NOTHING`)
	if err != nil {
		return 0, err
	}
	rows, err := app.DB.Query(ctx, `WITH due AS (
 SELECT c.attempt_id FROM service_stripe_refund_checks c JOIN service_payment_attempts a ON a.id=c.attempt_id
 JOIN service_purchase_requests r ON r.id=a.purchase_id JOIN shop_orders o ON o.id=r.order_id
 WHERE c.next_check_at<=now() AND o.status<>'refunded'
 ORDER BY c.next_check_at,c.attempt_id LIMIT 20 FOR UPDATE OF c SKIP LOCKED
 ) UPDATE service_stripe_refund_checks c SET next_check_at=now()+interval '10 minutes'
 FROM due,service_payment_attempts a WHERE c.attempt_id=due.attempt_id AND a.id=c.attempt_id
 RETURNING a.id::text,a.provider_reference`)
	if err != nil {
		return 0, err
	}
	type payment struct{ id, ref string }
	var work []payment
	for rows.Next() {
		var p payment
		if err = rows.Scan(&p.id, &p.ref); err != nil {
			rows.Close()
			return 0, err
		}
		work = append(work, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, p := range work {
		attempt, cancel := context.WithTimeout(ctx, 20*time.Second)
		e := SyncServiceStripePaymentRefunds(attempt, app, p.id, p.ref, c)
		cancel()
		if e != nil {
			errs = append(errs, fmt.Errorf("refund check %s: %w", p.id, e))
			_, err = app.DB.Exec(ctx, `UPDATE service_stripe_refund_checks SET next_check_at=now()+interval '5 minutes',failures=failures+1,last_error='Stripe refund verification failed; retry scheduled' WHERE attempt_id=$1`, p.id)
		} else {
			_, err = app.DB.Exec(ctx, `UPDATE service_stripe_refund_checks SET next_check_at=now()+interval '1 hour',last_success_at=now(),failures=0,last_error='' WHERE attempt_id=$1`, p.id)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return len(work), errors.Join(errs...)
}

package getters

import (
	"context"
	"errors"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v86"
	stripeTaxTransaction "github.com/stripe/stripe-go/v86/tax/transaction"
)

// ReconcileServiceRefundTax records an existing provider reversal after admin
// review. Actor must come from authenticated server identity. It only retrieves
// a Stripe transaction; it cannot create a reversal, refund, or Bitcoin payment.
func ReconcileServiceRefundTax(ctx context.Context, app *config.AppContext, actor, reference, reversal, reason string) error {
	if app.Env == nil || strings.TrimSpace(app.Env.StripeKey) == "" {
		return ErrServiceRefundTax
	}
	client := stripeTaxTransaction.Client{B: stripe.GetBackend(stripe.APIBackend), Key: app.Env.StripeKey}
	return reconcileServiceRefundTax(ctx, app, actor, reference, reversal, reason, func(id string) (*stripe.TaxTransaction, error) {
		params := &stripe.TaxTransactionParams{}
		params.Context = ctx
		params.AddExpand("line_items")
		return client.Get(id, params)
	})
}

func reconcileServiceRefundTax(ctx context.Context, app *config.AppContext, actor, reference, reversal, reason string, retrieve func(string) (*stripe.TaxTransaction, error)) error {
	reason = strings.TrimSpace(reason)
	if id, err := uuid.Parse(actor); err != nil || id.String() != actor {
		return ErrServiceRefundAdmin
	}
	if reference == "" || len(reference) > 1000 || !strings.HasPrefix(reversal, "tax_") || len(reversal) > 255 || reason == "" || len(reason) > 1000 {
		return ErrServiceRefundTax
	}
	var authorized bool
	if err := app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM people_roles r JOIN people p ON p.id=r.person_id WHERE r.person_id=$1 AND r.scope='global' AND r.position='admin' AND NOT p.is_deleted_account)`, actor).Scan(&authorized); err != nil {
		return err
	}
	if !authorized {
		return ErrServiceRefundAdmin
	}
	result, err := retrieve(reversal)
	if err != nil || result == nil || result.ID != reversal {
		return ErrServiceRefundTax
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var role int
	err = tx.QueryRow(ctx, `SELECT 1 FROM people_roles r JOIN people p ON p.id=r.person_id WHERE r.person_id=$1 AND r.scope='global' AND r.position='admin' AND NOT p.is_deleted_account FOR SHARE OF r,p`, actor).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrServiceRefundAdmin
	}
	if err != nil {
		return err
	}
	job := &serviceRefundTaxJob{Provider: "opennode", Reference: reference, Token: uuid.NewString()}
	var state, order, attempt string
	var revision int64
	err = tx.QueryRow(ctx, `SELECT j.state,coalesce(j.original_transaction,''),f.amount_cents,p.order_id::text,f.attempt_id::text,d.revision
 FROM service_refund_tax_jobs j JOIN service_payment_refunds f USING(provider,refund_reference)
 JOIN service_refund_jobs d USING(provider,refund_reference)
 JOIN service_payment_attempts a ON a.id=f.attempt_id JOIN service_purchase_requests p ON p.id=a.purchase_id
 WHERE j.provider='opennode' AND j.refund_reference=$1 FOR UPDATE OF j`, reference).Scan(&state, &job.Original, &job.Amount, &order, &attempt, &revision)
	if err != nil {
		return err
	}
	if state == "accepted" {
		var matches bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_refund_tax_reviews WHERE provider='opennode' AND refund_reference=$1 AND actor_id=$2 AND reversal_transaction=$3 AND reason=$4)`, reference, actor, reversal, reason).Scan(&matches)
		if err != nil {
			return err
		}
		if !matches {
			return ErrServiceRefundTax
		}
		return tx.Commit(ctx)
	}
	if state != "review" || job.Original == "" || job.Amount <= 0 {
		return ErrServiceRefundTax
	}
	// Use the same job-then-order lock order as the automatic worker.
	err = tx.QueryRow(ctx, `SELECT (p.quote_snapshot->>'total_cents')::bigint,(p.quote_snapshot->>'tax_cents')::bigint FROM service_purchase_requests p JOIN shop_orders o ON o.id=p.order_id WHERE o.id=$1 FOR UPDATE OF o`, order).Scan(&job.OriginalTotal, &job.OriginalTax)
	if err != nil {
		return err
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT
 (SELECT count(*)=1 FROM tax_transactions WHERE provider='stripe' AND provider_transaction_id=$1)
 AND EXISTS(SELECT 1 FROM tax_transactions WHERE provider='stripe' AND provider_transaction_id=$1 AND order_id=$2 AND status='recorded' AND sales_tax_amount_cents=$3)
 AND NOT EXISTS(SELECT 1 FROM service_refund_jobs d JOIN service_refund_tax_jobs t USING(provider,refund_reference) WHERE d.attempt_id=$4 AND d.revision<$5 AND t.state<>'accepted')`, job.Original, order, job.OriginalTax, attempt, revision).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrServiceRefundTax
	}
	if _, err = tx.Exec(ctx, `UPDATE service_refund_tax_jobs SET state='pending',lease_token=$2 WHERE provider='opennode' AND refund_reference=$1`, reference, job.Token); err != nil {
		return err
	}
	// All validations and normalized financial evidence are shared with the worker.
	// Any mismatch rolls back the temporary claim and leaves the review untouched.
	if err = recordServiceRefundTaxTx(ctx, tx, job, result, time.Now().UTC()); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO service_refund_tax_reviews(provider,refund_reference,actor_id,reversal_transaction,reason) VALUES('opennode',$1,$2,$3,$4)`, reference, actor, reversal, reason); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

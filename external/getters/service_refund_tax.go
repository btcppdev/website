package getters

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v86"
	stripeTaxTransaction "github.com/stripe/stripe-go/v86/tax/transaction"
)

var ErrServiceRefundTax = errors.New("service refund tax recording requires retry or review")

type serviceRefundTaxJob struct {
	Provider, Reference, Original, Token string
	Amount, OriginalTotal, OriginalTax   int64
	Skip                                 bool
}

// ReconcileDueServiceRefundTaxes only records already-verified OpenNode refunds
// in Stripe Tax. It never creates a customer refund or changes product access.
func ReconcileDueServiceRefundTaxes(ctx context.Context, app *config.AppContext) (int, error) {
	if app.Env == nil || strings.TrimSpace(app.Env.StripeKey) == "" {
		return 0, ErrServiceRefundTax
	}
	client := stripeTaxTransaction.Client{B: stripe.GetBackend(stripe.APIBackend), Key: app.Env.StripeKey}
	return reconcileDueServiceRefundTaxes(ctx, app, time.Now().UTC(), func(params *stripe.TaxTransactionCreateReversalParams) (*stripe.TaxTransaction, error) {
		return client.CreateReversal(params)
	})
}

func refundTaxReference(provider, reference string) string {
	sum := sha256.Sum256([]byte(provider + "\x00" + reference))
	return fmt.Sprintf("service-refund-tax:%x", sum)
}

func claimServiceRefundTax(ctx context.Context, app *config.AppContext, now time.Time) (*serviceRefundTaxJob, error) {
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	job := &serviceRefundTaxJob{Token: uuid.NewString()}
	var order string
	var first *time.Time
	var frozen *string
	err = tx.QueryRow(ctx, `SELECT j.provider,j.refund_reference,r.amount_cents,p.order_id::text,j.first_attempt_at,j.original_transaction
 FROM service_refund_tax_jobs j JOIN service_payment_refunds r USING(provider,refund_reference)
 JOIN service_refund_jobs delivery USING(provider,refund_reference)
 JOIN service_payment_attempts a ON a.id=r.attempt_id JOIN service_purchase_requests p ON p.id=a.purchase_id
 WHERE j.state='pending' AND j.next_attempt_at<=$1 AND j.lease_until<=$1
 AND EXISTS(SELECT 1 FROM tax_transactions t WHERE t.order_id=p.order_id AND t.provider='stripe' AND t.status='recorded')
 AND NOT EXISTS(SELECT 1 FROM service_refund_jobs earlier JOIN service_refund_tax_jobs tax USING(provider,refund_reference)
 WHERE earlier.attempt_id=r.attempt_id AND earlier.revision<delivery.revision AND tax.state<>'accepted')
 ORDER BY j.next_attempt_at,j.created_at,j.refund_reference LIMIT 1 FOR UPDATE OF j SKIP LOCKED`, now).Scan(&job.Provider, &job.Reference, &job.Amount, &order, &first, &frozen)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT p.quote_snapshot FROM service_purchase_requests p JOIN shop_orders o ON o.id=p.order_id WHERE o.id=$1 FOR UPDATE OF o`, order).Scan(&raw); err != nil {
		return nil, err
	}
	var quote ServiceQuote
	if err = json.Unmarshal(raw, &quote); err != nil {
		return nil, err
	}
	job.OriginalTotal, job.OriginalTax = quote.TotalCents, quote.TaxCents
	var count, tax int64
	err = tx.QueryRow(ctx, `SELECT count(*),coalesce(min(provider_transaction_id),''),coalesce(min(sales_tax_amount_cents),0) FROM tax_transactions WHERE order_id=$1 AND provider='stripe' AND status='recorded'`, order).Scan(&count, &job.Original, &tax)
	if err != nil {
		return nil, err
	}
	reason := ""
	if count != 1 || !strings.HasPrefix(job.Original, "tax_") || tax != quote.TaxCents || quote.Provider != "opennode" || quote.TotalCents <= 0 || quote.TaxCents < 0 || quote.TaxCents > quote.TotalCents || job.Amount < 0 || job.Amount > quote.TotalCents || (frozen != nil && *frozen != job.Original) {
		reason = "original_tax_mismatch"
	}
	if reason == "" {
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM tax_transactions WHERE provider='stripe' AND provider_transaction_id=$1`, job.Original).Scan(&count); err != nil {
			return nil, err
		}
		if count != 1 {
			reason = "original_tax_mismatch"
		}
	}
	// Persist the first potential submission before network I/O. Never reset this
	// clock after a timeout, restart, lease expiry or an ambiguous response.
	if reason == "" && job.Amount > 0 && first != nil && !now.Before(first.Add(23*time.Hour)) {
		reason = "idempotency_window_expired"
	}
	if reason != "" {
		_, err = tx.Exec(ctx, `UPDATE service_refund_tax_jobs SET state='review',review_reason=$3,lease_token=NULL WHERE provider=$1 AND refund_reference=$2`, job.Provider, job.Reference, reason)
		if err != nil {
			return nil, err
		}
		job.Skip = true
		return job, tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `UPDATE service_refund_tax_jobs SET original_transaction=$3,first_attempt_at=coalesce(first_attempt_at,$4),lease_token=$5,lease_until=$4::timestamptz+interval '1 minute',attempts=attempts+1 WHERE provider=$1 AND refund_reference=$2`, job.Provider, job.Reference, job.Original, now, job.Token)
	if err != nil {
		return nil, err
	}
	return job, tx.Commit(ctx)
}

func reconcileDueServiceRefundTaxes(ctx context.Context, app *config.AppContext, now time.Time, create func(*stripe.TaxTransactionCreateReversalParams) (*stripe.TaxTransaction, error)) (int, error) {
	var firstErr error
	started := time.Now()
	for n := 0; n < 20; n++ {
		claimNow := now.Add(time.Since(started))
		job, err := claimServiceRefundTax(ctx, app, claimNow)
		if err != nil {
			return n, err
		}
		if job == nil {
			return n, firstErr
		}
		if job.Skip {
			continue
		}
		if err = processServiceRefundTax(ctx, app, job, claimNow, create); err != nil {
			firstErr = err
		}
	}
	return 20, firstErr
}

func processServiceRefundTax(ctx context.Context, app *config.AppContext, job *serviceRefundTaxJob, now time.Time, create func(*stripe.TaxTransactionCreateReversalParams) (*stripe.TaxTransaction, error)) error {
	var result *stripe.TaxTransaction
	var err error
	if job.Amount > 0 {
		params := &stripe.TaxTransactionCreateReversalParams{Mode: stripe.String("partial"), OriginalTransaction: stripe.String(job.Original), Reference: stripe.String(refundTaxReference(job.Provider, job.Reference)), FlatAmount: stripe.Int64(-job.Amount)}
		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		params.Context = callCtx
		params.SetIdempotencyKey(*params.Reference)
		params.AddExpand("line_items")
		result, err = create(params)
		cancel()
		if err != nil {
			_, updateErr := app.DB.Exec(ctx, `UPDATE service_refund_tax_jobs SET next_attempt_at=$4::timestamptz+interval '15 minutes',lease_until=$4,lease_token=NULL,review_reason='provider_retry' WHERE provider=$1 AND refund_reference=$2 AND lease_token=$3 AND state='pending'`, job.Provider, job.Reference, job.Token, now)
			if updateErr != nil {
				return updateErr
			}
			return ErrServiceRefundTax
		}
	}
	return recordServiceRefundTax(ctx, app, job, result, now)
}

func recordServiceRefundTax(ctx context.Context, app *config.AppContext, job *serviceRefundTaxJob, result *stripe.TaxTransaction, now time.Time) error {
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	err = recordServiceRefundTaxTx(ctx, tx, job, result, now)
	if err != nil && !errors.Is(err, ErrServiceRefundTax) {
		return err
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		return commitErr
	}
	return err
}

func recordServiceRefundTaxTx(ctx context.Context, tx pgx.Tx, job *serviceRefundTaxJob, result *stripe.TaxTransaction, now time.Time) error {
	var token *string
	var state string
	err := tx.QueryRow(ctx, `SELECT lease_token::text,state FROM service_refund_tax_jobs WHERE provider=$1 AND refund_reference=$2 FOR UPDATE`, job.Provider, job.Reference).Scan(&token, &state)
	if err != nil {
		return err
	}
	if state != "pending" || token == nil || *token != job.Token {
		return ErrServiceRefundTax
	}
	var tax int64
	var reversal *string
	valid := true
	if job.Amount > 0 {
		valid = result != nil && strings.HasPrefix(result.ID, "tax_") && result.ID != job.Original && result.Type == "reversal" && result.Currency == "usd" && result.Reference == refundTaxReference(job.Provider, job.Reference) && result.Reversal != nil && result.Reversal.OriginalTransaction == job.Original && result.LineItems != nil && !result.LineItems.HasMore && len(result.LineItems.Data) == 1
		if valid {
			line := result.LineItems.Data[0]
			valid = line != nil && line.TaxBehavior == "inclusive" && line.Amount == -job.Amount && line.AmountTax <= 0 && line.AmountTax >= -job.Amount && (result.ShippingCost == nil || (result.ShippingCost.Amount == 0 && result.ShippingCost.AmountTax == 0))
			if valid {
				tax = -line.AmountTax
				reversal = &result.ID
			}
		}
	}
	var previouslyReversed, previousTax int64
	if err = tx.QueryRow(ctx, `SELECT coalesce(sum(amount_cents),0),coalesce(sum(tax_cents),0) FROM service_refund_tax_records WHERE original_transaction=$1`, job.Original).Scan(&previouslyReversed, &previousTax); err != nil {
		return err
	}
	if previouslyReversed > job.OriginalTotal || job.Amount > job.OriginalTotal-previouslyReversed || previousTax > job.OriginalTax || tax > job.OriginalTax-previousTax {
		valid = false
	}
	if !valid {
		if _, err = tx.Exec(ctx, `UPDATE service_refund_tax_jobs SET state='review',review_reason='provider_response_mismatch',lease_token=NULL WHERE provider=$1 AND refund_reference=$2`, job.Provider, job.Reference); err != nil {
			return err
		}
		return ErrServiceRefundTax
	}
	// Store only financial evidence, not Stripe's customer/address payload.
	evidence, err := json.Marshal(map[string]any{"original_transaction": job.Original, "reversal_transaction": reversal, "reference": refundTaxReference(job.Provider, job.Reference), "currency": "USD", "amount_cents": job.Amount, "tax_cents": tax, "zero_amount": job.Amount == 0})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO service_refund_tax_records(provider,refund_reference,original_transaction,reversal_transaction,amount_cents,tax_cents,evidence) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`, job.Provider, job.Reference, job.Original, reversal, job.Amount, tax, evidence)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE service_refund_tax_jobs SET state='accepted',review_reason='',lease_token=NULL,lease_until=$3 WHERE provider=$1 AND refund_reference=$2`, job.Provider, job.Reference, now)
	if err != nil {
		return err
	}
	return nil
}

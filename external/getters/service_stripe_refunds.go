package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ReconcileServiceStripeRefund reads saved payment identity before querying
// Stripe. It never trusts a callback's amount, payment reference or account.
// Callers must keep ingestion disabled until product refund delivery is ready.
func ReconcileServiceStripeRefund(ctx context.Context, app *config.AppContext, attemptID, refundID string, client servicestripe.RefundClient) error {
	var reference string
	var total int64
	err := app.DB.QueryRow(ctx, `SELECT a.provider_reference,(r.quote_snapshot->>'total_cents')::bigint
 FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id
 WHERE a.id=$1 AND a.provider='stripe' AND a.settled_at IS NOT NULL`, attemptID).Scan(&reference, &total)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrServiceRefund
	}
	if err != nil {
		return err
	}
	evidence, err := client.Verify(ctx, refundID, reference, total)
	if err != nil {
		return err
	}
	return RecordServiceRefund(ctx, app, VerifiedServiceRefund{
		AttemptID: attemptID, Provider: "stripe", PaymentReference: evidence.PaymentReference,
		RefundReference: evidence.RefundID, AmountCents: evidence.AmountCents, Currency: "USD",
		RefundedAt: evidence.ProviderCreatedAt, Reason: evidence.Reason,
	})
}

package getters

import (
	"context"
	"encoding/json"
	"time"

	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/servicebilling"
	"github.com/jackc/pgx/v5"
)

func validateAlertsSwitchPurchase(ctx context.Context, tx pgx.Tx, person string, q ServiceQuote) error {
	var quantity, end int64
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT b.quantity,b.period_end,s.snapshot FROM alerts_bitcoin_switches b JOIN alerts_stripe_checkouts c ON c.id=b.checkout_id JOIN service_stripe_cancellations x ON x.id=b.id JOIN alerts_stripe_subscription_state s ON s.checkout_id=c.id WHERE b.id=$1 AND c.person_id=$2 AND b.state='scheduled' AND b.purchase_id IS NULL AND x.state='confirmed' FOR UPDATE OF b,c`, q.AlertsSwitchID, person).Scan(&quantity, &end, &raw)
	if err != nil {
		return ErrAlertsBillingChanged
	}
	var state alertsstripe.SubscriptionState
	if json.Unmarshal(raw, &state) != nil || state.PeriodEnd != end || !(state.CancelAtPeriodEnd || (state.State == "cancelled" && state.CancellationReason == "cancellation_requested")) {
		return ErrAlertsBillingChanged
	}
	if q.ProductCode != servicebilling.AlertsProduct || q.Provider != "opennode" || q.Quantity != quantity || q.BillingMonths != 12 || q.BaseOrderID != "" || q.PeriodStart == nil || q.PeriodEnd == nil || q.PeriodStart.Unix() < end {
		return ErrServiceQuote
	}
	// A quote prepared before expiry is not allowed to charge for elapsed time.
	if q.PeriodStart.Before(time.Now().Add(-15 * time.Minute)) {
		return ErrServiceQuote
	}
	term, err := servicebilling.AlertsTerm("opennode", quantity, 12, *q.PeriodStart)
	if err != nil || !term.End.Equal(*q.PeriodEnd) || term.TotalCents != q.TotalCents {
		return ErrServiceQuote
	}
	return nil
}

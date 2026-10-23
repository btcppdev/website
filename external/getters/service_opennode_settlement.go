package getters

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
)

// ReconcileOpenNodeServiceCharge re-fetches authoritative provider state, records
// final payment once, then fulfills the immutable purchase. False means the
// provider has not confirmed final payment. It never creates another charge.
// Canceled/edited/expired purchases retain settlement evidence but fail fulfillment
// for operator review. Retrying after a crash between settlement and fulfillment
// is safe and resumes fulfillment.
func ReconcileOpenNodeServiceCharge(ctx context.Context, app *config.AppContext, chargeID string) (bool, error) {
	return reconcileOpenNodeServiceCharge(ctx, app, chargeID, openNodeHTTPClient)
}

func reconcileOpenNodeServiceCharge(ctx context.Context, app *config.AppContext, chargeID string, client *http.Client) (bool, error) {
	charge, err := lookupOpenNodeServiceCharge(ctx, app, chargeID, client)
	if err != nil {
		return false, err
	}
	if charge.Status != "paid" {
		return false, nil
	}
	var orderID string
	var raw []byte
	err = app.DB.QueryRow(ctx, `SELECT r.order_id::text,r.quote_snapshot FROM service_purchase_requests r JOIN service_payment_attempts a ON a.purchase_id=r.id WHERE a.id=$1`, charge.AttemptID).Scan(&orderID, &raw)
	if err != nil {
		return false, err
	}
	var quote ServiceQuote
	if err := json.Unmarshal(raw, &quote); err != nil {
		return false, err
	}
	evidence := &OpenNodeBitcoinEvidence{PriceSatoshis: charge.PriceSatoshis, FeeSatoshis: charge.FeeSatoshis, OverpaidSatoshis: charge.OverpaidSatoshis, MissingSatoshis: charge.MissingSatoshis}
	err = recordServicePayment(app, VerifiedServicePayment{AttemptID: charge.AttemptID, Provider: "opennode", Reference: charge.ID, EventID: "charge-paid:" + charge.ID, Currency: "USD", TotalCents: quote.TotalCents, OpenNodeBitcoin: evidence}, true)
	if err != nil {
		return false, err
	}
	return finishVerifiedServiceSettlement(ctx, app, orderID, FinalizeServiceTax)
}

// Tax recovery must not depend on granting access. Insider grants are delivered
// by its existing fulfillment outbox; refunded orders must never gain new access.
func finishVerifiedServiceSettlement(ctx context.Context, app *config.AppContext, orderID string, finalize func(context.Context, *config.AppContext, string) error) (bool, error) {
	var status, product string
	if err := app.DB.QueryRow(ctx, `SELECT o.status,r.quote_snapshot->>'product_code' FROM shop_orders o JOIN service_purchase_requests r ON r.order_id=o.id WHERE o.id=$1`, orderID).Scan(&status, &product); err != nil {
		return false, err
	}
	if status != "paid" && status != "partially_refunded" && status != "refunded" {
		return false, ErrServiceFulfillment
	}
	switch product {
	case "alerts.repository-slots":
		if status == "paid" {
			if _, err := FulfillServicePurchase(app, orderID); err != nil {
				if !errors.Is(err, ErrServiceFulfillment) {
					return false, err
				}
				// A refund may commit between the status read and the fulfillment
				// lock. Only that state transition permits skipping the grant.
				if check := app.DB.QueryRow(ctx, `SELECT status FROM shop_orders WHERE id=$1`, orderID).Scan(&status); check != nil {
					return false, check
				}
				if status != "partially_refunded" && status != "refunded" {
					return false, err
				}
			}
		}
	case "insider.membership.monthly", "insider.membership.annual", "insider.article", expedition.ProductCode:
		// recordServicePayment atomically queues Insider delivery, including on
		// settlement replays; there is no local Alerts entitlement to create.
	default:
		return false, ErrServiceFulfillment
	}
	return true, finalize(ctx, app, orderID)
}

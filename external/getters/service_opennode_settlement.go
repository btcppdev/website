package getters

import (
	"context"
	"encoding/json"
	"net/http"

	"btcpp-web/internal/config"
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
	_, err = FulfillServicePurchase(app, orderID)
	if err != nil {
		return false, err
	}
	return true, FinalizeServiceTax(ctx, app, orderID)
}

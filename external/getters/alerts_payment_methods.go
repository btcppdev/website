package getters

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"github.com/jackc/pgx/v5"
)

func alertsOwnedBinding(ctx context.Context, tx pgx.Tx, person, checkout string) (alertsstripe.Binding, error) {
	var b alertsstripe.Binding
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT s.binding FROM alerts_stripe_current_subscriptions s JOIN alerts_stripe_checkouts c ON c.id=s.checkout_id WHERE c.id=$1 AND c.person_id=$2 FOR UPDATE OF c`, checkout, person).Scan(&raw)
	if err != nil {
		return b, err
	}
	if json.Unmarshal(raw, &b) != nil || b.PersonID != person || b.IntentID != checkout {
		return b, alertsstripe.ErrSubscription
	}
	return b, nil
}

func AlertsCardUpdate(ctx context.Context, app *config.AppContext, person, checkout, id string, apply bool, client alertsstripe.Client) (string, error) {
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return "", err
	}
	var reference, status string
	if checkout != "" {
		if _, err = alertsOwnedBinding(ctx, tx, person, checkout); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO alerts_card_updates(id,checkout_id) VALUES($1,$2) ON CONFLICT(id) DO NOTHING`, id, checkout); err != nil {
			return "", err
		}
	}
	err = tx.QueryRow(ctx, `SELECT u.checkout_id::text,coalesce(u.session_id,''),u.state FROM alerts_card_updates u JOIN alerts_stripe_checkouts c ON c.id=u.checkout_id WHERE u.id=$1 AND c.person_id=$2 FOR UPDATE OF u`, id, person).Scan(&checkout, &reference, &status)
	if err != nil {
		return "", err
	}
	b, err := alertsOwnedBinding(ctx, tx, person, checkout)
	if err != nil {
		return "", err
	}
	if status == "applied" {
		return "", tx.Commit(ctx)
	}
	if apply {
		if reference == "" {
			return "", alertsstripe.ErrSubscription
		}
		if err = client.ApplyCardUpdate(ctx, b, id, reference); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `UPDATE alerts_card_updates SET state='applied' WHERE id=$1`, id); err != nil {
			return "", err
		}
		return "", tx.Commit(ctx)
	}
	// Persist the id before provider I/O so a failed reply can be retried safely.
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	session, err := client.StartCardUpdate(ctx, b, id, app.Env.GetURI())
	if err != nil {
		return "", err
	}
	_, err = app.DB.Exec(ctx, `UPDATE alerts_card_updates SET session_id=$2 WHERE id=$1 AND (session_id IS NULL OR session_id=$2)`, id, session.ID)
	return session.URL, err
}

type AlertsBitcoinSwitch struct {
	ID, CheckoutID, State, CancellationState, PurchaseID string
	Quantity, PeriodEnd                                  int64
}

func AlertsBitcoinSwitchForPerson(ctx context.Context, app *config.AppContext, person, id string) (*AlertsBitcoinSwitch, error) {
	out := &AlertsBitcoinSwitch{}
	err := app.DB.QueryRow(ctx, `SELECT b.id::text,b.checkout_id::text,b.state,x.state,coalesce(b.purchase_id::text,''),b.quantity,b.period_end FROM alerts_bitcoin_switches b JOIN alerts_stripe_checkouts c ON c.id=b.checkout_id JOIN service_stripe_cancellations x ON x.id=b.id WHERE b.id=$1 AND c.person_id=$2`, id, person).Scan(&out.ID, &out.CheckoutID, &out.State, &out.CancellationState, &out.PurchaseID, &out.Quantity, &out.PeriodEnd)
	return out, err
}
func StartAlertsBitcoinSwitch(ctx context.Context, app *config.AppContext, person, checkout, id string, revision, end int64, client alertsstripe.Client) (string, error) {
	var current string
	err := app.DB.QueryRow(ctx, `SELECT b.id::text FROM alerts_bitcoin_switches b JOIN alerts_stripe_checkouts c ON c.id=b.checkout_id WHERE c.id=$1 AND c.person_id=$2 AND b.state<>'withdrawn'`, checkout, person).Scan(&current)
	if err == nil {
		return current, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	reservation, err := ReserveAlertsStripeCancellation(ctx, app, person, checkout, id, revision, end)
	if err != nil {
		return "", err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return "", err
	}
	b, err := alertsOwnedBinding(ctx, tx, person, checkout)
	if err != nil {
		return "", err
	}
	// An existing switch is recovered, not replaced by a second browser tab.
	var existing string
	err = tx.QueryRow(ctx, `SELECT id::text FROM alerts_bitcoin_switches WHERE checkout_id=$1 AND state<>'withdrawn'`, checkout).Scan(&existing)
	if err == nil {
		return existing, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO alerts_bitcoin_switches(id,checkout_id,quantity,period_end) VALUES($1,$2,$3,$4)`, reservation.Intent.ID, checkout, b.PaidSlots, end)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	// The existing cancellation worker also retries this durable instruction.
	_, _ = ProcessAlertsStripeCancellation(ctx, app, person, reservation.Intent.ID, client)
	return reservation.Intent.ID, nil
}

func WithdrawAlertsBitcoinSwitch(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) error {
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return err
	}
	var checkout, status, cancellation string
	var end int64
	var purchase *string
	err = tx.QueryRow(ctx, `SELECT b.checkout_id::text,b.state,x.state,b.period_end,b.purchase_id::text FROM alerts_bitcoin_switches b JOIN service_stripe_cancellations x ON x.id=b.id JOIN alerts_stripe_checkouts c ON c.id=b.checkout_id WHERE b.id=$1 AND c.person_id=$2 FOR UPDATE OF b,c`, id, person).Scan(&checkout, &status, &cancellation, &end, &purchase)
	if err != nil {
		return err
	}
	if status == "withdrawn" {
		return tx.Commit(ctx)
	}
	// Never restart card billing while a payable Bitcoin order exists. Such an
	// order may settle late; finish/resolve it before any provider switch.
	if cancellation != "confirmed" || end <= time.Now().Unix() {
		return alertsstripe.ErrSubscription
	}
	if purchase != nil {
		var order, orderStatus string
		var attempted bool
		err = tx.QueryRow(ctx, `SELECT o.id::text,o.status,EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=r.id) FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id WHERE r.id=$1 AND r.person_id=$2 FOR UPDATE OF o`, *purchase, person).Scan(&order, &orderStatus, &attempted)
		if err != nil {
			return err
		}
		if attempted || (orderStatus != "pending" && orderStatus != "cancelled") {
			return alertsstripe.ErrSubscription
		}
		if _, err = tx.Exec(ctx, `UPDATE shop_orders SET status='cancelled' WHERE id=$1`, order); err != nil {
			return err
		}
	}
	b, err := alertsOwnedBinding(ctx, tx, person, checkout)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_bitcoin_switches SET state='resuming' WHERE id=$1`, id); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	state, err := client.ResumeAfterBitcoinSwitch(ctx, b, id, end)
	if err != nil {
		return err
	}
	tx, err = app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return err
	}
	if err = saveAlertsStripeSubscriptionState(ctx, tx, b, state, client, time.Now()); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_bitcoin_switches SET state='withdrawn' WHERE id=$1 AND state='resuming'`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Refresh only an order that has never reached the payment provider. Initiated
// OpenNode charges must be reconciled, since they may settle after a browser exits.
func RefreshAlertsBitcoinSwitchOrder(ctx context.Context, app *config.AppContext, person, id string) error {
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return err
	}
	var purchase, order, status string
	var attempted bool
	err = tx.QueryRow(ctx, `SELECT b.purchase_id::text,o.id::text,o.status,EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=r.id) FROM alerts_bitcoin_switches b JOIN alerts_stripe_checkouts c ON c.id=b.checkout_id JOIN service_purchase_requests r ON r.id=b.purchase_id JOIN shop_orders o ON o.id=r.order_id WHERE b.id=$1 AND c.person_id=$2 AND b.state='scheduled' FOR UPDATE OF b,o`, id, person).Scan(&purchase, &order, &status, &attempted)
	if err != nil {
		return err
	}
	if attempted || (status != "pending" && status != "cancelled") {
		return alertsstripe.ErrSubscription
	}
	if _, err = tx.Exec(ctx, `UPDATE shop_orders SET status='cancelled' WHERE id=$1`, order); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_bitcoin_switches SET purchase_id=NULL WHERE id=$1 AND purchase_id=$2`, id, purchase); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

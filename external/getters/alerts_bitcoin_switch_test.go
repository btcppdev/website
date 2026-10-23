package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/servicebilling"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func TestAlertsBitcoinSwitchPurchasePostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Bitcoin switch test') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person); err != nil {
			t.Error(err)
		}
	})
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	stored, err := ReserveAlertsStripeCheckout(app, person, "switch-fixture-checkout", 2, "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	checkout := stored.Intent.ID
	end := time.Now().UTC().Truncate(time.Second).AddDate(1, 0, 0)
	binding := alertsstripe.Binding{IntentID: checkout, PersonID: person, SubscriptionID: "sub_" + checkout, CustomerID: "cus_" + checkout, PaidSlots: 2}
	raw, _ := json.Marshal(binding)
	snapshot, _ := json.Marshal(alertsstripe.SubscriptionState{State: "active", ProviderStatus: "active", PeriodStart: time.Now().Add(-time.Hour).Unix(), PeriodEnd: end.Unix(), CancelAtPeriodEnd: true})
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3)`, checkout, binding.SubscriptionID, raw); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_stripe_subscription_state(checkout_id,revision,snapshot,observed_at) VALUES($1,1,$2,now())`, checkout, snapshot); err != nil {
		t.Fatal(err)
	}
	change := uuid.NewString()
	if _, err = app.DB.Exec(ctx, `INSERT INTO service_stripe_cancellations(id,checkout_id,period_end,state) VALUES($1,$2,$3,'confirmed')`, change, checkout, end.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO alerts_bitcoin_switches(id,checkout_id,quantity,period_end) VALUES($1,$2,2,$3)`, change, checkout, end.Unix()); err != nil {
		t.Fatal(err)
	}
	revision, err := AlertsBillingRevision(app, person)
	if err != nil {
		t.Fatal(err)
	}
	future := end.AddDate(1, 0, 0)
	quote := ServiceQuote{ProductCode: servicebilling.AlertsProduct, Name: "Alerts annual Bitcoin", Provider: "opennode", Quantity: 2, UnitCents: 2400, TotalCents: 4800, PeriodStart: &end, PeriodEnd: &future, ExpiresAt: time.Now().Add(10 * time.Minute), BillingMonths: 12, AccountRevision: revision, AlertsSwitchID: change}
	for _, kind := range []string{"overlap", "wrong quantity", "unconfirmed", "resuming"} {
		q := quote
		switch kind {
		case "overlap":
			v := end.Add(-time.Hour)
			q.PeriodStart = &v
		case "wrong quantity":
			q.Quantity = 3
			q.TotalCents = 7200
		case "unconfirmed":
			q.AlertsSwitchID = uuid.NewString()
		case "resuming":
			if _, err = app.DB.Exec(ctx, `UPDATE alerts_bitcoin_switches SET state='resuming' WHERE id=$1`, change); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = CreateServicePurchase(app, person, uuid.NewString(), q); err == nil {
			t.Fatal("accepted", kind)
		}
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_bitcoin_switches SET state='scheduled' WHERE id=$1`, change); err != nil {
		t.Fatal(err)
	}
	purchase, err := CreateServicePurchase(app, person, "switch-purchase-unique", quote)
	if err != nil {
		t.Fatal(err)
	}
	again, err := CreateServicePurchase(app, person, "switch-purchase-unique", quote)
	if err != nil || again.OrderID != purchase.OrderID {
		t.Fatal("retry", again, err)
	}
	if _, err = CreateServicePurchase(app, person, "switch-purchase-second", quote); err == nil {
		t.Fatal("duplicate switch purchase")
	}
	var n int
	if err = app.DB.QueryRow(ctx, `SELECT count(*) FROM service_entitlements WHERE person_id=$1`, person).Scan(&n); err != nil || n != 0 {
		t.Fatal("unpaid access granted", n, err)
	}
	// Abandoning an unstarted quote permits a fresh checkout without reopening
	// the canceled order; once a provider attempt exists this is forbidden.
	if err = RefreshAlertsBitcoinSwitchOrder(ctx, app, person, change); err != nil {
		t.Fatal(err)
	}
	var oldStatus string
	if err = app.DB.QueryRow(ctx, `SELECT status FROM shop_orders WHERE id=$1`, purchase.OrderID).Scan(&oldStatus); err != nil || oldStatus != "cancelled" {
		t.Fatal(oldStatus, err)
	}
	quote.AccountRevision, err = AlertsBillingRevision(app, person)
	if err != nil {
		t.Fatal(err)
	}
	purchase, err = CreateServicePurchase(app, person, uuid.NewString(), quote)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0

	if _, err = BeginServicePayment(app, person, purchase.OrderID); err != nil {
		t.Fatal(err)
	}
	if err = RefreshAlertsBitcoinSwitchOrder(ctx, app, person, change); err == nil {
		t.Fatal("abandoned an initiated payment")
	}
	client := alertsstripe.Client{Subscription: func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) { calls++; return nil, nil }}
	if err = WithdrawAlertsBitcoinSwitch(ctx, app, person, change, client); err == nil || calls != 0 {
		t.Fatal("resumed with payable Bitcoin order", err, calls)
	}
	if _, err = AlertsBitcoinSwitchForPerson(ctx, app, uuid.NewString(), change); err == nil {
		t.Fatal("foreign switch disclosed")
	} // A paid switch becomes a renewable prepaid term. Renewing never overlaps
	// that term or resurrects the old canceled Stripe subscription.
	if _, err = app.DB.Exec(ctx, `UPDATE shop_orders SET status='paid' WHERE id=$1`, purchase.OrderID); err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(ctx, `INSERT INTO service_entitlements(purchase_id,person_id,product_code,quantity,starts_at,ends_at) VALUES($1,$2,'alerts.repository-slots',2,$3,$4)`, purchase.PurchaseID, person, end, future); err != nil {
		t.Fatal(err)
	}
	scheduled, err := AccountAccess(ctx, app, person, time.Now())
	if err != nil || len(scheduled.ScheduledEntitlements) != 1 || len(scheduled.Entitlements) != 0 {
		t.Fatal("future access not separated from active access", scheduled, err)
	}
	snapshot, _ = json.Marshal(alertsstripe.SubscriptionState{State: "cancelled", ProviderStatus: "canceled", PeriodEnd: end.Unix(), CancellationReason: "cancellation_requested"})
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET snapshot=$2,revision=revision+1 WHERE checkout_id=$1`, checkout, snapshot); err != nil {
		t.Fatal(err)
	}
	nSlots, renewAt, err := AlertsPrepaidRenewal(ctx, app, person, purchase.OrderID)
	if err != nil || nSlots != 2 || !renewAt.Equal(future) {
		t.Fatal(nSlots, renewAt, err)
	}
	revision, err = AlertsBillingRevision(app, person)
	if err != nil {
		t.Fatal(err)
	}
	nextEnd := future.AddDate(1, 0, 0)
	renewal := quote
	renewal.AlertsSwitchID = ""
	renewal.AlertsRenewOrderID = purchase.OrderID
	renewal.AccountRevision = revision
	renewal.PeriodStart = &future
	renewal.PeriodEnd = &nextEnd
	// A canceled card record with a later paid period must not allow overlap.
	later, _ := json.Marshal(alertsstripe.SubscriptionState{State: "cancelled", ProviderStatus: "canceled", PeriodEnd: nextEnd.Unix()})
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET snapshot=$2,revision=revision+1 WHERE checkout_id=$1`, checkout, later); err != nil {
		t.Fatal(err)
	}
	if _, err = CreateServicePurchase(app, person, uuid.NewString(), renewal); err == nil {
		t.Fatal("renewal overlaps card period")
	}
	if _, err = app.DB.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET snapshot=$2,revision=revision+1 WHERE checkout_id=$1`, checkout, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err = CreateServicePurchase(app, person, uuid.NewString(), renewal); err != nil {

		t.Fatal("annual renewal rejected", err)
	}

}

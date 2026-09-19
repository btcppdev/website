package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/servicestripe"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

func TestDeletedAccountCancellationPostgres(t *testing.T) {
	for _, product := range []string{"alerts.repository-slots", "insider.membership.monthly", "insider.membership.annual"} {
		for _, scenario := range []string{"lost reply", "unbound", "wrong customer", "open checkout", "expired checkout", "completed checkout"} {
			t.Run(product+"/"+scenario, func(t *testing.T) {
				app := databaseSmokeContext(t)
				ctx := context.Background()
				person := insertSmokePerson(t, app, "Deleted subscriber")
				admin := insertSmokePerson(t, app, "Deletion admin")
				if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, admin); err != nil {
					t.Fatal(err)
				}
				id := uuid.NewString()
				subID := "sub_" + id
				b := servicestripe.Binding{IntentID: id, PersonID: person, SubscriptionID: subID, CustomerID: "cus_" + id, PriceID: "price_" + id, ProductID: "prod_" + id, ProductCode: product, TaxCode: "txcd_fixture", Quantity: 1}
				amount := int64(2100)
				behavior, interval := "exclusive", "month"
				if product == "insider.membership.annual" {
					amount = 21000
					interval = "year"
				}
				if product == "alerts.repository-slots" {
					amount = 200
					behavior = "inclusive"
					b.TaxCode = "txcd_10103000"
					b.Quantity = 2
				}
				intent, _ := json.Marshal(servicestripe.CheckoutIntent{ID: id, PersonID: person, Email: "deleted@example.test", ProductCode: product, TaxCode: b.TaxCode, Quantity: b.Quantity, CreatedAt: time.Now(), ReturnOrigin: "https://insider.btcpp.dev", SuccessURL: "https://insider.btcpp.dev/account", CancelURL: "https://insider.btcpp.dev/account"})
				if product == "alerts.repository-slots" {
					intent, _ = json.Marshal(alertsstripe.Intent{ID: id, PersonID: person, Email: "deleted@example.test", PaidSlots: 2, CreatedAt: time.Now(), ReturnBaseURL: "https://btcpp.dev"})
				}
				if _, err := app.DB.Exec(ctx, `INSERT INTO service_stripe_checkouts(id,product_code,person_id,request_key,intent) VALUES($1,$2,$3,$4,$5)`, id, product, person, "deletion-"+id, intent); err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(b)
				if product == "alerts.repository-slots" {
					raw, _ = json.Marshal(alertsstripe.Binding{IntentID: id, PersonID: person, SubscriptionID: subID, CustomerID: b.CustomerID, PriceID: b.PriceID, ProductID: b.ProductID, PaidSlots: 2})
				}
				if scenario == "lost reply" || scenario == "wrong customer" {
					if _, err := app.DB.Exec(ctx, `INSERT INTO service_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3)`, id, subID, raw); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "open checkout" || scenario == "expired checkout" || scenario == "completed checkout" {
					if _, err := app.DB.Exec(ctx, `UPDATE service_stripe_checkouts SET stripe_session_id=$2 WHERE id=$1`, id, "cs_"+id); err != nil {
						t.Fatal(err)
					}
				}
				if err := DeletePerson(app, person, admin); err != nil {
					t.Fatal(err)
				}
				now := time.Now().Add(time.Second)
				price := &stripe.Price{ID: b.PriceID, Currency: "usd", UnitAmount: amount, TaxBehavior: stripe.PriceTaxBehavior(behavior), Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: stripe.PriceRecurringInterval(interval), IntervalCount: 1}, Product: &stripe.Product{ID: b.ProductID, TaxCode: &stripe.TaxCode{ID: b.TaxCode}}}
				sub := &stripe.Subscription{ID: subID, Customer: &stripe.Customer{ID: b.CustomerID}, Status: "active", Metadata: map[string]string{"account_id": person, "checkout_intent": id, "product": product}, Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{Price: price, Quantity: b.Quantity, CurrentPeriodStart: now.Add(-time.Hour).Unix(), CurrentPeriodEnd: now.Add(10 * 24 * time.Hour).Unix()}}}}
				if scenario == "wrong customer" {
					sub.Customer.ID = "cus_unrelated"
				}
				updates := 0
				client := alertsstripe.Client{Subscription: func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error) { return sub, nil }, UpdateSubscription: func(_ string, p *stripe.SubscriptionParams) (*stripe.Subscription, error) {
					updates++
					if p.CancelAtPeriodEnd == nil || !*p.CancelAtPeriodEnd || p.Items != nil {
						t.Fatal("unsafe update", p)
					}
					sub.CancelAtPeriodEnd = true
					sub.CancelAt = sub.Items.Data[0].CurrentPeriodEnd
					return nil, errors.New("lost response")
				}}
				session := &stripe.CheckoutSession{ID: "cs_" + id, Mode: "subscription", Status: "complete", ClientReferenceID: id, Metadata: sub.Metadata, Customer: sub.Customer, Subscription: sub, LineItems: &stripe.LineItemList{Data: []*stripe.LineItem{{Quantity: b.Quantity, Price: price}}}}
				if scenario == "open checkout" {
					session.Status = "open"
					session.Subscription = nil
				}
				if scenario == "expired checkout" {
					session.Status = "expired"
					session.Subscription = nil
				}
				client.Retrieve = func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return session, nil }
				expirations := 0
				client.Expire = func(reference string, p *stripe.CheckoutSessionExpireParams) (*stripe.CheckoutSession, error) {
					expirations++
					if reference != session.ID || p.IdempotencyKey == nil || *p.IdempotencyKey != "deleted-account-expire:"+id {
						t.Fatal("unsafe expiration")
					}
					session.Status = "expired"
					return session, nil
				}
				closed := scenario == "open checkout" || scenario == "expired checkout"
				if n, err := ReconcileDeletedAccountCancellation(ctx, app, id, client, now); n != 1 || (err == nil) != closed {
					t.Fatal("first attempt", n, err)
				}
				var state string
				if err := app.DB.QueryRow(ctx, `SELECT state FROM account_deletion_cancellations WHERE checkout_id=$1`, id).Scan(&state); err != nil {
					t.Fatal(err)
				}
				if closed {
					if state != "confirmed" || updates != 0 || (scenario == "open checkout" && expirations != 1) || (scenario == "expired checkout" && expirations != 0) {
						t.Fatal("checkout not closed correctly", state, updates, expirations)
					}
					return
				}
				if scenario != "lost reply" && scenario != "completed checkout" {
					if state != "review" || updates != 0 {
						t.Fatal("unsafe checkout mutated", state, updates)
					}
					return
				}
				if state != "pending" || updates != 1 {
					t.Fatal("ambiguous response lost", state, updates)
				}
				if n, err := ReconcileDeletedAccountCancellation(ctx, app, id, client, now); n != 0 || err != nil {
					t.Fatal("backoff bypassed", n, err)
				}
				if n, err := ReconcileDeletedAccountCancellation(ctx, app, id, client, now.Add(2*time.Minute)); n != 1 || err != nil {
					t.Fatal("retry", n, err)
				}
				if err := app.DB.QueryRow(ctx, `SELECT state FROM account_deletion_cancellations WHERE checkout_id=$1`, id).Scan(&state); err != nil || state != "confirmed" || updates != 1 {
					t.Fatal("cancellation not reconciled", state, updates, err)
				}
				if n, err := ReconcileDeletedAccountCancellation(ctx, app, id, client, now.Add(time.Hour)); n != 0 || err != nil {
					t.Fatal("confirmed job repeated", n, err)
				}
			})
		}
	}
}

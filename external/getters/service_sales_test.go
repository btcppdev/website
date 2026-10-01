package getters

import (
	"context"
	"errors"
	"testing"
	"time"

	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/servicestripe"
	"github.com/google/uuid"
)

func TestServiceSalesClosurePostgres(t *testing.T) {
	ctx := context.Background()
	zero := int64(0)
	app, refund, _ := bitcoinRefundFixture(t, &OpenNodeBitcoinEvidence{PriceSatoshis: 1001, OverpaidSatoshis: &zero})
	// Only the dedicated local test DB is modified; remove fixture-wide service
	// state afterwards so other acceptance tests remain independent.
	t.Cleanup(func() {
		if _, err := app.DB.Exec(ctx, `TRUNCATE service_sales_closures`); err != nil {
			t.Error(err)
		}
	})
	var actor, order string
	if err := app.DB.QueryRow(ctx, `SELECT r.person_id::text,r.order_id::text FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id WHERE a.id=$1`, refund.AttemptID).Scan(&actor, &order); err != nil {
		t.Fatal(err)
	}
	if err := StopServiceSales(ctx, app, actor, "alerts", "Permanent closure"); !errors.Is(err, ErrServiceRefundAdmin) {
		t.Fatal("nonadmin", err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, actor); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := StopServiceSales(ctx, app, actor, "alerts", "Permanent closure"); err != nil {
			t.Fatal(err)
		}
	}
	if err := StopServiceSales(ctx, app, actor, "alerts", "Changed reason"); !errors.Is(err, ErrServiceQuote) {
		t.Fatal("changed replay", err)
	}
	if _, err := CreateServicePurchase(app, actor, uuid.NewString(), ServiceQuote{ProductCode: "alerts.repository-slots"}); !errors.Is(err, ErrServiceSalesClosed) {
		t.Fatal("prepaid", err)
	}
	if _, err := BeginServicePayment(app, actor, order); !errors.Is(err, ErrServiceSalesClosed) {
		t.Fatal("old payment", err)
	}
	if _, err := ReserveAlertsStripeCheckout(app, actor, uuid.NewString(), 1, "https://btcpp.dev"); !errors.Is(err, ErrServiceSalesClosed) {
		t.Fatal("stripe reservation", err)
	}
	if _, err := OpenAlertsStripeCheckout(ctx, app, actor, uuid.NewString(), alertsstripe.Client{}); !errors.Is(err, ErrServiceSalesClosed) {
		t.Fatal("stripe URL", err)
	}
	if err := checkServiceSales(ctx, app.DB, "insider.membership.annual"); err != nil {
		t.Fatal("other service affected", err)
	}
	// A sales stop cannot discard financial evidence or block refunds.
	if err := RecordServiceRefund(ctx, app, refund); err != nil {
		t.Fatal("refund blocked", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	end := now.AddDate(1, 0, 0)
	pending, err := CreateServicePurchase(app, actor, uuid.NewString(), ServiceQuote{ProductCode: "insider.membership.annual", Name: "Pending payment before shutdown", Provider: "opennode", Quantity: 1, UnitCents: 1000, TotalCents: 1000, ExpiresAt: now.Add(time.Hour), PeriodStart: &now, PeriodEnd: &end})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := BeginServicePayment(app, actor, pending.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	reference := uuid.NewString()
	if err := BindServicePayment(app, attempt.ID, "opennode", reference); err != nil {
		t.Fatal(err)
	}
	if err := StopServiceSales(ctx, app, actor, "insider", "Permanent closure"); err != nil {
		t.Fatal(err)
	}
	if err := RecordServicePayment(app, VerifiedServicePayment{AttemptID: attempt.ID, Provider: "opennode", Reference: reference, EventID: "paid:" + reference, Currency: "USD", TotalCents: 1000, PaidAt: now, OpenNodeBitcoin: &OpenNodeBitcoinEvidence{PriceSatoshis: 1001, OverpaidSatoshis: &zero}}); err != nil {
		t.Fatal("in-flight settlement blocked", err)
	}
	if _, err := ReserveServiceStripeCheckout(app, actor, uuid.NewString(), servicestripe.CheckoutIntent{ProductCode: "insider.membership.annual"}); !errors.Is(err, ErrServiceSalesClosed) {
		t.Fatal("insider reserve", err)
	}
	if _, err := OpenServiceStripeCheckout(ctx, app, actor, uuid.NewString(), "insider.membership.annual", servicestripe.CheckoutClient{}); !errors.Is(err, ErrServiceSalesClosed) {
		t.Fatal("insider URL", err)
	}
	if _, err := app.DB.Exec(ctx, `UPDATE service_sales_closures SET reason='changed' WHERE service='alerts'`); err == nil {
		t.Fatal("audit changed")
	}
}

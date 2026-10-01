package getters

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestUnsettledServicePaymentsPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Unsettled payment report fixture') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	excluded := map[string]bool{}
	for _, kind := range []string{"missing", "opennode", "stripe", "expired", "settled", "other-service"} {
		provider, product := "stripe", "insider.article"
		if kind == "opennode" {
			provider = "opennode"
		}
		if kind == "other-service" {
			product = "alerts.repository-slots"
		}
		start := time.Now().UTC().Truncate(time.Second)
		end := start.AddDate(1, 0, 0)
		quote := ServiceQuote{ProductCode: product, ResourceID: uuid.NewString(), Name: "Report fixture", Provider: provider, Quantity: 1, UnitCents: 200, TotalCents: 200, ExpiresAt: start.Add(time.Hour)}
		if kind == "other-service" {
			quote.PeriodStart, quote.PeriodEnd = &start, &end
		}
		purchase, err := CreateServicePurchase(app, person, uuid.NewString(), quote)
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := BeginServicePayment(app, person, purchase.OrderID)
		if err != nil {
			t.Fatal(err)
		}
		ref := "cs_" + attempt.ID
		if provider == "opennode" {
			ref = uuid.NewString()
		}
		if kind != "missing" {
			if err := BindServicePayment(app, attempt.ID, provider, ref); err != nil {
				t.Fatal(err)
			}
		}
		switch kind {
		case "expired":
			if _, err := app.DB.Exec(ctx, `INSERT INTO service_payment_checkout_expirations(attempt_id,provider_reference,evidence) VALUES($1,$2,'{}')`, attempt.ID, ref); err != nil {
				t.Fatal(err)
			}
			excluded[attempt.ID] = true
		case "settled":
			if err := RecordServicePayment(app, VerifiedServicePayment{AttemptID: attempt.ID, Provider: provider, Reference: ref, EventID: uuid.NewString(), Currency: "USD", TotalCents: 200, PaidAt: start}); err != nil {
				t.Fatal(err)
			}
			excluded[attempt.ID] = true
		case "other-service":
			excluded[attempt.ID] = true
		case "missing":
			want[attempt.ID] = "provider_reference_missing"
		case "opennode":
			want[attempt.ID] = "opennode_charge_requires_reconciliation"
		case "stripe":
			want[attempt.ID] = "stripe_payment_requires_reconciliation"
		}
	}
	seen := map[string]bool{}
	after := ""
	for {
		page, err := pageUnsettledServicePayments(ctx, app, "insider", after, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if seen[item.AttemptID] || excluded[item.AttemptID] {
				t.Fatal("duplicate or excluded attempt", item)
			}
			seen[item.AttemptID] = true
			if reason, ok := want[item.AttemptID]; ok && reason != item.ReviewReason {
				t.Fatal("incorrect review reason", item)
			}
		}
		if page.Next == "" {
			break
		}
		if _, err := pageUnsettledServicePayments(ctx, app, "alerts", page.Next, 2); err != ErrServiceRefundCursor {
			t.Fatal("cross-service cursor accepted", err)
		}
		after = page.Next
	}
	for id := range want {
		if !seen[id] {
			t.Fatal("missing attempt", id)
		}
	}
	if _, err := PageUnsettledServicePayments(ctx, app, "insider", "invalid"); err != ErrServiceRefundCursor {
		t.Fatal("invalid cursor accepted", err)
	}
}

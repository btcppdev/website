package alertsstripe

import (
	"context"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

func TestVoidedIncrease(t *testing.T) {
	for _, scenario := range []string{"void", "open", "uncollectible", "pending", "applied", "foreign", "scheduled"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, sub, _, invoice, now, _ := increaseFixture(t)
			q, err := c.PreviewIncrease(context.Background(), b, 3, now)
			if err != nil {
				t.Fatal(err)
			}
			invoice.ID = "in_void_upgrade"
			invoice.Status = "void"
			invoice.StatusTransitions = nil
			invoice.AmountRemaining = 0
			c.Invoice = func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return invoice, nil }
			switch scenario {
			case "open":
				invoice.Status = "open"
				invoice.AmountRemaining = invoice.Total
			case "uncollectible":
				invoice.Status = "uncollectible"
			case "pending":
				sub.PendingUpdate = &stripe.SubscriptionPendingUpdate{}
			case "applied":
				sub.Items.Data[0].Quantity = 3
			case "foreign":
				sub.Metadata["account_id"] = "other"
			case "scheduled":
				sub.Schedule = &stripe.SubscriptionSchedule{ID: "sub_sched_other"}
			}
			state, err := c.VoidedIncrease(context.Background(), invoice.ID, b, IncreaseIntent{ID: uuid.NewString(), Preview: *q})
			if scenario == "void" {
				if err != nil || state.State != "active" {
					t.Fatal(state, err)
				}
			} else if err == nil {
				t.Fatal("released unresolved upgrade", state)
			}
		})
	}
}

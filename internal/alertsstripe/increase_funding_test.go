package alertsstripe

import (
	"context"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

func TestIncreaseFundingRejectsInvalidPriorPayment(t *testing.T) {
	for _, scenario := range []string{"valid", "unpaid", "wrong period", "wrong quantity", "wrong invoice", "nested history", "credited"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, sub, _, invoice, now, _ := increaseFixture(t)
			prior, err := c.PreviewIncrease(context.Background(), b, 3, now)
			if err != nil {
				t.Fatal(err)
			}
			invoice.ID = "in_prior_upgrade"
			invoice.Status = "paid"
			invoice.AmountPaid = 100
			invoice.AmountRemaining = 0
			invoice.StatusTransitions = &stripe.InvoiceStatusTransitions{PaidAt: now.Unix()}
			proof := &IncreaseFunding{InvoiceID: invoice.ID, Prior: &IncreaseIntent{ID: uuid.NewString(), Preview: *prior}}
			c.Invoice = func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return invoice, nil }
			b.PaidSlots = 3
			sub.Items.Data[0].Quantity = 3
			state := SubscriptionState{PeriodStart: prior.PeriodStart, PeriodEnd: prior.PeriodEnd, LatestInvoice: invoice.ID}
			switch scenario {
			case "unpaid":
				invoice.Status = "open"
			case "wrong period":
				proof.Prior.Preview.PeriodEnd++
			case "wrong quantity":
				proof.Prior.Preview.PaidSlots++
			case "wrong invoice":
				proof.InvoiceID = "in_other"
			case "nested history":
				proof.Prior.Preview.Funding = &IncreaseFunding{InvoiceID: "in_nested"}
			case "credited":
				invoice.PostPaymentCreditNotesAmount = 1
			}
			err = c.verifyIncreaseFunding(context.Background(), b, state, proof, now)
			if (err == nil) != (scenario == "valid") {
				t.Fatal(scenario, err)
			}
		})
	}
}

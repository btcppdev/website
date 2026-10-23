package servicestripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

func TestRefundSyncLocatesSavedPayment(t *testing.T) {
	for _, reference := range []string{"cs_saved", "in_saved"} {
		c := RefundSyncClient{Verify: RefundClient{
			Checkout: func(id string, p *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
				return &stripe.CheckoutSession{ID: id, PaymentIntent: &stripe.PaymentIntent{ID: "pi_saved"}}, nil
			},
			Invoice: func(id string, p *stripe.InvoiceParams) (*stripe.Invoice, error) {
				return &stripe.Invoice{ID: id, Payments: &stripe.InvoicePaymentList{Data: []*stripe.InvoicePayment{{Payment: &stripe.InvoicePaymentPayment{Type: "payment_intent", PaymentIntent: &stripe.PaymentIntent{ID: "pi_saved"}}}}}}, nil
			},
		}, RefundIDs: func(ctx context.Context, pi, ch string) ([]string, error) {
			if pi != "pi_saved" || ch != "" {
				t.Fatal(pi, ch)
			}
			return []string{"re_saved"}, nil
		}}
		ids, err := c.PaymentRefundIDs(context.Background(), reference)
		if err != nil || len(ids) != 1 {
			t.Fatal(ids, err)
		}
		c.Verify.Live = true
		if _, err = c.PaymentRefundIDs(context.Background(), reference); err == nil {
			t.Fatal("wrong mode accepted")
		}
	}
}

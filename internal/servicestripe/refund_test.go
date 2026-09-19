package servicestripe

import (
	"context"
	"errors"
	"testing"

	"github.com/stripe/stripe-go/v86"
)

func TestRefundVerification(t *testing.T) {
	for _, reference := range []string{"cs_saved", "in_saved"} {
		t.Run(reference, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				change func(*RefundClient, *stripe.Refund, *stripe.Charge, *stripe.CheckoutSession, *stripe.Invoice)
			}{
				{"valid", nil},
				{"pending", func(c *RefundClient, r *stripe.Refund, ch *stripe.Charge, s *stripe.CheckoutSession, i *stripe.Invoice) {
					r.Status = "pending"
				}},
				{"wrong refund", func(c *RefundClient, r *stripe.Refund, ch *stripe.Charge, s *stripe.CheckoutSession, i *stripe.Invoice) {
					r.ID = "re_other"
				}},
				{"wrong charge", func(c *RefundClient, r *stripe.Refund, ch *stripe.Charge, s *stripe.CheckoutSession, i *stripe.Invoice) {
					ch.ID = "ch_other"
				}},
				{"wrong mode", func(c *RefundClient, r *stripe.Refund, ch *stripe.Charge, s *stripe.CheckoutSession, i *stripe.Invoice) {
					ch.Livemode = true
				}},
				{"uncaptured", func(c *RefundClient, r *stripe.Refund, ch *stripe.Charge, s *stripe.CheckoutSession, i *stripe.Invoice) {
					ch.Captured = false
				}},
				{"wrong currency", func(c *RefundClient, r *stripe.Refund, ch *stripe.Charge, s *stripe.CheckoutSession, i *stripe.Invoice) {
					r.Currency = "eur"
				}},
				{"excess amount", func(c *RefundClient, r *stripe.Refund, ch *stripe.Charge, s *stripe.CheckoutSession, i *stripe.Invoice) {
					r.Amount = 2201
				}},
				{"refund PI mismatch", func(c *RefundClient, r *stripe.Refund, ch *stripe.Charge, s *stripe.CheckoutSession, i *stripe.Invoice) {
					r.PaymentIntent = &stripe.PaymentIntent{ID: "pi_other"}
				}},
				{"paid object mismatch", func(c *RefundClient, r *stripe.Refund, ch *stripe.Charge, s *stripe.CheckoutSession, i *stripe.Invoice) {
					s.PaymentIntent = &stripe.PaymentIntent{ID: "pi_other"}
					i.Payments.Data[0].Payment.PaymentIntent = &stripe.PaymentIntent{ID: "pi_other"}
				}},
				{"paid object total", func(c *RefundClient, r *stripe.Refund, ch *stripe.Charge, s *stripe.CheckoutSession, i *stripe.Invoice) {
					s.AmountTotal = 2300
					i.Total = 2300
				}},
				{"provider failure", func(c *RefundClient, r *stripe.Refund, ch *stripe.Charge, s *stripe.CheckoutSession, i *stripe.Invoice) {
					c.Refund = func(string, *stripe.RefundParams) (*stripe.Refund, error) {
						return nil, errors.New("provider unavailable")
					}
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					pi := &stripe.PaymentIntent{ID: "pi_saved"}
					refund := &stripe.Refund{ID: "re_saved", Status: "succeeded", Amount: 500, Currency: "usd", Created: 1789820000, Charge: &stripe.Charge{ID: "ch_saved"}, PaymentIntent: pi}
					charge := &stripe.Charge{ID: "ch_saved", Paid: true, Captured: true, Amount: 2200, AmountCaptured: 2200, AmountRefunded: 500, Currency: "usd", PaymentIntent: pi}
					session := &stripe.CheckoutSession{ID: "cs_saved", Mode: "payment", Status: "complete", PaymentStatus: "paid", Currency: "usd", AmountTotal: 2200, PaymentIntent: pi}
					invoice := &stripe.Invoice{ID: "in_saved", Status: "paid", Currency: "usd", Total: 2200, AmountPaid: 2200, Payments: &stripe.InvoicePaymentList{Data: []*stripe.InvoicePayment{{ID: "inpay_saved", Invoice: &stripe.Invoice{ID: "in_saved"}, Status: "paid", Currency: "usd", AmountPaid: 2200, Payment: &stripe.InvoicePaymentPayment{Type: "payment_intent", PaymentIntent: pi}}}}}
					checkContext := func(ctx context.Context) {
						t.Helper()
						if ctx == nil {
							t.Fatal("missing provider context")
						}
						if _, ok := ctx.Deadline(); !ok {
							t.Fatal("missing provider deadline")
						}
					}
					client := RefundClient{
						Refund: func(id string, p *stripe.RefundParams) (*stripe.Refund, error) {
							checkContext(p.Context)
							return refund, nil
						},
						Charge: func(id string, p *stripe.ChargeParams) (*stripe.Charge, error) {
							checkContext(p.Context)
							return charge, nil
						},
						Checkout: func(id string, p *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
							checkContext(p.Context)
							return session, nil
						},
						Invoice: func(id string, p *stripe.InvoiceParams) (*stripe.Invoice, error) {
							checkContext(p.Context)
							if len(p.Expand) != 1 || *p.Expand[0] != "payments" {
								t.Fatal("invoice payment evidence not expanded")
							}
							return invoice, nil
						},
					}
					if tc.change != nil {
						tc.change(&client, refund, charge, session, invoice)
					}
					out, err := client.Verify(context.Background(), "re_saved", reference, 2200)
					if tc.change == nil {
						if err != nil || out.AmountCents != 500 || out.PaymentReference != reference || out.RefundID != "re_saved" || out.ProviderCreatedAt.Unix() != refund.Created {
							t.Fatal(out, err)
						}
					} else if err == nil {
						t.Fatal("mismatched refund accepted", out)
					}
				})
			}
		})
	}
}

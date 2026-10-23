package servicestripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/charge"
	"github.com/stripe/stripe-go/v86/checkout/session"
	"github.com/stripe/stripe-go/v86/invoice"
	"github.com/stripe/stripe-go/v86/invoicepayment"
	"github.com/stripe/stripe-go/v86/refund"
	"strings"
)

// RefundSyncClient only retrieves provider data; it cannot issue a refund.
type RefundSyncClient struct {
	Verify     RefundClient
	RefundIDs  func(context.Context, string, string) ([]string, error) // payment intent or charge
	References func(context.Context, string) ([]string, error)
}

func NewRefundSyncClient(live bool) RefundSyncClient {
	return RefundSyncClient{
		Verify: RefundClient{Refund: refund.Get, Charge: charge.Get, Checkout: session.Get, Invoice: invoice.Get, Live: live},
		RefundIDs: func(ctx context.Context, pi, ch string) ([]string, error) {
			p := &stripe.RefundListParams{}
			p.Context = ctx
			p.Limit = stripe.Int64(100)
			if pi != "" {
				p.PaymentIntent = stripe.String(pi)
			} else if ch != "" {
				p.Charge = stripe.String(ch)
			} else {
				return nil, ErrRefund
			}
			it := refund.List(p)
			var ids []string
			for it.Next() {
				if len(ids) >= 1000 {
					return nil, ErrRefund
				}
				ids = append(ids, it.Refund().ID)
			}
			return ids, it.Err()
		},
		References: func(ctx context.Context, pi string) ([]string, error) {
			if !strings.HasPrefix(pi, "pi_") {
				return nil, ErrRefund
			}
			var refs []string
			p := &stripe.InvoicePaymentListParams{Payment: &stripe.InvoicePaymentListPaymentParams{Type: stripe.String("payment_intent"), PaymentIntent: stripe.String(pi)}, Status: stripe.String("paid")}
			p.Context = ctx
			p.Limit = stripe.Int64(100)
			it := invoicepayment.List(p)
			for it.Next() {
				if len(refs) >= 1000 {
					return nil, ErrRefund
				}
				if v := it.InvoicePayment(); v.Invoice != nil {
					refs = append(refs, v.Invoice.ID)
				}
			}
			if err := it.Err(); err != nil {
				return nil, err
			}
			sp := &stripe.CheckoutSessionListParams{PaymentIntent: stripe.String(pi)}
			sp.Context = ctx
			sp.Limit = stripe.Int64(100)
			sit := session.List(sp)
			for sit.Next() {
				if len(refs) >= 1000 {
					return nil, ErrRefund
				}
				refs = append(refs, sit.CheckoutSession().ID)
			}
			return refs, sit.Err()
		}}
}

// PaymentRefundIDs locates refunds from our saved payment reference, including
// old payments whose refund webhook was never delivered.
func (c RefundSyncClient) PaymentRefundIDs(ctx context.Context, reference string) ([]string, error) {
	var pi, ch string
	switch {
	case strings.HasPrefix(reference, "cs_"):
		p := &stripe.CheckoutSessionParams{}
		p.Context = ctx
		v, err := c.Verify.Checkout(reference, p)
		if err != nil {
			return nil, err
		}
		if v == nil || v.ID != reference || v.Livemode != c.Verify.Live || v.PaymentIntent == nil {
			return nil, ErrRefund
		}
		pi = v.PaymentIntent.ID
	case strings.HasPrefix(reference, "in_"):
		p := &stripe.InvoiceParams{}
		p.Context = ctx
		p.AddExpand("payments")
		v, err := c.Verify.Invoice(reference, p)
		if err != nil {
			return nil, err
		}
		if v == nil || v.ID != reference || v.Livemode != c.Verify.Live || v.Payments == nil || v.Payments.HasMore || len(v.Payments.Data) != 1 {
			return nil, ErrRefund
		}
		pay := v.Payments.Data[0]
		if pay == nil || pay.Payment == nil {
			return nil, ErrRefund
		}
		if pay.Payment.Type == "payment_intent" && pay.Payment.PaymentIntent != nil {
			pi = pay.Payment.PaymentIntent.ID
		}
		if pay.Payment.Type == "charge" && pay.Payment.Charge != nil {
			ch = pay.Payment.Charge.ID
		}
	default:
		return nil, ErrRefund
	}
	if pi == "" && ch == "" {
		return nil, ErrRefund
	}
	return c.RefundIDs(ctx, pi, ch)
}

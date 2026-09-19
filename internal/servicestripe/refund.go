package servicestripe

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v86"
)

var ErrRefund = errors.New("Stripe refund does not match saved payment")

type RefundClient struct {
	Refund   func(string, *stripe.RefundParams) (*stripe.Refund, error)
	Charge   func(string, *stripe.ChargeParams) (*stripe.Charge, error)
	Checkout func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error)
	Invoice  func(string, *stripe.InvoiceParams) (*stripe.Invoice, error)
	Live     bool
}

type RefundEvidence struct {
	RefundID, PaymentReference, Reason string
	AmountCents                        int64
	// Stripe supplies refund creation time, not a completion timestamp.
	ProviderCreatedAt time.Time
}

// Verify retrieves provider objects; webhook bodies are only lookup hints.
// Saved payment reference and total must come from the immutable local ledger.
func (c RefundClient) Verify(ctx context.Context, refundID, reference string, total int64) (RefundEvidence, error) {
	var out RefundEvidence
	if c.Refund == nil || c.Charge == nil || !strings.HasPrefix(refundID, "re_") || total <= 0 {
		return out, ErrRefund
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rp := &stripe.RefundParams{}
	rp.Context = ctx
	refund, err := c.Refund(refundID, rp)
	if err != nil {
		return out, err
	}
	if refund == nil || refund.ID != refundID || refund.Status != "succeeded" || refund.Currency != "usd" || refund.Amount <= 0 || refund.Amount > total || refund.Created <= 0 || refund.Charge == nil || !strings.HasPrefix(refund.Charge.ID, "ch_") {
		return out, ErrRefund
	}
	cp := &stripe.ChargeParams{}
	cp.Context = ctx
	charge, err := c.Charge(refund.Charge.ID, cp)
	if err != nil {
		return out, err
	}
	if charge == nil || charge.ID != refund.Charge.ID || charge.Livemode != c.Live || !charge.Paid || !charge.Captured || charge.Currency != "usd" || charge.Amount != total || charge.AmountCaptured != total || charge.AmountRefunded < refund.Amount || charge.AmountRefunded > total {
		return out, ErrRefund
	}
	if refund.PaymentIntent != nil && (charge.PaymentIntent == nil || refund.PaymentIntent.ID != charge.PaymentIntent.ID) {
		return out, ErrRefund
	}
	switch {
	case strings.HasPrefix(reference, "cs_"):
		if c.Checkout == nil {
			return out, ErrRefund
		}
		p := &stripe.CheckoutSessionParams{}
		p.Context = ctx
		session, err := c.Checkout(reference, p)
		if err != nil {
			return out, err
		}
		if session == nil || session.ID != reference || session.Livemode != c.Live || session.Mode != "payment" || session.Status != "complete" || session.PaymentStatus != "paid" || session.Currency != "usd" || session.AmountTotal != total || session.PaymentIntent == nil || charge.PaymentIntent == nil || session.PaymentIntent.ID != charge.PaymentIntent.ID {
			return out, ErrRefund
		}
	case strings.HasPrefix(reference, "in_"):
		if c.Invoice == nil {
			return out, ErrRefund
		}
		p := &stripe.InvoiceParams{}
		p.Context = ctx
		p.AddExpand("payments")
		invoice, err := c.Invoice(reference, p)
		if err != nil {
			return out, err
		}
		if invoice == nil || invoice.ID != reference || invoice.Livemode != c.Live || invoice.Status != "paid" || invoice.Currency != "usd" || invoice.Total != total || invoice.AmountPaid != total || invoice.Payments == nil || invoice.Payments.HasMore || len(invoice.Payments.Data) != 1 {
			return out, ErrRefund
		}
		payment := invoice.Payments.Data[0]
		if payment == nil || payment.Invoice == nil || payment.Invoice.ID != reference || payment.Livemode != c.Live || payment.Status != "paid" || payment.Currency != "usd" || payment.AmountPaid != total || payment.Payment == nil {
			return out, ErrRefund
		}
		switch payment.Payment.Type {
		case "payment_intent":
			if payment.Payment.PaymentIntent == nil || charge.PaymentIntent == nil || payment.Payment.PaymentIntent.ID != charge.PaymentIntent.ID {
				return out, ErrRefund
			}
		case "charge":
			if payment.Payment.Charge == nil || payment.Payment.Charge.ID != charge.ID {
				return out, ErrRefund
			}
		default:
			return out, ErrRefund
		}
	default:
		return out, ErrRefund
	}
	return RefundEvidence{RefundID: refund.ID, PaymentReference: reference, Reason: string(refund.Reason), AmountCents: refund.Amount, ProviderCreatedAt: time.Unix(refund.Created, 0).UTC()}, nil
}

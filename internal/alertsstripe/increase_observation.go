package alertsstripe

import (
	"context"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v86"
)

// IncreaseInvoiceObservation identifies a verified invoice, not an access grant.
type IncreaseInvoiceObservation struct {
	InvoiceID string
	Status    string
}

// LatestIncreaseInvoice recovers an invoice reference from authenticated
// subscription state. It neither replaces a pending update nor attempts payment.
func (c Client) LatestIncreaseInvoice(ctx context.Context, b Binding, intent IncreaseIntent) (*IncreaseInvoiceObservation, error) {
	if c.Subscription == nil || c.Live != b.Live {
		return nil, ErrSubscription
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	p := &stripe.SubscriptionParams{}
	p.Context = ctx
	p.AddExpand("items.data.price.product")
	sub, err := c.Subscription(b.SubscriptionID, p)
	if err != nil {
		return nil, err
	}
	state, err := verifySubscription(sub, b)
	if err != nil {
		updated := b
		updated.PaidSlots = intent.Preview.PaidSlots
		state, err = verifySubscription(sub, updated)
		if err != nil {
			return nil, err
		}
	}
	if state.LatestInvoice == "" {
		return nil, ErrInvoice
	}
	return c.InspectIncreaseInvoice(ctx, state.LatestInvoice, b, intent)
}

// InspectIncreaseInvoice checks an existing invoice against the frozen review.
// Terminal statuses are evidence for the coordinator; callers must also check
// subscription state before releasing an unresolved upgrade instruction.
func (c Client) InspectIncreaseInvoice(ctx context.Context, id string, b Binding, intent IncreaseIntent) (*IncreaseInvoiceObservation, error) {
	if !strings.HasPrefix(id, "in_") || len(id) > 255 || c.Invoice == nil || c.Price == nil || c.Live != b.Live {
		return nil, ErrInvoice
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ip := &stripe.InvoiceParams{}
	ip.Context = ctx
	in, err := c.Invoice(id, ip)
	if err != nil {
		return nil, err
	}
	if in == nil || in.ID != id {
		return nil, ErrInvoice
	}
	pp := &stripe.PriceParams{}
	pp.Context = ctx
	pp.AddExpand("product")
	price, err := c.Price(b.PriceID, pp)
	if err != nil {
		return nil, err
	}
	if err = verifyReviewedIncreaseInvoice(in, price, b, intent); err != nil {
		return nil, err
	}
	switch in.Status {
	case "paid":
		if _, err = VerifyIncreaseInvoice(in, price, b, intent); err != nil {
			return nil, err
		}
	case "open":
		if in.AmountPaid != 0 || in.AmountRemaining != in.Total || (in.StatusTransitions != nil && in.StatusTransitions.PaidAt != 0) {
			return nil, ErrInvoice
		}
	case "void", "uncollectible":
		if in.AmountPaid != 0 || (in.StatusTransitions != nil && in.StatusTransitions.PaidAt != 0) {
			return nil, ErrInvoice
		}
	default:
		return nil, ErrInvoice
	}
	return &IncreaseInvoiceObservation{InvoiceID: in.ID, Status: string(in.Status)}, nil
}

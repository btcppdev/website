package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v86"
)

// Called only after signature verification. Older webhook versions omit the
// invoice parent field used for service routing. Retrieve the current invoice
// with our SDK version; the existing settlement validators still verify payment.
func normalizeStripeInvoiceEvent(ctx context.Context, event stripe.Event, fetch func(string, *stripe.InvoiceParams) (*stripe.Invoice, error)) (stripe.Event, error) {
	if event.Type != "invoice.paid" && event.Type != "invoice.payment_failed" {
		return event, nil
	}
	if event.Data == nil {
		return event, errors.New("missing invoice event data")
	}
	var in stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &in); err != nil {
		return event, err
	}
	if in.Parent != nil {
		return event, nil
	}
	if !strings.HasPrefix(in.ID, "in_") || len(in.ID) > 255 {
		return event, errors.New("invalid invoice event reference")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	params := &stripe.InvoiceParams{}
	params.Context = ctx
	current, err := fetch(in.ID, params)
	if err != nil {
		return event, err
	}
	if current == nil || current.ID != in.ID || current.Livemode != event.Livemode {
		return event, errors.New("invoice event lookup mismatch")
	}
	raw, err := json.Marshal(current)
	if err != nil {
		return event, err
	}
	data := *event.Data
	data.Raw = raw
	event.Data = &data
	return event, nil
}

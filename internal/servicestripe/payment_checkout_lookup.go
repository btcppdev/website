package servicestripe

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

// FindPaymentCheckout recovers a lost creation response without creating a new
// session. An empty or incomplete scan never establishes that payment is absent.
func (c CheckoutClient) FindPaymentCheckout(ctx context.Context, in PaymentCheckout, createdAt, expiresAt time.Time) (string, error) {
	for _, raw := range []string{in.AttemptID, in.OrderID} {
		if id, err := uuid.Parse(raw); err != nil || id.String() != raw {
			return "", ErrCheckout
		}
	}
	if in.Reference != "" || in.TotalCents <= 0 || createdAt.IsZero() || !expiresAt.After(createdAt) || c.List == nil {
		return "", ErrCheckout
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	from, to := createdAt.Add(-time.Minute).Unix(), expiresAt.Add(time.Minute).Unix()
	cursor, found := "", ""
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		p := &stripe.CheckoutSessionListParams{CreatedRange: &stripe.RangeQueryParams{GreaterThanOrEqual: from, LesserThanOrEqual: to}}
		p.Context = ctx
		p.Limit = stripe.Int64(100)
		p.Single = true
		if cursor != "" {
			p.StartingAfter = stripe.String(cursor)
		}
		result, err := c.List(p)
		if err != nil {
			return "", err
		}
		if result == nil || (result.HasMore && len(result.Data) == 0) {
			return "", ErrReconcile
		}
		for _, s := range result.Data {
			if s == nil || !strings.HasPrefix(s.ID, "cs_") || len(s.ID) > 255 || seen[s.ID] {
				return "", ErrReconcile
			}
			seen[s.ID], cursor = true, s.ID
			if s.Metadata["service-attempt-id"] != in.AttemptID {
				continue
			}
			if s.Metadata["service-order-id"] != in.OrderID || s.Metadata["checkout-kind"] != "service" || s.Livemode != c.Live || s.Mode != "payment" || s.Currency != "usd" || s.AmountTotal != in.TotalCents || s.Subscription != nil || s.Created < from || s.Created > to || found != "" {
				return "", ErrCheckout
			}
			found = s.ID
		}
		if !result.HasMore {
			if found == "" {
				return "", ErrReconcile
			}
			return found, nil
		}
	}
	return "", ErrReconcile
}

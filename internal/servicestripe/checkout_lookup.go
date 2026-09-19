package servicestripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"time"
)

// FindExistingCheckout only reads authenticated provider state. A complete,
// bounded scan must find exactly one matching frozen intent. No match does not
// prove that an in-flight creation cannot still complete, so it requires retry.
func (c CheckoutClient) FindExistingCheckout(ctx context.Context, in CheckoutIntent) (string, error) {
	if _, err := CheckoutParams(ctx, in, in.CreatedAt); err != nil {
		return "", err
	}
	if c.List == nil {
		return "", ErrReconcile
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	from, to := in.CreatedAt.Add(-time.Minute).Unix(), in.CreatedAt.Add(2*time.Hour).Unix()
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
			if s == nil || !strings.HasPrefix(s.ID, "cs_") || seen[s.ID] {
				return "", ErrReconcile
			}
			seen[s.ID] = true
			cursor = s.ID
			if s.ClientReferenceID != in.ID && s.Metadata["checkout_intent"] != in.ID {
				continue
			}
			if s.ClientReferenceID != in.ID || s.Metadata["checkout_intent"] != in.ID || s.Metadata["account_id"] != in.PersonID || s.Metadata["product"] != in.ProductCode || s.Livemode != c.Live || s.Mode != "subscription" || s.Created < from || s.Created > to || found != "" {
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

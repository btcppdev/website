package alertsstripe

import (
	"btcpp-web/internal/servicestripe"
	"context"
	"github.com/stripe/stripe-go/v86"
)

var ErrSubscription = servicestripe.ErrSubscription

type SubscriptionState = servicestripe.SubscriptionState

func (c Client) CurrentSubscription(ctx context.Context, b Binding) (SubscriptionState, error) {
	return (servicestripe.SubscriptionClient{Subscription: c.Subscription, Live: c.Live}).CurrentSubscription(ctx, serviceBinding(b))
}
func verifySubscription(s *stripe.Subscription, b Binding) (SubscriptionState, error) {
	return servicestripe.VerifySubscription(s, serviceBinding(b))
}

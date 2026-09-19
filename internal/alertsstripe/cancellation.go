package alertsstripe

import (
	"btcpp-web/internal/servicestripe"
	"context"
	"time"
)

type CancellationIntent = servicestripe.CancellationIntent

func (c Client) ScheduleCancellation(ctx context.Context, b Binding, in CancellationIntent, now time.Time) (SubscriptionState, error) {
	return (servicestripe.SubscriptionClient{Subscription: c.Subscription, UpdateSubscription: c.UpdateSubscription, Live: c.Live}).ScheduleCancellation(ctx, serviceBinding(b), in, now)
}

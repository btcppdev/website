package alertsstripe

import (
	"btcpp-web/internal/servicestripe"
	"context"
)

type CheckoutState = servicestripe.CheckoutState

func (c Client) ExpiredUnpaid(ctx context.Context, in Intent, reference string) error {
	return (servicestripe.CheckoutClient{Retrieve: c.Retrieve, Live: c.Live}).ExpiredUnpaid(ctx, serviceCheckoutIntent(in), reference)
}
func (c Client) CheckoutStatus(ctx context.Context, in Intent, reference string) (CheckoutState, error) {
	return (servicestripe.CheckoutClient{Retrieve: c.Retrieve, Live: c.Live}).CheckoutStatus(ctx, serviceCheckoutIntent(in), reference)
}

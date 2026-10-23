package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"context"
	"encoding/json"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"time"
)

// Called only after StripeCallback authenticates the webhook signature.
func handleServiceStripeRefundEvent(ctx context.Context, app *config.AppContext, event stripe.Event) (bool, error) {
	switch event.Type {
	case "refund.created", "refund.updated", "refund.failed", "charge.refunded":
	default:
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	live := strings.HasPrefix(app.Env.StripeKey, "sk_live_") || strings.HasPrefix(app.Env.StripeKey, "rk_live_")
	if event.Livemode != live || event.Data == nil {
		return true, servicestripe.ErrRefund
	}
	c := servicestripe.NewRefundSyncClient(live)
	if event.Type == "charge.refunded" {
		var ch stripe.Charge
		if err := json.Unmarshal(event.Data.Raw, &ch); err != nil {
			return true, err
		}
		if !strings.HasPrefix(ch.ID, "ch_") {
			return true, servicestripe.ErrRefund
		}
		ids, err := c.RefundIDs(ctx, "", ch.ID)
		if err != nil {
			return true, err
		}
		for _, id := range ids {
			if err = getters.ImportServiceStripeRefund(ctx, app, id, c); err != nil {
				return true, err
			}
		}
		return true, nil
	}
	var r stripe.Refund
	if err := json.Unmarshal(event.Data.Raw, &r); err != nil {
		return true, err
	}
	if !strings.HasPrefix(r.ID, "re_") {
		return true, servicestripe.ErrRefund
	}
	return true, getters.ImportServiceStripeRefund(ctx, app, r.ID, c)
}

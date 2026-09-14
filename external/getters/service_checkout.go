package getters

import (
	"context"
	"net/http"
	"net/url"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
)

// OpenNodeServiceCheckout uses only a stored purchase and authenticated provider
// responses. Retrying reuses its existing charge, never posts an ambiguous retry.
func OpenNodeServiceCheckout(ctx context.Context, app *config.AppContext, personID, orderID string) (string, error) {
	return openNodeServiceCheckout(ctx, app, personID, orderID, openNodeHTTPClient)
}

func openNodeServiceCheckout(ctx context.Context, app *config.AppContext, personID, orderID string, client *http.Client) (string, error) {
	id, err := initOpenNodeServiceCharge(ctx, app, personID, orderID, client)
	if err != nil {
		return "", err
	}
	charge, err := lookupOpenNodeServiceCharge(ctx, app, id, client)
	if err != nil {
		return "", err
	}
	if charge.Status != "unpaid" && charge.Status != "underpaid" {
		return "", ErrServiceChargeReconciliation
	}
	return safeOpenNodeHostedCheckout(charge.HostedCheckoutURL, id, app.Env.Prod)
}

func safeOpenNodeHostedCheckout(raw, id string, prod bool) (string, error) {
	parsedID, err := uuid.Parse(id)
	if err != nil || parsedID.String() != id {
		return "", ErrServicePayment
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.Path != "/"+id ||
		(u.Host != "checkout.opennode.com" && (prod || u.Host != "checkout.dev.opennode.com")) {
		return "", ErrServiceChargeReconciliation
	}
	return u.String(), nil
}

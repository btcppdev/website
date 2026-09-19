package handlers

import (
	"context"
	"os"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
)

func StartServiceStripeReconciliation(app *config.AppContext) {
	if os.Getenv("SERVICE_STRIPE_RECONCILIATION_ENABLED") != "true" || app == nil || app.Env == nil {
		return
	}
	key := app.Env.StripeKey
	live := strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "rk_live_")
	if !live && !strings.HasPrefix(key, "sk_test_") && !strings.HasPrefix(key, "rk_test_") {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			client := alertsstripe.NewClient(live)
			count, err := getters.ReconcileDueServiceStripeCheckouts(ctx, app, servicestripe.CheckoutClient{Retrieve: client.Retrieve, Live: live}, servicestripe.InvoiceClient{Invoice: client.Invoice, Price: client.Price, ListInvoices: client.ListInvoices, Live: live})
			cancel()
			if err != nil {
				app.Err.Printf("Service Stripe checkout recovery failed; checked=%d", count)
			}
		}
	}()
}

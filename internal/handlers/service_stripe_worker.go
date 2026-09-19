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
			client := alertsstripe.NewClient(live)
			runServiceStripeRecoveryPass(context.Background(), []serviceStripeRecoveryTask{
				{"alerts", func(ctx context.Context) (int, error) {
					return getters.ReconcileDueAlertsStripeCheckouts(ctx, app, client)
				}},
				{"insider", func(ctx context.Context) (int, error) {
					return getters.ReconcileDueServiceStripeCheckouts(ctx, app, servicestripe.CheckoutClient{Retrieve: client.Retrieve, Live: live}, servicestripe.InvoiceClient{Invoice: client.Invoice, Price: client.Price, ListInvoices: client.ListInvoices, Live: live}, servicestripe.SubscriptionClient{Subscription: client.Subscription, UpdateSubscription: client.UpdateSubscription, Live: live})
				}},
			}, func(product string, count int, err error) {
				if err != nil {
					app.Err.Printf("Service Stripe recovery failed; product=%s checked=%d", product, count)
				}
			})
		}
	}()
}

type serviceStripeRecoveryTask struct {
	product string
	recover func(context.Context) (int, error)
}

// Each product gets its own budget: an outage or backlog in one product must
// not prevent the other product from reconciling. Database leases prevent
// duplicate work across instances; one startup loop owns both dispatches.
func runServiceStripeRecoveryPass(ctx context.Context, tasks []serviceStripeRecoveryTask, report func(string, int, error)) {
	for _, task := range tasks {
		if ctx.Err() != nil {
			return
		}
		attempt, cancel := context.WithTimeout(ctx, 90*time.Second)
		count, err := task.recover(attempt)
		cancel()
		report(task.product, count, err)
	}
}

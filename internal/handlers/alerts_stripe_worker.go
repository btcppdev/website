package handlers

import (
	"context"
	"os"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
)

func StartAlertsStripeReconciliation(app *config.AppContext) {
	if os.Getenv("ALERTS_STRIPE_RECONCILIATION_ENABLED") != "true" || app.Env == nil {
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
			count, err := getters.ReconcileDueAlertsStripeCheckouts(ctx, app, alertsstripe.NewClient(live))
			cancel()
			if err != nil {
				app.Err.Printf("Alerts Stripe checkout recovery failed; checked=%d", count)
			}
		}
	}()
}

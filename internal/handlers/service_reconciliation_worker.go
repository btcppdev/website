package handlers

import (
	"context"
	"os"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
)

// Explicit opt-in keeps developer previews and unrelated deployments from
// contacting payment providers merely because a key is configured.
func StartServicePaymentReconciliation(app *config.AppContext) {
	if os.Getenv("SERVICE_PAYMENT_RECONCILIATION_ENABLED") != "true" || app.Env == nil || strings.TrimSpace(app.Env.OpenNode.Key) == "" {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
			count, err := getters.ReconcileDueOpenNodeServices(ctx, app)
			cancel()
			if err != nil {
				app.Err.Printf("service payment reconciliation checked=%d: %v", count, err)
			}
		}
	}()
}

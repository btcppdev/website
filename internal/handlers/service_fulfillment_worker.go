package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"os"
	"strings"
	"time"
)

func StartServiceFulfillment(app *config.AppContext) {
	if os.Getenv("SERVICE_FULFILLMENT_ENABLED") != "true" || !serviceFulfillmentReady(app) {
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, err := processServiceFulfillment(ctx, app, "", time.Now())
			cancel()
			if err != nil {
				app.Err.Printf("service fulfillment requires retry or review")
			}
		}
	}()
}
func serviceFulfillmentReady(app *config.AppContext) bool {
	return app != nil && app.Env != nil && strings.TrimSpace(app.Env.InsiderFulfillmentEndpoint) != "" && strings.TrimSpace(app.Env.InsiderFulfillmentToken) != "" && strings.TrimSpace(app.Env.InsiderMerchantName) != ""
}
func processServiceFulfillment(ctx context.Context, app *config.AppContext, attemptID string, now time.Time) (bool, error) {
	if !serviceFulfillmentReady(app) {
		return false, errors.New("external product fulfillment not configured")
	}
	job, err := getters.ClaimServiceFulfillment(ctx, app, attemptID, now)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	finish := func(state string, cause error) (bool, error) {
		_, err := getters.FinishServiceFulfillment(ctx, app, job, state, now)
		if err != nil {
			return true, err
		}
		return true, cause
	}
	purchase, err := getters.GetFulfillableServicePaymentContext(app, job.AttemptID)
	if err != nil {
		if errors.Is(err, getters.ErrServicePayment) {
			return finish("review", err)
		}
		return finish("retry", err)
	}
	if !strings.HasPrefix(purchase.Quote.ProductCode, "insider.") {
		return finish("review", getters.ErrServiceFulfillment)
	}
	if len(job.Payload) == 0 {
		event, err := buildInsiderSettlement(app, job.Payment)
		if err != nil {
			return finish("retry", err)
		}
		payload, err := json.Marshal(event)
		if err != nil {
			return finish("review", err)
		}
		if err = getters.FreezeServiceFulfillment(ctx, app, job, payload); err != nil {
			return finish("retry", err)
		}
	}
	var event insiderSettlementEvent
	if err = json.Unmarshal(job.Payload, &event); err != nil {
		return finish("review", err)
	}
	if err = postInsiderSettlement(ctx, app, event); err != nil {
		return finish("retry", err)
	}
	if err = getters.CompleteExternalServiceFulfillment(app, purchase.OrderID); err != nil {
		return finish("retry", err)
	}
	return finish("accepted", nil)
}

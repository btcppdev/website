package handlers

import (
	"context"
	"errors"
	"os"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"github.com/jackc/pgx/v5"
)

func StartExpeditionLifecycle(app *config.AppContext) {
	if os.Getenv("ANTARCTICA_INSTALLMENT_LIFECYCLE_ENABLED") != "true" {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
			if err := processExpeditionLifecycle(ctx, app, time.Now(), getters.ReconcileExpeditionPayments, func(m *emails.Mail) error { return emails.ComposeAndSendMail(app, m) }); err != nil {
				app.Err.Printf("expedition installment lifecycle needs retry or review: %v", err)
			}
			cancel()
		}
	}()
}

func processExpeditionLifecycle(ctx context.Context, app *config.AppContext, now time.Time, reconcile func(context.Context, *config.AppContext, string) error, send func(*emails.Mail) error) error {
	if !serviceReceiptMailReady(app) {
		return nil
	}
	candidates, err := getters.ExpeditionLifecycleCandidates(ctx, app)
	if err != nil {
		return err
	}
	var first error
	for _, c := range candidates {
		if err = reconcile(ctx, app, c.ID); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if _, err = getters.ProcessExpeditionLifecycle(ctx, app, c, now); err != nil && first == nil {
			first = err
		}
	}
	for n := 0; n < 100; n++ {
		j, err := getters.ClaimExpeditionLifecycleMail(ctx, app, now)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return err
		}
		m, composeErr := emails.AntarcticaInstallmentMail(j, app.Env.GetURI())
		if composeErr == nil {
			composeErr = send(m)
		}
		if err = getters.FinishExpeditionLifecycleMail(ctx, app, j, composeErr == nil, now); err != nil {
			return err
		}
		if composeErr != nil && first == nil {
			first = composeErr
		}
	}
	return first
}

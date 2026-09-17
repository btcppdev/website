package handlers

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"github.com/jackc/pgx/v5"
)

func StartServiceReceipts(app *config.AppContext) {
	if os.Getenv("SERVICE_RECEIPTS_ENABLED") != "true" || !serviceReceiptMailReady(app) {
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, err := processServiceReceipt(ctx, app, "", time.Now())
			cancel()
			if err != nil {
				app.Err.Printf("service receipt delivery needs retry or review")
			}
		}
	}()
}

func serviceReceiptMailReady(app *config.AppContext) bool {
	return app != nil && app.Env != nil && !app.Env.MailOff && strings.TrimSpace(app.Env.MailEndpoint) != ""
}

func processServiceReceipt(ctx context.Context, app *config.AppContext, orderID string, now time.Time) (bool, error) {
	// ComposeAndSendMail's MailOff path returns nil. Never consume a lease when
	// mail is disabled, or that nil would falsely mark the receipt accepted.
	if !serviceReceiptMailReady(app) {
		return false, nil
	}
	job, err := getters.ClaimServiceReceipt(ctx, app, orderID, now)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	finish := func(outcome string, cause error) (bool, error) {
		_, err := getters.FinishServiceReceipt(ctx, app, job, outcome, now)
		if err != nil {
			return true, err
		}
		return true, cause
	}
	order, err := getters.ServiceOrderForPerson(ctx, app, job.PersonID, job.PublicID)
	if err != nil {
		return finish("retry", err)
	}
	if !order.PaymentVerified || order.Status != "paid" {
		return finish("cancelled", nil)
	}
	message, err := emails.ServiceReceiptMail(order, job.Recipient, app.Env.GetURI())
	if err != nil {
		return finish("review", err)
	}
	if err = emails.ComposeAndSendMail(app, message); err != nil {
		return finish("retry", errors.New("mailer did not confirm receipt acceptance"))
	}
	return finish("accepted", nil)
}

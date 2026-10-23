package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/expedition"
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
	return processServiceReceiptWithSender(ctx, app, orderID, now, func(m *emails.Mail) error { return emails.ComposeAndSendMail(app, m) })
}

func processServiceReceiptWithSender(ctx context.Context, app *config.AppContext, orderID string, now time.Time, send func(*emails.Mail) error) (bool, error) {
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
	if err == nil && order.Quote.ProductCode == expedition.ProductCode {
		var f expedition.Fulfillment
		if json.Unmarshal(order.Quote.Fulfillment, &f) != nil || f.Version != 1 || f.ReservationID != order.Quote.ResourceID || f.Installment < 1 {
			return finish("review", errors.New("invalid expedition receipt identity"))
		}
		booking, loadErr := getters.ExpeditionBookingForPerson(ctx, app, job.PersonID, f.ReservationID)
		if loadErr != nil {
			return finish("retry", loadErr)
		}
		if booking.State == "held" {
			return finish("retry", errors.New("awaiting berth fulfillment"))
		}
		if booking.State != "confirmed" {
			return finish("review", errors.New("booking not confirmed"))
		}
		if order.ExpeditionState == "pending" {
			return finish("retry", errors.New("awaiting installment fulfillment"))
		}
		message, err = emails.AntarcticaBookingMail(order, booking, job.Recipient, app.Env.GetURI())
	}
	if err != nil {
		return finish("review", err)
	}
	if err = send(message); err != nil {
		return finish("retry", errors.New("mailer did not confirm receipt acceptance"))
	}
	return finish("accepted", nil)
}

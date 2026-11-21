package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"fmt"
	"github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/checkout/session"
	"strings"
	"time"
)

// A checked offer is an explicit eligibility attestation. It replaces a silent
// affiliate code, but cannot be stacked with a typed discount code.
func resolveTicketDiscount(app *config.AppContext, conf *types.Conf, ticket *types.ConfTicket, code, affiliate, offer string) (string, string, error) {
	code = strings.TrimSpace(code)
	offer = strings.TrimSpace(offer)
	if !ticket.DiscountsAllowed() {
		if code != "" || offer != "" {
			return "", "", fmt.Errorf("discounts are not available for this ticket type")
		}
		return "", "", nil
	}
	if offer != "" {
		if code != "" {
			return "", "", fmt.Errorf("choose either a special offer or a discount code")
		}
		offers, err := getters.ListCheckoutOffers(app, conf.Ref, true)
		if err != nil {
			return "", "", err
		}
		for _, o := range offers {
			if o.Code == offer {
				return o.Code, o.Label, nil
			}
		}
		return "", "", fmt.Errorf("this special offer is no longer available")
	}
	return effectiveDiscountCode(code, affiliate), code, nil
}

// Reconcile abandoned checkouts against the payment provider. A paid charge with
// a delayed callback keeps its allocation; it must not be resold.
func reconcileTicketReservations(app *config.AppContext) {
	rows, err := app.DB.Query(app.DatabaseContext(), `SELECT id::text,provider,provider_id FROM ticket_reservations WHERE status='pending' AND updated_at < now()-interval '35 minutes' ORDER BY updated_at LIMIT 25`)
	if err != nil {
		app.Err.Printf("ticket reservation reconciliation: %s", err)
		return
	}
	type pending struct{ id, provider, payment string }
	var batch []pending
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.id, &p.provider, &p.payment); err != nil {
			break
		}
		batch = append(batch, p)
	}
	if rows.Err() != nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		app.Err.Printf("ticket reservation reconciliation: %s", err)
		return
	}
	for _, p := range batch {
		// Move inspected rows to the back of the queue, including uncertain creates.
		_, _ = app.DB.Exec(app.DatabaseContext(), `UPDATE ticket_reservations SET updated_at=now() WHERE id=$1`, p.id)
		if p.payment == "" {
			app.Err.Printf("ticket reservation %s needs payment-provider reconciliation: create response unavailable", p.id)
			continue
		}
		expired := false
		if p.provider == "stripe" {
			params := &stripe.CheckoutSessionParams{}
			c, cancel := context.WithTimeout(app.DatabaseContext(), 15*time.Second)
			params.Context = c
			payment, e := session.Get(p.payment, params)
			cancel()
			if e != nil {
				app.Err.Printf("ticket reservation %s: %s", p.id, e)
				continue
			}
			expired = payment.ID == p.payment && payment.Metadata["ticket-reservation-id"] == p.id && string(payment.Status) == "expired" && string(payment.PaymentStatus) != "paid"
		} else {
			payment, e := GetCharge(app, p.payment)
			if e != nil {
				app.Err.Printf("ticket reservation %s: %s", p.id, e)
				continue
			}
			expired = payment.ID == p.payment && payment.Metadata != nil && payment.Metadata.ReservationID == p.id && payment.Status == "expired"
		}
		if expired {
			if err = getters.ReleaseTicketReservation(app, p.id, p.provider, p.payment); err != nil {
				app.Err.Printf("release ticket reservation %s: %s", p.id, err)
			}
		}
	}
}

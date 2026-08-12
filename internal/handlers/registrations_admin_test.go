package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestRegistrationAdminRowsPreservesMultipleTicketsForEmail(t *testing.T) {
	older := time.Date(2026, time.July, 1, 14, 0, 0, 0, time.UTC)
	newer := older.Add(24 * time.Hour)
	registrations := []*types.Registration{
		{
			RefID:        "genpop-ticket",
			CheckoutID:   "on-genpop-charge",
			Email:        "haas.haze@gmail.com",
			Type:         "genpop",
			Amount:       210,
			Currency:     "eur",
			Platform:     "opennode",
			RegisteredAt: &older,
		},
		{
			RefID:        "local-ticket",
			CheckoutID:   "cs_local_checkout",
			Email:        "haas.haze@gmail.com",
			Type:         "local",
			Amount:       75,
			Currency:     "eur",
			Platform:     "stripe",
			RegisteredAt: &newer,
		},
	}

	rows := registrationAdminRows(registrations, time.UTC)
	if len(rows) != 2 {
		t.Fatalf("registrationAdminRows() returned %d rows, want 2", len(rows))
	}
	if rows[0].RefID != "local-ticket" || rows[1].RefID != "genpop-ticket" {
		t.Fatalf("registration rows = [%s, %s], want newest ticket first", rows[0].RefID, rows[1].RefID)
	}
	if rows[0].PaymentLabel != "EUR 75.00" || rows[0].CheckoutID != "cs_local_checkout" {
		t.Fatalf("payment details = %q / %q, want EUR amount and Stripe checkout ID", rows[0].PaymentLabel, rows[0].CheckoutID)
	}
}

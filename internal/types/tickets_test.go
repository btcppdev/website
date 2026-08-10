package types

import (
	"testing"
	"time"
)

func TestCurrentAndNextConfTicketAtUseCutoffAndCapacity(t *testing.T) {
	now := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	early := &ConfTicket{ID: "early", BasePrice: 75, Max: 25, SalesEndAt: now.Add(5 * 24 * time.Hour)}
	general := &ConfTicket{ID: "general", BasePrice: 120, Max: 150, SalesEndAt: now.Add(30 * 24 * time.Hour)}
	tickets := []*ConfTicket{general, early}

	if got := CurrentConfTicketAt(tickets, 10, now); got != early {
		t.Fatalf("current ticket = %#v, want early", got)
	}
	if got := NextConfTicketAfter(tickets, early, 10); got != general {
		t.Fatalf("next ticket = %#v, want general", got)
	}
	if got := CurrentConfTicketAt(tickets, 25, now); got != general {
		t.Fatalf("sold-out early tier current = %#v, want general", got)
	}
	if got := CurrentConfTicketAt(tickets, 10, early.SalesEndAt); got != general {
		t.Fatalf("at cutoff current = %#v, want general", got)
	}
}

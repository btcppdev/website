package handlers

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestCheckInPageTicketPresentation(t *testing.T) {
	tests := []struct {
		ticketType string
		wantTheme  string
		wantLabel  string
	}{
		{types.TicketTypeGeneral, "blue", "General admission"},
		{types.TicketTypeLocal, "blue", "Local"},
		{types.TicketTypeSponsored, "red", "Sponsor"},
		{"sponsor", "red", "Sponsor"},
		{"volunteer", "green", "Volunteer"},
		{"speaker", "orange", "Speaker"},
	}
	for _, test := range tests {
		page := &CheckInPage{TicketType: test.ticketType}
		if got := page.TicketTheme(); got != test.wantTheme {
			t.Errorf("TicketTheme(%q) = %q, want %q", test.ticketType, got, test.wantTheme)
		}
		if got := page.TicketTypeLabel(); got != test.wantLabel {
			t.Errorf("TicketTypeLabel(%q) = %q, want %q", test.ticketType, got, test.wantLabel)
		}
	}
}

func TestCheckInPagePickupState(t *testing.T) {
	page := &CheckInPage{TShirtSize: "MM"}
	if got := page.TShirtSizeLabel(); got != "Men's medium" {
		t.Fatalf("TShirtSizeLabel() = %q", got)
	}
	if !page.HasPendingPickups() {
		t.Fatal("sized conference shirt should be pending")
	}
	page.ShirtPickedUp = true
	page.MerchPickups = []*types.ShopOrderItem{{Status: types.ShopItemStatusReady}}
	if !page.HasPendingPickups() {
		t.Fatal("ready merchandise should be pending")
	}
	page.MerchPickups[0].Status = types.ShopItemStatusFulfilled
	if page.HasPendingPickups() {
		t.Fatal("fulfilled shirt and merchandise should be complete")
	}
}

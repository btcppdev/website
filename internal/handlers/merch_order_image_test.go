package handlers

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestShopOrderItemImageUsesConferenceLeadingImageForTicket(t *testing.T) {
	item := &types.ShopOrderItem{
		ProductTagSnapshot: "ticket",
		SaleConferenceTag:  "atx24",
	}
	if got, want := shopOrderItemImage(item), "/static/img/atx24.png"; got != want {
		t.Fatalf("shopOrderItemImage(ticket) = %q, want %q", got, want)
	}
}

func TestShopOrderItemImagePrefersMerchImage(t *testing.T) {
	item := &types.ShopOrderItem{
		ImageObjectKey:    "/static/img/merch/example.avif",
		SaleConferenceTag: "atx24",
	}
	if got, want := shopOrderItemImage(item), "/static/img/merch/example.avif"; got != want {
		t.Fatalf("shopOrderItemImage(merch) = %q, want %q", got, want)
	}
}

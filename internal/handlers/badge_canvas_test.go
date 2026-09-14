package handlers

import (
	"btcpp-web/internal/types"
	"testing"
)

func TestCanvasCartSeparatesDesignsOnCancelAndPayment(t *testing.T) {
	lines := []shopCartLine{{VariantID: "canvas", BadgeReference: "one", Qty: 1}, {VariantID: "canvas", BadgeReference: "two", Qty: 3}, {VariantID: "hat", Qty: 2}}
	order := &types.ShopOrder{Items: []*types.ShopOrderItem{{VariantID: "canvas", Quantity: 2, BadgeCanvas: &types.BadgeCanvas{Reference: "one"}}}}
	restored := restoreShopOrderCart(lines, order)
	if len(restored) != 3 || restored[0].Qty != 2 || restored[1].Qty != 3 {
		t.Fatal("cancel combined canvas designs", restored)
	}
	remaining := removeShopOrderFromCart(restored, order)
	if len(remaining) != 2 || remaining[0].BadgeReference != "two" || remaining[0].Qty != 3 || remaining[1].VariantID != "hat" {
		t.Fatal("payment removed unrelated designs", remaining)
	}
	empty := restoreShopOrderCart(nil, order)
	if len(empty) != 1 || empty[0].BadgeReference != "one" || empty[0].VariantID != "canvas" {
		t.Fatal("restored order lost badge", empty)
	}
	item := &types.ShopOrderItem{BadgeCanvas: &types.BadgeCanvas{ArtworkURL: "https://example.test/printed.png"}, ImageObjectKey: "catalog.png"}
	if shopOrderItemImage(item) != "https://example.test/printed.png" {
		t.Fatal("receipt uses catalog instead of selected badge")
	}
}

func TestCanvasTaxReferencesAreDistinct(t *testing.T) {
	product := &types.MerchProduct{ID: "p"}
	variant := &types.MerchVariant{ID: "v"}
	cart := []*shopCartItem{{Product: product, Variant: variant, Qty: 1, LineTotalCents: 2500, BadgeCanvas: &types.BadgeCanvas{Reference: "one"}}, {Product: product, Variant: variant, Qty: 1, LineTotalCents: 2500, BadgeCanvas: &types.BadgeCanvas{Reference: "two"}}}
	params, err := shopStripeTaxParams(cart, &types.ShopAddress{Line1: "Test", City: "Austin", Country: "US"}, 0, "USD")
	if err != nil {
		t.Fatal(err)
	}
	if *params.LineItems[0].Reference == *params.LineItems[1].Reference {
		t.Fatal("different canvas designs share a tax reference")
	}
}

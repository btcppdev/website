package handlers

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestRestoreShopOrderCartPreservesExistingChanges(t *testing.T) {
	order := &types.ShopOrder{Items: []*types.ShopOrderItem{
		{VariantID: "hat", Quantity: 2},
		{VariantID: "shirt", Quantity: 1},
	}}
	lines := []shopCartLine{
		{VariantID: "hat", Qty: 3},
		{VariantID: "sticker", Qty: 1},
	}

	got := restoreShopOrderCart(lines, order)
	want := map[string]uint{"hat": 3, "shirt": 1, "sticker": 1}
	assertCartQuantities(t, got, want)
}

func TestRemoveShopOrderFromCartOnlyConsumesPurchasedQuantities(t *testing.T) {
	order := &types.ShopOrder{Items: []*types.ShopOrderItem{
		{VariantID: "hat", Quantity: 2},
		{VariantID: "shirt", Quantity: 1},
	}}
	lines := []shopCartLine{
		{VariantID: "hat", Qty: 3},
		{VariantID: "shirt", Qty: 1},
		{VariantID: "sticker", Qty: 2},
	}

	got := removeShopOrderFromCart(lines, order)
	want := map[string]uint{"hat": 1, "sticker": 2}
	assertCartQuantities(t, got, want)
}

func assertCartQuantities(t *testing.T, lines []shopCartLine, want map[string]uint) {
	t.Helper()
	got := make(map[string]uint, len(lines))
	for _, line := range lines {
		got[line.VariantID] += line.Qty
	}
	if len(got) != len(want) {
		t.Fatalf("cart has %d variants, want %d: %#v", len(got), len(want), got)
	}
	for variantID, quantity := range want {
		if got[variantID] != quantity {
			t.Errorf("%s quantity = %d, want %d", variantID, got[variantID], quantity)
		}
	}
}

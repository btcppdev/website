package handlers

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestMerchProductStockTotalsVariants(t *testing.T) {
	product := &types.MerchProduct{Variants: []*types.MerchVariant{
		{Stock: 7},
		nil,
		{Stock: 4},
	}}
	if got, want := merchProductStock(product), 11; got != want {
		t.Fatalf("merchProductStock() = %d, want %d", got, want)
	}
}

func TestAdminMerchProductURL(t *testing.T) {
	if got, want := adminMerchProductURL("product-id", "flash", "Product updated."), "/admin/merch/product-id?flash=Product+updated."; got != want {
		t.Fatalf("adminMerchProductURL() = %q, want %q", got, want)
	}
}

func TestFilterShopOrdersForAdminCountsAndFiltersShippingQueue(t *testing.T) {
	orders := []*types.ShopOrder{
		{PublicID: "ships-two", UnfulfilledShippingQuantity: 2},
		{PublicID: "complete"},
		nil,
		{PublicID: "ships-one", UnfulfilledShippingQuantity: 1},
		{PublicID: "pickup-four", EventPickupQuantity: 4},
	}

	all, total, needsShipping, eventPickup := filterShopOrdersForAdmin(orders, "all")
	if len(all) != 4 || total != 4 || needsShipping != 2 || eventPickup != 1 {
		t.Fatalf("all orders = %d, total = %d, needs shipping = %d, event pickup = %d", len(all), total, needsShipping, eventPickup)
	}

	queue, total, needsShipping, _ := filterShopOrdersForAdmin(orders, "needs_shipping")
	if len(queue) != 2 || total != 4 || needsShipping != 2 {
		t.Fatalf("shipping queue = %d, total = %d, needs shipping = %d", len(queue), total, needsShipping)
	}
	if queue[0].PublicID != "ships-two" || queue[1].PublicID != "ships-one" {
		t.Fatalf("shipping queue order = %q, %q", queue[0].PublicID, queue[1].PublicID)
	}
	pickups, _, _, eventPickup := filterShopOrdersForAdmin(orders, "event_pickup")
	if len(pickups) != 1 || eventPickup != 1 || pickups[0].PublicID != "pickup-four" {
		t.Fatalf("pickup queue = %#v, event pickup = %d", pickups, eventPickup)
	}
}

func TestShopOrderUsesAutomatedStripeRefund(t *testing.T) {
	tests := []struct {
		name  string
		order *types.ShopOrder
		want  bool
	}{
		{name: "stripe checkout", order: &types.ShopOrder{PaymentProvider: "stripe", PaymentProviderID: "cs_test_123"}, want: true},
		{name: "stripe missing checkout", order: &types.ShopOrder{PaymentProvider: "stripe"}},
		{name: "opennode", order: &types.ShopOrder{PaymentProvider: "opennode", PaymentProviderID: "charge-123"}},
		{name: "bitcoin", order: &types.ShopOrder{PaymentProvider: "btc"}},
		{name: "nil"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shopOrderUsesAutomatedStripeRefund(test.order); got != test.want {
				t.Fatalf("shopOrderUsesAutomatedStripeRefund() = %t, want %t", got, test.want)
			}
		})
	}
}

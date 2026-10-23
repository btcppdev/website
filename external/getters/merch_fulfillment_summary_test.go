package getters

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestPopulateShopOrderFulfillmentSummary(t *testing.T) {
	order := &types.ShopOrder{
		Status: types.ShopOrderStatusPaid,
		Items: []*types.ShopOrderItem{
			{ProductNameSnapshot: "Knots hat", Quantity: 4, FulfillmentMethod: types.ShopFulfillmentShip, Status: types.ShopItemStatusPending},
			{ProductNameSnapshot: "Core hat", Quantity: 3, FulfilledQuantity: 1, FulfillmentMethod: types.ShopFulfillmentEventPickup, Status: types.ShopItemStatusPending},
			{ProductNameSnapshot: "Old hat", Quantity: 1, FulfillmentMethod: types.ShopFulfillmentShip, Status: types.ShopItemStatusCancelled},
		},
	}

	populateShopOrderFulfillmentSummary(order)

	if order.UnfulfilledShippingQuantity != 4 || order.UnfulfilledShippingSummary != "Knots hat ×4" {
		t.Fatalf("shipping summary = %d %q", order.UnfulfilledShippingQuantity, order.UnfulfilledShippingSummary)
	}
	if order.EventPickupQuantity != 2 || order.EventPickupSummary != "Core hat ×2" {
		t.Fatalf("pickup summary = %d %q", order.EventPickupQuantity, order.EventPickupSummary)
	}
}

func TestPopulateShopOrderFulfillmentSummaryDoesNotQueuePendingShipping(t *testing.T) {
	order := &types.ShopOrder{
		Status: types.ShopOrderStatusPending,
		Items: []*types.ShopOrderItem{{
			ProductNameSnapshot: "Knots hat", Quantity: 4,
			FulfillmentMethod: types.ShopFulfillmentShip, Status: types.ShopItemStatusPending,
		}},
	}
	populateShopOrderFulfillmentSummary(order)
	if order.UnfulfilledShippingQuantity != 0 || order.UnfulfilledShippingSummary != "" {
		t.Fatalf("pending order entered shipping queue: %d %q", order.UnfulfilledShippingQuantity, order.UnfulfilledShippingSummary)
	}
}

func TestShopOrderFulfillmentDisplay(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		items        []*types.ShopOrderItem
		want         string
		remaining    uint
	}{
		{"unfulfilled", types.ShopOrderStatusPaid, []*types.ShopOrderItem{{VariantID: "variant", Quantity: 2, FulfillmentMethod: types.ShopFulfillmentShip}}, "unfulfilled", 2},
		{"mixed", types.ShopOrderStatusPaid, []*types.ShopOrderItem{{VariantID: "variant", Quantity: 2, FulfilledQuantity: 1, FulfillmentMethod: types.ShopFulfillmentEventPickup}}, "partial", 1},
		{"complete", types.ShopOrderStatusPaid, []*types.ShopOrderItem{{VariantID: "variant", Quantity: 2, FulfilledQuantity: 2, Status: types.ShopItemStatusFulfilled, FulfillmentMethod: types.ShopFulfillmentPOSTakeaway}}, "fulfilled", 0},
		{"unpaid", types.ShopOrderStatusPending, []*types.ShopOrderItem{{VariantID: "variant", Quantity: 1, FulfillmentMethod: types.ShopFulfillmentShip}}, "awaiting_payment", 1},
		{"ticket only", types.ShopOrderStatusPaid, nil, "not_required", 0},
		{"refunded", types.ShopOrderStatusRefunded, []*types.ShopOrderItem{{VariantID: "variant", Quantity: 1, FulfillmentMethod: types.ShopFulfillmentShip}}, "not_required", 1},
		{"partial refund", types.ShopOrderStatusPartiallyRefunded, []*types.ShopOrderItem{{VariantID: "variant", Quantity: 2, FulfilledQuantity: 1, RefundedQuantity: 1, FulfillmentMethod: types.ShopFulfillmentShip}}, "fulfilled", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			order := &types.ShopOrder{Status: tc.status, Items: tc.items}
			populateShopOrderFulfillmentSummary(order)
			if order.FulfillmentStatus() != tc.want || order.FulfillmentRemaining != tc.remaining {
				t.Fatalf("got %s, %d remaining", order.FulfillmentStatus(), order.FulfillmentRemaining)
			}
		})
	}
}

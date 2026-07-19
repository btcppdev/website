package getters

import (
	"context"
	"fmt"
	"testing"

	"btcpp-web/internal/config"
)

func TestEasyshipShipmentTransitionDoesNotRegressDelivered(t *testing.T) {
	status, _, delivery, shipped, delivered := easyshipShipmentTransition(
		"shipment.tracking.status.changed", "In Transit to Customer",
		"delivered", "generated", "Delivered",
	)
	if status != "delivered" || delivery != "Delivered" || shipped || delivered {
		t.Fatalf("transition = status %q delivery %q shipped %t delivered %t", status, delivery, shipped, delivered)
	}
}

func TestEasyshipShipmentTransitionDoesNotRegressGeneratedLabel(t *testing.T) {
	status, label, _, _, _ := easyshipShipmentTransition(
		"shipment.label.failed", "", "label_created", "generated", "not_created",
	)
	if status != "label_created" || label != "generated" {
		t.Fatalf("late label failure regressed state to (%q, %q)", status, label)
	}
}

func TestDatabaseSmokeEasyshipWebhookReplayAndTransitions(t *testing.T) {
	ctx := databaseSmokeContext(t)
	suffix := databaseSmokeSuffix()
	publicID := "easyship-" + suffix
	var orderID string
	if err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO shop_orders (public_id, buyer_email, status)
		VALUES ($1, $2, 'paid')
		RETURNING id::text
	`, publicID, publicID+"@example.test").Scan(&orderID); err != nil {
		t.Fatalf("insert Easyship test order: %s", err)
	}
	providerShipmentID := "ESTEST" + suffix
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM easyship_webhook_events WHERE easyship_shipment_id = $1`, providerShipmentID)
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM shop_orders WHERE id = $1::uuid`, orderID)
	})
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO shipments (order_id, provider, provider_shipment_id, status)
		VALUES ($1::uuid, 'easyship', $2, 'pending')
	`, orderID, providerShipmentID); err != nil {
		t.Fatalf("insert Easyship test shipment: %s", err)
	}
	var orderItemID string
	if err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO shop_order_items (
			order_id, quantity, refunded_quantity, unit_price_cents, line_total_cents,
			product_name_snapshot, fulfillment_method, status
		) VALUES ($1::uuid, 3, 1, 1000, 3000, 'Test hat', 'ship', 'partially_refunded')
		RETURNING id::text
	`, orderID).Scan(&orderItemID); err != nil {
		t.Fatalf("insert Easyship test order item: %s", err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO shipment_items (shipment_id, order_item_id, quantity)
		SELECT id, $2::uuid, 2
		FROM shipments
		WHERE provider = 'easyship' AND provider_shipment_id = $1
	`, providerShipmentID, orderItemID); err != nil {
		t.Fatalf("insert Easyship test shipment item: %s", err)
	}

	labelPayload := []byte(fmt.Sprintf(`{
		"event_type":"shipment.label.created","resource_type":"shipment","resource_id":%q,
		"data":{"easyship_shipment_id":%q,"status":"success","label_url":"https://example.test/label.pdf","tracking_number":"TRACK123","tracking_page_url":"https://example.test/track"}
	}`, providerShipmentID, providerShipmentID))
	created, err := StoreEasyshipWebhookEvent(ctx, EasyshipWebhookEventInput{
		EventType: "shipment.label.created", ResourceType: "shipment", ResourceID: providerShipmentID,
		EasyshipShipmentID: providerShipmentID, Payload: labelPayload,
	})
	if err != nil || !created {
		t.Fatalf("store label event = (%t, %v)", created, err)
	}
	created, err = StoreEasyshipWebhookEvent(ctx, EasyshipWebhookEventInput{
		EventType: "shipment.label.created", ResourceType: "shipment", ResourceID: providerShipmentID,
		EasyshipShipmentID: providerShipmentID, Payload: labelPayload,
	})
	if err != nil || created {
		t.Fatalf("store replay = (%t, %v), want duplicate", created, err)
	}
	if count, err := ProcessEasyshipWebhookEvents(ctx, 10); err != nil || count != 1 {
		t.Fatalf("process label events = (%d, %v)", count, err)
	}
	assertEasyshipShipmentState(t, ctx, providerShipmentID, "label_created", "generated", "not_created")
	assertShopOrderItemFulfillment(t, ctx, orderItemID, 0, "partially_refunded")

	shippedPayload := []byte(fmt.Sprintf(`{
		"event_type":"shipment.tracking.status.changed","resource_type":"shipment","resource_id":%q,
		"data":{"easyship_shipment_id":%q,"status":"In Transit to Customer","tracking_number":"TRACK123","tracking_page_url":"https://example.test/track"}
	}`, providerShipmentID, providerShipmentID))
	if created, err := StoreEasyshipWebhookEvent(ctx, EasyshipWebhookEventInput{
		EventType: "shipment.tracking.status.changed", ResourceType: "shipment", ResourceID: providerShipmentID,
		EasyshipShipmentID: providerShipmentID, Payload: shippedPayload,
	}); err != nil || !created {
		t.Fatalf("store shipped event = (%t, %v)", created, err)
	}
	if count, err := ProcessEasyshipWebhookEvents(ctx, 10); err != nil || count != 1 {
		t.Fatalf("process shipped events = (%d, %v)", count, err)
	}
	assertEasyshipShipmentState(t, ctx, providerShipmentID, "shipped", "generated", "In Transit to Customer")
	assertShopOrderItemFulfillment(t, ctx, orderItemID, 2, "partially_refunded")

	deliveredPayload := []byte(fmt.Sprintf(`{
		"event_type":"shipment.tracking.status.changed","resource_type":"shipment","resource_id":%q,
		"data":{"easyship_shipment_id":%q,"status":"Delivered","tracking_number":"TRACK123","tracking_page_url":"https://example.test/track"}
	}`, providerShipmentID, providerShipmentID))
	if created, err := StoreEasyshipWebhookEvent(ctx, EasyshipWebhookEventInput{
		EventType: "shipment.tracking.status.changed", ResourceType: "shipment", ResourceID: providerShipmentID,
		EasyshipShipmentID: providerShipmentID, Payload: deliveredPayload,
	}); err != nil || !created {
		t.Fatalf("store delivered event = (%t, %v)", created, err)
	}
	if count, err := ProcessEasyshipWebhookEvents(ctx, 10); err != nil || count != 1 {
		t.Fatalf("process delivered events = (%d, %v)", count, err)
	}
	assertEasyshipShipmentState(t, ctx, providerShipmentID, "delivered", "generated", "Delivered")
	assertShopOrderItemFulfillment(t, ctx, orderItemID, 2, "partially_refunded")

	stalePayload := []byte(fmt.Sprintf(`{
		"event_type":"shipment.tracking.status.changed","resource_type":"shipment","resource_id":%q,
		"data":{"easyship_shipment_id":%q,"status":"In Transit to Customer"}
	}`, providerShipmentID, providerShipmentID))
	if _, err := StoreEasyshipWebhookEvent(ctx, EasyshipWebhookEventInput{
		EventType: "shipment.tracking.status.changed", ResourceType: "shipment", ResourceID: providerShipmentID,
		EasyshipShipmentID: providerShipmentID, Payload: stalePayload,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessEasyshipWebhookEvents(ctx, 10); err != nil {
		t.Fatal(err)
	}
	assertEasyshipShipmentState(t, ctx, providerShipmentID, "delivered", "generated", "Delivered")
	assertShopOrderItemFulfillment(t, ctx, orderItemID, 2, "partially_refunded")
}

func assertEasyshipShipmentState(t *testing.T, app *config.AppContext, providerID, wantStatus, wantLabel, wantDelivery string) {
	t.Helper()
	// The smoke context is an AppContext; keep the query local so the assertion
	// checks the persisted callback result rather than transition helpers.
	var status, label, delivery string
	if err := app.DB.QueryRow(context.Background(), `
		SELECT status, label_state, delivery_state
		FROM shipments
		WHERE provider = 'easyship' AND provider_shipment_id = $1
	`, providerID).Scan(&status, &label, &delivery); err != nil {
		t.Fatalf("load Easyship shipment state: %s", err)
	}
	if status != wantStatus || label != wantLabel || delivery != wantDelivery {
		t.Fatalf("shipment state = (%q, %q, %q), want (%q, %q, %q)", status, label, delivery, wantStatus, wantLabel, wantDelivery)
	}
}

func assertShopOrderItemFulfillment(t *testing.T, app *config.AppContext, orderItemID string, wantQuantity int, wantStatus string) {
	t.Helper()
	var quantity int
	var status string
	if err := app.DB.QueryRow(context.Background(), `
		SELECT fulfilled_quantity, status
		FROM shop_order_items
		WHERE id = $1::uuid
	`, orderItemID).Scan(&quantity, &status); err != nil {
		t.Fatalf("load shop order item fulfillment: %s", err)
	}
	if quantity != wantQuantity || status != wantStatus {
		t.Fatalf("order item fulfillment = (%d, %q), want (%d, %q)", quantity, status, wantQuantity, wantStatus)
	}
}

package getters

import (
	"context"
	"testing"

	"btcpp-web/internal/types"
)

func TestDatabaseSmokeTicketPickupChecklist(t *testing.T) {
	app := databaseSmokeContext(t)
	conferenceID, _ := insertSmokeConference(t, app)
	suffix := databaseSmokeSuffix()
	email := "checkin-" + suffix + "@example.test"
	ticketRef := "checkin-" + suffix

	var personID string
	if err := app.DB.QueryRow(app.DatabaseContext(), `
		INSERT INTO people (name, email, tshirt)
		VALUES ('Check-in Test', $1::citext, 'MM')
		RETURNING id::text
	`, email).Scan(&personID); err != nil {
		t.Fatalf("create person: %s", err)
	}
	if _, err := app.DB.Exec(app.DatabaseContext(), `
		INSERT INTO registrations (ref_id, conference_id, type, email, checked_in_at, platform)
		VALUES ($1, $2::uuid, 'genpop', $3::citext, now(), 'dev-checkin-preview')
	`, ticketRef, conferenceID, email); err != nil {
		t.Fatalf("create registration: %s", err)
	}

	productID, err := CreateMerchProduct(app, MerchProductInput{
		Tag: "checkin-" + suffix, Slug: "checkin-" + suffix, Name: "Check-in hat",
		Status: types.MerchProductStatusPublished, BasePriceCents: 2500, Currency: "USD",
		AllowEventPickup: true,
	})
	if err != nil {
		t.Fatalf("create product: %s", err)
	}
	variantID, err := CreateMerchVariant(app, MerchVariantInput{
		ProductID: productID, SKU: "CHECKIN-" + suffix, Label: "Black",
		InventoryPolicy: types.MerchInventoryPolicyUnlimited, Status: "active",
	})
	if err != nil {
		t.Fatalf("create variant: %s", err)
	}
	order, err := CreateShopOrder(app, ShopOrderInput{
		BuyerEmail: email, BuyerName: "Check-in Test", PaymentProvider: "stripe",
		SubtotalCents: 2500, TotalCents: 2500,
	}, []ShopOrderItemInput{{
		ProductID: productID, VariantID: variantID, Quantity: 1,
		UnitPriceCents: 2500, LineTotalCents: 2500,
		ProductTagSnapshot: "checkin", ProductNameSnapshot: "Check-in hat",
		VariantLabelSnapshot: "Black", SKUSnapshot: "CHECKIN-" + suffix,
		FulfillmentMethod:  types.ShopFulfillmentEventPickup,
		PickupConferenceID: conferenceID, Status: types.ShopItemStatusPending,
	}})
	if err != nil {
		t.Fatalf("create order: %s", err)
	}
	t.Cleanup(func() {
		_, _ = app.DB.Exec(context.Background(), `DELETE FROM shop_orders WHERE id = $1::uuid`, order.ID)
		_, _ = app.DB.Exec(context.Background(), `DELETE FROM merch_products WHERE id = $1::uuid`, productID)
		_, _ = app.DB.Exec(context.Background(), `DELETE FROM people WHERE id = $1::uuid`, personID)
	})
	if _, err := MarkShopOrderPaid(app, order.ID, "stripe", "checkin_"+suffix, 0, 2500); err != nil {
		t.Fatalf("mark order paid: %s", err)
	}

	details, err := GetRegistrationCheckIn(app, ticketRef)
	if err != nil {
		t.Fatalf("load check-in details: %s", err)
	}
	if details.AttendeeName != "Check-in Test" || details.TShirtSize != "MM" || details.CheckedInAt == nil {
		t.Fatalf("check-in details = %+v", details)
	}
	previews, err := ListDevRegistrationCheckInPreviews(app)
	if err != nil {
		t.Fatalf("list check-in previews: %s", err)
	}
	foundPreview := false
	for _, preview := range previews {
		if preview.TicketRef == ticketRef {
			foundPreview = preview.AttendeeName == "Check-in Test" && preview.TShirtSize == "MM"
			break
		}
	}
	if !foundPreview {
		t.Fatal("seeded check-in preview was not returned with its attendee details")
	}
	pickups, err := ListShopPickupsForTicket(app, ticketRef)
	if err != nil || len(pickups) != 1 {
		t.Fatalf("pickups = (%d, %v), want one", len(pickups), err)
	}
	if err := MarkTicketPickups(app, ticketRef, []string{pickups[0].ID}, true, "volunteer@example.test"); err != nil {
		t.Fatalf("mark ticket pickups: %s", err)
	}

	details, err = GetRegistrationCheckIn(app, ticketRef)
	if err != nil || details.ShirtPickedUpAt == nil {
		t.Fatalf("shirt pickup = (%+v, %v)", details, err)
	}
	pickups, err = ListShopPickupsForTicket(app, ticketRef)
	if err != nil || len(pickups) != 1 || pickups[0].Status != types.ShopItemStatusFulfilled {
		t.Fatalf("fulfilled pickups = (%+v, %v)", pickups, err)
	}
	if err := MarkTicketPickups(app, ticketRef, []string{pickups[0].ID}, true, "volunteer@example.test"); err == nil {
		t.Fatal("replaying fulfilled pickups should fail")
	}
}

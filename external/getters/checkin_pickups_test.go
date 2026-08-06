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
		INSERT INTO people (name, tshirt)
		VALUES ('Check-in Test', 'MM')
		RETURNING id::text
	`).Scan(&personID); err != nil {
		t.Fatalf("create person: %s", err)
	}
	if _, err := app.DB.Exec(app.DatabaseContext(), `
		INSERT INTO person_emails (person_id, email, is_primary, verified_at)
		VALUES ($1::uuid, $2::citext, true, now())
	`, personID, email); err != nil {
		t.Fatalf("create person email: %s", err)
	}
	if _, err := app.DB.Exec(app.DatabaseContext(), `
		INSERT INTO registrations (ref_id, conference_id, type, email, person_id, checked_in_at, platform)
		VALUES ($1, $2::uuid, 'genpop', $3::citext, $4::uuid, now(), 'dev-checkin-preview')
	`, ticketRef, conferenceID, email, personID); err != nil {
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

	volunteerEmail := "volunteer-checkin-" + suffix + "@example.test"
	volunteerTicketRef := "volunteer-checkin-" + suffix
	volunteer := &types.Volunteer{
		Name:         "Volunteer Check-in Test",
		Email:        volunteerEmail,
		Phone:        "+1 555 0100",
		Signal:       "volunteer.01",
		Shirt:        "ML",
		Status:       "Scheduled",
		Availability: []string{},
		ScheduleFor:  []*types.Conf{{Ref: conferenceID}},
	}
	if err := RegisterVolunteer(app, volunteer); err != nil {
		t.Fatalf("register volunteer: %s", err)
	}
	t.Cleanup(func() {
		_, _ = app.DB.Exec(context.Background(), `DELETE FROM volunteers WHERE id = $1::uuid`, volunteer.Ref)
	})
	fetchedVolunteer, err := FetchVolunteer(app, volunteer.Ref)
	if err != nil {
		t.Fatalf("fetch normalized volunteer: %s", err)
	}
	if fetchedVolunteer.Name != volunteer.Name ||
		fetchedVolunteer.Email != volunteer.Email ||
		fetchedVolunteer.Shirt != volunteer.Shirt {
		t.Fatalf("normalized volunteer = %+v", fetchedVolunteer)
	}
	updatedVolunteer := &types.Volunteer{
		Name:         volunteer.Name,
		Email:        volunteer.Email,
		Phone:        "+1 555 0199",
		Signal:       volunteer.Signal,
		Shirt:        "MXXL",
		Status:       "Applied",
		Availability: []string{},
		ScheduleFor:  []*types.Conf{{Ref: conferenceID}},
	}
	if err := RegisterVolunteer(app, updatedVolunteer); err != nil {
		t.Fatalf("register returning volunteer: %s", err)
	}
	if updatedVolunteer.Ref != volunteer.Ref {
		t.Fatalf("returning volunteer application = %q, want existing %q", updatedVolunteer.Ref, volunteer.Ref)
	}
	if updatedVolunteer.Status != "Scheduled" {
		t.Fatalf("returning volunteer status = %q, want coordinator-managed Scheduled status preserved", updatedVolunteer.Status)
	}
	var applicationCount int
	if err := app.DB.QueryRow(app.DatabaseContext(), `
		SELECT count(*)
		FROM volunteers volunteer
		JOIN volunteers_conferences conference ON conference.volunteer_id = volunteer.id
		WHERE volunteer.email = $1::citext
			AND conference.conference_id = $2::uuid
			AND conference.kind = 'schedule_for'
	`, volunteer.Email, conferenceID).Scan(&applicationCount); err != nil {
		t.Fatalf("count returning volunteer applications: %s", err)
	}
	if applicationCount != 1 {
		t.Fatalf("returning volunteer application count = %d, want 1", applicationCount)
	}
	fetchedVolunteer, err = FetchVolunteer(app, volunteer.Ref)
	if err != nil {
		t.Fatalf("refetch normalized volunteer: %s", err)
	}
	if fetchedVolunteer.Phone != updatedVolunteer.Phone || fetchedVolunteer.Shirt != updatedVolunteer.Shirt {
		t.Fatalf("returning volunteer details were not updated: %+v", fetchedVolunteer)
	}
	if err := UpdateVolunteerStatus(app, volunteer.Ref, "Declined"); err != nil {
		t.Fatalf("decline volunteer before reapplication: %s", err)
	}
	reopenedVolunteer := *updatedVolunteer
	reopenedVolunteer.Ref = ""
	reopenedVolunteer.Status = "Applied"
	if err := RegisterVolunteer(app, &reopenedVolunteer); err != nil {
		t.Fatalf("reopen declined volunteer application: %s", err)
	}
	if reopenedVolunteer.Ref != volunteer.Ref || reopenedVolunteer.Status != "Applied" {
		t.Fatalf("reopened volunteer = (%q, %q), want (%q, Applied)", reopenedVolunteer.Ref, reopenedVolunteer.Status, volunteer.Ref)
	}
	if _, err := app.DB.Exec(app.DatabaseContext(), `
		INSERT INTO registrations (ref_id, conference_id, type, email, checked_in_at)
		VALUES ($1, $2::uuid, 'volunteer', $3::citext, now())
	`, volunteerTicketRef, conferenceID, volunteerEmail); err != nil {
		t.Fatalf("create volunteer registration: %s", err)
	}
	volunteerDetails, err := GetRegistrationCheckIn(app, volunteerTicketRef)
	if err != nil {
		t.Fatalf("load volunteer-only check-in details: %s", err)
	}
	if volunteerDetails.AttendeeName != "Volunteer Check-in Test" || volunteerDetails.TShirtSize != "MXXL" {
		t.Fatalf("volunteer-only check-in details = %+v", volunteerDetails)
	}
	if err := MarkTicketPickups(app, volunteerTicketRef, nil, true, "volunteer@example.test"); err != nil {
		t.Fatalf("mark volunteer-only conference shirt picked up: %s", err)
	}
}

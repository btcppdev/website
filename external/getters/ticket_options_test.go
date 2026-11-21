package getters

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/eventplan"
	"btcpp-web/internal/types"
)

func evergreenFixture(t *testing.T) (*config.AppContext, *types.Conf, []eventplan.Ticket) {
	t.Helper()
	app := postgresSmokeContext(t)
	tag := "evergreen-" + postgresSmokeSuffix()
	id, err := CreatePlannedEvent(app, eventplan.Basics{Tag: tag, Name: "Ticket options", Timezone: "UTC", Edition: "global"}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.DB.Exec(context.Background(), `DELETE FROM conferences WHERE id=$1`, id) })
	conf, err := GetConfByTag(app, tag)
	if err != nil {
		t.Fatal(err)
	}
	tickets := []eventplan.Ticket{
		{Tier: "Early", Price: 200, Capacity: 2, Currency: "EUR", SalesEnd: "2099-01-01T10:00", TaxCode: "txcd_20030000", CardSurchargeBPS: 1000},
		{Tier: "Standard", Price: 300, Capacity: 4, Currency: "EUR", SalesEnd: "2099-02-01T10:00", TaxCode: "txcd_20030000", CardSurchargeBPS: 1000},
		{Tier: "Student", Price: 50, Capacity: 1, Currency: "EUR", TaxCode: "txcd_20030000", CardSurchargeBPS: 1000, Evergreen: true, TicketKind: "student", Description: "Bring student ID"},
		{Tier: "Supporter", Price: 500, Capacity: 10, Currency: "EUR", TaxCode: "txcd_20030000", CardSurchargeBPS: 1000, Evergreen: true, TicketKind: "supporter", AllowDiscounts: true},
	}
	raw, _ := json.Marshal(eventplan.TicketSettings{Tickets: tickets, Offers: []eventplan.CheckoutOffer{{Label: "Eurocrypt ticket holders", Percent: 15, Enabled: true}}})
	if _, err = SaveEventPlanning(app, conf, 0, "", "tickets", raw); err != nil {
		t.Fatal(err)
	}
	conf, err = GetConfByTag(app, tag)
	if err != nil {
		t.Fatal(err)
	}
	for i := range tickets {
		for _, loaded := range conf.Tickets {
			if tickets[i].Tier == loaded.Tier {
				tickets[i].ID = loaded.ID
			}
		}
	}
	return app, conf, tickets
}
func ticketNamed(t *testing.T, conf *types.Conf, name string) *types.ConfTicket {
	t.Helper()
	for _, ticket := range conf.Tickets {
		if ticket.Tier == name {
			return ticket
		}
	}
	t.Fatal("missing ticket", name)
	return nil
}

func TestEvergreenInventoryAndConcurrentLastTicket(t *testing.T) {
	app, conf, _ := evergreenFixture(t)
	student := ticketNamed(t, conf, "Student")
	early := ticketNamed(t, conf, "Early")
	supporter := ticketNamed(t, conf, "Supporter")
	if !student.SalesEndAt.IsZero() || student.DiscountsAllowed() || !supporter.DiscountsAllowed() {
		t.Fatal("evergreen settings not hydrated")
	}
	var wg sync.WaitGroup
	results := make(chan string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := ReserveTickets(app, conf, student, 1, "buyer@example.test", "stripe")
			if err == nil {
				results <- id
			}
		}()
	}
	wg.Wait()
	close(results)
	var ids []string
	for id := range results {
		ids = append(ids, id)
	}
	if len(ids) != 1 {
		t.Fatalf("last student ticket reserved %d times", len(ids))
	}
	if err := AttachTicketPayment(app, ids[0], "cs_test"); err != nil {
		t.Fatal(err)
	}
	entry := &types.Entry{ID: "cs_test", ConfRef: conf.Ref, TicketID: student.ID, ReservationID: ids[0], Email: "buyer@example.test", Currency: "EUR", Created: time.Now(), Items: []types.Item{{Total: 5000, Type: "student", Desc: "Student"}}}
	for i := 0; i < 2; i++ {
		if err := AddTickets(app, entry, "stripe"); err != nil {
			t.Fatal(err)
		}
	}
	inv, err := TicketInventory(app, conf.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Sold != 1 || inv.Held != 0 || inv.ByTicket[student.ID] != 1 {
		t.Fatalf("fulfillment counted twice: %+v", inv)
	}
	if err = ReleaseTicketReservation(app, ids[0], "stripe", "cs_test"); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveTickets(app, conf, student, 1, "b@example.test", "stripe"); err == nil {
		t.Fatal("sold-out student ticket oversold")
	}
	earlyID, err := ReserveTickets(app, conf, early, 1, "b@example.test", "stripe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveTickets(app, conf, early, 1, "c@example.test", "stripe"); err == nil {
		t.Fatal("evergreen sales did not advance standard tier")
	}
	if err = ReleaseTicketReservation(app, earlyID, "stripe", ""); err != nil {
		t.Fatal(err)
	}
	inv, _ = TicketInventory(app, conf.Ref)
	if inv.Held != 0 || inv.Remaining(early, conf.Tickets, time.Now()) != 1 {
		t.Fatal("expiry did not release inventory")
	}
	if _, err = ReserveTickets(app, conf, supporter, 3, "c@example.test", "opennode"); err != nil {
		t.Fatal(err)
	}
	if _, err = ReserveTickets(app, conf, supporter, 1, "d@example.test", "stripe"); err == nil {
		t.Fatal("evergreen oversold event-wide capacity")
	}
	if inv.Remaining(student, conf.Tickets, time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)) != 0 {
		t.Fatal("evergreen remained available after event sales ended")
	}
}
func TestEvergreenOffersAndSetupEdits(t *testing.T) {
	app, conf, tickets := evergreenFixture(t)
	offers, err := ListCheckoutOffers(app, conf.Ref, true)
	if err != nil || len(offers) != 1 {
		t.Fatalf("offers: %v %v", offers, err)
	}
	price, disc, err := CalcDiscount(app, conf.Ref, offers[0].Code, 200, 1)
	if err != nil || price != 170 || disc.Ref != offers[0].ID {
		t.Fatalf("15 percent offer: %d %v", price, err)
	}
	if _, _, err = CalcDiscount(app, "00000000-0000-0000-0000-000000000000", offers[0].Code, 200, 1); err == nil {
		t.Fatal("offer crossed event boundary")
	}
	raw, _ := json.Marshal(eventplan.TicketSettings{Tickets: tickets[:2]})
	if _, err = SaveEventPlanning(app, conf, 1, "", "tickets", raw); err != nil {
		t.Fatal(err)
	}
	if _, _, err = CalcDiscount(app, conf.Ref, offers[0].Code, 200, 1); err == nil {
		t.Fatal("removed offer still redeemable")
	}
	conf, err = GetConfByTag(app, conf.Tag)
	if err != nil {
		t.Fatal(err)
	}
	if !ticketNamed(t, conf, "Student").Disabled {
		t.Fatal("omitted ticket still on sale")
	}
	if _, err = ReserveTickets(app, conf, ticketNamed(t, conf, "Student"), 1, "buyer@example.test", "stripe"); err == nil {
		t.Fatal("paused ticket reserved")
	}
}

func TestEvergreenOfferRestrictions(t *testing.T) {
	app, conf, tickets := evergreenFixture(t)
	offers, err := ListCheckoutOffers(app, conf.Ref, false)
	if err != nil || len(offers) != 1 {
		t.Fatalf("offers: %v %v", offers, err)
	}
	const note = "Proof of Eurocrypt ticket will be requested at the door."
	offers[0].Restrictions = "  " + note + "  "
	save := func(revision int64) error {
		raw, _ := json.Marshal(eventplan.TicketSettings{Tickets: tickets, Offers: offers})
		_, err := SaveEventPlanning(app, conf, revision, "", "tickets", raw)
		return err
	}
	if err = save(1); err != nil {
		t.Fatal(err)
	}
	for _, public := range []bool{false, true} {
		loaded, err := ListCheckoutOffers(app, conf.Ref, public)
		if err != nil || len(loaded) != 1 || loaded[0].Restrictions != note {
			t.Fatalf("restrictions round trip: %v %v", loaded, err)
		}
	}
	offers[0].Restrictions = strings.Repeat("x", 501)
	if err = save(2); err == nil {
		t.Fatal("oversized restriction accepted")
	}
	loaded, err := ListCheckoutOffers(app, conf.Ref, true)
	if err != nil || len(loaded) != 1 || loaded[0].Restrictions != note {
		t.Fatal("invalid update replaced saved restrictions", loaded, err)
	}
	offers[0].Restrictions = ""
	if err = save(2); err != nil {
		t.Fatal(err)
	}
	loaded, err = ListCheckoutOffers(app, conf.Ref, true)
	if err != nil || len(loaded) != 1 || loaded[0].Restrictions != "" {
		t.Fatal("optional restrictions were not cleared", loaded, err)
	}
}
func TestEvergreenAvailabilityAcrossStandardDeadlines(t *testing.T) {
	now := time.Now()
	early := &types.ConfTicket{ID: "early", Max: 2, SalesEndAt: now.Add(-time.Hour)}
	standard := &types.ConfTicket{ID: "standard", Max: 10, SalesEndAt: now.Add(time.Hour)}
	student := &types.ConfTicket{ID: "student", Evergreen: true, Max: 3}
	all := []*types.ConfTicket{nil, student, early, standard}
	a := &TicketAvailability{Sold: 2, ByTicket: map[string]uint{"student": 1}}
	if got := types.CurrentConfTicketAt(all, a.Sold, now); got != standard {
		t.Fatalf("current tier=%+v", got)
	}
	if a.Remaining(student, all, now) != 2 || a.Remaining(early, all, now) != 0 {
		t.Fatal("evergreen incorrectly followed expired tier")
	}
	if a.Remaining(student, all, now.Add(2*time.Hour)) != 0 {
		t.Fatal("evergreen outside sales window")
	}
}

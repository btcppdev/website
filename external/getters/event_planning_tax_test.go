package getters

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"btcpp-web/internal/eventplan"
	"btcpp-web/internal/types"
)

func TestPlanningTicketsOptionalTaxClassification(t *testing.T) {
	app := postgresSmokeContext(t)
	id, tag := insertSmokeConference(t, app)
	conf, err := GetConfByTag(app, tag)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := LoadEventPlanning(app, id)
	if err != nil {
		t.Fatal(err)
	}
	revision := plan.Revision
	tickets := []eventplan.Ticket{
		{Tier: "Standard", Price: 200, Capacity: 100, Currency: "EUR", SalesEnd: "2099-01-01T10:00"},
		{Tier: "Student", Price: 50, Capacity: 20, Currency: "EUR", Evergreen: true, TicketKind: "student", TaxCode: " \t "},
	}
	save := func() error {
		t.Helper()
		raw, err := json.Marshal(eventplan.TicketSettings{Tickets: tickets})
		if err != nil {
			t.Fatal(err)
		}
		next, err := SaveEventPlanning(app, conf, revision, "", "tickets", raw)
		if err == nil {
			revision = next
			conf, err = GetConfByTag(app, tag)
		}
		return err
	}
	if err = save(); err != nil {
		t.Fatalf("ticket prices without tax codes should save: %v", err)
	}
	for i := range tickets {
		stored := ticketNamed(t, conf, tickets[i].Tier)
		tickets[i].ID = stored.ID
		if stored.StripeTaxCode != types.StripeTaxCodeNontaxable {
			t.Fatalf("%s tax code = %q; want existing checkout default", stored.Tier, stored.StripeTaxCode)
		}
	}

	// An explicit classification remains available, including surrounding whitespace.
	tickets[0].TaxCode = " txcd_50013001 "
	if err = save(); err != nil {
		t.Fatal(err)
	}
	if got := ticketNamed(t, conf, "Standard").StripeTaxCode; got != "txcd_50013001" {
		t.Fatalf("override was not retained: %q", got)
	}

	// A later price-only edit must not silently reset an existing classification.
	tickets[0].TaxCode = ""
	tickets[0].Price = 250
	if err = save(); err != nil {
		t.Fatal(err)
	}
	standard := ticketNamed(t, conf, "Standard")
	if standard.StandardPrice() != 250 || standard.StripeTaxCode != "txcd_50013001" {
		t.Fatalf("price-only edit changed tax classification: price=%d tax=%q", standard.StandardPrice(), standard.StripeTaxCode)
	}
	if got := ticketNamed(t, conf, "Student").StripeTaxCode; got != types.StripeTaxCodeNontaxable {
		t.Fatalf("evergreen classification changed: %q", got)
	}

	// Invalid overrides still fail, and roll back price edits made earlier in the batch.
	tickets[0].Price = 300
	tickets[1].TaxCode = "not-a-tax-code"
	if err = save(); err == nil || !strings.Contains(err.Error(), "invalid Stripe tax code override for Student") {
		t.Fatalf("invalid override error = %v", err)
	}
	conf, err = GetConfByTag(app, tag)
	if err != nil {
		t.Fatal(err)
	}
	if got := ticketNamed(t, conf, "Standard").StandardPrice(); got != 250 {
		t.Fatalf("failed save changed price to %d", got)
	}
	var savedRevision int64
	if err = app.DB.QueryRow(context.Background(), `SELECT revision FROM event_plans WHERE conference_id=$1`, id).Scan(&savedRevision); err != nil || savedRevision != revision {
		t.Fatalf("failed save changed revision: %d, %v", savedRevision, err)
	}
}

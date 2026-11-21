package getters

import (
	"btcpp-web/internal/eventplan"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestEventPlanningLifecycle(t *testing.T) {
	app := postgresSmokeContext(t)
	c := context.Background()
	b := eventplan.Basics{Tag: "wizard-" + postgresSmokeSuffix(), Name: "Tokyo planning", Edition: "global", Timezone: "Asia/Tokyo", Start: "2027-05-12T09:00", End: "2027-05-13T18:00", Country: "JP"}
	id, e := CreatePlannedEvent(app, b, "")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { app.DB.Exec(c, `DELETE FROM conferences WHERE id=$1`, id) })
	conf, e := GetConfByTag(app, b.Tag)
	if e != nil {
		t.Fatal(e)
	}
	if conf.IsPublished() || conf.Active {
		t.Fatal("draft became public")
	}
	revision := int64(0)
	save := func(section string, v any) {
		t.Helper()
		raw, _ := json.Marshal(v)
		revision, e = SaveEventPlanning(app, conf, revision, "", section, raw)
		if e != nil {
			t.Fatal(section, e)
		}
		conf, e = GetConfByTag(app, b.Tag)
		if e != nil {
			t.Fatal(e)
		}
	}
	venues := []eventplan.Venue{{ID: "hall", Name: "Hall", Stages: "Main,Workshop", Country: "JP", Days: []eventplan.Day{{Date: "2027-05-12", StaffOpen: "07:00", DoorsOpen: "09:00", DoorsClose: "18:00", StaffClose: "20:00"}}}}
	save("venues", map[string]any{"venues": venues})
	var days int
	if e = app.DB.QueryRow(c, `SELECT count(*) FROM conference_days WHERE conference_id=$1 AND doors_start='09:00'`, id).Scan(&days); e != nil || days != 1 {
		t.Fatalf("schedule sync %d %v", days, e)
	}
	raw, _ := json.Marshal(b)
	if _, e = SaveEventPlanning(app, conf, 0, "", "basics", raw); !errors.Is(e, ErrEventPlanConflict) {
		t.Fatalf("stale edit: %v", e)
	}
	b.Start = "2027-05-13T09:00"
	raw, _ = json.Marshal(b)
	if _, e = SaveEventPlanning(app, conf, revision, "", "basics", raw); e == nil {
		t.Fatal("accepted dates excluding venue days")
	}
	applications := eventplan.Applications{TalksOpen: "2027-01-01T09:00", TalksClose: "2027-04-01T18:00", VolunteersOpen: "2027-02-01T09:00", VolunteersClose: "2027-05-01T18:00", ShiftHours: 4, MerchDesk: true}
	save("applications", applications)
	if conf.SpeakerApplicationsOpen == nil || conf.VolunteerApplicationsClose == nil {
		t.Fatal("application windows not hydrated")
	}
	tier := eventplan.Ticket{Tier: "Early", Price: 10000, Capacity: 100, Currency: "JPY", TaxCode: "txcd_20030000", SalesEnd: "2027-05-12T09:00", CardSurchargeBPS: 1000}
	save("tickets", eventplan.TicketSettings{Tickets: []eventplan.Ticket{tier}, StudentEligibility: "Bring student ID"})
	if len(conf.Tickets) != 1 || conf.Tickets[0].StudentPrice != nil || conf.Tickets[0].StandardPrice() != 10000 || conf.StudentTicketBody != "Bring student ID" {
		t.Fatal("standard ticket hydration")
	}
	tier.ID = conf.Tickets[0].ID
	tier.Price = 12000
	save("tickets", eventplan.TicketSettings{Tickets: []eventplan.Ticket{tier}})
	if len(conf.Tickets) != 1 || conf.Tickets[0].StandardPrice() != 12000 {
		t.Fatal("tier update created duplicate")
	}
	preview, e := ReconcileEventStaffing(app, conf, revision, false)
	if e != nil || len(preview) == 0 {
		t.Fatalf("preview %v", e)
	}
	var count int
	app.DB.QueryRow(c, `SELECT count(*) FROM work_shifts WHERE conference_id=$1`, id).Scan(&count)
	if count != 0 {
		t.Fatal("preview wrote shifts")
	}
	if _, e = ReconcileEventStaffing(app, conf, revision, true); e != nil {
		t.Fatal(e)
	}
	again, e := ReconcileEventStaffing(app, conf, revision, true)
	if e != nil {
		t.Fatal(e)
	}
	for _, ch := range again {
		if ch.Action != "keep" {
			t.Fatalf("not idempotent: %+v", ch)
		}
	}
	app.DB.Exec(c, `UPDATE work_shifts SET name='Custom crew shift' WHERE conference_id=$1 AND generation_key=$2`, id, preview[0].Shift.Key)
	changed, e := ReconcileEventStaffing(app, conf, revision, true)
	if e != nil {
		t.Fatal(e)
	}
	if changed[0].Action != "review" {
		t.Fatal("manual shift overwritten")
	}
	save("venues", map[string]any{"venues": []eventplan.Venue{}})
	app.DB.QueryRow(c, `SELECT count(*) FROM conference_days WHERE conference_id=$1 AND doors_start IS NOT NULL`, id).Scan(&count)
	if count != 0 {
		t.Fatal("stale public door times")
	}
}
func TestEventTeamInvitationLifecycle(t *testing.T) {
	app := postgresSmokeContext(t)
	c := context.Background()
	suffix := postgresSmokeSuffix()
	id, tag := insertSmokeConference(t, app)
	person, e := CreateSpeaker(app, SpeakerInput{Name: "Planner", Email: "planner-" + suffix + "@example.test"})
	if e != nil {
		t.Fatal(e)
	}
	wrong, e := CreateSpeaker(app, SpeakerInput{Name: "Other", Email: "other-" + suffix + "@example.test"})
	if e != nil {
		t.Fatal(e)
	}
	app.DB.Exec(c, `UPDATE person_emails SET verified_at=now() WHERE person_id=$1`, person)
	token, e := CreateEventTeamInvite(app, id, "", EventTeamMember{Email: "planner-" + suffix + "@example.test", Role: "staff"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = AcceptEventTeamInvite(app, token, wrong); e == nil {
		t.Fatal("wrong account accepted")
	}
	if _, e = AcceptEventTeamInvite(app, token, person); e != nil {
		t.Fatal(e)
	}
	if _, e = AcceptEventTeamInvite(app, token, person); e == nil {
		t.Fatal("invite replay")
	}
	members, e := ListEventTeam(app, id)
	if e != nil || len(members) != 1 {
		t.Fatal(e)
	}
	if !EventTeamCanRead(app, id, person) {
		t.Fatal("accepted team cannot read")
	}
	if e = RevokeEventTeamInvite(app, id, members[0].ID); e != nil {
		t.Fatal(e)
	}
	var roles int
	app.DB.QueryRow(c, `SELECT count(*) FROM people_roles WHERE person_id=$1 AND scope=$2`, person, tag).Scan(&roles)
	if roles != 0 {
		t.Fatal("role not revoked")
	}
	app.DB.Exec(c, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,$2,'staff')`, person, tag)
	token, e = CreateEventTeamInvite(app, id, "", EventTeamMember{Email: "planner-" + suffix + "@example.test", Role: "staff"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = AcceptEventTeamInvite(app, token, person); e != nil {
		t.Fatal(e)
	}
	members, _ = ListEventTeam(app, id)
	if e = RevokeEventTeamInvite(app, id, members[0].ID); e != nil {
		t.Fatal(e)
	}
	app.DB.QueryRow(c, `SELECT count(*) FROM people_roles WHERE person_id=$1 AND scope=$2`, person, tag).Scan(&roles)
	if roles != 1 {
		t.Fatal("pre-existing role revoked")
	}
}

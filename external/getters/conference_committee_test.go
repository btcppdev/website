package getters

import (
	"context"
	"strings"
	"testing"
)

func TestConferenceCommittee(t *testing.T) {
	app := postgresSmokeContext(t)
	c := context.Background()
	conf, _ := insertSmokeConference(t, app)
	other, _ := insertSmokeConference(t, app)
	person := insertSmokePerson(t, app, "committee-"+postgresSmokeSuffix())
	if _, err := app.DB.Exec(c, `UPDATE people SET name='Current profile name',norm_photo_path='portrait.avif',company='Profile company',org_logo_path='profile.svg' WHERE id=$1`, person); err != nil {
		t.Fatal(err)
	}
	if err := AddEventTeamMember(app, conf, "", EventTeamMember{PersonID: person, Role: "steering", Track: "  Cryptography  ", Responsibility: "Private planning notes"}); err != nil {
		t.Fatal(err)
	}
	team, err := ListEventTeam(app, conf)
	if err != nil || len(team) != 1 || team[0].Track != "Cryptography" {
		t.Fatalf("team track: %+v %v", team, err)
	}
	memberID := team[0].ID
	for _, fixture := range []struct{ name, status, light, dark string }{
		{"A independent organization", "active", "https://example.test/light.svg", ""},
		{"B another organization", "active", "", "https://example.test/dark.svg"},
		{"C no logo", "active", "", ""},
		{"Removed organization", "removed", "https://example.test/removed.svg", ""},
	} {
		var org string
		if err := app.DB.QueryRow(c, `INSERT INTO organizations(name,logo_light_url,logo_dark_url) VALUES($1,$2,$3) RETURNING id::text`, fixture.name, fixture.light, fixture.dark).Scan(&org); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { app.DB.Exec(c, `DELETE FROM organizations WHERE id=$1`, org) })
		if _, err := app.DB.Exec(c, `INSERT INTO organization_memberships(organization_id,person_id,role,status) VALUES($1,$2,'member',$3)`, org, person, fixture.status); err != nil {
			t.Fatal(err)
		}
	}
	// No talk, ticket, or sponsorship exists for the public committee member.
	for _, fixture := range []struct {
		role, conf        string
		accepted, deleted bool
	}{
		{"steering", conf, false, false}, {"staff", conf, true, false}, {"steering", other, true, false}, {"steering", conf, true, true},
	} {
		id := insertSmokePerson(t, app, "excluded-committee-"+postgresSmokeSuffix())
		if _, err := app.DB.Exec(c, `INSERT INTO event_team_members(conference_id,person_id,role,accepted_at) VALUES($1,$2,$3,CASE WHEN $4 THEN now() END)`, fixture.conf, id, fixture.role, fixture.accepted); err != nil {
			t.Fatal(err)
		}
		if fixture.deleted {
			if _, err := app.DB.Exec(c, `UPDATE people SET is_deleted_account=true WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	got, err := ListConferenceCommittee(app, conf)
	if err != nil || len(got) != 1 {
		t.Fatalf("public committee: %+v %v", got, err)
	}
	m := got[0]
	if m.Speaker.ID != person || m.Speaker.Name != "Current profile name" || m.Speaker.Photo != "portrait.avif" || m.Track != "Cryptography" {
		t.Fatalf("public member: %+v %+v", m, m.Speaker)
	}
	if len(m.Organizations) != 3 || m.Organizations[1].LogoDark != "https://example.test/dark.svg" || m.Organizations[2].Name != "C no logo" {
		t.Fatalf("non-sponsor affiliations: %+v", m.Organizations)
	}
	if m.Speaker.Email != "" || m.Speaker.Phone != "" {
		t.Fatal("public member loaded private contact details")
	}
	if err := UpdateEventSteeringTrack(app, other, memberID, "Wrong event"); err == nil {
		t.Fatal("cross-event track update accepted")
	}
	if err := UpdateEventSteeringTrack(app, conf, memberID, strings.Repeat("x", 121)); err == nil {
		t.Fatal("overlong track accepted")
	}
	if err := UpdateEventSteeringTrack(app, conf, memberID, "  Privacy  "); err != nil {
		t.Fatal(err)
	}
	got, _ = ListConferenceCommittee(app, conf)
	if got[0].Track != "Privacy" {
		t.Fatal("saved track missing")
	}
	if err := UpdateEventSteeringTrack(app, conf, memberID, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = ListConferenceCommittee(app, conf)
	if got[0].Track != "" {
		t.Fatal("track could not be cleared")
	}
	// Profile organization data remains available when there are no memberships.
	solo := insertSmokePerson(t, app, "solo-committee-"+postgresSmokeSuffix())
	if _, err := app.DB.Exec(c, `UPDATE people SET company='Independent lab',org_logo_path='lab.svg' WHERE id=$1`, solo); err != nil {
		t.Fatal(err)
	}
	if err := AddEventTeamMember(app, conf, "", EventTeamMember{PersonID: solo, Role: "steering"}); err != nil {
		t.Fatal(err)
	}
	got, err = ListConferenceCommittee(app, conf)
	if err != nil || len(got) != 2 {
		t.Fatalf("optional track: %+v %v", got, err)
	}
	for _, m := range got {
		if m.Speaker.ID == solo && (m.Track != "" || len(m.Organizations) != 0 || m.Speaker.Company != "Independent lab" || m.Speaker.OrgLogo != "lab.svg") {
			t.Fatalf("profile affiliation fallback: %+v", m)
		}
	}
	if err := RevokeEventTeamInvite(app, conf, memberID); err != nil {
		t.Fatal(err)
	}
	got, err = ListConferenceCommittee(app, conf)
	if err != nil || len(got) != 1 || got[0].Speaker.ID != solo {
		t.Fatal("removed member still public", err)
	}
}

func TestEventSteeringTrackInvites(t *testing.T) {
	app := postgresSmokeContext(t)
	conf, _ := insertSmokeConference(t, app)
	email := "committee-invite-" + postgresSmokeSuffix() + "@example.test"
	if _, err := CreateEventTeamInvite(app, conf, "", EventTeamMember{Email: email, Role: "steering", Track: "  Protocol  "}); err != nil {
		t.Fatal(err)
	}
	team, err := ListEventTeam(app, conf)
	if err != nil || len(team) != 1 || team[0].Track != "Protocol" {
		t.Fatalf("invited track: %+v %v", team, err)
	}
	if got, err := ListConferenceCommittee(app, conf); err != nil || len(got) != 0 {
		t.Fatal("unaccepted invite was public", err)
	}
	if _, err := CreateEventTeamInvite(app, conf, "", EventTeamMember{Email: email, Role: "steering", Track: strings.Repeat("x", 121)}); err == nil {
		t.Fatal("overlong invite track accepted")
	}
	staff := insertSmokePerson(t, app, "staff-track-"+postgresSmokeSuffix())
	if err := AddEventTeamMember(app, conf, "", EventTeamMember{PersonID: staff, Role: "staff", Track: "Not public"}); err != nil {
		t.Fatal(err)
	}
	team, _ = ListEventTeam(app, conf)
	for _, m := range team {
		if m.PersonID == staff {
			if m.Track != "" {
				t.Fatal("staff track persisted")
			}
			if err := UpdateEventSteeringTrack(app, conf, m.ID, "Invalid"); err == nil {
				t.Fatal("staff track update accepted")
			}
		}
	}
}

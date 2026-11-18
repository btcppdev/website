package getters

import (
	"context"
	"sync"
	"testing"
)

func TestEventTeamExistingAccounts(t *testing.T) {
	app := postgresSmokeContext(t)
	c := context.Background()
	conf, tag := insertSmokeConference(t, app)
	person, e := CreateSpeaker(app, SpeakerInput{Name: "Existing team member", Email: "existing-team-" + postgresSmokeSuffix() + "@example.test"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { app.DB.Exec(c, `DELETE FROM people WHERE id=$1`, person) })
	in := EventTeamMember{PersonID: person, Name: "Forged name", Email: "wrong@example.test", Role: "staff", Responsibility: "Venue lead"}
	// Parallel submissions must create one membership and one event-only role.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- AddEventTeamMember(app, conf, "", in) })
	}
	wg.Wait()
	close(results)
	added := 0
	for e := range results {
		if e == nil {
			added++
		}
	}
	if added != 1 {
		t.Fatalf("added %d duplicate memberships", added)
	}
	members, e := ListEventTeam(app, conf)
	if e != nil || len(members) != 1 || members[0].Name != "Existing team member" || members[0].Email == in.Email || members[0].PersonID != person {
		t.Fatalf("account identity not authoritative: %+v %v", members, e)
	}
	if !EventTeamCanRead(app, conf, person) {
		t.Fatal("selected account cannot view plan")
	}
	var roleCount int
	if e = app.DB.QueryRow(c, `SELECT count(*) FROM people_roles WHERE person_id=$1 AND scope=$2 AND position='staff'`, person, tag).Scan(&roleCount); e != nil || roleCount != 1 {
		t.Fatalf("event role %d %v", roleCount, e)
	}
	if e = RevokeEventTeamInvite(app, conf, members[0].ID); e != nil {
		t.Fatal(e)
	}
	app.DB.QueryRow(c, `SELECT count(*) FROM people_roles WHERE person_id=$1`, person).Scan(&roleCount)
	if roleCount != 0 {
		t.Fatal("membership role survived removal")
	}
	// Adding a person already holding a role must not take ownership of that grant.
	if _, e = app.DB.Exec(c, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,$2,'staff')`, person, tag); e != nil {
		t.Fatal(e)
	}
	if e = AddEventTeamMember(app, conf, "", in); e != nil {
		t.Fatal(e)
	}
	members, _ = ListEventTeam(app, conf)
	if e = RevokeEventTeamInvite(app, conf, members[0].ID); e != nil {
		t.Fatal(e)
	}
	app.DB.QueryRow(c, `SELECT count(*) FROM people_roles WHERE person_id=$1 AND scope=$2`, person, tag).Scan(&roleCount)
	if roleCount != 1 {
		t.Fatal("pre-existing role removed")
	}
	// People without email can still be selected; each remains a distinct member.
	for range 2 {
		var id string
		if e = app.DB.QueryRow(c, `INSERT INTO people(name) VALUES('Email-free planner') RETURNING id::text`).Scan(&id); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { app.DB.Exec(c, `DELETE FROM people WHERE id=$1`, id) })
		if e = AddEventTeamMember(app, conf, "", EventTeamMember{PersonID: id, Role: "steering"}); e != nil {
			t.Fatal(e)
		}
	}
	members, e = ListEventTeam(app, conf)
	if e != nil || len(members) != 2 {
		t.Fatalf("email-free accounts %+v %v", members, e)
	}
	if e = AddEventTeamMember(app, conf, "", EventTeamMember{PersonID: person, Role: "global"}); e == nil {
		t.Fatal("invalid role accepted")
	}
	var deleted string
	if e = app.DB.QueryRow(c, `INSERT INTO people(name,is_deleted_account) VALUES('Deleted test',true) RETURNING id::text`).Scan(&deleted); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { app.DB.Exec(c, `DELETE FROM people WHERE id=$1`, deleted) })
	if e = AddEventTeamMember(app, conf, "", EventTeamMember{PersonID: deleted, Role: "steering"}); e == nil {
		t.Fatal("deleted account added")
	}
}

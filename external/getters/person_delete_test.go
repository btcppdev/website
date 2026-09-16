package getters

import (
	"btcpp-web/internal/types"
	"context"
	"github.com/google/uuid"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestDatabaseSmokeDeletePersonPreservesHistory(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	actor := insertSmokePerson(t, app, "Deletion admin")
	source := insertSmokePerson(t, app, "Delete me")
	teammate := insertSmokePerson(t, app, "Keep me")
	conf, _ := insertSmokeConference(t, app)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := app.DB.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	scalar := func(q string, args ...any) string {
		t.Helper()
		var s string
		if err := app.DB.QueryRow(ctx, q, args...).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	email := "delete-" + uuid.NewString() + "@example.test"
	exec(`INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, actor)
	exec(`INSERT INTO person_emails(person_id,email,is_primary) VALUES($1,$2,false)`, source, email)
	exec(`INSERT INTO person_password_credentials(person_id,password_hash) VALUES($1,'not-a-real-hash')`, source)
	competition := scalar(`INSERT INTO competitions(conference_id,title) VALUES($1,'Delete test') RETURNING id::text`, conf)
	project := scalar(`INSERT INTO projects(competition_id,created_by_person_id,title,slug,status) VALUES($1,$2,'Keep project','delete-test','submitted') RETURNING id::text`, competition, source)
	exec(`INSERT INTO project_members(project_id,person_id,role) VALUES($1,$2,'owner'),($1,$3,'member')`, project, source, teammate)
	speaker := scalar(`INSERT INTO speaker_confs(speaker_id,company,coming_from) VALUES($1,'Private employer','Private city') RETURNING id::text`, source)
	proposal := scalar(`INSERT INTO proposals(conference_id,title) VALUES($1,'Keep talk') RETURNING id::text`, conf)
	exec(`INSERT INTO proposals_speaker_confs(proposal_id,speaker_conf_id) VALUES($1,$2)`, proposal, speaker)
	entry := &types.Entry{ID: uuid.NewString(), ConfRef: conf, Email: email, Created: time.Now(), Items: []types.Item{{Type: "general", Desc: "Ticket", Total: 1000}}}
	if err := AddTickets(app, entry, "stripe"); err != nil {
		t.Fatal(err)
	}
	ticketID := types.UniqueID(email, entry.ID, 0)
	exec(`UPDATE registrations SET checked_in_at=now() WHERE ref_id=$1`, ticketID)
	revokedTicketID := uuid.NewString()
	exec(`INSERT INTO registrations(ref_id,conference_id,person_id,email,revoked,checked_in_at) VALUES($1,$2,$3,$4,true,now())`, revokedTicketID, conf, source, email)
	exec(`INSERT INTO volunteers(person_id,comments) VALUES($1,'private note')`, source)
	exec(`INSERT INTO sessions(token,data,expiry) VALUES($1,convert_to($2,'UTF8'),now()+interval '1 day')`, uuid.NewString(), source)

	segment := scalar(`INSERT INTO competition_schedule_segments(competition_id,title) VALUES($1,'Review') RETURNING id::text`, competition)
	event := scalar(`INSERT INTO judge_events(competition_id,schedule_segment_id,name,playbook_type) VALUES($1,$2,'Review','expo') RETURNING id::text`, competition, segment)
	exec(`INSERT INTO scorecards(judge_event_id,project_id,judge_person_id,rank,comments) VALUES($1,$2,$3,1,'private comment')`, event, project, source)
	award := scalar(`INSERT INTO awards(competition_id,title,status) VALUES($1,'First','awarded') RETURNING id::text`, competition)
	prize := scalar(`INSERT INTO prizes(award_id,prize_type,title) VALUES($1,'sats','Sats') RETURNING id::text`, award)
	exec(`INSERT INTO project_awards(project_id,award_id) VALUES($1,$2)`, project, award)
	exec(`INSERT INTO award_distributions(competition_id,award_id,project_id,prize_id,person_id,distribution_type,amount_sats,status,notes) VALUES($1,$2,$3,$4,$5,'sats',1000,'sent','private payout note')`, competition, award, project, prize, source)
	order := scalar(`INSERT INTO shop_orders(public_id,buyer_person_id,buyer_name,buyer_email,total_cents) VALUES($1,$2,'Private name',$3,2500) RETURNING id::text`, uuid.NewString(), source, email)
	t.Cleanup(func() { app.DB.Exec(ctx, `DELETE FROM shop_orders WHERE id=$1`, order) })
	exec(`INSERT INTO shop_order_addresses(order_id,name,line1) VALUES($1,'Private name','Private address')`, order)
	exec(`INSERT INTO shipments(order_id,raw_response,tracking_url) VALUES($1,'{"name":"Private name"}','https://private.example')`, order)
	mergeSource := uuid.NewString()
	exec(`INSERT INTO person_merge_events(canonical_person_id,source_person_id,canonical_snapshot,source_snapshot,undo_expires_at) VALUES($1,$2,'{"name":"Private name"}','{"name":"Old name"}',now()+interval '1 day')`, source, mergeSource)
	exec(`UPDATE conferences SET publication_status='published' WHERE id=$1`, conf)
	exec(`UPDATE competitions SET visibility='public', public_gallery_enabled=true WHERE id=$1`, competition)
	exec(`UPDATE proposals SET status='Accepted' WHERE id=$1`, proposal)
	exec(`INSERT INTO conf_talks(conference_id,proposal_id) VALUES($1,$2)`, conf, proposal)
	coSpeaker := scalar(`INSERT INTO speaker_confs(speaker_id) VALUES($1) RETURNING id::text`, teammate)
	t.Cleanup(func() { app.DB.Exec(ctx, `DELETE FROM speaker_confs WHERE id=$1`, coSpeaker) })
	exec(`INSERT INTO proposals_speaker_confs(proposal_id,speaker_conf_id) VALUES($1,$2)`, proposal, coSpeaker)
	assertPublicProfiles := func(deletedID string) {
		t.Helper()
		profiles, err := ListPublicProfiles(app)
		if err != nil {
			t.Fatal(err)
		}
		foundTeammate, foundSource := false, false
		for _, profile := range profiles {
			if deletedID != "" && (profile.Speaker.ID == source || profile.Speaker.ID == deletedID) {
				t.Fatal("deleted profile in whois")
			}
			if profile.Speaker.ID == source {
				foundSource = true
			}
			if profile.Speaker.ID != teammate {
				continue
			}
			foundTeammate = true
			if len(profile.Talks) != 1 || len(profile.Projects) != 1 {
				t.Fatal("live teammate contributions missing")
			}
			for _, talk := range profile.Talks {
				for _, speaker := range talk.Talk.Speakers {
					if deletedID != "" && speaker.ID == deletedID {
						t.Fatal("deleted co-speaker in whois")
					}
				}
			}
			for _, project := range profile.Projects {
				for _, member := range project.Members {
					if deletedID != "" && member.PersonID == deletedID {
						t.Fatal("deleted team member in whois")
					}
				}
			}
		}
		if !foundTeammate || (deletedID == "" && !foundSource) {
			t.Fatal("public fixture missing")
		}
	}
	exec(`UPDATE proposals SET setup='Shared equipment',comments='Shared discussion',invite_token='old-shared-token' WHERE id=$1`, proposal)
	assertPublicProfiles("")
	version, err := PersonSessionVersion(app, source)
	if err != nil || version <= 0 {
		t.Fatalf("initial version %d: %v", version, err)
	}
	preview, err := PreviewPersonDeletion(app, source)
	if err != nil || preview.Talks != 1 || preview.Projects != 1 {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if err := DeletePerson(app, source, actor); err != nil {
		t.Fatal(err)
	}
	replacement := scalar(`SELECT speaker_id::text FROM speaker_confs WHERE id=$1`, speaker)
	t.Cleanup(func() {
		app.DB.Exec(ctx, `DELETE FROM speaker_confs WHERE id=$1`, speaker)
		app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, replacement)
	})

	if got := scalar(`SELECT rank::text FROM scorecards WHERE judge_person_id=$1`, replacement); got != "1" {
		t.Fatal("judging lost")
	}
	if got := scalar(`SELECT amount_sats::text FROM award_distributions WHERE person_id=$1 AND status='sent' AND notes=''`, replacement); got != "1000" {
		t.Fatal("payout history lost")
	}
	if got := scalar(`SELECT count(*)::text FROM person_merge_events WHERE source_person_id=$1`, mergeSource); got != "0" {
		t.Fatal("merge snapshot retained")
	}
	if got := scalar(`SELECT count(*)::text FROM shop_order_addresses WHERE order_id=$1`, order); got != "0" {
		t.Fatal("address retained")
	}
	if got := scalar(`SELECT total_cents::text FROM shop_orders WHERE id=$1 AND buyer_name=(SELECT name FROM people WHERE id=shop_orders.buyer_person_id)`, order); got != "2500" {
		t.Fatal("financial history lost")
	}
	if got := scalar(`SELECT raw_response::text FROM shipments WHERE order_id=$1 AND tracking_url=''`, order); got != "{}" {
		t.Fatal("provider PII retained")
	}
	if got := scalar(`SELECT role FROM project_members WHERE project_id=$1 AND person_id=$2`, project, teammate); got != "owner" {
		t.Fatal("ownership not transferred")
	}
	if got := scalar(`SELECT role FROM project_members WHERE project_id=$1 AND person_id=$2`, project, replacement); got != "member" {
		t.Fatal("anonymous contributor retains ownership")
	}
	if got := scalar(`SELECT setup||':'||comments FROM proposals WHERE id=$1`, proposal); got != "Shared equipment:Shared discussion" {
		t.Fatal("shared notes erased")
	}
	if got := scalar(`SELECT invite_token FROM proposals WHERE id=$1`, proposal); got == "" || got == "old-shared-token" {
		t.Fatal("shared invitation not rotated")
	}
	if err := AddTickets(app, entry, "stripe"); err != nil {
		t.Fatal(err)
	}
	if got := scalar(`SELECT email::text FROM registrations WHERE ref_id=$1 AND revoked AND revoked_before_account_deletion=false AND checked_in_at IS NOT NULL`, ticketID); got != "deleted-"+replacement+"@invalid.invalid" {
		t.Fatal("webhook restored personal data")
	}
	if got := scalar(`SELECT revoked_before_account_deletion::text FROM registrations WHERE ref_id=$1 AND revoked`, revokedTicketID); got != "true" {
		t.Fatal("previously revoked ticket lost historical status")
	}
	// Even a different writer cannot restore the identity or reactivate access.
	exec(`UPDATE registrations SET email=$2,person_id=$3,revoked=false,revoked_before_account_deletion=NULL WHERE ref_id=$1`, ticketID, email, teammate)
	if got := scalar(`SELECT person_id::text FROM registrations WHERE ref_id=$1 AND revoked AND revoked_before_account_deletion=false AND email<>$2`, ticketID, email); got != replacement {
		t.Fatal("registration anonymity guard bypassed")
	}
	assertPublicProfiles(replacement)
	deletedSpeaker, err := FetchSpeakerByID(app, replacement)
	if err != nil || !deletedSpeaker.IsDeletedAccount {
		t.Fatalf("missing deleted speaker marker: %v", err)
	}
	if replacement == source {
		t.Fatal("source survived")
	}
	if got := scalar(`SELECT count(*)::text FROM people WHERE id=$1`, source); got != "0" {
		t.Fatal("source profile retained")
	}
	if got := scalar(`SELECT name FROM people WHERE id=$1 AND is_deleted_account`, replacement); !regexp.MustCompile(`^anon[0-9]{5}$`).MatchString(got) {
		t.Fatal(got)
	}
	if got := scalar(`SELECT created_by_person_id::text FROM projects WHERE id=$1`, project); got != replacement {
		t.Fatal("creator lost")
	}
	if got := scalar(`SELECT count(*)::text FROM project_members WHERE project_id=$1`, project); got != "2" {
		t.Fatal("team size changed")
	}
	if got := scalar(`SELECT count(*)::text FROM person_password_credentials WHERE person_id=$1 OR person_id=$2`, source, replacement); got != "0" {
		t.Fatal("credentials retained")
	}
	if got := scalar(`SELECT count(*)::text FROM person_emails WHERE email=$1`, email); got != "0" {
		t.Fatal("email retained")
	}
	if got := scalar(`SELECT count(*)::text FROM sessions WHERE position(convert_to($1,'UTF8') in data)>0`, source); got != "0" {
		t.Fatal("session retained")
	}
	for _, id := range []string{source, replacement} {
		v, e := PersonSessionVersion(app, id)
		if e != nil || v != 0 {
			t.Fatalf("deleted account session version %d %v", v, e)
		}
	}
	if _, e := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email) VALUES($1,$2)`, replacement, email); e == nil {
		t.Fatal("placeholder can receive credentials")
	}
	if _, e := app.DB.Exec(ctx, `UPDATE people SET name='Restore' WHERE id=$1`, replacement); e == nil {
		t.Fatal("placeholder mutable")
	}
	if err := DeletePerson(app, teammate, actor); err != nil {
		t.Fatal(err)
	}
	if got := scalar(`SELECT count(*)::text FROM project_members WHERE project_id=$1 AND role='owner'`, project); got != "0" {
		t.Fatal("solo anonymous team retains owner")
	}
	if got := scalar(`SELECT setup||comments||invite_token FROM proposals WHERE id=$1`, proposal); got != "" {
		t.Fatal("sole remaining speaker notes retained")
	}
	if got := scalar(`SELECT count(DISTINCT person_id)::text FROM project_members WHERE project_id=$1`, project); got != "2" {
		t.Fatal("two deletions merged contributors")
	}
}

func TestDatabaseSmokeDeletePersonGuardsAndRollback(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	actor := insertSmokePerson(t, app, "Deletion guard admin")
	source := insertSmokePerson(t, app, "Keep on failure")
	if err := DeletePerson(app, actor, actor); err == nil {
		t.Fatal("self deletion allowed")
	}
	if err := DeletePerson(app, source, actor); err == nil {
		t.Fatal("non-admin deletion allowed")
	}
	if _, err := app.DB.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, actor); err != nil {
		t.Fatal(err)
	}
	// An unexpected restrictive relation must fail atomically, not leave half an account.
	if _, err := app.DB.Exec(ctx, `CREATE TABLE deletion_test_blocker(person_id uuid REFERENCES people(id) ON DELETE RESTRICT)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.DB.Exec(ctx, `DROP TABLE deletion_test_blocker`) })
	app.DB.Exec(ctx, `INSERT INTO deletion_test_blocker VALUES($1)`, source)
	var before, after int
	app.DB.QueryRow(ctx, `SELECT count(*) FROM people WHERE is_deleted_account`).Scan(&before)
	if err := DeletePerson(app, source, actor); err == nil || !strings.Contains(err.Error(), "deletion_test_blocker") {
		t.Fatalf("expected restrictive FK rollback, got %v", err)
	}
	app.DB.QueryRow(ctx, `SELECT count(*) FROM people WHERE is_deleted_account`).Scan(&after)
	if before != after {
		t.Fatal("replacement committed after failure")
	}
	var exists bool
	app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM people WHERE id=$1)`, source).Scan(&exists)
	if !exists {
		t.Fatal("source deleted on failure")
	}
}

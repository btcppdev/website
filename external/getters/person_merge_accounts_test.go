package getters

import (
	"btcpp-web/internal/config"
	"context"
	"github.com/google/uuid"
	"testing"
)

type mergeAccountsFixture struct {
	app                                               *config.AppContext
	canonical, source, org, conf, competition, client string
}

func newMergeAccountsFixture(t *testing.T) mergeAccountsFixture {
	t.Helper()
	app := databaseSmokeContext(t)
	c := context.Background()
	id := func(q string, args ...any) string {
		t.Helper()
		var s string
		if err := app.DB.QueryRow(c, q, args...).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	f := mergeAccountsFixture{app: app}
	f.canonical = id(`INSERT INTO people(name) VALUES('Merge target') RETURNING id::text`)
	f.source = id(`INSERT INTO people(name) VALUES('Merge source') RETURNING id::text`)
	f.org = id(`INSERT INTO organizations(name) VALUES($1) RETURNING id::text`, uuid.NewString())
	f.conf, _ = insertSmokeConference(t, app)
	f.competition = createSmokeCompetition(t, app, CompetitionInput{ConferenceID: f.conf, Title: "Merge test " + uuid.NewString()})
	f.client = id(`INSERT INTO oauth_clients(client_id,name,redirect_uris,allowed_scopes,created_by_person_id) VALUES($1,'Merge app',ARRAY['https://example.test/callback'],ARRAY['profile'],$2) RETURNING id::text`, uuid.NewString(), f.source)
	return f
}
func TestPersonMergeAccountRelationshipsAndUndo(t *testing.T) {
	f := newMergeAccountsFixture(t)
	app := f.app
	c := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := app.DB.Exec(c, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO organization_memberships(organization_id,person_id,invited_by_person_id) VALUES($1,$2,$2)`, f.org, f.source)
	// A duplicate membership has a different primary key and an inviter pointing at the source.
	var org2 string
	if err := app.DB.QueryRow(c, `INSERT INTO organizations(name) VALUES($1) RETURNING id::text`, uuid.NewString()).Scan(&org2); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO organization_memberships(organization_id,person_id,invited_by_person_id) VALUES($1,$2,$3),($1,$3,$3)`, org2, f.canonical, f.source)
	exec(`INSERT INTO oauth_consents(person_id,client_id,scopes) VALUES($1,$3,ARRAY['profile']),($2,$3,ARRAY['profile'])`, f.source, f.canonical, f.client)
	exec(`INSERT INTO oauth_authorization_codes(code_hash,client_id,person_id,redirect_uri,scopes,code_challenge,code_challenge_method,expires_at) VALUES($1,$2,$3,'https://example.test/callback',ARRAY['profile'],'challenge','S256',now()+interval '10 minutes')`, []byte(uuid.NewString()), f.client, f.source)
	exec(`INSERT INTO oauth_access_tokens(selector,token_hash,client_id,person_id,scopes,expires_at,revoked_at) VALUES($1,$2,$3,$4,ARRAY['profile'],now()+interval '1 hour',now())`, uuid.NewString(), []byte("hash"), f.client, f.source)
	exec(`INSERT INTO oauth_refresh_tokens(token_hash,family_id,client_id,person_id,scopes,expires_at) VALUES($1,$2,$3,$4,ARRAY['profile'],now()+interval '1 day')`, []byte(uuid.NewString()), uuid.NewString(), f.client, f.source)
	exec(`INSERT INTO hackathon_sponsor_contact_consents(competition_id,person_id) VALUES($1,$2),($1,$3)`, f.competition, f.source, f.canonical)
	exec(`INSERT INTO hackathon_sponsor_contact_consent_events(competition_id,person_id,all_hackathon_sponsors,entered_award_sponsors,policy_version) VALUES($1,$2,false,false,'test')`, f.competition, f.source)
	exec(`INSERT INTO sponsor_audit_events(organization_id,actor_person_id,action) VALUES($1,$2,'test')`, f.org, f.source)
	exec(`INSERT INTO organization_member_invites(organization_id,email,token_hash,invited_by_person_id,accepted_by_person_id,expires_at) VALUES($1,'merge@example.test',$2,$3,$3,now()+interval '1 day')`, f.org, uuid.NewString(), f.source)
	exec(`INSERT INTO organization_membership_requests(organization_id,person_id,reviewed_by_person_id,status) VALUES($1,$2,$2,'approved')`, f.org, f.source)
	exec(`INSERT INTO organization_applications(submitted_by_person_id,reviewed_by_person_id,applicant_email,name) VALUES($1,$1,'merge@example.test',$2)`, f.source, uuid.NewString())
	exec(`INSERT INTO proposals(conference_id,title,status,direct_invitee_person_id) VALUES($1,'Merge invitation','Invited',$2)`, f.conf, f.source)
	var sponsorship string
	if err := app.DB.QueryRow(c, `INSERT INTO sponsorships(organization_id) VALUES($1) RETURNING id::text`, f.org).Scan(&sponsorship); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO sponsorships_conferences(sponsorship_id,conference_id) VALUES($1,$2)`, sponsorship, f.conf)
	exec(`INSERT INTO sponsorship_entitlements(sponsorship_id,conference_id) VALUES($1,$2)`, sponsorship, f.conf)
	exec(`INSERT INTO sponsor_award_proposals(sponsorship_id,conference_id,competition_id,submitted_by_person_id,reviewed_by_person_id,reviewed_at,title,prize_title) VALUES($1,$2,$3,$4,$4,now(),'Test','Test')`, sponsorship, f.conf, f.competition, f.source)
	exec(`INSERT INTO sponsor_ticket_issuances(sponsorship_id,conference_id,issued_by_person_id,recipient_email,quantity,checkout_id) VALUES($1,$2,$3,'merge@example.test',1,$4)`, sponsorship, f.conf, f.source, uuid.NewString())
	originals := map[string][]map[string]any{}
	for _, spec := range personMergeRelationshipSpecs {
		rows, err := snapshotRows(c, app.DB, spec.Table, spec.PersonColumn, f.source)
		if err != nil {
			t.Fatal(err)
		}
		originals[spec.Table+spec.PersonColumn] = rows
	}
	event, err := MergePeople(app, PersonMergeInput{CanonicalPersonID: f.canonical, SourcePersonID: f.source, MergedByPersonID: f.canonical})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range personMergeRelationshipSpecs {
		rows, err := snapshotRows(c, app.DB, spec.Table, spec.PersonColumn, f.source)
		if err != nil || len(rows) != 0 {
			t.Fatalf("unmoved %s.%s: %v", spec.Table, spec.PersonColumn, err)
		}
	}
	preview, err := GetPersonMergeUndoPreview(app, event)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Changed {
		t.Fatalf("false undo changes: %+v", preview.Groups)
	}
	if err := UndoPersonMerge(app, event, f.canonical, preview); err != nil {
		t.Fatal(err)
	}
	for _, spec := range personMergeRelationshipSpecs {
		for _, before := range originals[spec.Table+spec.PersonColumn] {
			current, found, err := loadRelationshipRow(c, app.DB, spec, before, f.source)
			delete(before, "updated_at")
			delete(current, "updated_at")
			if err != nil || !found || !jsonValuesEqual(before, current) {
				t.Fatalf("bad restore %s.%s: %v\nbefore=%v\nafter=%v", spec.Table, spec.PersonColumn, err, before, current)
			}
		}
	}
}

func TestPersonMergeAccountConflicts(t *testing.T) {
	for _, kind := range []string{"organization_membership", "sponsor_contact_consent", "oauth_consent", "organization_membership_request", "direct_speaker_invitation"} {
		t.Run(kind, func(t *testing.T) {
			f := newMergeAccountsFixture(t)
			c := context.Background()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := f.app.DB.Exec(c, q, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "organization_membership":
				exec(`INSERT INTO organization_memberships(organization_id,person_id,role) VALUES($1,$2,'owner'),($1,$3,'member')`, f.org, f.canonical, f.source)
			case "sponsor_contact_consent":
				exec(`INSERT INTO hackathon_sponsor_contact_consents(competition_id,person_id,all_hackathon_sponsors) VALUES($1,$2,true),($1,$3,false)`, f.competition, f.canonical, f.source)
			case "oauth_consent":
				exec(`INSERT INTO oauth_consents(person_id,client_id,scopes,revoked_at) VALUES($1,$3,ARRAY['profile'],NULL),($2,$3,ARRAY['profile'],now())`, f.canonical, f.source, f.client)
			case "organization_membership_request":
				exec(`INSERT INTO organization_membership_requests(organization_id,person_id) VALUES($1,$2),($1,$3)`, f.org, f.canonical, f.source)
			case "direct_speaker_invitation":
				exec(`INSERT INTO proposals(conference_id,title,status,direct_invitee_person_id) VALUES($1,'Target','Invited',$2),($1,'Source','Invited',$3)`, f.conf, f.canonical, f.source)
			}
			preview, err := PreviewPersonMerge(f.app, f.canonical, f.source)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, conflict := range preview.Conflicts {
				if conflict.Kind == kind {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s conflict", kind)
			}
			if _, err := MergePeople(f.app, PersonMergeInput{CanonicalPersonID: f.canonical, SourcePersonID: f.source, MergedByPersonID: f.canonical}); err == nil {
				t.Fatal("merged unresolved conflict")
			}
			var exists bool
			if err := f.app.DB.QueryRow(c, `SELECT EXISTS(SELECT 1 FROM people WHERE id=$1)`, f.source).Scan(&exists); err != nil || !exists {
				t.Fatal("failed merge removed source")
			}
		})
	}
}

func TestPersonMergeUndoPreservesOAuthRevocation(t *testing.T) {
	f := newMergeAccountsFixture(t)
	c := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := f.app.DB.Exec(c, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	selector := uuid.NewString()
	hash := []byte(uuid.NewString())
	exec(`INSERT INTO oauth_access_tokens(selector,token_hash,client_id,person_id,scopes,expires_at) VALUES($1,$2,$3,$4,ARRAY['profile'],now()+interval '1 hour')`, selector, hash, f.client, f.source)
	exec(`INSERT INTO oauth_refresh_tokens(token_hash,family_id,client_id,person_id,scopes,expires_at) VALUES($1,$2,$3,$4,ARRAY['profile'],now()+interval '1 day')`, hash, uuid.NewString(), f.client, f.source)
	exec(`INSERT INTO oauth_authorization_codes(code_hash,client_id,person_id,redirect_uri,scopes,code_challenge,code_challenge_method,expires_at) VALUES($1,$2,$3,'https://example.test/callback',ARRAY['profile'],'test','S256',now()+interval '10 minutes')`, hash, f.client, f.source)
	exec(`INSERT INTO oauth_consents(person_id,client_id,scopes) VALUES($1,$3,ARRAY['profile']),($2,$3,ARRAY['profile'])`, f.source, f.canonical, f.client)
	event, err := MergePeople(f.app, PersonMergeInput{CanonicalPersonID: f.canonical, SourcePersonID: f.source, MergedByPersonID: f.canonical})
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE oauth_access_tokens SET revoked_at=now() WHERE selector=$1`, selector)
	exec(`UPDATE oauth_refresh_tokens SET revoked_at=now(),consumed_at=now() WHERE token_hash=$1`, hash)
	exec(`UPDATE oauth_authorization_codes SET consumed_at=now() WHERE code_hash=$1`, hash)
	exec(`UPDATE oauth_consents SET revoked_at=now() WHERE client_id=$1`, f.client)
	preview, err := GetPersonMergeUndoPreview(f.app, event)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Changed {
		t.Fatal("revocation absent from undo preview")
	}
	if err := UndoPersonMerge(f.app, event, f.canonical, preview); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`SELECT revoked_at IS NOT NULL FROM oauth_access_tokens WHERE person_id=$1`,
		`SELECT revoked_at IS NOT NULL AND consumed_at IS NOT NULL FROM oauth_refresh_tokens WHERE person_id=$1`,
		`SELECT consumed_at IS NOT NULL FROM oauth_authorization_codes WHERE person_id=$1`,
		`SELECT revoked_at IS NOT NULL FROM oauth_consents WHERE person_id=$1`,
	} {
		var safe bool
		if err := f.app.DB.QueryRow(c, q, f.source).Scan(&safe); err != nil || !safe {
			t.Fatalf("undo revived OAuth credential: %s (%v)", q, err)
		}
	}
}

func TestPersonMergeBallotHistoryAndUndo(t *testing.T) {
	f := newMergeAccountsFixture(t)
	c := context.Background()
	if err := ReplaceCompetitionScheduleSegments(f.app, f.competition, []CompetitionScheduleSegmentInput{{SegmentType: JudgeTypeExpo, Title: "Expo", DefaultDurationMinutes: 60}, {SegmentType: JudgeTypeFinals, Title: "Finals", DefaultDurationMinutes: 60}}); err != nil {
		t.Fatal(err)
	}
	events, err := ListJudgeEvents(f.app, f.competition)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events: %v", events)
	}
	for _, e := range events {
		if _, err = f.app.DB.Exec(c, `INSERT INTO judge_ballot_submissions(judge_event_id,judge_person_id) VALUES($1,$2)`, e.ID, f.source); err != nil {
			t.Fatal(err)
		}
	}
	// Duplicate history keeps the canonical record and archives the source in
	// the existing merge manifest, so undo can restore the exact original rows.
	if _, err = f.app.DB.Exec(c, `INSERT INTO judge_ballot_submissions(judge_event_id,judge_person_id) VALUES($1,$2)`, events[0].ID, f.canonical); err != nil {
		t.Fatal(err)
	}
	event, err := MergePeople(f.app, PersonMergeInput{CanonicalPersonID: f.canonical, SourcePersonID: f.source, MergedByPersonID: f.canonical})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.app.DB.QueryRow(c, `SELECT count(*) FROM judge_ballot_submissions WHERE judge_person_id=$1`, f.canonical).Scan(&count); err != nil || count != 2 {
		t.Fatalf("merged history %d: %v", count, err)
	}
	preview, err := GetPersonMergeUndoPreview(f.app, event)
	if err != nil || preview.Changed {
		t.Fatalf("undo preview: %+v %v", preview, err)
	}
	if err = UndoPersonMerge(f.app, event, f.canonical, preview); err != nil {
		t.Fatal(err)
	}
	if err = f.app.DB.QueryRow(c, `SELECT count(*) FROM judge_ballot_submissions WHERE judge_person_id=$1`, f.source).Scan(&count); err != nil || count != 2 {
		t.Fatalf("restored history %d: %v", count, err)
	}
	if err = f.app.DB.QueryRow(c, `SELECT count(*) FROM judge_ballot_submissions WHERE judge_person_id=$1`, f.canonical).Scan(&count); err != nil || count != 1 {
		t.Fatalf("canonical history %d: %v", count, err)
	}
}

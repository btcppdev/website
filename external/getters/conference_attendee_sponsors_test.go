package getters

import (
	"context"
	"testing"
)

func TestConferenceAttendeeSponsors(t *testing.T) {
	ctx := postgresSmokeContext(t)
	c := context.Background()
	confID, _ := insertSmokeConference(t, ctx)
	otherConfID, _ := insertSmokeConference(t, ctx)
	personID := insertSmokePerson(t, ctx, "sponsor-badge-"+postgresSmokeSuffix())
	outsiderID := insertSmokePerson(t, ctx, "no-sponsor-badge-"+postgresSmokeSuffix())

	// Each fixture exercises one reason a logo should or should not appear.
	fixtures := []struct {
		name, status, membership, light, dark, conference string
		archived, visible                                 bool
	}{
		{"A paid sponsor", "Paid", "active", "https://example.test/light.svg", "", confID, false, true},
		{"B committed sponsor", " Committed ", "active", "", "https://example.test/dark.svg", confID, false, true},
		{"Pending sponsor", "Pending", "active", "https://example.test/pending.svg", "", confID, false, false},
		{"Invoiced sponsor", "Invoiced", "active", "https://example.test/invoiced.svg", "", confID, false, false},
		{"Unconfirmed sponsor", "", "active", "https://example.test/blank.svg", "", confID, false, false},
		{"Past membership", "Paid", "removed", "https://example.test/removed.svg", "", confID, false, false},
		{"Different conference", "Paid", "active", "https://example.test/other.svg", "", otherConfID, false, false},
		{"Archived sponsor", "Paid", "active", "https://example.test/archived.svg", "", confID, true, false},
		{"No logo", "Paid", "active", " ", "", confID, false, false},
	}
	var expected []string
	for _, fixture := range fixtures {
		var orgID, sponsorshipID string
		if err := ctx.DB.QueryRow(c, `INSERT INTO organizations (name, logo_light_url, logo_dark_url) VALUES ($1,$2,$3) RETURNING id::text`, fixture.name, fixture.light, fixture.dark).Scan(&orgID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			ctx.DB.Exec(c, `DELETE FROM sponsorships WHERE organization_id=$1::uuid`, orgID)
			ctx.DB.Exec(c, `DELETE FROM organizations WHERE id=$1::uuid`, orgID)
		})
		if _, err := ctx.DB.Exec(c, `INSERT INTO organization_memberships (organization_id,person_id,role,status) VALUES ($1::uuid,$2::uuid,'member',$3)`, orgID, personID, fixture.membership); err != nil {
			t.Fatal(err)
		}
		if err := ctx.DB.QueryRow(c, `INSERT INTO sponsorships (organization_id,name,level,status,archived_at) VALUES ($1::uuid,$2,'Gold',$3,CASE WHEN $4 THEN now() END) RETURNING id::text`, orgID, fixture.name, fixture.status, fixture.archived).Scan(&sponsorshipID); err != nil {
			t.Fatal(err)
		}
		if _, err := ctx.DB.Exec(c, `INSERT INTO sponsorships_conferences (sponsorship_id,conference_id) VALUES ($1::uuid,$2::uuid)`, sponsorshipID, fixture.conference); err != nil {
			t.Fatal(err)
		}
		if fixture.visible {
			expected = append(expected, orgID)
		}
	}
	// A second sponsorship for one organization must not duplicate its logo.
	if _, err := ctx.DB.Exec(c, `WITH added AS (INSERT INTO sponsorships (organization_id,name,level,status) VALUES ($1::uuid,'Second sponsorship','Workshop','Paid') RETURNING id) INSERT INTO sponsorships_conferences (sponsorship_id,conference_id) SELECT id,$2::uuid FROM added`, expected[0], confID); err != nil {
		t.Fatal(err)
	}
	got, err := ListConferenceAttendeeSponsors(ctx, confID, []string{personID, outsiderID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[personID]) != len(expected) {
		t.Fatalf("unexpected sponsor affiliations: %+v", got)
	}
	for i, orgID := range expected {
		if got[personID][i].Ref != orgID {
			t.Fatalf("sponsor %d = %s, want %s", i, got[personID][i].Ref, orgID)
		}
	}
	if got[personID][1].LogoDark != "https://example.test/dark.svg" {
		t.Fatal("dark logo fallback was not loaded")
	}
	if empty, err := ListConferenceAttendeeSponsors(ctx, confID, nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty attendees: %v, %v", empty, err)
	}
	if empty, err := ListConferenceAttendeeSponsors(ctx, "", []string{personID}); err != nil || len(empty) != 0 {
		t.Fatalf("missing conference: %v, %v", empty, err)
	}
}

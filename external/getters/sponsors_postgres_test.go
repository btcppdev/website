package getters

import (
	"context"
	"testing"
)

func TestDatabaseSmokeGetSponsorshipLoadsOnlyItsGraph(t *testing.T) {
	ctx := postgresSmokeContext(t)
	suffix := postgresSmokeSuffix()
	confID, _ := insertSmokeConference(t, ctx)

	var orgID string
	if err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO organizations (name)
		VALUES ($1)
		RETURNING id::text
	`, "Scoped Sponsor "+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1::uuid`, orgID)
	})

	var sponsorshipID string
	if err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO sponsorships (organization_id, name, level, status)
		VALUES ($1::uuid, $2, 'Gold', 'Paid')
		RETURNING id::text
	`, orgID, "Scoped Sponsorship "+suffix).Scan(&sponsorshipID); err != nil {
		t.Fatalf("insert sponsorship: %v", err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO sponsorships_conferences (sponsorship_id, conference_id)
		VALUES ($1::uuid, $2::uuid)
	`, sponsorshipID, confID); err != nil {
		t.Fatalf("insert sponsorship conference link: %v", err)
	}

	loaded, err := GetSponsorship(ctx, sponsorshipID)
	if err != nil {
		t.Fatalf("GetSponsorship: %v", err)
	}
	if loaded == nil || loaded.Ref != sponsorshipID || loaded.Org == nil || loaded.Org.Ref != orgID || len(loaded.Confs) != 1 || loaded.Confs[0].Ref != confID {
		t.Fatalf("GetSponsorship returned incomplete scoped graph: %+v", loaded)
	}
}

package getters

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidBadgeCredentialURL(t *testing.T) {
	for _, value := range []string{"https://btcpp.dev/whois/mara", "http://localhost:8080/whois/mara", "http://127.0.0.1:8080/art.png", "http://[::1]:8080/art.png"} {
		if !validBadgeCredentialURL(value) {
			t.Fatalf("rejected credential URL %q", value)
		}
	}
	for _, value := range []string{"http://example.com/art.png", "https://user@example.com/art.png", "javascript:alert(1)", "https://example.com/art.png#fragment"} {
		if validBadgeCredentialURL(value) {
			t.Fatalf("accepted credential URL %q", value)
		}
	}
}

func TestOrganizationBadgeGrantLifecycle(t *testing.T) {
	ctx := postgresSmokeContext(t)
	suffix := postgresSmokeSuffix()
	managerID := insertSmokePerson(t, ctx, "badge-grant-manager-"+suffix)
	recipientID := insertSmokePerson(t, ctx, "badge-grant-recipient-"+suffix)
	var organizationID string
	if err := ctx.DB.QueryRow(context.Background(), `INSERT INTO organizations (name) VALUES ($1) RETURNING id::text`, "Badge Grant "+suffix).Scan(&organizationID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM organizations WHERE id=$1::uuid`, organizationID)
	})
	if _, err := ctx.DB.Exec(context.Background(), `INSERT INTO organization_memberships (organization_id, person_id, role, status) VALUES ($1::uuid,$2::uuid,'manager','active')`, organizationID, managerID); err != nil {
		t.Fatal(err)
	}
	issuer := strings.Repeat("a", 64)
	input := OrganizationBadgeGrantInput{OrganizationID: organizationID, RecipientPersonID: recipientID, CreatedByPersonID: managerID, IssuerPubkey: issuer, BadgeIdentifier: "builder", BadgeName: "Builder", BadgeDescription: "Builds things.", BadgeImageURL: "https://cdn.example/builder.png", SubjectProfileURL: "https://btcpp.dev/whois/builder"}
	grant, err := CreateOrganizationBadgeGrant(ctx, input)
	if err != nil || grant.State != BadgeGrantStateGranted || grant.RecipientPubkey != "" {
		t.Fatalf("initial grant=%+v err=%v", grant, err)
	}
	if _, err := CreateOrganizationBadgeGrant(ctx, input); !errors.Is(err, ErrBadgeGrantConflict) {
		t.Fatalf("duplicate grant error=%v", err)
	}
	recipientPubkey := strings.Repeat("b", 64)
	if _, err := ctx.DB.Exec(context.Background(), `INSERT INTO person_nostr_credentials (person_id, pubkey_hex, verified_at) VALUES ($1::uuid,$2,now())`, recipientID, recipientPubkey); err != nil {
		t.Fatal(err)
	}
	promoted, err := PromotePendingBadgeGrants(ctx, recipientID)
	if err != nil || len(promoted) != 1 || promoted[0].ID != grant.ID || promoted[0].State != BadgeGrantStateReady {
		t.Fatalf("promoted grants=%+v err=%v", promoted, err)
	}
	promotedAgain, err := PromotePendingBadgeGrants(ctx, recipientID)
	if err != nil || len(promotedAgain) != 0 {
		t.Fatalf("duplicate promotion=%+v err=%v", promotedAgain, err)
	}
	personGrants, err := ListPersonBadgeGrants(ctx, recipientID)
	if err != nil || len(personGrants) != 1 || personGrants[0].State != BadgeGrantStateReady || personGrants[0].RecipientPubkey != recipientPubkey || personGrants[0].ReadyAt == nil {
		t.Fatalf("ready grants=%+v err=%v", personGrants, err)
	}
	awardID := strings.Repeat("c", 64)
	if err := MarkBadgeGrantIssued(ctx, grant.ID, awardID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	acceptanceID := strings.Repeat("d", 64)
	if err := MarkBadgeGrantAccepted(ctx, grant.ID, acceptanceID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	finished, err := GetBadgeGrant(ctx, grant.ID)
	if err != nil || finished.State != BadgeGrantStateAccepted || finished.AwardEventID != awardID || finished.AcceptanceEventID != acceptanceID {
		t.Fatalf("finished grant=%+v err=%v", finished, err)
	}
	revocationID := strings.Repeat("e", 64)
	if err := MarkBadgeGrantRevoked(ctx, grant.ID, revocationID, "Issued in error", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	finished, err = GetBadgeGrant(ctx, grant.ID)
	if err != nil || finished.State != BadgeGrantStateRevoked || finished.RevocationEventID != revocationID || finished.RevocationReason != "Issued in error" || finished.RevokedAt == nil {
		t.Fatalf("revoked grant=%+v err=%v", finished, err)
	}

	input.BadgeIdentifier, input.BadgeName = "mentor", "Mentor"
	cancelable, err := CreateOrganizationBadgeGrant(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := CancelOrganizationBadgeGrant(ctx, organizationID, cancelable.ID, managerID); err != nil {
		t.Fatal(err)
	}
	input.BadgeIdentifier, input.BadgeName = "local-preview", "Local Preview"
	input.BadgeImageURL, input.SubjectProfileURL = "http://127.0.0.1:35173/artwork/badge.png", "http://localhost:8080/whois/local-preview"
	localGrant, err := CreateOrganizationBadgeGrant(ctx, input)
	if err != nil || localGrant.SubjectProfileURL != input.SubjectProfileURL {
		t.Fatalf("local fixture grant=%+v err=%v", localGrant, err)
	}
	visible, err := ListPersonBadgeGrants(ctx, recipientID)
	if err != nil || len(visible) != 2 {
		t.Fatalf("canceled recipient grants=%+v err=%v", visible, err)
	}
}

package getters

import (
	"context"
	"testing"
	"time"
)

func TestOrganizationInviteExpiryAndRenewal(t *testing.T) {
	ctx := postgresSmokeContext(t)
	suffix := postgresSmokeSuffix()
	personID := insertSmokePerson(t, ctx, "org-invite-expiry-"+suffix)
	email := smokePersonEmail(t, ctx, personID)
	var orgID string
	if err := ctx.DB.QueryRow(context.Background(), `INSERT INTO organizations(name) VALUES($1) RETURNING id::text`, "Invite expiry "+suffix).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = ctx.DB.Exec(context.Background(), `DELETE FROM organizations WHERE id=$1::uuid`, orgID) })
	token, invite, err := CreateOrganizationMemberInvite(ctx, orgID, email, OrganizationRoleManager, "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	assertLifetime := func(expires time.Time) {
		t.Helper()
		remaining := time.Until(expires)
		if remaining < 14*24*time.Hour-time.Minute || remaining > 14*24*time.Hour {
			t.Fatalf("lifetime %s, want 14 days", remaining)
		}
	}
	assertLifetime(invite.ExpiresAt)
	if _, err := ctx.DB.Exec(context.Background(), `UPDATE organization_member_invites SET expires_at=now()-interval '1 hour' WHERE id=$1::uuid`, invite.ID); err != nil {
		t.Fatal(err)
	}
	listed, err := ListPendingOrganizationMemberInvites(ctx, orgID)
	if err != nil || len(listed) != 1 || listed[0].ID != invite.ID || !listed[0].Expired() {
		t.Fatalf("expired invite unavailable to managers: %+v, %v", listed, err)
	}
	personal, err := ListPendingOrganizationMemberInvitesForPerson(ctx, personID)
	if err != nil || len(personal) != 0 {
		t.Fatalf("expired invite available to recipient: %+v, %v", personal, err)
	}
	if _, err := AcceptOrganizationMemberInvite(ctx, token, personID); err == nil {
		t.Fatal("expired link accepted")
	}
	replacementToken, replacement, err := ReplaceOrganizationMemberInvite(ctx, orgID, invite.ID, "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	assertLifetime(replacement.ExpiresAt)
	if replacementToken == token || replacement.ID == invite.ID {
		t.Fatal("replacement reused invitation")
	}
	listed, err = ListPendingOrganizationMemberInvites(ctx, orgID)
	if err != nil || len(listed) != 1 || listed[0].ID != replacement.ID {
		t.Fatal("revoked invitation still listed")
	}
	if _, err := AcceptOrganizationMemberInvite(ctx, token, personID); err == nil {
		t.Fatal("old link accepted")
	}
	if _, err := AcceptOrganizationMemberInvite(ctx, replacementToken, personID); err != nil {
		t.Fatal(err)
	}
	if _, err := AcceptOrganizationMemberInvite(ctx, replacementToken, personID); err == nil {
		t.Fatal("replacement link reused")
	}
	listed, err = ListPendingOrganizationMemberInvites(ctx, orgID)
	if err != nil || len(listed) != 0 {
		t.Fatal("accepted invitation still listed")
	}
}

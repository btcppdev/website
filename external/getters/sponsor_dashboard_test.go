package getters

import (
	"context"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestSponsorDashboardMembershipEntitlementsAndConsent(t *testing.T) {
	ctx := postgresSmokeContext(t)
	suffix := postgresSmokeSuffix()
	personID := insertSmokePerson(t, ctx, "sponsor-member-"+suffix)
	confID, _ := insertSmokeConference(t, ctx)

	var orgID string
	if err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO organizations (name, tagline, notes)
		VALUES ($1, 'Original tagline', 'private contract note')
		RETURNING id::text
	`, "Sponsor Dashboard "+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1::uuid`, orgID)
	})

	var sponsorshipID string
	if err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO sponsorships (organization_id, name, level, status)
		VALUES ($1::uuid, 'Sponsor fixture', 'Gold', 'Paid')
		RETURNING id::text
	`, orgID).Scan(&sponsorshipID); err != nil {
		t.Fatalf("insert sponsorship: %v", err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO sponsorships_conferences (sponsorship_id, conference_id)
		VALUES ($1::uuid, $2::uuid);
		INSERT INTO organization_memberships (organization_id, person_id, role, status)
		VALUES ($3::uuid, $4::uuid, 'owner', 'active');
		INSERT INTO sponsorship_entitlements (
			sponsorship_id, conference_id, ticket_allocation,
			sponsor_award_limit, participant_contact_access
		) VALUES ($1::uuid, $2::uuid, 20, 2, true)
	`, sponsorshipID, confID, orgID, personID); err != nil {
		t.Fatalf("insert sponsor dashboard fixtures: %v", err)
	}

	memberships, err := ListOrganizationMembershipsForPerson(ctx, personID)
	if err != nil {
		t.Fatalf("ListOrganizationMembershipsForPerson: %v", err)
	}
	if len(memberships) != 1 || memberships[0].Role != OrganizationRoleOwner || memberships[0].Organization.Name != "Sponsor Dashboard "+suffix {
		t.Fatalf("membership mismatch: %+v", memberships)
	}
	if memberships[0].Organization.Notes != "" {
		t.Fatalf("sponsor-facing membership loaded private organization notes: %q", memberships[0].Organization.Notes)
	}
	hasMembership, err := HasActiveOrganizationMembership(ctx, personID)
	if err != nil || !hasMembership {
		t.Fatalf("HasActiveOrganizationMembership: has=%t err=%v", hasMembership, err)
	}

	events, err := ListSponsorDashboardEvents(ctx, orgID)
	if err != nil {
		t.Fatalf("ListSponsorDashboardEvents: %v", err)
	}
	if len(events) != 1 || events[0].Entitlement.TicketAllocation != 20 || events[0].Entitlement.SponsorAwardLimit != 2 || !events[0].Entitlement.ParticipantContactAccess {
		t.Fatalf("event entitlement mismatch: %+v", events)
	}
	if events[0].Sponsorship.Notes != "" {
		t.Fatalf("sponsor dashboard loaded private sponsorship notes: %q", events[0].Sponsorship.Notes)
	}
	if _, err := ctx.DB.Exec(context.Background(), `UPDATE sponsorships SET archived_at = now() WHERE id = $1::uuid`, sponsorshipID); err != nil {
		t.Fatalf("archive sponsorship: %v", err)
	}
	archivedEvents, err := ListSponsorDashboardEvents(ctx, orgID)
	if err != nil || len(archivedEvents) != 0 {
		t.Fatalf("archived sponsorship remained on dashboard: events=%+v err=%v", archivedEvents, err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `UPDATE sponsorships SET archived_at = NULL WHERE id = $1::uuid`, sponsorshipID); err != nil {
		t.Fatalf("restore sponsorship: %v", err)
	}

	consent := &types.HackathonSponsorContactConsent{
		CompetitionID:        createSmokeCompetition(t, ctx, CompetitionInput{ConferenceID: confID, Title: "Sponsor consent " + suffix}),
		PersonID:             personID,
		EnteredAwardSponsors: true,
	}
	if err := SetHackathonSponsorContactConsent(ctx, consent); err != nil {
		t.Fatalf("SetHackathonSponsorContactConsent: %v", err)
	}
	gotConsent, err := GetHackathonSponsorContactConsent(ctx, consent.CompetitionID, personID)
	if err != nil {
		t.Fatalf("GetHackathonSponsorContactConsent: %v", err)
	}
	if gotConsent.AllHackathonSponsors || !gotConsent.EnteredAwardSponsors {
		t.Fatalf("consent mismatch: %+v", gotConsent)
	}
	var consentEvents int
	if err := ctx.DB.QueryRow(context.Background(), `
		SELECT count(*) FROM hackathon_sponsor_contact_consent_events
		WHERE competition_id = $1::uuid AND person_id = $2::uuid
		  AND policy_version = $3
	`, consent.CompetitionID, personID, SponsorContactConsentPolicyVersion).Scan(&consentEvents); err != nil {
		t.Fatalf("load sponsor contact consent history: %v", err)
	}
	if consentEvents != 1 {
		t.Fatalf("sponsor contact consent events = %d, want 1", consentEvents)
	}

	org, err := GetOrg(ctx, orgID)
	if err != nil {
		t.Fatalf("GetOrg: %v", err)
	}
	org.Tagline = "Sponsor-controlled public tagline"
	org.Notes = "attempted overwrite"
	if err := UpdateOrganizationPublicDetails(ctx, org); err != nil {
		t.Fatalf("UpdateOrganizationPublicDetails: %v", err)
	}
	var tagline, notes string
	if err := ctx.DB.QueryRow(context.Background(), `SELECT tagline, notes FROM organizations WHERE id = $1::uuid`, orgID).Scan(&tagline, &notes); err != nil {
		t.Fatalf("load updated org: %v", err)
	}
	if tagline != "Sponsor-controlled public tagline" || notes != "private contract note" {
		t.Fatalf("public/private update mismatch: tagline=%q notes=%q", tagline, notes)
	}

	invitedPersonID := insertSmokePerson(t, ctx, "sponsor-invited-"+suffix)
	invitedEmail := smokePersonEmail(t, ctx, invitedPersonID)
	token, invite, err := CreateOrganizationMemberInvite(ctx, orgID, invitedEmail, OrganizationRoleMember, personID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateOrganizationMemberInvite: %v", err)
	}
	if token == "" || invite.Email != strings.ToLower(invitedEmail) {
		t.Fatalf("organization invite mismatch: token=%q invite=%+v", token, invite)
	}
	loadedInvite, err := GetOrganizationMemberInviteByToken(ctx, token)
	if err != nil || loadedInvite == nil || loadedInvite.OrganizationName != "Sponsor Dashboard "+suffix {
		t.Fatalf("GetOrganizationMemberInviteByToken: invite=%+v err=%v", loadedInvite, err)
	}
	replacementToken, _, err := CreateOrganizationMemberInvite(ctx, orgID, invitedEmail, OrganizationRoleManager, personID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("replace organization member invite: %v", err)
	}
	if _, err := AcceptOrganizationMemberInvite(ctx, token, invitedPersonID); err == nil {
		t.Fatal("superseded organization invitation remained usable")
	}
	wrongPersonID := insertSmokePerson(t, ctx, "sponsor-wrong-invite-"+suffix)
	if _, err := AcceptOrganizationMemberInvite(ctx, replacementToken, wrongPersonID); err == nil {
		t.Fatal("accepted an organization invitation from an account without the invited verified email")
	}
	accepted, err := AcceptOrganizationMemberInvite(ctx, replacementToken, invitedPersonID)
	if err != nil || accepted.OrganizationID != orgID {
		t.Fatalf("AcceptOrganizationMemberInvite: invite=%+v err=%v", accepted, err)
	}
	if _, err := AcceptOrganizationMemberInvite(ctx, replacementToken, invitedPersonID); err == nil {
		t.Fatal("AcceptOrganizationMemberInvite reused a one-time invitation")
	}
	acceptedMembership, err := GetOrganizationMembership(ctx, invitedPersonID, orgID)
	if err != nil || acceptedMembership == nil || acceptedMembership.Role != OrganizationRoleManager {
		t.Fatalf("accepted organization membership: membership=%+v err=%v", acceptedMembership, err)
	}
	if _, _, err := CreateOrganizationMemberInvite(ctx, orgID, invitedEmail, OrganizationRoleMember, personID, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("created an invitation for an existing active organization member")
	}
	ownerEmail := smokePersonEmail(t, ctx, personID)
	if _, _, err := CreateOrganizationMemberInvite(ctx, orgID, ownerEmail, OrganizationRoleMember, personID, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("created an invitation capable of replacing an existing owner role")
	}
}

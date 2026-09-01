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
	if _, err := ctx.DB.Exec(context.Background(), `
		UPDATE conferences SET start_date = now() + interval '30 days', end_date = now() + interval '32 days'
		WHERE id = $1::uuid
	`, confID); err != nil {
		t.Fatalf("move sponsor fixture conference into future: %v", err)
	}

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
		VALUES ($1::uuid, $2::uuid)
	`, sponsorshipID, confID); err != nil {
		t.Fatalf("insert sponsor conference link: %v", err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO organization_memberships (organization_id, person_id, role, status)
		VALUES ($1::uuid, $2::uuid, 'owner', 'active')
	`, orgID, personID); err != nil {
		t.Fatalf("insert sponsor membership: %v", err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO sponsorship_entitlements (
			sponsorship_id, conference_id, ticket_allocation,
			sponsor_award_limit, participant_contact_access
		) VALUES ($1::uuid, $2::uuid, 20, 2, true)
	`, sponsorshipID, confID); err != nil {
		t.Fatalf("insert sponsor entitlement: %v", err)
	}
	competitionID := createSmokeCompetition(t, ctx, CompetitionInput{ConferenceID: confID, Title: "Sponsor benefits " + suffix})

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
	if len(events) != 1 || events[0].Entitlement.TicketAllocation != 20 || events[0].Entitlement.SponsorAwardLimit != 2 || !events[0].Entitlement.ParticipantContactAccess || events[0].Competition == nil || events[0].Competition.ID != competitionID {
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

	issuance, err := IssueSponsorTickets(ctx, orgID, sponsorshipID, confID, personID, "tickets-"+suffix+"@example.test", 2)
	if err != nil {
		t.Fatalf("IssueSponsorTickets: %v", err)
	}
	if issuance.Quantity != 2 || issuance.CheckoutID == "" {
		t.Fatalf("sponsor ticket issuance mismatch: %+v", issuance)
	}
	if _, err := IssueSponsorTickets(ctx, orgID, sponsorshipID, confID, personID, "too-many-"+suffix+"@example.test", 19); err == nil {
		t.Fatal("sponsor ticket issuance exceeded the event allocation")
	}
	issuances, err := ListSponsorTicketIssuances(ctx, orgID)
	if err != nil || len(issuances) != 1 || issuances[0].RecipientEmail != issuance.RecipientEmail {
		t.Fatalf("ListSponsorTicketIssuances: issuances=%+v err=%v", issuances, err)
	}
	var registrations int
	if err := ctx.DB.QueryRow(context.Background(), `SELECT count(*) FROM registrations WHERE checkout_id = $1`, issuance.CheckoutID).Scan(&registrations); err != nil || registrations != 2 {
		t.Fatalf("sponsor registrations=%d err=%v, want 2", registrations, err)
	}

	proposalInput := SponsorAwardProposalInput{
		SponsorshipID: sponsorshipID, ConferenceID: confID,
		CompetitionID: competitionID, OrganizationID: orgID,
		SubmittedByPersonID: personID, Title: "Sponsor challenge " + suffix,
		Description: "Build something useful", JudgingInstructions: "Prefer working demos",
		MaxAwardees: 1, OptInRequired: true, PrizeType: PrizeTypeSats,
		PrizeTitle: "Sponsor sats", PrizeValueText: "1000000",
	}
	proposal, err := CreateSponsorAwardProposal(ctx, proposalInput)
	if err != nil {
		t.Fatalf("CreateSponsorAwardProposal: %v", err)
	}
	proposals, err := ListSponsorAwardProposalsForCompetition(ctx, competitionID)
	if err != nil || len(proposals) != 1 || proposals[0].Status != "pending" {
		t.Fatalf("ListSponsorAwardProposalsForCompetition: proposals=%+v err=%v", proposals, err)
	}
	approved, err := ReviewSponsorAwardProposal(ctx, proposal.ID, competitionID, personID, "approved", "Looks good")
	if err != nil || approved.AwardID == "" || approved.Status != "approved" {
		t.Fatalf("ReviewSponsorAwardProposal: proposal=%+v err=%v", approved, err)
	}
	var awardStatus, prizeValue string
	if err := ctx.DB.QueryRow(context.Background(), `
		SELECT awards.status, prizes.value_text
		FROM awards JOIN prizes ON prizes.award_id = awards.id
		WHERE awards.id = $1::uuid AND awards.sponsored_by_org_id = $2::uuid
	`, approved.AwardID, orgID).Scan(&awardStatus, &prizeValue); err != nil || awardStatus != "available" || prizeValue != "1000000" {
		t.Fatalf("approved sponsor award: status=%q value=%q err=%v", awardStatus, prizeValue, err)
	}
	proposalInput.Title = "Second sponsor challenge " + suffix
	secondProposal, err := CreateSponsorAwardProposal(ctx, proposalInput)
	if err != nil || secondProposal.Status != "pending" {
		t.Fatalf("second CreateSponsorAwardProposal: proposal=%+v err=%v", secondProposal, err)
	}
	proposalInput.Title = "Over limit " + suffix
	if _, err := CreateSponsorAwardProposal(ctx, proposalInput); err == nil {
		t.Fatal("created a sponsor award proposal beyond the entitlement limit")
	}
	rejected, err := ReviewSponsorAwardProposal(ctx, secondProposal.ID, competitionID, personID, "rejected", "Needs a clearer rubric")
	if err != nil || rejected.Status != "rejected" || rejected.AwardID != "" {
		t.Fatalf("reject sponsor proposal: proposal=%+v err=%v", rejected, err)
	}
	proposalInput.Title = "Replacement proposal " + suffix
	if _, err := CreateSponsorAwardProposal(ctx, proposalInput); err != nil {
		t.Fatalf("rejected sponsor proposal did not release its allowance: %v", err)
	}

	consent := &types.HackathonSponsorContactConsent{
		CompetitionID:        competitionID,
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

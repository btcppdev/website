package getters

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestSponsorDashboardMembershipEntitlementsAndConsent(t *testing.T) {
	ctx := postgresSmokeContext(t)
	suffix := postgresSmokeSuffix()
	personID := insertSmokePerson(t, ctx, "sponsor-member-"+suffix)
	managerPersonID := insertSmokePerson(t, ctx, "sponsor-manager-"+suffix)
	confID, confTag := insertSmokeConference(t, ctx)
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
	if err := UpdateSponsorship(ctx, confID, &types.Sponsorship{
		Ref: sponsorshipID, Org: &types.Org{Ref: orgID, Name: "Sponsor Dashboard " + suffix},
		Level: "Gold", Status: "Paid", Notes: "private contract note",
	}, &types.SponsorshipEntitlement{
		TicketAllocation: 24, SponsorAwardLimit: 3,
		CanEditOrganization: true,
	}, SponsorshipWriteOptions{ManagerPersonID: managerPersonID, AssignedByPersonID: personID}); err != nil {
		t.Fatalf("UpdateSponsorship ticket allocation: %v", err)
	}
	events, err = ListSponsorDashboardEvents(ctx, orgID)
	if err != nil || len(events) != 1 || events[0].Entitlement.TicketAllocation != 24 || events[0].Entitlement.SponsorAwardLimit != 3 || !events[0].Entitlement.CanEditOrganization {
		t.Fatalf("updated ticket allocation: events=%+v err=%v", events, err)
	}
	managerMembership, err := GetOrganizationMembership(ctx, managerPersonID, orgID)
	if err != nil || managerMembership == nil || managerMembership.Role != OrganizationRoleManager || managerMembership.Status != "active" {
		t.Fatalf("assigned organization manager: membership=%+v err=%v", managerMembership, err)
	}
	talkProposalID, err := CreateProposal(ctx, ProposalInput{
		ScheduleForTag: confTag, Title: "Team member talk " + suffix,
		TalkType: "Talk", Status: "Applied", DesiredDuration: 30, AvailDuration: 30,
	})
	if err != nil {
		t.Fatalf("create organization speaker proposal: %v", err)
	}
	for _, speakerID := range []string{personID, managerPersonID} {
		if _, err := UpsertSpeakerConf(ctx, SpeakerConfInput{
			SpeakerID: speakerID, ConfTag: confTag, ProposalID: talkProposalID,
		}); err != nil {
			t.Fatalf("attach organization speaker %s: %v", speakerID, err)
		}
	}
	speakerApplications, err := ListSponsorSpeakerApplications(ctx, orgID)
	if err != nil || len(speakerApplications) != 1 {
		t.Fatalf("ListSponsorSpeakerApplications: applications=%+v err=%v", speakerApplications, err)
	}
	if application := speakerApplications[0]; application.ProposalID != talkProposalID || application.ConferenceID != confID || application.Status != "Applied" || len(application.MemberNames) != 2 {
		t.Fatalf("organization speaker application mismatch: %+v", application)
	}
	registered := &types.Sponsorship{
		Org:   &types.Org{Ref: orgID, Name: "Sponsor Dashboard " + suffix},
		Confs: []*types.Conf{{Ref: confID}}, Level: "Silver", Status: "Pending",
	}
	if err := RegisterSponsorship(ctx, registered, &types.SponsorshipEntitlement{
		TicketAllocation: 13, SponsorAwardLimit: 4,
		CanEditOrganization: true,
	}, SponsorshipWriteOptions{}); err != nil {
		t.Fatalf("RegisterSponsorship ticket allocation: %v", err)
	}
	var registeredAllocation, registeredAwardLimit int
	var registeredCanEditOrganization bool
	if err := ctx.DB.QueryRow(context.Background(), `
		SELECT ticket_allocation, sponsor_award_limit,
			can_edit_organization
		FROM sponsorship_entitlements
		WHERE sponsorship_id = $1::uuid AND conference_id = $2::uuid
	`, registered.Ref, confID).Scan(
		&registeredAllocation, &registeredAwardLimit, &registeredCanEditOrganization,
	); err != nil || registeredAllocation != 13 || registeredAwardLimit != 4 || !registeredCanEditOrganization {
		t.Fatalf("registered entitlement = tickets %d, prizes %d, edit org %t, err=%v", registeredAllocation, registeredAwardLimit, registeredCanEditOrganization, err)
	}
	if err := DeleteSponsorship(ctx, confID, registered.Ref); err != nil {
		t.Fatalf("DeleteSponsorship registered fixture: %v", err)
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
	if _, err := IssueSponsorTickets(ctx, orgID, sponsorshipID, confID, personID, "too-many-"+suffix+"@example.test", 23); err == nil {
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
	if err != nil || len(proposals) != 1 || proposals[0].Status != "pending" || proposals[0].EditableUntil == nil || proposals[0].CompetitionTitle == "" {
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
	proposalInput.Title = "Updated sponsor challenge " + suffix
	proposalInput.Description = "Updated challenge description"
	proposalInput.PrizeTitle = "Updated sponsor sats"
	proposalInput.PrizeValueText = "2000000"
	proposalInput.MaxAwardees = 2
	updated, err := UpdateSponsorAwardProposal(ctx, proposal.ID, orgID, proposalInput)
	if err != nil || updated.Title != proposalInput.Title || updated.AwardID != approved.AwardID {
		t.Fatalf("UpdateSponsorAwardProposal: proposal=%+v err=%v", updated, err)
	}
	var awardTitle, awardDescription, prizeTitle string
	var maxAwardees int
	if err := ctx.DB.QueryRow(context.Background(), `
		SELECT awards.title, awards.description, awards.max_awardees,
			prizes.title, prizes.value_text
		FROM awards JOIN prizes ON prizes.award_id = awards.id
		WHERE awards.id = $1::uuid
	`, approved.AwardID).Scan(&awardTitle, &awardDescription, &maxAwardees, &prizeTitle, &prizeValue); err != nil ||
		awardTitle != proposalInput.Title || awardDescription != proposalInput.Description ||
		maxAwardees != 2 || prizeTitle != proposalInput.PrizeTitle || prizeValue != "2000000" {
		t.Fatalf("updated approved challenge: award=%q description=%q max=%d prize=%q value=%q err=%v", awardTitle, awardDescription, maxAwardees, prizeTitle, prizeValue, err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		UPDATE competitions SET hacking_starts_at = now() - interval '1 minute'
		WHERE id = $1::uuid
	`, competitionID); err != nil {
		t.Fatalf("start sponsor fixture hackathon: %v", err)
	}
	if _, err := UpdateSponsorAwardProposal(ctx, proposal.ID, orgID, proposalInput); err == nil || !strings.Contains(err.Error(), "hackathon has started") {
		t.Fatalf("updated sponsor challenge after hacking started: %v", err)
	}
	if _, err := CreateSponsorAwardProposal(ctx, proposalInput); err == nil {
		t.Fatal("created sponsor challenge after hacking started")
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		UPDATE competitions SET hacking_starts_at = now() + interval '30 days'
		WHERE id = $1::uuid
	`, competitionID); err != nil {
		t.Fatalf("restore sponsor fixture hackathon start: %v", err)
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
	projectID := createSmokeProject(t, ctx, ProjectInput{
		CompetitionID: competitionID, CreatedByPersonID: personID,
		Slug: "sponsor-entry-" + suffix, Title: "Sponsor entry " + suffix,
		ShortDescription: "A project entered in the sponsor challenge.",
		GitHubURL:        "https://github.com/example/sponsor-entry",
	})
	if err := SetProjectAwardOptIns(ctx, projectID, []string{approved.AwardID}); err != nil {
		t.Fatalf("SetProjectAwardOptIns sponsor entry: %v", err)
	}
	nonConsentingPersonID := insertSmokePerson(t, ctx, "sponsor-entry-private-"+suffix)
	if err := AddProjectMember(ctx, projectID, nonConsentingPersonID, ProjectMemberRoleMember); err != nil {
		t.Fatalf("AddProjectMember non-consenting sponsor entry member: %v", err)
	}
	entries, err := ListSponsorPrizeEntries(ctx, orgID, true)
	if err != nil {
		t.Fatalf("ListSponsorPrizeEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].AwardID != approved.AwardID || entries[0].ProjectID != projectID || len(entries[0].Participants) != 2 {
		t.Fatalf("sponsor prize entries mismatch: %+v", entries)
	}
	contacts := map[string]string{}
	for _, participant := range entries[0].Participants {
		contacts[participant.PersonID] = participant.Email
	}
	if contacts[personID] != smokePersonEmail(t, ctx, personID) {
		t.Fatalf("consenting participant email = %q, want verified address", contacts[personID])
	}
	if contacts[nonConsentingPersonID] != "" {
		t.Fatalf("non-consenting participant email was disclosed: %q", contacts[nonConsentingPersonID])
	}
	if err := SubmitProject(ctx, projectID); err != nil {
		t.Fatalf("SubmitProject sponsor entry: %v", err)
	}
	generalProjectID := createSmokeProject(t, ctx, ProjectInput{
		CompetitionID: competitionID, CreatedByPersonID: personID,
		Slug: "general-sponsor-entry-" + suffix, Title: "General entry " + suffix,
		ShortDescription: "A submitted project that did not enter the sponsor challenge.",
	})
	if err := SubmitProject(ctx, generalProjectID); err != nil {
		t.Fatalf("SubmitProject general sponsor entry: %v", err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		UPDATE projects SET submitted_at = '2020-01-01 00:00:00+00'::timestamptz
		WHERE id = $1::uuid
	`, generalProjectID); err != nil {
		t.Fatalf("make general sponsor entry historical: %v", err)
	}
	sponsorViewerID := insertSmokePerson(t, ctx, "sponsor-project-viewer-"+suffix)
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO organization_memberships (organization_id, person_id, role, status)
		VALUES ($1::uuid, $2::uuid, 'member', 'active')
	`, orgID, sponsorViewerID); err != nil {
		t.Fatalf("insert sponsor project viewer: %v", err)
	}
	sponsoredVisible, err := CanViewProject(ctx, projectID, types.HackathonViewer{PersonID: sponsorViewerID})
	if err != nil || !sponsoredVisible {
		t.Fatalf("sponsor could not open its prize submission: visible=%t err=%v", sponsoredVisible, err)
	}
	generalVisible, err := CanViewProject(ctx, generalProjectID, types.HackathonViewer{PersonID: sponsorViewerID})
	if err != nil || generalVisible {
		t.Fatalf("sponsored-only viewer opened unrelated submission: visible=%t err=%v", generalVisible, err)
	}
	entries, err = ListSponsorPrizeEntries(ctx, orgID, true)
	if err != nil || len(entries) != 1 {
		t.Fatalf("sponsored-only access included unrelated submission: entries=%+v err=%v", entries, err)
	}
	exportEntries, err := ListSponsorPrizeEntriesForExport(ctx, orgID)
	if err != nil || len(exportEntries) != 0 {
		t.Fatalf("participant export bypassed entitlement: entries=%+v err=%v", exportEntries, err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		UPDATE sponsorship_entitlements
		SET all_hackathon_submissions_access = true,
			automatic_submission_contact_access = true,
			participant_contact_access = true,
			participant_contact_export = true
		WHERE sponsorship_id = $1::uuid AND conference_id = $2::uuid
	`, sponsorshipID, confID); err != nil {
		t.Fatalf("enable all hackathon submission access: %v", err)
	}
	entries, err = ListSponsorPrizeEntries(ctx, orgID, true)
	if err != nil || len(entries) != 2 {
		t.Fatalf("all-submission access entries=%+v err=%v, want two projects", entries, err)
	}
	generalVisible, err = CanViewProject(ctx, generalProjectID, types.HackathonViewer{PersonID: sponsorViewerID})
	if err != nil || !generalVisible {
		t.Fatalf("all-submission sponsor could not open project: visible=%t err=%v", generalVisible, err)
	}
	var generalEntry *types.SponsorPrizeEntry
	for _, entry := range entries {
		if entry.ProjectID == generalProjectID {
			generalEntry = entry
		}
	}
	if generalEntry == nil || generalEntry.SponsoredPrize || generalEntry.AwardID != "" || len(generalEntry.Participants) != 1 {
		t.Fatalf("general hackathon submission mismatch: %+v", generalEntry)
	}
	if generalEntry.Participants[0].Email != smokePersonEmail(t, ctx, personID) || generalEntry.Participants[0].ConsentScope != "included_sponsorship" {
		t.Fatalf("included sponsorship did not disclose submitted participant email: %+v", generalEntry.Participants[0])
	}
	exportEntries, err = ListSponsorPrizeEntriesForExport(ctx, orgID)
	if err != nil || len(exportEntries) != 2 {
		t.Fatalf("participant export entries=%+v err=%v, want two projects", exportEntries, err)
	}
	entriesWithoutContacts, err := ListSponsorPrizeEntries(ctx, orgID, false)
	if err != nil {
		t.Fatalf("ListSponsorPrizeEntries without contact permission: %v", err)
	}
	for _, participant := range entriesWithoutContacts[0].Participants {
		if participant.Email != "" {
			t.Fatalf("contact was disclosed to a read-only sponsor member: %+v", participant)
		}
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
	token, invite, err := CreateOrganizationMemberInvite(ctx, orgID, invitedEmail, OrganizationRoleManager, personID, time.Now().Add(time.Hour))
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
	_, pendingInvite, err := CreateOrganizationMemberInvite(ctx, orgID, invitedEmail, OrganizationRoleManager, personID, time.Now().Add(time.Hour))
	if !errors.Is(err, ErrOrganizationMemberInvitePending) || pendingInvite == nil || pendingInvite.ID != invite.ID {
		t.Fatalf("duplicate organization invite: pending=%+v err=%v", pendingInvite, err)
	}
	replacementToken, replacementInvite, err := ReplaceOrganizationMemberInvite(ctx, orgID, invite.ID, personID, time.Now().Add(72*time.Hour))
	if err != nil || replacementToken == "" || replacementInvite == nil || replacementInvite.ID == invite.ID || replacementInvite.Email != invite.Email {
		t.Fatalf("ReplaceOrganizationMemberInvite: token=%q invite=%+v err=%v", replacementToken, replacementInvite, err)
	}
	if _, err := AcceptOrganizationMemberInvite(ctx, token, invitedPersonID); err == nil {
		t.Fatal("replaced organization invitation remained usable")
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
	if err := RemoveOrganizationMembership(ctx, orgID, invitedPersonID, personID); err == nil {
		t.Fatal("manager removed the organization owner")
	}
	if err := RemoveOrganizationMembership(ctx, orgID, personID, personID); err == nil {
		t.Fatal("last active owner removed themselves")
	}
	if err := RemoveOrganizationMembership(ctx, orgID, invitedPersonID, invitedPersonID); err != nil {
		t.Fatalf("manager could not remove themselves: %v", err)
	}
	removedMembership, err := GetOrganizationMembership(ctx, invitedPersonID, orgID)
	if err != nil || removedMembership != nil {
		t.Fatalf("removed membership remained active: membership=%+v err=%v", removedMembership, err)
	}

	directPersonID := insertSmokePerson(t, ctx, "sponsor-direct-member-"+suffix)
	if err := AddOrganizationMembershipAsAdmin(ctx, orgID, directPersonID, OrganizationRoleMember, personID); err != nil {
		t.Fatalf("AddOrganizationMembershipAsAdmin: %v", err)
	}
	if err := AddOrganizationMembershipAsAdmin(ctx, orgID, directPersonID, OrganizationRoleManager, personID); err == nil {
		t.Fatal("AddOrganizationMembershipAsAdmin replaced an active membership")
	}
	directMembership, err := GetOrganizationMembership(ctx, directPersonID, orgID)
	if err != nil || directMembership == nil || directMembership.Role != OrganizationRoleMember {
		t.Fatalf("direct organization membership: membership=%+v err=%v", directMembership, err)
	}
	if err := UpdateOrganizationMembershipRoleAsAdmin(ctx, orgID, directPersonID, OrganizationRoleOwner); err != nil {
		t.Fatalf("promote direct organization member: %v", err)
	}
	if err := UpdateOrganizationMembershipRoleAsAdmin(ctx, orgID, personID, OrganizationRoleManager); err != nil {
		t.Fatalf("demote original owner with another owner active: %v", err)
	}
	if err := UpdateOrganizationMembershipRoleAsAdmin(ctx, orgID, directPersonID, OrganizationRoleMember); err == nil {
		t.Fatal("demoted the organization's last active owner")
	}
	if err := UpdateOrganizationMembershipRoleAsAdmin(ctx, orgID, personID, OrganizationRoleOwner); err != nil {
		t.Fatalf("restore original organization owner: %v", err)
	}
	if err := UpdateOrganizationMembershipRoleAsAdmin(ctx, orgID, directPersonID, OrganizationRoleManager); err != nil {
		t.Fatalf("change direct member role: %v", err)
	}
	if err := RemoveOrganizationMembershipAsAdmin(ctx, orgID, directPersonID); err != nil {
		t.Fatalf("RemoveOrganizationMembershipAsAdmin: %v", err)
	}
	if err := RemoveOrganizationMembershipAsAdmin(ctx, orgID, personID); err == nil {
		t.Fatal("admin removed the organization's last active owner")
	}

	revokedInvitePersonID := insertSmokePerson(t, ctx, "sponsor-revoked-invite-"+suffix)
	revokedInviteEmail := smokePersonEmail(t, ctx, revokedInvitePersonID)
	revokedToken, revokedInvite, err := CreateOrganizationMemberInvite(ctx, orgID, revokedInviteEmail, OrganizationRoleManager, personID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("create invitation to update and revoke: %v", err)
	}
	if err := UpdateOrganizationMemberInviteRoleAsAdmin(ctx, orgID, revokedInvite.ID, OrganizationRoleMember); err != nil {
		t.Fatalf("UpdateOrganizationMemberInviteRoleAsAdmin: %v", err)
	}
	updatedInvite, err := GetOrganizationMemberInviteByToken(ctx, revokedToken)
	if err != nil || updatedInvite == nil || updatedInvite.Role != OrganizationRoleMember {
		t.Fatalf("updated pending invitation: invite=%+v err=%v", updatedInvite, err)
	}
	if err := RevokeOrganizationMemberInviteAsAdmin(ctx, orgID, revokedInvite.ID); err != nil {
		t.Fatalf("RevokeOrganizationMemberInviteAsAdmin: %v", err)
	}
	if _, err := AcceptOrganizationMemberInvite(ctx, revokedToken, revokedInvitePersonID); err == nil {
		t.Fatal("accepted an admin-revoked organization invitation")
	}

	directPendingPersonID := insertSmokePerson(t, ctx, "sponsor-direct-pending-"+suffix)
	directPendingEmail := smokePersonEmail(t, ctx, directPendingPersonID)
	directPendingToken, _, err := CreateOrganizationMemberInvite(ctx, orgID, directPendingEmail, OrganizationRoleMember, personID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("create invitation before direct add: %v", err)
	}
	if err := AddOrganizationMembershipAsAdmin(ctx, orgID, directPendingPersonID, OrganizationRoleManager, personID); err != nil {
		t.Fatalf("directly add pending invitee: %v", err)
	}
	if _, err := AcceptOrganizationMemberInvite(ctx, directPendingToken, directPendingPersonID); err == nil {
		t.Fatal("direct add left the person's pending invitation usable")
	}
}

package getters

import (
	"context"
	"errors"
	"testing"

	"btcpp-web/internal/types"
)

func TestOrganizationCommunityMembershipAndApplications(t *testing.T) {
	ctx := postgresSmokeContext(t)
	suffix := postgresSmokeSuffix()
	ownerID := insertSmokePerson(t, ctx, "org-community-owner-"+suffix)
	requesterID := insertSmokePerson(t, ctx, "org-community-requester-"+suffix)
	autoMemberID := insertSmokePerson(t, ctx, "org-community-auto-"+suffix)
	applicantID := insertSmokePerson(t, ctx, "org-community-applicant-"+suffix)
	applicantEmail := smokePersonEmail(t, ctx, applicantID)

	var organizationID string
	if err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO organizations (name, tagline, membership_policy)
		VALUES ($1, 'Community fixture', 'request') RETURNING id::text
	`, "Community "+suffix).Scan(&organizationID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO organization_memberships (organization_id, person_id, role, status)
		VALUES ($1::uuid, $2::uuid, 'owner', 'active')
	`, organizationID, ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM organization_applications WHERE submitted_by_person_id = $1::uuid`, applicantID)
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM organizations WHERE name IN ($1, $2)`, "Community "+suffix, "Proposed "+suffix)
	})

	directory, err := ListOrganizationDirectoryForPerson(ctx, requesterID)
	if err != nil {
		t.Fatalf("ListOrganizationDirectoryForPerson: %v", err)
	}
	found := false
	for _, entry := range directory {
		if entry.Organization.Ref == organizationID {
			found = entry.Organization.MembershipPolicy == OrganizationMembershipPolicyRequest && entry.MembershipRole == ""
		}
	}
	if !found {
		t.Fatalf("directory omitted request-only organization: %+v", directory)
	}
	if _, err := ctx.DB.Exec(context.Background(), `UPDATE organizations SET hidden_from_directory = true WHERE id = $1::uuid`, organizationID); err != nil {
		t.Fatalf("hide organization from directory: %v", err)
	}
	hiddenDirectory, err := ListOrganizationDirectoryForPerson(ctx, requesterID)
	if err != nil {
		t.Fatalf("list directory with hidden organization: %v", err)
	}
	for _, entry := range hiddenDirectory {
		if entry.Organization.Ref == organizationID {
			t.Fatalf("hidden organization appeared in directory: %+v", entry)
		}
	}
	hiddenOrg, err := GetOrg(ctx, organizationID)
	if err != nil || !hiddenOrg.HiddenFromDirectory {
		t.Fatalf("administrative organization lookup lost visibility setting: %+v err=%v", hiddenOrg, err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `UPDATE organizations SET hidden_from_directory = false WHERE id = $1::uuid`, organizationID); err != nil {
		t.Fatalf("restore organization directory visibility: %v", err)
	}
	filteredDirectory, err := ListOrganizationDirectoryForPersonFiltered(ctx, requesterID, "Community "+suffix, 0)
	if err != nil || len(filteredDirectory) != 1 || filteredDirectory[0].Organization.Ref != organizationID {
		t.Fatalf("filtered organization directory = %+v err=%v", filteredDirectory, err)
	}
	limitedDirectory, err := ListOrganizationDirectoryForPersonFiltered(ctx, requesterID, "", 1)
	if err != nil || len(limitedDirectory) != 1 {
		t.Fatalf("limited organization directory = %+v err=%v", limitedDirectory, err)
	}
	if _, err := ListOrganizationDirectoryForPersonFiltered(ctx, requesterID, "", -1); err == nil {
		t.Fatal("organization directory accepted a negative limit")
	}

	request, autoApproved, err := CreateOrganizationMembershipRequest(ctx, organizationID, requesterID, "I contribute locally")
	if err != nil || autoApproved || request.Status != "pending" {
		t.Fatalf("manual membership request = %+v auto=%t err=%v", request, autoApproved, err)
	}
	if request.PersonName == "" || request.PersonEmail == "" || request.OrganizationName == "" {
		t.Fatalf("membership request omitted notification details: %+v", request)
	}
	managerRecipients, err := ListOrganizationManagerRecipients(ctx, organizationID)
	if err != nil || len(managerRecipients) != 1 || managerRecipients[0].PersonID != ownerID || managerRecipients[0].Email == "" {
		t.Fatalf("organization manager recipients = %+v err=%v", managerRecipients, err)
	}
	if _, _, err := CreateOrganizationMembershipRequest(ctx, organizationID, requesterID, "again"); !errors.Is(err, ErrOrganizationMembershipRequestPending) {
		t.Fatalf("duplicate membership request error = %v", err)
	}
	pending, err := ListPendingOrganizationMembershipRequests(ctx, organizationID)
	if err != nil || len(pending) != 1 || pending[0].PersonID != requesterID {
		t.Fatalf("pending requests = %+v err=%v", pending, err)
	}
	reviewedRequest, err := ReviewOrganizationMembershipRequest(ctx, organizationID, request.ID, ownerID, "approved", "welcome")
	if err != nil {
		t.Fatalf("approve membership request: %v", err)
	}
	if reviewedRequest.PersonEmail == "" || reviewedRequest.OrganizationName == "" || reviewedRequest.ReviewNote != "welcome" {
		t.Fatalf("reviewed request omitted notification details: %+v", reviewedRequest)
	}
	if membership, err := GetOrganizationMembership(ctx, requesterID, organizationID); err != nil || membership == nil || membership.Role != OrganizationRoleMember {
		t.Fatalf("approved membership = %+v err=%v", membership, err)
	}

	if err := UpdateOrganizationMembershipPolicy(ctx, organizationID, OrganizationMembershipPolicyOpen); err != nil {
		t.Fatalf("open membership policy: %v", err)
	}
	autoRequest, autoApproved, err := CreateOrganizationMembershipRequest(ctx, organizationID, autoMemberID, "")
	if err != nil || !autoApproved || autoRequest.Status != "approved" {
		t.Fatalf("automatic membership request = %+v auto=%t err=%v", autoRequest, autoApproved, err)
	}
	if membership, err := GetOrganizationMembership(ctx, autoMemberID, organizationID); err != nil || membership == nil {
		t.Fatalf("automatic membership = %+v err=%v", membership, err)
	}
	if err := UpdateOrganizationMembershipPolicy(ctx, organizationID, OrganizationMembershipPolicyClosed); err != nil {
		t.Fatalf("close membership policy: %v", err)
	}
	outsiderID := insertSmokePerson(t, ctx, "org-community-closed-"+suffix)
	if _, _, err := CreateOrganizationMembershipRequest(ctx, organizationID, outsiderID, ""); err == nil {
		t.Fatal("closed organization accepted a membership request")
	}

	application := &types.OrganizationApplication{
		SubmittedByPersonID: applicantID, ApplicantEmail: applicantEmail,
		Name: "Proposed " + suffix, Tagline: "A proposed community",
		Website: "https://example.test", LogoLight: "https://cdn.example.test/logo-light.svg",
		LogoDark: "https://cdn.example.test/logo-dark.svg", Notes: "We meet every month.",
	}
	invalidContact := *application
	invalidContact.ContactEmail = "not-an-email"
	if err := CreateOrganizationApplication(ctx, &invalidContact); err == nil {
		t.Fatal("organization application accepted an invalid contact email")
	}
	missingLightLogo := *application
	missingLightLogo.LogoLight = ""
	if err := CreateOrganizationApplication(ctx, &missingLightLogo); err == nil {
		t.Fatal("organization application accepted a missing light-background logo")
	}
	missingDarkLogo := *application
	missingDarkLogo.LogoDark = ""
	if err := CreateOrganizationApplication(ctx, &missingDarkLogo); err == nil {
		t.Fatal("organization application accepted a missing dark-background logo")
	}
	if err := CreateOrganizationApplication(ctx, application); err != nil {
		t.Fatalf("CreateOrganizationApplication: %v", err)
	}
	duplicate := *application
	duplicate.ID = ""
	if err := CreateOrganizationApplication(ctx, &duplicate); !errors.Is(err, ErrOrganizationApplicationPending) {
		t.Fatalf("duplicate organization application error = %v", err)
	}
	applications, err := ListOrganizationApplications(ctx, "pending")
	if err != nil {
		t.Fatalf("ListOrganizationApplications: %v", err)
	}
	applicationFound := false
	for _, candidate := range applications {
		applicationFound = applicationFound || candidate.ID == application.ID
	}
	if !applicationFound {
		t.Fatalf("pending application omitted %s", application.ID)
	}
	application.ReviewNote = "Approved fixture"
	approved, err := ReviewOrganizationApplication(ctx, application.ID, ownerID, "approved", application)
	if err != nil || approved.OrganizationID == "" || approved.Status != "approved" {
		t.Fatalf("approved organization application = %+v err=%v", approved, err)
	}
	if membership, err := GetOrganizationMembership(ctx, applicantID, approved.OrganizationID); err != nil || membership == nil || membership.Role != OrganizationRoleOwner {
		t.Fatalf("application owner membership = %+v err=%v", membership, err)
	}
	approvedOrganization, err := GetOrg(ctx, approved.OrganizationID)
	if err != nil || approvedOrganization.LogoLight != application.LogoLight || approvedOrganization.LogoDark != application.LogoDark {
		t.Fatalf("approved organization logos = %+v err=%v", approvedOrganization, err)
	}
}

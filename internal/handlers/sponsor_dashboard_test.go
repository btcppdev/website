package handlers

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"btcpp-web/external/getters"
	"btcpp-web/internal/types"
)

func TestSponsorManagerInvitationFromForm(t *testing.T) {
	for _, test := range []struct {
		name      string
		values    url.Values
		wantName  string
		wantEmail string
		wantErr   bool
	}{
		{name: "empty", values: url.Values{}},
		{name: "valid", values: url.Values{"ManagerName": {" Ada Nakamoto "}, "ManagerEmail": {" ADA@EXAMPLE.COM "}}, wantName: "Ada Nakamoto", wantEmail: "ada@example.com"},
		{name: "missing name", values: url.Values{"ManagerEmail": {"ada@example.com"}}, wantErr: true},
		{name: "missing email", values: url.Values{"ManagerName": {"Ada"}}, wantErr: true},
		{name: "invalid email", values: url.Values{"ManagerName": {"Ada"}, "ManagerEmail": {"not-an-email"}}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(test.values.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			gotName, gotEmail, err := sponsorManagerInvitationFromForm(r)
			if gotName != test.wantName || gotEmail != test.wantEmail || (err != nil) != test.wantErr {
				t.Fatalf("sponsorManagerInvitationFromForm() = %q, %q, %v; want %q, %q, error=%t", gotName, gotEmail, err, test.wantName, test.wantEmail, test.wantErr)
			}
		})
	}
}

func TestSponsorStatusGrantsCapabilities(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{status: "Paid", want: true},
		{status: " committed ", want: true},
		{status: "InProgress", want: false},
		{status: "Pending", want: false},
		{status: "", want: false},
	}
	for _, test := range tests {
		if got := sponsorStatusGrantsCapabilities(test.status); got != test.want {
			t.Errorf("sponsorStatusGrantsCapabilities(%q) = %t, want %t", test.status, got, test.want)
		}
	}
}

func TestSponsorTicketAllocationFromForm(t *testing.T) {
	for _, test := range []struct {
		raw     string
		want    int
		wantErr bool
	}{
		{raw: "", want: 0},
		{raw: "0", want: 0},
		{raw: "24", want: 24},
		{raw: "-1", wantErr: true},
		{raw: "2.5", wantErr: true},
	} {
		r := httptest.NewRequest("POST", "/?TicketAllocation="+test.raw, nil)
		got, err := sponsorTicketAllocationFromForm(r)
		if (err != nil) != test.wantErr || got != test.want {
			t.Errorf("sponsorTicketAllocationFromForm(%q) = %d, %v; want %d, error=%t", test.raw, got, err, test.want, test.wantErr)
		}
	}
}

func TestSponsorAwardLimitFromForm(t *testing.T) {
	for _, test := range []struct {
		raw     string
		want    int
		wantErr bool
	}{
		{raw: "", want: 0},
		{raw: "3", want: 3},
		{raw: "-1", wantErr: true},
		{raw: "one", wantErr: true},
	} {
		r := httptest.NewRequest("POST", "/?SponsorAwardLimit="+test.raw, nil)
		got, err := sponsorAwardLimitFromForm(r)
		if (err != nil) != test.wantErr || got != test.want {
			t.Errorf("sponsorAwardLimitFromForm(%q) = %d, %v; want %d, error=%t", test.raw, got, err, test.want, test.wantErr)
		}
	}
}

func TestSponsorLevelIncludesAllHackathonSubmissions(t *testing.T) {
	for _, level := range []string{"Headline", " headline ", "Hackathon", "HACKATHON"} {
		if !sponsorLevelIncludesAllHackathonSubmissions(level) {
			t.Errorf("sponsorLevelIncludesAllHackathonSubmissions(%q) = false, want true", level)
		}
	}
	for _, level := range []string{"Title", "Diamond", "Gold", "Workshop", ""} {
		if sponsorLevelIncludesAllHackathonSubmissions(level) {
			t.Errorf("sponsorLevelIncludesAllHackathonSubmissions(%q) = true, want false", level)
		}
	}
}

func TestSponsorDashboardSeparatesCurrentAndPastHackathonEntries(t *testing.T) {
	page := &SponsorDashboardPage{
		Past: []*types.SponsorDashboardEvent{{Conference: &types.Conf{Ref: "past-conf"}}},
		PrizeEntries: []*types.SponsorPrizeEntry{
			{ProjectID: "current-project", ConferenceID: "current-conf"},
			{ProjectID: "past-project", ConferenceID: "past-conf"},
		},
	}
	current := page.CurrentHackathonEntries()
	past := page.PastHackathonEntries()
	if len(current) != 1 || current[0].ProjectID != "current-project" {
		t.Fatalf("current entries = %+v", current)
	}
	if len(past) != 1 || past[0].ProjectID != "past-project" {
		t.Fatalf("past entries = %+v", past)
	}
}

func TestSponsorParticipantCSVRowsDeduplicateProjectParticipants(t *testing.T) {
	projectNumber := 7
	participant := &types.SponsorPrizeParticipant{
		PersonID: "person-id", Name: "=Mara", Role: "owner", PublicID: "mara",
		Email: "mara@example.test", AvailableToHire: true, ConsentScope: "included_sponsorship",
	}
	entries := []*types.SponsorPrizeEntry{
		{ConferenceID: "conf-id", ConferenceTag: "dev26", ConferenceTitle: "Local Dev", ProjectID: "project-id", ProjectNumber: &projectNumber, ProjectTitle: "Fixture Forge", ProjectStatus: "submitted", AwardTitle: "Best Tool", SponsoredPrize: true, Winner: true, Participants: []*types.SponsorPrizeParticipant{participant}},
		{ConferenceID: "conf-id", ConferenceTag: "dev26", ConferenceTitle: "Local Dev", ProjectID: "project-id", ProjectNumber: &projectNumber, ProjectTitle: "Fixture Forge", ProjectStatus: "submitted", AwardTitle: "Best Demo", SponsoredPrize: true, Participants: []*types.SponsorPrizeParticipant{participant}},
	}
	r := httptest.NewRequest("GET", "http://localhost:8888/dashboard/sponsor/org-id/hackathon-projects.csv", nil)
	rows := sponsorParticipantCSVRows(r, entries)
	if len(rows) != 1 {
		t.Fatalf("CSV rows = %+v, want one participant row", rows)
	}
	row := rows[0]
	if len(row.Awards) != 2 || !row.Winner || row.ProjectURL != "http://localhost:8888/dev26/hackathon/projects/project-id" || row.ProfileURL != "http://localhost:8888/whois/mara" {
		t.Fatalf("CSV row mismatch: %+v", row)
	}
	if got := sponsorCSVSafeCell(participant.Name); got != "'=Mara" {
		t.Fatalf("sponsorCSVSafeCell(%q) = %q", participant.Name, got)
	}
	if row.ContactBasis != "Included with submission" {
		t.Fatalf("contact basis = %q", row.ContactBasis)
	}
	if !row.AvailableToHire || sponsorCSVYesNo(row.AvailableToHire) != "Yes" {
		t.Fatalf("available to hire = %t", row.AvailableToHire)
	}
}

func TestProtectSponsorInviteResponse(t *testing.T) {
	w := httptest.NewRecorder()
	protectSponsorInviteResponse(w)
	if got := w.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
}

func TestSponsorContactConsentBelongsToIndividualProjectMember(t *testing.T) {
	members := []*types.ProjectMember{{PersonID: "person-a"}}
	if !viewerCanSetSponsorContactConsent(members, "person-a") {
		t.Fatal("project member could not set their own sponsor contact consent")
	}
	if viewerCanSetSponsorContactConsent(members, "person-b") {
		t.Fatal("non-member could set sponsor contact consent for a project")
	}
}

func TestSponsorDashboardMemberRemovalEligibility(t *testing.T) {
	owner := &types.OrganizationMembership{PersonID: "owner", Role: getters.OrganizationRoleOwner, Status: "active"}
	otherOwner := &types.OrganizationMembership{PersonID: "other-owner", Role: getters.OrganizationRoleOwner, Status: "active"}
	manager := &types.OrganizationMembership{PersonID: "manager", Role: getters.OrganizationRoleManager, Status: "active"}
	otherManager := &types.OrganizationMembership{PersonID: "other-manager", Role: getters.OrganizationRoleManager, Status: "active"}
	member := &types.OrganizationMembership{PersonID: "member", Role: getters.OrganizationRoleMember, Status: "active"}

	page := &SponsorDashboardPage{Membership: owner, Members: []*types.OrganizationMembership{owner, manager, member}}
	if page.CanRemoveMember(owner) {
		t.Fatal("last owner was allowed to remove themselves")
	}
	if !page.CanRemoveMember(manager) || !page.CanRemoveMember(member) {
		t.Fatal("owner could not remove another organization member")
	}
	page.Members = append(page.Members, otherOwner)
	if !page.CanRemoveMember(owner) || !page.CanRemoveMember(otherOwner) {
		t.Fatal("owner could not remove themselves or another owner when another owner remains")
	}

	page.Membership = manager
	page.Members = []*types.OrganizationMembership{owner, manager, otherManager, member}
	if !page.CanRemoveMember(manager) || !page.CanRemoveMember(member) {
		t.Fatal("manager could not remove themselves or an ordinary member")
	}
	if page.CanRemoveMember(owner) || page.CanRemoveMember(otherManager) {
		t.Fatal("manager could remove an owner or another manager")
	}

	page.Membership = member
	if !page.CanRemoveMember(member) || page.CanRemoveMember(manager) {
		t.Fatal("ordinary member self-removal permissions are incorrect")
	}
}

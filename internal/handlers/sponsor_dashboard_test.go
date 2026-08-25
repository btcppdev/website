package handlers

import (
	"net/http/httptest"
	"testing"

	"btcpp-web/internal/types"
)

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

package handlers

import (
	"testing"

	"btcpp-web/external/getters"
	"btcpp-web/internal/types"
)

func TestOrganizationDashboardMemberRemovalEligibility(t *testing.T) {
	owner := &types.OrganizationMembership{PersonID: "owner", Role: getters.OrganizationRoleOwner, Status: "active"}
	otherOwner := &types.OrganizationMembership{PersonID: "other-owner", Role: getters.OrganizationRoleOwner, Status: "active"}
	manager := &types.OrganizationMembership{PersonID: "manager", Role: getters.OrganizationRoleManager, Status: "active"}
	otherManager := &types.OrganizationMembership{PersonID: "other-manager", Role: getters.OrganizationRoleManager, Status: "active"}
	member := &types.OrganizationMembership{PersonID: "member", Role: getters.OrganizationRoleMember, Status: "active"}

	page := &OrganizationDashboardPage{Membership: owner, Members: []*types.OrganizationMembership{owner, manager, member}}
	if page.CanRemoveMember(owner) {
		t.Fatal("last owner was allowed to leave")
	}
	if !page.CanRemoveMember(manager) || !page.CanRemoveMember(member) {
		t.Fatal("owner could not remove another organization member")
	}
	page.Members = append(page.Members, otherOwner)
	if !page.CanRemoveMember(owner) || !page.CanRemoveMember(otherOwner) {
		t.Fatal("owner could not remove an owner when another owner remains")
	}

	page.Membership = manager
	page.Members = []*types.OrganizationMembership{owner, manager, otherManager, member}
	if !page.CanRemoveMember(manager) || !page.CanRemoveMember(member) {
		t.Fatal("manager could not leave or remove an ordinary member")
	}
	if page.CanRemoveMember(owner) || page.CanRemoveMember(otherManager) {
		t.Fatal("manager could remove an owner or peer manager")
	}

	page.Membership = member
	if !page.CanRemoveMember(member) || page.CanRemoveMember(manager) {
		t.Fatal("ordinary member removal permissions are incorrect")
	}
}

package handlers

import (
	"testing"

	"btcpp-web/internal/auth"
)

func TestAccountsAdminRoleCanOnlyBeAssignedByNifty(t *testing.T) {
	nifty := &auth.Identity{PrimaryEmail: " NIFTY@btcpp.dev "}
	otherAdmin := &auth.Identity{PrimaryEmail: "admin@btcpp.dev"}
	aliasLogin := &auth.Identity{PrimaryEmail: "admin@btcpp.dev", LoginEmail: "nifty@btcpp.dev"}

	if !accountsAdminRoleUpdateAllowed(nifty, nil, []string{auth.AccountsAdminTag}) {
		t.Fatal("nifty could not assign accts-admin")
	}
	if accountsAdminRoleUpdateAllowed(otherAdmin, nil, []string{"global-admin", auth.AccountsAdminTag}) {
		t.Fatal("another administrator could assign accts-admin")
	}
	if accountsAdminRoleUpdateAllowed(aliasLogin, nil, []string{auth.AccountsAdminTag}) {
		t.Fatal("logging in through a nifty alias bypassed the canonical-email check")
	}
	if !accountsAdminRoleUpdateAllowed(otherAdmin, []string{auth.AccountsAdminTag}, []string{"global-admin", auth.AccountsAdminTag}) {
		t.Fatal("another administrator could not preserve an existing accts-admin role")
	}
	if !accountsAdminRoleUpdateAllowed(otherAdmin, []string{auth.AccountsAdminTag}, []string{"global-admin"}) {
		t.Fatal("another administrator could not remove an existing accts-admin role")
	}
}

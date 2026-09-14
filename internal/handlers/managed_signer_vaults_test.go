package handlers

import (
	"btcpp-web/internal/auth"
	"btcpp-web/internal/types"
	"bytes"
	"html/template"
	"net/url"
	"strings"
	"testing"
)

func TestManagedSignerVaultChoicesFilterMemberships(t *testing.T) {
	identity := &auth.Identity{PersonID: "person-1", Speaker: &types.Speaker{Name: "Alice & Bob"}}
	membership := func(id, role, status string) *types.OrganizationMembership {
		return &types.OrganizationMembership{OrganizationID: id, Role: role, Status: status, Organization: &types.Org{Name: "Org " + id}}
	}
	choices := managedSignerVaultChoices("https://bunker.example/", identity, []*types.OrganizationMembership{
		nil, membership("owner", "owner", "active"), membership("manager", "manager", "active"), membership("member", "member", "active"), membership("pending", "manager", "pending"), membership("removed", "owner", "removed"), membership("owner", "owner", "active"),
	})
	if len(choices) != 3 {
		t.Fatalf("expected personal and two managed vaults, got %#v", choices)
	}
	for i, choice := range choices {
		destination, err := url.Parse(choice.URL)
		if err != nil {
			t.Fatal(err)
		}
		if destination.Host != "bunker.example" || destination.Query().Get("tenant_name") != choice.Name {
			t.Fatalf("bad destination: %s", choice.URL)
		}
		if i == 0 && (destination.Query().Get("tenant_id") != "person-1" || destination.Query().Get("open") != "1") {
			t.Fatal("personal vault must check authorized status")
		}
	}
	if len(managedSignerVaultChoices("https://bunker.example", nil, nil)) != 0 {
		t.Fatal("anonymous account received vaults")
	}
}

func TestManagedSignerVaultPickerTemplate(t *testing.T) {
	tmpl, err := template.ParseFiles("../../templates/managed_signer_vaults.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = tmpl.ExecuteTemplate(&output, "managed_signer_vaults.tmpl", managedSignerVaultsPage{PersonName: "<script>alert(1)</script>", Vaults: []managedSignerVaultChoice{{Name: "My personal vault", Kind: "Personal", URL: "https://bunker.example/?tenant=person&tenant_id=person-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "<script>") || !strings.Contains(output.String(), "Choose a key vault.") || !strings.Contains(output.String(), "My personal vault") {
		t.Fatal("picker is missing content or escaping")
	}
}

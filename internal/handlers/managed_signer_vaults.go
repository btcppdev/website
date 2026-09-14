package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

type managedSignerVaultChoice struct{ Name, Kind, URL string }
type managedSignerVaultsPage struct {
	PersonName string
	Vaults     []managedSignerVaultChoice
	Year       uint
}

// Memberships come from the current Bitcoin++ account, never from query parameters.
// Selecting a vault grants no signing permission; its actions still require approval.
func ManagedSignerVaults(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	setManagedSignerHeaders(w, ctx)
	identity, memberships, ok := organizationDashboardIdentity(w, r, ctx)
	if !ok || identity == nil {
		return
	}
	if identity.PersonID == "" {
		http.Error(w, "A personal account is required to open a vault.", http.StatusForbidden)
		return
	}
	if strings.TrimSpace(ctx.Env.SignerURL) == "" {
		http.Error(w, "Key vaults are not configured.", http.StatusServiceUnavailable)
		return
	}
	page := managedSignerVaultsPage{Vaults: managedSignerVaultChoices(ctx.Env.SignerURL, identity, memberships), Year: helpers.CurrentYear()}
	if identity.Speaker != nil {
		page.PersonName = identity.Speaker.Name
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "managed_signer_vaults.tmpl", page); err != nil {
		ctx.Err.Printf("render key vault picker: %s", err)
		http.Error(w, "Unable to load your vaults.", http.StatusInternalServerError)
	}
}

func managedSignerVaultChoices(signerURL string, identity *auth.Identity, memberships []*types.OrganizationMembership) []managedSignerVaultChoice {
	if identity == nil || identity.PersonID == "" {
		return nil
	}
	name := "My personal vault"
	if identity.Speaker != nil && strings.TrimSpace(identity.Speaker.Name) != "" {
		name = identity.Speaker.Name
	}
	choice := func(tenant, id, name, kind, role string) managedSignerVaultChoice {
		query := url.Values{"tenant": {tenant}, "tenant_id": {id}, "tenant_name": {name}, "role": {role}}
		if tenant == "person" {
			query.Set("open", "1")
		}
		return managedSignerVaultChoice{Name: name, Kind: kind, URL: strings.TrimRight(signerURL, "/") + "/?" + query.Encode()}
	}
	choices := []managedSignerVaultChoice{choice("person", identity.PersonID, name, "My personal vault", "member")}
	seen := map[string]bool{}
	for _, membership := range memberships {
		if membership == nil || membership.Organization == nil || membership.OrganizationID == "" || membership.Status != "active" || (membership.Role != getters.OrganizationRoleOwner && membership.Role != getters.OrganizationRoleManager) || seen[membership.OrganizationID] {
			continue
		}
		seen[membership.OrganizationID] = true
		choices = append(choices, choice("organization", membership.OrganizationID, membership.Organization.Name, "Organization vault", membership.Role))
	}
	return choices
}

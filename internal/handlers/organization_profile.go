package handlers

import (
	"net/http"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

// OrganizationProfilePage is the public, non-membership-gated organization
// surface. Badge Studio remains the credential source of truth and is loaded
// fail-open so an integration outage never takes down the Bitcoin++ profile.
type OrganizationProfilePage struct {
	Organization            *types.Org
	BadgeCatalog            *OrganizationBadgeCatalog
	BadgeCatalogUnavailable bool
	Year                    uint
}

func RenderOrganizationProfile(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	organizationID := strings.TrimSpace(mux.Vars(r)["organizationID"])
	if organizationID == "" {
		handle404(w, r, ctx)
		return
	}
	organization, err := getters.GetOrg(ctx, organizationID)
	if err != nil || organization.HiddenFromDirectory {
		handle404(w, r, ctx)
		return
	}

	var catalog *OrganizationBadgeCatalog
	catalogUnavailable := false
	if ctx.Env != nil && ctx.Env.BadgeStudioURL != "" {
		catalog, err = loadOrganizationBadgeCatalog(r.Context(), ctx.Env.BadgeStudioURL, organizationID)
		if err != nil {
			catalogUnavailable = true
			if ctx.Err != nil {
				ctx.Err.Printf("/organizations/%s badge catalog: %s", organizationID, err)
			}
		}
	}

	if err := ctx.TemplateCache.ExecuteTemplate(w, "organization_profile.tmpl", &OrganizationProfilePage{
		Organization: organization, BadgeCatalog: catalog,
		BadgeCatalogUnavailable: catalogUnavailable, Year: helpers.CurrentYear(),
	}); err != nil {
		http.Error(w, "Unable to load organization profile, please try again later", http.StatusInternalServerError)
		if ctx.Err != nil {
			ctx.Err.Printf("/organizations/%s template: %s", organizationID, err)
		}
	}
}

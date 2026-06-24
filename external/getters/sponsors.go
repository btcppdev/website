package getters

import (
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func ListOrgs(ctx *config.AppContext) ([]*types.Org, error) {
	if UsePostgresBackend(ctx) {
		return listOrgsPostgres(ctx)
	}
	return ListOrgsNotion(ctx.Notion)
}

// SearchOrgsByName returns up to limit orgs whose name contains q
// (case-insensitive substring). Used by the autocomplete on the speaker info
// editor.
func SearchOrgsByName(ctx *config.AppContext, q string, limit int) ([]*types.Org, error) {
	if UsePostgresBackend(ctx) {
		return searchOrgsByNamePostgres(ctx, q, limit)
	}
	q = strings.TrimSpace(strings.ToLower(q))
	if q == "" {
		return nil, nil
	}
	orgs, err := ListOrgs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*types.Org, 0, limit)
	for _, o := range orgs {
		if o == nil || o.Name == "" {
			continue
		}
		if strings.Contains(strings.ToLower(o.Name), q) {
			out = append(out, o)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func GetOrg(ctx *config.AppContext, ref string) (*types.Org, error) {
	if UsePostgresBackend(ctx) {
		return getOrgPostgres(ctx, ref)
	}
	return GetOrgNotion(ctx.Notion, ref)
}

func ListSponsorships(ctx *config.AppContext, confRef string) ([]*types.Sponsorship, error) {
	if UsePostgresBackend(ctx) {
		return listSponsorshipsPostgres(ctx, confRef)
	}
	return ListSponsorshipsNotion(ctx, confRef)
}

func ListSponsorshipsOnly(n *types.Notion) ([]*types.Sponsorship, error) {
	return ListSponsorshipsOnlyNotion(n)
}

// OrgUpdate is a sparse fill-only update for an existing Org row. Empty
// values are skipped.
type OrgUpdate struct {
	Website   string
	Twitter   string // bare handle
	Nostr     string
	Github    string
	LogoLight string // full Spaces URL
	LogoDark  string
}

func RegisterOrg(ctx *config.AppContext, org *types.Org) (string, error) {
	if UsePostgresBackend(ctx) {
		return registerOrgPostgres(ctx, org)
	}
	return RegisterOrgNotion(ctx.Notion, org)
}

func UpdateOrg(ctx *config.AppContext, orgID string, up OrgUpdate) error {
	if UsePostgresBackend(ctx) {
		return updateOrgPostgres(ctx, orgID, up)
	}
	return UpdateOrgNotion(ctx.Notion, orgID, up)
}

func UpdateOrgDetails(ctx *config.AppContext, org *types.Org) error {
	if UsePostgresBackend(ctx) {
		return updateOrgDetailsPostgres(ctx, org)
	}
	return UpdateOrgDetailsNotion(ctx.Notion, org)
}

func FindOrg(ctx *config.AppContext, website, name string) (*types.Org, error) {
	if UsePostgresBackend(ctx) {
		return findOrgPostgres(ctx, website, name)
	}
	return FindOrgNotion(ctx.Notion, website, name)
}

func RegisterSponsorship(ctx *config.AppContext, sp *types.Sponsorship) error {
	if UsePostgresBackend(ctx) {
		return registerSponsorshipPostgres(ctx, sp)
	}
	return RegisterSponsorshipNotion(ctx.Notion, sp)
}

func UpdateSponsorshipStatus(ctx *config.AppContext, ref string, status string) error {
	if UsePostgresBackend(ctx) {
		return updateSponsorshipStatusPostgres(ctx, ref, status)
	}
	return UpdateSponsorshipStatusNotion(ctx.Notion, ref, status)
}

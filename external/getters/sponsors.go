package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"fmt"
	"strings"
)

// SearchOrgsByName returns up to limit orgs whose name contains q
// (case-insensitive substring). Used by the autocomplete on the speaker info
// editor.

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
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	if org == nil {
		return "", fmt.Errorf("RegisterOrg: org is nil")
	}
	normalizeOrgInput(org)
	if org.Name == "" {
		return "", fmt.Errorf("RegisterOrg: org name is required")
	}

	var orgID string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO organizations (
			name, tagline, logo_light_url, logo_dark_url, email, website_url,
			linkedin_url, instagram_url, youtube_url, github_url, twitter_handle,
			nostr, matrix, hiring, notes
		) VALUES (
			$1, $2, $3, $4, NULLIF($5, '')::citext, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15
		)
		RETURNING id::text
	`, org.Name, org.Tagline, org.LogoLight, org.LogoDark, org.Email,
		org.Website, org.LinkedIn, org.Instagram, org.Youtube, org.Github,
		org.Twitter.Handle, org.Nostr, org.Matrix, org.Hiring, org.Notes).Scan(&orgID)
	if err != nil {
		return "", fmt.Errorf("insert org %q: %w", org.Name, err)
	}
	org.Ref = orgID
	return orgID, nil
}

func ListOrgs(ctx *config.AppContext) ([]*types.Org, error) {
	return queryOrgsPostgres(ctx, "organizations", "", nil, 0)
}

func GetOrg(ctx *config.AppContext, ref string) (*types.Org, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("org ref is required")
	}
	orgs, err := queryOrgsPostgres(ctx, "organization", "WHERE id::text = $1", []any{ref}, 1)
	if err != nil {
		return nil, err
	}
	if len(orgs) == 0 {
		return nil, fmt.Errorf("org %s not found", ref)
	}
	return orgs[0], nil
}

func SearchOrgsByName(ctx *config.AppContext, q string, limit int) ([]*types.Org, error) {
	q = strings.TrimSpace(q)
	if limit <= 0 {
		limit = 10
	}
	if q == "" {
		return queryOrgsPostgres(ctx, "organization search defaults", "", nil, limit)
	}
	return queryOrgsPostgres(ctx, "organization search", "WHERE name ILIKE '%' || $1 || '%'", []any{q}, limit)
}

func queryOrgsPostgres(ctx *config.AppContext, label string, whereSQL string, args []any, limit int) ([]*types.Org, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	sql := `
		SELECT id::text, name, tagline, logo_light_url, logo_dark_url,
			coalesce(email::text, ''), website_url, github_url, twitter_handle,
			nostr, matrix, linkedin_url, instagram_url, youtube_url, hiring, notes
		FROM organizations
		` + whereSQL + `
		ORDER BY name
	`
	if limit > 0 {
		args = append(args, limit)
		sql += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", label, err)
	}
	defer rows.Close()

	var out []*types.Org
	for rows.Next() {
		var org types.Org
		var twitterHandle string
		if err := rows.Scan(
			&org.Ref,
			&org.Name,
			&org.Tagline,
			&org.LogoLight,
			&org.LogoDark,
			&org.Email,
			&org.Website,
			&org.Github,
			&twitterHandle,
			&org.Nostr,
			&org.Matrix,
			&org.LinkedIn,
			&org.Instagram,
			&org.Youtube,
			&org.Hiring,
			&org.Notes,
		); err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}
		org.Twitter = types.ParseTwitter(twitterHandle)
		out = append(out, &org)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", label, err)
	}
	return out, nil
}

func FindOrg(ctx *config.AppContext, website, name string) (*types.Org, error) {
	wantSite := normalizeWebsite(website)
	wantName := normalizeName(name)
	if wantSite == "" && wantName == "" {
		return nil, nil
	}
	if wantSite != "" {
		orgs, err := queryOrgsPostgres(ctx, "organization by website", `
			WHERE lower(trim(trailing '/' from website_url)) = $1
		`, []any{wantSite}, 1)
		if err != nil || len(orgs) > 0 {
			return firstOrg(orgs), err
		}
	}
	if wantName != "" {
		orgs, err := queryOrgsPostgres(ctx, "organization by name", `
			WHERE lower(name) = $1
		`, []any{wantName}, 1)
		if err != nil || len(orgs) > 0 {
			return firstOrg(orgs), err
		}
	}
	return nil, nil
}

func firstOrg(orgs []*types.Org) *types.Org {
	if len(orgs) == 0 {
		return nil
	}
	return orgs[0]
}

func UpdateOrg(ctx *config.AppContext, orgID string, up OrgUpdate) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	up = normalizeOrgUpdate(up)
	setParts := []string{}
	args := []interface{}{}
	addSet := func(column string, value interface{}) {
		args = append(args, value)
		setParts = append(setParts, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if up.Website != "" {
		addSet("website_url", up.Website)
	}
	if up.Twitter != "" {
		addSet("twitter_handle", up.Twitter)
	}
	if up.Nostr != "" {
		addSet("nostr", up.Nostr)
	}
	if up.Github != "" {
		addSet("github_url", up.Github)
	}
	if up.LogoLight != "" {
		addSet("logo_light_url", up.LogoLight)
	}
	if up.LogoDark != "" {
		addSet("logo_dark_url", up.LogoDark)
	}
	if len(setParts) == 0 {
		return nil
	}

	args = append(args, orgID)
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE organizations
		SET `+strings.Join(setParts, ", ")+`
		WHERE id = $`+fmt.Sprint(len(args))+`
	`, args...)
	if err != nil {
		return fmt.Errorf("update org %s: %w", orgID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("org %s not found", orgID)
	}
	return nil
}

func UpdateOrgDetails(ctx *config.AppContext, org *types.Org) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if org == nil || strings.TrimSpace(org.Ref) == "" {
		return fmt.Errorf("UpdateOrgDetails: org ref is required")
	}
	normalizeOrgInput(org)
	if org.Name == "" {
		return fmt.Errorf("UpdateOrgDetails: org name is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE organizations
		SET name = $2,
			tagline = $3,
			logo_light_url = $4,
			logo_dark_url = $5,
			email = NULLIF($6, '')::citext,
			website_url = $7,
			linkedin_url = $8,
			instagram_url = $9,
			youtube_url = $10,
			github_url = $11,
			twitter_handle = $12,
			nostr = $13,
			matrix = $14,
			hiring = $15,
			notes = $16
		WHERE id = $1
	`, org.Ref, org.Name, org.Tagline, org.LogoLight, org.LogoDark, org.Email,
		org.Website, org.LinkedIn, org.Instagram, org.Youtube, org.Github,
		org.Twitter.Handle, org.Nostr, org.Matrix, org.Hiring, org.Notes)
	if err != nil {
		return fmt.Errorf("update org details %s: %w", org.Ref, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("org %s not found", org.Ref)
	}
	return nil
}

func ListSponsorships(ctx *config.AppContext, confRef string) ([]*types.Sponsorship, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}

	args := []interface{}{}
	where := "WHERE sponsorships.archived_at IS NULL"
	if confRef != "" {
		args = append(args, confRef)
		where += `
			AND EXISTS (
				SELECT 1
				FROM sponsorships_conferences sc
				WHERE sc.sponsorship_id = sponsorships.id
					AND sc.conference_id::text = $1
			)`
	}

	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT sponsorships.id::text, sponsorships.name,
			coalesce(sponsorships.organization_id::text, ''), sponsorships.level,
			sponsorships.label, sponsorships.status, sponsorships.is_vendor,
			sponsorships.notes
		FROM sponsorships
		`+where+`
		ORDER BY sponsorships.level, sponsorships.name
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query sponsorships: %w", err)
	}
	defer rows.Close()

	orgs, err := ListOrgs(ctx)
	if err != nil {
		return nil, err
	}
	orgByID := make(map[string]*types.Org, len(orgs))
	for _, org := range orgs {
		if org != nil {
			orgByID[org.Ref] = org
		}
	}

	var out []*types.Sponsorship
	ids := []string{}
	byID := map[string]*types.Sponsorship{}
	for rows.Next() {
		var sp types.Sponsorship
		var orgID string
		if err := rows.Scan(
			&sp.Ref,
			&sp.Name,
			&orgID,
			&sp.Level,
			&sp.Label,
			&sp.Status,
			&sp.IsVendor,
			&sp.Notes,
		); err != nil {
			return nil, fmt.Errorf("scan sponsorship: %w", err)
		}
		sp.Org = orgByID[orgID]
		out = append(out, &sp)
		ids = append(ids, sp.Ref)
		byID[sp.Ref] = &sp
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sponsorships: %w", err)
	}
	if err := hydrateSponsorshipConfsPostgres(ctx, ids, byID); err != nil {
		return nil, err
	}
	return out, nil
}

func hydrateSponsorshipConfsPostgres(ctx *config.AppContext, ids []string, byID map[string]*types.Sponsorship) error {
	if len(ids) == 0 {
		return nil
	}
	confs, err := listConferencesOnlyPostgres(ctx)
	if err != nil {
		return err
	}
	confByID := make(map[string]*types.Conf, len(confs))
	for _, conf := range confs {
		if conf != nil {
			confByID[conf.Ref] = conf
		}
	}

	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT sponsorship_id::text, conference_id::text
		FROM sponsorships_conferences
		WHERE sponsorship_id::text = ANY($1::text[])
	`, ids)
	if err != nil {
		return fmt.Errorf("query sponsorship conference links: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var sponsorshipID string
		var confID string
		if err := rows.Scan(&sponsorshipID, &confID); err != nil {
			return fmt.Errorf("scan sponsorship conference link: %w", err)
		}
		sp := byID[sponsorshipID]
		conf := confByID[confID]
		if sp != nil && conf != nil {
			sp.Confs = append(sp.Confs, conf)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sponsorship conference links: %w", err)
	}
	return nil
}

func RegisterSponsorship(ctx *config.AppContext, sp *types.Sponsorship) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if sp == nil {
		return fmt.Errorf("RegisterSponsorship: sponsorship is nil")
	}
	name := sp.Level + " Sponsorship"
	var orgID string
	if sp.Org != nil {
		name = sp.Org.Name + " @ " + sp.Level
		orgID = strings.TrimSpace(sp.Org.Ref)
	}

	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return fmt.Errorf("begin sponsorship registration: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())

	var sponsorshipID string
	err = tx.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO sponsorships (
			organization_id, name, level, label, status, is_vendor, notes
		) VALUES (
			NULLIF($1, '')::uuid, $2, $3, $4, $5, $6, $7
		)
		RETURNING id::text
	`, orgID, name, sp.Level, sp.Label, sp.Status, sp.IsVendor, sp.Notes).Scan(&sponsorshipID)
	if err != nil {
		return fmt.Errorf("insert sponsorship %q: %w", name, err)
	}
	for _, conf := range sp.Confs {
		if conf == nil || strings.TrimSpace(conf.Ref) == "" {
			continue
		}
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO sponsorships_conferences (sponsorship_id, conference_id)
			VALUES ($1, $2)
			ON CONFLICT (sponsorship_id, conference_id) DO NOTHING
		`, sponsorshipID, conf.Ref); err != nil {
			return fmt.Errorf("insert sponsorship conference link %s/%s: %w", sponsorshipID, conf.Ref, err)
		}
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return fmt.Errorf("commit sponsorship registration: %w", err)
	}
	sp.Ref = sponsorshipID
	sp.Name = name
	return nil
}

func UpdateSponsorship(ctx *config.AppContext, confRef string, sp *types.Sponsorship) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if sp == nil || strings.TrimSpace(sp.Ref) == "" {
		return fmt.Errorf("UpdateSponsorship: sponsorship ref is required")
	}
	confRef = strings.TrimSpace(confRef)
	if confRef == "" {
		return fmt.Errorf("UpdateSponsorship: conference ref is required")
	}
	name := sp.Level + " Sponsorship"
	var orgID string
	if sp.Org != nil {
		name = sp.Org.Name + " @ " + sp.Level
		orgID = strings.TrimSpace(sp.Org.Ref)
	}

	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE sponsorships
		SET organization_id = NULLIF($3, '')::uuid,
			name = $4,
			level = $5,
			label = $6,
			status = $7,
			is_vendor = $8,
			notes = $9
		WHERE id = $1
			AND archived_at IS NULL
			AND EXISTS (
				SELECT 1
				FROM sponsorships_conferences sc
				WHERE sc.sponsorship_id = sponsorships.id
					AND sc.conference_id::text = $2
			)
	`, sp.Ref, confRef, orgID, name, sp.Level, sp.Label, sp.Status, sp.IsVendor, sp.Notes)
	if err != nil {
		return fmt.Errorf("update sponsorship %s: %w", sp.Ref, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("sponsorship %s not found for conference %s", sp.Ref, confRef)
	}
	sp.Name = name
	return nil
}

func UpdateSponsorshipStatus(ctx *config.AppContext, ref string, status string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE sponsorships
		SET status = $2
		WHERE id = $1
	`, ref, status)
	if err != nil {
		return fmt.Errorf("update sponsorship %s status: %w", ref, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("sponsorship %s not found", ref)
	}
	return nil
}

func DeleteSponsorship(ctx *config.AppContext, confRef string, ref string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	confRef = strings.TrimSpace(confRef)
	ref = strings.TrimSpace(ref)
	if confRef == "" || ref == "" {
		return fmt.Errorf("DeleteSponsorship: conference ref and sponsorship ref are required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		DELETE FROM sponsorships
		WHERE id = $1
			AND EXISTS (
				SELECT 1
				FROM sponsorships_conferences sc
				WHERE sc.sponsorship_id = sponsorships.id
					AND sc.conference_id::text = $2
			)
	`, ref, confRef)
	if err != nil {
		return fmt.Errorf("delete sponsorship %s: %w", ref, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("sponsorship %s not found for conference %s", ref, confRef)
	}
	return nil
}

package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"fmt"
	"strings"
)

// SpeakerInput is the data needed to create a Speakers/People row.
type SpeakerInput struct {
	Name          string
	Email         string
	Photo         string
	Phone         string
	Signal        string
	Telegram      string
	Twitter       string
	Nostr         string
	Github        string
	Instagram     string
	LinkedIn      string
	Website       string
	Company       string
	OrgLogo       string
	AvailToHire   bool
	LookingToHire bool
	TShirt        string
}

// SpeakerUpdate is a sparse update for an existing speaker/person row. Empty
// strings mean "leave this field alone".
type SpeakerUpdate struct {
	Name      string
	Email     string
	Photo     string
	Phone     string
	Signal    string
	Telegram  string
	Twitter   string
	Nostr     string
	Github    string
	Instagram string
	LinkedIn  string
	Website   string
	Company   string
	OrgLogo   string
	TShirt    string
}

// SearchSpeakersByNameOrEmail returns up to limit Speakers whose Name or Email
// contains q (case-insensitive substring).

func normalizeSpeakerInput(in SpeakerInput) SpeakerInput {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.TrimSpace(in.Email)
	in.Photo = strings.TrimSpace(in.Photo)
	in.Phone = strings.TrimSpace(in.Phone)
	in.Signal = strings.TrimSpace(in.Signal)
	in.Telegram = strings.TrimSpace(in.Telegram)
	in.Twitter = types.ParseTwitter(in.Twitter).Handle
	in.Nostr = strings.TrimSpace(in.Nostr)
	in.Github = strings.TrimSpace(in.Github)
	in.Instagram = strings.TrimSpace(in.Instagram)
	in.LinkedIn = strings.TrimSpace(in.LinkedIn)
	in.Website = strings.TrimSpace(in.Website)
	in.Company = strings.TrimSpace(in.Company)
	in.OrgLogo = strings.TrimSpace(in.OrgLogo)
	in.TShirt = strings.TrimSpace(in.TShirt)
	return in
}

func normalizeSpeakerUpdate(up SpeakerUpdate) SpeakerUpdate {
	up.Name = strings.TrimSpace(up.Name)
	up.Email = strings.TrimSpace(up.Email)
	up.Photo = strings.TrimSpace(up.Photo)
	up.Phone = strings.TrimSpace(up.Phone)
	up.Signal = strings.TrimSpace(up.Signal)
	up.Telegram = strings.TrimSpace(up.Telegram)
	up.Twitter = types.ParseTwitter(up.Twitter).Handle
	up.Nostr = strings.TrimSpace(up.Nostr)
	up.Github = strings.TrimSpace(up.Github)
	up.Instagram = strings.TrimSpace(up.Instagram)
	up.LinkedIn = strings.TrimSpace(up.LinkedIn)
	up.Website = strings.TrimSpace(up.Website)
	up.Company = strings.TrimSpace(up.Company)
	up.OrgLogo = strings.TrimSpace(up.OrgLogo)
	up.TShirt = strings.TrimSpace(up.TShirt)
	return up
}

// MergeUniqueTags returns existing followed by additions not already present.
func MergeUniqueTags(existing, additions []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	out := make([]string, 0, len(existing)+len(additions))
	for _, s := range existing {
		if _, ok := seen[s]; ok || s == "" {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range additions {
		if _, ok := seen[s]; ok || s == "" {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func ListSpeakers(ctx *config.AppContext) ([]*types.Speaker, error) {
	return querySpeakersPostgres(ctx, "people", "ORDER BY lower(people.name), people.id")
}

func GetSpeakersByEmail(ctx *config.AppContext, email string) ([]*types.Speaker, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, nil
	}
	return querySpeakersPostgres(ctx, "people by email", "WHERE people.email = $1 ORDER BY lower(people.name), people.id", email)
}

func FetchSpeakerByID(ctx *config.AppContext, speakerID string) (*types.Speaker, error) {
	speakerID = strings.TrimSpace(speakerID)
	if speakerID == "" {
		return nil, nil
	}
	speakers, err := querySpeakersPostgres(ctx, "person by id", "WHERE people.id::text = $1 ORDER BY lower(people.name), people.id", speakerID)
	if err != nil {
		return nil, err
	}
	if len(speakers) == 0 {
		return nil, nil
	}
	return speakers[0], nil
}

func SearchSpeakersByNameOrEmail(ctx *config.AppContext, q string, limit int) ([]*types.Speaker, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	pattern := "%" + q + "%"
	return querySpeakersPostgres(ctx, "speaker search", `
		WHERE people.name ILIKE $1 OR people.email::text ILIKE $1
		ORDER BY lower(people.name), people.id
		LIMIT $2
	`, pattern, limit)
}

func ListSpeakersWithRole(ctx *config.AppContext, role string) ([]*types.Speaker, error) {
	scope, position, ok := splitRoleScopePosition(role)
	if !ok {
		return nil, nil
	}
	return querySpeakersPostgres(ctx, "speakers by role", `
		JOIN people_roles ON people_roles.person_id = people.id
		WHERE people_roles.scope = $1 AND people_roles.position = $2
		ORDER BY lower(people.name), people.id
	`, scope, position)
}

func ListHomepageFeaturedSpeakers(ctx *config.AppContext) ([]*types.Speaker, error) {
	return querySpeakersPostgres(ctx, "homepage featured speakers", `
		JOIN homepage_featured_speakers hfs ON hfs.person_id = people.id
		ORDER BY hfs.position
	`)
}

func ReplaceHomepageFeaturedSpeakers(ctx *config.AppContext, speakerIDs []string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	tx, err := ctx.DB.Begin(context.Background())
	if err != nil {
		return fmt.Errorf("begin homepage featured speakers update: %w", err)
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(context.Background(), `DELETE FROM homepage_featured_speakers`); err != nil {
		return fmt.Errorf("delete homepage featured speakers: %w", err)
	}
	position := 1
	seen := map[string]bool{}
	for _, id := range speakerIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if position > 8 {
			break
		}
		if _, err := tx.Exec(context.Background(), `
			INSERT INTO homepage_featured_speakers (position, person_id)
			VALUES ($1, $2::uuid)
		`, position, id); err != nil {
			return fmt.Errorf("insert homepage featured speaker %d/%s: %w", position, id, err)
		}
		position++
	}
	if err := tx.Commit(context.Background()); err != nil {
		return fmt.Errorf("commit homepage featured speakers update: %w", err)
	}
	return nil
}

func querySpeakersPostgres(ctx *config.AppContext, label string, clause string, args ...interface{}) ([]*types.Speaker, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(context.Background(), `
		SELECT people.id::text, people.name, coalesce(people.email::text, ''),
			people.norm_photo_path, people.phone, people.signal, people.telegram,
			people.twitter_handle, people.nostr, people.github_url, people.instagram,
			people.linkedin, people.website_url, people.company, people.org_logo_path,
			people.avail_to_hire, people.looking_to_hire, people.tshirt
		FROM people
		`+clause+`
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", label, err)
	}
	defer rows.Close()

	var speakers []*types.Speaker
	speakerByID := map[string]*types.Speaker{}
	for rows.Next() {
		var speaker types.Speaker
		var twitter string
		err := rows.Scan(
			&speaker.ID,
			&speaker.Name,
			&speaker.Email,
			&speaker.Photo,
			&speaker.Phone,
			&speaker.Signal,
			&speaker.Telegram,
			&twitter,
			&speaker.Nostr,
			&speaker.Github,
			&speaker.Instagram,
			&speaker.LinkedIn,
			&speaker.Website,
			&speaker.Company,
			&speaker.OrgLogo,
			&speaker.AvailToHire,
			&speaker.LookingToHire,
			&speaker.TShirt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}
		speaker.Twitter = types.ParseTwitter(twitter)
		speakers = append(speakers, &speaker)
		speakerByID[speaker.ID] = &speaker
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", label, err)
	}
	if err := loadSpeakerRolesPostgres(ctx, speakerByID); err != nil {
		return nil, err
	}
	return speakers, nil
}

func CreateSpeaker(ctx *config.AppContext, in SpeakerInput) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	in = normalizeSpeakerInput(in)
	var id string
	err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO people (
			name, email, norm_photo_path, phone, signal, telegram, twitter_handle,
			nostr, github_url, instagram, linkedin, website_url, company,
			org_logo_path, avail_to_hire, looking_to_hire, tshirt
		)
		VALUES ($1, NULLIF($2, '')::citext, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17)
		RETURNING id::text
	`, in.Name, in.Email, in.Photo, in.Phone, in.Signal, in.Telegram, in.Twitter,
		in.Nostr, in.Github, in.Instagram, in.LinkedIn, in.Website, in.Company,
		in.OrgLogo, in.AvailToHire, in.LookingToHire, in.TShirt).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create person: %w", err)
	}
	return id, nil
}

func UpdateSpeaker(ctx *config.AppContext, speakerID string, up SpeakerUpdate) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	up = normalizeSpeakerUpdate(up)
	_, err := ctx.DB.Exec(context.Background(), `
		UPDATE people
		SET norm_photo_path = CASE WHEN $2 <> '' THEN $2 ELSE norm_photo_path END,
			phone = CASE WHEN $3 <> '' THEN $3 ELSE phone END,
			signal = CASE WHEN $4 <> '' THEN $4 ELSE signal END,
			telegram = CASE WHEN $5 <> '' THEN $5 ELSE telegram END,
			twitter_handle = CASE WHEN $6 <> '' THEN $6 ELSE twitter_handle END,
			nostr = CASE WHEN $7 <> '' THEN $7 ELSE nostr END,
			github_url = CASE WHEN $8 <> '' THEN $8 ELSE github_url END,
			instagram = CASE WHEN $9 <> '' THEN $9 ELSE instagram END,
			linkedin = CASE WHEN $10 <> '' THEN $10 ELSE linkedin END,
			website_url = CASE WHEN $11 <> '' THEN $11 ELSE website_url END,
			company = CASE WHEN $12 <> '' THEN $12 ELSE company END,
			org_logo_path = CASE WHEN $13 <> '' THEN $13 ELSE org_logo_path END,
			tshirt = CASE WHEN $14 <> '' THEN $14 ELSE tshirt END,
			email = CASE WHEN $15 <> '' THEN $15::citext ELSE email END,
			name = CASE WHEN $16 <> '' THEN $16 ELSE name END
		WHERE id = $1::uuid
	`, speakerID, up.Photo, up.Phone, up.Signal, up.Telegram, up.Twitter,
		up.Nostr, up.Github, up.Instagram, up.LinkedIn, up.Website, up.Company,
		up.OrgLogo, up.TShirt, up.Email, up.Name)
	if err != nil {
		return fmt.Errorf("update person %s: %w", speakerID, err)
	}
	return nil
}

func UpdateSpeakerRoles(ctx *config.AppContext, speakerID string, roles []string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	tx, err := ctx.DB.Begin(context.Background())
	if err != nil {
		return fmt.Errorf("begin speaker role update: %w", err)
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(context.Background(), `DELETE FROM people_roles WHERE person_id = $1::uuid`, speakerID); err != nil {
		return fmt.Errorf("delete speaker roles: %w", err)
	}
	for _, role := range roles {
		scope, position, ok := splitRoleScopePosition(role)
		if !ok {
			continue
		}
		if _, err := tx.Exec(context.Background(), `
			INSERT INTO people_roles (person_id, scope, position)
			VALUES ($1::uuid, $2, $3)
			ON CONFLICT DO NOTHING
		`, speakerID, scope, position); err != nil {
			return fmt.Errorf("insert speaker role %q: %w", role, err)
		}
	}
	if err := tx.Commit(context.Background()); err != nil {
		return fmt.Errorf("commit speaker role update: %w", err)
	}
	return nil
}

func splitRoleScopePosition(role string) (string, string, bool) {
	role = strings.TrimSpace(role)
	parts := strings.SplitN(role, "-", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	scope := strings.TrimSpace(parts[0])
	position := strings.TrimSpace(parts[1])
	return scope, position, scope != "" && position != ""
}

func loadSpeakerRolesPostgres(ctx *config.AppContext, speakerByID map[string]*types.Speaker) error {
	ids := make([]string, 0, len(speakerByID))
	for id := range speakerByID {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}

	rows, err := ctx.DB.Query(context.Background(), `
		SELECT person_id::text, scope, position
		FROM people_roles
		WHERE person_id::text = ANY($1::text[])
		ORDER BY scope, position
	`, ids)
	if err != nil {
		return fmt.Errorf("query people roles: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var personID string
		var scope string
		var position string
		if err := rows.Scan(&personID, &scope, &position); err != nil {
			return fmt.Errorf("scan people role: %w", err)
		}
		if speaker := speakerByID[personID]; speaker != nil {
			speaker.Roles = append(speaker.Roles, scope+"-"+position)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate people roles: %w", err)
	}
	return nil
}

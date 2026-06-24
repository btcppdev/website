package getters

import (
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
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

func ListSpeakers(ctx *config.AppContext) ([]*types.Speaker, error) {
	if UsePostgresBackend(ctx) {
		return listSpeakersPostgres(ctx)
	}
	return ListSpeakersNotion(ctx.Notion)
}

func GetSpeakersByEmail(ctx *config.AppContext, email string) ([]*types.Speaker, error) {
	if UsePostgresBackend(ctx) {
		return getSpeakersByEmailPostgres(ctx, email)
	}
	return getSpeakersByEmailNotion(ctx.Notion, email)
}

// SearchSpeakersByNameOrEmail returns up to limit Speakers whose Name or Email
// contains q (case-insensitive substring).
func SearchSpeakersByNameOrEmail(ctx *config.AppContext, q string, limit int) ([]*types.Speaker, error) {
	if UsePostgresBackend(ctx) {
		return searchSpeakersByNameOrEmailPostgres(ctx, q, limit)
	}

	q = strings.TrimSpace(strings.ToLower(q))
	if q == "" {
		return nil, nil
	}
	out := make([]*types.Speaker, 0, limit)
	speakers, err := ListSpeakers(ctx)
	if err != nil {
		return nil, err
	}
	for _, s := range speakers {
		if s == nil {
			continue
		}
		if strings.Contains(strings.ToLower(s.Name), q) ||
			strings.Contains(strings.ToLower(s.Email), q) {
			out = append(out, s)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func CreateSpeaker(ctx *config.AppContext, in SpeakerInput) (string, error) {
	if UsePostgresBackend(ctx) {
		return createSpeakerPostgres(ctx, in)
	}
	return createSpeakerNotion(ctx.Notion, in)
}

func UpdateSpeaker(ctx *config.AppContext, speakerID string, up SpeakerUpdate) error {
	if UsePostgresBackend(ctx) {
		return updateSpeakerPostgres(ctx, speakerID, up)
	}
	return updateSpeakerNotion(ctx.Notion, speakerID, up)
}

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

func ListSpeakersWithRole(ctx *config.AppContext, role string) ([]*types.Speaker, error) {
	if UsePostgresBackend(ctx) {
		return listSpeakersWithRolePostgres(ctx, role)
	}

	role = strings.TrimSpace(role)
	if role == "" {
		return nil, nil
	}
	speakers, err := ListSpeakers(ctx)
	if err != nil {
		return nil, err
	}
	var out []*types.Speaker
	for _, speaker := range speakers {
		if speaker == nil {
			continue
		}
		for _, speakerRole := range speaker.Roles {
			if strings.EqualFold(strings.TrimSpace(speakerRole), role) {
				out = append(out, speaker)
				break
			}
		}
	}
	return out, nil
}

func FetchSpeakerByID(ctx *config.AppContext, speakerID string) (*types.Speaker, error) {
	if UsePostgresBackend(ctx) {
		return fetchSpeakerByIDPostgres(ctx, speakerID)
	}
	return fetchSpeakerByIDNotion(ctx.Notion, speakerID)
}

func UpdateSpeakerRoles(ctx *config.AppContext, speakerID string, roles []string) error {
	if UsePostgresBackend(ctx) {
		return updateSpeakerRolesPostgres(ctx, speakerID, roles)
	}
	return updateSpeakerRolesNotion(ctx.Notion, speakerID, roles)
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

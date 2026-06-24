package getters

import (
	"fmt"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func ListSpeakerConfs(ctx *config.AppContext, speakerMap map[string]*types.Speaker, proposalMap map[string]*types.Proposal) ([]*types.SpeakerConf, error) {
	if UsePostgresBackend(ctx) {
		return listSpeakerConfsPostgres(ctx, speakerMap, proposalMap)
	}
	return ListSpeakerConfsNotion(ctx, speakerMap, proposalMap)
}

func FetchSpeakerConfsForSpeaker(ctx *config.AppContext, speakerID string) []*types.SpeakerConf {
	if speakerID == "" {
		return nil
	}
	speaker, err := FetchSpeakerByID(ctx, speakerID)
	if err != nil || speaker == nil {
		return nil
	}
	rows, err := listSpeakerConfsForSpeakerNotion(ctx, speaker)
	if err != nil {
		return nil
	}
	return rows
}

func GetSpeakerConfByID(ctx *config.AppContext, id string) (*types.SpeakerConf, error) {
	if UsePostgresBackend(ctx) {
		return fetchSpeakerConfWithSpeakerPostgres(ctx, id)
	}
	return fetchSpeakerConfWithSpeakerNotion(ctx, id)
}

// GetSpeakerConfsByEmail looks up Speaker(s) by email and returns every
// SpeakerConf row linked to those speakers, fully resolved.
func GetSpeakerConfsByEmail(ctx *config.AppContext, email string) ([]*types.Speaker, []*types.SpeakerConf, error) {
	if email == "" {
		return nil, nil, nil
	}
	if UsePostgresBackend(ctx) {
		return getSpeakerConfsByEmailPostgres(ctx, email)
	}
	speakers, err := GetSpeakersByEmail(ctx, email)
	if err != nil {
		return nil, nil, fmt.Errorf("speakers by email: %w", err)
	}
	if len(speakers) == 0 {
		return nil, nil, nil
	}

	var allConfs []*types.SpeakerConf
	for _, sp := range speakers {
		rows, err := listSpeakerConfsForSpeakerNotion(ctx, sp)
		if err != nil {
			return nil, nil, fmt.Errorf("speaker confs for speaker %s: %w", sp.ID, err)
		}
		allConfs = append(allConfs, rows...)
	}
	return speakers, allConfs, nil
}

// FetchSpeakerConfWithSpeaker reads a SpeakerConf by ID with its speaker
// relation resolved.
func FetchSpeakerConfWithSpeaker(ctx *config.AppContext, speakerConfID string) (*types.SpeakerConf, error) {
	if UsePostgresBackend(ctx) {
		return fetchSpeakerConfWithSpeakerPostgres(ctx, speakerConfID)
	}
	return fetchSpeakerConfWithSpeakerNotion(ctx, speakerConfID)
}

package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func conferenceAttendeeSponsors(ctx *config.AppContext, conf *types.Conf, attendees []*types.Speaker) map[string][]*types.Org {
	if conf == nil || len(attendees) == 0 {
		return nil
	}
	ids := make([]string, 0, len(attendees))
	for _, attendee := range attendees {
		if attendee != nil && attendee.ID != "" {
			ids = append(ids, attendee.ID)
		}
	}
	sponsors, err := getters.ListConferenceAttendeeSponsors(ctx, conf.Ref, ids)
	if err != nil {
		// An optional badge must not prevent the conference from rendering.
		ctx.Err.Printf("/%s attendee sponsors: %s", conf.Tag, err)
		return nil
	}
	return sponsors
}

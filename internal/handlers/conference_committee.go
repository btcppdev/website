package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"net/url"
)

func conferenceCommittee(ctx *config.AppContext, conf *types.Conf) []*types.ConferenceCommitteeMember {
	if conf == nil {
		return nil
	}
	members, err := getters.ListConferenceCommittee(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s steering committee: %s", conf.Tag, err)
		return nil
	}
	for _, member := range members {
		if slug, ok := resolvedWhoIsPublicID(ctx, member.Speaker); ok {
			member.ProfileURL = "/whois/" + url.PathEscape(slug)
		}
	}
	return members
}

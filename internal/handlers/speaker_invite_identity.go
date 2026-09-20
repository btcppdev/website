package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

func resolveSpeakerInviteRecipient(ctx *config.AppContext, r *http.Request, proposal *types.Proposal) (*types.SpeakerConf, error) {
	personID, signature := r.URL.Query().Get("recipient"), r.URL.Query().Get("signature")
	if personID == "" && signature == "" {
		return nil, nil
	}
	if !helpers.VerifySpeakerInviteRecipient(ctx, proposal.ID, proposal.InviteToken, personID, signature) {
		return nil, fmt.Errorf("invalid recipient signature")
	}
	return findSpeakerInviteRecipient(proposal.SpeakerConfRefs, personID, func(ref string) (*types.SpeakerConf, error) {
		return getters.GetSpeakerConfByID(ctx, ref)
	})
}

func findSpeakerInviteRecipient(refs []string, personID string, load func(string) (*types.SpeakerConf, error)) (*types.SpeakerConf, error) {
	for _, ref := range refs {
		sc, err := load(ref)
		if err != nil {
			return nil, err
		}
		if sc != nil && sc.Speaker != nil && sc.Speaker.ID == personID {
			return sc, nil
		}
	}
	return nil, fmt.Errorf("recipient is not attached to this proposal")
}

func validateSpeakerInviteIdentity(invitee *types.SpeakerConf, existing *types.Speaker, email string) error {
	if invitee == nil {
		if existing != nil {
			return fmt.Errorf("This email already belongs to an account. Ask the organizer for a new invitation addressed specifically to you.")
		}
		return nil
	}
	if invitee.Speaker == nil || existing == nil || existing.ID != invitee.Speaker.ID || !strings.EqualFold(strings.TrimSpace(email), strings.TrimSpace(invitee.Speaker.Email)) {
		return fmt.Errorf("This invitation belongs to a different email address. Ask the organizer for an invitation addressed to you.")
	}
	return nil
}

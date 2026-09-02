package emails

import (
	"fmt"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// SendSponsorManagerInvitation sends the verified-email login link used to
// join an organization's sponsor workspace. The login destination is the
// one-time organization invitation; new users complete account setup first.
func SendSponsorManagerInvitation(ctx *config.AppContext, conf *types.Conf, invite *types.OrganizationMemberInvite, inviteeName, loginURL string) error {
	if conf == nil || invite == nil {
		return fmt.Errorf("conference and sponsor invitation are required")
	}
	inviteeName = strings.TrimSpace(inviteeName)
	greeting := "Hello"
	if inviteeName != "" {
		greeting = "Hi " + inviteeName
	}
	body := fmt.Sprintf("# Sponsor dashboard invitation\n\n%s,\n\nYou've been invited to manage **%s**'s sponsorship for **%s**.\n\n[Set up sponsor access](button#%s)\n\nIf you don't have a bitcoin++ account yet, we'll ask you to set up your profile before taking you to the sponsor dashboard. This secure link expires in 72 hours and is intended for %s.",
		greeting, invite.OrganizationName, conf.Desc, loginURL, invite.Email)
	return SendHackathonMessage(ctx, "sponsor-manager-invite-"+invite.ID, invite.Email,
		"["+conf.Tag+"] Sponsor dashboard invitation", body)
}

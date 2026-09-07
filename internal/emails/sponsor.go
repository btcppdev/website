package emails

import (
	"crypto/sha256"
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
	body := fmt.Sprintf("# Sponsor dashboard invitation\n\n%s,\n\nYou've been invited to manage **%s**'s sponsor workspace, including its sponsorship for **%s** and future bitcoin++ events.\n\n[Set up sponsor access](button#%s)\n\nIf you don't have a bitcoin++ account yet, we'll ask you to set up your profile before taking you to the sponsor dashboard. This secure link expires in 72 hours and is intended for %s.",
		greeting, invite.OrganizationName, conf.Desc, loginURL, invite.Email)
	return SendHackathonMessage(ctx, "sponsor-manager-invite-"+invite.ID, invite.Email,
		"["+conf.Tag+"] Sponsor dashboard invitation", body)
}

func SendOrganizationApplicationAdminNotice(ctx *config.AppContext, application *types.OrganizationApplication, adminEmail, reviewURL string) error {
	if application == nil || strings.TrimSpace(adminEmail) == "" {
		return fmt.Errorf("organization application and administrator email are required")
	}
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(adminEmail))))
	body := fmt.Sprintf("# New organization application\n\n**%s** submitted an application to create **%s** on bitcoin++.\n\n[Review, edit, approve, or deny](button#%s)\n\nThe application remains pending until a global administrator makes a decision.", application.ApplicantEmail, application.Name, reviewURL)
	return SendHackathonMessage(ctx, fmt.Sprintf("organization-application-%s-admin-%x", application.ID, digest[:6]), adminEmail,
		"[bitcoin++] Organization application: "+application.Name, body)
}

func SendOrganizationApplicationDecision(ctx *config.AppContext, application *types.OrganizationApplication, destinationURL string) error {
	if application == nil || strings.TrimSpace(application.ApplicantEmail) == "" {
		return fmt.Errorf("organization application and applicant email are required")
	}
	decision := strings.ToLower(strings.TrimSpace(application.Status))
	heading := "Organization application update"
	detail := "Your application for **" + application.Name + "** was denied."
	action := "[View your organizations](button#" + destinationURL + ")"
	if decision == "approved" {
		heading = "Your organization was approved"
		detail = "Your application for **" + application.Name + "** was approved. You are now its owner and can manage its profile and membership settings."
		action = "[Manage " + application.Name + "](button#" + destinationURL + ")"
	}
	if strings.TrimSpace(application.ReviewNote) != "" {
		detail += "\n\n**Administrator note:** " + application.ReviewNote
	}
	body := "# " + heading + "\n\n" + detail + "\n\n" + action
	return SendHackathonMessage(ctx, "organization-application-"+application.ID+"-"+decision,
		application.ApplicantEmail, "[bitcoin++] "+heading, body)
}

// SendOrganizationMemberInvitation sends a general organization invitation
// that is not tied to a particular sponsorship or conference.
func SendOrganizationMemberInvitation(ctx *config.AppContext, invite *types.OrganizationMemberInvite, loginURL string) error {
	if invite == nil || strings.TrimSpace(invite.OrganizationName) == "" {
		return fmt.Errorf("organization invitation is required")
	}
	role := strings.TrimSpace(invite.Role)
	if role == "" {
		role = "member"
	}
	body := fmt.Sprintf("# You're invited to join %s\n\nYou've been invited to join **%s** as a **%s** on bitcoin++.\n\n[Accept organization invitation](button#%s)\n\nSign in with %s to accept. This secure invitation expires in 72 hours.",
		invite.OrganizationName, invite.OrganizationName, role, loginURL, invite.Email)
	return SendHackathonMessage(ctx, "organization-member-invite-"+invite.ID, invite.Email,
		"[bitcoin++] Invitation to join "+invite.OrganizationName, body)
}

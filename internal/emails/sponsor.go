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

func SendOrganizationApplicationReceipt(ctx *config.AppContext, application *types.OrganizationApplication, dashboardURL string) error {
	if application == nil || strings.TrimSpace(application.ApplicantEmail) == "" {
		return fmt.Errorf("organization application and applicant email are required")
	}
	body := fmt.Sprintf("# We received your organization application\n\nYour application to create **%s** on bitcoin++ is ready for administrator review. We'll email you again when a decision is made.\n\n[View your organization applications](button#%s)", application.Name, dashboardURL)
	return SendHackathonMessage(ctx, "organization-application-"+application.ID+"-receipt",
		application.ApplicantEmail, "[bitcoin++] Organization application received", body)
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

func SendOrganizationMembershipRequestReceipt(ctx *config.AppContext, request *types.OrganizationMembershipRequest, dashboardURL string, autoApproved bool) error {
	if request == nil || strings.TrimSpace(request.PersonEmail) == "" {
		return fmt.Errorf("organization membership request and applicant email are required")
	}
	heading := "Membership request received"
	detail := fmt.Sprintf("Your request to join **%s** was sent to its organization managers. We'll email you when they make a decision.", request.OrganizationName)
	keyStatus := "pending"
	if autoApproved {
		heading = "You joined " + request.OrganizationName
		detail = fmt.Sprintf("**%s** allows open membership, so your request was approved automatically and you are now a member.", request.OrganizationName)
		keyStatus = "auto-approved"
	}
	body := fmt.Sprintf("# %s\n\n%s\n\n[View your organizations](button#%s)", heading, detail, dashboardURL)
	return SendHackathonMessage(ctx, "organization-membership-request-"+request.ID+"-"+keyStatus+"-applicant",
		request.PersonEmail, "[bitcoin++] "+heading, body)
}

func SendOrganizationMembershipRequestManagerNotice(ctx *config.AppContext, request *types.OrganizationMembershipRequest, recipient *types.NotificationRecipient, reviewURL string, autoApproved bool) error {
	if request == nil || recipient == nil || strings.TrimSpace(recipient.Email) == "" {
		return fmt.Errorf("membership request and manager recipient are required")
	}
	heading := "New membership request for " + request.OrganizationName
	detail := fmt.Sprintf("**%s** (%s) requested to join **%s**.", request.PersonName, request.PersonEmail, request.OrganizationName)
	action := "[Review membership request](button#" + reviewURL + ")"
	keyStatus := "pending"
	if autoApproved {
		heading = "A new member joined " + request.OrganizationName
		detail = fmt.Sprintf("**%s** (%s) joined **%s** through its open-membership policy.", request.PersonName, request.PersonEmail, request.OrganizationName)
		action = "[Manage organization members](button#" + reviewURL + ")"
		keyStatus = "auto-approved"
	} else if strings.TrimSpace(request.Message) != "" {
		detail += "\n\n**Applicant message:** " + request.Message
	}
	body := "# " + heading + "\n\n" + detail + "\n\n" + action
	return SendHackathonMessage(ctx, fmt.Sprintf("organization-membership-request-%s-%s-manager-%s", request.ID, keyStatus, recipientDigest(recipient.Email)),
		recipient.Email, "[bitcoin++] "+heading, body)
}

func SendOrganizationMembershipDecision(ctx *config.AppContext, request *types.OrganizationMembershipRequest, dashboardURL string) error {
	if request == nil || strings.TrimSpace(request.PersonEmail) == "" {
		return fmt.Errorf("organization membership request and applicant email are required")
	}
	decision := strings.ToLower(strings.TrimSpace(request.Status))
	heading := "Membership request update"
	detail := fmt.Sprintf("Your request to join **%s** was denied.", request.OrganizationName)
	if decision == "approved" {
		heading = "You joined " + request.OrganizationName
		detail = fmt.Sprintf("Your request to join **%s** was approved. You are now a member.", request.OrganizationName)
	}
	if strings.TrimSpace(request.ReviewNote) != "" {
		detail += "\n\n**Organization manager note:** " + request.ReviewNote
	}
	body := fmt.Sprintf("# %s\n\n%s\n\n[View your organizations](button#%s)", heading, detail, dashboardURL)
	return SendHackathonMessage(ctx, "organization-membership-request-"+request.ID+"-"+decision+"-applicant",
		request.PersonEmail, "[bitcoin++] "+heading, body)
}

func SendSponsorAwardProposalAdminNotice(ctx *config.AppContext, proposal *types.SponsorAwardProposal, recipientEmail, reviewURL string) error {
	if proposal == nil || strings.TrimSpace(recipientEmail) == "" {
		return fmt.Errorf("sponsor prize proposal and administrator email are required")
	}
	body := fmt.Sprintf("# New sponsor prize proposal\n\n**%s** proposed **%s** for **%s**.\n\n[Review sponsor prize proposal](button#%s)", proposal.OrganizationName, proposal.Title, proposal.CompetitionTitle, reviewURL)
	return SendHackathonMessage(ctx, fmt.Sprintf("sponsor-award-proposal-%s-admin-%s", proposal.ID, recipientDigest(recipientEmail)),
		recipientEmail, "[bitcoin++] Sponsor prize proposal: "+proposal.Title, body)
}

func SendSponsorAwardProposalDecision(ctx *config.AppContext, proposal *types.SponsorAwardProposal, recipient *types.NotificationRecipient, dashboardURL string) error {
	if proposal == nil || recipient == nil || strings.TrimSpace(recipient.Email) == "" {
		return fmt.Errorf("sponsor prize proposal and manager recipient are required")
	}
	decision := strings.ToLower(strings.TrimSpace(proposal.Status))
	heading := "Sponsor prize proposal rejected"
	detail := fmt.Sprintf("The proposal **%s** for **%s** was rejected.", proposal.Title, proposal.CompetitionTitle)
	if decision == "approved" {
		heading = "Sponsor prize proposal approved"
		detail = fmt.Sprintf("The proposal **%s** for **%s** was approved and added to the hackathon.", proposal.Title, proposal.CompetitionTitle)
	}
	if strings.TrimSpace(proposal.ReviewNotes) != "" {
		detail += "\n\n**Organizer note:** " + proposal.ReviewNotes
	}
	body := fmt.Sprintf("# %s\n\n%s\n\n[View sponsor prizes](button#%s)", heading, detail, dashboardURL)
	return SendHackathonMessage(ctx, fmt.Sprintf("sponsor-award-proposal-%s-%s-manager-%s", proposal.ID, decision, recipientDigest(recipient.Email)),
		recipient.Email, "[bitcoin++] "+heading, body)
}

func SendSponsorResultsNotice(ctx *config.AppContext, conf *types.Conf, competition *types.HackathonCompetition, recipient *types.SponsorOrganizationRecipient, publicURL, projectsURL string) error {
	if conf == nil || competition == nil || recipient == nil || strings.TrimSpace(recipient.Email) == "" {
		return fmt.Errorf("conference, competition, and sponsor manager recipient are required")
	}
	body := fmt.Sprintf("# Hackathon results are ready\n\nThe results for **%s** at **%s** have been finalized. As a manager of **%s**, you can now review the results and sponsor project details.\n\n[View public results](button#%s)\n\n[View sponsor projects](button#%s)", competition.Title, conf.Desc, recipient.OrganizationName, publicURL, projectsURL)
	return SendHackathonMessage(ctx, fmt.Sprintf("hackathon-results-%s-org-%s-manager-%s", competition.ID, recipient.OrganizationID, recipientDigest(recipient.Email)),
		recipient.Email, "["+conf.Tag+"] Hackathon results are ready", body)
}

func recipientDigest(email string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return fmt.Sprintf("%x", digest[:6])
}

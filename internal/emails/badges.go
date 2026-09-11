package emails

import (
	"fmt"
	"net/url"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// NotifyBadgeGrantCreated tells the recipient that an organization has
// granted them a badge. A grant that is waiting for a Nostr key links directly
// to account settings; a ready grant links to the recipient's badge inbox.
func NotifyBadgeGrantCreated(ctx *config.AppContext, grant *types.OrganizationBadgeGrant) []error {
	if grant == nil {
		return []error{fmt.Errorf("badge grant is required")}
	}
	email, err := verifiedBadgeRecipientEmail(ctx, grant.RecipientPersonID)
	if err != nil {
		return []error{err}
	}
	if email == "" {
		return nil
	}
	base := strings.TrimRight(ctx.Env.GetURI(), "/")
	heading := grant.OrganizationName + " granted you a badge"
	detail := fmt.Sprintf("**%s** granted you the **%s** badge. It is already recorded on your public Bitcoin++ profile.", grant.OrganizationName, grant.BadgeName)
	actionLabel := "Open your badge inbox"
	actionURL := base + "/dashboard/orgs#badge-inbox"
	if grant.State == getters.BadgeGrantStateGranted {
		detail += " To issue it as a portable Nostr badge, first add and verify your Nostr key. The grant will stay safely pending until then."
		actionLabel = "Add your Nostr key"
		actionURL = base + "/dashboard/settings?resume=nostr-link"
	} else {
		detail += " Your verified Nostr key is ready; the organization can now issue the portable credential."
	}
	body := fmt.Sprintf("# %s\n\nHi %s,\n\n%s\n\n[%s](button#%s)\n\nYour Bitcoin++ profile remains the permanent subject of this credential even if your Nostr key changes later.", heading, badgeRecipientName(grant), detail, actionLabel, actionURL)
	if err := SendHackathonMessage(ctx, "organization-badge-grant-"+grant.ID+"-recipient-created", email, "[bitcoin++] "+heading, body); err != nil {
		return []error{err}
	}
	return nil
}

// NotifyBadgeGrantReady tells every active owner and manager that a previously
// pending recipient has verified a Nostr key and the grant can be signed.
func NotifyBadgeGrantReady(ctx *config.AppContext, grant *types.OrganizationBadgeGrant) []error {
	if grant == nil {
		return []error{fmt.Errorf("badge grant is required")}
	}
	return notifyBadgeManagers(ctx, grant, "ready-"+grant.RecipientPubkey,
		grant.BadgeName+" is ready to issue",
		fmt.Sprintf("**%s** added a verified Nostr key. The **%s** grant from **%s** is now ready for an organization manager to sign.", badgeRecipientName(grant), grant.BadgeName, grant.OrganizationName),
		"Review and issue in Badge Studio", badgeGrantStudioURL(ctx, grant))
}

// NotifyBadgeGrantIssued tells the recipient that the portable Nostr award now
// exists and can be accepted into their profile badge list.
func NotifyBadgeGrantIssued(ctx *config.AppContext, grant *types.OrganizationBadgeGrant) []error {
	if grant == nil {
		return []error{fmt.Errorf("badge grant is required")}
	}
	email, err := verifiedBadgeRecipientEmail(ctx, grant.RecipientPersonID)
	if err != nil {
		return []error{err}
	}
	if email == "" {
		return nil
	}
	heading := grant.BadgeName + " was issued to you"
	body := fmt.Sprintf("# %s\n\nHi %s,\n\n**%s** signed and published your **%s** Nostr badge. Accept it to add the award to your public Nostr profile badge list.\n\n[Review your badge](button#%s)", heading, badgeRecipientName(grant), grant.OrganizationName, grant.BadgeName, grant.SubjectProfileURL)
	if err := SendHackathonMessage(ctx, "organization-badge-grant-"+grant.ID+"-recipient-issued", email, "[bitcoin++] "+heading, body); err != nil {
		return []error{err}
	}
	return nil
}

// NotifyBadgeGrantAccepted closes the loop for the organization managers who
// granted and issued the credential.
func NotifyBadgeGrantAccepted(ctx *config.AppContext, grant *types.OrganizationBadgeGrant) []error {
	if grant == nil {
		return []error{fmt.Errorf("badge grant is required")}
	}
	return notifyBadgeManagers(ctx, grant, "accepted",
		badgeRecipientName(grant)+" accepted "+grant.BadgeName,
		fmt.Sprintf("**%s** accepted the **%s** badge from **%s** into their public Nostr profile.", badgeRecipientName(grant), grant.BadgeName, grant.OrganizationName),
		"View the public profile", grant.SubjectProfileURL)
}

// NotifyBadgeGrantRevoked tells the recipient when an issued credential is no
// longer valid. The public Bitcoin++ profile remains the canonical status page.
func NotifyBadgeGrantRevoked(ctx *config.AppContext, grant *types.OrganizationBadgeGrant) []error {
	if grant == nil {
		return []error{fmt.Errorf("badge grant is required")}
	}
	email, err := verifiedBadgeRecipientEmail(ctx, grant.RecipientPersonID)
	if err != nil {
		return []error{err}
	}
	if email == "" {
		return nil
	}
	heading := grant.BadgeName + " was revoked"
	detail := fmt.Sprintf("**%s** revoked your **%s** badge.", grant.OrganizationName, grant.BadgeName)
	if reason := strings.TrimSpace(grant.RevocationReason); reason != "" {
		detail += "\n\n**Reason:** " + reason
	}
	body := fmt.Sprintf("# %s\n\nHi %s,\n\n%s\n\n[View credential status](button#%s)", heading, badgeRecipientName(grant), detail, grant.SubjectProfileURL)
	if err := SendHackathonMessage(ctx, "organization-badge-grant-"+grant.ID+"-recipient-revoked", email, "[bitcoin++] "+heading, body); err != nil {
		return []error{err}
	}
	return nil
}

func notifyBadgeManagers(ctx *config.AppContext, grant *types.OrganizationBadgeGrant, jobSuffix, heading, detail, actionLabel, actionURL string) []error {
	recipients, err := getters.ListOrganizationManagerRecipients(ctx, grant.OrganizationID)
	if err != nil {
		return []error{err}
	}
	var errs []error
	for _, recipient := range recipients {
		if recipient == nil || strings.TrimSpace(recipient.Email) == "" {
			continue
		}
		body := fmt.Sprintf("# %s\n\nHi %s,\n\n%s\n\n[%s](button#%s)", heading, badgeManagerName(recipient), detail, actionLabel, actionURL)
		jobKey := fmt.Sprintf("organization-badge-grant-%s-manager-%s-%s", grant.ID, jobSuffix, recipientDigest(recipient.Email))
		if err := SendHackathonMessage(ctx, jobKey, recipient.Email, "[bitcoin++] "+heading, body); err != nil {
			errs = append(errs, fmt.Errorf("notify %s: %w", recipient.Email, err))
		}
	}
	return errs
}

func verifiedBadgeRecipientEmail(ctx *config.AppContext, personID string) (string, error) {
	addresses, err := getters.ListPersonEmails(ctx, personID)
	if err != nil {
		return "", err
	}
	for _, address := range addresses {
		if address != nil && !address.VerifiedAt.IsZero() && strings.TrimSpace(address.Email) != "" {
			return strings.TrimSpace(address.Email), nil
		}
	}
	return "", nil
}

func badgeGrantStudioURL(ctx *config.AppContext, grant *types.OrganizationBadgeGrant) string {
	studio := strings.TrimRight(ctx.Env.BadgeStudioURL, "/")
	if studio == "" {
		return strings.TrimRight(ctx.Env.GetURI(), "/") + "/dashboard/orgs"
	}
	returnTo := "/?btcpp_org=" + url.QueryEscape(grant.OrganizationID) + "&grant=" + url.QueryEscape(grant.ID)
	return studio + "/api/auth/btcpp/continue?return_to=" + url.QueryEscape(returnTo)
}

func badgeRecipientName(grant *types.OrganizationBadgeGrant) string {
	if name := strings.TrimSpace(grant.RecipientName); name != "" {
		return name
	}
	return "there"
}

func badgeManagerName(recipient *types.NotificationRecipient) string {
	if name := strings.TrimSpace(recipient.Name); name != "" {
		return name
	}
	return "there"
}

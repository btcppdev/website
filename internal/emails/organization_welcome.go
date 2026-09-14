package emails

import (
	"bytes"
	"fmt"
	"html"
	htmltemplate "html/template"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// BuildOrganizationWelcomeMail confirms access that has already been granted.
// It shares the newsletter layout without newsletter subscription controls.
func BuildOrganizationWelcomeMail(ctx *config.AppContext, membership *types.OrganizationMembership, name, email, organizationURL, heroURL string) (*Mail, error) {
	if membership == nil || membership.Organization == nil || strings.TrimSpace(email) == "" {
		return nil, fmt.Errorf("organization membership and recipient email are required")
	}
	detail := ""
	switch membership.Role {
	case "member":
		detail = "You're now part of the team. Visit the organization page to see its profile and meet your fellow members."
	case "manager":
		detail = "You can update the organization's profile and help manage its members from your organization dashboard."
	case "owner":
		detail = "You can manage the organization's profile, membership settings, and member roles from your organization dashboard."
	default:
		return nil, fmt.Errorf("unsupported organization role %q", membership.Role)
	}
	greeting := "Hello"
	if strings.TrimSpace(name) != "" {
		greeting = "Hi " + strings.TrimSpace(name)
	}
	title := "Welcome to " + membership.Organization.Name
	confirmation := "You've been added to " + membership.Organization.Name + " as a " + membership.Role + " on bitcoin++."
	body := rebrandLead("YOUR ORGANIZATION", title, "Good things start with good company.") +
		"<p>" + html.EscapeString(greeting) + ",</p><p>" + html.EscapeString(confirmation) + "</p><p>" + html.EscapeString(detail) + "</p>" +
		rebrandButton("View organization", organizationURL) +
		"<p>Your access is ready. Sign in with " + html.EscapeString(email) + " to get started.</p>"
	var htmlBody bytes.Buffer
	err := ctx.TemplateCache.ExecuteTemplate(&htmlBody, "emails/rebrand.tmpl", &templatedNewsletterEmail{
		Content: htmltemplate.HTML(body),
		URI:     ctx.Env.GetURI(),
		Config:  templatedNewsletterConfig{Template: "roundup", Palette: "signal", Issue: "ORGANIZATIONS", Date: formatTemplatedNewsletterDate(time.Now()), Hero: heroURL},
		Styles:  rebrandEmailCSS("signal"),
	})
	if err != nil {
		return nil, err
	}
	// Use the immutable membership identity for both direct adds and launch retries.
	return &Mail{
		JobKey: fmt.Sprintf("organization-welcome-%s-%s-%d", membership.OrganizationID, membership.PersonID, membership.CreatedAt.UnixNano()),
		Email:  email, Title: "[bitcoin++] " + title, HTMLBody: htmlBody.Bytes(),
		TextBody: []byte(fmt.Sprintf("%s\n\n%s,\n\n%s\n\n%s\n\nView organization: %s\n\nYour access is ready. Sign in with %s to get started.", title, greeting, confirmation, detail, organizationURL, email)),
		SendAt:   time.Now(),
	}, nil
}

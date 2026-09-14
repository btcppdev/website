package emails

import (
	"bytes"
	"fmt"
	"html"
	htmltemplate "html/template"
	"net/url"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// BuildOrganizationWelcomeMail confirms access that has already been granted.
// It shares the newsletter layout without newsletter subscription controls.
func BuildOrganizationWelcomeMail(ctx *config.AppContext, membership *types.OrganizationMembership, name, email, organizationURL, heroURL, addedByName string) (*Mail, error) {
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
	article := "a"
	if membership.Role == "owner" {
		article = "an"
	}
	title := "You're now " + article + " " + membership.Role + " of the " + membership.Organization.Name + " org at bitcoin++"
	confirmation := "You've been added to " + membership.Organization.Name + " as " + article + " " + membership.Role + " on bitcoin++."
	if strings.TrimSpace(addedByName) != "" {
		confirmation = strings.TrimSpace(addedByName) + " added you to " + membership.Organization.Name + " as " + article + " " + membership.Role + "."
	}
	logo := ""
	logoURL := strings.TrimSpace(membership.Organization.LogoLight)
	if logoURL == "" {
		logoURL = strings.TrimSpace(membership.Organization.LogoDark)
	}
	if logoURL != "" {
		base, _ := url.Parse(ctx.Env.GetURI())
		parsed, err := url.Parse(logoURL)
		if err == nil && base != nil {
			parsed = base.ResolveReference(parsed)
			if parsed.Scheme == "https" || parsed.Scheme == "http" {
				logo = `<div style="margin-bottom:24px;"><img src="` + html.EscapeString(parsed.String()) + `" alt="` + html.EscapeString(membership.Organization.Name) + ` logo" width="160" style="display:block;width:160px;max-width:100%;height:auto;max-height:80px;object-fit:contain;object-position:left;"></div>`
			}
		}
	}
	body := logo + rebrandLead("YOUR ORGANIZATION", title, "Good things start with good company.") +
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

package emails

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func nextOrganizationWelcomeConference(confs []*types.Conf, now time.Time) *types.Conf {
	var next, latest *types.Conf
	for _, conf := range confs {
		if conf == nil || !conf.IsPublished() || conf.StartDate.IsZero() {
			continue
		}
		if !conf.StartDate.After(now) {
			if latest == nil || conf.StartDate.After(latest.StartDate) {
				latest = conf
			}
			continue
		}
		if next == nil || conf.StartDate.Before(next.StartDate) {
			next = conf
		}
	}
	if next == nil {
		return latest
	}
	return next
}

// SendOrganizationWelcomeEmail reloads active membership and recipient details
// so direct additions and the launch backfill share rendering and delivery keys.
func SendOrganizationWelcomeEmail(ctx *config.AppContext, organizationID, personID, name string) error {
	membership, err := getters.GetOrganizationMembership(ctx, personID, organizationID)
	if err != nil {
		return err
	}
	email, err := getters.GetPrimaryPersonEmail(ctx, personID)
	if err != nil {
		return err
	}
	if membership == nil || membership.Organization == nil || email == "" {
		return fmt.Errorf("organization membership or recipient email is missing")
	}
	baseURI := strings.TrimRight(ctx.Env.GetURI(), "/")
	heroURL := ""
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		ctx.Err.Printf("organization welcome conference image: %s", err)
	} else if conf := nextOrganizationWelcomeConference(confs, time.Now()); conf != nil {
		heroURL = baseURI + "/static/img/" + url.PathEscape(conf.Tag) + "/leading.png"
	}
	organizationURL := baseURI + "/organizations/" + url.PathEscape(organizationWelcomePathRef(membership.Organization))
	mail, err := BuildOrganizationWelcomeMail(ctx, membership, name, email, organizationURL, heroURL)
	if err != nil {
		return err
	}
	return ComposeAndSendMail(ctx, mail)
}

func organizationWelcomePathRef(org *types.Org) string {
	if slug := strings.TrimSpace(org.Slug); slug != "" {
		return slug
	}
	return strings.TrimSpace(org.Ref)
}

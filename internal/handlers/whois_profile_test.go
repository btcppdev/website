package handlers

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestProjectOnlyWhoIsProfileShowsEventBadges(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}

	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	templates, err := ctx.TemplateCache.Clone()
	if err != nil {
		t.Fatalf("clone templates: %v", err)
	}
	if _, err := templates.Parse(`{{ define "mainnav" }}<nav></nav>{{ end }}`); err != nil {
		t.Fatalf("replace test nav: %v", err)
	}

	edition := &types.Conf{
		Ref:       "conference-id",
		Tag:       "toronto",
		Desc:      "bitcoin++ Toronto",
		StartDate: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
	}
	var output bytes.Buffer
	if err := templates.ExecuteTemplate(&output, "whois_profile.tmpl", &WhoIsProfilePage{
		Person: &WhoIsPerson{
			PublicID: "project-builder",
			Speaker:  &types.Speaker{ID: "person-id", Name: "Project Builder"},
			Projects: []*WhoIsProject{{
				Project: &types.HackathonProject{ID: "project-id", Title: "Builder Project"},
				Conf:    edition,
				URL:     "/toronto/hackathon/projects/project-id",
			}},
			Editions: []*types.Conf{edition},
		},
	}); err != nil {
		t.Fatalf("render project-only WhoIs profile: %v", err)
	}

	html := output.String()
	if !strings.Contains(html, `EVENT BADGES`) || !strings.Contains(html, `href="/toronto"`) {
		t.Fatalf("project-only WhoIs profile is missing event badges: %s", html)
	}
	if strings.Contains(html, `TALKS & PANELS`) {
		t.Fatalf("project-only WhoIs profile unexpectedly renders the talks section: %s", html)
	}
	if strings.Contains(html, `archive-rain`) {
		t.Fatalf("individual WhoIs profile unexpectedly renders archive rain: %s", html)
	}
	eventBadgesIndex := strings.Index(html, `§01 · EVENT BADGES`)
	hackathonProjectsIndex := strings.Index(html, `§02 · HACKATHON PROJECTS`)
	if eventBadgesIndex == -1 || hackathonProjectsIndex == -1 || eventBadgesIndex > hackathonProjectsIndex {
		t.Fatalf("WhoIs profile sections are out of order: event badges index %d, hackathon projects index %d", eventBadgesIndex, hackathonProjectsIndex)
	}
}

func TestWhoIsBadgeGrantsHideRevokedAndDedupeStudioAwards(t *testing.T) {
	profile := &WhoIsBadgeProfile{Issued: []WhoIsIssuedBadge{{}}}
	profile.Issued[0].Award.EventID = strings.Repeat("a", 64)
	grants := []*types.OrganizationBadgeGrant{
		{ID: "represented", State: getters.BadgeGrantStateAccepted, AwardEventID: strings.Repeat("a", 64), RecipientPubkey: strings.Repeat("1", 64)},
		{ID: "issued", State: getters.BadgeGrantStateIssued, AwardEventID: strings.Repeat("b", 64), RecipientPubkey: strings.Repeat("2", 64)},
		{ID: "pending", State: getters.BadgeGrantStateGranted},
		{ID: "revoked", State: getters.BadgeGrantStateRevoked, AwardEventID: strings.Repeat("c", 64), RecipientPubkey: strings.Repeat("3", 64)},
		{ID: "canceled", State: getters.BadgeGrantStateCanceled},
	}
	visible := whoIsBadgeGrants(grants, profile)
	if len(visible) != 2 || visible[0].ID != "issued" || visible[1].ID != "pending" {
		t.Fatalf("unexpected visible grants: %+v", visible)
	}
}

func TestWhoIsProfileBadgesShowcaseAtBottomWithIssuer(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}

	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	templates, err := ctx.TemplateCache.Clone()
	if err != nil {
		t.Fatalf("clone templates: %v", err)
	}
	if _, err := templates.Parse(`{{ define "mainnav" }}<nav></nav>{{ end }}`); err != nil {
		t.Fatalf("replace test nav: %v", err)
	}

	profile := &WhoIsBadgeProfile{
		Issued: []WhoIsIssuedBadge{
			{Definition: WhoIsBadgeDefinition{IssuerPubkey: strings.Repeat("1", 64), Name: "Community mentor", Description: "Shares knowledge.", ImageURL: "https://cdn.example/mentor.png"}},
			{Definition: WhoIsBadgeDefinition{Name: "Revoked studio badge", Description: "Must not appear.", ImageURL: "https://cdn.example/revoked.png"}},
		},
		Pending: []WhoIsPendingBadge{{Badge: &WhoIsBadgeDefinition{Name: "Future builder", Description: "Builds what comes next.", ImageURL: "https://cdn.example/builder.png"}}},
	}
	profile.Issued[0].Award.EventID = strings.Repeat("a", 64)
	profile.Issued[1].Award.Revocation = &struct {
		CreatedAt time.Time `json:"created_at"`
		Reason    string    `json:"reason"`
	}{Reason: "Issued in error"}
	badgeGrants := []*types.OrganizationBadgeGrant{
		{ID: "represented", AwardEventID: strings.Repeat("a", 64), IssuerPubkey: strings.Repeat("1", 64), OrganizationID: "org-id", OrganizationSlug: "signet-systems", OrganizationName: "Signet Systems", OrganizationLogoURL: "https://cdn.example/signet-logo.png", State: getters.BadgeGrantStateAccepted},
		{ID: "grant", BadgeName: "Open-source host", BadgeDescription: "Makes everyone welcome.", BadgeImageURL: "https://cdn.example/host.png", OrganizationID: "guild-id", OrganizationSlug: "open-source-guild", OrganizationName: "Open-source Guild", State: getters.BadgeGrantStateGranted},
		{ID: "revoked", BadgeName: "Revoked database badge", BadgeDescription: "Must not appear.", BadgeImageURL: "https://cdn.example/revoked-grant.png", State: getters.BadgeGrantStateRevoked},
	}
	attachWhoIsBadgeIssuers(profile, badgeGrants)
	grants := whoIsBadgeGrants(badgeGrants, profile)
	collection := buildWhoIsBadgeCollection(publicWhoIsBadgeProfile(profile), grants, nil)

	var output bytes.Buffer
	if err := templates.ExecuteTemplate(&output, "whois_profile.tmpl", &WhoIsProfilePage{
		Person:          &WhoIsPerson{PublicID: "profile", Speaker: &types.Speaker{ID: "person-id", Name: "Profile Person"}},
		BadgeCollection: collection,
	}); err != nil {
		t.Fatalf("render WhoIs badge profile: %v", err)
	}

	html := output.String()
	for _, visible := range []string{
		"Badges earned.", "issued by",
		`alt="Artwork for Community mentor"`, "Community mentor", "Shares knowledge.",
		`href="/organizations/signet-systems"`, `src="https://cdn.example/signet-logo.png"`, "Signet Systems",
		`alt="Artwork for Future builder"`, "Future builder", "Builds what comes next.",
		`alt="Artwork for Open-source host"`, "Open-source host", "Makes everyone welcome.", "Open-source Guild",
	} {
		if !strings.Contains(html, visible) {
			t.Fatalf("WhoIs badge profile is missing %q: %s", visible, html)
		}
	}
	for _, private := range []string{
		"Revoked studio badge", "Revoked database badge",
		"accepted on nostr", "awarded · acceptance pending", "grant pending", "ready to issue",
		"delivery needs attention", "View credential", "Accept on Nostr",
		"whois-credential-status", "whois-credential-actions", "is-revoked", "is-pending",
	} {
		if strings.Contains(html, private) {
			t.Fatalf("WhoIs badge profile exposes lifecycle detail %q: %s", private, html)
		}
	}
	if len(profile.Issued) != 2 {
		t.Fatalf("public profile filtering mutated shared Badge Studio data: %+v", profile.Issued)
	}
	if eventIndex, badgeIndex := strings.Index(html, "§01 · EVENT BADGES"), strings.Index(html, "Badges earned."); eventIndex == -1 || badgeIndex == -1 || badgeIndex < eventIndex {
		t.Fatalf("badge showcase is not the final profile section: event index %d, badge index %d", eventIndex, badgeIndex)
	}
}

func TestBuildWhoIsBadgeCollectionFeaturesSixAndGroupsAllVisibleByIssuer(t *testing.T) {
	grants := make([]*WhoIsBadgeGrant, 0, 8)
	for index := 1; index <= 8; index++ {
		issuerName := "Signet Systems"
		issuerURL := "/organizations/signet-systems"
		if index > 4 {
			issuerName = "Open-source Guild"
			issuerURL = "/organizations/open-source-guild"
		}
		grants = append(grants, &WhoIsBadgeGrant{
			OrganizationBadgeGrant: &types.OrganizationBadgeGrant{ID: fmt.Sprintf("grant-%d", index), BadgeName: fmt.Sprintf("Badge %d", index), BadgeImageURL: fmt.Sprintf("https://cdn.example/%d.png", index)},
			Issuer:                 WhoIsBadgeIssuer{Name: issuerName, ProfileURL: issuerURL},
		})
	}
	presentations := []*types.PersonBadgePresentation{
		{BadgeRef: "btcpp-grant:grant-8", FeaturedPosition: 1},
		{BadgeRef: "btcpp-grant:grant-7", FeaturedPosition: 2},
		{BadgeRef: "btcpp-grant:grant-6"},
		{BadgeRef: "btcpp-grant:grant-1", Hidden: true},
	}

	collection := buildWhoIsBadgeCollection(nil, grants, presentations)
	if len(collection.Featured) != 6 || collection.Featured[0].Name != "Badge 8" || collection.Featured[1].Name != "Badge 7" {
		t.Fatalf("unexpected featured badges: %+v", collection.Featured)
	}
	if collection.TotalVisible != 7 || !collection.HasMore || len(collection.OtherVisible) != 1 || collection.OtherVisible[0].Name != "Badge 6" {
		t.Fatalf("unexpected visible badge collection: %+v", collection)
	}
	if len(collection.Hidden) != 1 || collection.Hidden[0].Name != "Badge 1" {
		t.Fatalf("unexpected hidden badges: %+v", collection.Hidden)
	}
	if len(collection.Groups) != 2 || len(collection.Groups[0].Badges) != 4 || len(collection.Groups[1].Badges) != 3 {
		t.Fatalf("unexpected issuer groups: %+v", collection.Groups)
	}
}

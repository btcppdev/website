package handlers

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"
)

func TestLoadTemplates(t *testing.T) {
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
	for _, name := range []string{"developers_api.tmpl", "dashboard_hackathons.tmpl", "dashboard_sponsor.tmpl", "sponsor_invite.tmpl", "hackathon.tmpl", "hackathon_judging.tmpl", "hackathon_project.tmpl", "hackathon_schedule.tmpl", "admin/hackathon_projects.tmpl", "admin/hackathon_judging.tmpl", "admin/hackathon_managers.tmpl", "admin/hackathon_scores.tmpl", "admin/hackathon_awards.tmpl", "admin/subscribers.tmpl", "admin/global_discounts.tmpl", "admin/inline_missive.tmpl", "admin/templated_missives_index.tmpl", "admin/conference_missives.tmpl"} {
		if ctx.TemplateCache.Lookup(name) == nil {
			t.Fatalf("template %s was not loaded", name)
		}
	}
	inlineTemplates, err := ctx.TemplateCache.Clone()
	if err != nil {
		t.Fatalf("clone templates for inline missive: %v", err)
	}
	if _, err := inlineTemplates.Parse(`{{ define "mainnav" }}<nav>test</nav>{{ end }}`); err != nil {
		t.Fatalf("override inline missive test nav: %v", err)
	}
	var apiDocs bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&apiDocs, "developers_api.tmpl", nil); err != nil {
		t.Fatalf("render API documentation: %v", err)
	}
	for _, expected := range []string{"Build on", "/api/v1/openapi.json", "profile:self:read", "OAuth authorization", `href="/developers/api">API Docs`, "data-docs-search", "data-api-language", "data-api-endpoint", "data-copy-example"} {
		if !strings.Contains(apiDocs.String(), expected) {
			t.Fatalf("API documentation omitted %q", expected)
		}
	}
	apiDocsScript, err := os.ReadFile("static/js/api-docs.js")
	if err != nil {
		t.Fatalf("read API documentation script: %v", err)
	}
	for _, expected := range []string{"renderExample", "navigator.clipboard", "data-docs-search-input", "IntersectionObserver", `cache: "no-store"`, "Response example unavailable"} {
		if !strings.Contains(string(apiDocsScript), expected) {
			t.Fatalf("API documentation script omitted %q", expected)
		}
	}
	var loginPage bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&loginPage, "login.tmpl", &LoginPage{DevLoginEnabled: true}); err != nil {
		t.Fatalf("render development login: %v", err)
	}
	if !strings.Contains(loginPage.String(), `name="Action" value="dev-login"`) || !strings.Contains(loginPage.String(), "No email is sent") {
		t.Fatalf("development login shortcut missing: %s", loginPage.String())
	}
	loginPage.Reset()
	if err := inlineTemplates.ExecuteTemplate(&loginPage, "login.tmpl", &LoginPage{}); err != nil {
		t.Fatalf("render production login: %v", err)
	}
	if strings.Contains(loginPage.String(), `value="dev-login"`) {
		t.Fatalf("production login exposed development login shortcut: %s", loginPage.String())
	}
	var accountSetupPage bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&accountSetupPage, "dashboard_edit_speaker.tmpl", &EditSpeakerPage{
		Mode: "create", EmailPlain: "new@example.test", SetupProvider: "GitHub", RequireFullProfile: true, CancelCSRF: "cancel-csrf",
	}); err != nil {
		t.Fatalf("render OAuth account setup: %v", err)
	}
	for _, expected := range []string{"NEW ACCOUNT", "Finish setting up", "GitHub sign-in is verified", "CREATE ACCOUNT", "profile-edit-actions__create-account", `action="/logout"`, `name="csrf" value="cancel-csrf"`} {
		if !strings.Contains(accountSetupPage.String(), expected) {
			t.Fatalf("OAuth account setup omitted %q: %s", expected, accountSetupPage.String())
		}
	}
	if strings.Contains(accountSetupPage.String(), "Arrange <span>your</span> record") {
		t.Fatalf("new-account onboarding used edit-profile copy: %s", accountSetupPage.String())
	}
	if strings.Contains(accountSetupPage.String(), "Back to dashboard") {
		t.Fatalf("new-account onboarding offered a dashboard back link: %s", accountSetupPage.String())
	}
	accountSetupPage.Reset()
	if err := inlineTemplates.ExecuteTemplate(&accountSetupPage, "dashboard_edit_speaker.tmpl", &EditSpeakerPage{
		Mode: "create", EmailPlain: "sponsor@example.test", SetupPurpose: "sponsor",
		SetupName: "Ada Nakamoto", NextURL: "/sponsor-invites/example", CancelCSRF: "cancel-csrf",
	}); err != nil {
		t.Fatalf("render sponsor account setup: %v", err)
	}
	if !strings.Contains(accountSetupPage.String(), "You’ll continue to your sponsor invitation next.") || !strings.Contains(accountSetupPage.String(), `value="Ada Nakamoto"`) {
		t.Fatalf("sponsor account setup omitted destination context: %s", accountSetupPage.String())
	}
	if strings.Contains(accountSetupPage.String(), `id="PicFile" type="file" accept="image/*" required`) || strings.Contains(accountSetupPage.String(), `id="Phone" name="Phone" type="tel" required`) || strings.Contains(accountSetupPage.String(), `id="Signal" name="Signal" type="text" required`) {
		t.Fatalf("sponsor account setup required speaker-only profile fields: %s", accountSetupPage.String())
	}
	accountSetupPage.Reset()
	if err := inlineTemplates.ExecuteTemplate(&accountSetupPage, "dashboard_edit_speaker.tmpl", &EditSpeakerPage{Mode: "create", IsAdmin: true}); err != nil {
		t.Fatalf("render admin speaker creation: %v", err)
	}
	if !strings.Contains(accountSetupPage.String(), "Create a <span>speaker record.</span>") || strings.Contains(accountSetupPage.String(), "GitHub sign-in is verified") {
		t.Fatalf("admin speaker creation used account-onboarding copy: %s", accountSetupPage.String())
	}
	if strings.Contains(accountSetupPage.String(), `action="/logout"`) {
		t.Fatalf("admin speaker creation exposed new-account cancellation: %s", accountSetupPage.String())
	}
	var settingsPage bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&settingsPage, "dashboard_person_emails.tmpl", &PersonEmailsPage{
		Emails: []*types.PersonEmail{
			{Email: "primary@example.test", IsPrimary: true},
			{Email: "secondary@example.test"},
		},
		PendingEmails:   []string{"pending@example.test"},
		AuthMethodsCSRF: "settings-csrf-token",
		NostrCredentials: []*NostrCredentialView{{
			Credential: &types.PersonNostrCredential{ID: "nostr-credential"},
			Display:    "npub1linked",
		}},
	}); err != nil {
		t.Fatalf("render settings with linked Nostr key: %v", err)
	}
	if !strings.Contains(settingsPage.String(), "npub1linked") || strings.Contains(settingsPage.String(), "data-nostr-link") {
		t.Fatalf("linked Nostr settings offered another key: %s", settingsPage.String())
	}
	for _, expected := range []string{
		`class="profile-edit-hero account-settings-hero"`,
		`href="#settings-sign-in"`,
		`id="settings-sign-in"`,
		`id="settings-apps"`,
		`id="settings-api"`,
		`id="settings-emails"`,
		`href="/dashboard/settings" class="dashboard-tab is-active" aria-current="page"`,
		`for="api-token-scope-talks-read"`,
		`id="api-token-scope-talks-read" type="checkbox" name="scopes"`,
	} {
		if !strings.Contains(settingsPage.String(), expected) {
			t.Fatalf("account settings omitted workspace navigation %q: %s", expected, settingsPage.String())
		}
	}
	if strings.Contains(settingsPage.String(), `class="profile-edit-back"`) {
		t.Fatalf("account settings retained redundant back-to-dashboard link: %s", settingsPage.String())
	}
	settingsCSS, err := os.ReadFile("static/css/custom.css")
	if err != nil {
		t.Fatalf("read account settings stylesheet: %v", err)
	}
	for _, expected := range []string{
		`.person-emails-page .profile-edit-hero {
	box-sizing: border-box;
	width: 100%;
	max-width: none;`,
		`.person-emails-page .person-email-add input:not([type="checkbox"]):not([type="radio"])`,
		`.person-emails-page .person-email-row input:not([type="checkbox"]):not([type="radio"])`,
		`.account-settings-page .person-email-add input:not([type="checkbox"]):not([type="radio"])`,
		"min-width: 18px;",
		"min-height: 18px;",
		"padding: 0;",
	} {
		if !strings.Contains(string(settingsCSS), expected) {
			t.Fatalf("account settings stylesheet omitted layout guard %q", expected)
		}
	}
	for _, action := range []string{"/dashboard/emails/primary", "/dashboard/emails/remove", "/dashboard/emails/resend", "/dashboard/emails/request"} {
		start := strings.Index(settingsPage.String(), `action="`+action+`"`)
		if start < 0 {
			t.Fatalf("settings email action %s was not rendered", action)
		}
		end := strings.Index(settingsPage.String()[start:], `</form>`)
		if end < 0 || !strings.Contains(settingsPage.String()[start:start+end], `name="csrf" value="settings-csrf-token"`) {
			t.Fatalf("settings email action %s omitted CSRF token", action)
		}
	}
	if strings.Contains(settingsPage.String(), `value="shop:accounting:read"`) {
		t.Fatalf("non-global account settings exposed shop accounting scope: %s", settingsPage.String())
	}
	settingsPage.Reset()
	if err := inlineTemplates.ExecuteTemplate(&settingsPage, "dashboard_person_emails.tmpl", &PersonEmailsPage{IsGlobalAdmin: true}); err != nil {
		t.Fatalf("render global-admin settings: %v", err)
	}
	for _, expected := range []string{
		`value="shop:accounting:read"`,
		`for="oauth-scope-profile-write"`,
		`id="oauth-scope-profile-write" type="checkbox" name="scopes"`,
	} {
		if !strings.Contains(settingsPage.String(), expected) {
			t.Fatalf("global-admin account settings omitted %q: %s", expected, settingsPage.String())
		}
	}
	settingsPage.Reset()
	if err := inlineTemplates.ExecuteTemplate(&settingsPage, "dashboard_person_emails.tmpl", &PersonEmailsPage{}); err != nil {
		t.Fatalf("render settings without Nostr key: %v", err)
	}
	if !strings.Contains(settingsPage.String(), "data-nostr-link") {
		t.Fatalf("unlinked Nostr settings omitted link action: %s", settingsPage.String())
	}
	var projectPage bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&projectPage, "hackathon_project.tmpl", &HackathonPage{
		Competition: &types.HackathonCompetition{ID: "competition-id", Title: "Hackathon"},
		Conf:        &types.Conf{Tag: "toronto"},
		Project:     &types.HackathonProject{ID: "project-id", Title: "Project", Status: getters.ProjectStatusSubmitted},
		Members: []*types.ProjectMember{
			{PersonID: "linked-member", Name: "Linked Member", Role: getters.ProjectMemberRoleOwner},
			{PersonID: "private-member", Name: "Private Member", Role: getters.ProjectMemberRoleMember},
		},
		MemberProfileURLs: map[string]string{"linked-member": "/whois/linked-member"},
	}); err != nil {
		t.Fatalf("render hackathon project: %v", err)
	}
	if !strings.Contains(projectPage.String(), `<a class="hack-project-file__person" href="/whois/linked-member">`) {
		t.Fatalf("hackathon project member does not link to public profile: %s", projectPage.String())
	}
	projectPage.Reset()
	if err := inlineTemplates.ExecuteTemplate(&projectPage, "hackathon_project.tmpl", &HackathonPage{
		Competition:                 &types.HackathonCompetition{ID: "competition-id", Title: "Hackathon"},
		Conf:                        &types.Conf{Tag: "dev26"},
		Project:                     &types.HackathonProject{ID: "project-id", Title: "Project", Status: getters.ProjectStatusCreated},
		Members:                     []*types.ProjectMember{{PersonID: "viewer", Name: "Viewer", Role: getters.ProjectMemberRoleOwner}},
		IsProjectEditor:             true,
		CanEdit:                     true,
		CanSetSponsorContactConsent: true,
		SponsorContactCSRF:          "contact-csrf",
		SponsorContactConsent: &types.HackathonSponsorContactConsent{
			EnteredAwardSponsors: true,
		},
	}); err != nil {
		t.Fatalf("render hackathon project sponsor consent: %v", err)
	}
	for _, want := range []string{"Sponsor contact", "Submitting a project includes email sharing", "Allow other sponsors of this hackathon to contact me.", "Allow other sponsors whose prizes I enter to contact me.", `name="csrf" value="contact-csrf"`, `name="EnteredAwardSponsors" type="checkbox" checked`} {
		if !strings.Contains(projectPage.String(), want) {
			t.Fatalf("hackathon sponsor consent omitted %q", want)
		}
	}
	if strings.Contains(projectPage.String(), `href="/whois/private-member"`) {
		t.Fatalf("hackathon project links a member without a public profile: %s", projectPage.String())
	}
	var shopItemPage bytes.Buffer
	shopProduct := &types.MerchProduct{
		Slug:        "libre-relay",
		Name:        "libre relay hat",
		Description: "Support Libre Relay with this one-of-a-kind hat.",
		Images: []*types.MerchProductImage{{
			ObjectKey:       "https://cdn.example/merch/libre-relay.avif",
			SocialObjectKey: "https://cdn.example/merch/social/libre-relay.jpg",
		}},
	}
	if err := inlineTemplates.ExecuteTemplate(&shopItemPage, "shop/item.tmpl", &shopPage{Product: shopProduct}); err != nil {
		t.Fatalf("render shop item: %v", err)
	}
	for _, want := range []string{
		`<link rel="canonical" href="https://btcpp.dev/shop/libre-relay"`,
		`<meta property="og:type" content="product"`,
		`<meta property="og:title" content="libre relay hat · bitcoin&#43;&#43; shop"`,
		`<meta property="og:description" content="Support Libre Relay with this one-of-a-kind hat."`,
		`<meta property="og:image" content="https://cdn.example/merch/social/libre-relay.jpg"`,
		`<meta property="og:image:type" content="image/jpeg"`,
		`<meta property="og:image:width" content="1200"`,
		`<meta property="og:image:height" content="630"`,
		`<meta name="twitter:card" content="summary_large_image"`,
	} {
		if !strings.Contains(shopItemPage.String(), want) {
			t.Fatalf("shop item metadata missing %q: %s", want, shopItemPage.String())
		}
	}
	var shopHomePage bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&shopHomePage, "shop/index.tmpl", &shopPage{Product: shopProduct}); err != nil {
		t.Fatalf("render shop home: %v", err)
	}
	for _, want := range []string{
		`<link rel="canonical" href="https://btcpp.dev/shop"`,
		`<meta property="og:url" content="https://btcpp.dev/shop"`,
		`<meta property="og:title" content="bitcoin&#43;&#43; shop · Gear for bitcoin builders"`,
		`<meta property="og:description" content="Small-batch bitcoin&#43;&#43; apparel, hats, and gear for people who build on bitcoin and run their own nodes."`,
		`<meta property="og:image" content="https://cdn.example/merch/social/libre-relay.jpg"`,
		`<meta property="og:image:type" content="image/jpeg"`,
		`<meta name="twitter:card" content="summary_large_image"`,
	} {
		if !strings.Contains(shopHomePage.String(), want) {
			t.Fatalf("shop home metadata missing %q: %s", want, shopHomePage.String())
		}
	}
	var merchNewPage bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&merchNewPage, "admin/merch_new.tmpl", &shopPage{
		Product: &types.MerchProduct{}, SpacesReady: true, IsDevelopment: true,
	}); err != nil {
		t.Fatalf("render new merchandise page: %v", err)
	}
	for _, want := range []string{
		`enctype="multipart/form-data"`,
		`name="file"`,
		`data-merch-social-source`,
		`data-merch-social-preview`,
		`1200 × 630 JPEG`,
		`href="/dev/merch-social-card"`,
	} {
		if !strings.Contains(merchNewPage.String(), want) {
			t.Fatalf("new merchandise social preview missing %q: %s", want, merchNewPage.String())
		}
	}
	var hackathonPage bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&hackathonPage, "hackathon.tmpl", &HackathonPage{
		Competition: &types.HackathonCompetition{
			ID:                   "competition-id",
			Title:                "Hackathon",
			Visibility:           getters.CompetitionVisibilityPublic,
			LifecycleOverride:    getters.CompetitionLifecycleOpen,
			PublicGalleryEnabled: true,
		},
		Conf:     &types.Conf{Tag: "toronto"},
		Projects: []*types.HackathonProject{{ID: "my-project", Title: "My Project"}},
		Awards:   []*types.Award{{ID: "sponsor-bounty-id", Title: "Build the future", SponsoredByOrgID: "sponsor-org"}},
		OrgsByID: map[string]*types.Org{
			"sponsor-org": {Name: "Future Sponsor", Website: "https://sponsor.example", LogoDark: "https://sponsor.example/logo.svg"},
		},
		PrizesByAward: map[string][]*types.Prize{
			"sponsor-bounty-id": {{Title: "Cash prize", ValueText: "1750000"}},
		},
		OwnedProjects: map[string]bool{"my-project": true},
		Viewer: &auth.Identity{
			PersonID: "person-id",
			Roles:    []auth.Role{{Scope: "toronto", Name: auth.RoleAdmin}},
		},
		CanJudge: true,
	}); err != nil {
		t.Fatalf("render hackathon with owned project: %v", err)
	}
	if !strings.Contains(hackathonPage.String(), `<a href="/toronto/hackathon/projects/my-project/edit" class="hack-button hack-button--accent">Edit project →</a>`) {
		t.Fatalf("hackathon does not render its established edit-project action: %s", hackathonPage.String())
	}
	for _, unwanted := range []string{`data-hackathon-tab="my-projects"`, `id="my-projects"`, `My projects`, `>My project</a>`} {
		if strings.Contains(hackathonPage.String(), unwanted) {
			t.Fatalf("hackathon still renders obsolete project panel marker %q: %s", unwanted, hackathonPage.String())
		}
	}
	if !strings.Contains(hackathonPage.String(), `data-hackathon-tab="projects"`) {
		t.Fatalf("hackathon with an open gallery does not render the Project gallery tab: %s", hackathonPage.String())
	}
	if strings.Contains(hackathonPage.String(), `View project gallery`) {
		t.Fatalf("hackathon renders the redundant View project gallery hero action: %s", hackathonPage.String())
	}
	for _, unwanted := range []string{`Judging →`, `Edit hackathon →`} {
		if strings.Contains(hackathonPage.String(), unwanted) {
			t.Fatalf("hackathon hero still renders relocated action %q: %s", unwanted, hackathonPage.String())
		}
	}
	for _, want := range []string{`href="/toronto/hackathon/judging"`, `href="/toronto/admin/hackathon" data-active-prefix="/toronto/admin/hackathon">/edit</a>`} {
		if !strings.Contains(hackathonPage.String(), want) {
			t.Fatalf("hackathon navigation missing relocated action %q: %s", want, hackathonPage.String())
		}
	}
	for _, want := range []string{
		`id="bounty-sponsor-bounty-id"`,
		`href="#bounty-sponsor-bounty-id"`,
		`id="award-sponsor-bounty-id"`,
		`href="#award-sponsor-bounty-id"`,
		`href="https://sponsor.example" target="_blank" rel="noreferrer">[ Future Sponsor ]</a>`,
		`class="hack-sponsored-award-logo" href="https://sponsor.example"`,
		`class="hack-award-card__value"`,
		`<span>Total prize value</span>`,
		`<strong>1.75M <small>sats</small></strong>`,
		`window.location.hash.indexOf('#award-') === 0) return 'awards'`,
	} {
		if !strings.Contains(hackathonPage.String(), want) {
			t.Fatalf("hackathon award permalink missing %q: %s", want, hackathonPage.String())
		}
	}
	var privateHackathonPage bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&privateHackathonPage, "hackathon.tmpl", &HackathonPage{
		Competition: &types.HackathonCompetition{
			ID:                   "private-competition-id",
			Title:                "New Hackathon",
			Visibility:           getters.CompetitionVisibilityPublic,
			LifecycleOverride:    getters.CompetitionLifecycleOpen,
			PublicGalleryEnabled: false,
		},
		Conf: &types.Conf{Tag: "berlin26"},
	}); err != nil {
		t.Fatalf("render hackathon with private gallery: %v", err)
	}
	for _, want := range []string{
		`href="/berlin26/hackathon#projects" data-hackathon-tab="projects"`,
		`id="projects"`,
		`This hackathon’s project gallery isn’t public yet.`,
		`href="/berlin26#tickets"`,
	} {
		if !strings.Contains(privateHackathonPage.String(), want) {
			t.Fatalf("private hackathon gallery missing %q: %s", want, privateHackathonPage.String())
		}
	}
	for _, unwanted := range []string{`Previous project gallery`, `Previous builds`, `previous hackathon`} {
		if strings.Contains(privateHackathonPage.String(), unwanted) {
			t.Fatalf("private hackathon gallery exposes prior-event copy %q: %s", unwanted, privateHackathonPage.String())
		}
	}
	var confHackathonSection bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&confHackathonSection, "conf_hackathon_section", &ConfPage{
		Conf: &types.Conf{Tag: "toronto", Desc: "Bitcoin++ Toronto"},
		Hackathon: &types.HackathonCompetition{
			Title:             "Toronto Hackathon",
			LifecycleOverride: getters.CompetitionLifecycleOpen,
		},
		HackathonJudges: []*types.CompetitionJudge{{Name: "Judge One"}, {Name: "Judge Two"}},
		HackathonPlaceRows: []*HackathonPlaceRow{
			{PlaceLabel: "01", ProjectID: "first-project", ProjectTitle: "First prize", Amount: "1,000,000 sats", Members: []*types.ProjectMember{{Name: "Ada Hacker", Photo: "ada.jpg"}}, GrandPrize: true},
			{PlaceLabel: "02", ProjectTitle: "Second prize", Amount: "500,000 sats"},
			{PlaceLabel: "03", ProjectTitle: "Third prize", Amount: "250,000 sats"},
		},
		HackathonPrizePoolSats: 1_750_000,
	}); err != nil {
		t.Fatalf("render conference hackathon section: %v", err)
	}
	for _, want := range []string{`class="conf-hackathon-redesign__prize-hero"`, `/static/img/rebrand/hackathon-trophy.jpg`, `>1.75M</strong>`, `>Sats up for grabs</span>`, `alt="Ada Hacker"`} {
		if !strings.Contains(confHackathonSection.String(), want) {
			t.Fatalf("conference hackathon feature missing %q: %s", want, confHackathonSection.String())
		}
	}
	if strings.Contains(confHackathonSection.String(), `conf-hackathon-redesign__stats`) {
		t.Fatalf("conference hackathon still renders the retired stats grid: %s", confHackathonSection.String())
	}
	confHackathonSection.Reset()
	if err := inlineTemplates.ExecuteTemplate(&confHackathonSection, "conf_hackathon_section", &ConfPage{
		Conf:                   &types.Conf{Tag: "berlin26", Desc: "Bitcoin++ Berlin"},
		Hackathon:              &types.HackathonCompetition{Title: "Berlin Hackathon"},
		HackathonJudges:        []*types.CompetitionJudge{{Name: "Past Judge"}},
		HackathonJudgesArePast: true,
		HackathonJudgesLabel:   "Berlin 2025",
	}); err != nil {
		t.Fatalf("render conference hackathon section with prior judges: %v", err)
	}
	for _, want := range []string{`>PAST JUDGES</strong>`, `Berlin 2025 · This event’s panel is coming soon`} {
		if !strings.Contains(confHackathonSection.String(), want) {
			t.Fatalf("conference prior-judge context missing %q: %s", want, confHackathonSection.String())
		}
	}
	confHackathonSection.Reset()
	if err := inlineTemplates.ExecuteTemplate(&confHackathonSection, "conf_hackathon_section", &ConfPage{
		Conf:      &types.Conf{Tag: "toronto", Desc: "Bitcoin++ Toronto"},
		Hackathon: &types.HackathonCompetition{Title: "Toronto Hackathon"},
	}); err != nil {
		t.Fatalf("render conference hackathon section without configured prizes: %v", err)
	}
	for _, want := range []string{`>Prizes</strong>`, `>Coming soon</span>`} {
		if !strings.Contains(confHackathonSection.String(), want) {
			t.Fatalf("conference hackathon fallback prize callout missing %q: %s", want, confHackathonSection.String())
		}
	}
	if strings.Contains(confHackathonSection.String(), `<dt>Schedule</dt>`) {
		t.Fatalf("conference hackathon prize fallback exposes the schedule: %s", confHackathonSection.String())
	}
	projectNumber := 7
	var adminProjects bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&adminProjects, "admin/hackathon_projects.tmpl", &HackathonAdminPage{
		Competition: &types.HackathonCompetition{ID: "competition-id", Title: "Hackathon"},
		Conf:        &types.Conf{Ref: "conference-id", Tag: "toronto"},
		Projects: []*types.HackathonProject{
			{ID: "draft-project", Title: "Draft", Status: getters.ProjectStatusCreated, ProjectNumber: &projectNumber},
			{ID: "submitted-project", Title: "Submitted", Status: getters.ProjectStatusSubmitted},
		},
		ActiveTab: "projects",
	}); err != nil {
		t.Fatalf("render admin hackathon projects: %v", err)
	}
	if got := strings.Count(adminProjects.String(), ">\n                    Submit project\n"); got != 1 {
		t.Fatalf("admin projects rendered %d submit actions, want one for the draft: %s", got, adminProjects.String())
	}
	for _, want := range []string{`name="Status" value="submitted"`, `name="ProjectNumber" value="7"`, `bypasses the submission deadline`} {
		if !strings.Contains(adminProjects.String(), want) {
			t.Fatalf("admin draft submit action missing %q: %s", want, adminProjects.String())
		}
	}
	var inlineMissive bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&inlineMissive, "admin/inline_missive.tmpl", &InlineMissivePage{
		Current: &mtypes.Letter{UID: 42, PageID: "page-id", OnlyFor: "volapp", Title: "Hi {{ .Name }}", Markdown: "Hello {{ .Volunteer.Name }}"},
		Fields:  onlyForTemplateFields("volapp"),
	}); err != nil {
		t.Fatalf("render inline missive editor: %v", err)
	}
	for _, want := range []string{`action="/admin/missives/42/inline"`, `{{ .Volunteer.Name }}`, `Triggered email`} {
		if !strings.Contains(inlineMissive.String(), want) {
			t.Fatalf("inline missive editor missing %q", want)
		}
	}
	var missiveIndex bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&missiveIndex, "admin/templated_missives_index.tmpl", &TemplatedMissivesPage{
		Letters:           []*mtypes.Letter{{UID: 42, OnlyFor: "volapp", Title: "Volunteer application"}},
		MissiveView:       missiveViewOneShots,
		MissiveTabCounts:  MissiveTabCounts{OneShots: 1, Unsent: 2, SentScheduled: 3},
		OneShotsTabURL:    "/admin/missives?view=oneshots",
		UnsentTabURL:      "/admin/missives?view=unsent",
		SentTabURL:        "/admin/missives?view=sent",
		ClearFilterURL:    "/admin/missives?view=oneshots",
		OneShotLabels:     oneShotMissiveLabels(),
		ScheduledMissives: map[uint64]bool{},
		IsDevelopment:     true,
		DevReviewEmail:    "developer@example.com",
	}); err != nil {
		t.Fatalf("render missive index: %v", err)
	}
	for _, want := range []string{"One-shots", "Unsent", "Sent / scheduled", "Volunteer application received", "volapp", `action="/admin/missives/weekly/test-auto-draft"`, "Review email redirects to developer@example.com"} {
		if !strings.Contains(missiveIndex.String(), want) {
			t.Fatalf("missive index missing %q", want)
		}
	}
	var missiveEditor bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&missiveEditor, "admin/templated_missives.tmpl", &TemplatedMissivesPage{
		Form: TemplatedMissiveForm{UID: 77, Title: "Weekly draft", Template: "roundup", Palette: "ember"},
	}); err != nil {
		t.Fatalf("render missive editor: %v", err)
	}
	for _, want := range []string{`id="MissiveControlsPanel"`, `id="OpenMissiveControls"`, `id="CloseMissiveControls"`, `id="NewsletterPreview"`, `window.matchMedia('(max-width: 1279px)')`} {
		if !strings.Contains(missiveEditor.String(), want) {
			t.Fatalf("mobile missive editor missing %q", want)
		}
	}
	if strings.Contains(missiveEditor.String(), `style="min-width:680px;"`) {
		t.Fatal("newsletter preview retains a forced desktop width on mobile")
	}
	var eventMissiveIndex bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&eventMissiveIndex, "admin/conference_missives.tmpl", &ConferenceMissivesPage{
		Conf:      &types.Conf{Tag: "dev26", Desc: "Local Dev 2026", Timezone: "America/Chicago"},
		Campaigns: []*types.ConferenceEmailCampaign{{ID: "campaign-id", Kind: "attendee-final", Title: "Event details", Audience: "attendees", Enabled: true}},
		View:      conferenceMissiveViewTemplates, ScheduleURL: "/dev26/admin/missives?view=schedule", OnSubURL: "/dev26/admin/missives?view=onsub", TemplatesURL: "/dev26/admin/missives?view=templates",
		DevEmailOverride: "developer@example.com", CanGenerateDev: true, CanSendDevDrafts: true, DraftCount: 6,
		Counts: ConferenceMissiveTabCounts{Schedule: 6, OnSub: 1, Templates: 7},
	}); err != nil {
		t.Fatalf("render conference missive index: %v", err)
	}
	for _, want := range []string{"Schedule", "On registration", "Templates", `href="/dev26/admin/missives/campaigns/campaign-id"`, "Campaign templates", `action="/dev26/admin/missives/dev-generate-all"`, "Generate all drafts", `action="/dev26/admin/missives/dev-send-all"`, "Send all drafts", "developer@example.com"} {
		if !strings.Contains(eventMissiveIndex.String(), want) {
			t.Fatalf("conference missive index missing %q", want)
		}
	}
	var eventMissiveEditor bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&eventMissiveEditor, "admin/templated_missives.tmpl", &TemplatedMissivesPage{
		Conf: &types.Conf{Tag: "dev26", Desc: "Local Dev 2026"}, IsOccurrence: true,
		Occurrence: &types.ConferenceEmailOccurrence{
			ID: "occurrence-id", CampaignKind: "attendee-reminder-28", CampaignTitle: "Event reminder", Audience: "attendees", SendLabel: "Friday at 10:00 AM",
		},
		EditorTitle: "Edit generated draft", EditorHeading: "Edit generated draft", EditorDescription: "Saving changes only this occurrence.",
		BackURL: "/dev26/admin/missives", BackLabel: "Event missives", FormAction: "/dev26/admin/missives/occurrences/occurrence-id",
		UploadImageURL: "/dev26/admin/missives/upload-image", TestSendAction: "/dev26/admin/missives/occurrences/occurrence-id/test-send", SaveLabel: "Save generated draft",
		RebuildAction: "/dev26/admin/missives/occurrences/occurrence-id/rebuild", CancelAction: "/dev26/admin/missives/occurrences/occurrence-id/cancel",
		Form: TemplatedMissiveForm{Title: "Event reminder", Template: "announce", Palette: "ember", ContentMarkdown: "Hello there"},
	}); err != nil {
		t.Fatalf("render conference missive editor: %v", err)
	}
	for _, want := range []string{`id="MissiveControlsPanel"`, `id="OpenMissiveControls"`, `id="NewsletterPreview"`, "Save generated draft", "Rebuild from event data", "Cancel email", "Send test", `/dev26/admin/missives/occurrences/occurrence-id/test-send`, "attendees · attendee-reminder-28 · sends Friday at 10:00 AM"} {
		if !strings.Contains(eventMissiveEditor.String(), want) {
			t.Fatalf("shared conference occurrence editor missing %q", want)
		}
	}
	var eventCampaignEditor bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&eventCampaignEditor, "admin/templated_missives.tmpl", &TemplatedMissivesPage{
		Conf: &types.Conf{Tag: "dev26", Desc: "Local Dev 2026"}, IsCampaign: true,
		Campaign:        &types.ConferenceEmailCampaign{ID: "campaign-id", Kind: "attendee-final", Audience: "attendees", Title: types.ConferenceCampaignSubject("Event details"), Enabled: true},
		CampaignEnabled: true, EditorTitle: "Edit attendee-final", EditorHeading: "Edit attendee-final", EditorDescription: "Build this campaign",
		BackURL: "/dev26/admin/missives?view=templates", BackLabel: "Event missives", FormAction: "/dev26/admin/missives/campaigns/campaign-id",
		TestSendAction: "/dev26/admin/missives/campaigns/campaign-id/test-send", UploadImageURL: "/dev26/admin/missives/upload-image", SaveLabel: "Save campaign template",
		Form: TemplatedMissiveForm{Title: types.ConferenceCampaignSubject("Event details"), Template: "announce", Palette: "ember"},
	}); err != nil {
		t.Fatalf("render conference campaign editor: %v", err)
	}
	for _, want := range []string{`action="/dev26/admin/missives/campaigns/campaign-id"`, `id="NewsletterPreview"`, "Save campaign template", "Campaign enabled"} {
		if !strings.Contains(eventCampaignEditor.String(), want) {
			t.Fatalf("conference campaign editor missing %q", want)
		}
	}
	discountTemplates, err := ctx.TemplateCache.Clone()
	if err != nil {
		t.Fatalf("clone templates for global discounts: %v", err)
	}
	if _, err := discountTemplates.Parse(`{{ define "mainnav" }}<nav>test</nav>{{ end }}`); err != nil {
		t.Fatalf("override global discounts test nav: %v", err)
	}
	var discounts bytes.Buffer
	if err := discountTemplates.ExecuteTemplate(&discounts, "admin/global_discounts.tmpl", &GlobalAdminDiscountsPage{
		Confs:                  []*types.Conf{{Ref: "seoul-id", Tag: "seoul26", Desc: "Seoul"}, {Ref: "berlin-id", Tag: "berlin26", Desc: "Berlin"}},
		Discounts:              []GlobalAdminDiscountRow{{AdminDiscountRow: AdminDiscountRow{ID: "discount-id", CodeName: "COMMUNITY25", AmountLabel: "$25 fixed"}}},
		Form:                   GlobalDiscountForm{DiscountForm: DiscountForm{DiscountType: "percent", Amount: "50"}},
		SelectedConferenceRefs: map[string]bool{"seoul-id": true, "berlin-id": true},
	}); err != nil {
		t.Fatalf("render global discounts: %v", err)
	}
	for _, want := range []string{`action="/admin/discounts"`, `value="seoul-id" checked`, `value="50"`, `value="fixed"`, `Set price to`, `name="action" value="delete"`, `name="discount_id" value="discount-id"`, `>Delete</button>`} {
		if !strings.Contains(discounts.String(), want) {
			t.Fatalf("global discounts render missing %q", want)
		}
	}
	var volunteerConfirmation bytes.Buffer
	if err := discountTemplates.ExecuteTemplate(&volunteerConfirmation, "volunteer_confirmation.tmpl", &VolunteerApplicationConfirmationPage{
		Error:     "Volunteer confirmation link is invalid, expired, or already used.",
		Token:     "expired-token",
		CanResend: true,
	}); err != nil {
		t.Fatalf("render volunteer confirmation error: %v", err)
	}
	for _, want := range []string{`href="/volunteer"`, `>Apply again</a>`, `action="/volunteer/confirm/resend"`, `value="expired-token"`, `>Resend confirmation email</button>`} {
		if !strings.Contains(volunteerConfirmation.String(), want) {
			t.Fatalf("volunteer confirmation error render missing %q", want)
		}
	}
	var nav bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&nav, "generic_conf_nav", &types.Conf{Tag: "toronto", ShowHackathon: true}); err != nil {
		t.Fatalf("render generic_conf_nav: %v", err)
	}
	if !strings.Contains(nav.String(), `href="/toronto#hackathon"`) {
		t.Fatalf("live hackathon nav missing conference hackathon anchor: %s", nav.String())
	}
	nav.Reset()
	if err := ctx.TemplateCache.ExecuteTemplate(&nav, "generic_conf_nav", &types.Conf{Tag: "toronto"}); err != nil {
		t.Fatalf("render generic_conf_nav without hackathon: %v", err)
	}
	if strings.Contains(nav.String(), `href="/toronto#hackathon"`) {
		t.Fatalf("inactive hackathon nav unexpectedly contains conference hackathon anchor: %s", nav.String())
	}
	if !strings.Contains(nav.String(), `aria-label="Primary navigation"`) || !strings.Contains(nav.String(), `class="site-conf-nav"`) || strings.Contains(nav.String(), `class="rebrand-nav"`) {
		t.Fatalf("conference page did not use unified global navigation: %s", nav.String())
	}
	if !strings.Contains(nav.String(), `href="/">/home</a>`) || !strings.Contains(nav.String(), `href="/events">/events</a>`) || strings.Contains(nav.String(), `href="/#events"`) || strings.Contains(nav.String(), `href="/timeline"`) {
		t.Fatalf("conference navigation did not expose the single canonical events destination: %s", nav.String())
	}
	if !strings.Contains(nav.String(), `/static/js/brand-wordmark.js?v=20260831-1`) {
		t.Fatalf("unified navigation did not include the site-wide bitcoin++ wordmark treatment: %s", nav.String())
	}
	nav.Reset()
	if err := ctx.TemplateCache.ExecuteTemplate(&nav, "hackathon_conf_nav", &HackathonPage{
		Competition: &types.HackathonCompetition{Title: "Signet Builders Sprint"},
		Conf:        &types.Conf{Tag: "dev26", Desc: "Local Dev 2026"},
		CanJudge:    true,
		Viewer:      &auth.Identity{Roles: []auth.Role{{Scope: "dev26", Name: auth.RoleAdmin}}},
	}); err != nil {
		t.Fatalf("render hackathon_conf_nav: %v", err)
	}
	for _, want := range []string{
		`class="site-conf-nav site-conf-nav--hackathon"`,
		`class="site-conf-nav__identity site-conf-nav__identity--back" href="/dev26"`,
		`<span>← THIS EVENT</span>`,
		`<strong>Hackathon</strong>`,
		`href="/dev26/hackathon" data-hackathon-tab="overview" data-active-exact>/start</a>`,
		`href="/dev26/hackathon#projects" data-hackathon-tab="projects"`,
		`href="/dev26/hackathon#awards" data-hackathon-tab="awards">/prizes</a>`,
		`href="/dev26/hackathon/schedule" data-active-prefix="/dev26/hackathon/schedule">/schedule</a>`,
		`href="/dev26/hackathon/judging" data-active-prefix="/dev26/hackathon/judging">/judging</a>`,
		`href="/dev26/admin/hackathon" data-active-prefix="/dev26/admin/hackathon">/edit</a>`,
	} {
		if !strings.Contains(nav.String(), want) {
			t.Fatalf("contextual hackathon navigation missing %q: %s", want, nav.String())
		}
	}
	if strings.Contains(nav.String(), `class="hack-tabs"`) {
		t.Fatalf("contextual hackathon navigation unexpectedly includes the legacy tab bar: %s", nav.String())
	}
	var accountNav bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&accountNav, "site_account_authenticated", &siteAccountNavView{
		Name: "Mara Chen", Email: "mara@example.test", Initial: "M", ProfileURL: "/whois/mara", CSRF: "logout-csrf", IsGlobalAdmin: true,
	}); err != nil {
		t.Fatalf("render authenticated account navigation: %v", err)
	}
	for _, want := range []string{"Mara Chen", "mara@example.test", `href="/dashboard"`, `href="/dashboard/settings"`, `href="/dashboard/profile"`, `href="/whois/mara"`, `href="/admin"`, `action="/logout"`, `name="csrf" value="logout-csrf"`} {
		if !strings.Contains(accountNav.String(), want) {
			t.Fatalf("authenticated account navigation missing %q: %s", want, accountNav.String())
		}
	}
	var dashboardTabs bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&dashboardTabs, "dashboard_tabs", map[string]any{
		"Active":    "overview",
		"ShowAdmin": false,
	}); err != nil {
		t.Fatalf("render dashboard_tabs: %v", err)
	}
	if strings.Contains(dashboardTabs.String(), `href="/admin"`) {
		t.Fatalf("non-admin dashboard tabs expose admin: %s", dashboardTabs.String())
	}
	if strings.Contains(dashboardTabs.String(), `href="/dashboard/hackathons"`) {
		t.Fatalf("nonparticipant dashboard tabs expose hackathons: %s", dashboardTabs.String())
	}
	if !strings.Contains(dashboardTabs.String(), `href="/dashboard/settings" class="dashboard-tab"`) {
		t.Fatalf("dashboard tabs omitted settings: %s", dashboardTabs.String())
	}
	if !strings.Contains(dashboardTabs.String(), `class="dashboard-tabs"`) {
		t.Fatalf("dashboard tabs were not rendered: %s", dashboardTabs.String())
	}
	dashboardTabs.Reset()
	if err := ctx.TemplateCache.ExecuteTemplate(&dashboardTabs, "dashboard_tabs", map[string]any{
		"Active":         "hackathons",
		"ShowHackathons": true,
		"ShowAdmin":      false,
	}); err != nil {
		t.Fatalf("render participant dashboard_tabs: %v", err)
	}
	if !strings.Contains(dashboardTabs.String(), `href="/dashboard/hackathons" class="dashboard-tab is-active" aria-current="page"`) {
		t.Fatalf("hackathons dashboard tab is not active: %s", dashboardTabs.String())
	}
	if !strings.Contains(dashboardTabs.String(), `class="dashboard-tabs"`) {
		t.Fatalf("multi-section participant dashboard does not render tab navigation: %s", dashboardTabs.String())
	}
	dashboardTabs.Reset()
	if err := ctx.TemplateCache.ExecuteTemplate(&dashboardTabs, "dashboard_tabs", map[string]any{
		"Active":      "sponsor",
		"ShowSponsor": true,
	}); err != nil {
		t.Fatalf("render sponsor dashboard_tabs: %v", err)
	}
	if !strings.Contains(dashboardTabs.String(), `href="/dashboard/sponsor" class="dashboard-tab is-active" aria-current="page"`) {
		t.Fatalf("sponsor dashboard tab is not active: %s", dashboardTabs.String())
	}
	dashboardTabs.Reset()
	if err := ctx.TemplateCache.ExecuteTemplate(&dashboardTabs, "dashboard_tabs", map[string]any{
		"Active":    "admin",
		"ShowAdmin": true,
	}); err != nil {
		t.Fatalf("render admin dashboard_tabs: %v", err)
	}
	if !strings.Contains(dashboardTabs.String(), `href="/admin" class="dashboard-tab is-active" aria-current="page"`) {
		t.Fatalf("admin dashboard tab is not active: %s", dashboardTabs.String())
	}
	if !strings.Contains(dashboardTabs.String(), `class="dashboard-tabs"`) {
		t.Fatalf("multi-section admin dashboard does not render tab navigation: %s", dashboardTabs.String())
	}
	if strings.LastIndex(dashboardTabs.String(), `href="/dashboard/settings"`) < strings.LastIndex(dashboardTabs.String(), `href="/admin"`) {
		t.Fatalf("settings is not the final dashboard tab: %s", dashboardTabs.String())
	}

	var adminDashboard bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&adminDashboard, "admin/dashboard.tmpl", &GlobalAdminDashboardPage{
		HasHackathonProjects:    true,
		HasSponsorOrganizations: true,
	}); err != nil {
		t.Fatalf("render global admin dashboard: %v", err)
	}
	for _, want := range []string{`href="/dashboard/hackathons"`, `href="/dashboard/sponsor"`, `href="/admin" class="dashboard-tab is-active" aria-current="page"`, `href="/dashboard/settings"`, `class="profile-edit-tag">§ Global workspace · global-admin`, `<h1>Site <span>administration.</span></h1>`} {
		if !strings.Contains(adminDashboard.String(), want) {
			t.Fatalf("global admin dashboard omitted %q: %s", want, adminDashboard.String())
		}
	}
	if strings.Contains(adminDashboard.String(), `dashboard-workspace-hero__mark`) {
		t.Fatalf("global admin dashboard retained workspace mark: %s", adminDashboard.String())
	}

	var sponsorDashboard bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&sponsorDashboard, "dashboard_sponsor.tmpl", &SponsorDashboardPage{
		Membership:           &types.OrganizationMembership{PersonID: "owner-id", Role: getters.OrganizationRoleOwner},
		Organization:         &types.Org{Ref: "org-id", Name: "Signet Systems", Tagline: "Test networks", LogoLight: "/logo-light.svg", LogoDark: "/logo-dark.svg"},
		Memberships:          []*types.OrganizationMembership{{OrganizationID: "org-id", Organization: &types.Org{Name: "Signet Systems"}}},
		HasHackathonProjects: true,
		IsGlobalAdmin:        true,
		Upcoming: []*types.SponsorDashboardEvent{{
			Sponsorship:   &types.Sponsorship{Ref: "sponsorship-id", Level: "Headline", Status: "Paid"},
			Conference:    &types.Conf{Ref: "conference-id", Tag: "dev26", Desc: "Local Dev", DateDesc: "Oct 2026", Location: "Austin", ShowHackathon: true},
			Competition:   &types.HackathonCompetition{ID: "competition-id", Title: "Local Hackathon"},
			Entitlement:   &types.SponsorshipEntitlement{TicketAllocation: 20, SponsorAwardLimit: 2, ParticipantContactAccess: true},
			TicketsIssued: 5,
			AwardCount:    1,
		}},
		Members: []*types.OrganizationMembership{
			{PersonID: "owner-id", Role: getters.OrganizationRoleOwner, Status: "active", PersonName: "Mara", PersonEmail: "mara@example.test"},
			{PersonID: "member-id", Role: getters.OrganizationRoleMember, Status: "active", PersonName: "Eli", PersonEmail: "eli@example.test"},
		},
		PrizeEntries: []*types.SponsorPrizeEntry{{
			AwardID: "award-id", AwardTitle: "Best Signet Infrastructure",
			ConferenceTag: "dev26", ConferenceTitle: "Local Dev",
			ProjectID: "project-id", ProjectTitle: "Fixture Forge",
			ProjectShortDescription: "Deterministic test data for bitcoin applications.",
			ProjectStatus:           "submitted", GitHubURL: "https://github.com/example/fixture-forge",
			Participants: []*types.SponsorPrizeParticipant{{
				PersonID: "person-id", Name: "Mara", Role: "owner", PublicID: "mara",
				Photo: "mara.jpg", Email: "mara@example.test", ConsentScope: "entered_award",
			}},
		}},
		CanManage:             true,
		CanEditOrg:            true,
		CanExportParticipants: true,
		SpacesReady:           true,
		CSRF:                  "sponsor-csrf",
		InviteLink:            "http://localhost:8888/sponsor-invites/example-token",
	}); err != nil {
		t.Fatalf("render sponsor dashboard: %v", err)
	}
	for _, want := range []string{"Signet Systems", "20", "Opt-in only", "Sponsor workspace sections", "Public sponsor card preview", "width: 25%", "width: 50%", `href="/dashboard/hackathons"`, `href="/admin"`, `name="csrf" value="sponsor-csrf"`, `action="/dashboard/sponsor/org-id/profile"`, `action="/dashboard/sponsor/org-id/invites"`, `action="/dashboard/sponsor/org-id/tickets"`, `action="/dashboard/sponsor/org-id/prize-proposals"`, `action="/dashboard/sponsor/org-id/members/member-id/remove"`, `href="/dashboard/sponsor/org-id/hackathon-projects.csv"`, "http://localhost:8888/sponsor-invites/example-token", `class="sponsor-logo-variant__preview is-light"`, `src="/logo-light.svg"`, `class="sponsor-logo-variant__preview is-dark"`, `src="/logo-dark.svg"`, `name="LogoLightFile"`, `name="LogoDarkFile"`, "Teams building for your challenges.", "Fixture Forge", `href="/whois/mara"`, `href="mailto:mara@example.test"`, "Consented through this prize"} {
		if !strings.Contains(sponsorDashboard.String(), want) {
			t.Fatalf("sponsor dashboard omitted %q", want)
		}
	}
	for _, unwanted := range []string{`name="LogoLight"`, `name="LogoDark"`} {
		if strings.Contains(sponsorDashboard.String(), unwanted) {
			t.Fatalf("sponsor dashboard still exposes raw logo URL input %q", unwanted)
		}
	}
	var sponsorEvents bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&sponsorEvents, "sponsors/events.tmpl", &SponsorshipsPage{
		Conf: &types.Conf{Tag: "dev26", Desc: "Local Dev"},
		Sponsorships: []*types.Sponsorship{{
			Ref: "sponsorship-id", Level: "Headline", Status: "Paid",
			Org: &types.Org{Ref: "org-id", Name: "Signet Systems"},
		}},
		Entitlements: map[string]*types.SponsorshipEntitlement{
			"sponsorship-id": {
				TicketAllocation: 24, SponsorAwardLimit: 3,
				AllHackathonSubmissions: true,
				CanEditOrganization:     true,
			},
		},
	}); err != nil {
		t.Fatalf("render event sponsorships: %v", err)
	}
	for _, want := range []string{`name="TicketAllocation"`, `value="24"`, `>Tickets</th>`, `name="SponsorAwardLimit"`, `value="3"`, `name="ManagerPersonID"`, `name="ManagerName"`, `name="ManagerEmail"`, "secure 72-hour login link", `data-search-url="/dev26/admin/sponsors/people/search"`, `name="CanEditOrganization"`, "Can edit organization"} {
		if !strings.Contains(sponsorEvents.String(), want) {
			t.Fatalf("event sponsorships omitted %q: %s", want, sponsorEvents.String())
		}
	}
	if strings.Contains(sponsorEvents.String(), "CanManageAwardJudges") || strings.Contains(sponsorEvents.String(), "manage prize judges") {
		t.Fatalf("event sponsorships exposed sponsor judge management: %s", sponsorEvents.String())
	}
	var sponsorInvite bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&sponsorInvite, "sponsor_invite.tmpl", &SponsorInvitePage{
		Invite: &types.OrganizationMemberInvite{OrganizationName: "Signet Systems", Email: "teammate@example.test", Role: getters.OrganizationRoleManager},
		Token:  "secure-token",
		CSRF:   "invite-csrf",
	}); err != nil {
		t.Fatalf("render sponsor invite: %v", err)
	}
	for _, want := range []string{"Join Signet Systems", "teammate@example.test", `action="/sponsor-invites/secure-token"`, `name="csrf" value="invite-csrf"`} {
		if !strings.Contains(sponsorInvite.String(), want) {
			t.Fatalf("sponsor invite omitted %q", want)
		}
	}
}

func TestHackathonRichTextHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "allowed formatting",
			input: `<p>Hello <strong>world</strong><br><a href="https://example.com" onclick="bad()">link</a></p>`,
			want:  `<p>Hello <strong>world</strong><br><a href="https://example.com" rel="noopener noreferrer">link</a></p>`,
		},
		{
			name:  "unsafe tags removed",
			input: `<p>Safe</p><script>alert("bad")</script><style>body{display:none}</style>`,
			want:  `<p>Safe</p>`,
		},
		{
			name:  "unsafe links lose href",
			input: `<a href="javascript:alert(1)">bad</a> <a href="/hackathons/test">good</a>`,
			want:  `<a>bad</a> <a href="/hackathons/test" rel="noopener noreferrer">good</a>`,
		},
		{
			name:  "plain text is escaped",
			input: `2 < 3 & 4 > 1`,
			want:  `2 &lt; 3 &amp; 4 &gt; 1`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(hackathonRichTextHTML(tt.input)); got != tt.want {
				t.Fatalf("hackathonRichTextHTML() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHackathonDescriptionHTML(t *testing.T) {
	markdown := string(hackathonDescriptionHTML("A **bold** [link](https://example.com).\n\n<script>bad()</script>", getters.CompetitionDescriptionFormatMarkdown))
	for _, want := range []string{
		"<strong>bold</strong>",
		`<a href="https://example.com" rel="noopener noreferrer">link</a>`,
		"&amp;lt;script&amp;gt;bad()&amp;lt;/script&amp;gt;",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown description missing %q in %q", want, markdown)
		}
	}
	if strings.Contains(markdown, "<script>") {
		t.Fatalf("markdown description rendered raw script: %q", markdown)
	}

	heading := string(hackathonDescriptionHTML("# Project\n\nBody", getters.CompetitionDescriptionFormatMarkdown))
	if !strings.Contains(heading, `<h1>Project</h1>`) {
		t.Fatalf("markdown heading missing h1 in %q", heading)
	}

	defaultMarkdown := string(hackathonDescriptionHTML("# Project", ""))
	if !strings.Contains(defaultMarkdown, `<h1>Project</h1>`) {
		t.Fatalf("default description format should render markdown, got %q", defaultMarkdown)
	}

	plain := string(hackathonDescriptionHTML("2 < 3\nnext", getters.CompetitionDescriptionFormatPlain))
	if plain != "2 &lt; 3<br>next" {
		t.Fatalf("plain description = %q", plain)
	}

	html := string(hackathonDescriptionHTML(`<p><em>ok</em></p><script>bad()</script>`, getters.CompetitionDescriptionFormatHTML))
	if html != "<p><em>ok</em></p>" {
		t.Fatalf("html description = %q", html)
	}
}

func TestHackathonScoreSummaries(t *testing.T) {
	n1, n2 := 1, 2
	rankOne, rankTwo := 1, 2
	projects := []*types.HackathonProject{
		{ID: "low", Title: "Low Project", ProjectNumber: &n2},
		{ID: "high", Title: "High Project", ProjectNumber: &n1},
		{ID: "empty", Title: "Empty Project"},
	}
	events := []*types.JudgeEvent{{ID: "expo", PlaybookType: getters.JudgeTypeExpo, RankLimit: 4}}
	scorecards := []*types.Scorecard{
		{
			ProjectID:    "low",
			JudgeEventID: "expo",
			Rank:         &rankTwo,
		},
		{
			ProjectID:    "high",
			JudgeEventID: "expo",
			Rank:         &rankOne,
		},
	}
	summaries := hackathonScoreSummaries(projects, scorecards, events)
	if len(summaries) != 3 {
		t.Fatalf("summaries len = %d, want 3", len(summaries))
	}
	if summaries[0].ProjectID != "high" || summaries[0].Points != 4 {
		t.Fatalf("first summary = %+v, want high score", summaries[0])
	}
	if summaries[1].ProjectID != "low" || summaries[1].Points != 3 || summaries[1].RankAverage != "2.0" {
		t.Fatalf("second summary = %+v, want low project rank data", summaries[1])
	}
	if summaries[2].ProjectID != "empty" || summaries[2].PointsLabel != "-" || summaries[2].Scorecards != 0 {
		t.Fatalf("third summary = %+v, want empty project last", summaries[2])
	}
}

func TestCurrentJudgeEvents(t *testing.T) {
	manual := &types.HackathonCompetition{JudgingMode: getters.CompetitionJudgingModeManual}
	events := []*types.JudgeEvent{
		{ID: "pending", State: getters.JudgeEventStatePending},
		{ID: "open", State: getters.JudgeEventStateOpen},
	}
	if got := currentJudgeEvents(manual, events, time.Now()); len(got) != 1 || got[0].ID != "open" {
		t.Fatalf("current events = %+v, want open", got)
	}

	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	before := now.Add(-time.Hour)
	after := now.Add(time.Hour)
	scheduled := []*types.JudgeEvent{{ID: "scheduled", StartsAt: &before, EndsAt: &after}}
	if got := currentJudgeEvents(manual, scheduled, now); len(got) != 0 {
		t.Fatalf("manual scheduled event without open state = %+v, want none", got)
	}
	automatic := &types.HackathonCompetition{JudgingMode: getters.CompetitionJudgingModeAutomatic}
	if got := currentJudgeEvents(automatic, scheduled, now); len(got) != 1 || got[0].ID != "scheduled" {
		t.Fatalf("automatic scheduled current events = %+v, want scheduled", got)
	}
}

func TestJudgingResultEvents(t *testing.T) {
	competition := &types.HackathonCompetition{JudgingMode: getters.CompetitionJudgingModeManual}
	events := []*types.JudgeEvent{
		{ID: "pending-expo", PlaybookType: getters.JudgeTypeExpo, State: getters.JudgeEventStatePending},
		{ID: "open-expo", PlaybookType: getters.JudgeTypeExpo, State: getters.JudgeEventStateOpen},
		{ID: "closed-expo", PlaybookType: getters.JudgeTypeExpo, State: getters.JudgeEventStateClosed},
		{ID: "closed-finals", PlaybookType: getters.JudgeTypeFinals, State: getters.JudgeEventStateClosed},
	}
	now := time.Now()

	judgeEvents := judgingResultEvents(
		competition,
		events,
		types.HackathonViewer{PersonID: "judge"},
		map[string]bool{getters.JudgeTypeExpo: true},
		now,
	)
	if len(judgeEvents) != 1 || judgeEvents[0].ID != "closed-expo" {
		t.Fatalf("judge result events = %+v, want only closed expo event", judgeEvents)
	}

	managerEvents := judgingResultEvents(
		competition,
		events,
		types.HackathonViewer{Manager: true},
		nil,
		now,
	)
	if len(managerEvents) != 2 || managerEvents[1].ID != "closed-finals" {
		t.Fatalf("manager result events = %+v, want every closed event", managerEvents)
	}

	if selected := selectedJudgingResultEvent(competition, judgeEvents, "closed-expo", now); selected == nil || selected.ID != "closed-expo" {
		t.Fatalf("requested result event = %+v, want closed-expo", selected)
	}
	if selected := selectedJudgingResultEvent(competition, judgeEvents, "", now); selected == nil || selected.ID != "closed-expo" {
		t.Fatalf("default result event = %+v, want closed-expo", selected)
	}
}

func TestApplyJudgeEventDeliberation(t *testing.T) {
	one := &HackathonScoreSummary{ProjectID: "one", ScoredScorecards: 2}
	two := &HackathonScoreSummary{ProjectID: "two", ScoredScorecards: 1}
	unscored := &HackathonScoreSummary{ProjectID: "unscored"}
	advanceCount := 1
	deliberation := &types.JudgeEventDeliberation{
		ProjectOrder: []string{"two", "one"},
		AdvanceCount: &advanceCount,
		Revision:     3,
	}

	ordered, gotCount, revision := applyJudgeEventDeliberation([]*HackathonScoreSummary{one, two, unscored}, deliberation, true)
	if len(ordered) != 3 || ordered[0].ProjectID != "two" || ordered[1].ProjectID != "one" || ordered[2].ProjectID != "unscored" {
		t.Fatalf("deliberation order = %+v, want two, one, unscored", ordered)
	}
	if gotCount != 1 || revision != 3 || !ordered[1].CutoffBefore {
		t.Fatalf("deliberation cutoff = count %d revision %d rows %+v", gotCount, revision, ordered)
	}

	finalOrder, gotCount, _ := applyJudgeEventDeliberation(ordered, deliberation, false)
	if gotCount != 0 {
		t.Fatalf("final round advance count = %d, want 0", gotCount)
	}
	for _, summary := range finalOrder {
		if summary.CutoffBefore {
			t.Fatalf("final round retained cutoff on %+v", summary)
		}
	}
}

func TestValidateDeliberationProjectOrder(t *testing.T) {
	summaries := []*HackathonScoreSummary{
		{ProjectID: "one", ScoredScorecards: 2},
		{ProjectID: "two", ScoredScorecards: 1},
		{ProjectID: "unscored"},
	}
	ordered, err := validateDeliberationProjectOrder(summaries, []string{"two", "one"})
	if err != nil || len(ordered) != 2 || ordered[0] != "two" {
		t.Fatalf("valid project order = %v, %v", ordered, err)
	}
	for _, invalid := range [][]string{{"one"}, {"one", "one"}, {"one", "unscored"}, {"one", "unknown"}} {
		if _, err := validateDeliberationProjectOrder(summaries, invalid); err == nil {
			t.Fatalf("invalid project order %v was accepted", invalid)
		}
	}
}

func TestProjectsForJudgeEventResultsKeepsScoredEliminations(t *testing.T) {
	projects := []*types.HackathonProject{
		{ID: "advanced", Status: getters.ProjectStatusAdvanced},
		{ID: "eliminated", Status: getters.ProjectStatusSubmitted},
		{ID: "unrelated", Status: getters.ProjectStatusSubmitted},
	}
	events := []*types.JudgeEvent{
		{ID: "expo"},
		{ID: "finals"},
	}
	scorecards := []*types.Scorecard{{JudgeEventID: "finals", ProjectID: "eliminated"}}

	got := projectsForJudgeEventResults(projects, events, "finals", scorecards)
	if len(got) != 2 || got[0].ID != "advanced" || got[1].ID != "eliminated" {
		t.Fatalf("result projects = %+v, want advanced and scored eliminated projects", got)
	}
}

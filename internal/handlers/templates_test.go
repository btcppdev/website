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
	mainNav, err := os.ReadFile("templates/section/main_nav.tmpl")
	if err != nil {
		t.Fatalf("read global navigation: %v", err)
	}
	for _, expected := range []string{
		`<details class="site-nav__events-menu">`,
		`<summary>/events <i aria-hidden="true">⌄</i></summary>`,
		`<a href="/events">/attend</a>`,
		`<a href="/talk">/speak</a>`,
		`<a href="/sponsor">/sponsor</a>`,
		`<a href="/volunteer">/volunteer</a>`,
	} {
		if !strings.Contains(string(mainNav), expected) {
			t.Fatalf("global navigation omitted %q", expected)
		}
	}
	for _, name := range []string{"reauth.tmpl", "developers_api.tmpl", "dashboard_hackathons.tmpl", "dashboard_org_discover.tmpl", "dashboard_sponsor.tmpl", "dashboard_sponsor_projects.tmpl", "sponsor_invite.tmpl", "hackathon.tmpl", "hackathon_judging.tmpl", "hackathon_project.tmpl", "hackathon_schedule.tmpl", "admin/hackathon_projects.tmpl", "admin/hackathon_judging.tmpl", "admin/hackathon_managers.tmpl", "admin/hackathon_scores.tmpl", "admin/hackathon_awards.tmpl", "admin/subscribers.tmpl", "admin/global_discounts.tmpl", "admin/inline_missive.tmpl", "admin/templated_missives_index.tmpl", "admin/conference_missives.tmpl"} {
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
	var reauthPage bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&reauthPage, "reauth.tmpl", &ReauthenticationPage{
		Next: "/dashboard/settings?resume=passkey-register", CancelURL: "/dashboard/settings", PersonName: "Mara Chen", Email: "mara@example.test", CSRF: "csrf",
		Preferred: &ReauthenticationMethodView{Key: "passkey", Label: "Use your passkey", Description: "Touch ID"}, PreferredWasLast: true,
		Alternatives: []*ReauthenticationMethodView{{Key: "nostr", Label: "Sign with Nostr", Description: "NIP-07"}}, DevLoginEnabled: true,
	}); err != nil {
		t.Fatalf("render reauthentication prompt: %v", err)
	}
	for _, expected := range []string{"Confirm it’s <em>you.</em>", "last used", "Use your passkey", "Use another sign-in method", "development email login-skip", `value="dev-reauth"`} {
		if !strings.Contains(reauthPage.String(), expected) {
			t.Fatalf("reauthentication prompt omitted %q: %s", expected, reauthPage.String())
		}
	}
	var signerContinue bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&signerContinue, "managed_signer_continue.tmpl", &ManagedSignerAuthorizationPage{ReturnTo: "https://signer.example/api/authorizations/callback", Token: "one-time-token", Year: 2026}); err != nil {
		t.Fatalf("render managed signer continuation: %v", err)
	}
	for _, expected := range []string{"Returning to your signer…", "You’ll continue automatically.", `src="/static/js/managed-signer-continue.js"`, `data-managed-signer-continue`, `action="https://signer.example/api/authorizations/callback"`, `value="one-time-token"`} {
		if !strings.Contains(signerContinue.String(), expected) {
			t.Fatalf("managed signer continuation omitted %q: %s", expected, signerContinue.String())
		}
	}
	var signerSetup bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&signerSetup, "managed_signer_authorize.tmpl", &ManagedSignerAuthorizationPage{
		PersonName: "Mara Chen", Tenant: "organization", TenantName: "Signet Systems", Role: "manager", Action: "create_identity", ActionLabel: "create a new Nostr signer", CSRF: "csrf", ReturnTo: "https://signer.example/api/authorizations/callback", Year: 2026,
	}); err != nil {
		t.Fatalf("render managed signer setup authorization: %v", err)
	}
	for _, expected := range []string{"Create a Nostr signer for Signet Systems?", "dedicated Nostr identity for <strong>Signet Systems</strong>", "Why this is needed", "Every Nostr badge must be cryptographically signed.", "without sharing an <code>nsec</code>", "What happens next", "The Bitcoin++ website never receives the private key or your passphrase.", "organization", "Signet Systems", "your role", "manager", "create a new Nostr signer", "Continue to signer setup →", "Cancel setup"} {
		if !strings.Contains(signerSetup.String(), expected) {
			t.Fatalf("managed signer setup omitted %q: %s", expected, signerSetup.String())
		}
	}
	var combinedLogin bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&combinedLogin, "managed_signer_authorize.tmpl", &ManagedSignerAuthorizationPage{
		PersonName: "Mara Chen", Tenant: "organization", TenantName: "Signet Systems", Role: "manager", Action: "connect_login", ActionLabel: "connect Badge Studio and sign in", ApplicationURL: "https://badges.example/api/auth/session", EventKind: 27235, EventHash: strings.Repeat("a", 64), Target: strings.Repeat("b", 48), CSRF: "csrf", ReturnTo: "https://signer.example/api/authorizations/callback", Year: 2026,
	}); err != nil {
		t.Fatalf("render combined signer authorization: %v", err)
	}
	for _, expected := range []string{"Connect and sign in to Badge Studio?", "Connect Badge Studio", "Sign in once", "https://badges.example/api/auth/session", "exact connection request hash", "connect Badge Studio and sign in", "Connect &amp; sign in →"} {
		if !strings.Contains(combinedLogin.String(), expected) {
			t.Fatalf("combined signer authorization omitted %q: %s", expected, combinedLogin.String())
		}
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
	for _, expected := range []string{"NEW ACCOUNT", "Finish setting up", "GitHub sign-in is verified", "CREATE ACCOUNT", "profile-edit-actions__create-account", `name="Subscribe" value="on" checked`, "newsletter!", `action="/logout"`, `name="csrf" value="cancel-csrf"`} {
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
	if !strings.Contains(accountSetupPage.String(), `name="Subscribe" value="on" checked`) {
		t.Fatal("organization invitation signup omitted newsletter opt-in")
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

	if strings.Contains(accountSetupPage.String(), `name="Subscribe"`) {
		t.Fatal("admin profile creation exposed personal newsletter opt-in")
	}
	accountSetupPage.Reset()
	if err := inlineTemplates.ExecuteTemplate(&accountSetupPage, "dashboard_edit_speaker.tmpl", &EditSpeakerPage{Mode: "edit", Speaker: &types.Speaker{}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(accountSetupPage.String(), `name="Subscribe"`) {
		t.Fatal("profile edits exposed signup newsletter opt-in")
	}
	profileCSS, err := os.ReadFile("static/css/custom.css")
	if err != nil {
		t.Fatalf("read profile stylesheet: %v", err)
	}
	if !strings.Contains(string(profileCSS), ".profile-edit-preview.hidden {\n\tdisplay: none;\n}") {
		t.Fatal("profile photo preview is not hidden before a file is selected")
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
	if strings.Contains(settingsPage.String(), `class="is-link">Resend email</button>`) {
		t.Fatalf("account settings rendered resend email as an undersized link control: %s", settingsPage.String())
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
		t.Fatalf("non-accounts-admin settings exposed shop accounting scope: %s", settingsPage.String())
	}
	settingsPage.Reset()
	if err := inlineTemplates.ExecuteTemplate(&settingsPage, "dashboard_person_emails.tmpl", &PersonEmailsPage{IsGlobalAdmin: true, IsAccountsAdmin: true}); err != nil {
		t.Fatalf("render admin settings: %v", err)
	}
	for _, expected := range []string{
		`value="shop:accounting:read"`,
		`for="oauth-scope-profile-write"`,
		`id="oauth-scope-profile-write" type="checkbox" name="scopes"`,
	} {
		if !strings.Contains(settingsPage.String(), expected) {
			t.Fatalf("admin account settings omitted %q: %s", expected, settingsPage.String())
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
	firstRank, secondRank, thirdRank := 1, 2, 3
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
		Awards: []*types.Award{
			{ID: "third-place", Title: "Third place", AwardRank: &thirdRank},
			{ID: "sponsor-bounty-id", Title: "Build the future", SponsoredByOrgID: "sponsor-org"},
			{ID: "first-place", Title: "First place", AwardRank: &firstRank},
			{ID: "second-place", Title: "Second place", AwardRank: &secondRank},
		},
		OrgsByID: map[string]*types.Org{
			"sponsor-org": {Name: "Future Sponsor", Website: "https://sponsor.example", LogoDark: "https://sponsor.example/logo.svg"},
		},
		PrizesByAward: map[string][]*types.Prize{
			"sponsor-bounty-id": {{Title: "Cash prize", ValueText: "1750000"}},
			"first-place":       {{Title: "First prize", ValueText: "3000000"}},
			"second-place":      {{Title: "Second prize", ValueText: "2000000"}},
			"third-place":       {{Title: "Third prize", ValueText: "1000000"}},
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
		`class="hack-award-podium"`,
		`hack-award-card--place-1`,
		`hack-award-card--place-2`,
		`hack-award-card--place-3`,
		`class="hack-award-card__trophy"`,
		`class="hack-award-card__place">1st</div>`,
		`id="bounty-build-the-future"`,
		`id="award-build-the-future"`,
		`href="/toronto/hackathon/awards/build-the-future"`,
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
	for _, unwanted := range []string{`id="bounty-sponsor-bounty-id"`, `id="award-sponsor-bounty-id"`, `hack-award-card__legacy-anchor`} {
		if strings.Contains(hackathonPage.String(), unwanted) {
			t.Fatalf("hackathon still renders legacy award-link infrastructure %q: %s", unwanted, hackathonPage.String())
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
		Competition: &types.HackathonCompetition{ID: "competition-id", ConferenceID: "conference-id", Title: "Hackathon"},
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
	for _, want := range []string{`name="Status" value="submitted"`, `name="ProjectNumber" value="7"`, `bypasses the submission deadline`, `action="/toronto/admin/hackathon/projects/draft-project/members"`, `data-person-picker-name="PersonID"`, `data-person-picker-max="1"`, `>Add member</button>`} {
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
	if !strings.Contains(nav.String(), `href="/">/home</a>`) || !strings.Contains(nav.String(), `href="/events">/attend</a>`) || strings.Contains(nav.String(), `href="/events">/events</a>`) || strings.Contains(nav.String(), `href="/#events"`) || strings.Contains(nav.String(), `href="/timeline"`) {
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
	if strings.Contains(dashboardTabs.String(), `href="/dashboard/sponsor"`) {
		t.Fatalf("ordinary dashboard tabs expose sponsors: %s", dashboardTabs.String())
	}
	if !strings.Contains(dashboardTabs.String(), `href="/dashboard/orgs" class="dashboard-tab"`) {
		t.Fatalf("dashboard tabs omitted universal organizations tab: %s", dashboardTabs.String())
	}
	if !strings.Contains(dashboardTabs.String(), `href="/dashboard/settings" class="dashboard-tab"`) {
		t.Fatalf("dashboard tabs omitted settings: %s", dashboardTabs.String())
	}
	if !strings.Contains(dashboardTabs.String(), `class="dashboard-tabs"`) {
		t.Fatalf("dashboard tabs were not rendered: %s", dashboardTabs.String())
	}
	dashboardTabs.Reset()
	if err := ctx.TemplateCache.ExecuteTemplate(&dashboardTabs, "dashboard_tabs", map[string]any{
		"Active":       "sponsors",
		"ShowSponsors": true,
	}); err != nil {
		t.Fatalf("render sponsor manager dashboard_tabs: %v", err)
	}
	if !strings.Contains(dashboardTabs.String(), `href="/dashboard/sponsor" class="dashboard-tab is-active" aria-current="page">/sponsors`) {
		t.Fatalf("organization manager dashboard tabs omit active sponsors: %s", dashboardTabs.String())
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
		"Active": "orgs",
	}); err != nil {
		t.Fatalf("render organization dashboard_tabs: %v", err)
	}
	if !strings.Contains(dashboardTabs.String(), `href="/dashboard/orgs" class="dashboard-tab is-active" aria-current="page"`) {
		t.Fatalf("organizations dashboard tab is not active: %s", dashboardTabs.String())
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
		HasHackathonProjects: true,
	}); err != nil {
		t.Fatalf("render global admin dashboard: %v", err)
	}
	for _, want := range []string{`href="/dashboard/hackathons"`, `href="/dashboard/orgs"`, `href="/admin" class="dashboard-tab is-active" aria-current="page"`, `href="/dashboard/settings"`, `class="profile-edit-tag">§ Global workspace · global-admin`, `<h1>Site <span>administration.</span></h1>`} {
		if !strings.Contains(adminDashboard.String(), want) {
			t.Fatalf("global admin dashboard omitted %q: %s", want, adminDashboard.String())
		}
	}
	if strings.Contains(adminDashboard.String(), `dashboard-workspace-hero__mark`) {
		t.Fatalf("global admin dashboard retained workspace mark: %s", adminDashboard.String())
	}
	var personalDashboard bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&personalDashboard, "dashboard.tmpl", &DashboardPage{
		Name: "Ada", Stats: &DashboardStats{},
		PendingSpeakerInvitations: []*types.Proposal{{
			ID: "speaker-invite-id", Title: types.PlaceholderTitlePrefix + "Ada)",
			Status: "Invited", InviteToken: "invite-token",
			ScheduleFor: &types.Conf{Tag: "dev26", Desc: "Local Dev", DateDesc: "Oct 2026", Location: "Austin"},
		}},
	}); err != nil {
		t.Fatalf("render dashboard speaker invitation: %v", err)
	}
	for _, want := range []string{"Action needed · speaker invitation", "You’re invited to speak", "Local Dev speaker invitation", `href="/invite-speaker/speaker-invite-id?t=invite-token"`, "Complete invitation", `action="/dashboard/talks/speaker-invite-id/decline"`} {
		if !strings.Contains(personalDashboard.String(), want) {
			t.Fatalf("dashboard speaker invitation omitted %q: %s", want, personalDashboard.String())
		}
	}

	var organizationsDashboard bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&organizationsDashboard, "dashboard_orgs.tmpl", &OrganizationDashboardIndexPage{
		Memberships: []*types.OrganizationMembership{{
			OrganizationID: "org-id", Role: getters.OrganizationRoleManager,
			Organization: &types.Org{Ref: "org-id", Slug: "signet-systems", Name: "Signet Systems", Tagline: "Test networks"},
		}},
		PendingInvites: []*types.OrganizationMemberInvite{{
			ID: "pending-invite-id", OrganizationName: "NDK Project", Email: "ada@example.test",
			Role: getters.OrganizationRoleMember, ExpiresAt: time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC),
		}},
		Directory: []*types.OrganizationDirectoryEntry{
			{Organization: &types.Org{Ref: "open-org", Slug: "open-builders", Name: "Open Builders", Tagline: "Anyone can join", MembershipPolicy: getters.OrganizationMembershipPolicyOpen}},
			{Organization: &types.Org{Ref: "request-org", Slug: "review-group", Name: "Review Group", MembershipPolicy: getters.OrganizationMembershipPolicyRequest}, RequestStatus: "pending"},
		},
		Applications:   []*types.OrganizationApplication{{ID: "application-id", Name: "Nairobi BitDevs", Status: "pending"}},
		Badges:         &WhoIsBadgeProfile{Issued: []WhoIsIssuedBadge{{Definition: WhoIsBadgeDefinition{Name: "Mentor", Description: "Shared knowledge", ImageURL: "https://cdn.example/mentor.png"}, CredentialURL: "https://badges.example/credentials/award/recipient", ClaimURL: "https://badges.example/api/auth/btcpp/continue?return_to=%2Fclaim%2Faward"}}},
		PersonalGrants: []*types.OrganizationBadgeGrant{{BadgeName: "Host", BadgeImageURL: "https://cdn.example/host.png", State: getters.BadgeGrantStateGranted}}, BadgeStudioURL: "https://badges.example",
		PendingOrgInviteCount: 1, HasHackathonProjects: true, ShowSponsors: true, SpacesReady: true, ManagedCount: 1, CSRF: "org-index-csrf",
	}); err != nil {
		t.Fatalf("render organizations dashboard: %v", err)
	}
	for _, want := range []string{`class="dashboard-tab__badge"`, `>1</span>`, `href="/dashboard/hackathons"`, "Your organizations.", "Pending invitations.", "NDK Project", "Invited as member via ada@example.test", `action="/dashboard/orgs/invites/pending-invite-id/accept"`, `name="csrf" value="org-index-csrf"`, "Badge inbox", "Mentor", "Shared knowledge", "Accept on Nostr", "Host", "Add or verify your Nostr key", "Signet Systems", "Test networks", "§ manager", `href="/dashboard/orgs/signet-systems"`, "Manage organization", "Discover organizations.", "Open Builders", `action="/dashboard/orgs/open-builders/membership-requests"`, "Join now", "Request awaiting review", `method="GET" action="/dashboard/orgs/discover" role="search"`, `class="organization-directory-search-row"`, `class="organization-directory-search-button" type="submit"`, `href="/dashboard/orgs/discover">View all organizations`, "Propose an organization.", `action="/dashboard/orgs/applications"`, `enctype="multipart/form-data"`, `name="LogoLightFile" type="file" accept="image/png,image/jpeg,image/gif,image/webp,image/svg+xml" required`, `name="LogoDarkFile" type="file" accept="image/png,image/jpeg,image/gif,image/webp,image/svg+xml" required`, "Both variants are required.", "Nairobi BitDevs"} {
		if !strings.Contains(organizationsDashboard.String(), want) {
			t.Fatalf("organizations dashboard omitted %q: %s", want, organizationsDashboard.String())
		}
	}
	var organizationsDashboardWithoutUploads bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&organizationsDashboardWithoutUploads, "dashboard_orgs.tmpl", &OrganizationDashboardIndexPage{}); err != nil {
		t.Fatalf("render organizations dashboard without uploads: %v", err)
	}
	if !strings.Contains(organizationsDashboardWithoutUploads.String(), "Organization proposals are temporarily unavailable because logo uploads are not configured.") || strings.Contains(organizationsDashboardWithoutUploads.String(), `action="/dashboard/orgs/applications"`) {
		t.Fatalf("organizations dashboard exposed proposal form without logo uploads: %s", organizationsDashboardWithoutUploads.String())
	}
	var organizationDirectory bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&organizationDirectory, "dashboard_org_discover.tmpl", &OrganizationDirectoryPage{
		Directory: []*types.OrganizationDirectoryEntry{{
			Organization: &types.Org{Ref: "open-org", Slug: "open-builders", Name: "Open Builders", Tagline: "Anyone can join", MembershipPolicy: getters.OrganizationMembershipPolicyOpen},
		}},
		Search: "Open", PendingOrgInviteCount: 1, HasHackathonProjects: true, ShowSponsors: true, IsGlobalAdmin: true, CSRF: "directory-csrf",
	}); err != nil {
		t.Fatalf("render organization directory: %v", err)
	}
	for _, want := range []string{`href="/dashboard/orgs">← Back to your organizations</a>`, `action="/dashboard/orgs/discover"`, `name="q" type="search" value="Open"`, `class="organization-directory-search-row"`, `class="organization-directory-search-button" type="submit"`, `href="/dashboard/orgs/discover">Clear search</a>`, "Showing 1 result", "Open Builders", `action="/dashboard/orgs/open-builders/membership-requests"`, `name="csrf" value="directory-csrf"`} {
		if !strings.Contains(organizationDirectory.String(), want) {
			t.Fatalf("organization directory omitted %q: %s", want, organizationDirectory.String())
		}
	}
	if strings.Contains(organizationDirectory.String(), "autofocus") {
		t.Fatalf("organization directory search unexpectedly autofocuses on mobile: %s", organizationDirectory.String())
	}
	for _, want := range []string{`href="/organizations/open-builders"`, "View public profile"} {
		if !strings.Contains(organizationDirectory.String(), want) {
			t.Fatalf("organization directory omitted public profile affordance %q: %s", want, organizationDirectory.String())
		}
	}

	var organizationProfile bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&organizationProfile, "organization_profile.tmpl", &OrganizationProfilePage{
		Organization: &types.Org{Ref: "org-id", Slug: "signet-systems", Name: "Signet Systems", Tagline: "Test networks", LogoLight: "/logo-light.svg", Website: "https://signet.example", Nostr: "npub1example"},
		BadgeCatalog: &OrganizationBadgeCatalog{IssuerPubkey: strings.Repeat("a", 64), CatalogURL: "https://badges.btcpp.dev/organizations/issuer", Badges: []WhoIsBadgeDefinition{{Name: "Relay Operator", Description: "Keeps packets moving.", ImageURL: "https://cdn.example/badge.png"}}},
	}); err != nil {
		t.Fatalf("render public organization profile: %v", err)
	}
	for _, want := range []string{"bitcoin++ organization", "Signet Systems", "Test networks", "published badge definitions", "Relay Operator", "Keeps packets moving.", "verify in badge studio", "not automatically claimable"} {
		if !strings.Contains(organizationProfile.String(), want) {
			t.Fatalf("public organization profile omitted %q: %s", want, organizationProfile.String())
		}
	}
	var unavailableOrganizationProfile bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&unavailableOrganizationProfile, "organization_profile.tmpl", &OrganizationProfilePage{
		Organization: &types.Org{Ref: "org-id", Name: "Signet Systems"}, BadgeCatalogUnavailable: true,
	}); err != nil {
		t.Fatalf("render unavailable public organization profile: %v", err)
	}
	if !strings.Contains(unavailableOrganizationProfile.String(), "temporarily unavailable") {
		t.Fatalf("public organization profile omitted fail-open catalog state: %s", unavailableOrganizationProfile.String())
	}

	organizationDashboardPage := &OrganizationDashboardPage{
		Membership:   &types.OrganizationMembership{OrganizationID: "org-id", PersonID: "owner-id", Role: getters.OrganizationRoleOwner},
		Organization: &types.Org{Ref: "org-id", Slug: "signet-systems", Name: "Signet Systems", Tagline: "Test networks", LogoLight: "/logo-light.svg", LogoDark: "/logo-dark.svg", Github: "https://github.com/example", MembershipPolicy: getters.OrganizationMembershipPolicyRequest},
		Memberships: []*types.OrganizationMembership{{
			OrganizationID: "org-id", Organization: &types.Org{Ref: "org-id", Slug: "signet-systems", Name: "Signet Systems"},
		}},
		Members: []*types.OrganizationMembership{
			{PersonID: "owner-id", Role: getters.OrganizationRoleOwner, Status: "active", PersonName: "Mara", PersonEmail: "mara@example.test"},
			{PersonID: "member-id", Role: getters.OrganizationRoleMember, Status: "active", PersonName: "Eli", PersonEmail: "eli@example.test"},
		},
		PendingInvites: []*types.OrganizationMemberInvite{{
			ID: "invite-id", Email: "pending@example.test", Role: getters.OrganizationRoleMember,
			ExpiresAt: time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC),
		}},
		PendingRequests:    []*types.OrganizationMembershipRequest{{ID: "request-id", PersonID: "requester-id", PersonName: "Rae", PersonEmail: "rae@example.test", Message: "I contribute", CreatedAt: time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC)}},
		SponsorEvents:      []*types.SponsorDashboardEvent{{Sponsorship: &types.Sponsorship{Ref: "sponsor-id"}}},
		BadgeCatalog:       &OrganizationBadgeCatalog{IssuerPubkey: strings.Repeat("a", 64), CatalogURL: "https://badges.example/organizations/issuer", Badges: []WhoIsBadgeDefinition{{Identifier: "mentor", Name: "Mentor", ImageURL: "https://cdn.example/mentor.png"}}},
		OrganizationGrants: []*types.OrganizationBadgeGrant{{ID: "grant-id", BadgeName: "Mentor", RecipientName: "Eli", RecipientPubkey: strings.Repeat("b", 64), State: getters.BadgeGrantStateReady}},
		BadgeStudioURL:     "https://badges.example", SignerURL: "https://signer.example",
		CanManage: true, IsOwner: true, HasHackathonProjects: true, ShowSponsors: true, SpacesReady: true, CSRF: "org-csrf",
		InviteLink: "http://localhost:8888/sponsor-invites/new-token", InviteEmail: "pending@example.test",
	}
	var organizationDashboard bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&organizationDashboard, "dashboard_org.tmpl", organizationDashboardPage); err != nil {
		t.Fatalf("render organization dashboard: %v", err)
	}
	for _, want := range []string{`href="/dashboard/hackathons"`, "Organization workspace · owner", `aria-label="Signet Systems workspace"`, `href="/dashboard/orgs/signet-systems" class="is-active" aria-current="page">/organization`, `href="/dashboard/orgs/signet-systems/badges">/badges`, "How Signet Systems appears.", `href="/organizations/signet-systems"`, "Your organization team.", "Mara", "Eli", "Add a teammate.", "Find a bitcoin++ person.", "Invite someone new.", "Search name, email, or phone", "Send invitation →", `src="/static/js/person-picker.js"`, `data-person-picker-search-url="/dashboard/orgs/signet-systems/people/search"`, `action="/dashboard/orgs/signet-systems/members"`, "Pending invitations", "pending@example.test", `action="/dashboard/orgs/signet-systems/profile"`, `action="/dashboard/orgs/signet-systems/invites"`, `action="/dashboard/orgs/signet-systems/invites/invite-id/replace"`, `action="/dashboard/orgs/signet-systems/invites/invite-id/revoke"`, `action="/dashboard/orgs/signet-systems/members/member-id/role"`, `action="/dashboard/orgs/signet-systems/members/member-id/remove"`, `href="/dashboard/sponsor/org-id"`, `name="csrf" value="org-csrf"`, "http://localhost:8888/sponsor-invites/new-token", "shown here once", "Who can join?", `action="/dashboard/orgs/signet-systems/membership-policy"`, "Managers approve each request", "Membership requests.", "Rae", "I contribute", `action="/dashboard/orgs/signet-systems/membership-requests/request-id"`, `class="organization-review-action is-primary"`, `class="organization-review-action is-secondary"`} {
		if !strings.Contains(organizationDashboard.String(), want) {
			t.Fatalf("organization dashboard omitted %q: %s", want, organizationDashboard.String())
		}
	}
	for _, excluded := range []string{"Badge catalog", "Open Badge Studio with bitcoin++", "Issue in Studio with bitcoin++"} {
		if strings.Contains(organizationDashboard.String(), excluded) {
			t.Fatalf("organization dashboard still includes badge management %q: %s", excluded, organizationDashboard.String())
		}
	}

	var organizationBadgeDashboard bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&organizationBadgeDashboard, "dashboard_org_badges.tmpl", organizationDashboardPage); err != nil {
		t.Fatalf("render organization badge dashboard: %v", err)
	}
	for _, want := range []string{`href="/dashboard/orgs/signet-systems">/organization`, `href="/dashboard/orgs/signet-systems/badges" class="is-active" aria-current="page">/badges`, `aria-label="Badge sections"`, "Badge catalog", "Mentor", `class="organization-badge-button is-secondary" href="https://badges.example/organizations/issuer"`, `class="organization-badge-button is-primary" href="https://badges.example/api/auth/btcpp/continue?return_to=%2F%3Fbtcpp_org%3Dorg-id"`, "Open Badge Studio ↗", "Issue &amp; manage", `class="organization-badge-button is-primary is-compact"`, "Issue in Studio ↗", `href="https://badges.example/api/auth/btcpp/continue?return_to=%2F%3Fbtcpp_org%3Dorg-id%26grant%3Dgrant-id"`, `class="organization-badge-button is-danger is-compact"`, "Cancel grant", `data-person-picker-search-url="/dashboard/orgs/signet-systems/people/search"`, `action="/dashboard/orgs/signet-systems/badge-grants"`, `action="/dashboard/orgs/signet-systems/badge-grants/grant-id/cancel"`} {
		if !strings.Contains(organizationBadgeDashboard.String(), want) {
			t.Fatalf("organization badge dashboard omitted %q: %s", want, organizationBadgeDashboard.String())
		}
	}
	if strings.Contains(organizationBadgeDashboard.String(), "Your organization team.") || strings.Contains(organizationBadgeDashboard.String(), "How Signet Systems appears.") {
		t.Fatalf("organization badge dashboard includes organization management sections: %s", organizationBadgeDashboard.String())
	}
	var memberOrganizationDashboard bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&memberOrganizationDashboard, "dashboard_org.tmpl", &OrganizationDashboardPage{
		Membership:    &types.OrganizationMembership{OrganizationID: "org-id", PersonID: "member-id", Role: getters.OrganizationRoleMember},
		Organization:  &types.Org{Ref: "org-id", Name: "Signet Systems"},
		SponsorEvents: []*types.SponsorDashboardEvent{{Sponsorship: &types.Sponsorship{Ref: "sponsor-id"}}},
	}); err != nil {
		t.Fatalf("render member organization dashboard: %v", err)
	}
	if strings.Contains(memberOrganizationDashboard.String(), `href="/dashboard/sponsor/org-id"`) || strings.Contains(memberOrganizationDashboard.String(), "Open sponsor workspace") {
		t.Fatalf("ordinary organization member sees sponsor workspace link: %s", memberOrganizationDashboard.String())
	}
	if strings.Contains(memberOrganizationDashboard.String(), `href="#profile"`) || strings.Contains(memberOrganizationDashboard.String(), "How Signet Systems appears.") {
		t.Fatalf("ordinary organization member sees organization profile management section: %s", memberOrganizationDashboard.String())
	}

	var organizationApplication bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&organizationApplication, "admin/organization_application.tmpl", &OrganizationApplicationAdminPage{Application: &types.OrganizationApplication{ID: "application-id", Name: "Nairobi BitDevs", ApplicantName: "Ada", ApplicantEmail: "ada@example.test", LogoLight: "https://cdn.example.test/light.svg", LogoDark: "https://cdn.example.test/dark.svg", Status: "pending", CreatedAt: time.Now()}}); err != nil {
		t.Fatalf("render organization application review: %v", err)
	}
	for _, want := range []string{"Review Nairobi BitDevs", "Submitted by Ada", `action="/admin/org-applications/application-id"`, "Proposed logo variants", "https://cdn.example.test/light.svg", "https://cdn.example.test/dark.svg", `value="approved"`, "Approve and create organization", `value="denied"`, "Deny application", `class="organization-review-action is-primary"`, `class="organization-review-action is-secondary"`} {
		if !strings.Contains(organizationApplication.String(), want) {
			t.Fatalf("organization application review omitted %q: %s", want, organizationApplication.String())
		}
	}

	var sponsorDashboard bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&sponsorDashboard, "dashboard_sponsor.tmpl", &SponsorDashboardPage{
		Membership:   &types.OrganizationMembership{PersonID: "owner-id", Role: getters.OrganizationRoleOwner},
		Organization: &types.Org{Ref: "org-id", Name: "Signet Systems", Tagline: "Test networks", LogoLight: "/logo-light.svg", LogoDark: "/logo-dark.svg"},
		Memberships:  []*types.OrganizationMembership{{OrganizationID: "org-id", Organization: &types.Org{Name: "Signet Systems"}}},
		SponsorMemberships: []*types.OrganizationMembership{
			{OrganizationID: "org-id", Organization: &types.Org{Ref: "org-id", Name: "Signet Systems", LogoLight: "/logo-light.svg"}},
			{OrganizationID: "other-org-id", Organization: &types.Org{Ref: "other-org-id", Name: "NDK Project"}},
		},
		HasHackathonProjects: true,
		IsGlobalAdmin:        true,
		Upcoming: []*types.SponsorDashboardEvent{{
			Sponsorship: &types.Sponsorship{Ref: "sponsorship-id", Level: "Headline", Status: "Paid"},
			Conference:  &types.Conf{Ref: "conference-id", Tag: "dev26", Desc: "Local Dev", DateDesc: "Oct 2026", Location: "Austin", ShowHackathon: true},
			Competition: &types.HackathonCompetition{ID: "competition-id", Title: "Local Hackathon"},
			Entitlement: &types.SponsorshipEntitlement{TicketAllocation: 20, SponsorAwardLimit: 2, ParticipantContactAccess: true},
			SpeakerApplications: []*types.SponsorSpeakerApplication{{
				ProposalID: "speaker-proposal-id", ConferenceID: "conference-id",
				Title: "Scaling Signet", TalkType: "Talk", Status: "InReview",
				DesiredMinutes: 30, MemberNames: []string{"Eli", "Mara"},
			}},
			TicketsIssued: 5,
			AwardCount:    1,
		}},
		Members: []*types.OrganizationMembership{
			{PersonID: "owner-id", Role: getters.OrganizationRoleOwner, Status: "active", PersonName: "Mara", PersonEmail: "mara@example.test"},
			{PersonID: "member-id", Role: getters.OrganizationRoleMember, Status: "active", PersonName: "Eli", PersonEmail: "eli@example.test"},
		},
		PrizeProposals: []*types.SponsorAwardProposal{{
			ID: "proposal-id", SponsorshipID: "sponsorship-id",
			ConferenceID: "conference-id", CompetitionID: "competition-id",
			ConferenceTitle: "Local Dev", CompetitionTitle: "Local Hackathon",
			Title: "Best Signet Infrastructure", Description: "Make signet easier to use.",
			JudgingInstructions: "Prefer working demos", MaxAwardees: 1,
			OptInRequired: true, PrizeType: getters.PrizeTypeSats,
			PrizeTitle: "1,000,000 sats", PrizeDescription: "Paid after the event.",
			PrizeValueText: "1000000", Status: "approved",
			EditableUntil: func() *time.Time { value := time.Date(2099, time.October, 1, 9, 0, 0, 0, time.UTC); return &value }(),
		}, {
			AwardID: "organizer-award-id", OrganizationID: "org-id",
			ConferenceID: "conference-id", CompetitionID: "competition-id",
			ConferenceTitle: "Local Dev", CompetitionTitle: "Local Hackathon",
			Title: "Organizer-created challenge", MaxAwardees: 1, OptInRequired: true,
			PrizeType: getters.PrizeTypeSats, PrizeTitle: "250,000 sats",
			PrizeValueText: "250000", Status: "available", OrganizerManaged: true,
		}},
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
	for _, want := range []string{"Signet Systems", "20", "Opt-in only", "Sponsor workspaces", "Switch organization", `href="/dashboard/sponsor/org-id" class="sponsor-organization-switcher__item is-active" aria-current="page"`, "Current workspace", `href="/dashboard/sponsor/other-org-id"`, "NDK Project", "Open sponsorships", "Sponsor workspace sections", "Team speaker applications", "Scaling Signet", "Eli, Mara · Talk · 30 min", "In review", "Your issued challenges", "Make signet easier to use.", `class="sponsor-challenge-description"`, `action="/dashboard/sponsor/org-id/prize-proposals/proposal-id"`, "Save challenge", "Organizer-created challenge", "This challenge was created by bitcoin++ organizers and is shown here as read-only.", "width: 25%", "width: 50%", `href="/dashboard/hackathons"`, `href="/dashboard/orgs"`, `href="/admin"`, `name="csrf" value="sponsor-csrf"`, `action="/dashboard/sponsor/org-id/tickets"`, `action="/dashboard/sponsor/org-id/prize-proposals"`, `href="/dashboard/sponsor/org-id/projects"`, "View all 1 project", `href="/dashboard/sponsor/org-id/hackathon-projects.csv"`, `href="/dashboard/orgs/org-id"`, "Teams building for your challenges.", "Latest projects", "Fixture Forge", `href="/whois/mara"`, `href="mailto:mara@example.test"`, "Consented through this prize"} {
		if !strings.Contains(sponsorDashboard.String(), want) {
			t.Fatalf("sponsor dashboard omitted %q", want)
		}
	}
	if !strings.Contains(sponsorDashboard.String(), `href="/dashboard/sponsor" class="dashboard-tab is-active" aria-current="page">/sponsors`) {
		t.Fatalf("sponsor workspace does not mark sponsors tab active: %s", sponsorDashboard.String())
	}
	for _, unwanted := range []string{`action="/dashboard/sponsor/org-id/profile"`, `action="/dashboard/sponsor/org-id/invites"`, `action="/dashboard/sponsor/org-id/members/member-id/remove"`, "Public sponsor card preview"} {
		if strings.Contains(sponsorDashboard.String(), unwanted) {
			t.Fatalf("sponsor dashboard retained organization management control %q", unwanted)
		}
	}
	for _, unwanted := range []string{`name="LogoLight"`, `name="LogoDark"`} {
		if strings.Contains(sponsorDashboard.String(), unwanted) {
			t.Fatalf("sponsor dashboard still exposes raw logo URL input %q", unwanted)
		}
	}
	var sponsorProjectDirectory bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&sponsorProjectDirectory, "dashboard_sponsor_projects.tmpl", &SponsorDashboardPage{
		Organization: &types.Org{Ref: "org-id", Name: "Signet Systems", LogoLight: "/logo-light.svg"},
		Past:         []*types.SponsorDashboardEvent{{Conference: &types.Conf{Ref: "past-conf"}}},
		PrizeEntries: []*types.SponsorPrizeEntry{
			{ConferenceID: "current-conf", ConferenceTag: "dev26", ConferenceTitle: "Local Dev", ProjectID: "current-project", ProjectTitle: "Current Fixture", ProjectStatus: "submitted"},
			{ConferenceID: "past-conf", ConferenceTag: "past25", ConferenceTitle: "Past Dev", ProjectID: "past-project", ProjectTitle: "Archived Fixture", ProjectStatus: "submitted"},
		},
		CanViewAllHackathonSubmissions: true, CanExportParticipants: true, HasHackathonProjects: true, IsGlobalAdmin: true,
	}); err != nil {
		t.Fatalf("render sponsor project directory: %v", err)
	}
	for _, want := range []string{`href="/dashboard/sponsor/org-id">← Back to Signet Systems sponsor workspace</a>`, "All submitted projects.", "Current hackathons", "Current Fixture", "Past hackathons", "Archived Fixture", `href="/dashboard/sponsor/org-id/hackathon-projects.csv"`} {
		if !strings.Contains(sponsorProjectDirectory.String(), want) {
			t.Fatalf("sponsor project directory omitted %q: %s", want, sponsorProjectDirectory.String())
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
	for _, want := range []string{`name="TicketAllocation"`, `value="24"`, `>Tickets</th>`, `name="SponsorAwardLimit"`, `value="3"`, `name="ManagerPersonID"`, `name="ManagerName"`, `name="ManagerEmail"`, "secure 72-hour login link", "Manager access belongs to the organization, not this event", "current and future sponsorships", `data-search-url="/dev26/admin/sponsors/people/search"`, `name="CanEditOrganization"`, "Can edit organization"} {
		if !strings.Contains(sponsorEvents.String(), want) {
			t.Fatalf("event sponsorships omitted %q: %s", want, sponsorEvents.String())
		}
	}
	if strings.Contains(sponsorEvents.String(), "CanManageAwardJudges") || strings.Contains(sponsorEvents.String(), "manage prize judges") {
		t.Fatalf("event sponsorships exposed sponsor judge management: %s", sponsorEvents.String())
	}
	var socialPage bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&socialPage, "talks/social.tmpl", &SocialAdminPage{
		Conf: &types.Conf{Tag: "berlin26", Desc: "Berlin"}, BufferOK: true,
		SpeakerRows: []*SocialSpeakerRow{{ID: "speaker-id", Name: "Ada", PhotoURL: "/speaker-card.png", InstaPhotoURL: "/speaker-square.png", SpeakerPhotoURL: "/speaker-portrait.jpg"}},
		TalkRows:    []*SocialTalkRow{{ID: "talk-id", Name: "Bitcoin"}},
		SponsorRows: []*SocialSponsorRow{{Ref: "sponsor-id", OrgName: "Builders"}},
	}); err != nil {
		t.Fatalf("render social upload controls: %v", err)
	}
	for _, want := range []string{`data-media-upload-url="/berlin26/admin/social/media"`, `name="media_items_speaker_speaker-id"`, `name="media_items_talk_talk-id"`, `name="media_items_sponsor_sponsor-id"`, `data-source="photo"`, `src="/speaker-portrait.jpg"`, `src="/speaker-card.png"`, `src="/speaker-square.png"`, `src="/static/js/social-media-upload.js"`, "Review the media in posting order"} {
		if !strings.Contains(socialPage.String(), want) {
			t.Errorf("social page missing %q", want)
		}
	}

	var orgDetail bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&orgDetail, "sponsors/detail.tmpl", &OrgDetailPage{
		Org: &types.Org{Ref: "org-id", Name: "Signet Systems"},
		Members: []*types.OrganizationMembership{
			{PersonID: "manager-id", PersonName: "Mara Manager", PersonEmail: "mara@example.test", Role: getters.OrganizationRoleManager},
			{PersonID: "owner-id", PersonName: "Owen Owner", PersonEmail: "owen@example.test", Role: getters.OrganizationRoleOwner},
		},
		PendingInvites: []*types.OrganizationMemberInvite{{
			ID: "pending-invite-id", Email: "pending@example.test", Role: getters.OrganizationRoleManager,
			ExpiresAt: time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC),
		}},
		InviteLink:  "http://localhost:8888/sponsor-invites/replacement-token",
		InviteEmail: "pending@example.test",
	}); err != nil {
		t.Fatalf("render organization detail: %v", err)
	}
	for _, want := range []string{"Organization members", "current and future sponsor workspaces", "Mara Manager", "mara@example.test", "Owen Owner", "Pending invitations", "pending@example.test", "Expires Sep 6, 2026", "Add an existing account", "Invite by email", `data-person-picker-search-url="/api/people/search"`, `action="/admin/orgs/org-id/members"`, `action="/admin/orgs/org-id/members/manager-id/role"`, `action="/admin/orgs/org-id/members/manager-id/remove"`, `action="/admin/orgs/org-id/invites"`, `action="/admin/orgs/org-id/invites/pending-invite-id/role"`, `action="/admin/orgs/org-id/invites/pending-invite-id/replace"`, `action="/admin/orgs/org-id/invites/pending-invite-id/revoke"`, "Create new link", "http://localhost:8888/sponsor-invites/replacement-token", "shown once", "Show in organization directory", `name="DirectoryVisible"`, "hidden from dashboard discovery and search"} {
		if !strings.Contains(orgDetail.String(), want) {
			t.Fatalf("organization detail omitted %q: %s", want, orgDetail.String())
		}
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

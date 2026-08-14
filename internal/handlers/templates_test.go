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
	for _, name := range []string{"dashboard_hackathons.tmpl", "hackathon.tmpl", "hackathon_judging.tmpl", "hackathon_project.tmpl", "hackathon_schedule.tmpl", "admin/hackathon_projects.tmpl", "admin/hackathon_judging.tmpl", "admin/hackathon_managers.tmpl", "admin/hackathon_scores.tmpl", "admin/hackathon_awards.tmpl", "admin/subscribers.tmpl", "admin/global_discounts.tmpl", "admin/inline_missive.tmpl", "admin/templated_missives_index.tmpl", "admin/conference_missives.tmpl"} {
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
	if strings.Contains(projectPage.String(), `href="/whois/private-member"`) {
		t.Fatalf("hackathon project links a member without a public profile: %s", projectPage.String())
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
		Conf:          &types.Conf{Tag: "toronto"},
		Projects:      []*types.HackathonProject{{ID: "my-project", Title: "My Project"}},
		OwnedProjects: map[string]bool{"my-project": true},
		Viewer:        &auth.Identity{PersonID: "person-id"},
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
	var confHackathonSection bytes.Buffer
	if err := inlineTemplates.ExecuteTemplate(&confHackathonSection, "conf_hackathon_section", &ConfPage{
		Conf: &types.Conf{Tag: "toronto", Desc: "Bitcoin++ Toronto"},
		Hackathon: &types.HackathonCompetition{
			Title:             "Toronto Hackathon",
			LifecycleOverride: getters.CompetitionLifecycleOpen,
		},
		HackathonJudges: []*types.CompetitionJudge{{Name: "Judge One"}, {Name: "Judge Two"}},
		HackathonPlaceRows: []*HackathonPlaceRow{
			{PlaceLabel: "01", ProjectTitle: "First prize", Amount: "1,000,000 sats", GrandPrize: true},
			{PlaceLabel: "02", ProjectTitle: "Second prize", Amount: "500,000 sats"},
			{PlaceLabel: "03", ProjectTitle: "Third prize", Amount: "250,000 sats"},
		},
		HackathonPrizePoolSats: 1_750_000,
	}); err != nil {
		t.Fatalf("render conference hackathon section: %v", err)
	}
	for _, want := range []string{`aria-label="Hackathon at a glance"`, `>Submissions open</dd>`, `>3</dd>`, `<dt>Total prizes</dt>`, `>1.8M</dd>`, `>2</dd>`} {
		if !strings.Contains(confHackathonSection.String(), want) {
			t.Fatalf("conference hackathon stats missing %q: %s", want, confHackathonSection.String())
		}
	}
	confHackathonSection.Reset()
	if err := inlineTemplates.ExecuteTemplate(&confHackathonSection, "conf_hackathon_section", &ConfPage{
		Conf:      &types.Conf{Tag: "toronto", Desc: "Bitcoin++ Toronto"},
		Hackathon: &types.HackathonCompetition{Title: "Toronto Hackathon"},
	}); err != nil {
		t.Fatalf("render conference hackathon section without configured prizes: %v", err)
	}
	for _, want := range []string{`<dt>Prizes</dt>`, `<dd>Coming Soon</dd>`} {
		if !strings.Contains(confHackathonSection.String(), want) {
			t.Fatalf("conference hackathon fallback stat missing %q: %s", want, confHackathonSection.String())
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
		Form:                   GlobalDiscountForm{DiscountForm: DiscountForm{DiscountType: "percent", Amount: "50"}},
		SelectedConferenceRefs: map[string]bool{"seoul-id": true, "berlin-id": true},
	}); err != nil {
		t.Fatalf("render global discounts: %v", err)
	}
	for _, want := range []string{`action="/admin/discounts"`, `value="seoul-id" checked`, `value="50"`} {
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
	if !strings.Contains(nav.String(), `href="/toronto/hackathon"`) {
		t.Fatalf("live hackathon nav missing public hackathon link: %s", nav.String())
	}
	nav.Reset()
	if err := ctx.TemplateCache.ExecuteTemplate(&nav, "generic_conf_nav", &types.Conf{Tag: "toronto"}); err != nil {
		t.Fatalf("render generic_conf_nav without hackathon: %v", err)
	}
	if strings.Contains(nav.String(), `href="/toronto/hackathon"`) {
		t.Fatalf("inactive hackathon nav unexpectedly contains public hackathon link: %s", nav.String())
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
	if strings.Contains(dashboardTabs.String(), `class="dashboard-tabs"`) {
		t.Fatalf("single-section dashboard renders redundant tab navigation: %s", dashboardTabs.String())
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

func TestHackathonAdvancedSelections(t *testing.T) {
	n1, n2, n3 := 1, 2, 3
	rankOne, rankTwo := 1, 2
	projects := []*types.HackathonProject{
		{ID: "winner", Title: "Winner", ProjectNumber: &n1, Status: getters.ProjectStatusSubmitted},
		{ID: "runner-up", Title: "Runner Up", ProjectNumber: &n2, Status: getters.ProjectStatusAdvanced},
		{ID: "created", Title: "Created", ProjectNumber: &n3, Status: getters.ProjectStatusCreated},
	}
	events := []*types.JudgeEvent{
		{ID: "expo", PlaybookType: getters.JudgeTypeExpo, RankLimit: 4},
		{ID: "finals", PlaybookType: getters.JudgeTypeFinals, RankLimit: 4},
	}
	scorecards := []*types.Scorecard{
		{ProjectID: "winner", JudgeEventID: "expo", Rank: &rankOne},
		{ProjectID: "runner-up", JudgeEventID: "expo", Rank: &rankTwo},
		{ProjectID: "created", JudgeEventID: "expo", Rank: &rankOne},
		{ProjectID: "runner-up", JudgeEventID: "finals", Rank: &rankOne},
	}

	expoAdvanced := hackathonAdvancedSelections(projects, scorecards, events, hackathonScoreModeExpo, 2)
	if len(expoAdvanced) != 2 || expoAdvanced[0].ID != "winner" || expoAdvanced[1].ID != "runner-up" {
		t.Fatalf("expo advanced = %+v, want winner then runner-up", expoAdvanced)
	}

	finalsAdvanced := hackathonAdvancedSelections(projects, scorecards, events, hackathonScoreModeFinals, 2)
	if len(finalsAdvanced) != 1 || finalsAdvanced[0].ID != "runner-up" {
		t.Fatalf("finals advanced = %+v, want runner-up only", finalsAdvanced)
	}

	if advanced := hackathonAdvancedSelections(projects, scorecards, events, hackathonScoreModeExpo, 0); len(advanced) != 0 {
		t.Fatalf("zero advance count returned %+v, want empty", advanced)
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

package handlers

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/db"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/types"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
)

func projectCardFixture() (*config.AppContext, *HackathonPage, *types.HackathonProject) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	project := &types.HackathonProject{ID: "project", CompetitionID: "competition", Title: "BlackIce", ShortDescription: "Private payments, built together.", ImageURL: "/static/img/rebrand/hero-hackathon.png", Status: getters.ProjectStatusSubmitted, Tags: []string{"privacy", "FROST"}}
	page := &HackathonPage{Conf: &types.Conf{Tag: "seoul", Desc: "bitcoin++ Seoul, privacy edition", Location: "Seoul, South Korea", DateDesc: "Nov 5–6, 2026", PublicationStatus: "published", AccentColor: "#7240c8"}, Competition: &types.HackathonCompetition{ID: "competition", Title: "Seoul Hackathon", Visibility: getters.CompetitionVisibilityPublic, PublicGalleryEnabled: true}, Project: project, Projects: []*types.HackathonProject{project}, Members: []*types.ProjectMember{{ProjectID: "project", Name: "Alice"}, {ProjectID: "project", Name: "Bob"}}, Awards: []*types.Award{{ID: "first", Title: "First place", Status: getters.AwardStatusAwarded}}, AwardeesByAward: map[string][]*types.ProjectAward{"first": {{ProjectID: "project"}}}, PrizesByAward: map[string][]*types.Prize{"first": {{PrizeType: getters.PrizeTypeSats, ValueText: "2500000"}}}}
	page.ProjectMembersByProject = map[string][]*types.ProjectMember{"project": page.Members}
	return ctx, page, project
}

func TestHackathonProjectSocialCardPublication(t *testing.T) {
	ctx, page, project := projectCardFixture()
	before := projectSocialCard(ctx, page, project)
	if before.Kind != "project" || before.Title != "BlackIce" || before.Callout != "Alice, Bob" || len(before.Images) != 1 || len(before.Badges) != 0 || before.AwardRibbon != 0 {
		t.Fatalf("project card: %+v", before)
	}
	if before.EventEdition != "privacy edition" || before.EventDate != "Seoul, South Korea · Nov 5–6, 2026" {
		t.Fatalf("project event header: edition %q, location/date %q", before.EventEdition, before.EventDate)
	}
	prizeBefore := awardSocialCard(ctx, page, page.Awards[0])
	if prizeBefore.Kind != "award" || strings.Contains(prizeBefore.Title, "BlackIce") {
		t.Fatal("unannounced result leaked", prizeBefore)
	}
	now := time.Now()
	page.Competition.ResultsFinalizedAt = &now
	after := projectSocialCard(ctx, page, project)
	winner := awardSocialCard(ctx, page, page.Awards[0])
	if after.AwardRibbon != 1 || winner.AwardRibbon != 1 || len(after.Badges) != 0 || winner.Kind != "award-winner" || winner.Title != "BlackIce" || winner.TitleSuffix != "First place" || winner.Callout != "Alice, Bob" || winner.Value != "2.5M" || winner.ValueSuffix != "sats" {
		t.Fatalf("announced card: %+v", winner)
	}
	if imgproc.SiteSocialCardID(before) == imgproc.SiteSocialCardID(after) || imgproc.SiteSocialCardID(prizeBefore) == imgproc.SiteSocialCardID(winner) {
		t.Fatal("publishing did not change share image versions")
	}
	project.Title = "BlackIce v2"
	if imgproc.SiteSocialCardID(winner) == imgproc.SiteSocialCardID(awardSocialCard(ctx, page, page.Awards[0])) {
		t.Fatal("editing the winner did not refresh the prize card")
	}
	page.Competition.ResultsFinalizedAt = nil
	if imgproc.SiteSocialCardID(prizeBefore) != imgproc.SiteSocialCardID(awardSocialCard(ctx, page, page.Awards[0])) {
		t.Fatal("withdrawing publication did not restore prize card")
	}
}

func TestHackathonAwardSocialCardRetainsSponsorLogo(t *testing.T) {
	ctx, page, project := projectCardFixture()
	award := page.Awards[0]
	award.SponsoredByOrgID = "sponsor"
	org := &types.Org{Name: "Prize Sponsor", LogoLight: "/sponsor-light.svg", LogoDark: "/sponsor-dark.svg"}
	page.OrgsByID = map[string]*types.Org{"sponsor": org}
	before := awardSocialCard(ctx, page, award)
	now := time.Now()
	page.Competition.ResultsFinalizedAt = &now
	card := awardSocialCard(ctx, page, award)
	if len(card.SponsorLogos) != 1 || card.SponsorLogos[0] != before.Images[0] || card.SponsorNames[0] != org.Name || len(card.Images) != 1 || card.Images[0] == card.SponsorLogos[0] {
		t.Fatalf("winner lost sponsor identity or replaced project artwork: %+v", card)
	}
	html, err := imgproc.RenderSiteSocialCardHTML(card)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(html, []byte(`<aside class="project-sponsor"`)) || !bytes.Contains(html, []byte(`alt="Prize Sponsor"`)) || bytes.Contains(html, []byte(`<div class="social-card-sponsor-ticker"`)) {
		t.Fatal("winner must show a dedicated sponsor logo without a ticker")
	}
	version := imgproc.SiteSocialCardID(card)
	org.LogoLight = "/updated-sponsor.svg"
	if version == imgproc.SiteSocialCardID(awardSocialCard(ctx, page, award)) {
		t.Fatal("updated sponsor logo did not change the card version")
	}
	org.LogoLight = ""
	fallback := awardSocialCard(ctx, page, award)
	if len(fallback.SponsorLogos) != 1 || !strings.HasSuffix(fallback.SponsorLogos[0], org.LogoDark) {
		t.Fatal("available fallback logo missing", fallback.SponsorLogos)
	}
	second := *project
	second.ID = "project-two"
	page.Projects = append(page.Projects, &second)
	page.AwardeesByAward[award.ID] = append(page.AwardeesByAward[award.ID], &types.ProjectAward{ProjectID: second.ID})
	shared := awardSocialCard(ctx, page, award)
	if shared.Eyebrow != "2 winning projects" || len(shared.SponsorLogos) != 1 || shared.SponsorLogos[0] != fallback.SponsorLogos[0] {
		t.Fatal("shared prize lost its sponsor logo", shared.SponsorLogos)
	}
	org.LogoDark = ""
	missing := awardSocialCard(ctx, page, award)
	if len(missing.SponsorLogos) != 0 || missing.SponsorLabel != org.Name {
		t.Fatal("missing logo must retain written sponsor credit")
	}
}

func TestHackathonProjectSocialCardTeamPortraits(t *testing.T) {
	ctx, page, project := projectCardFixture()
	page.Members = []*types.ProjectMember{
		{ProjectID: project.ID, Name: "Bob", Photo: "bob.avif", Email: "private@example.com"},
		nil,
		{ProjectID: "another-project", Name: "Unrelated", Photo: "unrelated.jpg"},
		{ProjectID: project.ID, Name: "Alice", Photo: "https://untrusted.example/avatar.jpg"},
		{ProjectID: project.ID, Name: "Charlie"},
	}
	page.ProjectMembersByProject[project.ID] = page.Members
	card := projectSocialCard(ctx, page, project)
	if len(card.Team) != 3 || card.Team[0].Name != "Alice" || card.Team[0].Photo != "" || card.Team[1].Name != "Bob" || !strings.HasSuffix(card.Team[1].Photo, "/speakers/bob.avif") || card.Team[2].Photo != "" {
		t.Fatalf("team portraits were not paired, filtered, or normalized: %+v", card.Team)
	}
	version := imgproc.SiteSocialCardID(card)
	page.Members[0], page.Members[4] = page.Members[4], page.Members[0]
	if version != imgproc.SiteSocialCardID(projectSocialCard(ctx, page, project)) {
		t.Fatal("member query order changed the card version")
	}
	page.Members[4].Photo = "bob-new.avif"
	updated := projectSocialCard(ctx, page, project)
	if version == imgproc.SiteSocialCardID(updated) {
		t.Fatal("updated profile photo did not refresh the card")
	}
	if before := awardSocialCard(ctx, page, page.Awards[0]); len(before.Team) != 0 {
		t.Fatal("unannounced prize revealed team portraits")
	}
	now := time.Now()
	page.Competition.ResultsFinalizedAt = &now
	winner := awardSocialCard(ctx, page, page.Awards[0])
	if len(winner.Team) != 3 || winner.Team[1] != updated.Team[1] {
		t.Fatalf("winner card lost its team portraits: %+v", winner.Team)
	}
	html, err := imgproc.RenderSiteSocialCardHTML(winner)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private@example.com", "Unrelated", "untrusted.example"} {
		if bytes.Contains(html, []byte(private)) {
			t.Fatalf("card exposed excluded member data: %s", private)
		}
	}
}

func TestHackathonProjectSocialCardVisibility(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*HackathonPage, *types.HackathonProject)
	}{
		{"draft project", func(p *HackathonPage, x *types.HackathonProject) { x.Status = getters.ProjectStatusCreated }},
		{"hidden project", func(p *HackathonPage, x *types.HackathonProject) { x.Status = getters.ProjectStatusHidden }},
		{"wrong competition", func(p *HackathonPage, x *types.HackathonProject) { x.CompetitionID = "other" }},
		{"closed gallery", func(p *HackathonPage, x *types.HackathonProject) { p.Competition.PublicGalleryEnabled = false }},
		{"hidden competition", func(p *HackathonPage, x *types.HackathonProject) {
			p.Competition.Visibility = getters.CompetitionVisibilityHidden
		}},
		{"draft conference", func(p *HackathonPage, x *types.HackathonProject) { p.Conf.PublicationStatus = "draft" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, page, project := projectCardFixture()
			now := time.Now()
			page.Competition.ResultsFinalizedAt = &now
			if !publicHackathonCardProject(page, project) {
				t.Fatal("public fixture rejected")
			}
			test.change(page, project)
			if publicHackathonCardProject(page, project) {
				t.Fatal("private project shareable")
			}
			if card := awardSocialCard(ctx, page, page.Awards[0]); card.Kind == "award-winner" {
				t.Fatal("private winner exposed")
			}
		})
	}
}

func TestHackathonAwardSocialCardMultipleWinners(t *testing.T) {
	ctx, page, project := projectCardFixture()
	now := time.Now()
	page.Competition.ResultsFinalizedAt = &now
	second := *project
	second.ID = "project-two"
	second.Title = "Unravel Rule"
	second.ImageURL = "/static/img/rebrand/breakthroughs.jpg"
	page.Projects = append(page.Projects, &second)
	page.AwardeesByAward["first"] = append(page.AwardeesByAward["first"], &types.ProjectAward{ProjectID: second.ID})
	page.ProjectMembersByProject[second.ID] = []*types.ProjectMember{{ProjectID: second.ID, Name: "Carol", Photo: "carol.jpg"}}
	card := awardSocialCard(ctx, page, page.Awards[0])
	if card.Eyebrow != "2 winning projects" || !strings.Contains(card.Title, project.Title) || !strings.Contains(card.Title, second.Title) || len(card.Images) != 2 || card.Callout != "" || len(card.Team) != 3 || card.Team[2].Name != "Carol" {
		t.Fatalf("multiple winners: %+v", card)
	}
	page.Projects[0], page.Projects[1] = page.Projects[1], page.Projects[0]
	if imgproc.SiteSocialCardID(card) != imgproc.SiteSocialCardID(awardSocialCard(ctx, page, page.Awards[0])) {
		t.Fatal("viewer ordering changed share version")
	}
}

func TestHackathonProjectSocialCardMetadata(t *testing.T) {
	t.Chdir(findRepoRoot(t))
	ctx, page, project := projectCardFixture()
	if err := loadTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	page.SocialCardURL = siteSocialCardPath("project", "seoul--project", projectSocialCard(ctx, page, project))
	var out bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&out, "hackathon_project.tmpl", page); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`property="og:image" content="https://btcpp.dev/social-cards/project/seoul--project.jpg?v=`, `name="twitter:image"`, `property="og:image:type" content="image/jpeg"`, `property="og:image:width" content="1200"`, `property="og:image:height" content="630"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("metadata missing %s", want)
		}
	}
	page.SocialCardURL = ""
	out.Reset()
	if err := ctx.TemplateCache.ExecuteTemplate(&out, "hackathon_project.tmpl", page); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `noindex, nofollow`) || strings.Contains(out.String(), `property="og:image"`) {
		t.Fatal("private project advertised a share image")
	}
}

func TestHackathonSocialCardEndpointsPostgres(t *testing.T) {
	connection := os.Getenv("HACKATHON_CARD_TEST_DATABASE_URL")
	if connection == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	c := context.Background()
	base, err := pgxpool.New(c, connection)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	schema := "hackathon_cards_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = base.Exec(c, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer base.Exec(c, "DROP SCHEMA "+schema+" CASCADE")
	cfg, err := pgxpool.ParseConfig(connection)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	migrations, err := db.LoadMigrations(filepath.Join(findRepoRoot(t), "db/migrations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if _, err = pool.Exec(c, m.SQL); err != nil {
			t.Fatalf("%s: %v", m.Name, err)
		}
	}
	app := &config.AppContext{DB: pool, Env: &types.EnvConfig{MailOff: true, Host: "localhost", Port: "8888"}, Err: log.New(io.Discard, "", 0)}
	insert := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(c, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	run := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(c, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	conf := insert(`INSERT INTO conferences(tag,description,publication_status) VALUES('card-test','bitcoin++ Seoul','published') RETURNING id::text`)
	competition := insert(`INSERT INTO competitions(conference_id,title,visibility,public_gallery_enabled) VALUES($1,'Seoul Hackathon','public',true) RETURNING id::text`, conf)
	project := insert(`INSERT INTO projects(competition_id,title,status,slug) VALUES($1,'BlackIce','submitted','blackice') RETURNING id::text`, competition)
	award := insert(`INSERT INTO awards(competition_id,title,status,public_slug) VALUES($1,'First place','awarded','first-place') RETURNING id::text`, competition)
	run(`INSERT INTO project_awards(project_id,award_id) VALUES($1,$2)`, project, award)
	originalStorage := siteSocialCardStorage
	siteSocialCardStorage.isConfigured = func() bool { return false }
	defer func() { siteSocialCardStorage = originalStorage }()
	request := func(kind, slug string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodHead, "/social-cards/"+kind+"/"+slug+".jpg", nil)
		r = mux.SetURLVars(r, map[string]string{"kind": kind, "slug": slug})
		w := httptest.NewRecorder()
		ServeSiteSocialCard(w, r, app)
		return w
	}
	slug := "card-test--" + project
	if w := request("project", slug); w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("public project endpoint: %d %s", w.Code, w.Body.String())
	}
	before, err := loadAwardSocialCard(app, "card-test--first-place")
	if err != nil || before.Kind != "award" {
		t.Fatalf("unannounced: %+v %v", before, err)
	}
	run(`UPDATE competitions SET results_finalized_at=now() WHERE id=$1`, competition)
	after, err := loadAwardSocialCard(app, "card-test--first-place")
	if err != nil || after.Kind != "award-winner" || after.Title != "BlackIce" {
		t.Fatalf("published winner: %+v %v", after, err)
	}
	run(`UPDATE projects SET status='hidden' WHERE id=$1`, project)
	if w := request("project", slug); w.Code != 404 {
		t.Fatal("hidden project image exposed", w.Code)
	}
	after, err = loadAwardSocialCard(app, "card-test--first-place")
	if err != nil || after.Kind != "award" {
		t.Fatal("hidden winner exposed", after, err)
	}
	run(`UPDATE projects SET status='submitted' WHERE id=$1`, project)
	run(`UPDATE competitions SET public_gallery_enabled=false WHERE id=$1`, competition)
	if w := request("project", slug); w.Code != 404 {
		t.Fatal("closed gallery image exposed", w.Code)
	}
	run(`UPDATE competitions SET public_gallery_enabled=true WHERE id=$1`, competition)
	run(`UPDATE conferences SET publication_status='draft' WHERE id=$1`, conf)
	if w := request("project", slug); w.Code != 404 {
		t.Fatal("unpublished event exposed", w.Code)
	}
}

// Optional visual fixtures, rendered with the same template used for JPEGs.
func TestHackathonSocialCardVisualFixtures(t *testing.T) {
	output := os.Getenv("HACKATHON_CARD_PREVIEW_DIR")
	if output == "" {
		t.Skip("set HACKATHON_CARD_PREVIEW_DIR to render examples")
	}
	t.Chdir(findRepoRoot(t))
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	ctx, page, project := projectCardFixture()
	ctx.Env.Port = "18914"
	project.ImageURL = "/example-image.png"
	project.Title = "Is this good for Bitcoin? Just Ask BIPS"
	project.ShortDescription = "An educational tool to learn more about consensus-changing Bitcoin proposals."
	cards := map[string]imgproc.SiteSocialCard{"project": projectSocialCard(ctx, page, project), "prize-before": awardSocialCard(ctx, page, page.Awards[0])}
	now := time.Now()
	page.Competition.ResultsFinalizedAt = &now
	cards["prize-winner"] = awardSocialCard(ctx, page, page.Awards[0])
	project.Title = "BlackIce — Distributed FROST Silent Payments"
	project.ImageURL = ""
	cards["project-no-image"] = projectSocialCard(ctx, page, project)
	cards["prize-no-image"] = awardSocialCard(ctx, page, page.Awards[0])
	project.Title = "A very long project title with Bitcoin payments, privacy, shared custody, and tools for builders working together across the world"
	cards["project-long-title"] = projectSocialCard(ctx, page, project)
	cards["prize-long-title"] = awardSocialCard(ctx, page, page.Awards[0])
	project.Title = "BlackIce"
	project.ImageURL = "/example-image.png"
	second := *project
	second.ID = "project-two"
	second.Title = "Unravel Rule"
	second.ImageURL = "/example-image-two.png"
	page.Projects = append(page.Projects, &second)
	page.AwardeesByAward["first"] = append(page.AwardeesByAward["first"], &types.ProjectAward{ProjectID: second.ID})
	cards["prize-shared"] = awardSocialCard(ctx, page, page.Awards[0])
	project.Title = "BlackIce - Distributed Frost Silent Payments"
	project.ShortDescription = "Silent Payments has suffered from the lack of multisig & hardware wallet support. We built payments using FROST / Frostsnap hardware that enable distributed signing coordinated using Marmot."
	project.ImageURL = "/blackice-hero.png"
	project.ImageURLs = []string{"/blackice-screen.png", "/blackice-demo.png"}
	project.Tags = []string{"Silent payments", "FROST", "Marmot"}
	page.Members = []*types.ProjectMember{{ProjectID: project.ID, Name: "robos", Photo: "http://localhost:18914/robos.jpg"}, {ProjectID: project.ID, Name: "Rademenez", Photo: "http://localhost:18914/rademenez.avif"}}
	page.ProjectMembersByProject[project.ID] = page.Members
	page.Projects = []*types.HackathonProject{project}
	page.AwardeesByAward["first"] = []*types.ProjectAward{{ProjectID: project.ID}}
	cards["seoul-project"] = projectSocialCard(ctx, page, project)
	cards["seoul-prize"] = awardSocialCard(ctx, page, page.Awards[0])
	sponsorAward := *page.Awards[0]
	sponsorAward.Title = "Best Use of the Marmot Protocol"
	sponsorAward.SponsoredByOrgID = "marmot"
	page.OrgsByID = map[string]*types.Org{"marmot": {Name: "Internet Privacy Foundation", LogoLight: "http://localhost:18914/ipf-logo.png"}}
	page.PrizesByAward[sponsorAward.ID] = []*types.Prize{{PrizeType: getters.PrizeTypeSats, ValueText: "1000000"}}
	cards["seoul-sponsor-prize"] = awardSocialCard(ctx, page, &sponsorAward)
	// Project links use the same card family, with every public award retained.
	projectSponsorAward := sponsorAward
	projectSponsorAward.ID = "marmot"
	page.Awards = append(page.Awards, &projectSponsorAward)
	page.AwardeesByAward[projectSponsorAward.ID] = []*types.ProjectAward{{ProjectID: project.ID}}
	cards["seoul-project-awarded"] = projectSocialCard(ctx, page, project)
	page.Awards = page.Awards[:len(page.Awards)-1]
	page.Competition.ResultsFinalizedAt = nil
	sponsorAward.Description = "Marmot is a protocol for efficient end-to-end encrypted group messaging over nostr relays, built for humans and agents. Think Signal, but unstoppable."
	cards["seoul-sponsor-challenge"] = awardSocialCard(ctx, page, &sponsorAward)
	cards["seoul-project-before"] = projectSocialCard(ctx, page, project)
	page.Competition.ResultsFinalizedAt = &now
	missingLogo := cards["seoul-sponsor-prize"]
	missingLogo.SponsorLogos = []string{"http://localhost:18914/missing-sponsor.png"}
	cards["sponsor-missing"] = missingLogo
	otherLogo := cards["seoul-sponsor-prize"]
	otherLogo.Title = "Sponsor logo layout sample"
	otherLogo.TitleSuffix = "Sample sponsor prize"
	otherLogo.SponsorLabel = "Tether MDK"
	otherLogo.SponsorNames = []string{"Tether MDK"}
	otherLogo.SponsorLogos = []string{"http://localhost:18914/tether-mdk-logo.png"}
	cards["sponsor-tether-sample"] = otherLogo
	otherEvent := cards["seoul-sponsor-prize"]
	otherEvent.HeroImage = siteSocialCardImageURL(ctx, confImagePath("berlin26", "leading"))
	otherEvent.Location = "Berlin"
	otherEvent.EventDate = "Artwork layout sample"
	cards["event-berlin-sample"] = otherEvent
	missingEvent := cards["seoul-sponsor-prize"]
	missingEvent.HeroImage = "http://localhost:18914/missing-leading.png"
	cards["event-missing"] = missingEvent
	page.PrizesByAward[sponsorAward.ID] = []*types.Prize{{PrizeType: getters.PrizeTypeSats, ValueText: "2500000"}}
	fallback := cards["seoul-project"]
	fallback.Team = []imgproc.SiteSocialCardMember{{Name: "Missing Photo", Photo: "http://localhost:18914/missing-avatar.jpg"}, {Name: "No Photo"}}
	cards["team-fallback"] = fallback
	largeTeam := cards["seoul-project"]
	largeTeam.Callout = "Rademenez, robos, Alice, Bob, Carol, Dave, Eve, Frankie"
	largeTeam.Team = append([]imgproc.SiteSocialCardMember(nil), largeTeam.Team...)
	for _, name := range []string{"Alice", "Bob", "Carol", "Dave", "Eve", "Frankie"} {
		largeTeam.Team = append(largeTeam.Team, imgproc.SiteSocialCardMember{Name: name})
	}
	cards["team-large"] = largeTeam
	largePrize := cards["seoul-sponsor-prize"]
	largePrize.Team, largePrize.Callout = largeTeam.Team, largeTeam.Callout
	cards["prize-large-team"] = largePrize
	originalAward := *page.Awards[0]
	for _, style := range []struct {
		name, title string
		rank        int
	}{
		{"first", "First place", 1}, {"second", "Second place", 2}, {"third", "Third place", 3}, {"honorable", "Honorable mention", 0},
	} {
		page.Awards[0].Title = style.title
		page.Awards[0].AwardRank = nil
		if style.rank > 0 {
			rank := style.rank
			page.Awards[0].AwardRank = &rank
		}
		cards["ribbon-"+style.name] = projectSocialCard(ctx, page, project)
	}
	*page.Awards[0] = originalAward
	project.ImageURLs = nil
	project.ImageURL = "/missing-image.png"
	cards["project-broken-image"] = projectSocialCard(ctx, page, project)
	for name, card := range cards {
		html, err := imgproc.RenderSiteSocialCardHTML(card)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, name+".html"), html, 0644); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("HACKATHON_CARD_RENDER_JPEG") == "1" && (strings.HasPrefix(name, "seoul-") || strings.HasPrefix(name, "ribbon-") || strings.HasPrefix(name, "team-") || strings.HasPrefix(name, "event-") || strings.HasPrefix(name, "sponsor-")) {
			jpeg, err := imgproc.MakeSiteSocialCardJPEG(card)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(output, name+".jpg"), jpeg, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestHackathonSocialCardAwardRibbons(t *testing.T) {
	for _, test := range []struct {
		name, title, sponsor, kind string
		rank, want                 int
	}{
		{name: "ranked first", title: "Overall winner", rank: 1, want: 1},
		{name: "second", title: "Second overall", want: 2},
		{name: "third", title: "3rd place", want: 3},
		{name: "honorable", title: "Honorable mention", want: 4},
		{name: "honourable", title: "Honourable   Mention", want: 4},
		{name: "fourth is not honorable", title: "Fourth place", rank: 4},
		{name: "sponsor first", title: "First place", sponsor: "sponsor", rank: 1},
		{name: "challenge first", title: "First place", kind: getters.AwardTypeChallenge, rank: 1},
		{name: "sponsor mention", title: "Honorable mention", sponsor: "sponsor"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, page, project := projectCardFixture()
			award := page.Awards[0]
			award.Title = test.title
			award.SponsoredByOrgID = test.sponsor
			award.AwardType = test.kind
			if test.rank > 0 {
				award.AwardRank = &test.rank
			}
			if card := projectSocialCard(ctx, page, project); card.AwardRibbon != 0 {
				t.Fatal("unpublished ribbon exposed")
			}
			now := time.Now()
			page.Competition.ResultsFinalizedAt = &now
			if card := projectSocialCard(ctx, page, project); card.AwardRibbon != test.want {
				t.Fatalf("project ribbon=%d, want %d", card.AwardRibbon, test.want)
			} else if (test.want > 0 && len(card.Badges) != 0) || (test.want == 0 && len(card.Badges) != 1) {
				t.Fatalf("lower badge must only show awards without a sash: %+v", card.Badges)
			}
			if card := awardSocialCard(ctx, page, award); card.AwardRibbon != test.want {
				t.Fatalf("prize ribbon=%d, want %d", card.AwardRibbon, test.want)
			}
		})
	}
	ctx, page, project := projectCardFixture()
	now := time.Now()
	page.Competition.ResultsFinalizedAt = &now
	mention := &types.Award{ID: "mention", Title: "Honorable mention"}
	page.Awards = append([]*types.Award{mention}, page.Awards...)
	page.AwardeesByAward[mention.ID] = []*types.ProjectAward{{ProjectID: project.ID}}
	if card := projectSocialCard(ctx, page, project); card.AwardRibbon != 1 || len(card.Badges) != 1 || card.Badges[0] != page.AwardWinnerBadgeLabel(mention) {
		t.Fatal("best award belongs on the sash and the additional award on a badge", card)
	}
	if card := awardSocialCard(ctx, page, mention); card.AwardRibbon != 4 {
		t.Fatal("prize card used the project's other award")
	}
	challenge := &types.Award{ID: "sponsor", Title: "Best Use of the Marmot Protocol", SponsoredByOrgID: "ipf"}
	page.Awards = []*types.Award{challenge}
	page.AwardeesByAward[challenge.ID] = []*types.ProjectAward{{ProjectID: project.ID}}
	if card := projectSocialCard(ctx, page, project); card.AwardRibbon != 0 || len(card.Badges) != 1 || card.Badges[0] != challenge.Title {
		t.Fatal("project winner banner must retain the full sponsor challenge name", card.Badges)
	}
}

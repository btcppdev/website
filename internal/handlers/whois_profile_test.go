package handlers

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

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

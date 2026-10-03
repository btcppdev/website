package handlers

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestHackathonJudgePickersUseScopedSearch(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	for _, conferenceScoped := range []bool{true, false} {
		page := &HackathonAdminPage{
			Competition: &types.HackathonCompetition{ID: "competition", ConferenceID: "conference"},
			Awards:      []*types.Award{{ID: "award", SponsoredByOrgID: "sponsor"}},
		}
		base := "/admin/hackathons/competition"
		if conferenceScoped {
			page.Conf = &types.Conf{Ref: "conference", Tag: "berlin26"}
			base = "/berlin26/admin/hackathon"
		}
		for _, name := range []string{"admin/hackathon_judging.tmpl", "admin/hackathon_awards.tmpl"} {
			var out bytes.Buffer
			if err := app.TemplateCache.ExecuteTemplate(&out, name, page); err != nil {
				t.Fatal(err)
			}
			want := `data-person-picker-search-url="` + base + `/people/search"`
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s missing scoped search %s", name, want)
			}
		}
	}
}

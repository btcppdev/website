package handlers

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestFinalizedProjectPrizeOrdering(t *testing.T) {
	now := time.Now()
	page := &HackathonPage{
		Competition:     &types.HackathonCompetition{PublicGalleryEnabled: true, ResultsFinalizedAt: &now},
		PrizesByAward:   map[string][]*types.Prize{},
		AwardeesByAward: map[string][]*types.ProjectAward{},
	}
	add := func(id string, podium int, title, sponsor, kind, value string) {
		award := &types.Award{ID: id, Title: title, SponsoredByOrgID: sponsor}
		if podium > 0 {
			award.AwardRank = &podium
		}
		page.Projects = append(page.Projects, &types.HackathonProject{ID: id, Status: getters.ProjectStatusSubmitted})
		page.Awards = append(page.Awards, award)
		page.AwardeesByAward[id] = []*types.ProjectAward{{ProjectID: id}}
		page.PrizesByAward[id] = []*types.Prize{{PrizeType: kind, ValueText: value}}
	}
	add("merch-low", 0, "Hardware", "sponsor", getters.PrizeTypeInKind, "1000")
	add("cash-low", 0, "Sponsor first place", "sponsor", getters.PrizeTypeSats, "200")
	add("third", 3, "Third", "", getters.PrizeTypeSats, "900")
	add("mention", 0, "Honorable mention", "", getters.PrizeTypeInKind, "0")
	add("merch-high", 0, "Equipment", "sponsor", getters.PrizeTypeInKind, "90000000")
	add("second", 2, "Second", "", getters.PrizeTypeSats, "100")
	add("cash-high", 0, "Sponsor winner", "sponsor", getters.PrizeTypeSats, "10000")
	add("first", 1, "First", "", getters.PrizeTypeSats, "1")
	page.Projects = append(page.Projects,
		&types.HackathonProject{ID: "unawarded", Status: getters.ProjectStatusSubmitted},
		&types.HackathonProject{ID: "hidden", Status: getters.ProjectStatusHidden})
	page.AwardeesByAward["first"] = append(page.AwardeesByAward["first"], &types.ProjectAward{ProjectID: "hidden"})
	// An additional sponsor prize must not demote the first-place project or duplicate it.
	page.AwardeesByAward["cash-high"] = append(page.AwardeesByAward["cash-high"], &types.ProjectAward{ProjectID: "first"})
	ids := func(projects []*types.HackathonProject) []string {
		var result []string
		for _, p := range projects {
			result = append(result, p.ID)
		}
		return result
	}
	want := []string{"first", "second", "third", "mention", "cash-high", "cash-low", "merch-high", "merch-low", "unawarded"}
	if got := ids(page.GalleryProjects()); !reflect.DeepEqual(got, want) {
		t.Fatalf("gallery = %v, want %v", got, want)
	}
	if got := ids(page.FeaturedProjects()); !reflect.DeepEqual(got, want[:8]) {
		t.Fatalf("featured = %v, want all winners %v", got, want[:8])
	}
	if got := ids(page.PodiumProjects()); !reflect.DeepEqual(got, want[:3]) {
		t.Fatalf("podium = %v", got)
	}
	if got := ids(page.OtherWinningProjects()); !reflect.DeepEqual(got, want[3:8]) {
		t.Fatalf("remaining winners = %v", got)
	}
	if page.Projects[0].ID != "merch-low" {
		t.Fatal("sort mutated source projects")
	}
	page.Competition.ResultsFinalizedAt = nil
	if got := page.GalleryProjects()[0].ID; got != "merch-low" {
		t.Fatalf("unpublished order changed: %s", got)
	}
	if len(page.FeaturedProjects()) != 3 {
		t.Fatal("preview should retain three featured projects")
	}
}

func TestHackathonFinalizedResultsLayout(t *testing.T) {
	t.Chdir(findRepoRoot(t))
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	page := &HackathonPage{
		Conf:            &types.Conf{Tag: "berlin26"},
		Competition:     &types.HackathonCompetition{Title: "Hackathon", PublicGalleryEnabled: true},
		Projects:        []*types.HackathonProject{{ID: "winner", Title: "Winner", Status: getters.ProjectStatusSubmitted}},
		Awards:          []*types.Award{{ID: "award", Title: "First place"}},
		AwardeesByAward: map[string][]*types.ProjectAward{"award": {{ProjectID: "winner"}}},
	}
	for _, title := range []string{"Second place", "Third place", "Honorable mention"} {
		id := title
		page.Projects = append(page.Projects, &types.HackathonProject{ID: id, Title: title, Status: getters.ProjectStatusSubmitted})
		page.Awards = append(page.Awards, &types.Award{ID: id, Title: title})
		page.AwardeesByAward[id] = []*types.ProjectAward{{ProjectID: id}}
	}
	for _, finalized := range []bool{false, true} {
		page.Competition.ResultsFinalizedAt = nil
		if finalized {
			page.Competition.ResultsFinalizedAt = &now
		}
		var out bytes.Buffer
		if err := ctx.TemplateCache.ExecuteTemplate(&out, "hackathon.tmpl", page); err != nil {
			t.Fatal(err)
		}
		html := out.String()
		if finalized && os.Getenv("BTCPP_HACKATHON_PREVIEW") == "1" {
			if err := os.WriteFile("/tmp/btcpp-hackathon-preview.html", out.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
		for _, class := range []string{`class="hack-participation"`, `class="hack-participation__intro"`, `class="hack-actions hack-control__actions"`} {
			if strings.Contains(html, class) == finalized {
				t.Errorf("finalized=%v: unexpected visibility of %s", finalized, class)
			}
		}
		featured := strings.Index(html, `class="hack-featured-projects"`)
		participation := strings.Index(html, `class="hack-participation"`)
		if featured < 0 || (!finalized && (participation < 0 || featured < participation)) {
			t.Errorf("finalized=%v: incorrect section ordering", finalized)
		}
		if finalized {
			first := strings.Index(html, "hack-card--podium-1")
			second := strings.Index(html, "hack-card--podium-2")
			third := strings.Index(html, "hack-card--podium-3")
			others := strings.Index(html, "Honorable mentions &amp; sponsor prizes")
			if first < 0 || second <= first || third <= second || others <= third {
				t.Fatal("expected first, second, third, then other winners")
			}
		} else if strings.Contains(html, "hack-card--podium") {
			t.Fatal("unfinalized overview unexpectedly has a podium")
		}
		if strings.Count(html, `class="hack-featured-projects"`) != 1 {
			t.Error("featured projects duplicated")
		}
	}
}

package handlers

import (
	"fmt"
	"sort"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/types"
)

// Share images always use the anonymous gallery, even when the page viewer is
// an organizer. Draft projects and unpublished winners must not enter a CDN.
func publicHackathonCardProject(page *HackathonPage, project *types.HackathonProject) bool {
	return page != nil && page.Conf != nil && page.Conf.IsPublished() &&
		page.Competition != nil && page.Competition.Visibility == getters.CompetitionVisibilityPublic &&
		page.ProjectGalleryOpen() && project != nil && project.CompetitionID == page.Competition.ID &&
		(project.Status == getters.ProjectStatusSubmitted || project.Status == getters.ProjectStatusAdvanced)
}

func projectSocialCard(ctx *config.AppContext, page *HackathonPage, project *types.HackathonProject) imgproc.SiteSocialCard {
	card := imgproc.SiteSocialCard{Kind: "project", Eyebrow: "hackathon project", Title: project.Title}
	applySiteSocialCardPalette(&card, page.Conf)
	if image := confImagePath(page.Conf.Tag, "leading"); image != "/static/img/rebrand/light-sketch-bg.avif" {
		card.HeroImage = image
	}
	card.Location = page.ConferenceLabel()
	card.EventEdition = page.Conf.ArchiveEdition()
	card.EventDate = strings.Join(nonEmptyStrings(page.Conf.Location, page.Conf.DateDesc), " · ")
	card.Subtitle = socialCardPlainText(project.ShortDescription)
	if card.Subtitle == "" {
		card.Subtitle = socialCardPlainText(project.Description)
	}
	card.Images = page.ProjectImageGallery(project)
	card.Team = hackathonSocialCardTeam(page.Members, project.ID)
	var names []string
	for _, member := range card.Team {
		names = append(names, member.Name)
	}
	card.Callout = strings.Join(names, ", ")
	if project.ProjectNumber != nil {
		card.Eyebrow = "hackathon project · " + page.ProjectNumberLabel(project)
	}
	if page.ResultsFinalized() {
		awards := page.ProjectWinningAwards(project)
		for _, award := range awards {
			if rank := hackathonAwardRibbonRank(page, award); rank > 0 && (card.AwardRibbon == 0 || rank < card.AwardRibbon) {
				card.AwardRibbon = rank
			}
		}
		for _, award := range awards {
			if card.AwardRibbon > 0 && hackathonAwardRibbonRank(page, award) == card.AwardRibbon {
				continue
			}
			card.Badges = append(card.Badges, page.AwardWinnerBadgeLabel(award))
		}
	}
	return normalizeSiteSocialCard(ctx, card)
}

func hackathonSocialCardTeam(members []*types.ProjectMember, projectID string) []imgproc.SiteSocialCardMember {
	var team []imgproc.SiteSocialCardMember
	for _, member := range members {
		if member == nil || member.ProjectID != projectID {
			continue
		}
		name := strings.TrimSpace(member.Name)
		if name == "" {
			continue
		}
		team = append(team, imgproc.SiteSocialCardMember{Name: name, Photo: siteSocialCardStoredImage("speakers", member.Photo)})
	}
	// Names and portraits stay paired, regardless of database result order.
	sort.Slice(team, func(i, j int) bool {
		if team[i].Name == team[j].Name {
			return team[i].Photo < team[j].Photo
		}
		return team[i].Name < team[j].Name
	})
	return team
}

// Use overall placement for project ribbons; sponsor challenge placements must
// not be presented as the overall hackathon result. Honorable mentions have no
// dedicated database type, so follow the public gallery's title convention.
func hackathonAwardRibbonRank(page *HackathonPage, award *types.Award) int {
	if page == nil || !page.ResultsFinalized() || award == nil || page.AwardHasSponsor(award) || page.AwardIsChallenge(award) {
		return 0
	}
	if rank := page.AwardPodiumRank(award); rank > 0 {
		return rank
	}
	title := strings.ToLower(strings.Join(strings.Fields(award.Title), " "))
	if strings.Contains(title, "honorable mention") || strings.Contains(title, "honourable mention") {
		return 4
	}
	return 0
}

func awardWinnerSocialCard(ctx *config.AppContext, page *HackathonPage, award *types.Award, prize imgproc.SiteSocialCard) (imgproc.SiteSocialCard, bool) {
	if page == nil || !page.ResultsFinalized() || !page.ProjectGalleryOpen() {
		return imgproc.SiteSocialCard{}, false
	}
	winning := map[string]bool{}
	for _, winner := range page.Awardees(award) {
		if winner != nil {
			winning[winner.ProjectID] = true
		}
	}
	var projects []*types.HackathonProject
	for _, project := range page.Projects {
		if publicHackathonCardProject(page, project) && winning[project.ID] {
			projects = append(projects, project)
			delete(winning, project.ID)
		}
	}
	if len(projects) == 0 {
		return imgproc.SiteSocialCard{}, false
	}
	// Use stable ordering so different viewers produce the same content version.
	sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })
	projectPage := *page
	projectPage.Members = page.ProjectMembersByProject[projects[0].ID]
	card := projectSocialCard(ctx, &projectPage, projects[0])
	card.Kind = "award-winner"
	card.AwardRibbon = hackathonAwardRibbonRank(page, award)
	card.Eyebrow = "hackathon winner"
	card.TitleSuffix = award.Title
	card.Badges = nil
	card.Details = append([]string(nil), prize.Details...)
	card.Value, card.ValueSuffix, card.ValueLabel = prize.Value, prize.ValueSuffix, prize.ValueLabel
	if page.AwardHasSponsor(award) {
		card.SponsorLabel = page.AwardSponsorLabel(award)
		// The unannounced prize card already chose the sponsor's light-mode
		// logo (or its available fallback). Keep it separate from project art.
		card.SponsorLogos = append([]string(nil), prize.Images...)
		for range card.SponsorLogos {
			card.SponsorNames = append(card.SponsorNames, card.SponsorLabel)
		}
	}
	if len(projects) > 1 {
		card.Eyebrow = fmt.Sprintf("%d winning projects", len(projects))
		card.Callout, card.Subtitle = "", ""
		card.Team = nil
		card.Images, card.ImageLabels = nil, nil
		var titles []string
		for i, project := range projects {
			card.Team = append(card.Team, hackathonSocialCardTeam(page.ProjectMembersByProject[project.ID], project.ID)...)
			if i < 3 {
				titles = append(titles, project.Title)
			}
			for _, raw := range page.ProjectImageGallery(project) {
				if image := siteSocialCardImageURL(ctx, raw); image != "" {
					card.Images = append(card.Images, image)
					card.ImageLabels = append(card.ImageLabels, project.Title)
					break
				}
			}
		}
		card.Title = strings.Join(titles, " / ")
		if len(projects) > 3 {
			card.Subtitle = fmt.Sprintf("And %d more winning projects", len(projects)-3)
		}
	}
	return normalizeSiteSocialCard(ctx, card), true
}

func loadProjectSocialCard(ctx *config.AppContext, encodedSlug string) (imgproc.SiteSocialCard, error) {
	parts := strings.SplitN(strings.TrimSpace(encodedSlug), "--", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return imgproc.SiteSocialCard{}, fmt.Errorf("invalid project social card slug")
	}
	conf, err := getters.GetConfByTag(ctx, parts[0])
	if err != nil || conf == nil || !conf.IsPublished() {
		return imgproc.SiteSocialCard{}, fmt.Errorf("published conference not found")
	}
	competition, err := getters.GetCompetitionByConferenceID(ctx, conf.Ref)
	if err != nil || competition == nil {
		return imgproc.SiteSocialCard{}, fmt.Errorf("public hackathon not found")
	}
	project, err := getters.GetProjectByID(ctx, parts[1])
	page := &HackathonPage{Conf: conf, Competition: competition, Project: project}
	if err != nil || !publicHackathonCardProject(page, project) {
		return imgproc.SiteSocialCard{}, fmt.Errorf("public project not found")
	}
	page.Members, err = getters.ListProjectMembers(ctx, project.ID)
	if err != nil {
		return imgproc.SiteSocialCard{}, err
	}
	page.Awards, page.PrizesByAward, page.PrizePoolByAward, page.AwardeesByAward, err = loadPublicHackathonAwards(ctx, competition.ID, page.ResultsFinalized())
	if err != nil {
		return imgproc.SiteSocialCard{}, err
	}
	return projectSocialCard(ctx, page, project), nil
}

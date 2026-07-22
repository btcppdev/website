package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	stdhtml "html"
	"html/template"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/ics"
	"btcpp-web/internal/types"
	"github.com/gomarkdown/markdown"
	mdhtml "github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/gorilla/mux"
	"golang.org/x/net/html"
)

var bitcoinPrizeAmountPattern = regexp.MustCompile(`[-+]?\d[\d,]*(?:\.\d+)?`)

type HackathonPage struct {
	Competition                 *types.HackathonCompetition
	Competitions                []*types.HackathonCompetition
	Conf                        *types.Conf
	Confs                       []*types.Conf
	OrgsByID                    map[string]*types.Org
	Projects                    []*types.HackathonProject
	ProjectMembersByProject     map[string][]*types.ProjectMember
	ChallengeProjects           []*types.HackathonProject
	Project                     *types.HackathonProject
	Members                     []*types.ProjectMember
	JudgeEvents                 []*types.JudgeEvent
	Scorecards                  []*types.Scorecard
	Judges                      []*types.CompetitionJudge
	JudgeProfileURLs            map[string]string
	JudgeTypes                  map[string]bool
	Awards                      []*types.Award
	ChallengeAwardsForJudge     []*types.Award
	AwardVotes                  []*types.AwardVote
	OptInAwards                 []*types.Award
	AwardOptIns                 map[string]bool
	PrizesByAward               map[string][]*types.Prize
	PrizePoolByAward            map[string][]*types.Prize
	HackathonPlaceRows          []*HackathonPlaceRow
	AwardeesByAward             map[string][]*types.ProjectAward
	ScheduleEventsByCompetition map[string][]HackathonScheduleEvent
	ScheduleEventList           []HackathonScheduleEvent
	Viewer                      *auth.Identity
	OwnedProjects               map[string]bool
	IsNew                       bool
	IsProjectEditor             bool
	HasConferenceTicket         bool
	CanCreate                   bool
	CanEdit                     bool
	CanSubmit                   bool
	CanJudge                    bool
	CanScoreAll                 bool
	CanRemoveProjectMembers     bool
	InviteLink                  string
	InviteQRCodeURI             string
	FlashMessage                string
	FlashError                  string
	Year                        uint
}

type HackathonScheduleEvent struct {
	SegmentID   string
	Label       string
	Time        *time.Time
	End         *time.Time
	Venue       string
	SegmentType string
}

type HackathonTimelineView struct {
	Label         string
	Value         string
	HasCountdown  bool
	CountdownUnix int64
}

type HackathonPrimaryAction struct {
	Label    string
	URL      string
	Disabled bool
}

func (p *HackathonPage) ConferenceLabel() string {
	if p == nil || p.Conf == nil {
		return ""
	}
	if p.Conf.Desc != "" {
		return publicHackathonConferenceName(p.Conf.Desc)
	}
	return p.Conf.Tag
}

func (p *HackathonPage) ConferenceURL() string {
	if p == nil || p.Conf == nil || strings.TrimSpace(p.Conf.Tag) == "" {
		return ""
	}
	return "/" + url.PathEscape(p.Conf.Tag)
}

func (p *HackathonPage) HackathonEditionName() string {
	if p == nil || p.Conf == nil {
		return "conference"
	}
	edition := strings.TrimSpace(p.Conf.ArchiveEdition())
	if edition == "" {
		edition = strings.TrimSpace(p.Conf.ArchiveTitle())
	}
	if edition == "" {
		edition = p.ConferenceLabel()
	}
	edition = strings.TrimSpace(edition)
	for _, suffix := range []string{" edition", " Edition", " EDITION"} {
		if strings.HasSuffix(edition, suffix) {
			edition = strings.TrimSpace(strings.TrimSuffix(edition, suffix))
			break
		}
	}
	if edition == "" {
		return "conference"
	}
	return edition
}

func (p *HackathonPage) HackathonEditionTitle() string {
	edition := p.HackathonEditionName()
	if edition == "" {
		return "Conference"
	}
	return strings.ToUpper(edition[:1]) + edition[1:]
}

func (p *HackathonPage) HackathonLocation() string {
	if p == nil || p.Conf == nil {
		return "the conference"
	}
	if location := strings.TrimSpace(p.Conf.Location); location != "" {
		return location
	}
	if title := strings.TrimSpace(p.Conf.ArchiveTitle()); title != "" {
		return title
	}
	return "the conference"
}

func (p *HackathonPage) CompetitionConferenceLabel(competition *types.HackathonCompetition) string {
	conf := p.competitionConf(competition)
	if conf == nil {
		return ""
	}
	if conf.Desc != "" {
		return publicHackathonConferenceName(conf.Desc)
	}
	return conf.Tag
}

func (p *HackathonPage) CompetitionConferenceURL(competition *types.HackathonCompetition) string {
	conf := p.competitionConf(competition)
	if conf == nil || strings.TrimSpace(conf.Tag) == "" {
		return ""
	}
	return "/" + url.PathEscape(conf.Tag)
}

func (p *HackathonPage) CompetitionConferenceDate(competition *types.HackathonCompetition) string {
	conf := p.competitionConf(competition)
	if conf == nil {
		return ""
	}
	return strings.TrimSpace(conf.DateDesc)
}

func (p *HackathonPage) CompetitionConferenceTag(competition *types.HackathonCompetition) string {
	conf := p.competitionConf(competition)
	if conf == nil {
		return ""
	}
	return strings.TrimSpace(conf.Tag)
}

func (p *HackathonPage) CompetitionImagePNG(competition *types.HackathonCompetition) string {
	conf := p.competitionConf(competition)
	if conf == nil || strings.TrimSpace(conf.Tag) == "" {
		return "/static/img/berlin_hack_day.png"
	}
	return "/static/img/" + url.PathEscape(conf.Tag) + "/leading.png"
}

func (p *HackathonPage) CompetitionImageAVIF(competition *types.HackathonCompetition) string {
	conf := p.competitionConf(competition)
	if conf == nil || strings.TrimSpace(conf.Tag) == "" {
		return ""
	}
	return "/static/img/" + url.PathEscape(conf.Tag) + "/leading.avif"
}

func publicHackathonConferenceName(name string) string {
	name = strings.TrimSpace(name)
	for _, prefix := range []string{"bitcoin++", "Bitcoin++", "BITCOIN++"} {
		if strings.HasPrefix(name, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(name, prefix))
		}
	}
	return name
}

func lowerFirst(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToLower(value[:1]) + value[1:]
}

const (
	hackathonSortNewest     = "newest"
	hackathonSortOldest     = "oldest"
	hackathonSortTitle      = "title"
	hackathonSortConference = "conference"
)

func normalizeHackathonSort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case hackathonSortOldest:
		return hackathonSortOldest
	case hackathonSortTitle:
		return hackathonSortTitle
	case hackathonSortConference:
		return hackathonSortConference
	default:
		return hackathonSortNewest
	}
}

func hackathonListControls(r *http.Request) (string, string) {
	return strings.TrimSpace(r.URL.Query().Get("q")), normalizeHackathonSort(r.URL.Query().Get("sort"))
}

func applyHackathonListControls(competitions []*types.HackathonCompetition, confs []*types.Conf, query, sortMode string) []*types.HackathonCompetition {
	competitions = filterHackathonCompetitions(competitions, confs, query)
	sortHackathonCompetitions(competitions, confs, sortMode)
	return competitions
}

func filterHackathonCompetitions(competitions []*types.HackathonCompetition, confs []*types.Conf, query string) []*types.HackathonCompetition {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return competitions
	}
	filtered := make([]*types.HackathonCompetition, 0, len(competitions))
	for _, competition := range competitions {
		if hackathonCompetitionMatches(competition, confs, query) {
			filtered = append(filtered, competition)
		}
	}
	return filtered
}

func hackathonCompetitionMatches(competition *types.HackathonCompetition, confs []*types.Conf, query string) bool {
	if competition == nil {
		return false
	}
	fields := []string{competition.Title}
	if conf := confForHackathon(confs, competition); conf != nil {
		fields = append(fields, publicHackathonConferenceName(conf.Desc), conf.Tag)
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

func sortHackathonCompetitions(competitions []*types.HackathonCompetition, confs []*types.Conf, mode string) {
	sort.SliceStable(competitions, func(i, j int) bool {
		left := competitions[i]
		right := competitions[j]
		if left == nil {
			return false
		}
		if right == nil {
			return true
		}
		switch mode {
		case hackathonSortOldest:
			if !left.CreatedAt.Equal(right.CreatedAt) {
				return left.CreatedAt.Before(right.CreatedAt)
			}
		case hackathonSortTitle:
			if compare := strings.Compare(strings.ToLower(left.Title), strings.ToLower(right.Title)); compare != 0 {
				return compare < 0
			}
		case hackathonSortConference:
			leftConf := strings.ToLower(publicHackathonCompetitionConferenceName(confs, left))
			rightConf := strings.ToLower(publicHackathonCompetitionConferenceName(confs, right))
			if compare := strings.Compare(leftConf, rightConf); compare != 0 {
				return compare < 0
			}
		default:
			if !left.CreatedAt.Equal(right.CreatedAt) {
				return left.CreatedAt.After(right.CreatedAt)
			}
		}
		return strings.ToLower(left.Title) < strings.ToLower(right.Title)
	})
}

func publicHackathonCompetitionConferenceName(confs []*types.Conf, competition *types.HackathonCompetition) string {
	conf := confForHackathon(confs, competition)
	if conf == nil {
		return ""
	}
	if conf.Desc != "" {
		return publicHackathonConferenceName(conf.Desc)
	}
	return conf.Tag
}

func (p *HackathonPage) CompetitionURL(competition *types.HackathonCompetition) string {
	return hackathonURLForConf(p.competitionConf(competition))
}

func (p *ConfPage) HackathonURL() string {
	if p == nil {
		return ""
	}
	return hackathonURLForConf(p.Conf)
}

func (p *ConfPage) HackathonScheduleURL() string {
	if p == nil {
		return ""
	}
	return hackathonScheduleURLForConf(p.Conf)
}

func (p *ConfPage) HackathonCreateProjectURL() string {
	if p == nil || p.Hackathon == nil {
		return ""
	}
	return hackathonURLForConf(p.Conf) + "/projects/new"
}

func (p *ConfPage) HackathonAdminURL() string {
	if p == nil || p.Hackathon == nil {
		return "/admin/hackathons"
	}
	return "/" + url.PathEscape(p.Conf.Tag) + "/admin/hackathon"
}

func (p *ConfPage) HackathonStatusLabel() string {
	if p == nil {
		return ""
	}
	return hackathonLifecycleLabel(p.Hackathon)
}

func (p *ConfPage) HackathonAcceptsProjects() bool {
	if p == nil {
		return false
	}
	return competitionAcceptsProjects(p.Hackathon)
}

func (p *ConfPage) HackathonTimeline() HackathonTimelineView {
	if p == nil {
		return HackathonTimelineView{Label: "Timeline", Value: "TBA"}
	}
	if event := nextHackathonScheduleEvent(p.HackathonScheduleEvents); event != nil {
		return HackathonTimelineView{
			Label:         lowerFirst(event.Label) + " in",
			Value:         formatHackathonRelativeDuration(time.Until(*event.Time)),
			HasCountdown:  true,
			CountdownUnix: event.Time.Unix(),
		}
	}
	label, milestoneAt := hackathonNextMilestoneTime(p.Hackathon)
	if milestoneAt != nil {
		return HackathonTimelineView{
			Label:         lowerFirst(label) + " in",
			Value:         formatHackathonRelativeDuration(time.Until(*milestoneAt)),
			HasCountdown:  true,
			CountdownUnix: milestoneAt.Unix(),
		}
	}
	if label == "View schedule" {
		value := completedHackathonScheduleValue(p.Hackathon)
		if value != "" {
			return HackathonTimelineView{
				Label: "Schedule",
				Value: value,
			}
		}
	}
	return HackathonTimelineView{
		Label: "Timeline",
		Value: "TBA",
	}
}

func (p *ConfPage) HackathonJudgeNote(defaultNote string) string {
	if p == nil || len(p.HackathonJudges) == 0 {
		return strings.TrimSpace(defaultNote)
	}
	names := make([]string, 0, len(p.HackathonJudges))
	for _, judge := range p.HackathonJudges {
		if judge == nil {
			continue
		}
		name := strings.TrimSpace(judge.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return strings.TrimSpace(defaultNote)
	}
	if len(names) == 1 {
		return "Judged by " + names[0]
	}
	if len(names) == 2 {
		return "Judged by " + names[0] + " and " + names[1]
	}
	displayNames := names
	if len(displayNames) > 5 {
		displayNames = displayNames[:5]
	}
	note := "Judged by " + strings.Join(displayNames[:len(displayNames)-1], ", ") + ", and " + displayNames[len(displayNames)-1]
	if remaining := len(names) - len(displayNames); remaining > 0 {
		note += fmt.Sprintf(" + %d more", remaining)
	}
	return note
}

func (p *HackathonPage) CompetitionScheduleURL(competition *types.HackathonCompetition) string {
	return hackathonScheduleURLForConf(p.competitionConf(competition))
}

func (p *HackathonPage) CompetitionStatusLabelFor(competition *types.HackathonCompetition) string {
	return hackathonLifecycleLabel(competition)
}

func (p *HackathonPage) CompetitionTimeline(competition *types.HackathonCompetition) HackathonTimelineView {
	if event := nextHackathonScheduleEvent(p.scheduleEventsForCompetition(competition)); event != nil {
		return HackathonTimelineView{
			Label:         lowerFirst(event.Label) + " in",
			Value:         formatHackathonRelativeDuration(time.Until(*event.Time)),
			HasCountdown:  true,
			CountdownUnix: event.Time.Unix(),
		}
	}
	label, milestoneAt := hackathonNextMilestoneTime(competition)
	if milestoneAt != nil {
		return HackathonTimelineView{
			Label:         lowerFirst(label) + " in",
			Value:         formatHackathonRelativeDuration(time.Until(*milestoneAt)),
			HasCountdown:  true,
			CountdownUnix: milestoneAt.Unix(),
		}
	}
	if label == "View schedule" {
		value := completedHackathonScheduleValue(competition)
		if value != "" {
			return HackathonTimelineView{
				Label: "Schedule",
				Value: value,
			}
		}
	}
	return HackathonTimelineView{
		Label: "Timeline",
		Value: "TBA",
	}
}

func (p *HackathonPage) CompetitionAcceptsProjects(competition *types.HackathonCompetition) bool {
	return competitionAcceptsProjects(competition)
}

func (p *HackathonPage) CompetitionAdminEditURL(competition *types.HackathonCompetition) string {
	if competition == nil {
		return "/admin/hackathons"
	}
	if conf := p.competitionConf(competition); conf != nil {
		return "/" + url.PathEscape(conf.Tag) + "/admin/hackathon"
	}
	return "/admin/hackathons/" + url.PathEscape(competition.ID)
}

func (p *HackathonPage) CompetitionVisibleToAdmin(competition *types.HackathonCompetition) bool {
	if competition == nil || competition.Visibility == getters.CompetitionVisibilityPublic {
		return false
	}
	return p.CompetitionCanAdminEdit(competition)
}

func (p *HackathonPage) CompetitionCanAdminEdit(competition *types.HackathonCompetition) bool {
	if competition == nil {
		return false
	}
	if p == nil {
		return false
	}
	viewer := hackathonViewerFromIdentity(p.Viewer, p.competitionConf(competition))
	if viewer.Admin {
		return true
	}
	if p.Viewer == nil || p.Viewer.Speaker == nil {
		return false
	}
	for _, judge := range p.Judges {
		if judge == nil || judge.PersonID != p.Viewer.Speaker.ID {
			continue
		}
		for _, judgeType := range judge.JudgeTypes {
			if judgeType == getters.JudgeTypeCoordinator {
				return true
			}
		}
		return len(judge.JudgeTypes) == 0 && judge.JudgeType == getters.JudgeTypeCoordinator
	}
	return false
}

func (p *HackathonPage) competitionConf(competition *types.HackathonCompetition) *types.Conf {
	if p == nil {
		return nil
	}
	if competition != nil && p.Conf != nil && p.Conf.Ref == competition.ConferenceID {
		return p.Conf
	}
	return confForHackathon(p.Confs, competition)
}

func confForHackathon(confs []*types.Conf, competition *types.HackathonCompetition) *types.Conf {
	if competition == nil || strings.TrimSpace(competition.ConferenceID) == "" {
		return nil
	}
	for _, conf := range confs {
		if conf != nil && conf.Ref == competition.ConferenceID {
			return conf
		}
	}
	return nil
}

func (p *HackathonPage) ProjectTagsCSV() string {
	if p == nil || p.Project == nil {
		return ""
	}
	return strings.Join(p.Project.Tags, ", ")
}

func (p *HackathonPage) HackathonURL() string {
	if p == nil {
		return ""
	}
	return hackathonURLForConf(p.Conf)
}

func (p *HackathonPage) ProjectNewURL() string {
	if p == nil {
		return ""
	}
	base := p.HackathonURL()
	if base == "" {
		return ""
	}
	return base + "/projects/new"
}

func (p *HackathonPage) PrimaryProjectAction() HackathonPrimaryAction {
	if p == nil {
		return HackathonPrimaryAction{Label: "Coming soon", Disabled: true}
	}
	if project := p.PrimaryOwnedProject(); project != nil {
		return HackathonPrimaryAction{Label: "Edit project →", URL: p.ProjectEditURL(project)}
	}
	if competitionAcceptsProjects(p.Competition) {
		if p.Viewer != nil && hackathonViewerPersonID(p.Viewer) != "" && !p.HasConferenceTicket {
			return HackathonPrimaryAction{Label: "Buy ticket →", URL: p.TicketURL()}
		}
		return HackathonPrimaryAction{Label: "Create project →", URL: p.ProjectNewURL()}
	}
	if competitionSubmissionsUpcoming(p.Competition) {
		return HackathonPrimaryAction{Label: "Coming soon", Disabled: true}
	}
	return HackathonPrimaryAction{Label: "Submissions closed", Disabled: true}
}

func (p *HackathonPage) ProjectCreateURL() string {
	if p == nil {
		return ""
	}
	base := p.HackathonURL()
	if base == "" {
		return ""
	}
	return base + "/projects"
}

func (p *HackathonPage) TicketURL() string {
	if p == nil {
		return ""
	}
	return ticketURLForConf(p.Conf)
}

func (p *HackathonPage) ProjectURL(project *types.HackathonProject) string {
	if p == nil || project == nil {
		return ""
	}
	base := p.HackathonURL()
	if base == "" {
		return ""
	}
	return base + "/projects/" + url.PathEscape(project.ID)
}

func (p *HackathonPage) ProjectEditURL(project *types.HackathonProject) string {
	if p == nil {
		return ""
	}
	return projectEditURLForConf(p.Conf, project)
}

func projectEditURLForConf(conf *types.Conf, project *types.HackathonProject) string {
	if project == nil {
		return ""
	}
	base := hackathonURLForConf(conf)
	if base == "" {
		return ""
	}
	return base + "/projects/" + url.PathEscape(project.ID) + "/edit"
}

func (p *HackathonPage) ProjectSubmitURL(project *types.HackathonProject) string {
	projectURL := p.ProjectURL(project)
	if projectURL == "" {
		return ""
	}
	return projectURL + "/submit"
}

func (p *HackathonPage) ProjectInviteURL(project *types.HackathonProject) string {
	projectURL := p.ProjectURL(project)
	if projectURL == "" {
		return ""
	}
	return projectURL + "/invites"
}

func (p *HackathonPage) ProjectMemberRemoveURL(project *types.HackathonProject) string {
	projectURL := p.ProjectURL(project)
	if projectURL == "" {
		return ""
	}
	return projectURL + "/team/remove"
}

func (p *HackathonPage) LoginURL() string {
	if p == nil {
		return "/login"
	}
	next := p.HackathonURL()
	if next == "" {
		return "/login"
	}
	return "/login?next=" + url.QueryEscape(next)
}

func (p *HackathonPage) CanManageProject(projectID string) bool {
	return p != nil && p.OwnedProjects != nil && p.OwnedProjects[projectID]
}

func (p *HackathonPage) PrimaryOwnedProject() *types.HackathonProject {
	if p == nil || len(p.OwnedProjects) == 0 {
		return nil
	}
	for _, project := range p.Projects {
		if project != nil && p.CanManageProject(project.ID) {
			return project
		}
	}
	return nil
}

func (p *HackathonPage) MyProjects() []*types.HackathonProject {
	if p == nil || len(p.OwnedProjects) == 0 {
		return nil
	}
	out := make([]*types.HackathonProject, 0, len(p.OwnedProjects))
	for _, project := range p.Projects {
		if project != nil && p.CanManageProject(project.ID) {
			out = append(out, project)
		}
	}
	return out
}

func (p *HackathonPage) GalleryProjects() []*types.HackathonProject {
	if p == nil || !p.ProjectGalleryOpen() {
		return nil
	}
	projects := make([]*types.HackathonProject, 0, len(p.Projects))
	for _, project := range p.Projects {
		if project == nil || project.Status == getters.ProjectStatusCreated || project.Status == getters.ProjectStatusHidden {
			continue
		}
		projects = append(projects, project)
	}
	if p.Competition == nil || p.Competition.ResultsFinalizedAt == nil {
		return projects
	}
	projectAwardRank := func(project *types.HackathonProject) (bool, int64) {
		var finalistsOnly bool
		var totalValue int64
		for _, award := range p.ProjectWinningAwards(project) {
			if award == nil {
				continue
			}
			finalistsOnly = finalistsOnly || award.FinalistsOnly
			for _, prize := range p.AwardPrizes(award) {
				totalValue += prizeValueSats(prize)
			}
		}
		return finalistsOnly, totalValue
	}
	sort.SliceStable(projects, func(i, j int) bool {
		aFinalist, aValue := projectAwardRank(projects[i])
		bFinalist, bValue := projectAwardRank(projects[j])
		if aFinalist != bFinalist {
			return aFinalist
		}
		if aValue != bValue {
			return aValue > bValue
		}
		return false
	})
	return projects
}

func (p *HackathonPage) ProjectGalleryOpen() bool {
	return p != nil && p.Competition != nil && p.Competition.PublicGalleryEnabled
}

func (p *HackathonPage) FeaturedProjects() []*types.HackathonProject {
	if p == nil {
		return nil
	}
	const limit = 3
	featured := make([]*types.HackathonProject, 0, limit)
	seen := make(map[string]bool, limit)
	appendProject := func(project *types.HackathonProject) {
		if project == nil || seen[project.ID] || len(featured) >= limit {
			return
		}
		seen[project.ID] = true
		featured = append(featured, project)
	}
	for _, project := range p.GalleryProjects() {
		if len(p.ProjectWinningAwards(project)) > 0 {
			appendProject(project)
		}
	}
	for _, project := range p.GalleryProjects() {
		appendProject(project)
	}
	return featured
}

func (p *HackathonPage) CanAdminEdit() bool {
	return p != nil && p.CompetitionCanAdminEdit(p.Competition)
}

func (p *HackathonPage) AdminEditURL() string {
	if p == nil || p.Competition == nil {
		return "/admin/hackathons"
	}
	return "/" + url.PathEscape(p.Conf.Tag) + "/admin/hackathon"
}

func (p *HackathonPage) JudgingURL() string {
	if p == nil || p.Competition == nil || p.Conf == nil {
		return ""
	}
	return hackathonURLForConf(p.Conf) + "/judging"
}

func (p *HackathonPage) CompetitionStatusLabel() string {
	if p == nil || p.Competition == nil {
		return ""
	}
	return hackathonLifecycleLabel(p.Competition)
}

func (p *HackathonPage) ProjectStatusLabel(status string) string {
	return hackathonStatusLabel(status)
}

func (p *HackathonPage) ProjectIsAdvanced(project *types.HackathonProject) bool {
	return project != nil && project.Status == getters.ProjectStatusAdvanced
}

func (p *HackathonPage) ProjectNumberLabel(project *types.HackathonProject) string {
	if project == nil || project.ProjectNumber == nil {
		return "Unassigned"
	}
	return fmt.Sprintf("#%d", *project.ProjectNumber)
}

func (p *HackathonPage) JudgeTypeLabel(judgeType string) string {
	switch strings.TrimSpace(judgeType) {
	case getters.JudgeTypeExpo:
		return "Expo"
	case getters.JudgeTypeFinals:
		return "Finals"
	case getters.JudgeTypeCoordinator:
		return "Coordinator"
	default:
		return strings.TrimSpace(judgeType)
	}
}

func (p *HackathonPage) JudgeRoleTypes(judge *types.CompetitionJudge) []string {
	if judge == nil {
		return nil
	}
	if len(judge.JudgeTypes) > 0 {
		return judge.JudgeTypes
	}
	if judge.JudgeType != "" {
		return []string{judge.JudgeType}
	}
	return nil
}

func (p *HackathonPage) PublicJudgeRoleLabel(judge *types.CompetitionJudge) string {
	if judge != nil {
		if override := strings.TrimSpace(judge.PublicLabelOverride); override != "" {
			return override
		}
		if company := strings.TrimSpace(judge.Company); company != "" {
			return company
		}
	}
	var isJudge, isCoordinator bool
	for _, judgeType := range p.JudgeRoleTypes(judge) {
		switch judgeType {
		case getters.JudgeTypeExpo, getters.JudgeTypeFinals:
			isJudge = true
		case getters.JudgeTypeCoordinator:
			isCoordinator = true
		}
	}
	switch {
	case isJudge:
		return "Judge"
	case isCoordinator:
		return "Coordinator"
	default:
		return "Judge"
	}
}

func (p *HackathonPage) JudgeEventStateLabel(event *types.JudgeEvent) string {
	return judgeEventStateLabel(event)
}

func (p *HackathonPage) CurrentJudgeEvent() *types.JudgeEvent {
	events := currentJudgeEvents(p.JudgeEvents)
	if len(events) == 0 {
		return nil
	}
	return events[0]
}

func (p *HackathonPage) JudgeNumber(index int) string {
	return fmt.Sprintf("JUDGE #%02d", index+1)
}

func (p *HackathonPage) Scorecard(projectID, judgeEventID string) *types.Scorecard {
	if p == nil {
		return nil
	}
	for _, scorecard := range p.Scorecards {
		if scorecard != nil && scorecard.ProjectID == projectID && scorecard.JudgeEventID == judgeEventID {
			return scorecard
		}
	}
	return nil
}

func (p *HackathonPage) ScoreValue(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func (p *HackathonPage) RankLimit(event *types.JudgeEvent) int {
	return judgeEventRankLimit(event)
}

func (p *HackathonPage) RankOptions(event *types.JudgeEvent) []int {
	limit := judgeEventRankLimit(event)
	options := make([]int, limit)
	for i := range options {
		options[i] = i + 1
	}
	return options
}

func (p *HackathonPage) RankOptionLabel(event *types.JudgeEvent, rank int) string {
	points := rankPoints(event, rank)
	if points == 1 {
		return fmt.Sprintf("%s (%d point)", ordinal(rank), points)
	}
	return fmt.Sprintf("%s (%d points)", ordinal(rank), points)
}

func (p *HackathonPage) CanScoreJudgeEvent(event *types.JudgeEvent) bool {
	if p == nil || event == nil {
		return false
	}
	if !judgeEventAcceptsScores(event) {
		return false
	}
	if p.CanScoreAll {
		return true
	}
	return p.JudgeTypes[event.PlaybookType]
}

func (p *HackathonPage) Awardees(award *types.Award) []*types.ProjectAward {
	if p == nil || p.AwardeesByAward == nil || award == nil {
		return nil
	}
	return p.AwardeesByAward[award.ID]
}

func (p *HackathonPage) JudgeProfileURL(judge *types.CompetitionJudge) string {
	if p == nil || judge == nil || p.JudgeProfileURLs == nil {
		return ""
	}
	return p.JudgeProfileURLs[judge.PersonID]
}

func (p *HackathonPage) AwardIsChallenge(award *types.Award) bool {
	return award != nil && strings.TrimSpace(award.AwardType) == getters.AwardTypeChallenge
}

func (p *HackathonPage) AwardHasSponsor(award *types.Award) bool {
	return award != nil && strings.TrimSpace(award.SponsoredByOrgID) != ""
}

func (p *HackathonPage) AwardSponsorLabel(award *types.Award) string {
	if org := p.AwardSponsorOrg(award); org != nil && strings.TrimSpace(org.Name) != "" {
		return strings.TrimSpace(org.Name)
	}
	return "Sponsor TBD"
}

func (p *HackathonPage) AwardSponsorURL(award *types.Award) string {
	if org := p.AwardSponsorOrg(award); org != nil && strings.TrimSpace(org.Website) != "" {
		return strings.TrimSpace(org.Website)
	}
	return ""
}

func (p *HackathonPage) AwardSponsorOrg(award *types.Award) *types.Org {
	if p == nil || award == nil || p.OrgsByID == nil {
		return nil
	}
	return p.OrgsByID[strings.TrimSpace(award.SponsoredByOrgID)]
}

func (p *HackathonPage) AwardLogoURL(award *types.Award) string {
	org := p.AwardSponsorOrg(award)
	if org == nil {
		return ""
	}
	return orgLogoURL(org)
}

func (p *HackathonPage) AwardLogoAlt(award *types.Award) string {
	if p.AwardHasSponsor(award) {
		return p.AwardSponsorLabel(award)
	}
	if award != nil && strings.TrimSpace(award.Title) != "" {
		return strings.TrimSpace(award.Title)
	}
	return "Award"
}

func (p *HackathonPage) AwardVote(award *types.Award) *types.AwardVote {
	if p == nil || award == nil {
		return nil
	}
	for _, vote := range p.AwardVotes {
		if vote != nil && vote.AwardID == award.ID {
			return vote
		}
	}
	return nil
}

func (p *HackathonPage) ProjectSelectedForAward(project *types.HackathonProject, award *types.Award) bool {
	if project == nil {
		return false
	}
	vote := p.AwardVote(award)
	return vote != nil && vote.ProjectID == project.ID
}

func (p *HackathonPage) ChallengeAwardProjectOptions(award *types.Award) []*types.HackathonProject {
	if p == nil || award == nil {
		return nil
	}
	projects := p.ChallengeProjects
	if projects == nil {
		projects = p.Projects
	}
	if !award.OptInRequired {
		return projects
	}
	var out []*types.HackathonProject
	for _, project := range projects {
		if project != nil && p.AwardOptIns != nil && p.AwardOptIns[project.ID+"|"+award.ID] {
			out = append(out, project)
		}
	}
	return out
}

func (p *HackathonPage) ProjectWinningAwards(project *types.HackathonProject) []*types.Award {
	if p == nil || project == nil {
		return nil
	}
	var awards []*types.Award
	for _, award := range p.Awards {
		for _, awardee := range p.Awardees(award) {
			if awardee != nil && awardee.ProjectID == project.ID {
				awards = append(awards, award)
				break
			}
		}
	}
	sortPublicHackathonAwards(awards, p.PrizesByAward)
	return awards
}

func (p *HackathonPage) AwardWinnerBadgeLabel(award *types.Award) string {
	if award == nil {
		return "Winner"
	}
	if award.AwardRank != nil {
		return ordinal(*award.AwardRank) + " place"
	}
	if title := strings.TrimSpace(award.Title); title != "" {
		return title
	}
	if p.AwardHasSponsor(award) {
		return p.AwardSponsorLabel(award)
	}
	return "Winner"
}

func (p *HackathonPage) AwardPrizes(award *types.Award) []*types.Prize {
	if p == nil || p.PrizesByAward == nil || award == nil {
		return nil
	}
	return p.PrizesByAward[award.ID]
}

func (p *HackathonPage) AwardPrizeAmount(award *types.Award) string {
	return hackathonPlacePrizeAmount(p.AwardPrizes(award))
}

func (p *HackathonPage) AwardDisplayLabel(award *types.Award) string {
	if award != nil && award.AwardRank != nil {
		return ordinal(*award.AwardRank) + " place"
	}
	if p.AwardHasSponsor(award) {
		return p.AwardSponsorLabel(award)
	}
	if p.AwardIsChallenge(award) {
		return "Challenge"
	}
	return "Award"
}

func (p *HackathonPage) ChallengeAwards() []*types.Award {
	if p == nil {
		return nil
	}
	challenges := make([]*types.Award, 0, len(p.Awards))
	for _, award := range p.Awards {
		if !p.AwardIsChallenge(award) {
			continue
		}
		challenges = append(challenges, award)
	}
	return challenges
}

func (p *HackathonPage) AdditionalOverviewAwards() []*types.Award {
	if p == nil {
		return nil
	}
	awards := make([]*types.Award, 0, len(p.Awards))
	for _, award := range p.Awards {
		if award == nil || p.AwardIsChallenge(award) {
			continue
		}
		rank := hackathonPlaceAwardRank(award)
		if rank >= 1 && rank <= 3 {
			continue
		}
		awards = append(awards, award)
	}
	return awards
}

func (p *HackathonPage) PrizePoolValue() string {
	sats := p.PrizePoolSats()
	return strings.TrimSuffix(compactSatoshiLabel(sats), " satoshis")
}

func (p *HackathonPage) PrizePoolSats() int64 {
	if p == nil {
		return 0
	}
	prizesByAward := p.PrizePoolByAward
	if prizesByAward == nil {
		prizesByAward = p.PrizesByAward
	}
	var total int64
	for _, prizes := range prizesByAward {
		for _, prize := range prizes {
			total += prizeValueSats(prize)
		}
	}
	return total
}

func (p *HackathonPage) AwardOptedIn(award *types.Award) bool {
	if p == nil || p.AwardOptIns == nil || award == nil {
		return false
	}
	return p.AwardOptIns[award.ID]
}

func (p *HackathonPage) ProjectAwardNumber(award *types.ProjectAward) string {
	if award == nil || award.ProjectNumber == nil {
		return "TBA"
	}
	return strconv.Itoa(*award.ProjectNumber)
}

func (p *HackathonPage) PrizeTypeLabel(prizeType string) string {
	switch strings.TrimSpace(prizeType) {
	case getters.PrizeTypeSats:
		return "Sats"
	case getters.PrizeTypeInKind:
		return "In-kind"
	case getters.PrizeTypeTickets:
		return "Tickets"
	case getters.PrizeTypePooled:
		return "Pooled"
	case getters.PrizeTypeTrophy:
		return "Trophy"
	default:
		return strings.TrimSpace(prizeType)
	}
}

func (p *HackathonPage) PercentLabel(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64) + "%"
}

func prizeValueSats(prize *types.Prize) int64 {
	if prize == nil {
		return 0
	}
	text := strings.ToLower(strings.TrimSpace(prize.ValueText))
	match := bitcoinPrizeAmountPattern.FindString(text)
	if match == "" {
		return 0
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(match, ",", ""), 64)
	if err != nil || value <= 0 {
		return 0
	}
	if strings.Contains(text, "btc") || strings.Contains(text, "₿") {
		return int64(value*100_000_000 + 0.5)
	}
	return int64(value + 0.5)
}

func prizeValueLabel(prize *types.Prize) string {
	sats := prizeValueSats(prize)
	if sats <= 0 {
		if prize == nil {
			return ""
		}
		return strings.TrimSpace(prize.ValueText)
	}
	return compactSatoshiLabel(sats)
}

func (p *HackathonPage) PrizeValueLabel(prize *types.Prize) string {
	return prizeValueLabel(prize)
}

func (p *HackathonPage) JudgeEventTimeRange(event *types.JudgeEvent) string {
	if p == nil {
		return formatJudgeEventTimeRange(event, nil)
	}
	return formatJudgeEventTimeRange(event, p.Conf)
}

func (p *HackathonPage) RichText(value string) template.HTML {
	return hackathonRichTextHTML(value)
}

func (p *HackathonPage) DescriptionHTML(value, format string) template.HTML {
	return hackathonDescriptionHTML(value, format)
}

func (p *HackathonPage) ProjectDescriptionHTML(project *types.HackathonProject) template.HTML {
	if project == nil {
		return ""
	}
	return hackathonDescriptionHTML(project.Description, project.DescriptionFormat)
}

func (p *HackathonPage) ProjectMembers(project *types.HackathonProject) []*types.ProjectMember {
	if p == nil || project == nil || p.ProjectMembersByProject == nil {
		return nil
	}
	return p.ProjectMembersByProject[project.ID]
}

func (p *HackathonPage) ProjectImageGallery(project *types.HackathonProject) []string {
	if project == nil {
		return nil
	}
	values := append([]string{project.ImageURL}, project.ImageURLs...)
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func (p *HackathonPage) NextMilestoneLabel() string {
	if p == nil {
		return ""
	}
	if event := nextHackathonScheduleEvent(p.ScheduleEventList); event != nil {
		return event.Label
	}
	label, _ := hackathonNextMilestone(p.Competition)
	return label
}

func (p *HackathonPage) NextMilestoneValue() string {
	if p == nil {
		return ""
	}
	if event := nextHackathonScheduleEvent(p.ScheduleEventList); event != nil {
		return formatHackathonScheduleEventTime(event)
	}
	if value := completedHackathonScheduledEventRange(p.ScheduleEventList); value != "" {
		return value
	}
	_, milestoneAt := hackathonNextMilestoneTime(p.Competition)
	if milestoneAt == nil {
		return ""
	}
	loc := time.Local
	if p.Conf != nil {
		loc = p.Conf.Loc()
	}
	return milestoneAt.In(loc).Format("2006-01-02 15:04 MST")
}

func (p *HackathonPage) NextMilestoneTime() *time.Time {
	if p == nil {
		return nil
	}
	if event := nextHackathonScheduleEvent(p.ScheduleEventList); event != nil {
		return event.Time
	}
	_, milestoneAt := hackathonNextMilestoneTime(p.Competition)
	return milestoneAt
}

func (p *HackathonPage) NextMilestoneIsScheduleLink() bool {
	if p == nil {
		return false
	}
	if nextHackathonScheduleEvent(p.ScheduleEventList) != nil {
		return false
	}
	if len(p.ScheduleEventList) > 0 {
		return true
	}
	return hackathonMilestoneIsScheduleLink(p.Competition)
}

func (p *HackathonPage) ScheduleURL() string {
	if p == nil {
		return ""
	}
	return hackathonScheduleURLForConf(p.Conf)
}

func (p *HackathonPage) ScheduleEvents() []HackathonScheduleEvent {
	if p == nil || p.Competition == nil {
		return nil
	}
	if len(p.ScheduleEventList) > 0 {
		return p.ScheduleEventList
	}
	competition := p.Competition
	publicAt := competition.PublicGalleryAt
	if publicAt == nil {
		publicAt = competition.SubmissionsCloseAt
	}
	return []HackathonScheduleEvent{
		{Label: "Submissions open", Time: competition.SubmissionsOpenAt},
		{Label: "Submissions close", Time: competition.SubmissionsCloseAt},
		{Label: "Submissions go public", Time: publicAt},
	}
}

func (p *HackathonPage) scheduleEventsForCompetition(competition *types.HackathonCompetition) []HackathonScheduleEvent {
	if p == nil || competition == nil {
		return nil
	}
	if p.ScheduleEventsByCompetition == nil {
		return nil
	}
	return p.ScheduleEventsByCompetition[competition.ID]
}

func hackathonNextMilestone(competition *types.HackathonCompetition) (string, string) {
	label, milestoneAt := hackathonNextMilestoneTime(competition)
	if milestoneAt != nil {
		return label, formatHackathonTime(milestoneAt)
	}
	if label == "View schedule" {
		return label, completedHackathonScheduleValue(competition)
	}
	return "", ""
}

func nextHackathonScheduleEvent(events []HackathonScheduleEvent) *HackathonScheduleEvent {
	now := time.Now()
	var next *HackathonScheduleEvent
	for i := range events {
		event := &events[i]
		if event.Time == nil || !event.Time.After(now) {
			continue
		}
		if next == nil || event.Time.Before(*next.Time) {
			next = event
		}
	}
	return next
}

func formatHackathonScheduleEventTime(event *HackathonScheduleEvent) string {
	if event == nil || event.Time == nil {
		return ""
	}
	if event.End != nil {
		return event.Time.Format("2006-01-02 15:04") + " - " + event.End.Format("15:04")
	}
	return formatHackathonTime(event.Time)
}

func completedHackathonScheduledEventRange(events []HackathonScheduleEvent) string {
	var first, last *time.Time
	for i := range events {
		event := events[i]
		if event.Time != nil && (first == nil || event.Time.Before(*first)) {
			first = event.Time
		}
		if event.End != nil && (last == nil || event.End.After(*last)) {
			last = event.End
		} else if event.Time != nil && (last == nil || event.Time.After(*last)) {
			last = event.Time
		}
	}
	if first == nil || last == nil {
		return ""
	}
	if first.Format("2006-01-02") == last.Format("2006-01-02") {
		return first.Format("2006-01-02 15:04") + " - " + last.Format("15:04")
	}
	return first.Format("2006-01-02 15:04") + " - " + last.Format("2006-01-02 15:04")
}

func formatJudgeEventTimeRange(event *types.JudgeEvent, conf *types.Conf) string {
	if event == nil || (event.StartsAt == nil && event.EndsAt == nil) {
		return ""
	}
	loc := time.Local
	if conf != nil {
		loc = conf.Loc()
	}
	if event.StartsAt == nil {
		return event.EndsAt.In(loc).Format("Jan 2, 2006 3:04 PM MST")
	}
	if event.EndsAt == nil {
		return event.StartsAt.In(loc).Format("Jan 2, 2006 3:04 PM MST")
	}
	start := event.StartsAt.In(loc)
	end := event.EndsAt.In(loc)
	if start.Format("2006-01-02") == end.Format("2006-01-02") {
		return start.Format("Jan 2, 2006 3:04 PM") + " - " + end.Format("3:04 PM MST")
	}
	return start.Format("Jan 2, 2006 3:04 PM MST") + " - " + end.Format("Jan 2, 2006 3:04 PM MST")
}

func formatJudgeEventTimeOnlyRange(event *types.JudgeEvent, conf *types.Conf) string {
	if event == nil || (event.StartsAt == nil && event.EndsAt == nil) {
		return ""
	}
	loc := time.Local
	if conf != nil {
		loc = conf.Loc()
	}
	if event.StartsAt == nil {
		return event.EndsAt.In(loc).Format("3:04 PM MST")
	}
	if event.EndsAt == nil {
		return event.StartsAt.In(loc).Format("3:04 PM MST")
	}
	start := event.StartsAt.In(loc)
	end := event.EndsAt.In(loc)
	if start.Format("2006-01-02") == end.Format("2006-01-02") {
		return start.Format("3:04 PM") + " - " + end.Format("3:04 PM MST")
	}
	return start.Format("3:04 PM MST") + " - " + end.Format("3:04 PM MST")
}

func hackathonNextMilestoneTime(competition *types.HackathonCompetition) (string, *time.Time) {
	if competition == nil {
		return "", nil
	}
	now := time.Now()
	if competition.SubmissionsOpenAt != nil && competition.SubmissionsOpenAt.After(now) {
		return "Submissions open", competition.SubmissionsOpenAt
	}
	if competition.SubmissionsCloseAt != nil && competition.SubmissionsCloseAt.After(now) {
		return "Submissions close", competition.SubmissionsCloseAt
	}
	if competition.PublicGalleryAt != nil && competition.PublicGalleryAt.After(now) {
		return "Submissions go public", competition.PublicGalleryAt
	}
	if hackathonMilestoneIsScheduleLink(competition) {
		return "View schedule", nil
	}
	return "", nil
}

func hackathonMilestoneIsScheduleLink(competition *types.HackathonCompetition) bool {
	if competition == nil {
		return false
	}
	now := time.Now()
	if competition.SubmissionsOpenAt != nil && competition.SubmissionsOpenAt.After(now) {
		return false
	}
	if competition.SubmissionsCloseAt != nil && competition.SubmissionsCloseAt.After(now) {
		return false
	}
	if competition.PublicGalleryAt != nil && competition.PublicGalleryAt.After(now) {
		return false
	}
	return competition.SubmissionsOpenAt != nil || competition.SubmissionsCloseAt != nil || competition.PublicGalleryAt != nil
}

func formatHackathonTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

func formatHackathonRelativeDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return pluralizeDurationUnit(int(d.Round(time.Minute)/time.Minute), "minute")
	case d < 48*time.Hour:
		return pluralizeDurationUnit(int(d.Round(time.Hour)/time.Hour), "hour")
	case d < 60*24*time.Hour:
		return pluralizeDurationUnit(int(d.Round(24*time.Hour)/(24*time.Hour)), "day")
	default:
		return pluralizeDurationUnit(int(d.Round(30*24*time.Hour)/(30*24*time.Hour)), "month")
	}
}

func pluralizeDurationUnit(value int, unit string) string {
	if value < 1 {
		value = 1
	}
	if value == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", value, unit)
}

func completedHackathonScheduleValue(competition *types.HackathonCompetition) string {
	if competition == nil {
		return ""
	}
	if competition.SubmissionsOpenAt != nil && competition.SubmissionsCloseAt != nil {
		return formatHackathonTime(competition.SubmissionsOpenAt) + " - " + formatHackathonTime(competition.SubmissionsCloseAt)
	}
	if competition.PublicGalleryAt != nil {
		return formatHackathonTime(competition.PublicGalleryAt)
	}
	if competition.SubmissionsCloseAt != nil {
		return formatHackathonTime(competition.SubmissionsCloseAt)
	}
	if competition.SubmissionsOpenAt != nil {
		return formatHackathonTime(competition.SubmissionsOpenAt)
	}
	return ""
}

func hackathonDescriptionHTML(value, format string) template.HTML {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case getters.CompetitionDescriptionFormatPlain:
		return hackathonPlainTextHTML(value)
	case getters.CompetitionDescriptionFormatMarkdown:
		return hackathonMarkdownHTML(value)
	default:
		return hackathonRichTextHTML(value)
	}
}

func hackathonPlainTextHTML(value string) template.HTML {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	escaped := stdhtml.EscapeString(value)
	escaped = strings.ReplaceAll(escaped, "\r\n", "\n")
	escaped = strings.ReplaceAll(escaped, "\r", "\n")
	escaped = strings.ReplaceAll(escaped, "\n", "<br>")
	return template.HTML(escaped)
}

func hackathonMarkdownHTML(value string) template.HTML {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	escaped := stdhtml.EscapeString(value)
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs | parser.NoEmptyLineBeforeBlock
	doc := parser.NewWithExtensions(extensions).Parse([]byte(escaped))
	renderer := mdhtml.NewRenderer(mdhtml.RendererOptions{
		Flags: mdhtml.CommonFlags,
	})
	return hackathonRichTextHTML(string(markdown.Render(doc, renderer)))
}

func hackathonRichTextHTML(value string) template.HTML {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	nodes, err := html.ParseFragment(strings.NewReader(value), nil)
	if err != nil {
		return template.HTML(stdhtml.EscapeString(value))
	}
	var b strings.Builder
	for _, node := range nodes {
		renderHackathonHTMLNode(&b, node)
	}
	return template.HTML(b.String())
}

func renderHackathonHTMLNode(b *strings.Builder, node *html.Node) {
	if node == nil {
		return
	}
	switch node.Type {
	case html.TextNode:
		b.WriteString(stdhtml.EscapeString(node.Data))
	case html.ElementNode:
		tag := strings.ToLower(node.Data)
		if tag == "script" || tag == "style" {
			return
		}
		if !hackathonAllowedHTMLTag(tag) {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				renderHackathonHTMLNode(b, child)
			}
			return
		}
		b.WriteByte('<')
		b.WriteString(tag)
		if tag == "a" {
			href := safeHackathonHref(node)
			if href != "" {
				b.WriteString(` href="`)
				b.WriteString(stdhtml.EscapeString(href))
				b.WriteString(`" rel="noopener noreferrer"`)
			}
		}
		b.WriteByte('>')
		if !hackathonVoidHTMLTag(tag) {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				renderHackathonHTMLNode(b, child)
			}
			b.WriteString("</")
			b.WriteString(tag)
			b.WriteByte('>')
		}
	case html.DocumentNode:
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderHackathonHTMLNode(b, child)
		}
	}
}

func hackathonAllowedHTMLTag(tag string) bool {
	switch tag {
	case "a", "b", "br", "code", "em", "h2", "h3", "h4", "i", "li", "ol", "p", "pre", "strong", "u", "ul":
		return true
	default:
		return false
	}
}

func hackathonVoidHTMLTag(tag string) bool {
	return tag == "br"
}

func safeHackathonHref(node *html.Node) string {
	for _, attr := range node.Attr {
		if strings.ToLower(attr.Key) != "href" {
			continue
		}
		href := strings.TrimSpace(attr.Val)
		lower := strings.ToLower(href)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(href, "/") || strings.HasPrefix(href, "#") {
			return href
		}
	}
	return ""
}

func loadHackathonScheduleEvents(ctx *config.AppContext, competitionID string) ([]HackathonScheduleEvent, error) {
	segments, err := getters.ListCompetitionScheduleSegments(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	events := make([]HackathonScheduleEvent, 0, len(segments))
	for _, segment := range segments {
		if segment == nil {
			continue
		}
		event := HackathonScheduleEvent{
			SegmentID:   segment.ID,
			Label:       segment.Title,
			SegmentType: segment.SegmentType,
		}
		confTalk, err := confTalkForHackathonScheduleSegment(ctx, segment)
		if err != nil {
			return nil, err
		}
		if confTalk != nil {
			event.Venue = strings.TrimSpace(confTalk.Venue)
			if confTalk.Sched != nil {
				event.Time = &confTalk.Sched.Start
				event.End = confTalk.Sched.End
				if event.End == nil && segment.DefaultDurationMinutes > 0 {
					end := confTalk.Sched.Start.Add(time.Duration(segment.DefaultDurationMinutes) * time.Minute)
					event.End = &end
				}
			}
		}
		events = append(events, event)
	}
	return events, nil
}

func confTalkForHackathonScheduleSegment(ctx *config.AppContext, segment *types.CompetitionScheduleSegment) (*types.ConfTalk, error) {
	if segment == nil {
		return nil, nil
	}
	if strings.TrimSpace(segment.ConfTalkID) != "" {
		confTalk, err := getters.GetConfTalkByID(ctx, segment.ConfTalkID)
		if err != nil || confTalk != nil {
			return confTalk, err
		}
	}
	if strings.TrimSpace(segment.ProposalID) != "" {
		return getters.GetConfTalkByProposal(ctx, segment.ProposalID)
	}
	return nil, nil
}

func HackathonShow(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, projects, err := loadHackathonPageData(w, r, ctx)
	if err != nil {
		return
	}
	personID := hackathonViewerPersonID(id)
	viewer := hackathonViewerFromIdentity(id, conf)
	awards, prizesByAward, prizePoolByAward, awardeesByAward, err := loadPublicHackathonAwards(ctx, competition.ID, competition.ResultsFinalizedAt != nil)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s awards: %s", competition.ID, err)
		http.Error(w, "Unable to load awards", http.StatusInternalServerError)
		return
	}
	orgMap, err := loadHackathonOrgMap(ctx)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s orgs: %s", competition.ID, err)
		http.Error(w, "Unable to load sponsors", http.StatusInternalServerError)
		return
	}
	placeRows, err := loadConfHackathonPlaceRows(ctx, competition.ID, competition.ResultsFinalizedAt != nil, orgMap)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s place rows: %s", competition.ID, err)
		http.Error(w, "Unable to load prizes", http.StatusInternalServerError)
		return
	}
	judges, err := getters.ListCompetitionJudges(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s judges: %s", competition.ID, err)
		http.Error(w, "Unable to load judges", http.StatusInternalServerError)
		return
	}
	projectMembers, err := getters.ListProjectMembersForCompetition(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s project members: %s", competition.ID, err)
		http.Error(w, "Unable to load project teams", http.StatusInternalServerError)
		return
	}
	judgeProfileURLs, err := hackathonJudgeProfileURLs(ctx, judges)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s judge profiles failed (continuing): %s", competition.ID, err)
	}
	scheduleEvents, err := loadHackathonScheduleEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s schedule events: %s", competition.ID, err)
		http.Error(w, "Unable to load schedule", http.StatusInternalServerError)
		return
	}
	scheduleEvents = localizeHackathonScheduleEvents(scheduleEvents, conf.Loc())
	ownedProjects := ownedProjectMap(ctx, projects, personID)
	hasTicket := false
	if id != nil && personID != "" {
		hasTicket, err = viewerHasConferenceTicket(ctx, conf, id)
		if err != nil {
			ctx.Err.Printf("/hackathons/%s ticket lookup failed for %s: %s", competition.ID, id.Email, err)
		}
	}
	page := &HackathonPage{
		Competition:             competition,
		Conf:                    conf,
		OrgsByID:                orgMap,
		Projects:                projects,
		ProjectMembersByProject: projectMembers,
		Judges:                  judges,
		JudgeProfileURLs:        judgeProfileURLs,
		Awards:                  awards,
		PrizesByAward:           prizesByAward,
		PrizePoolByAward:        prizePoolByAward,
		HackathonPlaceRows:      placeRows,
		AwardeesByAward:         awardeesByAward,
		ScheduleEventList:       scheduleEvents,
		Viewer:                  id,
		OwnedProjects:           ownedProjects,
		HasConferenceTicket:     hasTicket,
		CanCreate:               id != nil && hasTicket && competitionAcceptsProjects(competition) && len(ownedProjects) == 0,
		CanJudge:                viewer.Admin || viewer.Coordinator || viewerCanJudgeCompetition(ctx, competition.ID, personID),
		FlashMessage:            r.URL.Query().Get("flash"),
		FlashError:              r.URL.Query().Get("error"),
		Year:                    helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s template: %s", competition.ID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func hackathonJudgeProfileURLs(ctx *config.AppContext, judges []*types.CompetitionJudge) (map[string]string, error) {
	if len(judges) == 0 {
		return nil, nil
	}
	people, err := buildWhoIsDirectory(ctx)
	if err != nil {
		return nil, err
	}
	judgeIDs := make(map[string]bool, len(judges))
	for _, judge := range judges {
		if judge != nil && strings.TrimSpace(judge.PersonID) != "" {
			judgeIDs[judge.PersonID] = true
		}
	}
	urls := make(map[string]string, len(judgeIDs))
	for _, person := range people {
		if person == nil || person.Speaker == nil || !judgeIDs[person.Speaker.ID] || strings.TrimSpace(person.PublicID) == "" {
			continue
		}
		urls[person.Speaker.ID] = "/whois/" + url.PathEscape(person.PublicID)
	}
	return urls, nil
}

func hackathonNavConference(conf *types.Conf, talks []*types.Talk, showHackathon bool) *types.Conf {
	if conf == nil {
		return nil
	}
	copy := *conf
	copy.ShowHackathon = showHackathon
	if talks != nil {
		copy.HasAgenda = anyScheduledTalk(&copy, talks)
	}
	return &copy
}

func publicHackathonNavConference(ctx *config.AppContext, conf *types.Conf, talks []*types.Talk) *types.Conf {
	if conf == nil {
		return nil
	}
	showHackathon := false
	competition, err := getters.GetCompetitionByConferenceID(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s conference nav hackathon lookup failed (continuing): %s", conf.Tag, err)
	} else {
		showHackathon = competition != nil && competition.Visibility == getters.CompetitionVisibilityPublic
	}
	return hackathonNavConference(conf, talks, showHackathon)
}

func HackathonSchedule(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return
	}
	scheduleEvents, err := loadHackathonScheduleEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/schedule events: %s", competition.ID, err)
		http.Error(w, "Unable to load schedule", http.StatusInternalServerError)
		return
	}
	scheduleEvents = localizeHackathonScheduleEvents(scheduleEvents, conf.Loc())
	page := &HackathonPage{
		Competition:       competition,
		Conf:              conf,
		ScheduleEventList: scheduleEvents,
		Viewer:            id,
		Year:              helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon_schedule.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s/schedule template: %s", competition.ID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func localizeHackathonScheduleEvents(events []HackathonScheduleEvent, loc *time.Location) []HackathonScheduleEvent {
	if loc == nil {
		return events
	}
	localized := make([]HackathonScheduleEvent, len(events))
	for i := range events {
		localized[i] = events[i]
		if events[i].Time != nil {
			value := events[i].Time.In(loc)
			localized[i].Time = &value
		}
		if events[i].End != nil {
			value := events[i].End.In(loc)
			localized[i].End = &value
		}
	}
	return localized
}

func HackathonScheduleICS(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, _, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return
	}
	segmentID := strings.TrimSpace(mux.Vars(r)["segmentID"])
	events, err := loadHackathonScheduleEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/schedule/%s/calendar.ics events: %s", competition.ID, segmentID, err)
		http.Error(w, "Unable to load schedule", http.StatusInternalServerError)
		return
	}
	var selected *HackathonScheduleEvent
	for i := range events {
		if events[i].SegmentID == segmentID {
			selected = &events[i]
			break
		}
	}
	if selected == nil || selected.Time == nil || selected.End == nil {
		http.NotFound(w, r)
		return
	}

	calEvent := hackathonScheduleCalendarEvent(conf, competition, selected)
	filename := fmt.Sprintf("%s-hackathon-%s.ics", conf.Tag, segmentID)
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(ics.Render(calEvent))
}

func hackathonScheduleCalendarEvent(conf *types.Conf, competition *types.HackathonCompetition, event *HackathonScheduleEvent) ics.Event {
	location := ""
	if event != nil {
		location = ics.MapVenue(event.Venue)
	}
	if location == "" && conf != nil {
		location = conf.Venue
	}
	var start, end time.Time
	if event != nil {
		if event.Time != nil {
			start = *event.Time
		}
		if event.End != nil {
			end = *event.End
		}
	}
	title := "Hackathon session"
	description := ""
	uidSeed := ""
	if event != nil {
		if strings.TrimSpace(event.Label) != "" {
			title = event.Label
		}
		uidSeed = event.SegmentID
	}
	if competition != nil {
		description = "Hackathon schedule session for " + competition.Title + "."
		uidSeed = competition.ID + "-" + uidSeed
	}
	var loc *time.Location
	if conf != nil {
		loc = conf.Loc()
	}
	return ics.Event{
		Method:        ics.MethodPublish,
		UID:           ics.NewUID("hackathon-schedule", uidSeed),
		Sequence:      0,
		Status:        ics.StatusConfirmed,
		Summary:       title,
		Description:   description,
		Location:      location,
		Start:         start,
		End:           end,
		TZ:            loc,
		Organizer:     ics.ReplyToTalk,
		OrganizerName: ics.ReplyToTalkName,
	}
}

func HackathonJudging(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, events, err := loadHackathonJudgingAccess(w, r, ctx)
	if err != nil {
		return
	}
	currentEvents := currentJudgeEvents(events)
	viewer := hackathonViewerFromIdentity(id, conf)
	judgeTypes := judgeTypesForPerson(ctx, competition.ID, viewer.PersonID)
	canJudge := viewer.Admin || viewer.Coordinator || viewerCanJudgeCompetition(ctx, competition.ID, viewer.PersonID)
	projects, err := getters.ListProjectsForCompetition(ctx, competition.ID, viewer)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/judging list projects: %s", competition.ID, err)
		http.Error(w, "Unable to load projects", http.StatusInternalServerError)
		return
	}
	challengeProjects := hackathonSubmittedProjects(projects)
	projects = projectsForJudgeEvents(projects, events, currentEvents)
	challengeAwards, err := challengeAwardsForJudge(ctx, competition.ID, viewer)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/judging challenge awards: %s", competition.ID, err)
		http.Error(w, "Unable to load challenge awards", http.StatusInternalServerError)
		return
	}
	orgMap, err := loadHackathonOrgMap(ctx)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/judging orgs: %s", competition.ID, err)
		http.Error(w, "Unable to load sponsors", http.StatusInternalServerError)
		return
	}
	var scorecards []*types.Scorecard
	if viewer.PersonID != "" {
		scorecards, err = getters.ListScorecardsForJudge(ctx, competition.ID, viewer.PersonID)
		if err != nil {
			ctx.Err.Printf("/hackathons/%s/judging scorecards: %s", competition.ID, err)
			http.Error(w, "Unable to load scorecards", http.StatusInternalServerError)
			return
		}
	}
	var awardVotes []*types.AwardVote
	if viewer.PersonID != "" {
		awardVotes, err = getters.ListAwardVotesForJudge(ctx, competition.ID, viewer.PersonID)
		if err != nil {
			ctx.Err.Printf("/hackathons/%s/judging award votes: %s", competition.ID, err)
			http.Error(w, "Unable to load challenge votes", http.StatusInternalServerError)
			return
		}
	}
	awardOptIns, err := challengeAwardOptInMap(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/judging challenge opt-ins: %s", competition.ID, err)
		http.Error(w, "Unable to load challenge opt-ins", http.StatusInternalServerError)
		return
	}
	flash := r.URL.Query().Get("flash")
	if flash == "Rankings saved" {
		flash = ""
	}
	page := &HackathonPage{
		Competition:             competition,
		Conf:                    conf,
		OrgsByID:                orgMap,
		Projects:                projects,
		ChallengeProjects:       challengeProjects,
		JudgeEvents:             currentEvents,
		Scorecards:              scorecards,
		JudgeTypes:              judgeTypes,
		ChallengeAwardsForJudge: challengeAwards,
		AwardVotes:              awardVotes,
		AwardOptIns:             awardOptIns,
		Viewer:                  id,
		CanScoreAll:             canJudge,
		FlashMessage:            flash,
		FlashError:              r.URL.Query().Get("error"),
		Year:                    helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon_judging.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s/judging template: %s", competition.ID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonScorecardSubmit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, events, err := loadHackathonJudgingAccess(w, r, ctx)
	if err != nil {
		return
	}
	dest := hackathonURLForConf(conf) + "/judging"
	viewer := hackathonViewerFromIdentity(id, conf)
	if viewer.PersonID == "" {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Your account needs a person profile before you can score projects."), http.StatusSeeOther)
		return
	}
	in, err := scorecardRankingsInputFromRequest(w, r)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	event := judgeEventByID(events, in.JudgeEventID)
	if event == nil {
		handle404(w, r, ctx)
		return
	}
	if !judgeEventAcceptsScores(event) {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("That judging round is not open for scoring."), http.StatusSeeOther)
		return
	}
	if !viewer.Admin && !viewer.Coordinator && !viewerCanJudgeCompetition(ctx, competition.ID, viewer.PersonID) {
		handle404(w, r, ctx)
		return
	}
	if err := validateScorecardRankings(ctx, competition, viewer, events, event, in.Rankings); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	in.JudgePersonID = viewer.PersonID
	if err := getters.ReplaceScorecardRankings(ctx, in); err != nil {
		ctx.Err.Printf("/hackathons/%s/judging scorecard rankings: %s", competition.ID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"#event-"+url.PathEscape(event.ID), http.StatusSeeOther)
}

func HackathonAwardVoteSubmit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, _, err := loadHackathonJudgingAccess(w, r, ctx)
	if err != nil {
		return
	}
	dest := hackathonURLForConf(conf) + "/judging"
	viewer := hackathonViewerFromIdentity(id, conf)
	if viewer.PersonID == "" {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Your account needs a person profile before you can judge challenge awards."), http.StatusSeeOther)
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	awardID := strings.TrimSpace(r.FormValue("AwardID"))
	projectID := strings.TrimSpace(r.FormValue("ProjectID"))
	if awardID == "" || projectID == "" {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Choose a project for the challenge award."), http.StatusSeeOther)
		return
	}
	if !viewer.Admin && !viewer.Coordinator && !viewerCanJudgeAward(ctx, competition.ID, awardID, viewer.PersonID) {
		handle404(w, r, ctx)
		return
	}
	project, err := getters.GetProjectByID(ctx, projectID)
	if err != nil || project.CompetitionID != competition.ID || !hackathonProjectSubmitted(project) {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Project is not available for challenge judging."), http.StatusSeeOther)
		return
	}
	in := getters.AwardVoteInput{
		AwardID:       awardID,
		JudgePersonID: viewer.PersonID,
		ProjectID:     projectID,
		Notes:         r.FormValue("Notes"),
	}
	if err := getters.UpsertAwardVote(ctx, in); err != nil {
		ctx.Err.Printf("/hackathons/%s/judging award vote: %s", competition.ID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := getters.ReplaceProjectAwardWinner(ctx, awardID, projectID); err != nil {
		ctx.Err.Printf("/hackathons/%s/judging award winner: %s", competition.ID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Challenge winner saved")+"#award-"+url.PathEscape(awardID), http.StatusSeeOther)
}

func HackathonProjectNew(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return
	}
	if id == nil {
		redirectHackathonProfile(w, r, ctx, "Create your profile to start a hackathon project.")
		return
	}
	if !competitionAcceptsProjects(competition) {
		http.Redirect(w, r, hackathonURLForConf(conf)+"?error="+url.QueryEscape("Project submissions are not open."), http.StatusSeeOther)
		return
	}
	if hackathonViewerPersonID(id) == "" {
		redirectHackathonProfile(w, r, ctx, "Create your profile to start a hackathon project.")
		return
	}
	if ok, err := viewerHasConferenceTicket(ctx, conf, id); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/new ticket lookup failed for %s: %s", competition.ID, id.Email, err)
		http.Error(w, "Unable to verify your ticket", http.StatusInternalServerError)
		return
	} else if !ok {
		http.Redirect(w, r, ticketURLForConf(conf), http.StatusSeeOther)
		return
	}
	if project, err := existingProjectForHackathonViewer(ctx, competition, conf, id); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/new existing project lookup failed for %s: %s", competition.ID, id.Email, err)
		http.Error(w, "Unable to verify your project", http.StatusInternalServerError)
		return
	} else if project != nil {
		http.Redirect(w, r, projectEditURLForConf(conf, project), http.StatusSeeOther)
		return
	}
	awards, err := getters.ListAwardsForCompetition(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/new awards: %s", competition.ID, err)
		http.Error(w, "Unable to load award opt-ins", http.StatusInternalServerError)
		return
	}
	page := &HackathonPage{
		Competition:     competition,
		Conf:            conf,
		Project:         &types.HackathonProject{CompetitionID: competition.ID},
		Viewer:          id,
		IsNew:           true,
		IsProjectEditor: true,
		CanEdit:         true,
		CanSubmit:       false,
		OptInAwards:     availableOptInAwards(awards),
		FlashMessage:    r.URL.Query().Get("flash"),
		FlashError:      r.URL.Query().Get("error"),
		Year:            helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon_project.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/new template: %s", competition.ID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonProjectCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return
	}
	base := hackathonURLForConf(conf)
	if id == nil {
		redirectHackathonProfile(w, r, ctx, "Create your profile to start a hackathon project.")
		return
	}
	if !competitionAcceptsProjects(competition) {
		http.Redirect(w, r, base+"?error="+url.QueryEscape("Project submissions are not open."), http.StatusSeeOther)
		return
	}
	personID := hackathonViewerPersonID(id)
	if personID == "" {
		redirectHackathonProfile(w, r, ctx, "Create your profile to start a hackathon project.")
		return
	}
	if ok, err := viewerHasConferenceTicket(ctx, conf, id); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects create ticket lookup failed for %s: %s", competition.ID, id.Email, err)
		http.Error(w, "Unable to verify your ticket", http.StatusInternalServerError)
		return
	} else if !ok {
		http.Redirect(w, r, ticketURLForConf(conf), http.StatusSeeOther)
		return
	}
	if project, err := existingProjectForHackathonViewer(ctx, competition, conf, id); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects create existing project lookup failed for %s: %s", competition.ID, id.Email, err)
		http.Error(w, "Unable to verify your project", http.StatusInternalServerError)
		return
	} else if project != nil {
		http.Redirect(w, r, projectEditURLForConf(conf, project), http.StatusSeeOther)
		return
	}
	in, err := projectInputFromRequest(ctx, w, r, competition.ID)
	if err != nil {
		http.Redirect(w, r, base+"/projects/new?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	in.CreatedByPersonID = personID
	in.Slug, err = generatedProjectSlug()
	if err != nil {
		ctx.Err.Printf("/hackathons/%s create project slug: %s", competition.ID, err)
		http.Redirect(w, r, base+"/projects/new?error="+url.QueryEscape("Unable to create project ID"), http.StatusSeeOther)
		return
	}
	projectID, err := getters.CreateProjectWithAwardOptIns(ctx, in, r.Form["AwardID"])
	if err != nil {
		ctx.Err.Printf("/hackathons/%s create project: %s", competition.ID, err)
		http.Redirect(w, r, base+"/projects/new?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, base+"/projects/"+url.PathEscape(projectID)+"/edit?flash="+url.QueryEscape("Project created"), http.StatusSeeOther)
}

func HackathonProjectShow(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, project, canManage, err := loadViewableHackathonProject(w, r, ctx)
	if err != nil {
		return
	}
	renderHackathonProjectPage(w, r, ctx, competition, conf, id, project, canManage, false, false, false)
}

func HackathonProjectEdit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, project, err := loadEditableHackathonProject(w, r, ctx)
	if err != nil {
		return
	}
	canSubmit := projectEditableByDeadline(competition) && project.Status != getters.ProjectStatusSubmitted
	renderHackathonProjectPage(w, r, ctx, competition, conf, id, project, true, true, true, canSubmit)
}

func renderHackathonProjectPage(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, competition *types.HackathonCompetition, conf *types.Conf, id *auth.Identity, project *types.HackathonProject, canManage, isProjectEditor, canEdit, canSubmit bool) {
	members, err := getters.ListProjectMembers(ctx, project.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s members: %s", competition.ID, project.ID, err)
		http.Error(w, "Unable to load project members", http.StatusInternalServerError)
		return
	}
	optInAwards, awardOptIns, err := loadProjectAwardOptInState(ctx, competition.ID, project.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s award opt-ins: %s", competition.ID, project.ID, err)
		http.Error(w, "Unable to load project award opt-ins", http.StatusInternalServerError)
		return
	}
	awards, prizesByAward, prizePoolByAward, awardeesByAward, err := loadPublicHackathonAwards(ctx, competition.ID, competition.ResultsFinalizedAt != nil)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s awards: %s", competition.ID, project.ID, err)
		http.Error(w, "Unable to load project awards", http.StatusInternalServerError)
		return
	}
	orgMap, err := loadHackathonOrgMap(ctx)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s orgs: %s", competition.ID, project.ID, err)
		http.Error(w, "Unable to load sponsors", http.StatusInternalServerError)
		return
	}
	inviteLink := strings.TrimSpace(r.URL.Query().Get("invite"))
	inviteQRCodeURI := ""
	if inviteLink != "" {
		if uri, err := qrCodeDataURI(inviteLink, 192); err != nil {
			ctx.Err.Printf("/hackathons/%s/projects/%s invite qr: %s", competition.ID, project.ID, err)
		} else {
			inviteQRCodeURI = uri
		}
	}
	viewer := hackathonViewerForCompetition(ctx, id, conf, competition.ID)
	coordinator := viewer.Admin || viewer.Coordinator
	canRemoveProjectMembers := isProjectEditor && canEdit && (coordinator || (project.Status == getters.ProjectStatusCreated && viewerIsProjectOwner(members, id)))

	page := &HackathonPage{
		Competition:             competition,
		Conf:                    conf,
		OrgsByID:                orgMap,
		Project:                 project,
		Members:                 members,
		Awards:                  awards,
		PrizesByAward:           prizesByAward,
		PrizePoolByAward:        prizePoolByAward,
		AwardeesByAward:         awardeesByAward,
		OptInAwards:             optInAwards,
		AwardOptIns:             awardOptIns,
		Viewer:                  id,
		OwnedProjects:           map[string]bool{project.ID: canManage},
		IsProjectEditor:         isProjectEditor,
		CanEdit:                 canEdit,
		CanSubmit:               canSubmit,
		CanRemoveProjectMembers: canRemoveProjectMembers,
		InviteLink:              inviteLink,
		InviteQRCodeURI:         inviteQRCodeURI,
		FlashMessage:            r.URL.Query().Get("flash"),
		FlashError:              r.URL.Query().Get("error"),
		Year:                    helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon_project.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s template: %s", competition.ID, project.ID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonProjectUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, _, project, err := loadEditableHackathonProject(w, r, ctx)
	if err != nil {
		return
	}
	dest := hackathonURLForConf(conf) + "/projects/" + url.PathEscape(project.ID) + "/edit"
	in, err := projectInputFromRequest(ctx, w, r, competition.ID)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	in.Slug = project.Slug
	if err := getters.UpdateProject(ctx, project.ID, in); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s update: %s", competition.ID, project.ID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Project saved"), http.StatusSeeOther)
}

func HackathonProjectSubmit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, _, project, err := loadEditableHackathonProject(w, r, ctx)
	if err != nil {
		return
	}
	dest := hackathonURLForConf(conf) + "/projects/" + url.PathEscape(project.ID) + "/edit"
	if !projectEditableByDeadline(competition) {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Project submissions are closed.")+"#submission", http.StatusSeeOther)
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form")+"#submission", http.StatusSeeOther)
		return
	}
	if err := getters.SetProjectAwardOptIns(ctx, project.ID, r.Form["AwardID"]); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s award opt-ins: %s", competition.ID, project.ID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error())+"#submission", http.StatusSeeOther)
		return
	}
	if err := getters.SubmitProject(ctx, project.ID); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s submit: %s", competition.ID, project.ID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error())+"#submission", http.StatusSeeOther)
		return
	}
	if project.SubmittedAt == nil {
		members, membersErr := getters.ListProjectMembers(ctx, project.ID)
		if membersErr != nil {
			ctx.Err.Printf("/hackathons/%s/projects/%s submission email members: %s", competition.ID, project.ID, membersErr)
		} else {
			projectURL := absoluteURL(r, hackathonURLForConf(conf)+"/projects/"+url.PathEscape(project.ID))
			for _, sendErr := range emails.SendProjectSubmissionConfirmations(ctx, conf, competition, project, members, projectURL) {
				ctx.Err.Printf("/hackathons/%s/projects/%s submission confirmation: %s", competition.ID, project.ID, sendErr)
			}
		}
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Project submitted")+"#submission", http.StatusSeeOther)
}

func loadProjectAwardOptInState(ctx *config.AppContext, competitionID, projectID string) ([]*types.Award, map[string]bool, error) {
	awards, err := getters.ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		return nil, nil, err
	}
	optInAwards := availableOptInAwards(awards)
	optIns, err := getters.ListProjectAwardOptInsForProject(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	awardOptIns := make(map[string]bool, len(optIns))
	for _, optIn := range optIns {
		if optIn != nil {
			awardOptIns[optIn.AwardID] = true
		}
	}
	return optInAwards, awardOptIns, nil
}

func availableOptInAwards(awards []*types.Award) []*types.Award {
	var out []*types.Award
	for _, award := range awards {
		if award == nil || !award.OptInRequired || award.Status == getters.AwardStatusDraft {
			continue
		}
		out = append(out, award)
	}
	return out
}

func HackathonProjectInviteCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, _, project, err := loadEditableHackathonProject(w, r, ctx)
	if err != nil {
		return
	}
	projectURL := hackathonURLForConf(conf) + "/projects/" + url.PathEscape(project.ID)
	dest := projectURL
	fragment := ""
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	switch returnTo := strings.TrimSpace(r.FormValue("ReturnTo")); returnTo {
	case projectURL + "/edit":
		dest = returnTo
	case projectURL + "/edit#team":
		dest = projectURL + "/edit"
		fragment = "#team"
	}
	token, _, err := getters.CreateProjectInvite(ctx, project.ID, "", nil)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s invite: %s", competition.ID, project.ID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error())+fragment, http.StatusSeeOther)
		return
	}
	inviteURL := absoluteURL(r, "/hackathons/invites/"+url.PathEscape(token))
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Invite link created")+"&invite="+url.QueryEscape(inviteURL)+fragment, http.StatusSeeOther)
}

func HackathonProjectMemberRemove(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return
	}
	if id == nil {
		redirectHackathonLogin(w, r)
		return
	}
	project, err := getters.GetProjectByID(ctx, mux.Vars(r)["projectID"])
	if err != nil || project == nil || project.CompetitionID != competition.ID {
		handle404(w, r, ctx)
		return
	}
	dest := hackathonURLForConf(conf) + "/projects/" + url.PathEscape(project.ID) + "/edit"
	fragment := "#team"
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form")+fragment, http.StatusSeeOther)
		return
	}
	personID := strings.TrimSpace(r.FormValue("PersonID"))
	if personID == "" {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Member is required.")+fragment, http.StatusSeeOther)
		return
	}
	members, err := getters.ListProjectMembers(ctx, project.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s remove member list: %s", competition.ID, project.ID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unable to load project members.")+fragment, http.StatusSeeOther)
		return
	}
	viewer := hackathonViewerFromIdentity(id, conf)
	coordinatorRemoval := viewer.Admin || viewer.Coordinator
	if !coordinatorRemoval && !viewerIsProjectOwner(members, id) {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Only project owners or coordinators can remove team members.")+fragment, http.StatusSeeOther)
		return
	}
	if !coordinatorRemoval && project.Status != getters.ProjectStatusCreated {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Team members cannot be removed after submission. Ask a hackathon coordinator for help.")+fragment, http.StatusSeeOther)
		return
	}
	target := projectMemberByPersonID(members, personID)
	if target == nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Project member not found.")+fragment, http.StatusSeeOther)
		return
	}
	if target.Role == getters.ProjectMemberRoleOwner {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Project owners cannot be removed from this page.")+fragment, http.StatusSeeOther)
		return
	}
	if err := getters.RemoveProjectMember(ctx, project.ID, personID, coordinatorRemoval); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s remove member %s: %s", competition.ID, project.ID, personID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error())+fragment, http.StatusSeeOther)
		return
	}
	if returnTo := strings.TrimSpace(r.FormValue("ReturnTo")); returnTo == "admin-projects" && coordinatorRemoval {
		dest = hackathonAdminRequestURL(r, competition.ID, "/projects")
		fragment = ""
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Project member removed")+fragment, http.StatusSeeOther)
}

func HackathonProjectInviteAccept(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email := strings.TrimSpace(ctx.Session.GetString(r.Context(), auth.SessionEmailKey))
	if email == "" {
		redirectHackathonLogin(w, r)
		return
	}
	personID, err := getters.GetPersonIDByEmail(ctx, email)
	if err != nil {
		ctx.Err.Printf("/hackathons/invites person %s: %s", email, err)
		http.Redirect(w, r, "/dashboard?error="+url.QueryEscape("Your account needs a person profile before you can join a project."), http.StatusSeeOther)
		return
	}
	token := mux.Vars(r)["token"]
	invite, err := getters.GetProjectInviteByToken(ctx, token)
	if err != nil {
		ctx.Err.Printf("/hackathons/invites load: %s", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	project, err := getters.GetProjectByID(ctx, invite.ProjectID)
	if err != nil {
		ctx.Err.Printf("/hackathons/invites project %s: %s", invite.ProjectID, err)
		http.Error(w, "Unable to load project", http.StatusInternalServerError)
		return
	}
	competition, err := getters.GetCompetitionByID(ctx, project.CompetitionID)
	if err != nil {
		ctx.Err.Printf("/hackathons/invites competition %s: %s", project.CompetitionID, err)
		http.Error(w, "Unable to load hackathon", http.StatusInternalServerError)
		return
	}
	conf, err := getters.GetConfByRef(ctx, competition.ConferenceID)
	if err != nil {
		ctx.Err.Printf("/hackathons/invites conf %s: %s", competition.ConferenceID, err)
		http.Error(w, "Unable to load conference", http.StatusInternalServerError)
		return
	}
	hasTicket, err := emailHasConferenceTicket(ctx, conf, email)
	if err != nil {
		ctx.Err.Printf("/hackathons/invites ticket check %s: %s", email, err)
		http.Error(w, "Unable to verify conference ticket", http.StatusInternalServerError)
		return
	}
	if !hasTicket {
		dest := "/" + url.PathEscape(conf.Tag) + "?error=" + url.QueryEscape("Project team members need a conference ticket before joining this hackathon project.") + "#tickets"
		http.Redirect(w, r, dest, http.StatusSeeOther)
		return
	}
	if _, err = getters.AcceptProjectInvite(ctx, token, personID); err != nil {
		ctx.Err.Printf("/hackathons/invites accept: %s", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, hackathonURLForConf(conf)+"/projects/"+url.PathEscape(project.ID)+"?flash="+url.QueryEscape("Joined project"), http.StatusSeeOther)
}

func HackathonJudgeInviteAccept(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email := strings.TrimSpace(ctx.Session.GetString(r.Context(), auth.SessionEmailKey))
	if email == "" {
		redirectHackathonLogin(w, r)
		return
	}
	personID, err := getters.GetPersonIDByEmail(ctx, email)
	if err != nil {
		ctx.Err.Printf("/hackathons/judge-invites person %s: %s", email, err)
		encHMAC := base64.RawURLEncoding.EncodeToString([]byte(helpers.CreateEmailHMAC(ctx, email)))
		encEmail := base64.RawURLEncoding.EncodeToString([]byte(email))
		profileURL := dashboardSpeakerEditURLWithFlash(encHMAC, encEmail, r.URL.RequestURI(), "Create your profile to accept this judge invite.")
		http.Redirect(w, r, profileURL, http.StatusSeeOther)
		return
	}
	invite, err := getters.AcceptCompetitionJudgeInvite(ctx, mux.Vars(r)["token"], personID)
	if err != nil {
		ctx.Err.Printf("/hackathons/judge-invites accept: %s", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	competition, err := getters.GetCompetitionByID(ctx, invite.CompetitionID)
	if err != nil {
		ctx.Err.Printf("/hackathons/judge-invites competition %s: %s", invite.CompetitionID, err)
		http.Error(w, "Unable to load hackathon", http.StatusInternalServerError)
		return
	}
	conf, err := getters.GetConfByRef(ctx, competition.ConferenceID)
	if err != nil {
		ctx.Err.Printf("/hackathons/judge-invites conf %s: %s", competition.ConferenceID, err)
		http.Error(w, "Unable to load conference", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, hackathonURLForConf(conf)+"/judging?flash="+url.QueryEscape("Judge access enabled"), http.StatusSeeOther)
}

func loadPublicHackathonAwards(ctx *config.AppContext, competitionID string, publishWinners bool) ([]*types.Award, map[string][]*types.Prize, map[string][]*types.Prize, map[string][]*types.ProjectAward, error) {
	awards, err := getters.ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	prizes, err := getters.ListPrizesForCompetition(ctx, competitionID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	projectAwards, err := getters.ListProjectAwardsForCompetition(ctx, competitionID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	awardeesByAward := make(map[string][]*types.ProjectAward)
	for _, projectAward := range projectAwards {
		if projectAward != nil {
			awardeesByAward[projectAward.AwardID] = append(awardeesByAward[projectAward.AwardID], projectAward)
		}
	}
	activeAwardIDs := map[string]bool{}
	publicAwards := make([]*types.Award, 0, len(awards))
	for _, award := range awards {
		if award == nil {
			continue
		}
		switch award.Status {
		case getters.AwardStatusAvailable, getters.AwardStatusAwarded:
			activeAwardIDs[award.ID] = true
			publicAwards = append(publicAwards, award)
		default:
			continue
		}
	}
	prizesByAward := make(map[string][]*types.Prize)
	prizePoolByAward := make(map[string][]*types.Prize)
	for _, prize := range prizes {
		if prize == nil {
			continue
		}
		if activeAwardIDs[prize.AwardID] {
			prizesByAward[prize.AwardID] = append(prizesByAward[prize.AwardID], prize)
			prizePoolByAward[prize.AwardID] = append(prizePoolByAward[prize.AwardID], prize)
		}
	}
	sortPublicHackathonAwards(publicAwards, prizesByAward)
	publicAwardeesByAward := make(map[string][]*types.ProjectAward)
	if publishWinners {
		for awardID := range activeAwardIDs {
			publicAwardeesByAward[awardID] = awardeesByAward[awardID]
		}
	}
	return publicAwards, prizesByAward, prizePoolByAward, publicAwardeesByAward, nil
}

func sortPublicHackathonAwards(awards []*types.Award, prizesByAward map[string][]*types.Prize) {
	awardValue := func(award *types.Award) int64 {
		if award == nil {
			return 0
		}
		var total int64
		for _, prize := range prizesByAward[award.ID] {
			total += prizeValueSats(prize)
		}
		return total
	}
	sort.SliceStable(awards, func(i, j int) bool {
		a, b := awards[i], awards[j]
		if a == nil || b == nil {
			return b == nil && a != nil
		}
		if a.FinalistsOnly != b.FinalistsOnly {
			return a.FinalistsOnly
		}
		aValue, bValue := awardValue(a), awardValue(b)
		if aValue != bValue {
			return aValue > bValue
		}
		aTitle, bTitle := strings.ToLower(strings.TrimSpace(a.Title)), strings.ToLower(strings.TrimSpace(b.Title))
		if aTitle != bTitle {
			return aTitle < bTitle
		}
		return a.ID < b.ID
	})
}

func loadHackathonOrgMap(ctx *config.AppContext) (map[string]*types.Org, error) {
	orgs, err := getters.ListOrgs(ctx)
	if err != nil {
		return nil, err
	}
	return orgsByID(orgs), nil
}

func loadConfHackathonPlaceRows(ctx *config.AppContext, competitionID string, publishWinners bool, orgsByID map[string]*types.Org) ([]*HackathonPlaceRow, error) {
	awards, err := getters.ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	prizes, err := getters.ListPrizesForCompetition(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	var projectAwards []*types.ProjectAward
	if publishWinners {
		projectAwards, err = getters.ListProjectAwardsForCompetition(ctx, competitionID)
		if err != nil {
			return nil, err
		}
	}
	prizesByAward := make(map[string][]*types.Prize)
	for _, prize := range prizes {
		if prize != nil {
			prizesByAward[prize.AwardID] = append(prizesByAward[prize.AwardID], prize)
		}
	}
	awardeesByAward := make(map[string][]*types.ProjectAward)
	for _, awardee := range projectAwards {
		if awardee != nil {
			awardeesByAward[awardee.AwardID] = append(awardeesByAward[awardee.AwardID], awardee)
		}
	}
	rowsByRank := make(map[int]*HackathonPlaceRow)
	for _, award := range awards {
		if award == nil || !hackathonPlaceAwardStatusVisible(award.Status) {
			continue
		}
		rank := hackathonPlaceAwardRank(award)
		if rank < 1 || rank > 3 || rowsByRank[rank] != nil {
			continue
		}
		row := &HackathonPlaceRow{
			PlaceLabel:      hackathonPlaceLabel(rank),
			PlaceName:       hackathonPlaceName(rank),
			ProjectTitle:    strings.TrimSpace(award.Title),
			Amount:          hackathonPlacePrizeAmount(prizesByAward[award.ID]),
			Detail:          hackathonPlaceDetail(award, prizesByAward[award.ID], false),
			ExtraPrizeNames: nonCashPrizeNames(prizesByAward[award.ID]),
			GrandPrize:      rank == 1,
		}
		if org := orgsByID[strings.TrimSpace(award.SponsoredByOrgID)]; org != nil {
			row.SponsorLabel = strings.TrimSpace(org.Name)
			row.SponsorURL = strings.TrimSpace(org.Website)
			row.SponsorLogoURL = orgLogoURL(org)
			row.SponsorLogoAlt = orgLogoAlt(org)
		}
		if awardees := awardeesByAward[award.ID]; len(awardees) > 0 && awardees[0] != nil {
			row.ProjectID = awardees[0].ProjectID
			row.ProjectTitle = strings.TrimSpace(awardees[0].ProjectTitle)
			row.Detail = hackathonPlaceDetail(award, prizesByAward[award.ID], true)
		}
		if row.ProjectTitle == "" {
			row.ProjectTitle = hackathonPlaceLabel(rank)
		}
		rowsByRank[rank] = row
	}
	rows := make([]*HackathonPlaceRow, 0, 3)
	for rank := 1; rank <= 3; rank++ {
		if row := rowsByRank[rank]; row != nil {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func orgLogoURL(org *types.Org) string {
	if org == nil {
		return ""
	}
	if logo := strings.TrimSpace(org.LogoLight); logo != "" {
		return logo
	}
	return strings.TrimSpace(org.LogoDark)
}

func orgLogoAlt(org *types.Org) string {
	if org != nil && strings.TrimSpace(org.Name) != "" {
		return strings.TrimSpace(org.Name)
	}
	return "Sponsor"
}

func hackathonPlaceAwardStatusVisible(status string) bool {
	switch strings.TrimSpace(status) {
	case getters.AwardStatusAvailable, getters.AwardStatusAwarded:
		return true
	default:
		return false
	}
}

func hackathonPlaceAwardRank(award *types.Award) int {
	if award == nil {
		return 0
	}
	if award.AwardRank != nil {
		return *award.AwardRank
	}
	title := strings.ToLower(strings.TrimSpace(award.Title))
	switch {
	case strings.Contains(title, "1st"), strings.Contains(title, "first"):
		return 1
	case strings.Contains(title, "2nd"), strings.Contains(title, "second"):
		return 2
	case strings.Contains(title, "3rd"), strings.Contains(title, "third"):
		return 3
	default:
		return 0
	}
}

func hackathonPlaceLabel(rank int) string {
	switch rank {
	case 1:
		return "★ 1ST"
	case 2:
		return "★ 2ND"
	case 3:
		return "★ 3RD"
	default:
		return "★"
	}
}

func hackathonPlaceName(rank int) string {
	switch rank {
	case 1:
		return "1st"
	case 2:
		return "2nd"
	case 3:
		return "3rd"
	default:
		return ""
	}
}

func hackathonPlaceDetail(award *types.Award, prizes []*types.Prize, awarded bool) string {
	value := hackathonPlacePrizeAmount(prizes)
	title := ""
	if award != nil {
		title = strings.TrimSpace(award.Title)
	}
	if awarded && title != "" && value != "" {
		return title + " · " + value
	}
	if awarded && title != "" {
		return title
	}
	if value != "" {
		return value
	}
	if award != nil {
		return strings.TrimSpace(award.Description)
	}
	return ""
}

func hackathonPlacePrizeAmount(prizes []*types.Prize) string {
	if total := cashPrizeValueSatsTotal(prizes); total > 0 {
		return compactSatoshiLabel(total)
	}
	return ""
}

func cashPrizeValueSatsTotal(prizes []*types.Prize) int64 {
	var total int64
	for _, prize := range prizes {
		total += cashPrizeValueSats(prize)
	}
	return total
}

func cashPrizeValueSats(prize *types.Prize) int64 {
	if prize == nil {
		return 0
	}
	switch strings.TrimSpace(prize.PrizeType) {
	case "", getters.PrizeTypeSats:
		return prizeValueSats(prize)
	default:
		return 0
	}
}

func nonCashPrizeNames(prizes []*types.Prize) []string {
	names := make([]string, 0, len(prizes))
	for _, prize := range prizes {
		if prize == nil {
			continue
		}
		if prize.PrizeType == "" || strings.TrimSpace(prize.PrizeType) == getters.PrizeTypeSats {
			continue
		}
		name := strings.TrimSpace(prize.Title)
		if name == "" {
			name = publicNonCashPrizeTypeLabel(prize.PrizeType)
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func publicNonCashPrizeTypeLabel(prizeType string) string {
	switch strings.TrimSpace(prizeType) {
	case getters.PrizeTypeInKind:
		return "In-kind prize"
	case getters.PrizeTypeTickets:
		return "Tickets"
	case getters.PrizeTypePooled:
		return "Prize pool"
	case getters.PrizeTypeTrophy:
		return "Trophy"
	default:
		return strings.TrimSpace(prizeType)
	}
}

func compactSatoshiLabel(sats int64) string {
	value := float64(sats)
	suffix := ""
	switch {
	case sats >= 1_000_000:
		value /= 1_000_000
		suffix = "M"
	case sats >= 1_000:
		value /= 1_000
		suffix = "k"
	}
	formatted := strconv.FormatFloat(value, 'f', 1, 64)
	formatted = strings.TrimRight(strings.TrimRight(formatted, "0"), ".")
	return formatted + suffix + " satoshis"
}

func loadHackathonPageData(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*types.HackathonCompetition, *types.Conf, *auth.Identity, []*types.HackathonProject, error) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	viewer := hackathonViewerFromIdentity(id, conf)
	projects, err := getters.ListProjectsForCompetition(ctx, competition.ID, viewer)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s list projects: %s", competition.ID, err)
		http.Error(w, "Unable to load projects", http.StatusInternalServerError)
		return nil, nil, nil, nil, err
	}
	return competition, conf, id, projects, nil
}

func loadHackathonCompetition(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*types.HackathonCompetition, *types.Conf, *auth.Identity, error) {
	confTag := strings.TrimSpace(mux.Vars(r)["conf"])
	conf, err := getters.GetConfByTag(ctx, confTag)
	if err != nil {
		handle404(w, r, ctx)
		return nil, nil, nil, err
	}
	if conf == nil {
		handle404(w, r, ctx)
		return nil, nil, nil, fmt.Errorf("conference %s not found", confTag)
	}
	competition, err := getters.GetCompetitionByConferenceID(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s/hackathon competition: %s", conf.Tag, err)
		handle404(w, r, ctx)
		return nil, nil, nil, err
	}
	if competition == nil {
		handle404(w, r, ctx)
		return nil, nil, nil, fmt.Errorf("conference %s has no hackathon", conf.Tag)
	}
	id := auth.RequireOptional(r, ctx)
	viewer := hackathonViewerFromIdentity(id, conf)
	if competition.Visibility != getters.CompetitionVisibilityPublic && !viewer.Admin && !viewer.Coordinator {
		handle404(w, r, ctx)
		return nil, nil, nil, fmt.Errorf("hidden competition %s", competition.ID)
	}
	conf = hackathonNavConference(conf, nil, competition.Visibility == getters.CompetitionVisibilityPublic)
	if navTalks, navErr := getters.GetTalksFor(ctx, conf.Tag); navErr != nil {
		ctx.Err.Printf("/%s/hackathon nav talks failed (continuing): %s", conf.Tag, navErr)
	} else {
		conf = hackathonNavConference(conf, navTalks, conf.ShowHackathon)
	}
	return competition, conf, id, nil
}

func loadHackathonJudgingAccess(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*types.HackathonCompetition, *types.Conf, *auth.Identity, []*types.JudgeEvent, error) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if id == nil {
		redirectHackathonLogin(w, r)
		return nil, nil, nil, nil, fmt.Errorf("not logged in")
	}
	events, err := getters.ListJudgeEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/judging events: %s", competition.ID, err)
		http.Error(w, "Unable to load judge events", http.StatusInternalServerError)
		return nil, nil, nil, nil, err
	}
	events = timelineJudgeEvents(events)
	viewer := hackathonViewerFromIdentity(id, conf)
	if !viewer.Admin && !viewer.Coordinator && !viewerCanJudgeCompetition(ctx, competition.ID, viewer.PersonID) && !viewerCanJudgeAnyAward(ctx, competition.ID, viewer.PersonID) {
		handle404(w, r, ctx)
		return nil, nil, nil, nil, fmt.Errorf("viewer cannot judge competition %s", competition.ID)
	}
	return competition, conf, id, events, nil
}

func loadEditableHackathonProject(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*types.HackathonCompetition, *types.Conf, *auth.Identity, *types.HackathonProject, error) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if id == nil {
		redirectHackathonLogin(w, r)
		return nil, nil, nil, nil, fmt.Errorf("not logged in")
	}
	projectID := mux.Vars(r)["projectID"]
	project, err := getters.GetProjectByID(ctx, projectID)
	if err != nil || project.CompetitionID != competition.ID {
		handle404(w, r, ctx)
		if err == nil {
			err = fmt.Errorf("project %s is not in competition %s", projectID, competition.ID)
		}
		return nil, nil, nil, nil, err
	}
	canEdit, reason := viewerCanEditHackathonProject(ctx, competition, conf, id, project.ID)
	if !canEdit {
		http.Redirect(w, r, hackathonURLForConf(conf)+"?error="+url.QueryEscape(reason), http.StatusSeeOther)
		return nil, nil, nil, nil, fmt.Errorf("viewer cannot edit project %s", project.ID)
	}
	return competition, conf, id, project, nil
}

func loadViewableHackathonProject(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*types.HackathonCompetition, *types.Conf, *auth.Identity, *types.HackathonProject, bool, error) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return nil, nil, nil, nil, false, err
	}
	projectID := mux.Vars(r)["projectID"]
	project, err := getters.GetProjectByID(ctx, projectID)
	if err != nil || project.CompetitionID != competition.ID {
		handle404(w, r, ctx)
		if err == nil {
			err = fmt.Errorf("project %s is not in competition %s", projectID, competition.ID)
		}
		return nil, nil, nil, nil, false, err
	}
	viewer := hackathonViewerForCompetition(ctx, id, conf, competition.ID)
	canManage, _ := viewerCanEditHackathonProject(ctx, competition, conf, id, project.ID)
	canView, err := getters.CanViewProject(ctx, project.ID, viewer)
	if err != nil {
		ctx.Err.Printf("/%s/hackathon/projects/%s visibility: %s", conf.Tag, project.ID, err)
		http.Error(w, "Unable to load project", http.StatusInternalServerError)
		return nil, nil, nil, nil, false, err
	}
	if !canView {
		if id == nil {
			redirectHackathonLogin(w, r)
		} else {
			handle404(w, r, ctx)
		}
		return nil, nil, nil, nil, false, fmt.Errorf("viewer cannot view project %s", project.ID)
	}
	return competition, conf, id, project, canManage, nil
}

func projectInputFromRequest(ctx *config.AppContext, w http.ResponseWriter, r *http.Request, competitionID string) (getters.ProjectInput, error) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadFileBytes+maxFormBodyBytes)
		if err := r.ParseMultipartForm(maxUploadFileBytes); err != nil {
			return getters.ProjectInput{}, fmt.Errorf("bad form")
		}
	} else {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			return getters.ProjectInput{}, fmt.Errorf("bad form")
		}
	}
	in := getters.ProjectInput{
		CompetitionID:     competitionID,
		Title:             strings.TrimSpace(r.FormValue("Title")),
		ShortDescription:  strings.TrimSpace(r.FormValue("ShortDescription")),
		Description:       strings.TrimSpace(r.FormValue("Description")),
		DescriptionFormat: strings.TrimSpace(r.FormValue("DescriptionFormat")),
		ImageURL:          strings.TrimSpace(r.FormValue("ImageURL")),
		ImageURLs:         projectImageURLsFromForm(r),
		GitHubURL:         strings.TrimSpace(r.FormValue("GitHubURL")),
		DemoURL:           strings.TrimSpace(r.FormValue("DemoURL")),
		VideoURL:          strings.TrimSpace(r.FormValue("VideoURL")),
		SlidesURL:         strings.TrimSpace(r.FormValue("SlidesURL")),
		DocsURL:           strings.TrimSpace(r.FormValue("DocsURL")),
		Tags:              splitProjectTags(r.FormValue("Tags")),
	}
	if in.Title == "" {
		return getters.ProjectInput{}, fmt.Errorf("project title is required")
	}
	imageURLs, err := uploadedProjectImageURLs(ctx, r, in.ImageURLs)
	if err != nil {
		return getters.ProjectInput{}, err
	}
	in.ImageURLs = imageURLs
	if len(in.ImageURLs) > 0 {
		in.ImageURL = in.ImageURLs[0]
	} else {
		in.ImageURL = ""
	}
	return in, nil
}

func projectImageURLsFromForm(r *http.Request) []string {
	values := append([]string{r.FormValue("ImageURL")}, r.Form["ImageURLs"]...)
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func uploadedProjectImageURLs(ctx *config.AppContext, r *http.Request, current []string) ([]string, error) {
	imageURLs := append([]string{}, current...)
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		return imageURLs, nil
	}
	files := r.MultipartForm.File["ImageFiles"]
	if len(files) == 0 {
		return imageURLs, nil
	}
	for _, header := range files {
		raw, contentType, ext, err := readProjectImageFileHeader(header)
		if err != nil {
			if errors.Is(err, errUploadTooLarge) {
				return nil, fmt.Errorf("project image is too large")
			}
			return nil, fmt.Errorf("project image upload failed: %w", err)
		}
		url, err := getters.UploadFile(ctx, contentType, "project-image"+ext, raw)
		if err != nil {
			return nil, fmt.Errorf("project image upload failed: %w", err)
		}
		imageURLs = append(imageURLs, url)
	}
	return imageURLs, nil
}

func readProjectImageFileHeader(header *multipart.FileHeader) ([]byte, string, string, error) {
	if header == nil {
		return nil, "", "", http.ErrMissingFile
	}
	file, err := header.Open()
	if err != nil {
		return nil, "", "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxUploadFileBytes+1))
	if err != nil {
		return nil, "", "", err
	}
	if int64(len(raw)) > maxUploadFileBytes {
		return nil, "", "", errUploadTooLarge
	}
	if len(raw) == 0 {
		return nil, "", "", errors.New("empty upload")
	}
	contentType := detectedImageContentType(raw, header.Filename, false)
	if contentType == "" {
		return nil, "", "", errors.New("unsupported image type")
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" || contentTypeFromFilename(header.Filename) != contentType {
		ext = extForImageContentType(contentType)
	}
	return raw, contentType, ext, nil
}

func generatedProjectSlug() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "project-" + hex.EncodeToString(b[:]), nil
}

func scorecardRankingsInputFromRequest(w http.ResponseWriter, r *http.Request) (getters.ScorecardRankingsInput, error) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		return getters.ScorecardRankingsInput{}, fmt.Errorf("bad form")
	}
	in := getters.ScorecardRankingsInput{
		JudgeEventID: strings.TrimSpace(r.FormValue("JudgeEventID")),
	}
	if in.JudgeEventID == "" {
		return getters.ScorecardRankingsInput{}, fmt.Errorf("judge event is required")
	}
	projectIDs := r.PostForm["ProjectID"]
	for _, projectID := range projectIDs {
		projectID = strings.TrimSpace(projectID)
		if projectID == "" {
			continue
		}
		rawRank := strings.TrimSpace(r.FormValue("Rank_" + projectID))
		if rawRank == "" {
			continue
		}
		rank, err := strconv.Atoi(rawRank)
		if err != nil || rank <= 0 {
			return getters.ScorecardRankingsInput{}, fmt.Errorf("rank must be a positive number")
		}
		in.Rankings = append(in.Rankings, getters.ScorecardRankingInput{
			ProjectID: projectID,
			Rank:      rank,
		})
	}
	return in, nil
}

func validateScorecardRankings(ctx *config.AppContext, competition *types.HackathonCompetition, viewer types.HackathonViewer, events []*types.JudgeEvent, event *types.JudgeEvent, rankings []getters.ScorecardRankingInput) error {
	seenRanks := map[int]bool{}
	seenProjects := map[string]bool{}
	limit := judgeEventRankLimit(event)
	for _, ranking := range rankings {
		projectID := strings.TrimSpace(ranking.ProjectID)
		if projectID == "" {
			continue
		}
		if ranking.Rank > limit {
			return fmt.Errorf("rank must be between 1 and %d", limit)
		}
		if seenRanks[ranking.Rank] {
			return fmt.Errorf("each rank can only be used once")
		}
		if seenProjects[projectID] {
			return fmt.Errorf("each project can only be ranked once")
		}
		seenRanks[ranking.Rank] = true
		seenProjects[projectID] = true
		project, err := getters.GetProjectByID(ctx, projectID)
		if err != nil || competition == nil || project.CompetitionID != competition.ID {
			return fmt.Errorf("project is invalid")
		}
		if !projectEligibleForJudgeEvent(project, events, event) {
			return fmt.Errorf("project is not eligible for this judging round")
		}
		ok, err := getters.CanViewProject(ctx, project.ID, viewer)
		if err != nil {
			return fmt.Errorf("unable to score project")
		}
		if !ok {
			return fmt.Errorf("project is not visible")
		}
	}
	return nil
}

func optionalIntFromForm(r *http.Request, field, label string, min, max int) (*int, error) {
	raw := strings.TrimSpace(r.FormValue(field))
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be a number", label)
	}
	if n < min {
		return nil, fmt.Errorf("%s must be at least %d", label, min)
	}
	if max > 0 && n > max {
		return nil, fmt.Errorf("%s must be at most %d", label, max)
	}
	return &n, nil
}

func splitProjectTags(raw string) []string {
	parts := strings.Split(raw, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			tags = append(tags, part)
		}
	}
	return tags
}

func hackathonViewerFromIdentity(id *auth.Identity, conf *types.Conf) types.HackathonViewer {
	if id == nil {
		return types.HackathonViewer{}
	}
	viewer := types.HackathonViewer{
		PersonID: hackathonViewerPersonID(id),
		Admin:    id.IsGlobalAdmin(),
	}
	if conf != nil {
		viewer.Admin = viewer.Admin || id.HasRoleForConf(conf.Tag, auth.RoleAdmin)
		viewer.Coordinator = id.HasRoleForConf(conf.Tag, auth.RoleVolcoord)
	}
	return viewer
}

func hackathonViewerForCompetition(ctx *config.AppContext, id *auth.Identity, conf *types.Conf, competitionID string) types.HackathonViewer {
	viewer := hackathonViewerFromIdentity(id, conf)
	if !viewer.Coordinator && hackathonIdentityIsCoordinator(ctx, competitionID, id) {
		viewer.Coordinator = true
	}
	return viewer
}

func hackathonViewerPersonID(id *auth.Identity) string {
	if id == nil || id.Speaker == nil {
		return ""
	}
	return id.Speaker.ID
}

func viewerHasConferenceTicket(ctx *config.AppContext, conf *types.Conf, id *auth.Identity) (bool, error) {
	if ctx == nil || conf == nil || id == nil {
		return false, nil
	}
	email := strings.TrimSpace(id.Email)
	if email == "" && id.Speaker != nil {
		email = strings.TrimSpace(id.Speaker.Email)
	}
	return emailHasConferenceTicket(ctx, conf, email)
}

func emailHasConferenceTicket(ctx *config.AppContext, conf *types.Conf, email string) (bool, error) {
	if ctx == nil || conf == nil {
		return false, nil
	}
	email = strings.TrimSpace(email)
	if email == "" || strings.TrimSpace(conf.Ref) == "" {
		return false, nil
	}
	registrations, err := getters.ListRegistrationsByEmail(ctx, email)
	if err != nil {
		return false, err
	}
	for _, registration := range registrations {
		if registrationCountsForConferenceTicket(registration, conf) {
			return true, nil
		}
	}
	return false, nil
}

func registrationCountsForConferenceTicket(registration *types.Registration, conf *types.Conf) bool {
	return registration != nil && conf != nil && !registration.Revoked && registration.ConfRef == conf.Ref
}

func competitionAcceptsProjects(competition *types.HackathonCompetition) bool {
	if competition == nil || competition.Visibility != getters.CompetitionVisibilityPublic {
		return false
	}
	switch competition.LifecycleOverride {
	case getters.CompetitionLifecycleOpen:
		return true
	case getters.CompetitionLifecycleSubmissionsClosed:
		return competition.AllowLateSubmissions
	case getters.CompetitionLifecycleUpcoming, getters.CompetitionLifecycleClosed:
		return false
	}
	if competition.SubmissionsOpenAt == nil {
		return false
	}
	now := time.Now()
	if competition.SubmissionsOpenAt.After(now) {
		return false
	}
	return projectEditableByDeadline(competition)
}

func competitionSubmissionsUpcoming(competition *types.HackathonCompetition) bool {
	if competition == nil || competition.Visibility != getters.CompetitionVisibilityPublic {
		return true
	}
	switch competition.LifecycleOverride {
	case getters.CompetitionLifecycleUpcoming:
		return true
	case getters.CompetitionLifecycleOpen, getters.CompetitionLifecycleSubmissionsClosed, getters.CompetitionLifecycleClosed:
		return false
	}
	return competition.SubmissionsOpenAt == nil || competition.SubmissionsOpenAt.After(time.Now())
}

func projectEditableByDeadline(competition *types.HackathonCompetition) bool {
	if competition == nil {
		return false
	}
	switch competition.LifecycleOverride {
	case getters.CompetitionLifecycleOpen:
		return true
	case getters.CompetitionLifecycleSubmissionsClosed:
		return competition.AllowLateSubmissions
	case getters.CompetitionLifecycleUpcoming, getters.CompetitionLifecycleClosed:
		return false
	}
	if competition.AllowLateSubmissions {
		return true
	}
	return competition.SubmissionsCloseAt == nil || competition.SubmissionsCloseAt.After(time.Now())
}

func viewerCanManageProject(ctx *config.AppContext, projectID, personID string) bool {
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return false
	}
	members, err := getters.ListProjectMembers(ctx, projectID)
	if err != nil {
		ctx.Err.Printf("list project members %s: %s", projectID, err)
		return false
	}
	for _, member := range members {
		if member != nil && member.PersonID == personID {
			return true
		}
	}
	return false
}

func viewerCanEditHackathonProject(ctx *config.AppContext, competition *types.HackathonCompetition, conf *types.Conf, id *auth.Identity, projectID string) (bool, string) {
	if competition == nil {
		return false, "Hackathon not found."
	}
	viewer := hackathonViewerForCompetition(ctx, id, conf, competition.ID)
	if viewer.Admin || viewer.Coordinator {
		return true, ""
	}
	if !viewerCanManageProject(ctx, projectID, viewer.PersonID) {
		return false, "Only project members or hackathon coordinators can edit that project."
	}
	events, err := getters.ListJudgeEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("list judging events before project edit %s: %s", projectID, err)
		return false, "Unable to verify the judging schedule. Please try again."
	}
	if projectEditingPausedForJudging(events) {
		return false, "Project editing is paused while judging is active. Editing will reopen when the judging period closes."
	}
	return true, ""
}

func projectEditingPausedForJudging(events []*types.JudgeEvent) bool {
	return len(currentJudgeEvents(events)) > 0
}

func viewerIsProjectOwner(members []*types.ProjectMember, id *auth.Identity) bool {
	personID := strings.TrimSpace(hackathonViewerPersonID(id))
	if personID == "" {
		return false
	}
	member := projectMemberByPersonID(members, personID)
	return member != nil && member.Role == getters.ProjectMemberRoleOwner
}

func projectMemberByPersonID(members []*types.ProjectMember, personID string) *types.ProjectMember {
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return nil
	}
	for _, member := range members {
		if member != nil && member.PersonID == personID {
			return member
		}
	}
	return nil
}

func viewerCanJudgeCompetition(ctx *config.AppContext, competitionID, personID string) bool {
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return false
	}
	judges, err := getters.ListCompetitionJudges(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("list competition judges %s: %s", competitionID, err)
		return false
	}
	for _, judge := range judges {
		if judge == nil || judge.PersonID != personID {
			continue
		}
		for _, judgeType := range judge.JudgeTypes {
			if judgeType == getters.JudgeTypeExpo || judgeType == getters.JudgeTypeFinals {
				return true
			}
		}
		return len(judge.JudgeTypes) == 0 && (judge.JudgeType == getters.JudgeTypeExpo || judge.JudgeType == getters.JudgeTypeFinals)
	}
	return false
}

func viewerCanJudgeAnyAward(ctx *config.AppContext, competitionID, personID string) bool {
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return false
	}
	judges, err := getters.ListAwardJudgesForCompetition(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("list award judges %s: %s", competitionID, err)
		return false
	}
	for _, judge := range judges {
		if judge != nil && judge.PersonID == personID {
			return true
		}
	}
	return false
}

func viewerCanJudgeAward(ctx *config.AppContext, competitionID, awardID, personID string) bool {
	personID = strings.TrimSpace(personID)
	awardID = strings.TrimSpace(awardID)
	if personID == "" || awardID == "" {
		return false
	}
	judges, err := getters.ListAwardJudgesForCompetition(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("list award judges %s: %s", competitionID, err)
		return false
	}
	for _, judge := range judges {
		if judge != nil && judge.AwardID == awardID && judge.PersonID == personID {
			return true
		}
	}
	return false
}

func viewerCanJudgeType(ctx *config.AppContext, competitionID, personID, judgeType string) bool {
	personID = strings.TrimSpace(personID)
	judgeType = strings.TrimSpace(judgeType)
	if personID == "" || judgeType == "" {
		return false
	}
	judges, err := getters.ListCompetitionJudges(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("list competition judges %s: %s", competitionID, err)
		return false
	}
	for _, judge := range judges {
		if judge == nil || judge.PersonID != personID {
			continue
		}
		return true
	}
	return false
}

func judgeTypesForPerson(ctx *config.AppContext, competitionID, personID string) map[string]bool {
	personID = strings.TrimSpace(personID)
	out := map[string]bool{}
	if personID == "" {
		return out
	}
	judges, err := getters.ListCompetitionJudges(ctx, competitionID)
	if err != nil {
		ctx.Err.Printf("list competition judges %s: %s", competitionID, err)
		return out
	}
	for _, judge := range judges {
		if judge != nil && judge.PersonID == personID {
			for _, judgeType := range judge.JudgeTypes {
				out[judgeType] = true
			}
			if len(judge.JudgeTypes) == 0 {
				out[judge.JudgeType] = true
			}
		}
	}
	return out
}

func challengeAwardsForJudge(ctx *config.AppContext, competitionID string, viewer types.HackathonViewer) ([]*types.Award, error) {
	awards, err := getters.ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if viewer.Admin || viewer.Coordinator {
		return challengeAwardsOnly(awards), nil
	}
	judges, err := getters.ListAwardJudgesForCompetition(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	assigned := map[string]bool{}
	for _, judge := range judges {
		if judge != nil && judge.PersonID == viewer.PersonID {
			assigned[judge.AwardID] = true
		}
	}
	var out []*types.Award
	for _, award := range awards {
		if award != nil && award.AwardType == getters.AwardTypeChallenge && assigned[award.ID] {
			out = append(out, award)
		}
	}
	return out, nil
}

func challengeAwardsOnly(awards []*types.Award) []*types.Award {
	var out []*types.Award
	for _, award := range awards {
		if award != nil && award.AwardType == getters.AwardTypeChallenge {
			out = append(out, award)
		}
	}
	return out
}

func hackathonSubmittedProjects(projects []*types.HackathonProject) []*types.HackathonProject {
	var out []*types.HackathonProject
	for _, project := range projects {
		if hackathonProjectSubmitted(project) {
			out = append(out, project)
		}
	}
	return out
}

func hackathonProjectSubmitted(project *types.HackathonProject) bool {
	if project == nil {
		return false
	}
	return project.Status == getters.ProjectStatusSubmitted || project.Status == getters.ProjectStatusAdvanced
}

func challengeAwardOptInMap(ctx *config.AppContext, competitionID string) (map[string]bool, error) {
	optIns, err := getters.ListProjectAwardOptInsForCompetition(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(optIns))
	for _, optIn := range optIns {
		if optIn != nil {
			out[optIn.ProjectID+"|"+optIn.AwardID] = true
		}
	}
	return out, nil
}

func judgeEventByID(events []*types.JudgeEvent, eventID string) *types.JudgeEvent {
	eventID = strings.TrimSpace(eventID)
	for _, event := range events {
		if event != nil && event.ID == eventID {
			return event
		}
	}
	return nil
}

func ownedProjectMap(ctx *config.AppContext, projects []*types.HackathonProject, personID string) map[string]bool {
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return nil
	}
	out := make(map[string]bool)
	for _, project := range projects {
		if project != nil && viewerCanManageProject(ctx, project.ID, personID) {
			out[project.ID] = true
		}
	}
	return out
}

func existingProjectForHackathonViewer(ctx *config.AppContext, competition *types.HackathonCompetition, conf *types.Conf, id *auth.Identity) (*types.HackathonProject, error) {
	personID := hackathonViewerPersonID(id)
	if competition == nil || strings.TrimSpace(competition.ID) == "" || personID == "" {
		return nil, nil
	}
	viewer := hackathonViewerFromIdentity(id, conf)
	projects, err := getters.ListProjectsForCompetition(ctx, competition.ID, viewer)
	if err != nil {
		return nil, err
	}
	membersByProject, err := getters.ListProjectMembersForCompetition(ctx, competition.ID)
	if err != nil {
		return nil, err
	}
	return existingProjectFromMembers(projects, membersByProject, personID), nil
}

func existingProjectFromMembers(projects []*types.HackathonProject, membersByProject map[string][]*types.ProjectMember, personID string) *types.HackathonProject {
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return nil
	}
	for _, project := range projects {
		if project == nil {
			continue
		}
		for _, member := range membersByProject[project.ID] {
			if member != nil && member.PersonID == personID {
				return project
			}
		}
	}
	return nil
}

func absoluteURL(r *http.Request, path string) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	return scheme + "://" + r.Host + path
}

func redirectHackathonLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
}

func redirectHackathonProfile(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, flash string) {
	email := strings.TrimSpace(ctx.Session.GetString(r.Context(), auth.SessionEmailKey))
	if email == "" {
		redirectHackathonLogin(w, r)
		return
	}
	encHMAC := base64.RawURLEncoding.EncodeToString([]byte(helpers.CreateEmailHMAC(ctx, email)))
	encEmail := base64.RawURLEncoding.EncodeToString([]byte(email))
	http.Redirect(w, r, dashboardSpeakerEditURLWithFlash(encHMAC, encEmail, r.URL.RequestURI(), flash), http.StatusSeeOther)
}

func hackathonURLForConf(conf *types.Conf) string {
	if conf == nil || strings.TrimSpace(conf.Tag) == "" {
		return ""
	}
	return "/" + url.PathEscape(conf.Tag) + "/hackathon"
}

func ticketURLForConf(conf *types.Conf) string {
	if conf == nil || strings.TrimSpace(conf.Tag) == "" {
		return "/"
	}
	return "/" + url.PathEscape(conf.Tag) + "#tickets"
}

func hackathonScheduleURLForConf(conf *types.Conf) string {
	base := hackathonURLForConf(conf)
	if base == "" {
		return ""
	}
	return base + "/schedule"
}

func hackathonLifecycleLabel(competition *types.HackathonCompetition) string {
	if competition == nil {
		return ""
	}
	if label := hackathonLifecycleOverrideLabel(competition.LifecycleOverride); label != "" {
		return label
	}
	now := time.Now()
	if competition.SubmissionsOpenAt == nil {
		return "Schedule TBA"
	}
	if competition.SubmissionsOpenAt.After(now) {
		return "Upcoming"
	}
	if competition.SubmissionsCloseAt == nil || competition.SubmissionsCloseAt.After(now) {
		return "Open"
	}
	return "Submissions closed"
}

func hackathonLifecycleOverrideLabel(value string) string {
	switch strings.TrimSpace(value) {
	case getters.CompetitionLifecycleUpcoming:
		return "Upcoming"
	case getters.CompetitionLifecycleOpen:
		return "Submissions open"
	case getters.CompetitionLifecycleSubmissionsClosed:
		return "Submissions closed"
	case getters.CompetitionLifecycleClosed:
		return "Closed"
	default:
		return ""
	}
}

func hackathonStatusLabel(status string) string {
	switch strings.TrimSpace(status) {
	case "created":
		return "Created"
	case "submitted":
		return "Submitted"
	case "hidden":
		return "Hidden"
	case "advanced", "finalist":
		return "Advanced"
	default:
		return strings.TrimSpace(status)
	}
}

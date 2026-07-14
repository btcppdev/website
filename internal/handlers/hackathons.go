package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	stdhtml "html"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
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
	Projects                    []*types.HackathonProject
	Project                     *types.HackathonProject
	Members                     []*types.ProjectMember
	JudgeEvents                 []*types.JudgeEvent
	Scorecards                  []*types.Scorecard
	JudgeTypes                  map[string]bool
	Awards                      []*types.Award
	OptInAwards                 []*types.Award
	AwardOptIns                 map[string]bool
	PrizesByAward               map[string][]*types.Prize
	AwardeesByAward             map[string][]*types.ProjectAward
	ScheduleEventsByCompetition map[string][]HackathonScheduleEvent
	ScheduleEventList           []HackathonScheduleEvent
	Viewer                      *auth.Identity
	OwnedProjects               map[string]bool
	IsNew                       bool
	CanCreate                   bool
	CanEdit                     bool
	CanSubmit                   bool
	CanJudge                    bool
	CanScoreAll                 bool
	InviteLink                  string
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
	fields := []string{competition.Title, competition.Slug}
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
	return "/admin/hackathons/" + url.PathEscape(p.Hackathon.ID)
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
	return viewer.Admin || viewer.Coordinator
}

func (p *HackathonPage) competitionConf(competition *types.HackathonCompetition) *types.Conf {
	if p == nil {
		return nil
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

func (p *HackathonPage) CanAdminEdit() bool {
	if p == nil || p.Viewer == nil {
		return false
	}
	return hackathonViewerFromIdentity(p.Viewer, p.Conf).Admin
}

func (p *HackathonPage) AdminEditURL() string {
	if p == nil || p.Competition == nil {
		return "/admin/hackathons"
	}
	return "/admin/hackathons/" + url.PathEscape(p.Competition.ID)
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

func (p *HackathonPage) ProjectIsFinalist(project *types.HackathonProject) bool {
	return project != nil && project.Status == getters.ProjectStatusFinalist
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
	if p.CanScoreAll {
		return true
	}
	return len(p.JudgeTypes) > 0
}

func (p *HackathonPage) Awardees(award *types.Award) []*types.ProjectAward {
	if p == nil || p.AwardeesByAward == nil || award == nil {
		return nil
	}
	return p.AwardeesByAward[award.ID]
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
	return awards
}

func (p *HackathonPage) AwardPrizes(award *types.Award) []*types.Prize {
	if p == nil || p.PrizesByAward == nil || award == nil {
		return nil
	}
	return p.PrizesByAward[award.ID]
}

func (p *HackathonPage) RankedPrizePoolLabel() string {
	sats := p.RankedPrizePoolSats()
	if sats == 0 {
		return "₿0"
	}
	btc := float64(sats) / 100_000_000
	return "₿" + strings.TrimRight(strings.TrimRight(strconv.FormatFloat(btc, 'f', 8, 64), "0"), ".")
}

func (p *HackathonPage) RankedPrizePoolSats() int64 {
	if p == nil || p.PrizesByAward == nil {
		return 0
	}
	var total int64
	for _, prizes := range p.PrizesByAward {
		for _, prize := range prizes {
			total += bitcoinPrizeSats(prize)
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

func bitcoinPrizeSats(prize *types.Prize) int64 {
	if prize == nil || strings.TrimSpace(prize.PrizeType) != getters.PrizeTypeSats {
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
	_, value := hackathonNextMilestone(p.Competition)
	return value
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
	awards, prizesByAward, awardeesByAward, err := loadPublicHackathonAwards(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s awards: %s", competition.Slug, err)
		http.Error(w, "Unable to load awards", http.StatusInternalServerError)
		return
	}
	scheduleEvents, err := loadHackathonScheduleEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s schedule events: %s", competition.Slug, err)
		http.Error(w, "Unable to load schedule", http.StatusInternalServerError)
		return
	}
	page := &HackathonPage{
		Competition:       competition,
		Conf:              conf,
		Projects:          projects,
		Awards:            awards,
		PrizesByAward:     prizesByAward,
		AwardeesByAward:   awardeesByAward,
		ScheduleEventList: scheduleEvents,
		Viewer:            id,
		OwnedProjects:     ownedProjectMap(ctx, projects, personID),
		CanCreate:         id != nil && competitionAcceptsProjects(competition),
		CanJudge:          viewer.Admin || viewer.Coordinator || viewerCanJudgeCompetition(ctx, competition.ID, personID),
		FlashMessage:      r.URL.Query().Get("flash"),
		FlashError:        r.URL.Query().Get("error"),
		Year:              helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s template: %s", competition.Slug, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonSchedule(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return
	}
	scheduleEvents, err := loadHackathonScheduleEvents(ctx, competition.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/schedule events: %s", competition.Slug, err)
		http.Error(w, "Unable to load schedule", http.StatusInternalServerError)
		return
	}
	page := &HackathonPage{
		Competition:       competition,
		Conf:              conf,
		ScheduleEventList: scheduleEvents,
		Viewer:            id,
		Year:              helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon_schedule.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s/schedule template: %s", competition.Slug, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonJudging(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, events, err := loadHackathonJudgingAccess(w, r, ctx)
	if err != nil {
		return
	}
	viewer := hackathonViewerFromIdentity(id, conf)
	var scorecards []*types.Scorecard
	if viewer.PersonID != "" {
		scorecards, err = getters.ListScorecardsForJudge(ctx, competition.ID, viewer.PersonID)
		if err != nil {
			ctx.Err.Printf("/hackathons/%s/judging scorecards: %s", competition.Slug, err)
			http.Error(w, "Unable to load scorecards", http.StatusInternalServerError)
			return
		}
	}
	judgeTypes := judgeTypesForPerson(ctx, competition.ID, viewer.PersonID)
	canJudge := viewer.Admin || viewer.Coordinator || viewerCanJudgeCompetition(ctx, competition.ID, viewer.PersonID)
	projects, err := getters.ListProjectsForCompetition(ctx, competition.ID, viewer)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/judging list projects: %s", competition.Slug, err)
		http.Error(w, "Unable to load projects", http.StatusInternalServerError)
		return
	}
	page := &HackathonPage{
		Competition:  competition,
		Conf:         conf,
		Projects:     projects,
		JudgeEvents:  events,
		Scorecards:   scorecards,
		JudgeTypes:   judgeTypes,
		Viewer:       id,
		CanScoreAll:  canJudge,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon_judging.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s/judging template: %s", competition.Slug, err)
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
	if !viewer.Admin && !viewer.Coordinator && !viewerCanJudgeCompetition(ctx, competition.ID, viewer.PersonID) {
		handle404(w, r, ctx)
		return
	}
	if err := validateScorecardRankings(ctx, competition, viewer, event, in.Rankings); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	in.JudgePersonID = viewer.PersonID
	if err := getters.ReplaceScorecardRankings(ctx, in); err != nil {
		ctx.Err.Printf("/hackathons/%s/judging scorecard rankings: %s", competition.Slug, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Rankings saved")+"#event-"+url.PathEscape(event.ID), http.StatusSeeOther)
}

func HackathonProjectNew(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return
	}
	if id == nil {
		redirectHackathonLogin(w, r)
		return
	}
	if !competitionAcceptsProjects(competition) {
		http.Redirect(w, r, hackathonURLForConf(conf)+"?error="+url.QueryEscape("Project submissions are not open."), http.StatusSeeOther)
		return
	}
	page := &HackathonPage{
		Competition:  competition,
		Conf:         conf,
		Project:      &types.HackathonProject{CompetitionID: competition.ID},
		Viewer:       id,
		IsNew:        true,
		CanEdit:      true,
		CanSubmit:    false,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon_project.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/new template: %s", competition.Slug, err)
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
		redirectHackathonLogin(w, r)
		return
	}
	if !competitionAcceptsProjects(competition) {
		http.Redirect(w, r, base+"?error="+url.QueryEscape("Project submissions are not open."), http.StatusSeeOther)
		return
	}
	personID := hackathonViewerPersonID(id)
	if personID == "" {
		http.Redirect(w, r, base+"?error="+url.QueryEscape("Your account needs a person profile before you can create a project."), http.StatusSeeOther)
		return
	}
	in, err := projectInputFromRequest(w, r, competition.ID)
	if err != nil {
		http.Redirect(w, r, base+"/projects/new?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	in.CreatedByPersonID = personID
	in.Slug, err = generatedProjectSlug()
	if err != nil {
		ctx.Err.Printf("/hackathons/%s create project slug: %s", competition.Slug, err)
		http.Redirect(w, r, base+"/projects/new?error="+url.QueryEscape("Unable to create project ID"), http.StatusSeeOther)
		return
	}
	projectID, err := getters.CreateProject(ctx, in)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s create project: %s", competition.Slug, err)
		http.Redirect(w, r, base+"/projects/new?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, base+"/projects/"+url.PathEscape(projectID)+"?flash="+url.QueryEscape("Project created"), http.StatusSeeOther)
}

func HackathonProjectEdit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, id, project, err := loadEditableHackathonProject(w, r, ctx)
	if err != nil {
		return
	}
	canEdit := id != nil && projectEditableByDeadline(competition)
	canSubmit := canEdit && project.Status != getters.ProjectStatusSubmitted
	members, err := getters.ListProjectMembers(ctx, project.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s members: %s", competition.Slug, project.ID, err)
		http.Error(w, "Unable to load project members", http.StatusInternalServerError)
		return
	}
	optInAwards, awardOptIns, err := loadProjectAwardOptInState(ctx, competition.ID, project.ID)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s award opt-ins: %s", competition.Slug, project.ID, err)
		http.Error(w, "Unable to load project award opt-ins", http.StatusInternalServerError)
		return
	}
	page := &HackathonPage{
		Competition:  competition,
		Conf:         conf,
		Project:      project,
		Members:      members,
		OptInAwards:  optInAwards,
		AwardOptIns:  awardOptIns,
		Viewer:       id,
		CanEdit:      canEdit,
		CanSubmit:    canSubmit,
		InviteLink:   strings.TrimSpace(r.URL.Query().Get("invite")),
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "hackathon_project.tmpl", page); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s template: %s", competition.Slug, project.ID, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func HackathonProjectUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	competition, conf, _, project, err := loadEditableHackathonProject(w, r, ctx)
	if err != nil {
		return
	}
	dest := hackathonURLForConf(conf) + "/projects/" + url.PathEscape(project.ID)
	if !projectEditableByDeadline(competition) {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Project edits are closed."), http.StatusSeeOther)
		return
	}
	in, err := projectInputFromRequest(w, r, competition.ID)
	if err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	in.Slug = project.Slug
	if err := getters.UpdateProject(ctx, project.ID, in); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s update: %s", competition.Slug, project.ID, err)
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
	dest := hackathonURLForConf(conf) + "/projects/" + url.PathEscape(project.ID)
	if !projectEditableByDeadline(competition) {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Project submissions are closed."), http.StatusSeeOther)
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	if err := getters.SetProjectAwardOptIns(ctx, project.ID, r.Form["AwardID"]); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s award opt-ins: %s", competition.Slug, project.ID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := getters.SubmitProject(ctx, project.ID); err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s submit: %s", competition.Slug, project.ID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Project submitted"), http.StatusSeeOther)
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
		if award == nil || !award.OptInRequired || award.Status != getters.AwardStatusAvailable {
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
	dest := hackathonURLForConf(conf) + "/projects/" + url.PathEscape(project.ID)
	if !projectEditableByDeadline(competition) {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Project edits are closed."), http.StatusSeeOther)
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, dest+"?error="+url.QueryEscape("Bad form"), http.StatusSeeOther)
		return
	}
	token, _, err := getters.CreateProjectInvite(ctx, project.ID, r.FormValue("Email"), nil)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s/projects/%s invite: %s", competition.Slug, project.ID, err)
		http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	inviteURL := absoluteURL(r, "/hackathons/invites/"+url.PathEscape(token))
	http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Invite link created")+"&invite="+url.QueryEscape(inviteURL), http.StatusSeeOther)
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
	invite, err := getters.AcceptProjectInvite(ctx, mux.Vars(r)["token"], personID)
	if err != nil {
		ctx.Err.Printf("/hackathons/invites accept: %s", err)
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

func loadPublicHackathonAwards(ctx *config.AppContext, competitionID string) ([]*types.Award, map[string][]*types.Prize, map[string][]*types.ProjectAward, error) {
	awards, err := getters.ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		return nil, nil, nil, err
	}
	prizes, err := getters.ListPrizesForCompetition(ctx, competitionID)
	if err != nil {
		return nil, nil, nil, err
	}
	projectAwards, err := getters.ListProjectAwardsForCompetition(ctx, competitionID)
	if err != nil {
		return nil, nil, nil, err
	}
	awardeesByAward := make(map[string][]*types.ProjectAward)
	for _, projectAward := range projectAwards {
		if projectAward != nil {
			awardeesByAward[projectAward.AwardID] = append(awardeesByAward[projectAward.AwardID], projectAward)
		}
	}
	publicAwardIDs := map[string]bool{}
	publicAwards := make([]*types.Award, 0, len(awards))
	for _, award := range awards {
		if award == nil || award.Status != getters.AwardStatusAwarded || len(awardeesByAward[award.ID]) == 0 {
			continue
		}
		publicAwardIDs[award.ID] = true
		publicAwards = append(publicAwards, award)
	}
	prizesByAward := make(map[string][]*types.Prize)
	for _, prize := range prizes {
		if prize != nil && publicAwardIDs[prize.AwardID] {
			prizesByAward[prize.AwardID] = append(prizesByAward[prize.AwardID], prize)
		}
	}
	publicAwardeesByAward := make(map[string][]*types.ProjectAward)
	for awardID := range publicAwardIDs {
		publicAwardeesByAward[awardID] = awardeesByAward[awardID]
	}
	return publicAwards, prizesByAward, publicAwardeesByAward, nil
}

func loadHackathonPageData(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*types.HackathonCompetition, *types.Conf, *auth.Identity, []*types.HackathonProject, error) {
	competition, conf, id, err := loadHackathonCompetition(w, r, ctx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	viewer := hackathonViewerFromIdentity(id, conf)
	projects, err := getters.ListProjectsForCompetition(ctx, competition.ID, viewer)
	if err != nil {
		ctx.Err.Printf("/hackathons/%s list projects: %s", competition.Slug, err)
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
		return nil, nil, nil, fmt.Errorf("hidden competition %s", competition.Slug)
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
		ctx.Err.Printf("/hackathons/%s/judging events: %s", competition.Slug, err)
		http.Error(w, "Unable to load judge events", http.StatusInternalServerError)
		return nil, nil, nil, nil, err
	}
	viewer := hackathonViewerFromIdentity(id, conf)
	if !viewer.Admin && !viewer.Coordinator && !viewerCanJudgeCompetition(ctx, competition.ID, viewer.PersonID) {
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
	viewer := hackathonViewerFromIdentity(id, conf)
	if !viewer.Admin && !viewer.Coordinator && !viewerCanManageProject(ctx, project.ID, viewer.PersonID) {
		http.Redirect(w, r, hackathonURLForConf(conf)+"?error="+url.QueryEscape("Only project members can edit that project."), http.StatusSeeOther)
		return nil, nil, nil, nil, fmt.Errorf("viewer cannot edit project %s", project.ID)
	}
	return competition, conf, id, project, nil
}

func projectInputFromRequest(w http.ResponseWriter, r *http.Request, competitionID string) (getters.ProjectInput, error) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		return getters.ProjectInput{}, fmt.Errorf("bad form")
	}
	in := getters.ProjectInput{
		CompetitionID:    competitionID,
		Title:            strings.TrimSpace(r.FormValue("Title")),
		ShortDescription: strings.TrimSpace(r.FormValue("ShortDescription")),
		Description:      strings.TrimSpace(r.FormValue("Description")),
		GitHubURL:        strings.TrimSpace(r.FormValue("GitHubURL")),
		DemoURL:          strings.TrimSpace(r.FormValue("DemoURL")),
		VideoURL:         strings.TrimSpace(r.FormValue("VideoURL")),
		SlidesURL:        strings.TrimSpace(r.FormValue("SlidesURL")),
		DocsURL:          strings.TrimSpace(r.FormValue("DocsURL")),
		Tags:             splitProjectTags(r.FormValue("Tags")),
	}
	if in.Title == "" {
		return getters.ProjectInput{}, fmt.Errorf("project title is required")
	}
	return in, nil
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

func validateScorecardRankings(ctx *config.AppContext, competition *types.HackathonCompetition, viewer types.HackathonViewer, event *types.JudgeEvent, rankings []getters.ScorecardRankingInput) error {
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

func hackathonViewerPersonID(id *auth.Identity) string {
	if id == nil || id.Speaker == nil {
		return ""
	}
	return id.Speaker.ID
}

func competitionAcceptsProjects(competition *types.HackathonCompetition) bool {
	if competition == nil || competition.Visibility != getters.CompetitionVisibilityPublic {
		return false
	}
	switch competition.LifecycleOverride {
	case getters.CompetitionLifecycleOpen:
		return true
	case getters.CompetitionLifecycleSubmissionsClosed, getters.CompetitionLifecyclePublicGallery:
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

func projectEditableByDeadline(competition *types.HackathonCompetition) bool {
	if competition == nil {
		return false
	}
	switch competition.LifecycleOverride {
	case getters.CompetitionLifecycleOpen:
		return true
	case getters.CompetitionLifecycleSubmissionsClosed, getters.CompetitionLifecyclePublicGallery:
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
		if judge != nil && judge.PersonID == personID {
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
			out[judge.JudgeType] = true
		}
	}
	return out
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

func hackathonURLForConf(conf *types.Conf) string {
	if conf == nil || strings.TrimSpace(conf.Tag) == "" {
		return ""
	}
	return "/" + url.PathEscape(conf.Tag) + "/hackathon"
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
	if competition.PublicGalleryEnabled {
		return "Submissions public"
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
	case getters.CompetitionLifecyclePublicGallery:
		return "Public gallery"
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
	case "withdrawn":
		return "Withdrawn"
	case "noshow":
		return "No-show"
	case "finalist":
		return "Finalist"
	case "disqualified":
		return "Disqualified"
	case "shipped":
		return "Shipped"
	default:
		return strings.TrimSpace(status)
	}
}

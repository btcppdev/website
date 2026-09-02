package api

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"time"
)

type dataSource interface {
	ListConferences() ([]*types.Conf, error)
	GetConference(tag string) (*types.Conf, error)
	ListConferenceDays(tag string) ([]*types.ConfInfo, error)
	ListConferenceTalks(tag string) ([]*types.Talk, error)
	ListPublicProfiles() ([]*getters.PublicProfile, error)
	ListOrganizations() ([]*types.Org, error)
	ListSponsorships(conferenceID string) ([]*types.Sponsorship, error)
	ListRecordings() ([]*types.Recording, error)
	ListCompetitions() ([]*types.HackathonCompetition, error)
	ListProjects(competitionID string) ([]*types.HackathonProject, error)
	ListProjectMembers(competitionID string) (map[string][]*types.ProjectMember, error)
	ListAwards(competitionID string) ([]*types.Award, error)
	ListPrizes(competitionID string) ([]*types.Prize, error)
	ListProjectAwards(competitionID string) ([]*types.ProjectAward, error)
	ListAccountingInventoryVariants(after time.Time, afterID string, limit int) ([]*types.AccountingInventoryVariant, error)
	ListAccountingInventorySales(after time.Time, afterID string, limit int) ([]*types.AccountingInventorySale, error)
}

type postgresSource struct {
	app *config.AppContext
}

func (s postgresSource) ListConferences() ([]*types.Conf, error) {
	return getters.ListConfs(s.app)
}

func (s postgresSource) GetConference(tag string) (*types.Conf, error) {
	return getters.GetConfByTag(s.app, tag)
}

func (s postgresSource) ListConferenceDays(tag string) ([]*types.ConfInfo, error) {
	return getters.ListConfInfos(s.app, tag)
}

func (s postgresSource) ListConferenceTalks(tag string) ([]*types.Talk, error) {
	return getters.ListTalksForConf(s.app, tag)
}

func (s postgresSource) ListPublicProfiles() ([]*getters.PublicProfile, error) {
	return getters.ListPublicProfiles(s.app)
}

func (s postgresSource) ListOrganizations() ([]*types.Org, error) {
	return getters.ListOrgs(s.app)
}

func (s postgresSource) ListSponsorships(conferenceID string) ([]*types.Sponsorship, error) {
	return getters.ListSponsorships(s.app, conferenceID)
}

func (s postgresSource) ListRecordings() ([]*types.Recording, error) {
	return getters.ListRecordings(s.app)
}

func (s postgresSource) ListCompetitions() ([]*types.HackathonCompetition, error) {
	return getters.ListCompetitions(s.app)
}

func (s postgresSource) ListProjects(competitionID string) ([]*types.HackathonProject, error) {
	return getters.ListProjectsForCompetition(s.app, competitionID, types.HackathonViewer{})
}

func (s postgresSource) ListProjectMembers(competitionID string) (map[string][]*types.ProjectMember, error) {
	return getters.ListProjectMembersForCompetition(s.app, competitionID)
}

func (s postgresSource) ListAwards(competitionID string) ([]*types.Award, error) {
	return getters.ListAwardsForCompetition(s.app, competitionID)
}

func (s postgresSource) ListPrizes(competitionID string) ([]*types.Prize, error) {
	return getters.ListPrizesForCompetition(s.app, competitionID)
}

func (s postgresSource) ListProjectAwards(competitionID string) ([]*types.ProjectAward, error) {
	return getters.ListProjectAwardsForCompetition(s.app, competitionID)
}

func (s postgresSource) ListAccountingInventoryVariants(after time.Time, afterID string, limit int) ([]*types.AccountingInventoryVariant, error) {
	return getters.ListAccountingInventoryVariants(s.app, after, afterID, limit)
}

func (s postgresSource) ListAccountingInventorySales(after time.Time, afterID string, limit int) ([]*types.AccountingInventorySale, error) {
	return getters.ListAccountingInventorySales(s.app, after, afterID, limit)
}

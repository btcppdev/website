package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/ics"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func TestHackathonAdminPageHackathonURLUsesLoadedConference(t *testing.T) {
	competition := &types.HackathonCompetition{ConferenceID: "conf-toronto"}
	page := &HackathonAdminPage{
		Conf: &types.Conf{Ref: "conf-toronto", Tag: "toronto"},
	}
	if got := page.HackathonURL(competition); got != "/toronto/hackathon" {
		t.Fatalf("HackathonURL() = %q, want %q", got, "/toronto/hackathon")
	}
}

func TestHackathonPageCompetitionImageUsesLoadedConference(t *testing.T) {
	competition := &types.HackathonCompetition{ConferenceID: "conf-toronto"}
	page := &HackathonPage{
		Competition: competition,
		Conf:        &types.Conf{Ref: "conf-toronto", Tag: "toronto"},
	}

	if got := page.CompetitionImagePNG(competition); got != "/static/img/toronto/leading.png" {
		t.Fatalf("CompetitionImagePNG() = %q, want Toronto leading image", got)
	}
	if got := page.CompetitionImageAVIF(competition); got != "/static/img/toronto/leading.avif" {
		t.Fatalf("CompetitionImageAVIF() = %q, want Toronto leading image", got)
	}
}

func TestHackathonPrimaryProjectActionOpenSubmissions(t *testing.T) {
	page := &HackathonPage{
		Competition: &types.HackathonCompetition{
			Visibility:        getters.CompetitionVisibilityPublic,
			LifecycleOverride: getters.CompetitionLifecycleOpen,
		},
		Conf: &types.Conf{Tag: "toronto"},
	}
	action := page.PrimaryProjectAction()
	if action.Label != "Create project →" || action.URL != "/toronto/hackathon/projects/new" || action.Disabled {
		t.Fatalf("PrimaryProjectAction() = %+v, want create-project link for signed-out users", action)
	}

	page.Viewer = &auth.Identity{Email: "builder@example.com"}
	action = page.PrimaryProjectAction()
	if action.Label != "Create project →" || action.URL != "/toronto/hackathon/projects/new" || action.Disabled {
		t.Fatalf("PrimaryProjectAction() signed in without profile = %+v, want create-project link", action)
	}

	page.Viewer = &auth.Identity{Email: "builder@example.com", Speaker: &types.Speaker{ID: "person-1"}}
	action = page.PrimaryProjectAction()
	if action.Label != "Buy ticket →" || action.URL != "/toronto#tickets" || action.Disabled {
		t.Fatalf("PrimaryProjectAction() signed in without ticket = %+v, want buy-ticket link", action)
	}

	page.HasConferenceTicket = true
	action = page.PrimaryProjectAction()
	if action.Label != "Create project →" || action.URL != "/toronto/hackathon/projects/new" || action.Disabled {
		t.Fatalf("PrimaryProjectAction() signed in with ticket = %+v, want create-project link", action)
	}
}

func TestHackathonPrimaryProjectActionExistingProject(t *testing.T) {
	page := &HackathonPage{
		Competition: &types.HackathonCompetition{
			Visibility:        getters.CompetitionVisibilityPublic,
			LifecycleOverride: getters.CompetitionLifecycleOpen,
		},
		Conf:          &types.Conf{Tag: "toronto"},
		Projects:      []*types.HackathonProject{{ID: "project-1"}},
		OwnedProjects: map[string]bool{"project-1": true},
		Viewer:        &auth.Identity{Email: "builder@example.com", Speaker: &types.Speaker{ID: "person-1"}},
	}
	action := page.PrimaryProjectAction()
	if action.Label != "Edit project →" || action.URL != "/toronto/hackathon/projects/project-1/edit" || action.Disabled {
		t.Fatalf("PrimaryProjectAction() = %+v, want edit-project link", action)
	}
}

func TestExistingProjectFromMembers(t *testing.T) {
	projects := []*types.HackathonProject{
		{ID: "project-1", Title: "First"},
		{ID: "project-2", Title: "Second"},
	}
	members := map[string][]*types.ProjectMember{
		"project-2": {{ProjectID: "project-2", PersonID: "person-1"}},
	}

	got := existingProjectFromMembers(projects, members, "person-1")
	if got == nil || got.ID != "project-2" {
		t.Fatalf("existingProjectFromMembers() = %+v, want project-2", got)
	}
	if got := existingProjectFromMembers(projects, members, "missing-person"); got != nil {
		t.Fatalf("existingProjectFromMembers() missing person = %+v, want nil", got)
	}
}

func TestProjectEditURLForConf(t *testing.T) {
	got := projectEditURLForConf(&types.Conf{Tag: "toronto"}, &types.HackathonProject{ID: "project/id"})
	want := "/toronto/hackathon/projects/project%2Fid/edit"
	if got != want {
		t.Fatalf("projectEditURLForConf() = %q, want %q", got, want)
	}
}

func TestHackathonPageJudgeProfileURL(t *testing.T) {
	judge := &types.CompetitionJudge{PersonID: "judge-id"}
	page := &HackathonPage{JudgeProfileURLs: map[string]string{"judge-id": "/whois/alice"}}
	if got := page.JudgeProfileURL(judge); got != "/whois/alice" {
		t.Fatalf("JudgeProfileURL() = %q, want /whois/alice", got)
	}
	if got := page.JudgeProfileURL(&types.CompetitionJudge{PersonID: "no-profile"}); got != "" {
		t.Fatalf("JudgeProfileURL() without public profile = %q, want empty", got)
	}
}

func TestHackathonAdminPageUsesConferenceScopedAdminURLs(t *testing.T) {
	competition := &types.HackathonCompetition{ID: "hackathon-id", ConferenceID: "conf-toronto"}
	page := &HackathonAdminPage{
		Competition: competition,
		Conf:        &types.Conf{Ref: "conf-toronto", Tag: "toronto"},
	}
	if got := page.EditURL(competition); got != "/toronto/admin/hackathon" {
		t.Fatalf("EditURL() = %q", got)
	}
	if got := page.ProjectsURL(competition); got != "/toronto/admin/hackathon/projects" {
		t.Fatalf("ProjectsURL() = %q", got)
	}
	if got := page.JudgingURL(competition); got != "/toronto/admin/hackathon/judging" {
		t.Fatalf("JudgingURL() = %q", got)
	}
	if got := page.AwardsURL(competition); got != "/toronto/admin/hackathon/awards" {
		t.Fatalf("AwardsURL() = %q", got)
	}
}

func TestConferenceScopedHackathonAdminRoutes(t *testing.T) {
	router := mux.NewRouter()
	registerConferenceHackathonAdminRoutes(router, nil)
	for _, path := range []string{
		"/toronto/admin/hackathon",
		"/toronto/admin/hackathon/projects",
		"/toronto/admin/hackathon/timeline",
		"/toronto/admin/hackathon/judging",
		"/toronto/admin/hackathon/judging/scores",
		"/toronto/admin/hackathon/awards",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		var match mux.RouteMatch
		if !router.Match(req, &match) {
			t.Errorf("conference hackathon admin route %s is not registered", path)
		}
	}
}

func TestEventBlockSeparatesHackathonCoordinatorFromJudge(t *testing.T) {
	coordinator := &EventBlock{JudgeTypes: []string{"coordinator"}}
	if !coordinator.IsHackathonCoordinator() || coordinator.IsHackathonJudge() {
		t.Fatalf("coordinator classification is wrong: %+v", coordinator)
	}
	judgeCoordinator := &EventBlock{JudgeTypes: []string{"expo", "coordinator"}}
	if !judgeCoordinator.IsHackathonCoordinator() || !judgeCoordinator.IsHackathonJudge() {
		t.Fatalf("judge/coordinator classification is wrong: %+v", judgeCoordinator)
	}
}

func TestCoordinatorRoleDoesNotGrantScoringAccess(t *testing.T) {
	event := &types.JudgeEvent{PlaybookType: "expo", State: "open"}
	coordinator := &HackathonPage{
		Competition: &types.HackathonCompetition{JudgingMode: "manual"},
		JudgeTypes:  map[string]bool{"coordinator": true},
	}
	if coordinator.CanScoreJudgeEvent(event) {
		t.Fatal("coordinator-only assignment grants expo scoring access")
	}
	expoJudge := &HackathonPage{
		Competition: &types.HackathonCompetition{JudgingMode: "manual"},
		JudgeTypes:  map[string]bool{"expo": true},
	}
	if !expoJudge.CanScoreJudgeEvent(event) {
		t.Fatal("expo assignment does not grant expo scoring access")
	}
}

func TestProjectEditingPausedOnlyDuringActiveJudging(t *testing.T) {
	now := time.Date(2026, time.July, 22, 15, 0, 0, 0, time.UTC)
	manual := &types.HackathonCompetition{JudgingMode: getters.CompetitionJudgingModeManual}
	if projectEditingPausedForJudging(manual, []*types.JudgeEvent{{State: getters.JudgeEventStatePending}}, now) {
		t.Fatal("pending manual judging event paused project editing")
	}
	if !projectEditingPausedForJudging(manual, []*types.JudgeEvent{{State: getters.JudgeEventStateOpen}}, now) {
		t.Fatal("open manual judging event did not pause project editing")
	}
	if projectEditingPausedForJudging(manual, []*types.JudgeEvent{{State: getters.JudgeEventStateClosed}}, now) {
		t.Fatal("closed manual judging event paused project editing")
	}

	automatic := &types.HackathonCompetition{JudgingMode: getters.CompetitionJudgingModeAutomatic}
	starts := now.Add(-time.Hour)
	ends := now.Add(time.Hour)
	if !projectEditingPausedForJudging(automatic, []*types.JudgeEvent{{StartsAt: &starts, EndsAt: &ends}}, now) {
		t.Fatal("active automatic judging window did not pause project editing")
	}
	ended := now.Add(-time.Minute)
	if projectEditingPausedForJudging(automatic, []*types.JudgeEvent{{StartsAt: &starts, EndsAt: &ended}}, now) {
		t.Fatal("ended automatic judging window paused project editing")
	}
}

func TestPublicJudgeRoleLabel(t *testing.T) {
	page := &HackathonPage{}
	tests := []struct {
		name  string
		roles []string
		want  string
	}{
		{name: "expo", roles: []string{"expo"}, want: "Judge"},
		{name: "finals", roles: []string{"finals"}, want: "Judge"},
		{name: "both judging rounds", roles: []string{"expo", "finals"}, want: "Judge"},
		{name: "coordinator", roles: []string{"coordinator"}, want: "Coordinator"},
		{name: "judge and coordinator", roles: []string{"finals", "coordinator"}, want: "Judge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			judge := &types.CompetitionJudge{JudgeTypes: tt.roles}
			if got := page.PublicJudgeRoleLabel(judge); got != tt.want {
				t.Fatalf("PublicJudgeRoleLabel() = %q, want %q", got, tt.want)
			}
		})
	}
	if got := page.PublicJudgeRoleLabel(&types.CompetitionJudge{Company: "ACME Labs", JudgeTypes: []string{"expo"}}); got != "ACME Labs" {
		t.Fatalf("PublicJudgeRoleLabel() with company = %q, want ACME Labs", got)
	}
	if got := page.PublicJudgeRoleLabel(&types.CompetitionJudge{PublicLabelOverride: "Partner judge", Company: "ACME Labs", JudgeTypes: []string{"expo"}}); got != "Partner judge" {
		t.Fatalf("PublicJudgeRoleLabel() with override = %q, want Partner judge", got)
	}
}

func TestScoreAdvanceOnlyAppearsBeforeAnotherJudgingRound(t *testing.T) {
	events := []*types.JudgeEvent{
		{ID: "expo", Name: "Expo"},
		{ID: "finals", Name: "Finals"},
	}
	page := &HackathonAdminPage{JudgeEvents: events, ScoreJudgeEventID: "expo"}
	if !page.ScoreHasNextJudgeEvent() || page.ScoreNextJudgeEventLabel() != "Finals" {
		t.Fatalf("expo next event = %+v, %q", page.ScoreNextJudgeEvent(), page.ScoreNextJudgeEventLabel())
	}
	page.ScoreJudgeEventID = "finals"
	if page.ScoreHasNextJudgeEvent() || page.ScoreNextJudgeEvent() != nil {
		t.Fatalf("finals unexpectedly has a next judging event: %+v", page.ScoreNextJudgeEvent())
	}
}

func TestHackathonAdminPageAwardCanAssignHonorsLimit(t *testing.T) {
	limit := 1
	award := &types.Award{ID: "award", MaxAwardees: &limit}
	page := &HackathonAdminPage{AwardeesByAward: map[string][]*types.ProjectAward{}}
	if !page.AwardCanAssign(award) {
		t.Fatal("AwardCanAssign() = false before the award has a winner")
	}
	page.AwardeesByAward[award.ID] = []*types.ProjectAward{{AwardID: award.ID, ProjectID: "winner"}}
	if page.AwardCanAssign(award) {
		t.Fatal("AwardCanAssign() = true after reaching the awardee limit")
	}
	if got := page.AwardAssignmentLimitMessage(award); !strings.Contains(got, "1 of 1") {
		t.Fatalf("AwardAssignmentLimitMessage() = %q, want count and limit", got)
	}

	unlimited := &types.Award{ID: "unlimited"}
	page.AwardeesByAward[unlimited.ID] = []*types.ProjectAward{{AwardID: unlimited.ID, ProjectID: "winner"}}
	if !page.AwardCanAssign(unlimited) {
		t.Fatal("AwardCanAssign() = false for an unlimited award")
	}
}

func TestScoreAwardHelpersShowExistingAssignments(t *testing.T) {
	limit := 1
	assigned := &types.Award{ID: "assigned", Title: "First place", MaxAwardees: &limit}
	available := &types.Award{ID: "available", Title: "Design prize"}
	finalistsOnly := &types.Award{ID: "finalists-only", Title: "Second place", FinalistsOnly: true}
	page := &HackathonAdminPage{
		Awards: []*types.Award{assigned, available, finalistsOnly},
		Projects: []*types.HackathonProject{
			{ID: "finalist", Status: getters.ProjectStatusAdvanced},
		},
		NonFinalistProjects: []*types.HackathonProject{
			{ID: "winner", Status: getters.ProjectStatusSubmitted},
		},
		AwardeesByAward: map[string][]*types.ProjectAward{
			assigned.ID: {{AwardID: assigned.ID, ProjectID: "winner"}},
		},
	}
	gotAssigned := page.ProjectAssignedAwards("winner")
	if len(gotAssigned) != 1 || gotAssigned[0] != assigned {
		t.Fatalf("ProjectAssignedAwards() = %+v, want assigned award", gotAssigned)
	}
	gotAssignable := page.ProjectAssignableAwards("winner")
	if len(gotAssignable) != 1 || gotAssignable[0] != available {
		t.Fatalf("ProjectAssignableAwards(non-finalist) = %+v, want only available award", gotAssignable)
	}
	gotAssignable = page.ProjectAssignableAwards("finalist")
	if len(gotAssignable) != 2 || gotAssignable[0] != available || gotAssignable[1] != finalistsOnly {
		t.Fatalf("ProjectAssignableAwards(finalist) = %+v, want general and finalists-only awards", gotAssignable)
	}
	finalizedAt := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	page.Competition = &types.HackathonCompetition{
		ResultsFinalizedAt:   &finalizedAt,
		ResultsFinalizedName: "Results Coordinator",
	}
	if got := page.ProjectAssignableAwards("finalist"); len(got) != 0 {
		t.Fatalf("ProjectAssignableAwards(finalized) = %+v, want none", got)
	}
	if page.AwardCanAssign(available) {
		t.Fatal("AwardCanAssign() = true after results finalization")
	}
	if got := page.ResultsFinalizedLabel(); !strings.Contains(got, "Results Coordinator") {
		t.Fatalf("ResultsFinalizedLabel() = %q, want finalizer name", got)
	}
}

func TestAvailableOptInAwardsIncludesTentativeOutcomeStatuses(t *testing.T) {
	awards := []*types.Award{
		{ID: "draft", OptInRequired: true, Status: getters.AwardStatusDraft},
		{ID: "available", OptInRequired: true, Status: getters.AwardStatusAvailable},
		{ID: "unawarded", OptInRequired: true, Status: getters.AwardStatusUnawarded},
		{ID: "awarded", OptInRequired: true, Status: getters.AwardStatusAwarded},
		{ID: "not-opt-in", OptInRequired: false, Status: getters.AwardStatusAvailable},
	}

	got := availableOptInAwards(awards)
	if len(got) != 3 || got[0].ID != "available" || got[1].ID != "unawarded" || got[2].ID != "awarded" {
		t.Fatalf("availableOptInAwards() = %+v, want all active opt-in awards", got)
	}
}

func TestHackathonAdminConfsOnlyReturnsAssignedConferences(t *testing.T) {
	toronto := &types.Conf{Tag: "toronto"}
	nairobi := &types.Conf{Tag: "nairobi"}
	id := &auth.Identity{Roles: []auth.Role{{Scope: "toronto", Name: auth.RoleAdmin}}}

	got := hackathonAdminConfs(id, []*types.Conf{toronto, nairobi})
	if len(got) != 1 || got[0] != toronto {
		t.Fatalf("hackathonAdminConfs() = %+v, want Toronto only", got)
	}

	id.Roles = []auth.Role{{Scope: auth.GlobalScope, Name: auth.RoleAdmin}}
	got = hackathonAdminConfs(id, []*types.Conf{toronto, nairobi})
	if len(got) != 2 {
		t.Fatalf("global hackathonAdminConfs() returned %d conferences, want 2", len(got))
	}
}

func TestJudgeTypeFromForm(t *testing.T) {
	for _, judgeType := range []string{"expo", "finals", "coordinator"} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(url.Values{"JudgeType": {judgeType}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		got, err := judgeTypeFromForm(r)
		if err != nil || got != judgeType {
			t.Errorf("judgeTypeFromForm(%q) = %q, %v", judgeType, got, err)
		}
	}

	r := httptest.NewRequest("POST", "/", strings.NewReader(url.Values{"JudgeType": {"judge"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatalf("ParseForm invalid: %v", err)
	}
	if _, err := judgeTypeFromForm(r); err == nil {
		t.Fatal("judgeTypeFromForm accepted an invalid judge type")
	}
}

func TestJudgeTypesFromFormAllowsMultipleRoles(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(url.Values{
		"JudgeType": {"expo", "finals"},
	}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	got, err := judgeTypesFromForm(r)
	if err != nil {
		t.Fatalf("judgeTypesFromForm: %v", err)
	}
	if !sameJudgeTypes(got, []string{"expo", "finals"}) {
		t.Fatalf("judgeTypesFromForm = %v, want expo and finals", got)
	}

	empty := httptest.NewRequest("POST", "/", nil)
	if err := empty.ParseForm(); err != nil {
		t.Fatalf("ParseForm empty: %v", err)
	}
	if _, err := judgeTypesFromForm(empty); err == nil {
		t.Fatal("judgeTypesFromForm accepted no roles")
	}
}

func TestJudgeInviteTypesFromFormAllowsJudgingRolesOnly(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(url.Values{
		"InviteJudgeType": {"expo", "finals"},
	}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	got, err := judgeInviteTypesFromForm(r)
	if err != nil || !sameJudgeTypes(got, []string{"expo", "finals"}) {
		t.Fatalf("judgeInviteTypesFromForm() = %v, %v", got, err)
	}

	coordinator := httptest.NewRequest("POST", "/", strings.NewReader(url.Values{
		"InviteJudgeType": {"coordinator"},
	}.Encode()))
	coordinator.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := coordinator.ParseForm(); err != nil {
		t.Fatalf("ParseForm coordinator: %v", err)
	}
	if _, err := judgeInviteTypesFromForm(coordinator); err == nil {
		t.Fatal("judgeInviteTypesFromForm accepted coordinator access")
	}
}

func TestJudgeRolesFromFormGroupsRolesByPerson(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(url.Values{
		"JudgePersonID": {"person-one", "person-two"},
		"JudgeRole": {
			"person-one|expo",
			"person-one|finals",
			"person-two|coordinator",
		},
	}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	got, err := judgeRolesFromForm(r)
	if err != nil {
		t.Fatalf("judgeRolesFromForm: %v", err)
	}
	if !sameJudgeTypes(got["person-one"], []string{"expo", "finals"}) {
		t.Fatalf("person-one roles = %v, want expo and finals", got["person-one"])
	}
	if !sameJudgeTypes(got["person-two"], []string{"coordinator"}) {
		t.Fatalf("person-two roles = %v, want coordinator", got["person-two"])
	}

	missingRole := httptest.NewRequest("POST", "/", strings.NewReader(url.Values{
		"JudgePersonID": {"person-one", "person-two"},
		"JudgeRole":     {"person-one|expo"},
	}.Encode()))
	missingRole.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := missingRole.ParseForm(); err != nil {
		t.Fatalf("ParseForm missing role: %v", err)
	}
	if _, err := judgeRolesFromForm(missingRole); err == nil {
		t.Fatal("judgeRolesFromForm accepted a judge with no roles")
	}
}

func TestCompactSatoshiLabel(t *testing.T) {
	tests := []struct {
		sats int64
		want string
	}{
		{sats: 0, want: "0 satoshis"},
		{sats: 750, want: "750 satoshis"},
		{sats: 1_000, want: "1k satoshis"},
		{sats: 750_000, want: "750k satoshis"},
		{sats: 2_500_000, want: "2.5M satoshis"},
		{sats: 100_000_000, want: "100M satoshis"},
	}

	for _, tt := range tests {
		if got := compactSatoshiLabel(tt.sats); got != tt.want {
			t.Errorf("compactSatoshiLabel(%d) = %q, want %q", tt.sats, got, tt.want)
		}
	}
}

func TestHackathonPrizePoolValueIncludesNonCashPrizeValues(t *testing.T) {
	page := &HackathonPage{
		PrizePoolByAward: map[string][]*types.Prize{
			"first": {
				{PrizeType: getters.PrizeTypeSats, ValueText: "6000000"},
				{PrizeType: getters.PrizeTypeInKind, Title: "Hardware wallet", ValueText: "2500000"},
			},
		},
	}
	if got := page.PrizePoolValue(); got != "8.5M" {
		t.Fatalf("PrizePoolValue() = %q, want %q", got, "8.5M")
	}
}

func TestHackathonPlacePrizeAmountSumsCashPrizes(t *testing.T) {
	prizes := []*types.Prize{
		{PrizeType: getters.PrizeTypeSats, ValueText: "1000000"},
		{PrizeType: getters.PrizeTypeSats, ValueText: "500000 sats"},
		{PrizeType: getters.PrizeTypeSats, ValueText: "0.01 BTC"},
		{PrizeType: getters.PrizeTypeTrophy, Title: "Trophy", ValueText: "2000000"},
	}
	if got := hackathonPlacePrizeAmount(prizes); got != "2.5M satoshis" {
		t.Fatalf("hackathonPlacePrizeAmount() = %q, want %q", got, "2.5M satoshis")
	}
}

func TestNonCashPrizeNamesIncludesConfiguredPrizeTypes(t *testing.T) {
	prizes := []*types.Prize{
		{PrizeType: getters.PrizeTypeSats, Title: "Cash", ValueText: "1000000"},
		{PrizeType: getters.PrizeTypeInKind, Title: "Hardware wallet", ValueText: "500000"},
		{PrizeType: getters.PrizeTypeTickets, Title: "Conference ticket", ValueText: "250000"},
		{PrizeType: getters.PrizeTypeTrophy},
	}
	got := nonCashPrizeNames(prizes)
	want := []string{"Hardware wallet", "Conference ticket", "Trophy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nonCashPrizeNames() = %#v, want %#v", got, want)
	}
}

func TestHackathonNavConferenceUsesConferenceNavState(t *testing.T) {
	start := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	original := &types.Conf{Tag: "toronto"}
	got := hackathonNavConference(original, []*types.Talk{{Status: StatusScheduled, Sched: &types.Times{Start: start}}}, true)
	if !got.ShowHackathon || !got.HasAgenda {
		t.Fatalf("hackathonNavConference() = %+v, want hackathon and agenda links enabled", got)
	}
	if original.ShowHackathon || original.HasAgenda {
		t.Fatalf("hackathonNavConference mutated original conference: %+v", original)
	}
}

func TestHackathonScheduleCalendarEventUsesPublicVenueLabel(t *testing.T) {
	start := time.Date(2026, 7, 22, 10, 0, 0, 0, time.FixedZone("CDT", -5*60*60))
	end := start.Add(45 * time.Minute)
	conf := &types.Conf{Tag: "toronto", Venue: "Fallback venue"}
	competition := &types.HackathonCompetition{ID: "competition-id", Title: "Toronto Hackathon"}
	event := &HackathonScheduleEvent{
		SegmentID: "segment-id",
		Label:     "Hackathon kickoff",
		Time:      &start,
		End:       &end,
		Venue:     "one",
	}

	got := hackathonScheduleCalendarEvent(conf, competition, event)
	if got.Method != ics.MethodPublish || got.Summary != event.Label {
		t.Fatalf("calendar event = %+v", got)
	}
	if got.Location != "Main Stage" {
		t.Fatalf("Location = %q, want Main Stage", got.Location)
	}
	if !got.Start.Equal(start) || !got.End.Equal(end) {
		t.Fatalf("calendar time = %s-%s, want %s-%s", got.Start, got.End, start, end)
	}
}

func TestHackathonOverviewSelections(t *testing.T) {
	winnerAward := &types.Award{ID: "winner-award", Title: "First place"}
	bountyAward := &types.Award{ID: "bounty", Title: "Best Lightning project"}
	page := &HackathonPage{
		Competition: &types.HackathonCompetition{PublicGalleryEnabled: true},
		Projects: []*types.HackathonProject{
			{ID: "regular", Title: "Regular", Status: getters.ProjectStatusSubmitted},
			{ID: "winner", Title: "Winner", Status: getters.ProjectStatusSubmitted},
			{ID: "third", Title: "Third", Status: getters.ProjectStatusSubmitted},
			{ID: "fourth", Title: "Fourth", Status: getters.ProjectStatusSubmitted},
		},
		Awards: []*types.Award{winnerAward, bountyAward, {ID: "empty", Title: "No prize yet"}},
		PrizesByAward: map[string][]*types.Prize{
			"winner-award": {{AwardID: "winner-award", ValueText: "1000000"}},
			"bounty":       {{AwardID: "bounty", ValueText: "500000"}},
		},
		AwardeesByAward: map[string][]*types.ProjectAward{
			"winner-award": {{AwardID: "winner-award", ProjectID: "winner"}},
		},
	}

	featured := page.FeaturedProjects()
	if len(featured) != 3 || featured[0].ID != "winner" || featured[1].ID != "regular" || featured[2].ID != "third" {
		t.Fatalf("FeaturedProjects() = %+v, want winner followed by gallery order", featured)
	}
	bounties := page.BountyAwards()
	if len(bounties) != 1 || bounties[0].ID != "bounty" {
		t.Fatalf("BountyAwards() = %+v, want only non-ranked award with a prize", bounties)
	}
}

func TestSortPublicHackathonAwardsFinalistsFirstThenValue(t *testing.T) {
	awards := []*types.Award{
		{ID: "general-large", Title: "General Large"},
		{ID: "final-small", Title: "Final Small", FinalistsOnly: true},
		{ID: "general-small", Title: "General Small"},
		{ID: "final-large", Title: "Final Large", FinalistsOnly: true},
		{ID: "general-alpha", Title: "Alpha General"},
	}
	prizes := map[string][]*types.Prize{
		"general-large": {{ValueText: "2000000"}},
		"final-small":   {{ValueText: "500000"}},
		"general-small": {{ValueText: "100000"}},
		"final-large":   {{ValueText: "1000000"}, {ValueText: "250000"}},
		"general-alpha": {{ValueText: "100000"}},
	}

	sortPublicHackathonAwards(awards, prizes)
	want := []string{"final-large", "final-small", "general-large", "general-alpha", "general-small"}
	for i, award := range awards {
		if award == nil || award.ID != want[i] {
			t.Fatalf("sorted awards[%d] = %+v, want %s; all=%+v", i, award, want[i], awards)
		}
	}
}

func TestPublishedProjectGalleryOrdersFinalistAwardsThenPrizeValue(t *testing.T) {
	finalizedAt := time.Now()
	finalSmall := &types.Award{ID: "final-small", Title: "Final Small", FinalistsOnly: true}
	finalLarge := &types.Award{ID: "final-large", Title: "Final Large", FinalistsOnly: true}
	generalLarge := &types.Award{ID: "general-large", Title: "General Large"}
	projects := []*types.HackathonProject{
		{ID: "unawarded", Title: "Unawarded", Status: getters.ProjectStatusSubmitted},
		{ID: "general", Title: "General Winner", Status: getters.ProjectStatusSubmitted},
		{ID: "final-small-project", Title: "Final Small Winner", Status: getters.ProjectStatusSubmitted},
		{ID: "final-large-project", Title: "Final Large Winner", Status: getters.ProjectStatusSubmitted},
	}
	page := &HackathonPage{
		Competition: &types.HackathonCompetition{PublicGalleryEnabled: true, ResultsFinalizedAt: &finalizedAt},
		Projects:    projects,
		Awards:      []*types.Award{generalLarge, finalSmall, finalLarge},
		PrizesByAward: map[string][]*types.Prize{
			"general-large": {{ValueText: "5000000"}},
			"final-small":   {{ValueText: "500000"}},
			"final-large":   {{ValueText: "1000000"}, {ValueText: "250000"}},
		},
		AwardeesByAward: map[string][]*types.ProjectAward{
			"general-large": {{ProjectID: "general"}},
			"final-small":   {{ProjectID: "final-small-project"}},
			"final-large":   {{ProjectID: "final-large-project"}},
		},
	}

	got := page.GalleryProjects()
	want := []string{"final-large-project", "final-small-project", "general", "unawarded"}
	for i, project := range got {
		if project == nil || project.ID != want[i] {
			t.Fatalf("GalleryProjects()[%d] = %+v, want %s; all=%+v", i, project, want[i], got)
		}
	}

	mixedProject := &types.HackathonProject{ID: "mixed", Title: "Mixed Winner"}
	page.AwardeesByAward["general-large"] = append(page.AwardeesByAward["general-large"], &types.ProjectAward{ProjectID: mixedProject.ID})
	page.AwardeesByAward["final-small"] = append(page.AwardeesByAward["final-small"], &types.ProjectAward{ProjectID: mixedProject.ID})
	winningAwards := page.ProjectWinningAwards(mixedProject)
	if len(winningAwards) != 2 || winningAwards[0].ID != "final-small" || winningAwards[1].ID != "general-large" {
		t.Fatalf("ProjectWinningAwards() = %+v, want finalist-only award first", winningAwards)
	}
}

func TestGalleryProjectsRequirePublicGallery(t *testing.T) {
	page := &HackathonPage{
		Competition: &types.HackathonCompetition{},
		Projects: []*types.HackathonProject{
			{ID: "submitted", Status: getters.ProjectStatusSubmitted},
		},
	}
	if got := page.GalleryProjects(); len(got) != 0 {
		t.Fatalf("GalleryProjects() with closed gallery = %+v, want none", got)
	}
	page.Competition.PublicGalleryEnabled = true
	page.Projects = append(page.Projects,
		&types.HackathonProject{ID: "created", Status: getters.ProjectStatusCreated},
		&types.HackathonProject{ID: "hidden", Status: getters.ProjectStatusHidden},
	)
	got := page.GalleryProjects()
	if len(got) != 1 || got[0].ID != "submitted" {
		t.Fatalf("GalleryProjects() = %+v, want only submitted project", got)
	}
}

func TestFilterHackathonCompetitionsSearchesTitleSlugAndConference(t *testing.T) {
	competitions := []*types.HackathonCompetition{
		{ID: "comp-1", ConferenceID: "conf-1", Slug: "berlin-build", Title: "Lightning Builder Day"},
		{ID: "comp-2", ConferenceID: "conf-2", Slug: "austin-ai", Title: "AI Sprint"},
	}
	confs := []*types.Conf{
		{Ref: "conf-1", Tag: "berlin25", Desc: "bitcoin++ Berlin 2025"},
		{Ref: "conf-2", Tag: "austin25", Desc: "bitcoin++ Austin 2025"},
	}

	tests := []struct {
		name string
		q    string
		want string
	}{
		{name: "title", q: "lightning", want: "comp-1"},
		{name: "slug", q: "austin-ai", want: "comp-2"},
		{name: "conference", q: "berlin", want: "comp-1"},
		{name: "conference tag", q: "austin25", want: "comp-2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterHackathonCompetitions(competitions, confs, tt.q)
			if len(got) != 1 || got[0].ID != tt.want {
				t.Fatalf("filterHackathonCompetitions(%q) = %#v, want only %s", tt.q, got, tt.want)
			}
		})
	}
}

func TestSortHackathonCompetitions(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	confs := []*types.Conf{
		{Ref: "conf-a", Desc: "bitcoin++ Austin 2025"},
		{Ref: "conf-b", Desc: "bitcoin++ Berlin 2025"},
	}

	tests := []struct {
		name string
		mode string
		want []string
	}{
		{name: "newest", mode: hackathonSortNewest, want: []string{"new", "middle", "old"}},
		{name: "oldest", mode: hackathonSortOldest, want: []string{"old", "middle", "new"}},
		{name: "title", mode: hackathonSortTitle, want: []string{"middle", "old", "new"}},
		{name: "conference", mode: hackathonSortConference, want: []string{"old", "new", "middle"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			competitions := []*types.HackathonCompetition{
				{ID: "new", ConferenceID: "conf-a", Title: "Zebra", CreatedAt: newer},
				{ID: "old", ConferenceID: "conf-a", Title: "Beta", CreatedAt: older},
				{ID: "middle", ConferenceID: "conf-b", Title: "Alpha", CreatedAt: older.Add(24 * time.Hour)},
			}

			sortHackathonCompetitions(competitions, confs, tt.mode)
			for i, want := range tt.want {
				if competitions[i].ID != want {
					t.Fatalf("position %d = %s, want %s", i, competitions[i].ID, want)
				}
			}
		})
	}
}

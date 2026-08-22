package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

type fakeSource struct {
	conferences   []*types.Conf
	days          map[string][]*types.ConfInfo
	talks         map[string][]*types.Talk
	profiles      []*getters.PublicProfile
	organizations []*types.Org
	sponsorships  map[string][]*types.Sponsorship
	recordings    []*types.Recording
	competitions  []*types.HackathonCompetition
	projects      map[string][]*types.HackathonProject
	members       map[string]map[string][]*types.ProjectMember
	awards        map[string][]*types.Award
	prizes        map[string][]*types.Prize
	projectAwards map[string][]*types.ProjectAward
	err           error
}

func (f *fakeSource) ListConferences() ([]*types.Conf, error) {
	return f.conferences, f.err
}

func (f *fakeSource) GetConference(tag string) (*types.Conf, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, conf := range f.conferences {
		if conf != nil && conf.Tag == tag {
			return conf, nil
		}
	}
	return nil, nil
}

func (f *fakeSource) ListConferenceDays(tag string) ([]*types.ConfInfo, error) {
	return f.days[tag], f.err
}

func (f *fakeSource) ListConferenceTalks(tag string) ([]*types.Talk, error) {
	return f.talks[tag], f.err
}

func (f *fakeSource) ListPublicProfiles() ([]*getters.PublicProfile, error) {
	return f.profiles, f.err
}

func (f *fakeSource) ListOrganizations() ([]*types.Org, error) { return f.organizations, f.err }
func (f *fakeSource) ListSponsorships(conferenceID string) ([]*types.Sponsorship, error) {
	return f.sponsorships[conferenceID], f.err
}
func (f *fakeSource) ListRecordings() ([]*types.Recording, error) { return f.recordings, f.err }
func (f *fakeSource) ListCompetitions() ([]*types.HackathonCompetition, error) {
	return f.competitions, f.err
}
func (f *fakeSource) ListProjects(competitionID string) ([]*types.HackathonProject, error) {
	return f.projects[competitionID], f.err
}
func (f *fakeSource) ListProjectMembers(competitionID string) (map[string][]*types.ProjectMember, error) {
	return f.members[competitionID], f.err
}
func (f *fakeSource) ListAwards(competitionID string) ([]*types.Award, error) {
	return f.awards[competitionID], f.err
}
func (f *fakeSource) ListPrizes(competitionID string) ([]*types.Prize, error) {
	return f.prizes[competitionID], f.err
}
func (f *fakeSource) ListProjectAwards(competitionID string) ([]*types.ProjectAward, error) {
	return f.projectAwards[competitionID], f.err
}

func testRouter(source dataSource, now time.Time) http.Handler {
	root := mux.NewRouter()
	s := &server{source: source, now: func() time.Time { return now }}
	s.register(root.PathPrefix("/api/v1").Subrouter())
	return root
}

func protectedTestRouter(token *types.PersonAPIToken, person *types.Speaker, emails []*types.PersonEmail) http.Handler {
	root := mux.NewRouter()
	s := &server{
		source: &fakeSource{}, now: time.Now,
		authenticateToken: func(raw string) (*auth.BearerGrant, error) {
			if raw != "test-secret" {
				return nil, nil
			}
			return &auth.BearerGrant{TokenID: token.ID, PersonID: token.PersonID, Scopes: token.Scopes, Kind: "personal_access_token"}, nil
		},
		loadPerson:       func(id string) (*types.Speaker, error) { return person, nil },
		listPersonEmails: func(id string) ([]*types.PersonEmail, error) { return emails, nil },
	}
	s.register(root.PathPrefix("/api/v1").Subrouter())
	return root
}

func TestMeRequiresBearerTokenAndExactScope(t *testing.T) {
	person := &types.Speaker{ID: "person-1", Name: "Mara"}
	router := protectedTestRouter(&types.PersonAPIToken{
		PersonID: person.ID, Scopes: []string{"talks:read"},
	}, person, nil)

	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if missing.Code != http.StatusUnauthorized || !strings.Contains(missing.Header().Get("WWW-Authenticate"), "invalid_token") {
		t.Fatalf("missing token response = %d, challenge %q", missing.Code, missing.Header().Get("WWW-Authenticate"))
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	request.Header.Set("Authorization", "Bearer test-secret")
	insufficient := httptest.NewRecorder()
	router.ServeHTTP(insufficient, request)
	if insufficient.Code != http.StatusForbidden || !strings.Contains(insufficient.Header().Get("WWW-Authenticate"), "profile:self:read") {
		t.Fatalf("scope response = %d, challenge %q, body %s", insufficient.Code, insufficient.Header().Get("WWW-Authenticate"), insufficient.Body.String())
	}
}

func TestMeReturnsPrivateProfileWithoutCredentialSecrets(t *testing.T) {
	verified := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	person := &types.Speaker{
		ID: "person-1", Name: "Mara", Email: "legacy@example.test", Phone: "private-phone",
		TaxFormObjectKey: "private-tax-form", Roles: []string{"dev26-admin"},
	}
	router := protectedTestRouter(&types.PersonAPIToken{
		PersonID: person.ID, Scopes: []string{"profile:self:read"},
	}, person, []*types.PersonEmail{{Email: "mara@example.test", IsPrimary: true, VerifiedAt: verified}})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	request.Header.Set("Authorization", "Bearer test-secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "mara@example.test") || strings.Contains(body, "private-phone") || strings.Contains(body, "private-tax-form") || strings.Contains(body, "legacy@example.test") {
		t.Fatalf("unexpected private profile projection: %s", body)
	}
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}

func TestIdentityReturnsOnlyMinimalAccountAndCurrentRoles(t *testing.T) {
	person := &types.Speaker{
		ID: "person-1", Name: "Mara", Email: "private@example.test", Phone: "private-phone",
		Bio: "private biography", Roles: []string{"dev26-admin", "global-admin"},
	}
	router := protectedTestRouter(&types.PersonAPIToken{
		PersonID: person.ID, Scopes: []string{"identity:self:read"},
	}, person, []*types.PersonEmail{{Email: "also-private@example.test"}})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me/identity", nil)
	request.Header.Set("Authorization", "Bearer test-secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, expected := range []string{`"id":"person-1"`, `"name":"Mara"`, `"global-admin"`, `"dev26-admin"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("identity response omitted %s: %s", expected, body)
		}
	}
	for _, private := range []string{"private@example.test", "also-private@example.test", "private-phone", "private biography"} {
		if strings.Contains(body, private) {
			t.Fatalf("identity response leaked %q: %s", private, body)
		}
	}
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}

func TestMutationURLAndRecordingKeyValidation(t *testing.T) {
	goodURL := "https://example.test/path"
	badURL := "javascript:alert(1)"
	empty := ""
	if !validOptionalHTTPURL(&goodURL) || !validOptionalHTTPURL(&empty) || validOptionalHTTPURL(&badURL) {
		t.Fatal("HTTP URL validation accepted or rejected the wrong value")
	}
	for _, good := range []string{"", "dev26/recordings/edits/talk.mp4"} {
		if !validRecordingObjectKey(good) {
			t.Fatalf("rejected object key %q", good)
		}
	}
	for _, bad := range []string{"/absolute", "../secret", "dev26//talk.mp4", "https://bucket.example/talk.mp4", `dev26\talk.mp4`} {
		if validRecordingObjectKey(bad) {
			t.Fatalf("accepted object key %q", bad)
		}
	}
}

func TestSchedulePlacementRejectsRoomCollisions(t *testing.T) {
	conf := publishedConference("dev26")
	start := conf.StartDate.Add(10 * time.Hour)
	end := start.Add(time.Hour)
	current := &types.Talk{ID: "talk-current", Status: "Accepted", Speakers: []*types.Speaker{{ID: "speaker-1"}}}
	confTalk := &types.ConfTalk{ID: current.ID, Conf: conf}
	source := &fakeSource{
		conferences: []*types.Conf{conf},
		days:        map[string][]*types.ConfInfo{"dev26": {{Day: 1, Venues: []string{"Main"}}}},
		talks:       map[string][]*types.Talk{"dev26": {current}},
	}
	server := &server{source: source}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/conferences/dev26/talks/talk-current/schedule", nil)
	if conflict := server.validateSchedulePlacement(httptest.NewRecorder(), request, confTalk, current, "Main", start, end); conflict {
		t.Fatal("valid placement was rejected")
	}
	existingEnd := end
	source.talks["dev26"] = append(source.talks["dev26"], &types.Talk{
		ID: "talk-existing", Status: "Scheduled", Venue: "Main",
		Sched: &types.Times{Start: start.Add(15 * time.Minute), End: &existingEnd},
	})
	response := httptest.NewRecorder()
	if conflict := server.validateSchedulePlacement(response, request, confTalk, current, "Main", start, end); !conflict || response.Code != http.StatusConflict {
		t.Fatalf("collision accepted: conflict=%v status=%d body=%s", conflict, response.Code, response.Body.String())
	}
}

func publishedConference(tag string) *types.Conf {
	loc := time.FixedZone("Conference", -5*60*60)
	return &types.Conf{
		Ref: "00000000-0000-4000-8000-000000000001", Tag: tag,
		PublicationStatus: "published", Desc: "A public conference",
		EditionType: "conference", Tagline: "Build Bitcoin", Timezone: "America/Chicago",
		Location: "Austin, TX", Venue: "Main Hall", TZ: loc,
		StartDate: time.Date(2026, 8, 20, 0, 0, 0, 0, loc),
		EndDate:   time.Date(2026, 8, 22, 0, 0, 0, 0, loc),
	}
}

func TestBootstrapHasStableETagAndRequestMetadata(t *testing.T) {
	router := testRouter(&fakeSource{}, time.Now())
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", first.Code, first.Body.String())
	}
	if first.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing X-Request-ID")
	}
	if got := first.Header().Get("Cache-Control"); got != publicCacheControl {
		t.Fatalf("Cache-Control = %q", got)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	var body struct {
		Data bootstrapDTO `json:"data"`
		Meta responseMeta `json:"meta"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.APIVersion != "v1" || body.Meta.RequestID == "" {
		t.Fatalf("response = %#v", body)
	}

	secondRequest := httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil)
	secondRequest.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	router.ServeHTTP(second, secondRequest)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("conditional status = %d, body = %q", second.Code, second.Body.String())
	}
	if second.Header().Get("ETag") != etag {
		t.Fatal("ETag changed between identical representations")
	}
}

func TestConferenceListContainsOnlyPublishedAllowlistedFields(t *testing.T) {
	public := publishedConference("dev26")
	draft := publishedConference("secret27")
	draft.PublicationStatus = "draft"
	// These fields exist on the domain object but must not enter the API DTO.
	public.SpeakerDinnerNotes = "private speaker instructions"
	public.PickupAddressLine1 = "private pickup address"

	router := testRouter(&fakeSource{conferences: []*types.Conf{public, draft}}, time.Now())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/conferences", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, "secret27") || strings.Contains(body, "private speaker") || strings.Contains(body, "pickup") {
		t.Fatalf("response leaked draft/private conference data: %s", body)
	}
	var decoded struct {
		Data []conferenceDTO `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(decoded.Data) != 1 || decoded.Data[0].Tag != "dev26" {
		t.Fatalf("conferences = %#v", decoded.Data)
	}
}

func TestDraftConferenceIsIndistinguishableFromMissing(t *testing.T) {
	draft := publishedConference("draft26")
	draft.PublicationStatus = "draft"
	router := testRouter(&fakeSource{conferences: []*types.Conf{draft}}, time.Now())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/conferences/draft26", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "draft") {
		t.Fatalf("draft state leaked in error: %s", response.Body.String())
	}
}

func TestAgendaUsesWebsitePublicationRulesAndPublicSpeakerProjection(t *testing.T) {
	conf := publishedConference("dev26")
	now := conf.StartDate.Add(12 * time.Hour)
	start := now.Add(time.Hour)
	end := start.Add(30 * time.Minute)
	speaker := &types.Speaker{
		ID: "00000000-0000-4000-8000-000000000002", Name: "Mara", Company: "Bitcoin++",
		Email: "private@example.test", Phone: "555-private", TaxFormObjectKey: "private.pdf",
	}
	scheduled := &types.Talk{
		ID: "00000000-0000-4000-8000-000000000003", Name: "Public talk",
		Description: "Public description", Type: "Talk", Status: "Scheduled",
		Sched: &types.Times{Start: start, End: &end}, Venue: "Main", Speakers: []*types.Speaker{speaker},
	}
	accepted := &types.Talk{
		ID: "00000000-0000-4000-8000-000000000004", Name: "Not public yet", Status: "Accepted",
		Sched: &types.Times{Start: start.Add(time.Hour)},
	}
	unscheduled := &types.Talk{
		ID: "00000000-0000-4000-8000-000000000005", Name: "No time", Status: "Scheduled",
	}
	source := &fakeSource{
		conferences: []*types.Conf{conf},
		days: map[string][]*types.ConfInfo{"dev26": {{
			ID: "00000000-0000-4000-8000-000000000006", Day: 1, Venues: []string{"Main"},
			Doors: &types.Times{Start: now},
		}}},
		talks: map[string][]*types.Talk{"dev26": {accepted, unscheduled, scheduled}},
	}
	router := testRouter(source, now)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/conferences/dev26/agenda", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, private := range []string{"private@example.test", "555-private", "private.pdf", "Not public yet", "No time"} {
		if strings.Contains(body, private) {
			t.Fatalf("agenda leaked %q: %s", private, body)
		}
	}
	var decoded struct {
		Data agendaDTO `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(decoded.Data.Talks) != 1 || decoded.Data.Talks[0].Title != "Public talk" {
		t.Fatalf("talks = %#v", decoded.Data.Talks)
	}
	if len(decoded.Data.Talks[0].Speakers) != 1 || decoded.Data.Talks[0].Speakers[0].Name != "Mara" {
		t.Fatalf("speakers = %#v", decoded.Data.Talks[0].Speakers)
	}
}

func TestPastAgendaPreservesScheduledAcceptedArchiveTalks(t *testing.T) {
	conf := publishedConference("past26")
	now := conf.EndDate.Add(24 * time.Hour)
	start := conf.StartDate.Add(10 * time.Hour)
	talk := &types.Talk{
		ID: "00000000-0000-4000-8000-000000000007", Name: "Imported archive talk",
		Status: "Accepted", Sched: &types.Times{Start: start},
	}
	source := &fakeSource{conferences: []*types.Conf{conf}, talks: map[string][]*types.Talk{"past26": {talk}}}
	router := testRouter(source, now)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/conferences/past26/agenda", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Imported archive talk") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestPersonEndpointIsHolisticAndNeverExposesPrivateContactFields(t *testing.T) {
	conf := publishedConference("dev26")
	start := conf.StartDate.Add(10 * time.Hour)
	speaker := &types.Speaker{
		ID: "00000000-0000-4000-8000-000000000101", Name: "Mara Chen", Photo: "mara.avif",
		Company: "Bitcoin++", Bio: "Builds useful things", Email: "private@example.test",
		Phone: "555-private", Signal: "private-signal", Telegram: "private-telegram", TShirt: "private-size",
		Github: "mara", Website: "https://example.com",
	}
	talk := &types.Talk{
		ID: "00000000-0000-4000-8000-000000000102", Name: "API design",
		Description: "Public talk", Type: "Talk", Sched: &types.Times{Start: start},
		Speakers: []*types.Speaker{speaker}, YTLink: "https://youtube.com/watch?v=public",
	}
	project := &types.HackathonProject{
		ID: "00000000-0000-4000-8000-000000000103", CompetitionID: "00000000-0000-4000-8000-000000000104",
		Title: "Wallet", ShortDescription: "Public project", Tags: []string{"wallet"},
	}
	profile := &getters.PublicProfile{
		Speaker: speaker,
		Talks:   []*getters.PublicProfileTalk{{Talk: talk, Conf: conf}},
		Projects: []*getters.PublicProfileProject{{
			Project: project, Conf: conf,
			Members: []*types.ProjectMember{{PersonID: speaker.ID, Name: speaker.Name, Email: "member-private@example.test", Role: "owner"}},
			Awards:  []*getters.PublicProfileProjectAward{{ID: "00000000-0000-4000-8000-000000000105", Title: "Best Wallet"}},
		}},
		Editions: []*types.Conf{conf},
	}
	router := testRouter(&fakeSource{profiles: []*getters.PublicProfile{profile}}, time.Now())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/people/"+speaker.ID, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, publicValue := range []string{"Mara Chen", "API design", "Wallet", "Best Wallet", "/whois/mara"} {
		if !strings.Contains(body, publicValue) {
			t.Errorf("person response is missing %q: %s", publicValue, body)
		}
	}
	for _, privateValue := range []string{"private@example.test", "555-private", "private-signal", "private-telegram", "private-size", "member-private@example.test"} {
		if strings.Contains(body, privateValue) {
			t.Errorf("person response leaked %q: %s", privateValue, body)
		}
	}
}

func TestPublicRecordingsHideSourceFilesAndFuturePublications(t *testing.T) {
	conf := publishedConference("dev26")
	talkID := "00000000-0000-4000-8000-000000000111"
	now := conf.EndDate.Add(time.Hour)
	future := now.Add(time.Hour)
	source := &fakeSource{
		conferences: []*types.Conf{conf},
		talks:       map[string][]*types.Talk{"dev26": {{ID: talkID, Name: "Published talk"}}},
		recordings: []*types.Recording{
			{ID: "00000000-0000-4000-8000-000000000112", ConfTalkID: talkID, FileURI: "private/source.mp4", YTLink: "https://youtube.com/public"},
			{ID: "00000000-0000-4000-8000-000000000113", ConfTalkID: talkID, YTLink: "https://youtube.com/future", PublishAt: &future},
		},
	}
	router := testRouter(source, now)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/recordings", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "https://youtube.com/public") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "private/source.mp4") || strings.Contains(response.Body.String(), "youtube.com/future") {
		t.Fatalf("recording response leaked private/future data: %s", response.Body.String())
	}
}

func TestHackathonResultsRequireFinalization(t *testing.T) {
	conf := publishedConference("dev26")
	competition := &types.HackathonCompetition{
		ID: "00000000-0000-4000-8000-000000000121", ConferenceID: conf.Ref,
		Title: "Hackathon", Visibility: getters.CompetitionVisibilityPublic, PublicGalleryEnabled: true,
	}
	router := testRouter(&fakeSource{conferences: []*types.Conf{conf}, competitions: []*types.HackathonCompetition{competition}}, time.Now())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/hackathons/"+competition.ID+"/results", nil))
	if response.Code != http.StatusNotFound || strings.Contains(strings.ToLower(response.Body.String()), "not finalized") {
		t.Fatalf("unfinalized result response = %d %s", response.Code, response.Body.String())
	}
}

func TestDataSourceFailureUsesStableInternalError(t *testing.T) {
	router := testRouter(&fakeSource{err: errors.New("database unavailable")}, time.Now())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/conferences", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "database unavailable") || !strings.Contains(response.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("unstable/leaky error response: %s", response.Body.String())
	}
}

func TestAPIRejectsUnsupportedResponseMediaType(t *testing.T) {
	router := testRouter(&fakeSource{}, time.Now())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil)
	request.Header.Set("Accept", "text/html")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotAcceptable || !strings.Contains(response.Body.String(), `"code":"not_acceptable"`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestUnknownAPIRouteReturnsJSONErrorWithRequestID(t *testing.T) {
	router := testRouter(&fakeSource{}, time.Now())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/not-here", nil))
	if response.Code != http.StatusNotFound || response.Header().Get("X-Request-ID") == "" {
		t.Fatalf("status = %d, request ID = %q, body = %s", response.Code, response.Header().Get("X-Request-ID"), response.Body.String())
	}
	if !strings.Contains(response.Header().Get("Content-Type"), "application/json") || !strings.Contains(response.Body.String(), `"code":"not_found"`) {
		t.Fatalf("unexpected API error response: %s", response.Body.String())
	}
}

func TestOpenAPIContractListsEveryV1RouteAndNoTranscriptSurface(t *testing.T) {
	document := openAPIV1
	var contract struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(document, &contract); err != nil {
		t.Fatalf("parse OpenAPI contract as JSON: %v", err)
	}
	for _, path := range []string{
		"/openapi.json",
		"/bootstrap",
		"/conferences",
		"/conferences/{tag}",
		"/conferences/{tag}/days",
		"/conferences/{tag}/agenda",
		"/conferences/{tag}/talks/{talk_id}",
		"/conferences/{tag}/talks/{talk_id}/schedule",
		"/conferences/{tag}/talks/{talk_id}/recording",
		"/conferences/{tag}/recording-candidates",
		"/conferences/{tag}/speakers",
		"/conferences/{tag}/sponsors",
		"/conferences/{tag}/hackathons",
		"/people",
		"/people/{person_id}",
		"/organizations/{organization_id}",
		"/recordings",
		"/recordings/{recording_id}",
		"/hackathons/{competition_id}",
		"/hackathons/{competition_id}/projects",
		"/hackathons/{competition_id}/projects/{project_id}",
		"/hackathons/{competition_id}/awards",
		"/hackathons/{competition_id}/results",
		"/me/identity",
		"/me",
		"/me/talks",
	} {
		if _, ok := contract.Paths[path]; !ok {
			t.Errorf("OpenAPI contract is missing %s", path)
		}
	}
	if strings.Contains(strings.ToLower(string(document)), "transcript") {
		t.Fatal("initial OpenAPI contract must not expose transcripts")
	}
}

func TestOpenAPIContractIncludesDocumentationExamples(t *testing.T) {
	var contract struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Examples map[string]struct {
				Value json.RawMessage `json:"value"`
			} `json:"examples"`
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(openAPIV1, &contract); err != nil {
		t.Fatalf("parse OpenAPI contract as JSON: %v", err)
	}
	wiredExamples := make(map[string]string)
	wiredSchemas := make(map[string]string)
	for _, pathItem := range contract.Paths {
		for method, rawOperation := range pathItem {
			if method == "parameters" {
				continue
			}
			var operation struct {
				OperationID string `json:"operationId"`
				Responses   map[string]struct {
					Content map[string]struct {
						Schema struct {
							Ref string `json:"$ref"`
						} `json:"schema"`
						Examples map[string]struct {
							Ref string `json:"$ref"`
						} `json:"examples"`
					} `json:"content"`
				} `json:"responses"`
			}
			if err := json.Unmarshal(rawOperation, &operation); err != nil {
				t.Fatalf("parse %s operation: %v", method, err)
			}
			media := operation.Responses["200"].Content["application/json"]
			wiredSchemas[operation.OperationID] = media.Schema.Ref
			for _, example := range media.Examples {
				wiredExamples[operation.OperationID] = strings.TrimPrefix(example.Ref, "#/components/examples/")
				break
			}
		}
	}

	expectedSchemas := map[string]string{
		"listConferences": "ConferenceListResponse", "getConference": "ConferenceResponse", "getConferenceAgenda": "AgendaResponse",
		"listConferenceSpeakers": "PersonSummaryListResponse", "listPeople": "PersonSummaryListResponse", "getPerson": "PersonResponse",
		"listRecordings": "RecordingListResponse", "listRecordingCandidates": "RecordingCandidateListResponse",
		"putConferenceTalkRecording": "RecordingAdminResponse", "updateRecordingBroadcast": "RecordingBroadcastResponse",
		"listHackathonProjects": "HackathonProjectListResponse", "getHackathonProject": "HackathonProjectResponse",
		"listHackathonResults": "HackathonResultListResponse", "getMyIdentity": "AccountIdentityResponse",
		"getMe": "AccountProfileResponse", "updateMe": "AccountProfileResponse", "listMyTalks": "AccountTalkListResponse",
		"updateConferenceTalkSchedule": "TalkResponse",
	}
	for operationID, schema := range expectedSchemas {
		if got := strings.TrimPrefix(wiredSchemas[operationID], "#/components/schemas/"); got != schema {
			t.Errorf("%s 200 response schema = %q, want %q", operationID, got, schema)
		}
		if got := wiredExamples[operationID]; got != operationID {
			t.Errorf("%s 200 response example = %q, want %q", operationID, got, operationID)
		}
		if _, ok := contract.Components.Examples[operationID]; !ok {
			t.Errorf("OpenAPI contract is missing documentation example %s", operationID)
			continue
		}
		assertOpenAPIExampleSchema(t, contract.Components.Examples[operationID].Value, schema, contract.Components.Schemas)
	}

	assertOpenAPIExample[[]conferenceDTO](t, contract.Components.Examples, "listConferences")
	assertOpenAPIExample[conferenceDTO](t, contract.Components.Examples, "getConference")
	assertOpenAPIExample[agendaDTO](t, contract.Components.Examples, "getConferenceAgenda")
	assertOpenAPIExample[[]personSummaryDTO](t, contract.Components.Examples, "listConferenceSpeakers")
	assertOpenAPIExample[[]personSummaryDTO](t, contract.Components.Examples, "listPeople")
	assertOpenAPIExample[personDTO](t, contract.Components.Examples, "getPerson")
	assertOpenAPIExample[[]recordingDTO](t, contract.Components.Examples, "listRecordings")
	assertOpenAPIExample[[]recordingCandidateDTO](t, contract.Components.Examples, "listRecordingCandidates")
	assertOpenAPIExample[recordingAdminDTO](t, contract.Components.Examples, "putConferenceTalkRecording")
	assertOpenAPIExample[recordingBroadcastDTO](t, contract.Components.Examples, "updateRecordingBroadcast")
	assertOpenAPIExample[[]hackathonProjectDTO](t, contract.Components.Examples, "listHackathonProjects")
	assertOpenAPIExample[hackathonProjectDTO](t, contract.Components.Examples, "getHackathonProject")
	assertOpenAPIExample[[]resultDTO](t, contract.Components.Examples, "listHackathonResults")
	assertOpenAPIExample[accountIdentityDTO](t, contract.Components.Examples, "getMyIdentity")
	assertOpenAPIExample[accountProfileDTO](t, contract.Components.Examples, "getMe")
	assertOpenAPIExample[accountProfileDTO](t, contract.Components.Examples, "updateMe")
	assertOpenAPIExample[[]accountTalkDTO](t, contract.Components.Examples, "listMyTalks")
	assertOpenAPIExample[talkDTO](t, contract.Components.Examples, "updateConferenceTalkSchedule")
}

func assertOpenAPIExampleSchema(t *testing.T, value json.RawMessage, schemaName string, schemas map[string]json.RawMessage) {
	t.Helper()
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Errorf("decode %s example for schema validation: %v", schemaName, err)
		return
	}
	if err := validateOpenAPIExampleValue(decoded, schemas[schemaName], schemas, schemaName); err != nil {
		t.Errorf("OpenAPI example does not satisfy %s: %v", schemaName, err)
	}
}

func validateOpenAPIExampleValue(value any, rawSchema json.RawMessage, schemas map[string]json.RawMessage, path string) error {
	var schema struct {
		Ref                  string                     `json:"$ref"`
		Type                 json.RawMessage            `json:"type"`
		Required             []string                   `json:"required"`
		Properties           map[string]json.RawMessage `json:"properties"`
		Items                json.RawMessage            `json:"items"`
		OneOf                []json.RawMessage          `json:"oneOf"`
		AdditionalProperties any                        `json:"additionalProperties"`
	}
	if err := json.Unmarshal(rawSchema, &schema); err != nil {
		return fmt.Errorf("%s has an invalid schema: %w", path, err)
	}
	if schema.Ref != "" {
		const prefix = "#/components/schemas/"
		if !strings.HasPrefix(schema.Ref, prefix) {
			return fmt.Errorf("%s uses unsupported reference %q", path, schema.Ref)
		}
		name := strings.TrimPrefix(schema.Ref, prefix)
		referenced, ok := schemas[name]
		if !ok {
			return fmt.Errorf("%s references missing schema %q", path, name)
		}
		return validateOpenAPIExampleValue(value, referenced, schemas, path)
	}
	if len(schema.OneOf) > 0 {
		var failures []string
		for _, candidate := range schema.OneOf {
			if err := validateOpenAPIExampleValue(value, candidate, schemas, path); err == nil {
				return nil
			} else {
				failures = append(failures, err.Error())
			}
		}
		return fmt.Errorf("%s does not satisfy any oneOf option: %s", path, strings.Join(failures, "; "))
	}
	var allowedTypes []string
	if len(schema.Type) > 0 {
		if schema.Type[0] == '[' {
			if err := json.Unmarshal(schema.Type, &allowedTypes); err != nil {
				return fmt.Errorf("%s has invalid type list: %w", path, err)
			}
		} else {
			var single string
			if err := json.Unmarshal(schema.Type, &single); err != nil {
				return fmt.Errorf("%s has invalid type: %w", path, err)
			}
			allowedTypes = []string{single}
		}
	}
	actualType := openAPIExampleType(value)
	if len(allowedTypes) > 0 && !containsString(allowedTypes, actualType) {
		return fmt.Errorf("%s is %s, want %s", path, actualType, strings.Join(allowedTypes, " or "))
	}
	switch typed := value.(type) {
	case map[string]any:
		for _, required := range schema.Required {
			if _, ok := typed[required]; !ok {
				return fmt.Errorf("%s is missing required property %q", path, required)
			}
		}
		for name, child := range typed {
			childSchema, ok := schema.Properties[name]
			if !ok {
				if allowed, ok := schema.AdditionalProperties.(bool); ok && !allowed {
					return fmt.Errorf("%s has undocumented property %q", path, name)
				}
				continue
			}
			if err := validateOpenAPIExampleValue(child, childSchema, schemas, path+"."+name); err != nil {
				return err
			}
		}
	case []any:
		for index, child := range typed {
			if err := validateOpenAPIExampleValue(child, schema.Items, schemas, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func openAPIExampleType(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number:
		if strings.ContainsAny(typed.String(), ".eE") {
			return "number"
		}
		return "integer"
	default:
		return "unknown"
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target || (value == "number" && target == "integer") {
			return true
		}
	}
	return false
}

func assertOpenAPIExample[T any](t *testing.T, examples map[string]struct {
	Value json.RawMessage `json:"value"`
}, operationID string) {
	t.Helper()
	example, ok := examples[operationID]
	if !ok {
		return
	}
	var envelope struct {
		Data T            `json:"data"`
		Meta responseMeta `json:"meta"`
	}
	decoder := json.NewDecoder(bytes.NewReader(example.Value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		t.Errorf("OpenAPI documentation example %s does not match its Go response DTO: %v", operationID, err)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Errorf("OpenAPI documentation example %s has trailing JSON", operationID)
		return
	}
	roundTrip, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal %s example DTO: %v", operationID, err)
	}
	var originalValue, roundTripValue any
	if err := json.Unmarshal(example.Value, &originalValue); err != nil {
		t.Fatalf("parse original %s example: %v", operationID, err)
	}
	if err := json.Unmarshal(roundTrip, &roundTripValue); err != nil {
		t.Fatalf("parse round-tripped %s example: %v", operationID, err)
	}
	if !reflect.DeepEqual(originalValue, roundTripValue) {
		t.Errorf("OpenAPI documentation example %s omits or changes fields when round-tripped through its Go response DTO", operationID)
	}
}

func TestOpenAPIContractIsPubliclyServed(t *testing.T) {
	router := testRouter(&fakeSource{}, time.Now())
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil)
	request.Header.Set("Accept", "application/vnd.oai.openapi+json")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "application/vnd.oai.openapi+json") {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if !bytes.Equal(response.Body.Bytes(), openAPIV1) {
		t.Fatal("served OpenAPI contract differs from embedded source")
	}
}

// Package api implements the versioned JSON API without depending on the
// website's HTML handlers or template-facing representations.
package api

import (
	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/publicid"
	"btcpp-web/internal/requestid"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

const publicCacheControl = "public, max-age=60, stale-while-revalidate=300"

type server struct {
	app                 *config.AppContext
	source              dataSource
	now                 func() time.Time
	authenticateToken   func(string) (*auth.BearerGrant, error)
	loadPerson          func(string) (*types.Speaker, error)
	listPersonEmails    func(string) ([]*types.PersonEmail, error)
	listPersonConfs     func(string) ([]*types.SpeakerConf, error)
	updateProfile       func(string, getters.SpeakerProfilePatch) error
	loadConfTalk        func(string) (*types.ConfTalk, error)
	loadTalk            func(string) (*types.Talk, error)
	updateProposal      func(string, getters.ProposalPatch) error
	updateTalkResources func(string, string, string, string) error
	updateTalkSchedule  func(string, string, time.Time, time.Time) error
	listConfRecordings  func(string) ([]*types.Recording, error)
	upsertRecording     func(string, getters.RecordingUpsert) (*types.Recording, error)
	recordAudit         func(*types.AuthAuditEvent) error
	limiter             *rateLimiter
}

type apiPrincipal struct {
	Grant    *auth.BearerGrant
	Person   *types.Speaker
	Identity *auth.Identity
}

type principalContextKey struct{}

// Register installs API v1 before the website's broad conference routes.
func Register(root *mux.Router, app *config.AppContext) {
	s := &server{
		app: app, source: postgresSource{app: app}, now: time.Now,
		authenticateToken: func(raw string) (*auth.BearerGrant, error) {
			return auth.AuthenticateBearerToken(app, raw)
		},
		loadPerson: func(personID string) (*types.Speaker, error) {
			return getters.FetchSpeakerByID(app, personID)
		},
		listPersonEmails: func(personID string) ([]*types.PersonEmail, error) {
			return getters.ListPersonEmails(app, personID)
		},
		listPersonConfs: func(personID string) ([]*types.SpeakerConf, error) {
			_, confs, err := getters.GetSpeakerConfsByPersonID(app, personID)
			return confs, err
		},
		updateProfile: func(personID string, patch getters.SpeakerProfilePatch) error {
			return getters.UpdateSpeakerProfile(app, personID, patch)
		},
		loadConfTalk:   func(id string) (*types.ConfTalk, error) { return getters.GetConfTalkByID(app, id) },
		loadTalk:       func(id string) (*types.Talk, error) { return getters.LoadTalkFromConfTalk(app, id) },
		updateProposal: func(id string, patch getters.ProposalPatch) error { return getters.UpdateProposalPatch(app, id, patch) },
		updateTalkResources: func(id, repository, slides, objectKey string) error {
			return getters.UpdateConfTalkResources(app, id, repository, slides, objectKey)
		},
		updateTalkSchedule: func(id, venue string, start, end time.Time) error {
			return getters.UpdateConfTalkSchedule(app, id, venue, start, end)
		},
		listConfRecordings: func(tag string) ([]*types.Recording, error) { return getters.ListRecordingsForConf(app, tag) },
		upsertRecording: func(id string, update getters.RecordingUpsert) (*types.Recording, error) {
			return getters.UpsertRecordingForConfTalk(app, id, update)
		},
		recordAudit: func(event *types.AuthAuditEvent) error { return getters.RecordAuthAuditEvent(app, event) },
		limiter:     newRateLimiter(),
	}
	s.register(root.PathPrefix("/api/v1").Subrouter())
}

func (s *server) register(r *mux.Router) {
	r.Use(s.middleware)
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		s.writeError(w, req, http.StatusNotFound, "not_found", "API resource not found.")
	})
	r.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		s.writeError(w, req, http.StatusMethodNotAllowed, "method_not_allowed", "That method is not supported for this resource.")
	})

	r.HandleFunc("/bootstrap", s.bootstrap).Methods(http.MethodGet)
	r.HandleFunc("/conferences", s.conferences).Methods(http.MethodGet)
	r.HandleFunc("/conferences/{tag}", s.conference).Methods(http.MethodGet)
	r.HandleFunc("/conferences/{tag}/days", s.conferenceDays).Methods(http.MethodGet)
	r.HandleFunc("/conferences/{tag}/agenda", s.conferenceAgenda).Methods(http.MethodGet)
	r.HandleFunc("/conferences/{tag}/talks/{talkID}", s.conferenceTalk).Methods(http.MethodGet)
	r.HandleFunc("/conferences/{tag}/talks/{talkID}", s.patchConferenceTalk).Methods(http.MethodPatch)
	r.HandleFunc("/conferences/{tag}/talks/{talkID}/schedule", s.updateConferenceTalkSchedule).Methods(http.MethodPut)
	r.HandleFunc("/conferences/{tag}/recording-candidates", s.recordingCandidates).Methods(http.MethodGet)
	r.HandleFunc("/conferences/{tag}/talks/{talkID}/recording", s.putTalkRecording).Methods(http.MethodPut)
	r.HandleFunc("/conferences/{tag}/speakers", s.conferenceSpeakers).Methods(http.MethodGet)
	r.HandleFunc("/people", s.people).Methods(http.MethodGet)
	r.HandleFunc("/people/{personID}", s.person).Methods(http.MethodGet)
	r.HandleFunc("/me", s.me).Methods(http.MethodGet)
	r.HandleFunc("/me", s.patchMe).Methods(http.MethodPatch)
	r.HandleFunc("/me/talks", s.myTalks).Methods(http.MethodGet)
	r.HandleFunc("/conferences/{tag}/sponsors", s.conferenceSponsors).Methods(http.MethodGet)
	r.HandleFunc("/organizations/{organizationID}", s.organization).Methods(http.MethodGet)
	r.HandleFunc("/recordings", s.recordings).Methods(http.MethodGet)
	r.HandleFunc("/recordings/{recordingID}", s.recording).Methods(http.MethodGet)
	r.HandleFunc("/conferences/{tag}/hackathons", s.conferenceHackathons).Methods(http.MethodGet)
	r.HandleFunc("/hackathons/{competitionID}", s.hackathon).Methods(http.MethodGet)
	r.HandleFunc("/hackathons/{competitionID}/projects", s.hackathonProjects).Methods(http.MethodGet)
	r.HandleFunc("/hackathons/{competitionID}/projects/{projectID}", s.hackathonProject).Methods(http.MethodGet)
	r.HandleFunc("/hackathons/{competitionID}/awards", s.hackathonAwards).Methods(http.MethodGet)
	r.HandleFunc("/hackathons/{competitionID}/results", s.hackathonResults).Methods(http.MethodGet)
}

func (s *server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := requestid.From(r.Context())
		if id == "" {
			id = requestid.New()
			r = r.WithContext(requestid.With(r.Context(), id))
		}
		w.Header().Set("X-Request-ID", id)
		if s.limiter != nil {
			if allowed, retry := s.limiter.allow("public:"+remoteIP(r), 150, 2); !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(retry))
				s.writeError(w, r, http.StatusTooManyRequests, "rate_limited", "Too many API requests. Try again shortly.")
				return
			}
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				if s.app != nil && s.app.Err != nil {
					s.app.Err.Printf("api panic request_id=%s: %v", id, recovered)
				}
				s.writeError(w, r, http.StatusInternalServerError, "internal_error", "The API could not complete that request.")
			}
		}()
		if !acceptsJSON(r.Header.Get("Accept")) {
			s.writeError(w, r, http.StatusNotAcceptable, "not_acceptable", "This API serves application/json.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) requireScope(w http.ResponseWriter, r *http.Request, scope string) (*apiPrincipal, *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	raw, ok := bearerCredential(r.Header.Get("Authorization"))
	if !ok || s.authenticateToken == nil || s.loadPerson == nil {
		s.writeAuthError(w, r, http.StatusUnauthorized, "invalid_token", "A valid API bearer token is required.", scope)
		return nil, r
	}
	grant, err := s.authenticateToken(raw)
	if err != nil {
		s.internalError(w, r, "authenticate bearer token", err)
		return nil, r
	}
	if grant == nil {
		s.writeAuthError(w, r, http.StatusUnauthorized, "invalid_token", "The API bearer token is invalid or expired.", scope)
		return nil, r
	}
	if s.limiter != nil {
		if allowed, retry := s.limiter.allow("token-read:"+bearerRateKey(raw), 700, 10); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			s.writeError(w, r, http.StatusTooManyRequests, "rate_limited", "This API token has exceeded its request limit.")
			return nil, r
		}
	}
	if !grantHasScope(grant, scope) {
		s.writeAuthError(w, r, http.StatusForbidden, "insufficient_scope", "The API token does not grant the required scope.", scope)
		return nil, r
	}
	person, err := s.loadPerson(grant.PersonID)
	if err != nil {
		s.internalError(w, r, "load API token owner", err)
		return nil, r
	}
	if person == nil {
		s.writeAuthError(w, r, http.StatusUnauthorized, "invalid_token", "The API token owner no longer exists.", scope)
		return nil, r
	}
	principal := &apiPrincipal{
		Grant: grant, Person: person,
		Identity: &auth.Identity{PersonID: person.ID, Speaker: person, Roles: auth.ParseRoles(person.Roles)},
	}
	r = r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal))
	return principal, r
}

func bearerCredential(header string) (string, bool) {
	parts := strings.Fields(strings.TrimSpace(header))
	returnValue := ""
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		returnValue = strings.TrimSpace(parts[1])
	}
	return returnValue, returnValue != ""
}

func grantHasScope(grant *auth.BearerGrant, wanted string) bool {
	if grant == nil {
		return false
	}
	for _, scope := range grant.Scopes {
		if scope == wanted || (scope == "profile:read" && wanted == "profile:self:read") {
			return true
		}
	}
	return false
}

func (s *server) writeAuthError(w http.ResponseWriter, r *http.Request, status int, code, message, scope string) {
	challenge := `Bearer realm="btcpp-api"`
	if code != "" {
		challenge += `, error="` + code + `"`
	}
	if scope != "" {
		challenge += `, scope="` + scope + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	s.writeError(w, r, status, code, message)
}

func (s *server) bootstrap(w http.ResponseWriter, r *http.Request) {
	s.writePublic(w, r, http.StatusOK, bootstrapDTO{
		APIVersion: "v1",
		Links: map[string]string{
			"bootstrap":             "/api/v1/bootstrap",
			"conferences":           "/api/v1/conferences",
			"people":                "/api/v1/people",
			"recordings":            "/api/v1/recordings",
			"me":                    "/api/v1/me",
			"oauth_server_metadata": "/.well-known/oauth-authorization-server",
		},
	})
}

func (s *server) conferences(w http.ResponseWriter, r *http.Request) {
	confs, err := s.source.ListConferences()
	if err != nil {
		s.internalError(w, r, "list conferences", err)
		return
	}
	data := make([]conferenceDTO, 0, len(confs))
	for _, conf := range confs {
		if conf != nil && conf.IsPublished() {
			data = append(data, conferenceFromDomain(conf))
		}
	}
	writePublicCollection(s, w, r, data)
}

func (s *server) conference(w http.ResponseWriter, r *http.Request) {
	conf, ok := s.publicConference(w, r)
	if !ok {
		return
	}
	s.writePublic(w, r, http.StatusOK, conferenceFromDomain(conf))
}

func (s *server) conferenceDays(w http.ResponseWriter, r *http.Request) {
	conf, ok := s.publicConference(w, r)
	if !ok {
		return
	}
	days, err := s.source.ListConferenceDays(conf.Tag)
	if err != nil {
		s.internalError(w, r, "list conference days", err)
		return
	}
	s.writePublic(w, r, http.StatusOK, daysFromDomain(days))
}

func (s *server) conferenceAgenda(w http.ResponseWriter, r *http.Request) {
	conf, ok := s.publicConference(w, r)
	if !ok {
		return
	}
	days, err := s.source.ListConferenceDays(conf.Tag)
	if err != nil {
		s.internalError(w, r, "list conference agenda days", err)
		return
	}
	talks, err := s.source.ListConferenceTalks(conf.Tag)
	if err != nil {
		s.internalError(w, r, "list conference agenda talks", err)
		return
	}
	s.writePublic(w, r, http.StatusOK, agendaDTO{
		Conference: conferenceFromDomain(conf),
		Days:       daysFromDomain(days),
		Talks:      publicTalksFromDomain(conf, talks, s.now()),
	})
}

func (s *server) conferenceTalk(w http.ResponseWriter, r *http.Request) {
	conf, ok := s.publicConference(w, r)
	if !ok {
		return
	}
	talks, err := s.source.ListConferenceTalks(conf.Tag)
	if err != nil {
		s.internalError(w, r, "list conference talks", err)
		return
	}
	talkID := strings.TrimSpace(mux.Vars(r)["talkID"])
	for _, talk := range talks {
		if talk != nil && talk.ID == talkID && isPublicAgendaTalk(conf, talk, s.now()) {
			s.writePublic(w, r, http.StatusOK, talkFromDomain(talk))
			return
		}
	}
	s.writeError(w, r, http.StatusNotFound, "not_found", "Talk not found.")
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	principal, r := s.requireScope(w, r, "profile:self:read")
	if principal == nil {
		return
	}
	profile, ok := s.accountProfile(w, r, principal.Person)
	if !ok {
		return
	}
	s.writePrivate(w, r, http.StatusOK, profile)
}

func (s *server) patchMe(w http.ResponseWriter, r *http.Request) {
	principal, r := s.requireScope(w, r, "profile:self:write")
	if principal == nil {
		return
	}
	if !s.requireMutationLimit(w, r, principal, false) {
		return
	}
	if s.updateProfile == nil {
		s.internalError(w, r, "update API profile", io.ErrClosedPipe)
		return
	}
	var input profilePatchDTO
	if !s.decodeJSON(w, r, &input) {
		return
	}
	if input.Name != nil && strings.TrimSpace(*input.Name) == "" {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "Name cannot be empty.")
		return
	}
	if !validOptionalHTTPURL(input.Website) || !validOptionalHTTPURL(input.GitHub) {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "Website and GitHub values must be absolute http or https URLs, or empty to clear them.")
		return
	}
	err := s.updateProfile(principal.Person.ID, getters.SpeakerProfilePatch{
		Name: input.Name, Company: input.Company, Bio: input.Biography,
		Website: input.Website, Twitter: input.X, Github: input.GitHub,
		Instagram: input.Instagram, LinkedIn: input.LinkedIn, LeetCode: input.LeetCode,
	})
	if err != nil {
		s.internalError(w, r, "update API profile", err)
		return
	}
	s.auditMutation(r, principal, "api_profile_updated", nil)
	person, err := s.loadPerson(principal.Person.ID)
	if err != nil || person == nil {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		s.internalError(w, r, "reload API profile", err)
		return
	}
	profile, ok := s.accountProfile(w, r, person)
	if !ok {
		return
	}
	s.writePrivate(w, r, http.StatusOK, profile)
}

func (s *server) accountProfile(w http.ResponseWriter, r *http.Request, person *types.Speaker) (accountProfileDTO, bool) {
	if person == nil || s.listPersonEmails == nil {
		s.internalError(w, r, "load API profile", io.ErrClosedPipe)
		return accountProfileDTO{}, false
	}
	emails, err := s.listPersonEmails(person.ID)
	if err != nil {
		s.internalError(w, r, "list API profile emails", err)
		return accountProfileDTO{}, false
	}
	addresses := make([]accountEmailDTO, 0, len(emails))
	for _, address := range emails {
		if address != nil {
			addresses = append(addresses, accountEmailDTO{
				Email: address.Email, IsPrimary: address.IsPrimary,
				VerifiedAt: address.VerifiedAt.UTC().Format(time.RFC3339),
			})
		}
	}
	roles := append([]string(nil), person.Roles...)
	sort.Strings(roles)
	return accountProfileDTO{
		ID: person.ID, Name: person.Name, Photo: person.Photo, Company: person.Company,
		Biography: person.Bio, Website: person.Website, X: person.Twitter.Handle,
		GitHub: person.Github, Instagram: person.Instagram, LinkedIn: person.LinkedIn,
		LeetCode: person.LeetCode, Nostr: person.Nostr, Emails: addresses, Roles: roles,
	}, true
}

func (s *server) myTalks(w http.ResponseWriter, r *http.Request) {
	principal, r := s.requireScope(w, r, "talks:read")
	if principal == nil {
		return
	}
	if s.listPersonConfs == nil {
		s.internalError(w, r, "list API talks", io.ErrClosedPipe)
		return
	}
	confs, err := s.listPersonConfs(principal.Person.ID)
	if err != nil {
		s.internalError(w, r, "list API talks", err)
		return
	}
	seen := make(map[string]bool)
	talks := make([]accountTalkDTO, 0)
	for _, speakerConf := range confs {
		if speakerConf == nil {
			continue
		}
		for _, proposal := range speakerConf.Proposals {
			if proposal == nil || seen[proposal.ID] {
				continue
			}
			seen[proposal.ID] = true
			conferenceTag := ""
			if proposal.ScheduleFor != nil {
				conferenceTag = proposal.ScheduleFor.Tag
			}
			speakerIDs := make([]string, 0, len(proposal.Speakers))
			for _, linked := range proposal.Speakers {
				if linked != nil && linked.Speaker != nil {
					speakerIDs = append(speakerIDs, linked.Speaker.ID)
				}
			}
			talks = append(talks, accountTalkDTO{
				ID: proposal.ID, ConferenceTag: conferenceTag, Title: proposal.Title,
				Description: proposal.Description, Kind: proposal.TalkType, Status: proposal.Status,
				DesiredDuration: proposal.DesiredDuration, SpeakerPersonIDs: speakerIDs,
			})
		}
	}
	sort.Slice(talks, func(i, j int) bool {
		if talks[i].ConferenceTag == talks[j].ConferenceTag {
			return talks[i].Title < talks[j].Title
		}
		return talks[i].ConferenceTag < talks[j].ConferenceTag
	})
	s.writePrivate(w, r, http.StatusOK, talks)
}

func (s *server) patchConferenceTalk(w http.ResponseWriter, r *http.Request) {
	principal, r := s.requireScope(w, r, "talks:write")
	if principal == nil {
		return
	}
	if !s.requireMutationLimit(w, r, principal, false) {
		return
	}
	confTalk, talk, ok := s.authorizedTalk(w, r, principal, true)
	if !ok {
		return
	}
	var input talkPatchDTO
	if !s.decodeJSON(w, r, &input) {
		return
	}
	if input.Title != nil && strings.TrimSpace(*input.Title) == "" {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "Title cannot be empty.")
		return
	}
	if input.Kind != nil && strings.TrimSpace(*input.Kind) == "" {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "Kind cannot be empty.")
		return
	}
	if input.DesiredDurationMinutes != nil && (*input.DesiredDurationMinutes <= 0 || *input.DesiredDurationMinutes > 480) {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "Desired duration must be between 1 and 480 minutes.")
		return
	}
	if !validOptionalHTTPURL(input.RepositoryURL) || !validOptionalHTTPURL(input.SlidesURL) {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "Repository and slides values must be absolute http or https URLs, or empty to clear them.")
		return
	}
	if confTalk.Proposal == nil || s.updateProposal == nil || s.updateTalkResources == nil {
		s.internalError(w, r, "resolve API talk proposal", io.ErrUnexpectedEOF)
		return
	}
	if err := s.updateProposal(confTalk.Proposal.ID, getters.ProposalPatch{
		Title: input.Title, Description: input.Description, TalkType: input.Kind,
		DesiredDuration: input.DesiredDurationMinutes,
	}); err != nil {
		s.internalError(w, r, "update API talk", err)
		return
	}
	if input.RepositoryURL != nil || input.SlidesURL != nil {
		repository, slides := talk.GithubRepoURL, talk.SlidesURL
		if input.RepositoryURL != nil {
			repository = strings.TrimSpace(*input.RepositoryURL)
		}
		if input.SlidesURL != nil {
			slides = strings.TrimSpace(*input.SlidesURL)
		}
		if err := s.updateTalkResources(confTalk.ID, repository, slides, confTalk.SlidesObjectKey); err != nil {
			s.internalError(w, r, "update API talk resources", err)
			return
		}
	}
	s.auditMutation(r, principal, "api_talk_updated", map[string]any{"conference": mux.Vars(r)["tag"], "talk_id": confTalk.ID})
	updated, err := s.loadTalk(confTalk.ID)
	if err != nil || updated == nil {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		s.internalError(w, r, "reload API talk", err)
		return
	}
	s.writePrivate(w, r, http.StatusOK, talkFromDomain(updated))
}

func (s *server) updateConferenceTalkSchedule(w http.ResponseWriter, r *http.Request) {
	principal, r := s.requireScope(w, r, "schedule:write")
	if principal == nil {
		return
	}
	if !s.requireMutationLimit(w, r, principal, false) {
		return
	}
	confTalk, talk, ok := s.authorizedTalk(w, r, principal, false)
	if !ok {
		return
	}
	var input scheduleUpdateDTO
	if !s.decodeJSON(w, r, &input) {
		return
	}
	start, startErr := time.Parse(time.RFC3339, strings.TrimSpace(input.StartsAt))
	end, endErr := time.Parse(time.RFC3339, strings.TrimSpace(input.EndsAt))
	if startErr != nil || endErr != nil || !end.After(start) || strings.TrimSpace(input.Venue) == "" {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "starts_at and ends_at must be RFC3339 timestamps, ends_at must follow starts_at, and venue is required.")
		return
	}
	if talk.Status != "Accepted" && talk.Status != "Scheduled" {
		s.writeError(w, r, http.StatusConflict, "conflict", "Only accepted or scheduled talks can be placed on the schedule.")
		return
	}
	if conflict := s.validateSchedulePlacement(w, r, confTalk, talk, strings.TrimSpace(input.Venue), start, end); conflict {
		return
	}
	if s.updateTalkSchedule == nil {
		s.internalError(w, r, "update API schedule", io.ErrClosedPipe)
		return
	}
	if err := s.updateTalkSchedule(confTalk.ID, strings.TrimSpace(input.Venue), start, end); err != nil {
		s.internalError(w, r, "update API schedule", err)
		return
	}
	s.auditMutation(r, principal, "api_schedule_updated", map[string]any{"conference": mux.Vars(r)["tag"], "talk_id": confTalk.ID})
	updated, err := s.loadTalk(confTalk.ID)
	if err != nil || updated == nil {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		s.internalError(w, r, "reload API scheduled talk", err)
		return
	}
	s.writePrivate(w, r, http.StatusOK, talkFromDomain(updated))
}

func (s *server) validateSchedulePlacement(w http.ResponseWriter, r *http.Request, confTalk *types.ConfTalk, talk *types.Talk, venue string, start, end time.Time) bool {
	conf := confTalk.Conf
	location := conf.Loc()
	conferenceStart := time.Date(conf.StartDate.In(location).Year(), conf.StartDate.In(location).Month(), conf.StartDate.In(location).Day(), 0, 0, 0, 0, location)
	conferenceEndDate := conf.EndDate.In(location)
	conferenceEnd := time.Date(conferenceEndDate.Year(), conferenceEndDate.Month(), conferenceEndDate.Day(), 0, 0, 0, 0, location).Add(24 * time.Hour)
	if start.Before(conferenceStart) || end.After(conferenceEnd) {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "The schedule time must fall within the conference dates.")
		return true
	}
	days, err := s.source.ListConferenceDays(conf.Tag)
	if err != nil {
		s.internalError(w, r, "validate API schedule venue", err)
		return true
	}
	configuredVenues := map[string]bool{}
	for _, day := range days {
		if day != nil {
			for _, configured := range day.Venues {
				configuredVenues[strings.TrimSpace(configured)] = true
			}
		}
	}
	if len(configuredVenues) > 0 && !configuredVenues[venue] {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "Venue is not configured for this conference.")
		return true
	}
	talks, err := s.source.ListConferenceTalks(conf.Tag)
	if err != nil {
		s.internalError(w, r, "validate API schedule conflicts", err)
		return true
	}
	speakerIDs := map[string]bool{}
	for _, speaker := range talk.Speakers {
		if speaker != nil {
			speakerIDs[speaker.ID] = true
		}
	}
	for _, existing := range talks {
		if existing == nil || existing.ID == confTalk.ID || existing.Sched == nil || existing.Sched.End == nil {
			continue
		}
		if !start.Before(*existing.Sched.End) || !existing.Sched.Start.Before(end) {
			continue
		}
		if existing.Venue == venue {
			s.writeError(w, r, http.StatusConflict, "schedule_conflict", "Another talk already occupies that venue and time.")
			return true
		}
		for _, speaker := range existing.Speakers {
			if speaker != nil && speakerIDs[speaker.ID] {
				s.writeError(w, r, http.StatusConflict, "schedule_conflict", "A speaker is already scheduled for another talk at that time.")
				return true
			}
		}
	}
	return false
}

func (s *server) recordingCandidates(w http.ResponseWriter, r *http.Request) {
	principal, r := s.requireScope(w, r, "recordings:write")
	if principal == nil {
		return
	}
	conf, ok := s.adminConference(w, r, principal)
	if !ok {
		return
	}
	talks, err := s.source.ListConferenceTalks(conf.Tag)
	if err != nil {
		s.internalError(w, r, "list recording candidate talks", err)
		return
	}
	if s.listConfRecordings == nil {
		s.internalError(w, r, "list conference recordings", io.ErrClosedPipe)
		return
	}
	recordings, err := s.listConfRecordings(conf.Tag)
	if err != nil {
		s.internalError(w, r, "list conference recordings", err)
		return
	}
	byTalk := make(map[string]*types.Recording, len(recordings))
	for _, recording := range recordings {
		if recording != nil {
			byTalk[recording.ConfTalkID] = recording
		}
	}
	data := make([]recordingCandidateDTO, 0, len(talks))
	for _, talk := range talks {
		if talk == nil || (talk.Status != "Accepted" && talk.Status != "Scheduled") {
			continue
		}
		data = append(data, recordingCandidateFromDomain(talk, byTalk[talk.ID]))
	}
	s.writePrivate(w, r, http.StatusOK, data)
}

func (s *server) putTalkRecording(w http.ResponseWriter, r *http.Request) {
	principal, r := s.requireScope(w, r, "recordings:write")
	if principal == nil {
		return
	}
	if !s.requireMutationLimit(w, r, principal, true) {
		return
	}
	confTalk, talk, ok := s.authorizedTalk(w, r, principal, false)
	if !ok {
		return
	}
	var input recordingUpsertDTO
	if !s.decodeJSON(w, r, &input) {
		return
	}
	var publishAt *time.Time
	setPublishAt := input.PublishedAt != nil
	if input.PublishedAt != nil && strings.TrimSpace(*input.PublishedAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*input.PublishedAt))
		if err != nil {
			s.writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "published_at must be an RFC3339 timestamp or an empty string.")
			return
		}
		publishAt = &parsed
	}
	if !validOptionalHTTPURL(input.YouTubeURL) || !validOptionalHTTPURL(input.XURL) || !validOptionalHTTPURL(input.XReplyURL) {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "Published recording links must be absolute http or https URLs, or empty to clear them.")
		return
	}
	if input.FileURI != nil && !validRecordingObjectKey(*input.FileURI) {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "file_uri must be an object key without a URL scheme, leading slash, or parent traversal.")
		return
	}
	if s.upsertRecording == nil {
		s.internalError(w, r, "upsert API recording", io.ErrClosedPipe)
		return
	}
	talkName := input.TalkName
	if talkName == nil {
		name := talk.Name
		talkName = &name
	}
	recording, err := s.upsertRecording(confTalk.ID, getters.RecordingUpsert{
		TalkName: talkName, FileURI: input.FileURI, YTLink: input.YouTubeURL,
		XLink: input.XURL, XReplyLink: input.XReplyURL,
		PublishAt: publishAt, SetPublishAt: setPublishAt,
	})
	if err != nil {
		s.internalError(w, r, "upsert API recording", err)
		return
	}
	s.auditMutation(r, principal, "api_recording_upserted", map[string]any{"conference": mux.Vars(r)["tag"], "talk_id": confTalk.ID, "recording_id": recording.ID})
	s.writePrivate(w, r, http.StatusOK, recordingAdminFromDomain(recording))
}

func (s *server) auditMutation(r *http.Request, principal *apiPrincipal, event string, metadata map[string]any) {
	if s.recordAudit == nil || principal == nil || principal.Person == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	if principal.Grant != nil {
		metadata["api_token_id"] = principal.Grant.TokenID
		metadata["credential_kind"] = principal.Grant.Kind
		if principal.Grant.ClientID != "" {
			metadata["oauth_client_id"] = principal.Grant.ClientID
		}
	}
	err := s.recordAudit(&types.AuthAuditEvent{
		PersonID: principal.Person.ID, Method: "api_token", Event: event,
		RemoteAddress: r.RemoteAddr, UserAgent: r.UserAgent(), Metadata: metadata,
	})
	if err != nil && s.app != nil && s.app.Err != nil {
		s.app.Err.Printf("record API audit event %s: %v", event, err)
	}
}

func (s *server) requireMutationLimit(w http.ResponseWriter, r *http.Request, principal *apiPrincipal, recording bool) bool {
	if s.limiter == nil || principal == nil || principal.Grant == nil {
		return true
	}
	capacity, refill, category := 60.0, 1.0, "mutation"
	if recording {
		capacity, refill, category = 10, 10.0/60.0, "recording"
	}
	if allowed, retry := s.limiter.allow(category+":"+principal.Grant.TokenID, capacity, refill); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		s.writeError(w, r, http.StatusTooManyRequests, "rate_limited", "This API token has exceeded its mutation limit.")
		return false
	}
	if recording {
		if allowed, retry := s.limiter.allow("recording-person:"+principal.Person.ID, 30, 30.0/3600.0); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			s.writeError(w, r, http.StatusTooManyRequests, "rate_limited", "This account has exceeded its hourly recording-write limit.")
			return false
		}
	}
	return true
}

func (s *server) authorizedTalk(w http.ResponseWriter, r *http.Request, principal *apiPrincipal, allowSpeaker bool) (*types.ConfTalk, *types.Talk, bool) {
	if s.loadConfTalk == nil || s.loadTalk == nil {
		s.internalError(w, r, "load API talk", io.ErrClosedPipe)
		return nil, nil, false
	}
	tag, talkID := strings.TrimSpace(mux.Vars(r)["tag"]), strings.TrimSpace(mux.Vars(r)["talkID"])
	confTalk, err := s.loadConfTalk(talkID)
	if err != nil {
		s.internalError(w, r, "load API conference talk", err)
		return nil, nil, false
	}
	if confTalk == nil || confTalk.Conf == nil || confTalk.Conf.Tag != tag {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Talk not found.")
		return nil, nil, false
	}
	talk, err := s.loadTalk(talkID)
	if err != nil || talk == nil {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		s.internalError(w, r, "load API talk speakers", err)
		return nil, nil, false
	}
	allowed := principal != nil && principal.Identity != nil && principal.Identity.HasRoleForConf(tag, auth.RoleAdmin)
	if allowSpeaker && !allowed && principal != nil && principal.Person != nil {
		for _, speaker := range talk.Speakers {
			if speaker != nil && speaker.ID == principal.Person.ID {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		s.writeError(w, r, http.StatusForbidden, "forbidden", "This account cannot modify that talk.")
		return nil, nil, false
	}
	return confTalk, talk, true
}

func (s *server) adminConference(w http.ResponseWriter, r *http.Request, principal *apiPrincipal) (*types.Conf, bool) {
	tag := strings.TrimSpace(mux.Vars(r)["tag"])
	conf, err := s.source.GetConference(tag)
	if err != nil {
		s.internalError(w, r, "load admin API conference", err)
		return nil, false
	}
	if conf == nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Conference not found.")
		return nil, false
	}
	if principal == nil || principal.Identity == nil || !principal.Identity.HasRoleForConf(tag, auth.RoleAdmin) {
		s.writeError(w, r, http.StatusForbidden, "forbidden", "Conference admin access is required.")
		return nil, false
	}
	return conf, true
}

func (s *server) people(w http.ResponseWriter, r *http.Request) {
	profiles, publicIDs, ok := s.publicProfiles(w, r)
	if !ok {
		return
	}
	data := make([]personSummaryDTO, 0, len(profiles))
	for _, profile := range profiles {
		if profile != nil && profile.Speaker != nil {
			data = append(data, personSummaryFromDomain(profile.Speaker, publicIDs[profile.Speaker.ID]))
		}
	}
	writePublicCollection(s, w, r, data)
}

func (s *server) person(w http.ResponseWriter, r *http.Request) {
	profiles, publicIDs, ok := s.publicProfiles(w, r)
	if !ok {
		return
	}
	personID := strings.TrimSpace(mux.Vars(r)["personID"])
	for _, profile := range profiles {
		if profile != nil && profile.Speaker != nil && profile.Speaker.ID == personID {
			s.writePublic(w, r, http.StatusOK, personFromDomain(profile, publicIDs))
			return
		}
	}
	s.writeError(w, r, http.StatusNotFound, "not_found", "Person not found.")
}

func (s *server) conferenceSpeakers(w http.ResponseWriter, r *http.Request) {
	conf, ok := s.publicConference(w, r)
	if !ok {
		return
	}
	profiles, publicIDs, ok := s.publicProfiles(w, r)
	if !ok {
		return
	}
	data := make([]personSummaryDTO, 0)
	for _, profile := range profiles {
		if profile == nil || profile.Speaker == nil || !profileHasTalkAtConference(profile, conf.Tag) {
			continue
		}
		data = append(data, personSummaryFromDomain(profile.Speaker, publicIDs[profile.Speaker.ID]))
	}
	writePublicCollection(s, w, r, data)
}

func (s *server) publicProfiles(w http.ResponseWriter, r *http.Request) ([]*getters.PublicProfile, map[string]string, bool) {
	profiles, err := s.source.ListPublicProfiles()
	if err != nil {
		s.internalError(w, r, "list public people", err)
		return nil, nil, false
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i] == nil || profiles[i].Speaker == nil {
			return false
		}
		if profiles[j] == nil || profiles[j].Speaker == nil {
			return true
		}
		a, b := strings.ToLower(profiles[i].Speaker.Name), strings.ToLower(profiles[j].Speaker.Name)
		if a == b {
			return profiles[i].Speaker.ID < profiles[j].Speaker.ID
		}
		return a < b
	})
	speakers := make([]*types.Speaker, 0, len(profiles))
	for _, profile := range profiles {
		if profile != nil && profile.Speaker != nil {
			speakers = append(speakers, profile.Speaker)
		}
	}
	return profiles, publicid.AssignSpeakers(speakers), true
}

func (s *server) conferenceSponsors(w http.ResponseWriter, r *http.Request) {
	conf, ok := s.publicConference(w, r)
	if !ok {
		return
	}
	sponsorships, err := s.source.ListSponsorships(conf.Ref)
	if err != nil {
		s.internalError(w, r, "list conference sponsors", err)
		return
	}
	data := make([]sponsorDTO, 0, len(sponsorships))
	for _, sponsorship := range sponsorships {
		if !isPublicSponsorship(sponsorship) {
			continue
		}
		data = append(data, sponsorFromDomain(sponsorship))
	}
	writePublicCollection(s, w, r, data)
}

func (s *server) organization(w http.ResponseWriter, r *http.Request) {
	organizationID := strings.TrimSpace(mux.Vars(r)["organizationID"])
	visible, err := s.publicOrganizationIDs()
	if err != nil {
		s.internalError(w, r, "resolve public organizations", err)
		return
	}
	if !visible[organizationID] {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Organization not found.")
		return
	}
	organizations, err := s.source.ListOrganizations()
	if err != nil {
		s.internalError(w, r, "list organizations", err)
		return
	}
	for _, organization := range organizations {
		if organization != nil && organization.Ref == organizationID {
			s.writePublic(w, r, http.StatusOK, organizationFromDomain(organization))
			return
		}
	}
	s.writeError(w, r, http.StatusNotFound, "not_found", "Organization not found.")
}

func (s *server) recordings(w http.ResponseWriter, r *http.Request) {
	data, ok := s.publicRecordings(w, r)
	if ok {
		writePublicCollection(s, w, r, data)
	}
}

func (s *server) recording(w http.ResponseWriter, r *http.Request) {
	data, ok := s.publicRecordings(w, r)
	if !ok {
		return
	}
	recordingID := strings.TrimSpace(mux.Vars(r)["recordingID"])
	for _, item := range data {
		if item.ID == recordingID {
			s.writePublic(w, r, http.StatusOK, item)
			return
		}
	}
	s.writeError(w, r, http.StatusNotFound, "not_found", "Recording not found.")
}

func (s *server) conferenceHackathons(w http.ResponseWriter, r *http.Request) {
	conf, ok := s.publicConference(w, r)
	if !ok {
		return
	}
	competitions, err := s.source.ListCompetitions()
	if err != nil {
		s.internalError(w, r, "list conference hackathons", err)
		return
	}
	data := make([]hackathonDTO, 0)
	for _, competition := range competitions {
		if competition != nil && competition.ConferenceID == conf.Ref && competition.Visibility == getters.CompetitionVisibilityPublic {
			data = append(data, hackathonFromDomain(competition, conf))
		}
	}
	writePublicCollection(s, w, r, data)
}

func (s *server) hackathon(w http.ResponseWriter, r *http.Request) {
	competition, conf, ok := s.publicCompetition(w, r)
	if ok {
		s.writePublic(w, r, http.StatusOK, hackathonFromDomain(competition, conf))
	}
}

func (s *server) hackathonProjects(w http.ResponseWriter, r *http.Request) {
	competition, _, ok := s.publicCompetition(w, r)
	if !ok {
		return
	}
	projects, members, publicIDs, ok := s.publicCompetitionProjects(w, r, competition)
	if !ok {
		return
	}
	data := make([]hackathonProjectDTO, 0, len(projects))
	for _, project := range projects {
		if project != nil {
			data = append(data, hackathonProjectFromDomain(project, members[project.ID], publicIDs))
		}
	}
	writePublicCollection(s, w, r, data)
}

func (s *server) hackathonProject(w http.ResponseWriter, r *http.Request) {
	competition, _, ok := s.publicCompetition(w, r)
	if !ok {
		return
	}
	projects, members, publicIDs, ok := s.publicCompetitionProjects(w, r, competition)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(mux.Vars(r)["projectID"])
	for _, project := range projects {
		if project != nil && project.ID == projectID {
			s.writePublic(w, r, http.StatusOK, hackathonProjectFromDomain(project, members[project.ID], publicIDs))
			return
		}
	}
	s.writeError(w, r, http.StatusNotFound, "not_found", "Hackathon project not found.")
}

func (s *server) hackathonAwards(w http.ResponseWriter, r *http.Request) {
	competition, _, ok := s.publicCompetition(w, r)
	if !ok {
		return
	}
	data, ok := s.publicAwards(w, r, competition.ID)
	if ok {
		writePublicCollection(s, w, r, data)
	}
}

func (s *server) hackathonResults(w http.ResponseWriter, r *http.Request) {
	competition, _, ok := s.publicCompetition(w, r)
	if !ok {
		return
	}
	if competition.ResultsFinalizedAt == nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Hackathon results not found.")
		return
	}
	awards, ok := s.publicAwards(w, r, competition.ID)
	if !ok {
		return
	}
	projects, members, publicIDs, ok := s.publicCompetitionProjects(w, r, competition)
	if !ok {
		return
	}
	projectAwards, err := s.source.ListProjectAwards(competition.ID)
	if err != nil {
		s.internalError(w, r, "list hackathon results", err)
		return
	}
	awardsByID := make(map[string]awardDTO, len(awards))
	for _, award := range awards {
		awardsByID[award.ID] = award
	}
	projectsByID := make(map[string]*types.HackathonProject, len(projects))
	for _, project := range projects {
		if project != nil {
			projectsByID[project.ID] = project
		}
	}
	data := make([]resultDTO, 0, len(projectAwards))
	for _, assignment := range projectAwards {
		if assignment == nil {
			continue
		}
		award, awardOK := awardsByID[assignment.AwardID]
		project := projectsByID[assignment.ProjectID]
		if !awardOK || project == nil {
			continue
		}
		data = append(data, resultDTO{Award: award, Project: hackathonProjectFromDomain(project, members[project.ID], publicIDs)})
	}
	writePublicCollection(s, w, r, data)
}

func (s *server) publicConference(w http.ResponseWriter, r *http.Request) (*types.Conf, bool) {
	tag := strings.TrimSpace(mux.Vars(r)["tag"])
	if tag == "" {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Conference not found.")
		return nil, false
	}
	conf, err := s.source.GetConference(tag)
	if err != nil {
		s.internalError(w, r, "get conference", err)
		return nil, false
	}
	if conf == nil || !conf.IsPublished() {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Conference not found.")
		return nil, false
	}
	return conf, true
}

func (s *server) writePublic(w http.ResponseWriter, r *http.Request, status int, data any) {
	s.writePublicWithMeta(w, r, status, data, responseMeta{})
}

func (s *server) writePublicWithMeta(w http.ResponseWriter, r *http.Request, status int, data any, meta responseMeta) {
	id := responseRequestID(w, r)
	meta.RequestID = id
	representation, err := json.Marshal(data)
	if err != nil {
		s.internalError(w, r, "encode response representation", err)
		return
	}
	digest := sha256.Sum256(representation)
	// The response metadata contains a per-request ID, so this validator is
	// intentionally weak and represents the stable resource data only.
	etag := `W/"` + hex.EncodeToString(digest[:]) + `"`
	w.Header().Set("Cache-Control", publicCacheControl)
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(responseEnvelope{
		Data: data,
		Meta: meta,
	})
}

func (s *server) writePrivate(w http.ResponseWriter, r *http.Request, status int, data any) {
	id := responseRequestID(w, r)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(responseEnvelope{Data: data, Meta: responseMeta{RequestID: id}})
}

func (s *server) decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "application/json" {
		s.writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return false
	}
	reader := http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_json", "Request body must contain one valid JSON object with known fields.")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		s.writeError(w, r, http.StatusBadRequest, "invalid_json", "Request body must contain exactly one JSON object.")
		return false
	}
	return true
}

func validOptionalHTTPURL(value *string) bool {
	if value == nil || strings.TrimSpace(*value) == "" {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(*value))
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" && parsed.User == nil
}

func validRecordingObjectKey(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.Contains(value, "//") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return !strings.Contains(value, "://")
}

func (s *server) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	id := responseRequestID(w, r)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Error: errorDTO{
		Code: code, Message: message, RequestID: id,
	}})
}

func responseRequestID(w http.ResponseWriter, r *http.Request) string {
	id := ""
	if r != nil {
		id = requestid.From(r.Context())
	}
	if id == "" {
		id = requestid.New()
	}
	w.Header().Set("X-Request-ID", id)
	return id
}

func (s *server) internalError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	if s.app != nil && s.app.Err != nil {
		s.app.Err.Printf("api %s request_id=%s: %s", operation, requestid.From(r.Context()), err)
	}
	s.writeError(w, r, http.StatusInternalServerError, "internal_error", "The API could not complete that request.")
}

func matchesETag(header, etag string) bool {
	want := strings.TrimPrefix(etag, "W/")
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == want {
			return true
		}
	}
	return false
}

func acceptsJSON(header string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return true
	}
	for _, item := range strings.Split(header, ",") {
		mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(item, ";", 2)[0]))
		switch mediaType {
		case "*/*", "application/*", "application/json":
			return true
		}
	}
	return false
}

func conferenceFromDomain(conf *types.Conf) conferenceDTO {
	return conferenceDTO{
		ID: conf.Ref, Tag: conf.Tag, Description: conf.Desc,
		EditionType: conf.EditionType, Tagline: conf.Tagline,
		Timezone: conf.Timezone, Location: conf.Location, Venue: conf.Venue,
		StartsAt: timeString(conf.StartDate), EndsAt: timeString(conf.EndDate),
	}
}

func daysFromDomain(days []*types.ConfInfo) []conferenceDayDTO {
	out := make([]conferenceDayDTO, 0, len(days))
	for _, day := range days {
		if day == nil {
			continue
		}
		venues := append([]string(nil), day.Venues...)
		if venues == nil {
			venues = []string{}
		}
		out = append(out, conferenceDayDTO{
			ID: day.ID, DayNumber: day.Day, Venues: venues,
			Doors: rangeFromDomain(day.Doors), Breakfast: rangeFromDomain(day.Breakfast),
			Lunch: rangeFromDomain(day.Lunch), Coffee: rangeFromDomain(day.Coffee),
		})
	}
	return out
}

func publicTalksFromDomain(conf *types.Conf, talks []*types.Talk, now time.Time) []talkDTO {
	public := make([]*types.Talk, 0, len(talks))
	for _, talk := range talks {
		if isPublicAgendaTalk(conf, talk, now) {
			public = append(public, talk)
		}
	}
	sort.SliceStable(public, func(i, j int) bool {
		if public[i].Sched.Start.Equal(public[j].Sched.Start) {
			return public[i].ID < public[j].ID
		}
		return public[i].Sched.Start.Before(public[j].Sched.Start)
	})
	out := make([]talkDTO, 0, len(public))
	for _, talk := range public {
		out = append(out, talkFromDomain(talk))
	}
	return out
}

func isPublicAgendaTalk(conf *types.Conf, talk *types.Talk, now time.Time) bool {
	if talk == nil || talk.Sched == nil {
		return false
	}
	if talk.Status == "Scheduled" {
		return true
	}
	return conf != nil && conf.HasEndedAt(now) && talk.Status == "Accepted"
}

func talkFromDomain(talk *types.Talk) talkDTO {
	speakers := make([]talkSpeakerDTO, 0, len(talk.Speakers))
	seen := make(map[string]bool, len(talk.Speakers))
	for _, speaker := range talk.Speakers {
		if speaker == nil || speaker.ID == "" || seen[speaker.ID] {
			continue
		}
		seen[speaker.ID] = true
		speakers = append(speakers, talkSpeakerDTO{
			PersonID: speaker.ID, Name: speaker.Name, Company: speaker.Company,
		})
	}
	var startsAt *string
	var endsAt *string
	if talk.Sched != nil {
		startsAt = timeString(talk.Sched.Start)
		endsAt = optionalTime(talk.Sched.End)
	}
	return talkDTO{
		ID: talk.ID, Title: talk.Name, Description: talk.Description, Kind: talk.Type,
		StartsAt: startsAt, EndsAt: endsAt,
		Venue: talk.Venue, Section: talk.Section, Speakers: speakers,
		RepositoryURL: optionalString(talk.GithubRepoURL), SlidesURL: optionalString(talk.SlidesURL),
		RecordingURL: optionalString(talk.YTLink),
	}
}

func recordingCandidateFromDomain(talk *types.Talk, recording *types.Recording) recordingCandidateDTO {
	policy := "full"
	reasons := make([]string, 0)
	eligible := true
	if talk.RecordingRestricted {
		policy = "prohibited"
		eligible = false
		reasons = append(reasons, "speaker_recording_consent_prohibits_recording")
	} else if talk.RecordingAudioOnly {
		policy = "audio_only"
	}
	var startsAt, endsAt *string
	if talk.Sched == nil || talk.Sched.Start.IsZero() {
		eligible = false
		reasons = append(reasons, "talk_is_not_scheduled")
	} else {
		startsAt = timeString(talk.Sched.Start)
		endsAt = optionalTime(talk.Sched.End)
	}
	return recordingCandidateDTO{
		TalkID: talk.ID, Title: talk.Name, Status: talk.Status, StartsAt: startsAt,
		EndsAt: endsAt, Venue: talk.Venue, Speakers: talkSpeakersFromDomain(talk.Speakers),
		RecordingPolicy: policy, Eligible: eligible, Reasons: reasons,
		Recording: recordingAdminFromDomain(recording),
	}
}

func recordingAdminFromDomain(recording *types.Recording) *recordingAdminDTO {
	if recording == nil {
		return nil
	}
	return &recordingAdminDTO{
		ID: recording.ID, TalkID: recording.ConfTalkID, TalkName: recording.TalkName,
		FileURI: recording.FileURI, YouTubeURL: recording.YTLink, XURL: recording.XLink,
		XReplyURL: recording.XReplyLink, PublishedAt: optionalTime(recording.PublishAt),
	}
}

func profileHasTalkAtConference(profile *getters.PublicProfile, tag string) bool {
	for _, row := range profile.Talks {
		if row != nil && row.Conf != nil && row.Conf.Tag == tag {
			return true
		}
	}
	return false
}

func personSummaryFromDomain(speaker *types.Speaker, slug string) personSummaryDTO {
	return personSummaryDTO{
		ID: speaker.ID, PublicID: slug, ProfileURL: "/whois/" + url.PathEscape(slug),
		Name: speaker.Name, AvatarURL: speakerPhotoURL(speaker.Photo),
		Company: optionalString(speaker.Company), Biography: optionalString(speaker.Bio),
	}
}

func personFromDomain(profile *getters.PublicProfile, publicIDs map[string]string) personDTO {
	speaker := profile.Speaker
	out := personDTO{
		personSummaryDTO: personSummaryFromDomain(speaker, publicIDs[speaker.ID]),
		Links: personLinksDTO{
			Website: normalizedProfileURL(speaker.Website, ""), Nostr: optionalString(speaker.Nostr),
			X: optionalString(speaker.Twitter.Link()), GitHub: normalizedProfileURL(speaker.Github, "github.com"),
			Instagram: normalizedProfileURL(speaker.Instagram, "instagram.com"),
			LinkedIn:  normalizedProfileURL(speaker.LinkedIn, "linkedin.com/in"),
			LeetCode:  normalizedProfileURL(speaker.LeetCode, "leetcode.com"),
		},
		Stats:    personStatsDTO{Editions: len(profile.Editions), Talks: len(profile.Talks), Projects: len(profile.Projects)},
		Editions: make([]personEditionDTO, 0, len(profile.Editions)),
		Talks:    make([]personTalkDTO, 0, len(profile.Talks)),
		Projects: make([]personProjectDTO, 0, len(profile.Projects)),
	}
	for _, conf := range profile.Editions {
		if conf != nil {
			out.Editions = append(out.Editions, personEditionDTO{Conference: conferenceFromDomain(conf)})
		}
	}
	for _, row := range profile.Talks {
		if row == nil || row.Talk == nil || row.Conf == nil {
			continue
		}
		talk := row.Talk
		item := personTalkDTO{
			ID: talk.ID, Conference: conferenceFromDomain(row.Conf), Title: talk.Name,
			Description: talk.Description, Kind: talk.Type, Venue: talk.Venue,
			Speakers: talkSpeakersFromDomain(talk.Speakers), RecordingURL: optionalString(talk.YTLink),
			SlidesURL: optionalString(talk.SlidesURL), RepositoryURL: optionalString(talk.GithubRepoURL),
		}
		if talk.Sched != nil && !talk.Sched.Start.IsZero() {
			item.StartsAt = optionalTime(&talk.Sched.Start)
			item.EndsAt = optionalTime(talk.Sched.End)
		}
		out.Talks = append(out.Talks, item)
	}
	for _, row := range profile.Projects {
		if row == nil || row.Project == nil || row.Conf == nil {
			continue
		}
		project := row.Project
		item := personProjectDTO{
			ID: project.ID, Conference: conferenceFromDomain(row.Conf), Title: project.Title,
			ShortDescription: project.ShortDescription,
			ProjectURL:       "/" + url.PathEscape(row.Conf.Tag) + "/hackathon/projects/" + url.PathEscape(project.ID),
			ImageURL:         optionalString(project.ImageURL), Tags: append([]string{}, project.Tags...),
			RepositoryURL: optionalString(project.GitHubURL), DemoURL: optionalString(project.DemoURL),
			VideoURL: optionalString(project.VideoURL), SlidesURL: optionalString(project.SlidesURL),
			DocsURL: optionalString(project.DocsURL), Teammates: make([]projectMemberDTO, 0, len(row.Members)),
			Awards: make([]projectAwardDTO, 0, len(row.Awards)),
		}
		for _, member := range row.Members {
			if member == nil {
				continue
			}
			memberSlug := publicIDs[member.PersonID]
			var memberPublicID, memberURL *string
			if memberSlug != "" {
				memberPublicID = optionalString(memberSlug)
				memberURL = optionalString("/whois/" + url.PathEscape(memberSlug))
			}
			item.Teammates = append(item.Teammates, projectMemberDTO{
				PersonID: member.PersonID, PublicID: memberPublicID, ProfileURL: memberURL,
				Name: member.Name, AvatarURL: speakerPhotoURL(member.Photo), Role: member.Role,
			})
		}
		for _, award := range row.Awards {
			if award != nil {
				item.Awards = append(item.Awards, projectAwardDTO{ID: award.ID, Title: award.Title, Rank: award.Rank})
			}
		}
		out.Projects = append(out.Projects, item)
	}
	return out
}

func talkSpeakersFromDomain(speakers []*types.Speaker) []talkSpeakerDTO {
	out := make([]talkSpeakerDTO, 0, len(speakers))
	seen := make(map[string]bool, len(speakers))
	for _, speaker := range speakers {
		if speaker == nil || speaker.ID == "" || seen[speaker.ID] {
			continue
		}
		seen[speaker.ID] = true
		out = append(out, talkSpeakerDTO{PersonID: speaker.ID, Name: speaker.Name, Company: speaker.Company})
	}
	return out
}

func speakerPhotoURL(photo string) string {
	if strings.TrimSpace(photo) == "" {
		return spaces.PublicURL("speakers/default.avif")
	}
	return spaces.PublicURL("speakers/" + strings.TrimSpace(photo))
}

func normalizedProfileURL(raw, host string) *string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(strings.ToLower(raw), "http://") || strings.HasPrefix(strings.ToLower(raw), "https://") {
		return optionalString(raw)
	}
	raw = strings.TrimPrefix(raw, "@")
	if host != "" {
		raw = "https://" + strings.TrimSuffix(host, "/") + "/" + strings.TrimPrefix(raw, "/")
	} else {
		raw = "https://" + raw
	}
	return optionalString(raw)
}

func isPublicSponsorship(sponsorship *types.Sponsorship) bool {
	if sponsorship == nil || sponsorship.Org == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(sponsorship.Status)) {
	case "paid", "committed":
		return true
	default:
		return false
	}
}

func organizationFromDomain(organization *types.Org) organizationDTO {
	return organizationDTO{
		ID: organization.Ref, Name: organization.Name, Tagline: organization.Tagline,
		LogoLight: optionalString(organization.LogoLight), LogoDark: optionalString(organization.LogoDark),
		Website:   normalizedProfileURL(organization.Website, ""),
		LinkedIn:  normalizedProfileURL(organization.LinkedIn, "linkedin.com/company"),
		Instagram: normalizedProfileURL(organization.Instagram, "instagram.com"),
		YouTube:   normalizedProfileURL(organization.Youtube, "youtube.com"),
		GitHub:    normalizedProfileURL(organization.Github, "github.com"),
		X:         optionalString(organization.Twitter.Link()), Nostr: optionalString(organization.Nostr),
		Matrix: optionalString(organization.Matrix), Hiring: organization.Hiring,
	}
}

func sponsorFromDomain(sponsorship *types.Sponsorship) sponsorDTO {
	return sponsorDTO{
		ID: sponsorship.Ref, Name: sponsorship.Name, Level: sponsorship.Level,
		Label: sponsorship.Label, Organization: organizationFromDomain(sponsorship.Org),
	}
}

func (s *server) publicOrganizationIDs() (map[string]bool, error) {
	conferences, err := s.source.ListConferences()
	if err != nil {
		return nil, err
	}
	visible := map[string]bool{}
	publicConferenceIDs := map[string]bool{}
	for _, conf := range conferences {
		if conf == nil || !conf.IsPublished() {
			continue
		}
		publicConferenceIDs[conf.Ref] = true
		sponsorships, err := s.source.ListSponsorships(conf.Ref)
		if err != nil {
			return nil, err
		}
		for _, sponsorship := range sponsorships {
			if isPublicSponsorship(sponsorship) {
				visible[sponsorship.Org.Ref] = true
			}
		}
	}
	competitions, err := s.source.ListCompetitions()
	if err != nil {
		return nil, err
	}
	for _, competition := range competitions {
		if competition == nil || competition.Visibility != getters.CompetitionVisibilityPublic || !publicConferenceIDs[competition.ConferenceID] {
			continue
		}
		awards, err := s.source.ListAwards(competition.ID)
		if err != nil {
			return nil, err
		}
		for _, award := range awards {
			if award != nil && award.Status != getters.AwardStatusDraft && strings.TrimSpace(award.SponsoredByOrgID) != "" {
				visible[award.SponsoredByOrgID] = true
			}
		}
	}
	return visible, nil
}

func (s *server) publicRecordings(w http.ResponseWriter, r *http.Request) ([]recordingDTO, bool) {
	conferences, err := s.source.ListConferences()
	if err != nil {
		s.internalError(w, r, "list recording conferences", err)
		return nil, false
	}
	type talkContext struct{ tag, title string }
	talksByID := map[string]talkContext{}
	for _, conf := range conferences {
		if conf == nil || !conf.IsPublished() {
			continue
		}
		talks, err := s.source.ListConferenceTalks(conf.Tag)
		if err != nil {
			s.internalError(w, r, "list recording talks", err)
			return nil, false
		}
		for _, talk := range talks {
			if talk != nil {
				talksByID[talk.ID] = talkContext{tag: conf.Tag, title: talk.Name}
			}
		}
	}
	recordings, err := s.source.ListRecordings()
	if err != nil {
		s.internalError(w, r, "list recordings", err)
		return nil, false
	}
	data := make([]recordingDTO, 0, len(recordings))
	for _, recording := range recordings {
		if recording == nil || !recordingIsPublic(recording, s.now()) {
			continue
		}
		context, knownTalk := talksByID[recording.ConfTalkID]
		if !knownTalk {
			continue
		}
		title := strings.TrimSpace(recording.TalkName)
		if title == "" {
			title = context.title
		}
		data = append(data, recordingDTO{
			ID: recording.ID, TalkID: recording.ConfTalkID, ConferenceTag: context.tag, Title: title,
			YouTubeURL: optionalString(recording.YTLink), XURL: optionalString(recording.XLink),
			XReplyURL: optionalString(recording.XReplyLink), PublishedAt: optionalTime(recording.PublishAt),
		})
	}
	return data, true
}

func recordingIsPublic(recording *types.Recording, now time.Time) bool {
	if recording == nil || (strings.TrimSpace(recording.YTLink) == "" && strings.TrimSpace(recording.XLink) == "") {
		return false
	}
	return recording.PublishAt == nil || !recording.PublishAt.After(now)
}

func (s *server) publicCompetition(w http.ResponseWriter, r *http.Request) (*types.HackathonCompetition, *types.Conf, bool) {
	competitionID := strings.TrimSpace(mux.Vars(r)["competitionID"])
	competitions, err := s.source.ListCompetitions()
	if err != nil {
		s.internalError(w, r, "list hackathons", err)
		return nil, nil, false
	}
	var competition *types.HackathonCompetition
	for _, candidate := range competitions {
		if candidate != nil && candidate.ID == competitionID && candidate.Visibility == getters.CompetitionVisibilityPublic {
			competition = candidate
			break
		}
	}
	if competition == nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Hackathon not found.")
		return nil, nil, false
	}
	conferences, err := s.source.ListConferences()
	if err != nil {
		s.internalError(w, r, "list hackathon conference", err)
		return nil, nil, false
	}
	for _, conf := range conferences {
		if conf != nil && conf.Ref == competition.ConferenceID && conf.IsPublished() {
			return competition, conf, true
		}
	}
	s.writeError(w, r, http.StatusNotFound, "not_found", "Hackathon not found.")
	return nil, nil, false
}

func hackathonFromDomain(competition *types.HackathonCompetition, conf *types.Conf) hackathonDTO {
	return hackathonDTO{
		ID: competition.ID, Conference: conferenceFromDomain(conf), Title: competition.Title,
		Description: competition.Description, PublicGalleryEnabled: competition.PublicGalleryEnabled,
		MaxTeamSize: competition.MaxTeamSize, SubmissionsOpenAt: optionalTime(competition.SubmissionsOpenAt),
		SubmissionsCloseAt: optionalTime(competition.SubmissionsCloseAt),
		HackingStartsAt:    optionalTime(competition.HackingStartsAt), HackingEndsAt: optionalTime(competition.HackingEndsAt),
		ExpoStartsAt: optionalTime(competition.ExpoStartsAt), ExpoEndsAt: optionalTime(competition.ExpoEndsAt),
		FinalsStartsAt: optionalTime(competition.FinalsStartsAt), FinalsEndsAt: optionalTime(competition.FinalsEndsAt),
		AwardsCeremonyAt: optionalTime(competition.AwardsCeremonyAt), ResultsFinalizedAt: optionalTime(competition.ResultsFinalizedAt),
	}
}

func (s *server) publicCompetitionProjects(w http.ResponseWriter, r *http.Request, competition *types.HackathonCompetition) ([]*types.HackathonProject, map[string][]*types.ProjectMember, map[string]string, bool) {
	if !competition.PublicGalleryEnabled {
		return []*types.HackathonProject{}, map[string][]*types.ProjectMember{}, map[string]string{}, true
	}
	projects, err := s.source.ListProjects(competition.ID)
	if err != nil {
		s.internalError(w, r, "list public hackathon projects", err)
		return nil, nil, nil, false
	}
	members, err := s.source.ListProjectMembers(competition.ID)
	if err != nil {
		s.internalError(w, r, "list public hackathon teammates", err)
		return nil, nil, nil, false
	}
	profiles, publicIDs, ok := s.publicProfiles(w, r)
	_ = profiles
	if !ok {
		return nil, nil, nil, false
	}
	return projects, members, publicIDs, true
}

func hackathonProjectFromDomain(project *types.HackathonProject, members []*types.ProjectMember, publicIDs map[string]string) hackathonProjectDTO {
	out := hackathonProjectDTO{
		ID: project.ID, CompetitionID: project.CompetitionID, ProjectNumber: project.ProjectNumber,
		Title: project.Title, ShortDescription: project.ShortDescription, Description: project.Description,
		ImageURL: optionalString(project.ImageURL), ImageURLs: append([]string{}, project.ImageURLs...), Tags: append([]string{}, project.Tags...),
		RepositoryURL: optionalString(project.GitHubURL), DemoURL: optionalString(project.DemoURL),
		VideoURL: optionalString(project.VideoURL), SlidesURL: optionalString(project.SlidesURL), DocsURL: optionalString(project.DocsURL),
		Teammates: make([]projectMemberDTO, 0, len(members)), SubmittedAt: optionalTime(project.SubmittedAt),
	}
	for _, member := range members {
		if member == nil {
			continue
		}
		slug := publicIDs[member.PersonID]
		var publicID, profileURL *string
		if slug != "" {
			publicID = optionalString(slug)
			profileURL = optionalString("/whois/" + url.PathEscape(slug))
		}
		out.Teammates = append(out.Teammates, projectMemberDTO{
			PersonID: member.PersonID, PublicID: publicID, ProfileURL: profileURL,
			Name: member.Name, AvatarURL: speakerPhotoURL(member.Photo), Role: member.Role,
		})
	}
	return out
}

func (s *server) publicAwards(w http.ResponseWriter, r *http.Request, competitionID string) ([]awardDTO, bool) {
	awards, err := s.source.ListAwards(competitionID)
	if err != nil {
		s.internalError(w, r, "list public hackathon awards", err)
		return nil, false
	}
	prizes, err := s.source.ListPrizes(competitionID)
	if err != nil {
		s.internalError(w, r, "list public hackathon prizes", err)
		return nil, false
	}
	prizesByAward := map[string][]prizeDTO{}
	for _, prize := range prizes {
		if prize == nil {
			continue
		}
		prizesByAward[prize.AwardID] = append(prizesByAward[prize.AwardID], prizeDTO{
			ID: prize.ID, Type: prize.PrizeType, Title: prize.Title, Description: prize.Description,
			ValueText: prize.ValueText, PoolPercentage: prize.PoolPercentage, PoolURL: optionalString(prize.PoolURL),
		})
	}
	data := make([]awardDTO, 0, len(awards))
	for _, award := range awards {
		if award == nil || award.Status == getters.AwardStatusDraft {
			continue
		}
		data = append(data, awardDTO{
			ID: award.ID, CompetitionID: award.CompetitionID, SponsoredByOrgID: optionalString(award.SponsoredByOrgID),
			Type: award.AwardType, Title: award.Title, Description: award.Description, Rank: award.AwardRank,
			MaximumAwardees: award.MaxAwardees, OptInRequired: award.OptInRequired, FinalistsOnly: award.FinalistsOnly,
			Prizes: append([]prizeDTO{}, prizesByAward[award.ID]...),
		})
	}
	return data, true
}

func rangeFromDomain(value *types.Times) *timeRangeDTO {
	if value == nil || value.Start.IsZero() {
		return nil
	}
	return &timeRangeDTO{StartsAt: value.Start.Format(time.RFC3339), EndsAt: optionalTime(value.End)}
}

func timeString(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}

func optionalTime(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

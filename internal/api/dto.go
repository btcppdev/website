package api

type responseMeta struct {
	RequestID  string `json:"request_id"`
	NextCursor string `json:"next_cursor,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type responseEnvelope struct {
	Data any          `json:"data"`
	Meta responseMeta `json:"meta"`
}

type errorEnvelope struct {
	Error errorDTO `json:"error"`
}

type errorDTO struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

type bootstrapDTO struct {
	APIVersion string            `json:"api_version"`
	Links      map[string]string `json:"links"`
}

type conferenceDTO struct {
	ID          string  `json:"id"`
	Tag         string  `json:"tag"`
	Description string  `json:"description"`
	EditionType string  `json:"edition_type"`
	Tagline     string  `json:"tagline"`
	Timezone    string  `json:"timezone"`
	Location    string  `json:"location"`
	Venue       string  `json:"venue"`
	StartsAt    *string `json:"starts_at"`
	EndsAt      *string `json:"ends_at"`
}

type timeRangeDTO struct {
	StartsAt string  `json:"starts_at"`
	EndsAt   *string `json:"ends_at"`
}

type conferenceDayDTO struct {
	ID        string        `json:"id"`
	DayNumber int           `json:"day_number"`
	Venues    []string      `json:"venues"`
	Doors     *timeRangeDTO `json:"doors"`
	Breakfast *timeRangeDTO `json:"breakfast"`
	Lunch     *timeRangeDTO `json:"lunch"`
	Coffee    *timeRangeDTO `json:"coffee"`
}

type talkSpeakerDTO struct {
	PersonID string `json:"person_id"`
	Name     string `json:"name"`
	Company  string `json:"company"`
}

type talkDTO struct {
	ID            string           `json:"id"`
	Title         string           `json:"title"`
	Description   string           `json:"description"`
	Kind          string           `json:"kind"`
	StartsAt      *string          `json:"starts_at"`
	EndsAt        *string          `json:"ends_at"`
	Venue         string           `json:"venue"`
	Section       string           `json:"section"`
	Speakers      []talkSpeakerDTO `json:"speakers"`
	RepositoryURL *string          `json:"repository_url"`
	SlidesURL     *string          `json:"slides_url"`
	RecordingURL  *string          `json:"recording_url"`
}

type agendaDTO struct {
	Conference conferenceDTO      `json:"conference"`
	Days       []conferenceDayDTO `json:"days"`
	Talks      []talkDTO          `json:"talks"`
}

type personSummaryDTO struct {
	ID         string  `json:"id"`
	PublicID   string  `json:"public_id"`
	ProfileURL string  `json:"profile_url"`
	Name       string  `json:"name"`
	AvatarURL  string  `json:"avatar_url"`
	Company    *string `json:"company"`
	Biography  *string `json:"biography"`
}

type personLinksDTO struct {
	Website   *string `json:"website"`
	Nostr     *string `json:"nostr"`
	X         *string `json:"x"`
	GitHub    *string `json:"github"`
	Instagram *string `json:"instagram"`
	LinkedIn  *string `json:"linkedin"`
	LeetCode  *string `json:"leetcode"`
}

type personStatsDTO struct {
	Editions int `json:"editions"`
	Talks    int `json:"talks"`
	Projects int `json:"projects"`
}

type personEditionDTO struct {
	Conference conferenceDTO `json:"conference"`
}

type personTalkDTO struct {
	ID            string           `json:"id"`
	Conference    conferenceDTO    `json:"conference"`
	Title         string           `json:"title"`
	Description   string           `json:"description"`
	Kind          string           `json:"kind"`
	StartsAt      *string          `json:"starts_at"`
	EndsAt        *string          `json:"ends_at"`
	Venue         string           `json:"venue"`
	Speakers      []talkSpeakerDTO `json:"speakers"`
	RecordingURL  *string          `json:"recording_url"`
	SlidesURL     *string          `json:"slides_url"`
	RepositoryURL *string          `json:"repository_url"`
}

type projectMemberDTO struct {
	PersonID   string  `json:"person_id"`
	PublicID   *string `json:"public_id"`
	ProfileURL *string `json:"profile_url"`
	Name       string  `json:"name"`
	AvatarURL  string  `json:"avatar_url"`
	Role       string  `json:"role"`
}

type projectAwardDTO struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Rank  *int   `json:"rank"`
}

type personProjectDTO struct {
	ID               string             `json:"id"`
	Conference       conferenceDTO      `json:"conference"`
	Title            string             `json:"title"`
	ShortDescription string             `json:"short_description"`
	ProjectURL       string             `json:"project_url"`
	ImageURL         *string            `json:"image_url"`
	Tags             []string           `json:"tags"`
	RepositoryURL    *string            `json:"repository_url"`
	DemoURL          *string            `json:"demo_url"`
	VideoURL         *string            `json:"video_url"`
	SlidesURL        *string            `json:"slides_url"`
	DocsURL          *string            `json:"docs_url"`
	Teammates        []projectMemberDTO `json:"teammates"`
	Awards           []projectAwardDTO  `json:"awards"`
}

type personDTO struct {
	personSummaryDTO
	Links    personLinksDTO     `json:"links"`
	Stats    personStatsDTO     `json:"stats"`
	Editions []personEditionDTO `json:"editions"`
	Talks    []personTalkDTO    `json:"talks"`
	Projects []personProjectDTO `json:"projects"`
}

type accountEmailDTO struct {
	Email      string `json:"email"`
	IsPrimary  bool   `json:"is_primary"`
	VerifiedAt string `json:"verified_at"`
}

type accountProfileDTO struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Photo     string            `json:"photo"`
	Company   string            `json:"company"`
	Biography string            `json:"biography"`
	Website   string            `json:"website"`
	X         string            `json:"x"`
	GitHub    string            `json:"github"`
	Instagram string            `json:"instagram"`
	LinkedIn  string            `json:"linkedin"`
	LeetCode  string            `json:"leetcode"`
	Nostr     string            `json:"nostr"`
	Emails    []accountEmailDTO `json:"emails"`
	Roles     []string          `json:"roles"`
}

type accountTalkDTO struct {
	ID               string   `json:"id"`
	ConferenceTag    string   `json:"conference_tag"`
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	Kind             string   `json:"kind"`
	Status           string   `json:"status"`
	DesiredDuration  int      `json:"desired_duration_minutes"`
	SpeakerPersonIDs []string `json:"speaker_person_ids"`
}

type profilePatchDTO struct {
	Name      *string `json:"name"`
	Company   *string `json:"company"`
	Biography *string `json:"biography"`
	Website   *string `json:"website"`
	X         *string `json:"x"`
	GitHub    *string `json:"github"`
	Instagram *string `json:"instagram"`
	LinkedIn  *string `json:"linkedin"`
	LeetCode  *string `json:"leetcode"`
}

type talkPatchDTO struct {
	Title                  *string `json:"title"`
	Description            *string `json:"description"`
	Kind                   *string `json:"kind"`
	DesiredDurationMinutes *int    `json:"desired_duration_minutes"`
	RepositoryURL          *string `json:"repository_url"`
	SlidesURL              *string `json:"slides_url"`
}

type scheduleUpdateDTO struct {
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`
	Venue    string `json:"venue"`
}

type recordingCandidateDTO struct {
	TalkID          string             `json:"talk_id"`
	Title           string             `json:"title"`
	Status          string             `json:"status"`
	StartsAt        *string            `json:"starts_at"`
	EndsAt          *string            `json:"ends_at"`
	Venue           string             `json:"venue"`
	Speakers        []talkSpeakerDTO   `json:"speakers"`
	RecordingPolicy string             `json:"recording_policy"`
	Eligible        bool               `json:"eligible"`
	Reasons         []string           `json:"reasons"`
	Recording       *recordingAdminDTO `json:"recording"`
}

type recordingAdminDTO struct {
	ID          string  `json:"id"`
	TalkID      string  `json:"talk_id"`
	TalkName    string  `json:"talk_name"`
	FileURI     string  `json:"file_uri"`
	YouTubeURL  string  `json:"youtube_url"`
	XURL        string  `json:"x_url"`
	XReplyURL   string  `json:"x_reply_url"`
	PublishedAt *string `json:"published_at"`
}

type recordingUpsertDTO struct {
	TalkName    *string `json:"talk_name"`
	FileURI     *string `json:"file_uri"`
	YouTubeURL  *string `json:"youtube_url"`
	XURL        *string `json:"x_url"`
	XReplyURL   *string `json:"x_reply_url"`
	PublishedAt *string `json:"published_at"`
}

type organizationDTO struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Tagline   string  `json:"tagline"`
	LogoLight *string `json:"logo_light_url"`
	LogoDark  *string `json:"logo_dark_url"`
	Website   *string `json:"website_url"`
	LinkedIn  *string `json:"linkedin_url"`
	Instagram *string `json:"instagram_url"`
	YouTube   *string `json:"youtube_url"`
	GitHub    *string `json:"github_url"`
	X         *string `json:"x_url"`
	Nostr     *string `json:"nostr"`
	Matrix    *string `json:"matrix"`
	Hiring    bool    `json:"hiring"`
}

type sponsorDTO struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Level        string          `json:"level"`
	Label        string          `json:"label"`
	Organization organizationDTO `json:"organization"`
}

type recordingDTO struct {
	ID            string  `json:"id"`
	TalkID        string  `json:"talk_id"`
	ConferenceTag string  `json:"conference_tag"`
	Title         string  `json:"title"`
	YouTubeURL    *string `json:"youtube_url"`
	XURL          *string `json:"x_url"`
	XReplyURL     *string `json:"x_reply_url"`
	PublishedAt   *string `json:"published_at"`
}

type hackathonDTO struct {
	ID                   string        `json:"id"`
	Conference           conferenceDTO `json:"conference"`
	Title                string        `json:"title"`
	Description          string        `json:"description"`
	PublicGalleryEnabled bool          `json:"public_gallery_enabled"`
	MaxTeamSize          *int          `json:"max_team_size"`
	SubmissionsOpenAt    *string       `json:"submissions_open_at"`
	SubmissionsCloseAt   *string       `json:"submissions_close_at"`
	HackingStartsAt      *string       `json:"hacking_starts_at"`
	HackingEndsAt        *string       `json:"hacking_ends_at"`
	ExpoStartsAt         *string       `json:"expo_starts_at"`
	ExpoEndsAt           *string       `json:"expo_ends_at"`
	FinalsStartsAt       *string       `json:"finals_starts_at"`
	FinalsEndsAt         *string       `json:"finals_ends_at"`
	AwardsCeremonyAt     *string       `json:"awards_ceremony_at"`
	ResultsFinalizedAt   *string       `json:"results_finalized_at"`
}

type hackathonProjectDTO struct {
	ID               string             `json:"id"`
	CompetitionID    string             `json:"competition_id"`
	ProjectNumber    *int               `json:"project_number"`
	Title            string             `json:"title"`
	ShortDescription string             `json:"short_description"`
	Description      string             `json:"description"`
	ImageURL         *string            `json:"image_url"`
	ImageURLs        []string           `json:"image_urls"`
	Tags             []string           `json:"tags"`
	RepositoryURL    *string            `json:"repository_url"`
	DemoURL          *string            `json:"demo_url"`
	VideoURL         *string            `json:"video_url"`
	SlidesURL        *string            `json:"slides_url"`
	DocsURL          *string            `json:"docs_url"`
	Teammates        []projectMemberDTO `json:"teammates"`
	SubmittedAt      *string            `json:"submitted_at"`
}

type prizeDTO struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	ValueText      string   `json:"value_text"`
	PoolPercentage *float64 `json:"pool_percentage"`
	PoolURL        *string  `json:"pool_url"`
}

type awardDTO struct {
	ID               string     `json:"id"`
	CompetitionID    string     `json:"competition_id"`
	SponsoredByOrgID *string    `json:"sponsored_by_organization_id"`
	Type             string     `json:"type"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Rank             *int       `json:"rank"`
	MaximumAwardees  *int       `json:"maximum_awardees"`
	OptInRequired    bool       `json:"opt_in_required"`
	FinalistsOnly    bool       `json:"finalists_only"`
	Prizes           []prizeDTO `json:"prizes"`
}

type resultDTO struct {
	Award   awardDTO            `json:"award"`
	Project hackathonProjectDTO `json:"project"`
}

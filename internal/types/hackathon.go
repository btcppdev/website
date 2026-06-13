package types

import "time"

type HackathonCompetition struct {
	ID                    string
	ConferenceID          string
	Slug                  string
	Title                 string
	Description           string
	Visibility            string
	MaxTeamSize           *int
	SubmissionsOpenAt     *time.Time
	SubmissionsCloseAt    *time.Time
	PublicGalleryAt       *time.Time
	HackingStartsAt       *time.Time
	HackingEndsAt         *time.Time
	JudgesMeetingAt       *time.Time
	ExpoStartsAt          *time.Time
	ExpoEndsAt            *time.Time
	ExpoJudgingStartsAt   *time.Time
	ExpoJudgingEndsAt     *time.Time
	FinalsStartsAt        *time.Time
	FinalsEndsAt          *time.Time
	FinalsJudgingStartsAt *time.Time
	FinalsJudgingEndsAt   *time.Time
	AwardsCeremonyAt      *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type HackathonProject struct {
	ID                string
	CompetitionID     string
	CreatedByPersonID string
	Slug              string
	Title             string
	ShortDescription  string
	Description       string
	GitHubURL         string
	DemoURL           string
	VideoURL          string
	SlidesURL         string
	DocsURL           string
	ProjectNumber     *int
	Status            string
	Tags              []string
	SubmittedAt       *time.Time
	ShippedAt         *time.Time
	PublishedAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ProjectMember struct {
	ProjectID string
	PersonID  string
	Name      string
	Email     string
	Role      string
	CreatedAt time.Time
}

type ProjectInvite struct {
	ID                 string
	ProjectID          string
	Email              string
	AcceptedByPersonID string
	AcceptedAt         *time.Time
	ExpiresAt          *time.Time
	CreatedAt          time.Time
}

type CompetitionJudge struct {
	CompetitionID string
	PersonID      string
	Name          string
	Email         string
	JudgeType     string
	CreatedAt     time.Time
}

type JudgeEvent struct {
	ID                    string
	CompetitionID         string
	Name                  string
	PlaybookType          string
	Ordering              int
	StartsAt              *time.Time
	EndsAt                *time.Time
	StartingProjectNumber *int
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type Scorecard struct {
	ID             string
	JudgeEventID   string
	ProjectID      string
	JudgePersonID  string
	IdeaScore      *int
	ExecutionScore *int
	ImpactScore    *int
	Rank           *int
	NoShow         bool
	Comments       string
	SubmittedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type HackathonViewer struct {
	PersonID    string
	Admin       bool
	Coordinator bool
}

package types

import (
	"strings"
	"time"
)

type (
	Org struct {
		Ref                 string
		Name                string
		Tagline             string
		LogoLight           string // URL to light mode logo on Spaces
		LogoDark            string // URL to dark mode logo on Spaces
		Email               string
		Website             string
		LinkedIn            string
		Instagram           string
		Youtube             string
		Github              string
		Twitter             Twitter
		Nostr               string
		Matrix              string
		Hiring              bool
		Notes               string
		MembershipPolicy    string
		HiddenFromDirectory bool
	}

	OrganizationDirectoryEntry struct {
		Organization     *Org
		MembershipRole   string
		MembershipStatus string
		RequestID        string
		RequestStatus    string
	}

	OrganizationMembershipRequest struct {
		ID                 string
		OrganizationID     string
		OrganizationName   string
		PersonID           string
		PersonName         string
		PersonEmail        string
		Message            string
		Status             string
		ReviewedByPersonID string
		ReviewNote         string
		ReviewedAt         *time.Time
		CreatedAt          time.Time
		UpdatedAt          time.Time
	}

	OrganizationApplication struct {
		ID                  string
		SubmittedByPersonID string
		ApplicantName       string
		ApplicantEmail      string
		Name                string
		Tagline             string
		ContactEmail        string
		Website             string
		Github              string
		LogoLight           string
		LogoDark            string
		Notes               string
		Status              string
		ReviewNote          string
		ReviewedByPersonID  string
		ReviewedAt          *time.Time
		OrganizationID      string
		CreatedAt           time.Time
		UpdatedAt           time.Time
	}

	Sponsorship struct {
		Ref   string
		Name  string
		Org   *Org
		Confs []*Conf
		// Level is the canonical tier — drives logo size + columns
		// at render time. One of: Title / Diamond / Gold / Silver /
		// Bronze / Workshop / Hackathon / Networking / Media /
		// Community.
		Level string
		// Label is the section-heading display string for the conf
		// page (e.g. "Satoshi Level Sponsors", "Pool Party Sponsor",
		// "VIP Dinner Sponsor"). Falls back to a per-tier default
		// when blank. Stored as a rich_text field on Notion.
		Label    string
		Status   string
		IsVendor bool
		Notes    string
	}

	OrganizationMembership struct {
		OrganizationID    string
		PersonID          string
		PersonName        string
		PersonEmail       string
		PersonNostr       string
		Role              string
		Status            string
		Organization      *Org
		InvitedByPersonID string
		CreatedAt         time.Time
		UpdatedAt         time.Time
	}

	NotificationRecipient struct {
		PersonID string
		Name     string
		Email    string
	}

	SponsorOrganizationRecipient struct {
		OrganizationID   string
		OrganizationName string
		PersonID         string
		Name             string
		Email            string
	}

	OrganizationMemberInvite struct {
		ID                 string
		OrganizationID     string
		OrganizationName   string
		Email              string
		Role               string
		InvitedByPersonID  string
		AcceptedByPersonID string
		AcceptedAt         *time.Time
		RevokedAt          *time.Time
		ExpiresAt          time.Time
		CreatedAt          time.Time
	}

	SponsorshipEntitlement struct {
		SponsorshipID                    string
		ConferenceID                     string
		TicketAllocation                 int
		SponsorAwardLimit                int
		AllHackathonSubmissions          bool
		AutomaticSubmissionContactAccess bool
		ParticipantContactAccess         bool
		ParticipantContactExport         bool
		CanEditOrganization              bool
		CreatedAt                        time.Time
		UpdatedAt                        time.Time
	}

	SponsorDashboardEvent struct {
		Sponsorship         *Sponsorship
		Conference          *Conf
		Competition         *HackathonCompetition
		Entitlement         *SponsorshipEntitlement
		SpeakerApplications []*SponsorSpeakerApplication
		AwardCount          int
		WinnerCount         int
		TicketsIssued       int
	}

	SponsorSpeakerApplication struct {
		ProposalID     string
		ConferenceID   string
		Title          string
		TalkType       string
		Status         string
		DesiredMinutes int
		MemberNames    []string
		SubmittedAt    time.Time
	}

	SponsorAwardProposal struct {
		ID                  string
		SponsorshipID       string
		ConferenceID        string
		CompetitionID       string
		OrganizationID      string
		OrganizationName    string
		SubmittedByPersonID string
		SubmittedByName     string
		Title               string
		Description         string
		JudgingInstructions string
		MaxAwardees         int
		OptInRequired       bool
		FinalistsOnly       bool
		PrizeType           string
		PrizeTitle          string
		PrizeDescription    string
		PrizeValueText      string
		Status              string
		ReviewNotes         string
		ReviewedByPersonID  string
		AwardID             string
		PrizeID             string
		ConferenceTitle     string
		CompetitionTitle    string
		OrganizerManaged    bool
		EditableUntil       *time.Time
		ReviewedAt          *time.Time
		CreatedAt           time.Time
		UpdatedAt           time.Time
	}

	SponsorTicketIssuance struct {
		ID               string
		SponsorshipID    string
		ConferenceID     string
		ConferenceTag    string
		IssuedByPersonID string
		RecipientEmail   string
		Quantity         int
		CheckoutID       string
		CreatedAt        time.Time
	}

	SponsorPrizeEntry struct {
		AwardID                 string
		AwardTitle              string
		AwardStatus             string
		CompetitionID           string
		CompetitionTitle        string
		ConferenceID            string
		ConferenceTag           string
		ConferenceTitle         string
		ProjectID               string
		ProjectTitle            string
		ProjectShortDescription string
		ProjectImageURL         string
		ProjectStatus           string
		ProjectNumber           *int
		GitHubURL               string
		DemoURL                 string
		OptedInAt               time.Time
		Winner                  bool
		GeneralPodiumWinner     bool
		SponsoredPrize          bool
		AutomaticContact        bool
		Participants            []*SponsorPrizeParticipant
	}

	SponsorPrizeParticipant struct {
		PersonID        string
		Name            string
		Photo           string
		Role            string
		Email           string
		PublicID        string
		AvailableToHire bool
		ConsentScope    string
	}

	HackathonSponsorContactConsent struct {
		CompetitionID        string
		PersonID             string
		AllHackathonSponsors bool
		EnteredAwardSponsors bool
		CreatedAt            time.Time
		UpdatedAt            time.Time
	}
)

func (a *SponsorSpeakerApplication) StatusLabel() string {
	if a == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(a.Status)) {
	case "", "applied":
		return "Applied"
	case "inreview":
		return "In review"
	case "theydecline":
		return "Withdrawn"
	case "wedecline", "rejected":
		return "Declined"
	default:
		return strings.TrimSpace(a.Status)
	}
}

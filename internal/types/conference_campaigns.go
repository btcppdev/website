package types

import (
	"strings"
	"time"
)

const (
	ConferenceCampaignAttendeeReminder70 = "attendee-reminder-70"
	ConferenceCampaignAttendeeReminder49 = "attendee-reminder-49"
	ConferenceCampaignAttendeeReminder28 = "attendee-reminder-28"
	ConferenceCampaignSpeakerReminder    = "speaker-reminder"
	ConferenceCampaignAttendeeFinal      = "attendee-final"
	ConferenceCampaignVolunteerOrient    = "volunteer-orientation"
	ConferenceCampaignSpeakerOnboarding  = "speaker-onboarding"
)

const ConferenceCampaignSubjectPrefix = "✨ bitcoin++ {{ .Conf.Tag }} {{ .Conf.Emoji }}: "

func ConferenceCampaignSubject(title string) string {
	title = strings.TrimSpace(title)
	if strings.HasPrefix(title, "✨ bitcoin++ ") {
		return title
	}
	return ConferenceCampaignSubjectPrefix + title
}

func ConferenceCampaignTemplateOnlyFor(kind string) string {
	return "conference-" + kind
}

type ConferenceEmailCampaign struct {
	ID                 string
	ConferenceID       string
	Kind               string
	Audience           string
	Title              string
	Markdown           string
	TemplateMissiveID  string
	TemplateMissiveUID uint64
	Enabled            bool
	SendTime           string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ConferenceEmailOccurrence struct {
	ID            string
	CampaignID    string
	CampaignKind  string
	CampaignTitle string
	Audience      string
	ConferenceID  string
	ConferenceTag string
	Conference    *Conf
	OccurrenceKey string
	BuildAt       time.Time
	SendAt        time.Time
	BuildLabel    string
	SendLabel     string
	MissiveID     string
	MissiveUID    uint64
	TargetKey     string
	TargetEmail   string
	Status        string
	Enabled       bool
	BuiltAt       *time.Time
	QueuedAt      *time.Time
	SentAt        *time.Time
	SkippedAt     *time.Time
	LastError     string
}

type ConferenceEmailDelivery struct {
	ID           string
	OccurrenceID string
	RecipientKey string
	Email        string
	JobKey       string
	Status       string
	QueuedAt     *time.Time
	LastError    string
}

type ConferenceEmailRecipient struct {
	Key           string
	Email         string
	Name          string
	SpeakerConfID string
	VolunteerID   string
	Registrations []*Registration
}

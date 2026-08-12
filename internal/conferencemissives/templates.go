package conferencemissives

import (
	"embed"
	"fmt"
	"strings"

	"btcpp-web/internal/types"
)

//go:embed templates/*.md
var templateFiles embed.FS

type Definition struct {
	Kind     string
	OnlyFor  string
	Label    string
	Title    string
	Markdown string
}

func OnlyFor(kind string) string {
	return types.ConferenceCampaignTemplateOnlyFor(strings.TrimSpace(kind))
}

func Definitions() ([]Definition, error) {
	definitions := []struct {
		kind, label, title, filename string
	}{
		{types.ConferenceCampaignAttendeeReminder70, "Event attendee reminder · 70 days", "{{ .Conf.Desc }} is getting closer", "attendee-reminder.md"},
		{types.ConferenceCampaignAttendeeReminder49, "Event attendee reminder · 49 days", "What to know before {{ .Conf.Desc }}", "attendee-reminder.md"},
		{types.ConferenceCampaignAttendeeReminder28, "Event attendee reminder · 28 days", "One month until {{ .Conf.Desc }}", "attendee-reminder.md"},
		{types.ConferenceCampaignSpeakerReminder, "Event speaker reminder", "{{ .Conf.Desc }} is three weeks away", "speaker-reminder.md"},
		{types.ConferenceCampaignAttendeeFinal, "Event final details and tickets", "Everything you need for {{ .Conf.Desc }}", "attendee-final.md"},
		{types.ConferenceCampaignVolunteerOrient, "Event volunteer orientation reminder", "Volunteer orientation for {{ .Conf.Desc }}", "volunteer-orientation.md"},
		{types.ConferenceCampaignSpeakerOnboarding, "Event speaker onboarding", "You've signed up to speak. Here's what to expect", "speaker-onboarding.md"},
	}
	out := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		body, err := templateFiles.ReadFile("templates/" + definition.filename)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", definition.filename, err)
		}
		out = append(out, Definition{
			Kind: definition.kind, OnlyFor: OnlyFor(definition.kind), Label: definition.label,
			Title: definition.title, Markdown: strings.TrimSpace(string(body)),
		})
	}
	return out, nil
}

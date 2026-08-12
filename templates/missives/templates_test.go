package conferencemissives

import (
	"strings"
	"testing"
	texttemplate "text/template"

	"btcpp-web/internal/types"
)

func TestDefinitionsLoadAndParse(t *testing.T) {
	definitions, err := Definitions()
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 7 {
		t.Fatalf("definition count = %d, want 7", len(definitions))
	}
	seen := make(map[string]bool)
	wantKinds := map[string]bool{
		types.ConferenceCampaignAttendeeReminder70: true,
		types.ConferenceCampaignAttendeeReminder49: true,
		types.ConferenceCampaignAttendeeReminder28: true,
		types.ConferenceCampaignSpeakerReminder:    true,
		types.ConferenceCampaignAttendeeFinal:      true,
		types.ConferenceCampaignVolunteerOrient:    true,
		types.ConferenceCampaignSpeakerOnboarding:  true,
	}
	for _, definition := range definitions {
		if definition.Kind == "" || definition.OnlyFor == "" || definition.Title == "" || definition.Markdown == "" {
			t.Fatalf("incomplete definition: %#v", definition)
		}
		if !strings.Contains(definition.Markdown, `{{ lead `) || !strings.Contains(definition.Markdown, `{{ cta `) {
			t.Fatalf("%s body does not use the newsletter section components", definition.Kind)
		}
		if !strings.Contains(definition.Markdown, `{{ lead `) || !strings.Contains(definition.Markdown, `.CampaignTitle`) {
			t.Fatalf("%s lead does not follow the campaign subject", definition.Kind)
		}
		if strings.Contains(definition.Markdown, "Your participation") {
			t.Fatalf("%s retains an unconditional participation heading", definition.Kind)
		}
		if !strings.HasPrefix(definition.OnlyFor, "conference-") || seen[definition.OnlyFor] {
			t.Fatalf("invalid or duplicate only_for %q", definition.OnlyFor)
		}
		seen[definition.OnlyFor] = true
		if !strings.HasPrefix(definition.Title, types.ConferenceCampaignSubjectPrefix) {
			t.Fatalf("%s subject does not use event prefix: %q", definition.Kind, definition.Title)
		}
		if !wantKinds[definition.Kind] {
			t.Fatalf("unexpected campaign kind %q", definition.Kind)
		}
		delete(wantKinds, definition.Kind)
		if subjectCopy := strings.TrimSpace(strings.TrimPrefix(definition.Title, types.ConferenceCampaignSubjectPrefix)); subjectCopy == "" {
			t.Fatalf("%s subject has no editable title copy", definition.Kind)
		}
		if !strings.HasPrefix(definition.Markdown, "---\n") {
			t.Fatalf("%s body does not use newsletter frontmatter", definition.Kind)
		}
		if _, err := texttemplate.New("subject").Parse(definition.Title); err != nil {
			t.Fatalf("parse %s subject: %v", definition.Kind, err)
		}
		newsletterFuncs := texttemplate.FuncMap{
			"lead": func(...any) string { return "" },
			"cta":  func(...any) string { return "" },
		}
		if _, err := texttemplate.New("body").Funcs(newsletterFuncs).Parse(definition.Markdown); err != nil {
			t.Fatalf("parse %s body: %v", definition.Kind, err)
		}
	}
	if len(wantKinds) != 0 {
		t.Fatalf("missing campaign kinds: %v", wantKinds)
	}
}

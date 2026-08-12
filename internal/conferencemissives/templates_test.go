package conferencemissives

import (
	"strings"
	"testing"
	texttemplate "text/template"
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
	for _, definition := range definitions {
		if definition.Kind == "" || definition.OnlyFor == "" || definition.Title == "" || definition.Markdown == "" {
			t.Fatalf("incomplete definition: %#v", definition)
		}
		if !strings.HasPrefix(definition.OnlyFor, "conference-") || seen[definition.OnlyFor] {
			t.Fatalf("invalid or duplicate only_for %q", definition.OnlyFor)
		}
		seen[definition.OnlyFor] = true
		if _, err := texttemplate.New("subject").Parse(definition.Title); err != nil {
			t.Fatalf("parse %s subject: %v", definition.Kind, err)
		}
		if _, err := texttemplate.New("body").Parse(definition.Markdown); err != nil {
			t.Fatalf("parse %s body: %v", definition.Kind, err)
		}
	}
}

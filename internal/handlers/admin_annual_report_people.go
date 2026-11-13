package handlers

import (
	"net/url"
	"path"
	"strings"
)

type annualPersonHighlight struct {
	Person      reportPerson
	Event, Role string
}

func annualPortraitAvailable(person reportPerson) bool {
	if strings.TrimSpace(person.Name) == "" || strings.TrimSpace(person.Photo) == "" {
		return false
	}
	u, err := url.Parse(person.Photo)
	if err != nil {
		return false
	}
	filename := strings.ToLower(path.Base(u.Path))
	return strings.TrimSuffix(filename, path.Ext(filename)) != "default"
}

// Photo keys also deduplicate older winner snapshots that have no person ID.
func annualClaimPortrait(person reportPerson, seen map[string]bool) bool {
	if !annualPortraitAvailable(person) || seen["photo:"+person.Photo] || (person.ID != "" && seen["id:"+person.ID]) {
		return false
	}
	seen["photo:"+person.Photo] = true
	if person.ID != "" {
		seen["id:"+person.ID] = true
	}
	return true
}

func annualSpeakerPortraits(e annualEvent, limit int) []reportPerson {
	var out []reportPerson
	seen := map[string]bool{}
	for _, person := range e.SpeakerPortraits {
		if annualClaimPortrait(person, seen) {
			out = append(out, person)
			if len(out) == limit {
				break
			}
		}
	}
	return out
}

func annualEventPeople(e annualEvent) []annualPersonHighlight {
	// Prioritize this event's panel participants for the community gallery.
	// Build a separate slice so recap ordering and frozen source data stay intact.
	prioritized := make([]reportPerson, 0, len(e.SpeakerPortraits))
	for _, panel := range []bool{true, false} {
		for _, person := range e.SpeakerPortraits {
			if person.PanelParticipant == panel {
				prioritized = append(prioritized, person)
			}
		}
	}
	e.SpeakerPortraits = prioritized
	speakers := annualSpeakerPortraits(e, len(e.SpeakerPortraits))
	var builders []reportPerson
	seenBuilders := map[string]bool{}
	if e.HackKnown() {
		for _, project := range e.WinningProjects {
			for _, person := range project.Portraits {
				if annualClaimPortrait(person, seenBuilders) {
					builders = append(builders, person)
				}
			}
		}
	}
	var out []annualPersonHighlight
	seen := map[string]bool{}
	// Alternate roles so the gallery includes builders as well as speakers.
	for i := 0; i < len(speakers) || i < len(builders); i++ {
		if i < len(speakers) && annualClaimPortrait(speakers[i], seen) {
			out = append(out, annualPersonHighlight{Person: speakers[i], Event: e.Event.Location, Role: "Speaker"})
		}
		if i < len(builders) && annualClaimPortrait(builders[i], seen) {
			out = append(out, annualPersonHighlight{Person: builders[i], Event: e.Event.Location, Role: "Hackathon builder"})
		}
	}
	return out
}

// Give each included event a place, then aim for an even mix of speakers and
// builders when photos are available. Frozen editions use their saved portraits.
func (p annualReportPage) CommunityPeople() []annualPersonHighlight {
	var groups [][]annualPersonHighlight
	for _, e := range p.Included() {
		groups = append(groups, annualEventPeople(e))
	}
	var out []annualPersonHighlight
	seen := map[string]bool{}
	builders := 0
	take := func(group []annualPersonHighlight, role string) bool {
		for _, person := range group {
			if role != "" && person.Role != role {
				continue
			}
			if annualClaimPortrait(person.Person, seen) {
				out = append(out, person)
				if person.Role == "Hackathon builder" {
					builders++
				}
				return true
			}
		}
		return false
	}
	for _, group := range groups {
		if !take(group, "Hackathon builder") {
			take(group, "")
		}
		if len(out) == 12 {
			return out
		}
	}
	for _, role := range []string{"Hackathon builder", ""} {
		for len(out) < 12 && (role == "" || builders < 6) {
			added := false
			for _, group := range groups {
				if take(group, role) {
					added = true
				}
				if len(out) == 12 || (role != "" && builders == 6) {
					break
				}
			}
			if !added {
				break
			}
		}
	}
	return out
}

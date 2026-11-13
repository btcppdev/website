package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"testing"
)

func TestAnnualReportCommunityPeopleSelection(t *testing.T) {
	person := func(id string) reportPerson {
		return reportPerson{ID: id, Name: id, Photo: "https://example.test/" + id + ".jpg"}
	}
	p := annualReportPage{Events: []annualEvent{
		{Included: true, Event: reportEvent{Location: "Nairobi"}, HackCoverage: "records",
			SpeakerPortraits: []reportPerson{{Name: "Missing photo"}, {Name: "Placeholder", Photo: "https://example.test/default.avif"}, person("Ada"), person("Bo")},
			WinningProjects:  []reportWinner{{Portraits: []reportPerson{person("Builder"), {Name: "Ada", Photo: person("Ada").Photo}}}}},
		{Included: true, Event: reportEvent{Location: "Berlin"}, HackCoverage: "incomplete",
			SpeakerPortraits: []reportPerson{person("Ada"), person("Cai")},
			WinningProjects:  []reportWinner{{Portraits: []reportPerson{person("Incomplete")}}}},
		{Included: false, SpeakerPortraits: []reportPerson{person("Excluded")}},
	}}
	people := p.CommunityPeople()
	if len(people) != 4 || people[0].Person.Name != "Builder" || people[0].Role != "Hackathon builder" || people[1].Event != "Berlin" {
		t.Fatalf("expected distinct speakers and builders across included events: %+v", people)
	}
	if got := annualSpeakerPortraits(p.Events[0], 3); len(got) != 2 || got[0].Name != "Ada" {
		t.Fatalf("recap includes missing photos or wrong roles: %+v", got)
	}
	// Frozen editions should retain the selected people without a fresh lookup.
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var frozen annualReportPage
	if err := json.Unmarshal(raw, &frozen); err != nil {
		t.Fatal(err)
	}
	if got := frozen.CommunityPeople(); len(got) != len(people) || got[2].Person.Photo != people[2].Person.Photo {
		t.Fatal("portraits lost from annual snapshot")
	}
}

func TestAnnualReportCommunityPeopleBalancesEvents(t *testing.T) {
	p := annualReportPage{}
	for event := 0; event < 6; event++ {
		e := annualEvent{Included: true, HackCoverage: "records", Event: reportEvent{Location: fmt.Sprint(event)}}
		for n := 0; n < 8; n++ {
			id := fmt.Sprintf("%d-%d", event, n)
			e.SpeakerPortraits = append(e.SpeakerPortraits, reportPerson{ID: id, Name: id, Photo: "https://example.test/" + id + ".jpg"})
		}
		p.Events = append(p.Events, e)
	}
	people := p.CommunityPeople()
	if len(people) != 12 {
		t.Fatalf("gallery size = %d, want 12", len(people))
	}
	counts := map[string]int{}
	for _, person := range people {
		counts[person.Event]++
	}
	for event := 0; event < 6; event++ {
		if counts[fmt.Sprint(event)] != 2 {
			t.Fatalf("gallery dominated by one event: %+v", counts)
		}
	}
	for event := 0; event < 3; event++ {
		for n := 0; n < 4; n++ {
			id := fmt.Sprintf("builder-%d-%d", event, n)
			p.Events[event].WinningProjects = append(p.Events[event].WinningProjects, reportWinner{Portraits: []reportPerson{{ID: id, Name: id, Photo: "https://example.test/" + id + ".jpg"}}})
		}
	}
	roles := map[string]int{}
	for _, person := range p.CommunityPeople() {
		roles[person.Role]++
	}
	if roles["Speaker"] != 6 || roles["Hackathon builder"] != 6 {
		t.Fatalf("gallery lost the mix of speakers and builders: %+v", roles)
	}
}

func TestAnnualReportPeopleRender(t *testing.T) {
	p := annualFixture()
	p.Events[0].SpeakerPortraits = []reportPerson{{ID: "speaker", Name: "Speaker <one>", Photo: "https://example.test/speaker.jpg"}}
	p.Events[3].WinningProjects[0].Portraits = []reportPerson{{Name: "Builder", Photo: "https://example.test/builder.jpg"}}
	tmpl, err := template.ParseFiles("../../templates/admin/annual_report.tmpl", "../../templates/admin/reports.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, p); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="annual-recap-people"`, `class="annual-people-gallery"`, "Speaker &lt;one&gt;", "Hackathon builder", "builder.jpg"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out.String(), `class="annual-globe"`) {
		t.Fatal("gallery and fallback map both shown")
	}
}
